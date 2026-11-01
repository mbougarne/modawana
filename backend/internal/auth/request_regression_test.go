package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestDecodeClassifiesRequestErrorsWithoutAuthenticationFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		code                    string
	}{
		{"valid", `{"code":"test"}`, "application/json; charset=utf-8", 200, ""},
		{"malformed", `{"code":`, "application/json", 400, "bad_request"},
		{"unknown", `{"unexpected":true}`, "application/json", 400, "bad_request"},
		{"null", `null`, "application/json", 400, "bad_request"},
		{"array", `[]`, "application/json", 400, "bad_request"},
		{"trailing", `{} {}`, "application/json", 400, "bad_request"},
		{"empty", ``, "application/json", 400, "bad_request"},
		{"large", strings.Repeat(" ", 65537), "application/json", 413, "too_large"},
		{"type", `{}`, "text/plain", 415, "unsupported_media_type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/v1/session", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			var in struct {
				Code string `json:"code"`
			}
			ok := decode(w, r, &in)
			if w.Code != tc.status || ok != (tc.status == 200) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if !ok {
				var out struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || out.Error.Code != tc.code {
					t.Fatal(out, e)
				}
			}
		})
	}
}

func TestRevocationsRollbackWhenAuditWriteFails(t *testing.T) {
	for _, action := range []string{"logout", "token"} {
		t.Run(action, func(t *testing.T) {
			s := fixture(t)
			raw, session := loggedIn(t, s)
			token, e := s.CreatePublisherToken(context.Background(), session, TokenInput{Label: "test", Scopes: []string{"draft"}, AllowArticles: true, Days: 1})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec(`CREATE TRIGGER reject_security_audit BEFORE INSERT ON security_events BEGIN SELECT RAISE(ABORT,'injected audit failure'); END`); e != nil {
				t.Fatal(e)
			}
			path, body := "/api/v1/session", "{}"
			if action == "token" {
				path, body = "/api/v1/security/publisher-tokens", `{"id":"`+token.ID+`"}`
			}
			r := httptest.NewRequest("DELETE", path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", s.Config.BaseURL)
			r.Header.Set("X-CSRF-Token", csrf(raw))
			r.AddCookie(&http.Cookie{Name: ownerCookie, Value: raw})
			w := httptest.NewRecorder()
			if !s.Handle(w, r) || w.Code != 500 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			var live int
			query, id := "SELECT count(*) FROM sessions WHERE revoked_at IS NULL AND id=?", session.ID
			if action == "token" {
				query, id = "SELECT count(*) FROM api_tokens WHERE revoked_at IS NULL AND id=?", token.ID
			}
			if e = s.DB.QueryRow(query, id).Scan(&live); e != nil || live != 1 {
				t.Fatal("partial revocation", live, e)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed revocation cleared cookie")
			}
		})
	}
}

func TestThrottlePressureRetainsAccountAndCallerWithoutGlobalLockout(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if e := s.limit(ctx, "failed_account", 25); e != nil {
		t.Fatal(e)
	}
	if e := s.limit(ctx, "owner", 2); e != nil {
		t.Fatal(e)
	}
	_, e := s.DB.Exec(`WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<10001) INSERT INTO auth_throttles(bucket,attempts,expires_at) SELECT 'pressure-'||n,1,? FROM numbers`, db.UTC(s.Now().Add(15*time.Minute)))
	if e != nil {
		t.Fatal(e)
	}
	if e = s.limit(ctx, "owner", 2); e != nil {
		t.Fatal("global lockout", e)
	}
	var count, account int
	if e = s.DB.QueryRow("SELECT count(*) FROM auth_throttles").Scan(&count); e != nil || count != 10000 {
		t.Fatal(count, e)
	}
	if e = s.DB.QueryRow("SELECT attempts FROM auth_throttles WHERE bucket=?", digest("failed_account")).Scan(&account); e != nil || account != 1 {
		t.Fatal(account, e)
	}
	if e = s.limit(ctx, "owner", 2); e != ErrRate {
		t.Fatal("caller limit lost", e)
	}
}
