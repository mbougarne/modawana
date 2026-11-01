package httpserver

import (
	"net/http"
	"strings"
)

func maintenanceAllowed(r *http.Request) bool {
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/auth/") || p == "/api/v1/session" || strings.HasPrefix(p, "/api/v1/session/") {
		return true
	}
	if strings.HasPrefix(p, "/api/v1/security/") {
		return p != "/api/v1/security/oauth" && (p != "/api/v1/security/publisher-tokens" || r.Method == "GET")
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	if p == "/healthz" || strings.HasPrefix(p, "/assets/") {
		return true
	}
	for _, locale := range []string{"en", "ar", "fr"} {
		if p == "/"+locale+"/login" || p == "/"+locale+"/setup" || p == "/"+locale+"/admin/security" {
			return true
		}
	}
	return false
}
