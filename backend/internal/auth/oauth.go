package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/db"
)

const MCPResourcePath = "/api/v1/publisher/mcp"
const oauthScope = "publisher offline_access"

var oauthCallback = regexp.MustCompile(`^https://chatgpt\.com/(connector_platform_oauth_redirect|connector/oauth/[A-Za-z0-9_-]{1,160})$`)
var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

type oauthContextKey struct{}

// MCPInternalRequest preserves authenticated audience binding only across the
// in-process MCP-to-publisher adapter. No HTTP header can create this context.
func MCPInternalRequest(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), oauthContextKey{}, true))
}
func (s *Service) oauthPublisher(r *http.Request, raw string) (PublisherToken, error) {
	var out PublisherToken
	internal, _ := r.Context().Value(oauthContextKey{}).(bool)
	if r.URL.Path != MCPResourcePath && !internal {
		return out, ErrAuth
	}
	e := s.DB.QueryRowContext(r.Context(), `SELECT t.id,t.owner_id,t.label,t.expires_at,t.allow_articles,g.access_hash FROM oauth_grants g JOIN api_tokens t ON t.id=g.token_id JOIN owner o ON o.id=t.owner_id WHERE g.access_hash=? AND g.revoked=0 AND g.access_expires>? AND g.expires_at>? AND g.auth_version=o.auth_version AND t.revoked_at IS NULL AND t.expires_at>?`, digest(raw), s.now(), s.now(), s.now()).Scan(&out.ID, &out.Owner, &out.Label, &out.Expires, &out.AllowArticles, &out.OAuthHash)
	if e != nil {
		return PublisherToken{}, ErrAuth
	}
	return out, nil
}
func (s *Service) oauthCheck(ctx context.Context, tx *sql.Tx, t PublisherToken) error {
	if t.OAuthHash == "" {
		return nil
	}
	var count int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM oauth_grants g JOIN owner o ON o.id=? WHERE g.access_hash=? AND g.token_id=? AND g.revoked=0 AND g.access_expires>? AND g.expires_at>? AND g.auth_version=o.auth_version`, t.Owner, t.OAuthHash, t.ID, s.now(), s.now()).Scan(&count)
	if e != nil {
		return e
	}
	if count != 1 {
		return ErrForbidden
	}
	return nil
}
func oauthError(w http.ResponseWriter, status int, code string) {
	jsonOut(w, status, map[string]string{"error": code})
}
func (s *Service) oauthLimit(r *http.Request) error {
	// Global limits bound persistent anonymous records as well as token attempts.
	return s.limit(r.Context(), "oauth:"+r.URL.Path, 120)
}
func singleOAuth(v url.Values) bool {
	for _, values := range v {
		if len(values) != 1 || len(values[0]) > 2048 {
			return false
		}
	}
	return true
}
func (s *Service) oauthRoutes(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if p != "/.well-known/oauth-authorization-server" && p != "/.well-known/oauth-protected-resource" && p != "/.well-known/oauth-protected-resource"+MCPResourcePath && p != "/oauth/register" && p != "/oauth/authorize" && p != "/oauth/token" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	base := s.Config.BaseURL
	if strings.HasPrefix(p, "/.well-known/") {
		if r.Method != "GET" {
			oauthError(w, 405, "invalid_request")
			return true
		}
		if p == "/.well-known/oauth-authorization-server" {
			jsonOut(w, 200, map[string]any{"issuer": base, "authorization_endpoint": base + "/oauth/authorize", "token_endpoint": base + "/oauth/token", "registration_endpoint": base + "/oauth/register", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"publisher", "offline_access"}, "authorization_response_iss_parameter_supported": true})
		} else {
			jsonOut(w, 200, map[string]any{"resource": base + MCPResourcePath, "authorization_servers": []string{base}, "scopes_supported": []string{"publisher", "offline_access"}, "bearer_methods_supported": []string{"header"}})
		}
		return true
	}
	if e := s.oauthLimit(r); e != nil {
		oauthError(w, 429, "temporarily_unavailable")
		return true
	}
	if p == "/oauth/register" {
		s.oauthRegister(w, r)
		return true
	}
	if p == "/oauth/authorize" {
		s.oauthAuthorize(w, r)
		return true
	}
	s.oauthToken(w, r)
	return true
}
func (s *Service) oauthRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.RawQuery != "" {
		oauthError(w, 405, "invalid_request")
		return
	}
	var in struct {
		Redirects []string `json:"redirect_uris"`
		Method    string   `json:"token_endpoint_auth_method"`
		Name      string   `json:"client_name"`
		Grants    []string `json:"grant_types"`
		Responses []string `json:"response_types"`
		Scope     string   `json:"scope"`
		ClientURI string   `json:"client_uri"`
		LogoURI   string   `json:"logo_uri"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Redirects) != 1 || !oauthCallback.MatchString(in.Redirects[0]) || (in.Method != "" && in.Method != "none") || len(in.Name) > 200 {
		oauthError(w, 400, "invalid_client_metadata")
		return
	}
	for _, v := range in.Grants {
		if v != "authorization_code" && v != "refresh_token" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	for _, v := range in.Responses {
		if v != "code" {
			oauthError(w, 400, "invalid_client_metadata")
			return
		}
	}
	if in.Scope != "" && !validOAuthScope(in.Scope) {
		oauthError(w, 400, "invalid_scope")
		return
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(r.Context(), "DELETE FROM oauth_grants WHERE expires_at<=?", s.now()); e != nil {
		fail(w, e)
		return
	}
	if _, e = tx.ExecContext(r.Context(), "DELETE FROM oauth_clients WHERE expires_at<=?", s.now()); e != nil {
		fail(w, e)
		return
	}
	var count int
	if e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM oauth_clients").Scan(&count); e != nil {
		fail(w, e)
		return
	}
	if count >= 200 {
		oauthError(w, 429, "temporarily_unavailable")
		return
	}
	id, e := secret()
	if e != nil {
		fail(w, e)
		return
	}
	_, e = tx.ExecContext(r.Context(), "INSERT INTO oauth_clients VALUES(?,?,?)", id, in.Redirects[0], db.UTC(s.Now().Add(365*24*time.Hour)))
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, e)
		return
	}
	jsonOut(w, 201, map[string]any{"client_id": id, "client_name": "Personal Blog connection", "redirect_uris": in.Redirects, "token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "scope": oauthScope})
}
func validOAuthScope(scope string) bool {
	seen := map[string]bool{}
	for _, v := range strings.Fields(scope) {
		if (v != "publisher" && v != "offline_access") || seen[v] {
			return false
		}
		seen[v] = true
	}
	return seen["publisher"] && seen["offline_access"]
}
func (s *Service) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	q, e := url.ParseQuery(r.URL.RawQuery)
	if r.Method != "GET" || e != nil || len(r.URL.RawQuery) > 8192 || !singleOAuth(q) {
		oauthError(w, 400, "invalid_request")
		return
	}
	var redirect string
	e = s.DB.QueryRowContext(r.Context(), "SELECT redirect_uri FROM oauth_clients WHERE id=? AND expires_at>?", q.Get("client_id"), s.now()).Scan(&redirect)
	if e != nil || redirect != q.Get("redirect_uri") {
		oauthError(w, 400, "invalid_request")
		return
	}
	if q.Get("response_type") != "code" || q.Get("resource") != s.Config.BaseURL+MCPResourcePath || !validOAuthScope(q.Get("scope")) || q.Get("code_challenge_method") != "S256" || !validSecret(q.Get("code_challenge")) || q.Get("state") == "" || len(q.Get("state")) > 512 {
		oauthError(w, 400, "invalid_request")
		return
	}
	// ui_locales is an optional display hint sent by ChatGPT. Accept but ignore it;
	// it never changes authorization or the fixed consent destination.
	if len(q.Get("ui_locales")) > 256 {
		oauthError(w, 400, "invalid_request")
		return
	}
	// Reject unsupported parameters instead of silently accepting alternate flows.
	for k := range q {
		if k != "client_id" && k != "redirect_uri" && k != "response_type" && k != "resource" && k != "scope" && k != "code_challenge_method" && k != "code_challenge" && k != "state" && k != "ui_locales" {
			oauthError(w, 400, "invalid_request")
			return
		}
	}
	id, e := secret()
	if e != nil {
		fail(w, e)
		return
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(r.Context(), "DELETE FROM oauth_requests WHERE expires_at<=?", s.now())
	if e != nil {
		fail(w, e)
		return
	}
	var n int
	if e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM oauth_requests").Scan(&n); e != nil {
		fail(w, e)
		return
	}
	if n >= 200 {
		oauthError(w, 429, "temporarily_unavailable")
		return
	}
	_, e = tx.ExecContext(r.Context(), "INSERT INTO oauth_requests(id,client_id,state,challenge,expires_at) VALUES(?,?,?,?,?)", id, q.Get("client_id"), q.Get("state"), q.Get("code_challenge"), db.UTC(s.Now().Add(10*time.Minute)))
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, e)
		return
	}
	http.Redirect(w, r, "/en/admin/oauth?id="+id, http.StatusFound)
}
func (s *Service) oauthSecurity(w http.ResponseWriter, r *http.Request, session Session) bool {
	if r.URL.Path != "/api/v1/security/oauth" {
		return false
	}
	if r.Method == "GET" && r.URL.Query().Get("id") == "" {
		rows, e := s.DB.QueryContext(r.Context(), `SELECT g.id,t.label,g.expires_at FROM oauth_grants g JOIN api_tokens t ON t.id=g.token_id JOIN owner o ON o.id=t.owner_id WHERE t.owner_id=? AND g.revoked=0 AND g.expires_at>? AND g.auth_version=o.auth_version AND t.revoked_at IS NULL AND t.expires_at>? ORDER BY g.id LIMIT 50`, session.Owner, s.now(), s.now())
		if e != nil {
			fail(w, e)
			return true
		}
		defer rows.Close()
		out := []map[string]string{}
		for rows.Next() {
			var id, label, expiry string
			if e = rows.Scan(&id, &label, &expiry); e != nil {
				fail(w, e)
				return true
			}
			out = append(out, map[string]string{"id": id, "label": label, "expires_at": expiry})
		}
		if e = rows.Err(); e != nil {
			fail(w, e)
		} else {
			jsonOut(w, 200, out)
		}
		return true
	}
	if r.Method == "DELETE" {
		var in struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &in) {
			return true
		}
		tx, e := s.DB.BeginTx(r.Context(), nil)
		if e != nil {
			fail(w, e)
			return true
		}
		defer tx.Rollback()
		if e = s.freshInTx(r.Context(), tx, session); e == nil {
			_, e = tx.ExecContext(r.Context(), `UPDATE oauth_grants SET revoked=1 WHERE id=? AND token_id IN(SELECT id FROM api_tokens WHERE owner_id=?)`, in.ID, session.Owner)
		}
		if e == nil {
			e = s.audit(r.Context(), tx, "oauth_grant_revoked", "")
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			fail(w, e)
		} else {
			jsonOut(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	if r.Method == "GET" {
		var redirect string
		e := s.DB.QueryRowContext(r.Context(), `SELECT c.redirect_uri FROM oauth_requests a JOIN oauth_clients c ON c.id=a.client_id WHERE a.id=? AND a.code_hash IS NULL AND a.consumed=0 AND a.expires_at>? AND c.expires_at>?`, r.URL.Query().Get("id"), s.now(), s.now()).Scan(&redirect)
		if e != nil {
			fail(w, ErrAuth)
		} else {
			jsonOut(w, 200, map[string]string{"redirect_uri": redirect, "scope": oauthScope})
		}
		return true
	}
	if r.Method != "POST" {
		fail(w, ErrForbidden)
		return true
	}
	var in struct {
		ID      string `json:"id"`
		TokenID string `json:"token_id"`
		Approve bool   `json:"approve"`
	}
	if !decode(w, r, &in) {
		return true
	}
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, e)
		return true
	}
	defer tx.Rollback()
	if e = s.freshInTx(r.Context(), tx, session); e != nil {
		fail(w, e)
		return true
	}
	var redirect, state string
	e = tx.QueryRowContext(r.Context(), `SELECT c.redirect_uri,a.state FROM oauth_requests a JOIN oauth_clients c ON c.id=a.client_id WHERE a.id=? AND a.code_hash IS NULL AND a.consumed=0 AND a.expires_at>? AND c.expires_at>?`, in.ID, s.now(), s.now()).Scan(&redirect, &state)
	if e != nil {
		fail(w, ErrAuth)
		return true
	}
	u, _ := url.Parse(redirect)
	q := u.Query()
	q.Set("state", state)
	q.Set("iss", s.Config.BaseURL)
	if in.Approve {
		var version int64
		e = tx.QueryRowContext(r.Context(), `SELECT o.auth_version FROM api_tokens t JOIN owner o ON o.id=t.owner_id WHERE t.id=? AND t.owner_id=? AND t.revoked_at IS NULL AND t.expires_at>?`, in.TokenID, session.Owner, s.now()).Scan(&version)
		if e != nil {
			fail(w, ErrForbidden)
			return true
		}
		var n int
		if e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM oauth_grants WHERE revoked=0 AND expires_at>?", s.now()).Scan(&n); e != nil {
			fail(w, e)
			return true
		}
		if n >= 50 {
			fail(w, ErrRate)
			return true
		}
		code, err := secret()
		if err != nil {
			fail(w, err)
			return true
		}
		_, e = tx.ExecContext(r.Context(), "UPDATE oauth_requests SET code_hash=?,token_id=?,auth_version=?,expires_at=? WHERE id=?", digest(code), in.TokenID, version, db.UTC(s.Now().Add(60*time.Second)), in.ID)
		q.Set("code", code)
	} else {
		_, e = tx.ExecContext(r.Context(), "UPDATE oauth_requests SET consumed=1 WHERE id=?", in.ID)
		q.Set("error", "access_denied")
	}
	if e == nil {
		e = s.audit(r.Context(), tx, "oauth_consent", "")
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, e)
		return true
	}
	u.RawQuery = q.Encode()
	jsonOut(w, 200, map[string]string{"redirect": u.String()})
	return true
}
func (s *Service) oauthToken(w http.ResponseWriter, r *http.Request) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Method != "POST" || r.URL.RawQuery != "" || ct != "application/x-www-form-urlencoded" || r.Header.Get("Authorization") != "" {
		oauthError(w, 400, "invalid_request")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil || !singleOAuth(r.PostForm) || r.PostForm.Get("resource") != s.Config.BaseURL+MCPResourcePath {
		oauthError(w, 400, "invalid_request")
		return
	}
	f := r.PostForm
	for k := range f {
		if k != "grant_type" && k != "client_id" && k != "code" && k != "redirect_uri" && k != "code_verifier" && k != "resource" && k != "refresh_token" && k != "scope" {
			oauthError(w, 400, "invalid_request")
			return
		}
	}
	if f.Get("scope") != "" && f.Get("scope") != oauthScope {
		oauthError(w, 400, "invalid_scope")
		return
	}
	out, e := s.exchangeOAuth(r.Context(), f)
	if e != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	jsonOut(w, 200, out)
}
func (s *Service) exchangeOAuth(ctx context.Context, f url.Values) (map[string]any, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var redirect string
	if e = tx.QueryRowContext(ctx, "SELECT redirect_uri FROM oauth_clients WHERE id=? AND expires_at>?", f.Get("client_id"), s.now()).Scan(&redirect); e != nil {
		return nil, ErrAuth
	}
	access, e := secret()
	if e != nil {
		return nil, e
	}
	refresh, e := secret()
	if e != nil {
		return nil, e
	}
	expiry := db.UTC(s.Now().Add(30 * 24 * time.Hour))
	accessExpiry := db.UTC(s.Now().Add(15 * time.Minute))
	switch f.Get("grant_type") {
	case "authorization_code":
		if !validSecret(f.Get("code")) || !pkceVerifier.MatchString(f.Get("code_verifier")) || f.Get("redirect_uri") != redirect {
			return nil, ErrAuth
		}
		var id, challenge, tokenID string
		var version int64
		e = tx.QueryRowContext(ctx, `SELECT a.id,a.challenge,a.token_id,a.auth_version FROM oauth_requests a JOIN api_tokens t ON t.id=a.token_id JOIN owner o ON o.id=t.owner_id WHERE a.code_hash=? AND a.client_id=? AND a.consumed=0 AND a.expires_at>? AND a.auth_version=o.auth_version AND t.revoked_at IS NULL AND t.expires_at>?`, digest(f.Get("code")), f.Get("client_id"), s.now(), s.now()).Scan(&id, &challenge, &tokenID, &version)
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if e != nil || !same(challenge, base64.RawURLEncoding.EncodeToString(sum[:])) {
			// A repeated code is never redeemed twice. Revoke its issued grant.
			if errors.Is(e, sql.ErrNoRows) {
				if _, err := tx.ExecContext(ctx, "UPDATE oauth_grants SET revoked=1 WHERE code_hash=? AND client_id=?", digest(f.Get("code")), f.Get("client_id")); err == nil {
					_ = tx.Commit()
				}
			}
			return nil, ErrAuth
		}
		grant, e := db.NewID()
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE oauth_requests SET consumed=1 WHERE id=?", id); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO oauth_grants(id,code_hash,client_id,token_id,auth_version,access_hash,access_expires,refresh_hash,expires_at) VALUES(?,?,?,?,?,?,?,?,?)", grant, digest(f.Get("code")), f.Get("client_id"), tokenID, version, digest(access), accessExpiry, digest(refresh), expiry); e != nil {
			return nil, e
		}
	case "refresh_token":
		if !validSecret(f.Get("refresh_token")) {
			return nil, ErrAuth
		}
		hash := digest(f.Get("refresh_token"))
		var id string
		e = tx.QueryRowContext(ctx, `SELECT g.id,g.expires_at FROM oauth_grants g JOIN api_tokens t ON t.id=g.token_id JOIN owner o ON o.id=t.owner_id WHERE g.refresh_hash=? AND g.client_id=? AND g.revoked=0 AND g.expires_at>? AND g.auth_version=o.auth_version AND t.revoked_at IS NULL AND t.expires_at>?`, hash, f.Get("client_id"), s.now(), s.now()).Scan(&id, &expiry)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				_, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked=1 WHERE client_id=? AND id IN(SELECT grant_id FROM oauth_refresh_used WHERE hash=?)`, f.Get("client_id"), hash)
				if err == nil {
					_ = tx.Commit()
				}
			}
			return nil, ErrAuth
		}
		var rotations int
		if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM oauth_refresh_used WHERE grant_id=?", id).Scan(&rotations); e != nil {
			return nil, e
		}
		if rotations >= 4096 {
			return nil, ErrAuth
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO oauth_refresh_used VALUES(?,?)", hash, id); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE oauth_grants SET access_hash=?,access_expires=?,refresh_hash=? WHERE id=?", digest(access), accessExpiry, digest(refresh), id); e != nil {
			return nil, e
		}
	default:
		return nil, ErrAuth
	}
	if e = s.audit(ctx, tx, "oauth_token_issued", ""); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 900, "refresh_token": refresh, "scope": oauthScope, "resource": s.Config.BaseURL + MCPResourcePath}, nil
}
