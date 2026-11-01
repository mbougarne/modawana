package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/mbougarne/modawana/backend/internal/db"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func oauthHTTP(s *Service, method, path, body, owner string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, s.Config.BaseURL+path, strings.NewReader(body))
	if strings.HasPrefix(path, "/oauth/token") {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r.Header.Set("Content-Type", "application/json")
	}
	if owner != "" {
		r.AddCookie(&http.Cookie{Name: ownerCookie, Value: owner})
		r.Header.Set("Origin", s.Config.BaseURL)
		r.Header.Set("X-CSRF-Token", csrf(owner))
	}
	w := httptest.NewRecorder()
	s.Handle(w, r)
	return w
}
func oauthJSON(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d want %d", w.Code, status)
	}
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func oauthSetup(t *testing.T, s *Service) (string, string, PublisherToken, url.Values) {
	t.Helper()
	raw, session := loggedIn(t, s)
	token, e := s.CreatePublisherToken(context.Background(), session, TokenInput{Label: "Alex", Scopes: []string{"draft"}, AllowArticles: true, Days: 90})
	if e != nil {
		t.Fatal(e)
	}
	client := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/register", `{"redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"none"}`, ""), 201)["client_id"].(string)
	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"client_id": {client}, "response_type": {"code"}, "redirect_uri": {"https://chatgpt.com/connector_platform_oauth_redirect"}, "resource": {s.Config.BaseURL + MCPResourcePath}, "state": {"opaque-client-state"}, "scope": {oauthScope}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "ui_locales": {"en-US"}}
	w := oauthHTTP(s, "GET", "/oauth/authorize?"+q.Encode(), "", "")
	if w.Code != 302 {
		t.Fatalf("authorize: %d", w.Code)
	}
	location, _ := url.Parse(w.Header().Get("Location"))
	id := location.Query().Get("id")
	body, _ := json.Marshal(map[string]any{"id": id, "token_id": token.ID, "approve": true})
	oauthJSON(t, oauthHTTP(s, "POST", "/api/v1/security/oauth", string(body), ""), 403)
	result := oauthJSON(t, oauthHTTP(s, "POST", "/api/v1/security/oauth", string(body), raw), 200)
	callback, _ := url.Parse(result["redirect"].(string))
	if callback.Query().Get("state") != "opaque-client-state" || callback.Query().Get("iss") != s.Config.BaseURL {
		t.Fatal("callback binding lost")
	}
	return raw, client, token, url.Values{"grant_type": {"authorization_code"}, "client_id": {client}, "code": {callback.Query().Get("code")}, "code_verifier": {verifier}, "redirect_uri": {q.Get("redirect_uri")}, "resource": {q.Get("resource")}}
}
func TestOAuthCodePKCEAudienceScopeAndRefresh(t *testing.T) {
	s := fixture(t)
	_, client, parent, form := oauthSetup(t, s)
	good := form.Get("code_verifier")
	form.Set("code_verifier", strings.Repeat("b", 43))
	oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 400)
	form.Set("code_verifier", good)
	form.Set("resource", "https://elsewhere.example")
	oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 400)
	form.Set("resource", s.Config.BaseURL+MCPResourcePath)
	result := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 200)
	access := result["access_token"].(string)
	req := httptest.NewRequest("POST", s.Config.BaseURL+MCPResourcePath, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	token, e := s.PublisherRequest(req)
	if e != nil || token.ID != parent.ID || token.OAuthHash == "" {
		t.Fatal("access token invalid", e)
	}
	direct := req.Clone(req.Context())
	direct.URL.Path = "/api/v1/publisher/content/example/en"
	if _, e = s.PublisherRequest(direct); e == nil {
		t.Fatal("audience bypass")
	}
	if _, e = s.PublisherRequest(MCPInternalRequest(direct)); e != nil {
		t.Fatal("adapter failed", e)
	}
	tx, _ := s.DB.BeginTx(context.Background(), nil)
	if e = s.PublisherCheck(context.Background(), tx, token, "draft", "example", "", "article", false); e != nil {
		t.Fatal(e)
	}
	if e = s.PublisherCheck(context.Background(), tx, token, "publish", "example", "", "article", false); e == nil {
		t.Fatal("scope escalation")
	}
	if e = s.PublisherCheck(context.Background(), tx, token, "draft", "example", "other", "lesson", false); e == nil {
		t.Fatal("collection escalation")
	}
	tx.Rollback()
	refresh := url.Values{"grant_type": {"refresh_token"}, "client_id": {client}, "resource": {s.Config.BaseURL + MCPResourcePath}, "refresh_token": {result["refresh_token"].(string)}}
	rotated := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", refresh.Encode(), ""), 200)
	if _, e = s.PublisherRequest(req); e == nil {
		t.Fatal("old access survived rotation")
	}
	req.Header.Set("Authorization", "Bearer "+rotated["access_token"].(string))
	if _, e = s.PublisherRequest(req); e != nil {
		t.Fatal(e)
	}
	oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", refresh.Encode(), ""), 400)
	if _, e = s.PublisherRequest(req); e == nil {
		t.Fatal("refresh reuse did not revoke grant")
	}
}
func TestOAuthRevocationExpiryAndConsent(t *testing.T) {
	for _, mode := range []string{"parent", "owner", "expiry", "code_replay"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			_, _, parent, form := oauthSetup(t, s)
			result := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 200)
			req := httptest.NewRequest("POST", s.Config.BaseURL+MCPResourcePath, nil)
			req.Header.Set("Authorization", "Bearer "+result["access_token"].(string))
			token, e := s.PublisherRequest(req)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "parent":
				s.DB.Exec("UPDATE api_tokens SET revoked_at=? WHERE id=?", s.now(), parent.ID)
			case "owner":
				s.DB.Exec("UPDATE owner SET auth_version=auth_version+1")
			case "expiry":
				now := s.Now()
				s.Now = func() time.Time { return now.Add(16 * time.Minute) }
			case "code_replay":
				oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 400)
			}
			if _, e = s.PublisherRequest(req); e == nil {
				t.Fatal("revoked access accepted")
			}
			tx, _ := s.DB.BeginTx(context.Background(), nil)
			defer tx.Rollback()
			if e = s.PublisherCheck(context.Background(), tx, token, "draft", "example", "", "article", false); e == nil {
				t.Fatal("transaction did not recheck authorization")
			}
		})
	}
}
func TestOAuthRegistrationAndDiscovery(t *testing.T) {
	s := fixture(t)
	metadata := oauthJSON(t, oauthHTTP(s, "GET", "/.well-known/oauth-authorization-server", "", ""), 200)
	if metadata["issuer"] != s.Config.BaseURL {
		t.Fatal("wrong issuer")
	}
	resource := oauthJSON(t, oauthHTTP(s, "GET", "/.well-known/oauth-protected-resource"+MCPResourcePath, "", ""), 200)
	if resource["resource"] != s.Config.BaseURL+MCPResourcePath {
		t.Fatal("wrong resource")
	}
	for _, redirect := range []string{"https://evil.example/callback", "https://chatgpt.com.evil.example/connector_platform_oauth_redirect", "https://chatgpt.com/connector_platform_oauth_redirect?next=evil", "http://localhost/callback"} {
		body, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirect}})
		oauthJSON(t, oauthHTTP(s, "POST", "/oauth/register", string(body), ""), 400)
	}
}

func TestOAuthConsentRequiresFreshSessionAndExplicitApproval(t *testing.T) {
	s := fixture(t)
	raw, _, parent, _ := oauthSetup(t, s)
	create := func() string {
		id, _ := secret()
		_, e := s.DB.Exec(`INSERT INTO oauth_requests(id,client_id,state,challenge,expires_at) SELECT ?,client_id,state,challenge,? FROM oauth_requests LIMIT 1`, id, db.UTC(s.Now().Add(time.Hour)))
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	id := create()
	body, _ := json.Marshal(map[string]any{"id": id, "token_id": parent.ID, "approve": true})
	req := httptest.NewRequest("POST", s.Config.BaseURL+"/api/v1/security/oauth", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: ownerCookie, Value: raw})
	w := httptest.NewRecorder()
	s.Handle(w, req)
	oauthJSON(t, w, 403)
	denied, _ := json.Marshal(map[string]any{"id": id, "approve": false})
	response := oauthJSON(t, oauthHTTP(s, "POST", "/api/v1/security/oauth", string(denied), raw), 200)
	callback, _ := url.Parse(response["redirect"].(string))
	if callback.Query().Get("error") != "access_denied" || callback.Query().Get("code") != "" || callback.Query().Get("iss") != s.Config.BaseURL {
		t.Fatal("denial callback invalid")
	}
	oauthJSON(t, oauthHTTP(s, "POST", "/api/v1/security/oauth", string(body), raw), 401)
	id = create()
	body, _ = json.Marshal(map[string]any{"id": id, "token_id": parent.ID, "approve": true})
	now := s.Now()
	s.Now = func() time.Time { return now.Add(31 * time.Minute) }
	response = oauthJSON(t, oauthHTTP(s, "POST", "/api/v1/security/oauth", string(body), raw), 403)
	_ = response
}
func TestOAuthDisconnectDoesNotRevokePublisherToken(t *testing.T) {
	s := fixture(t)
	raw, _, parent, form := oauthSetup(t, s)
	result := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 200)
	var id string
	if e := s.DB.QueryRow("SELECT id FROM oauth_grants").Scan(&id); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"id": id})
	oauthJSON(t, oauthHTTP(s, "DELETE", "/api/v1/security/oauth", string(body), raw), 200)
	r := httptest.NewRequest("POST", s.Config.BaseURL+MCPResourcePath, nil)
	r.Header.Set("Authorization", "Bearer "+result["access_token"].(string))
	if _, e := s.PublisherRequest(r); e == nil {
		t.Fatal("disconnected token accepted")
	}
	r.Header.Set("Authorization", "Bearer "+parent.Secret)
	if _, e := s.PublisherRequest(r); e != nil {
		t.Fatal("parent token revoked", e)
	}
}

func TestOAuthExchangeBindingsAndExpiry(t *testing.T) {
	for _, mode := range []string{"client", "callback", "expired_code", "duplicate_field", "missing_resource"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			_, _, _, form := oauthSetup(t, s)
			switch mode {
			case "client":
				form.Set("client_id", strings.Repeat("x", 43))
			case "callback":
				form.Set("redirect_uri", "https://chatgpt.com/connector/oauth/different")
			case "expired_code":
				now := s.Now()
				s.Now = func() time.Time { return now.Add(61 * time.Second) }
			case "duplicate_field":
				form.Add("resource", s.Config.BaseURL+MCPResourcePath)
			case "missing_resource":
				form.Del("resource")
			}
			oauthJSON(t, oauthHTTP(s, "POST", "/oauth/token", form.Encode(), ""), 400)
			var count int
			if e := s.DB.QueryRow("SELECT count(*) FROM oauth_grants").Scan(&count); e != nil || count != 0 {
				t.Fatal("invalid exchange created a grant", e)
			}
		})
	}
}

func TestOAuthAuthorizeLocaleHint(t *testing.T) {
	s := fixture(t)
	client := oauthJSON(t, oauthHTTP(s, "POST", "/oauth/register", `{"redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"none"}`, ""), 201)["client_id"].(string)
	sum := sha256.Sum256([]byte(strings.Repeat("a", 43)))
	for _, tc := range []struct {
		name    string
		hints   []string
		unknown bool
		status  int
	}{
		{"absent", nil, false, 302},
		{"chatgpt", []string{"en-US"}, false, 302},
		{"preferences", []string{"ar-MA en-US"}, false, 302},
		{"unsupported locale", []string{"ja-JP"}, false, 302},
		{"duplicate", []string{"en-US", "ar"}, false, 400},
		{"oversized", []string{strings.Repeat("a", 257)}, false, 400},
		{"unknown parameter", []string{"en-US"}, true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{"client_id": {client}, "response_type": {"code"}, "redirect_uri": {"https://chatgpt.com/connector_platform_oauth_redirect"}, "resource": {s.Config.BaseURL + MCPResourcePath}, "state": {"test-state"}, "scope": {oauthScope}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
			if tc.hints != nil {
				q["ui_locales"] = tc.hints
			}
			if tc.unknown {
				q.Set("unsupported", "value")
			}
			w := oauthHTTP(s, "GET", "/oauth/authorize?"+q.Encode(), "", "")
			if w.Code != tc.status {
				t.Fatalf("status %d want %d", w.Code, tc.status)
			}
			if w.Code == 302 {
				location, err := url.Parse(w.Header().Get("Location"))
				if err != nil || location.IsAbs() || location.Path != "/en/admin/oauth" || location.Query().Get("id") == "" || len(location.Query()) != 1 {
					t.Fatal("unexpected consent destination")
				}
			}
		})
	}
}
