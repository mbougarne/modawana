package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/social"
)

func TestSocialOwnerBoundaryAndOAuthReturn(t *testing.T) {
	cfg := config.Config{BaseURL: "https://blog.example"}
	account := auth.New(nil, cfg)
	server := New(Options{Config: cfg, Auth: account, Domain: &Domain{Auth: account, Social: &social.Service{}}})
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, socialAPI},
		{http.MethodGet, socialAPI + "/connection"},
		{http.MethodPost, socialAPI + "/connect"},
		{http.MethodPost, socialAPI + "/schedule"},
	} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(tc.method, cfg.BaseURL+tc.path, nil))
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: got %d", tc.method, tc.path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		cfg.BaseURL+social.CallbackPath+"?code=unsolicited-code&state=unexpected-state", nil))
	body := w.Body.String()
	requestID := w.Header().Get("X-Request-ID")
	if w.Code != http.StatusOK || w.Header().Get("Referrer-Policy") != "no-referrer" ||
		w.Header().Get("Cache-Control") != "no-store" || requestID == "" ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "'nonce-"+requestID+"'") ||
		!strings.Contains(body, "nonce=\""+requestID+"\"") || !strings.Contains(body, "/en/admin/social?oauth=failed") ||
		strings.Contains(body, "unsolicited-code") || strings.Contains(body, "unexpected-state") {
		t.Fatalf("unsafe callback response (%d)", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != socialCookie || !cookies[0].Secure || !cookies[0].HttpOnly ||
		cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].MaxAge != -1 {
		t.Fatal("the callback did not remove the short-lived browser proof")
	}
}
