package auth

import (
	"context"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	d, e := db.Open(context.Background(), dir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	s := New(d, config.Config{BaseURL: "https://blog.example", RPID: "blog.example", WebAuthnOrigins: []string{"https://blog.example"}})
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	return s
}
func bootstrap(t *testing.T, s *Service) (string, string) {
	t.Helper()
	ctx := context.Background()
	grant, e := s.Grant(ctx, false)
	if e != nil {
		t.Fatal(e)
	}
	flow, e := s.Start(ctx, "test")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := s.Redeem(ctx, flow, grant, "owner@example.test", "test")
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.flow(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	return raw, f.Owner
}
func TestBootstrapAndSessionBoundaries(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	grant, _ := s.Grant(ctx, false)
	flow, _ := s.Start(ctx, "test")
	restricted, e := s.Redeem(ctx, flow, grant, "owner@example.test", "req")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Session(ctx, restricted, false); e == nil {
		t.Fatal("enrollment became owner")
	}
	next, _ := s.Start(ctx, "test")
	if _, e = s.Redeem(ctx, next, grant, "owner@example.test", ""); e == nil {
		t.Fatal("grant replay")
	}
	if _, e = s.Grant(ctx, false); e == nil {
		t.Fatal("public second owner")
	}
	f, _ := s.flow(ctx, restricted)
	tx, _ := s.DB.BeginTx(ctx, nil)
	raw, e := s.complete(ctx, tx, f.Owner, "passkey", "credential", "", "req")
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	session, e := s.Session(ctx, raw, true)
	if e != nil {
		t.Fatal(e)
	}
	var stored string
	s.DB.QueryRow("SELECT token_hash FROM sessions WHERE id=?", session.ID).Scan(&stored)
	if stored == raw || stored != digest(raw) {
		t.Fatal("raw session stored")
	}
	old := s.Now()
	s.Now = func() time.Time { return old.Add(31 * time.Minute) }
	if _, e = s.Session(ctx, raw, true); e != ErrForbidden {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return old.Add(121 * time.Minute) }
	if _, e = s.Session(ctx, raw, false); e == nil {
		t.Fatal("idle expiry")
	}
	s.Now = func() time.Time { return old }
	s.DB.Exec("UPDATE sessions SET revoked_at=?", s.now())
	if _, e = s.Session(ctx, raw, false); e == nil {
		t.Fatal("revoked cookie")
	}
}
func TestCSRFOriginAndIncompleteOwnerAPI(t *testing.T) {
	s := fixture(t)
	flow, _ := bootstrap(t, s)
	for _, tc := range []struct {
		origin, csrf string
		want         int
	}{{"https://evil.test", csrf(flow), 403}, {s.Config.BaseURL, "", 403}, {s.Config.BaseURL, csrf(flow), 401}} {
		r := httptest.NewRequest("POST", "https://blog.example/api/v1/security/passkeys", strings.NewReader("{}"))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-CSRF-Token", tc.csrf)
		r.AddCookie(&http.Cookie{Name: ownerCookie, Value: flow})
		w := httptest.NewRecorder()
		s.Handle(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	setCookie(w, ownerCookie, "test", 3600)
	c := w.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || c.Path != "/" {
		t.Fatal(c)
	}
}
func TestExpiredGrantAndThrottle(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	grant, _ := s.Grant(ctx, false)
	now := s.Now()
	s.Now = func() time.Time { return now.Add(11 * time.Minute) }
	flow, _ := s.Start(ctx, "test")
	if _, e := s.Redeem(ctx, flow, grant, "owner@example.test", ""); e == nil {
		t.Fatal("expired grant")
	}
	for i := 0; i < 5; i++ {
		if e := s.limit(ctx, "test", 5); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.limit(ctx, "test", 5); e != ErrRate {
		t.Fatal(e)
	}
	s.Now = func() time.Time { return now.Add(27 * time.Minute) }
	if e := s.limit(ctx, "test", 5); e != nil {
		t.Fatal(e)
	}
}

func TestFailuresSurviveNewChallengesAndFreshnessRevocation(t *testing.T) {
	s := fixture(t)
	raw, session := loggedIn(t, s)
	ctx := context.Background()
	r := httptest.NewRequest("POST", "https://blog.example/api/v1/session", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	for i := 0; i < 5; i++ {
		s.failure(r, "fallback_failed")
		s.Start(ctx, "different-challenge")
	}
	if e := s.checkFailures(r); e != ErrRate {
		t.Fatal(e)
	}
	s.DB.Exec("UPDATE sessions SET revoked_at=? WHERE id=?", s.now(), session.ID)
	if _, e := s.regenerateCodes(ctx, session, ""); e == nil {
		t.Fatal("revoked session changed codes")
	}
	if _, e := s.Session(ctx, raw, false); e == nil {
		t.Fatal("revoked session accepted")
	}
}

func TestAuthHandlerNeverLeaksSecretsAndRejectsAdminWithoutOwner(t *testing.T) {
	s := fixture(t)
	flow, _ := bootstrap(t, s)
	for _, path := range []string{"/api/v1/security/passkeys", "/api/v1/security/factors", "/api/v1/security/sessions", "/api/v1/security/events"} {
		r := httptest.NewRequest("GET", "https://blog.example"+path, nil)
		r.AddCookie(&http.Cookie{Name: flowCookie, Value: flow})
		w := httptest.NewRecorder()
		s.Handle(w, r)
		if w.Code != 401 || strings.Contains(w.Body.String(), flow) {
			t.Fatal(path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "https://blog.example/api/v1/session", strings.NewReader(`{"password":"never-log-this","email":"private@example.test"}`))
	r.Header.Set("Origin", "https://evil.test")
	r.AddCookie(&http.Cookie{Name: flowCookie, Value: flow})
	w := httptest.NewRecorder()
	s.Handle(w, r)
	if strings.Contains(w.Body.String(), "never-log-this") || strings.Contains(w.Body.String(), "private@example.test") {
		t.Fatal("secret reflected")
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM security_events WHERE event LIKE '%never-log%' OR request_id LIKE '%private@%'").Scan(&count)
	if count != 0 {
		t.Fatal("secret audit")
	}
}

func TestLostOwnerCookieRejectsSecurityWriteBeforeSessionAuthentication(t *testing.T) {
	s := fixture(t)
	r := httptest.NewRequest("POST", "https://blog.example/api/v1/security/recovery-codes", strings.NewReader("{}"))
	r.Header.Set("Origin", s.Config.BaseURL)
	r.Header.Set("X-CSRF-Token", "stale-csrf")
	w := httptest.NewRecorder()
	s.Handle(w, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"code":"forbidden"`) {
		t.Fatal("missing cookie must fail CSRF", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.Handle(w, httptest.NewRequest("GET", "https://blog.example/api/v1/session", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"anonymous"`) {
		t.Fatal("session probe must distinguish loss from permission denial", w.Code, w.Body.String())
	}
}
