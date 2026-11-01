package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

func TestMediaHTTPBoundary(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	f, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	owner, _ := db.NewID()
	session, _ := db.NewID()
	d.Exec("INSERT INTO owner(id) VALUES(?)", owner)
	raw := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(raw))
	csrf := sha256.Sum256([]byte("csrf:" + raw))
	now := time.Now()
	_, e = d.Exec(`INSERT INTO sessions(id,owner_id,token_hash,kind,auth_method,auth_version,authenticated_at,expires_at,last_seen_at) VALUES(?,?,?,'owner','passkey',1,?,?,?)`, session, owner, hex.EncodeToString(sum[:]), db.UTC(now), db.UTC(now.Add(time.Hour)), db.UTC(now))
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(d, cfg)
	domain := &Domain{Auth: a, Media: media.New(d, f, &access.Live{DB: d})}
	h := New(Options{Config: cfg, Auth: a, Domain: domain})
	for _, tc := range []struct {
		cookie, csrf, origin bool
		want                 int
	}{{false, false, false, 401}, {true, false, true, 403}, {true, true, false, 403}, {true, true, true, 201}} {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		part, _ := mw.CreateFormFile("file", "captions.vtt")
		part.Write([]byte("WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n"))
		mw.Close()
		r := httptest.NewRequest("POST", "https://example.test/api/v1/media", &b)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		if tc.cookie {
			r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
		}
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrf[:]))
		}
		if tc.origin {
			r.Header.Set("Origin", cfg.BaseURL)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), root) || strings.Contains(w.Body.String(), "storage_key") {
			t.Fatal("path leaked")
		}
	}
	d.Exec("UPDATE sessions SET revoked_at=?", db.UTC(now))
	r := httptest.NewRequest("GET", "https://example.test/api/v1/media", nil)
	r.AddCookie(&http.Cookie{Name: "__Host-owner", Value: raw})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("revoked session read", w.Code)
	}
}
