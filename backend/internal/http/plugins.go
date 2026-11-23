package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/plugin"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
)

func pluginError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, plugin.ErrManifest), errors.Is(e, plugin.ErrFiles), errors.Is(e, plugin.ErrChecksum):
		// Validation reasons are stable strings without data-root paths; the
		// owner needs them to fix the plugin directory.
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "invalid_fields", "message": messages[InvalidFields], "request_id": w.Header().Get("X-Request-ID"), "fields": []siteconfig.FieldIssue{{Path: "plugin", Code: e.Error()}}}})
	case errors.Is(e, plugin.ErrStorageInvalid), errors.Is(e, plugin.ErrMigration):
		writeError(w, InvalidFields)
	case errors.Is(e, plugin.ErrInvoke), errors.Is(e, plugin.ErrSecretKey):
		writeError(w, Unavailable)
	default:
		domainError(w, e)
	}
}

// pluginRoutes serves the owner plugin-management API and every route a loaded
// plugin registered: /plugins/{slug}{path} publicly, read-only; and
// /api/v1/plugins/{slug}{path} behind the owner session. Install, enable,
// disable, and remove additionally recheck fresh reauthentication inside the
// mutation's transaction, like other sensitive owner operations.
func (d *Domain) pluginRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if p == "/api/v1/extension-contributions" || strings.HasPrefix(p, "/api/v1/plugin-usage/") {
		d.extensionRead(w, r)
		return true
	}
	if strings.HasPrefix(p, "/api/v1/plugin-configuration/") {
		d.pluginConfiguration(w, r, strings.TrimPrefix(p, "/api/v1/plugin-configuration/"))
		return true
	}
	if strings.HasPrefix(p, "/plugins/") {
		d.servePluginRoute(w, r, strings.TrimPrefix(p, "/plugins"), true)
		return true
	}
	if p != "/api/v1/plugins" && !strings.HasPrefix(p, "/api/v1/plugins/") {
		return false
	}
	if _, e := d.Auth.OwnerRequest(r); e != nil {
		domainError(w, e)
		return true
	}
	if p == "/api/v1/plugins" {
		if r.Method != "GET" {
			writeError(w, MethodNotAllowed)
			return true
		}
		items, e := d.Plugins.List(r.Context())
		if e != nil {
			pluginError(w, e)
		} else {
			writeJSON(w, 200, map[string]any{"items": items, "host_api_version": plugin.HostAPIVersion})
		}
		return true
	}
	parts := strings.Split(strings.TrimPrefix(p, "/api/v1/plugins/"), "/")
	if len(parts) == 2 && r.Method == "POST" && (parts[1] == "install" || parts[1] == "enable" || parts[1] == "disable" || parts[1] == "remove") {
		d.managePlugin(w, r, parts[0], parts[1])
		return true
	}
	d.servePluginRoute(w, r, strings.TrimPrefix(p, "/api/v1/plugins"), false)
	return true
}

func (d *Domain) managePlugin(w http.ResponseWriter, r *http.Request, slug, action string) {
	var in struct {
		ExpectedVersion int64  `json:"expected_version"`
		Checksum        string `json:"candidate_checksum,omitempty"`
	}
	if !DecodeJSON(w, r, &in, 4096) {
		return
	}
	check, e := d.Auth.FreshOwnerCheck(r)
	if e != nil {
		domainError(w, e)
		return
	}
	switch action {
	case "install":
		out, e := d.Plugins.ApproveCandidate(r.Context(), slug, in.Checksum, in.ExpectedVersion, check)
		if e != nil {
			pluginError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
	case "enable", "disable":
		out, e := d.Plugins.SetEnabled(r.Context(), slug, action == "enable", in.ExpectedVersion, check)
		if e != nil {
			pluginError(w, e)
		} else {
			writeJSON(w, 200, out)
		}
	case "remove":
		if e := d.Plugins.Remove(r.Context(), slug, in.ExpectedVersion, check); e != nil {
			pluginError(w, e)
		} else {
			writeJSON(w, 200, map[string]bool{"removed": true})
		}
	}
}

// servePluginRoute dispatches one registered plugin route. The namespace is
// claimed whole: an unregistered path under it is a structured 404, never a
// fall-through to page rendering.
func (d *Domain) servePluginRoute(w http.ResponseWriter, r *http.Request, suffix string, public bool) {
	segments := strings.SplitN(strings.TrimPrefix(suffix, "/"), "/", 2)
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		writeError(w, NotFound)
		return
	}
	slug, path := segments[0], "/"+segments[1]
	var matched *extension.RouteRegistration
	pathKnown := false
	for _, route := range d.extensions().Routes() {
		if route.Extension != slug || route.Public != public || route.Path != path {
			continue
		}
		pathKnown = true
		if route.Method == r.Method || (route.Method == "GET" && r.Method == "HEAD") {
			matched = &route
			break
		}
	}
	if matched == nil {
		if pathKnown {
			writeError(w, MethodNotAllowed)
		} else {
			writeError(w, NotFound)
		}
		return
	}
	matched.Handler.ServeHTTP(w, r)
}

func (d *Domain) pluginConfiguration(w http.ResponseWriter, r *http.Request, suffix string) {
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return
	}
	parts := strings.Split(suffix, "/")
	ctx := r.Context()
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		out, err := d.Plugins.Configuration(ctx, parts[0])
		if err != nil {
			pluginError(w, err)
		} else {
			writeJSON(w, http.StatusOK, out)
		}
	case len(parts) == 1 && r.Method == http.MethodPut:
		var input plugin.SettingsInput
		if !DecodeJSON(w, r, &input, 160<<10) {
			return
		}
		if err := d.Plugins.SaveSettings(ctx, parts[0], input); err != nil {
			pluginError(w, err)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	case len(parts) == 3 && parts[1] == "secrets" && r.Method == http.MethodPut:
		var input struct {
			Version int64           `json:"expected_version"`
			Value   json.RawMessage `json:"value"`
		}
		if !DecodeJSON(w, r, &input, 32<<10) {
			return
		}
		var value *string
		if json.Unmarshal(input.Value, &value) != nil {
			writeError(w, InvalidFields)
			return
		}
		check, err := d.Auth.FreshOwnerCheck(r)
		if err == nil {
			err = d.Plugins.ChangeSecret(ctx, parts[0], parts[2], value, input.Version, check)
		}
		if err != nil {
			pluginError(w, err)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		writeError(w, MethodNotAllowed)
	}
}

func (d *Domain) extensionRead(w http.ResponseWriter, r *http.Request) {
	if _, err := d.Auth.OwnerRequest(r); err != nil {
		domainError(w, err)
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, MethodNotAllowed)
		return
	}
	if r.URL.Path == "/api/v1/extension-contributions" {
		writeJSON(w, http.StatusOK, map[string]any{"items": d.extensions().Contributions("")})
		return
	}
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/plugin-usage/")
	cursor := r.URL.Query().Get("cursor")
	if d.Content == nil || slug == "" || strings.Contains(slug, "/") || len(slug) > 64 || len(cursor) > 32 {
		writeError(w, InvalidFields)
		return
	}
	page, err := d.Content.PageUses(r.Context(), slug, cursor)
	if err != nil {
		domainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}
