package httpserver

import (
	"context"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/learning"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestLearningHTTPBoundary(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	database, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(database, cfg)
	d := &Domain{Auth: a, Learning: learning.NewIdentity(database, 180, 100)}
	h := New(Options{Config: cfg, Domain: d, Auth: a})
	request := func(method, path, raw, csrf, origin, custom string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("X-Blog-Request", custom)
		if raw != "" {
			r.AddCookie(&http.Cookie{Name: learningCookie, Value: raw})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/en/writing", "/api/v1/learning/progress"} {
		request("GET", path, "", "", "", "")
	}
	var count int
	database.QueryRow("SELECT count(*) FROM learning_identities").Scan(&count)
	if count != 0 {
		t.Fatal("visit created identity")
	}
	for _, bad := range [][2]string{{"", "1"}, {cfg.BaseURL, ""}, {"https://other.test", "1"}} {
		if w := request("POST", "/api/v1/learning/session", "", "", bad[0], bad[1]); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	w := request("POST", "/api/v1/learning/session", "", "", cfg.BaseURL, "1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" {
		t.Fatal(cookies)
	}
	raw := cookies[0].Value
	var state struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(w.Body.Bytes(), &state)
	other := request("POST", "/api/v1/learning/session", "", "", cfg.BaseURL, "1").Result().Cookies()[0].Value
	for _, r := range [][3]string{{raw, learning.LearningCSRF(other), cfg.BaseURL}, {raw, state.CSRF, "https://other.test"}} {
		if w = request("DELETE", "/api/v1/learning/progress", r[0], r[1], r[2], ""); w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if w = request("GET", "/api/v1/learning/progress?identity_id=other", raw, "", "", ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("GET", cfg.BaseURL+"/api/v1/session", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	if string(out.Body.Bytes()) == "owner" {
		t.Fatal("learning owner escalation")
	}
	if _, e = a.OwnerRequest(r); e == nil {
		t.Fatal("learning owner escalation")
	}
	if w = request("DELETE", "/api/v1/learning/progress", raw, state.CSRF, cfg.BaseURL, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = d.Learning.Current(context.Background(), other, false); e != nil {
		t.Fatal("reset crossed identities")
	}
	if _, e = d.Learning.Current(context.Background(), raw, false); e == nil {
		t.Fatal("reset token remains valid")
	}
}
