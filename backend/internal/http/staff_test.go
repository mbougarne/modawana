package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/auth"
	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestStaffRoutesKeepOwnerBoundaryAndRevisionAttribution(t *testing.T) {
	ctx := t.Context()
	c, owner := newThemeContent(t)
	cfg := config.Config{BaseURL: "https://example.test"}
	a := auth.New(c.DB, cfg)
	assets, err := LoadAssets(buildThemeFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Config: cfg, Assets: assets, Auth: a, Domain: &Domain{Auth: a, Content: c}})
	member, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DB.Exec(`INSERT INTO staff_members(id,owner_id,name,role,credential_id,credential)
		VALUES(?,?,'Editor','editor','test-key','{}')`, member, owner); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("K", 42) + "A"
	sum := sha256.Sum256([]byte(token))
	csrfHash := sha256.Sum256([]byte("csrf:" + token))
	if _, err := c.DB.Exec("INSERT INTO staff_sessions VALUES(?,?,?)", hex.EncodeToString(sum[:]), member, db.UTC(time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, write bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, cfg.BaseURL+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: "__Host-staff", Value: token})
		if write {
			r.Header.Set("Origin", cfg.BaseURL)
			r.Header.Set("X-CSRF-Token", hex.EncodeToString(csrfHash[:]))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		return w
	}
	request("GET", "/api/v1/content?locale=en&kind=article", "", false, 401)
	request("GET", "/api/v1/staff/management", "", false, 401)
	request("GET", "/api/v1/staff/content?locale=en&kind=lesson", "", false, 422)
	const draftURL = "/api/v1/staff/content/staff-post/en/draft"
	const draftBody = `{"expected_version":0,"kind":"article","slug":"staff-post","title":"A staff draft","markdown":"A draft body."}`
	request("PUT", draftURL, draftBody, false, 403)
	request("PUT", draftURL, draftBody, true, 200)
	draft, err := c.Draft(ctx, "staff-post", "en")
	if err != nil || draft.Revision.StaffAuthor != member || draft.Revision.Title != "A staff draft" {
		t.Fatal(draft, err)
	}
	const publicationURL = "/api/v1/staff/content/staff-post/en/publish"
	body := fmt.Sprintf(`{"expected_version":%d,"revision_id":%q}`, draft.Version, draft.Revision.ID)
	request("POST", publicationURL, body, true, 403)
	if _, err := c.DB.Exec("UPDATE staff_members SET role='owner' WHERE id=?", member); err != nil {
		t.Fatal(err)
	}
	request("POST", publicationURL, body, true, 200)
	var actor string
	if err := c.DB.QueryRow("SELECT staff_actor_id FROM outbox_events WHERE name='content.published'").Scan(&actor); err != nil || actor != member {
		t.Fatal("queued grant", actor, err)
	}
	if w := get(t, h, http.MethodGet, "/fr/staff"); w.Code != 200 ||
		!strings.Contains(w.Header().Get("X-Robots-Tag"), "noindex") || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatal("staff sign-in shell", w.Code, w.Body.String())
	}
	if _, err := c.DB.Exec("UPDATE staff_members SET revoked_at=? WHERE id=?", db.UTC(time.Now()), member); err != nil {
		t.Fatal(err)
	}
	request("GET", draftURL, "", false, 401)
}
