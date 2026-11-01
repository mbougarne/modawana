package httpserver

import (
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
	case errors.Is(e, plugin.ErrInvoke):
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
		ExpectedVersion int64 `json:"expected_version"`
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
		out, e := d.Plugins.Approve(r.Context(), slug, in.ExpectedVersion, check)
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
