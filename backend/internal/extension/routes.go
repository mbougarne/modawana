package extension

import (
	"net/http"
	"regexp"
)

var routeMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
var routePattern = regexp.MustCompile(`^/[a-z0-9_-]*(/[a-z0-9_-]+)*$`)

// Route declares a handler the host serves under the extension's namespace:
// /plugins/{slug}{Path} when Public, /api/v1/plugins/{slug}{Path} otherwise.
// Access control stays with the host: it authenticates, authorizes, rate
// limits, and logs every registered route before the handler runs.
type Route struct {
	Method  string
	Path    string
	Public  bool
	Handler http.Handler
}

// RouteRegistration is one registered route and the extension that owns it.
type RouteRegistration struct {
	Extension string
	Route
}

// Routes returns the registered routes in installation order.
func (h *Host) Routes() []RouteRegistration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]RouteRegistration(nil), h.routes...)
}

// SettingsSection declares a dashboard settings section. The schema uses the
// bounded validation vocabulary of site configuration and is rendered
// generically by the dashboard; values are validated by the host.
type SettingsSection struct {
	Key    string
	Schema map[string]any
}

// SettingsRegistration is one registered section and the extension that owns it.
type SettingsRegistration struct {
	Extension string
	SettingsSection
}

// SettingsSections returns the registered sections in installation order.
func (h *Host) SettingsSections() []SettingsRegistration {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]SettingsRegistration(nil), h.sections...)
}
