package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func TestConfigurationOwnerBoundary(t *testing.T) {
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	files, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer files.Close()
	owner, _ := db.NewID()
	sid, _ := db.NewID()
	token := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(token))
	csrf := sha256.Sum256([]byte("csrf:" + token))
	now := time.Now()
	if _, e = d.Exec("INSERT INTO owner(id,login_email) VALUES(?,?)", owner, "private@example.test"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, sid, owner, hex.EncodeToString(hash[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now)); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(d, cfg)
	service := &siteconfig.Service{DB: d, Files: files}
	handler := New(Options{Config: cfg, Auth: a, SiteConfig: service, Domain: &Domain{Auth: a, SiteConfig: service}})
	for _, tc := range []struct {
		method, path, body   string
		cookie, csrf, origin bool
		want                 int
	}{
		{"GET", "/api/v1/settings", "", false, false, false, 401},
		{"GET", "/api/v1/settings", "", true, false, false, 200},
		{"PUT", "/api/v1/labels", `{"expected_version":0,"snapshot":{}}`, false, false, false, 401},
		{"PUT", "/api/v1/labels", `{"expected_version":0,"snapshot":{}}`, true, false, true, 403},
		{"PUT", "/api/v1/labels", `{"expected_version":0,"snapshot":{}}`, true, true, false, 403},
		{"PUT", "/api/v1/labels", `{"expected_version":0,"snapshot":{}}`, true, true, true, 200},
		{"PUT", "/api/v1/labels", `{"expected_version":0,"snapshot":{}}`, true, true, true, 409},
		{"PUT", "/api/v1/settings", `{"expected_version":0,"snapshot":{"login_email":"bad@example.test"}}`, true, true, true, 422},
		{"GET", "/api/v1/public/site?locale=ar", "", false, false, false, 200},
	} {
		r := httptest.NewRequest(tc.method, "https://example.test"+tc.path, bytes.NewBufferString(tc.body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer publisher-cannot-change-settings")
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: token})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private@example.test") || strings.Contains(w.Body.String(), root) {
			t.Fatal("private configuration leaked")
		}
	}
}
