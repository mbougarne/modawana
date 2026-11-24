// Package social connects the owner to an approved provider and delivers
// explicitly scheduled links to exact published revisions.
package social

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/mbougarne/modawana/backend/internal/publichttp"
)

const CallbackPath = "/api/v1/owner/social/callback"
const mastodonScopes = "profile write:statuses"
const replayWindow = 45 * time.Minute

var ErrInvalid = errors.New("invalid social publishing request")
var ErrUnavailable = errors.New("social connection unavailable")
var ErrConflict = errors.New("social operation conflicts with saved state")

var numericID = regexp.MustCompile(`^[0-9]{1,32}$`)
var mastodonName = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
var oauthValue = regexp.MustCompile(`^[A-Za-z0-9_.~-]+$`)

func validOAuthValue(value string) bool {
	return len(value) <= 1024 && oauthValue.MatchString(value)
}

type MastodonConfig struct {
	Instance, ClientID, ClientSecretFile string
}

type Account struct {
	ID   string `json:"id"`
	Name string `json:"username"`
	URL  string `json:"url"`
}

type Receipt struct {
	ID      string  `json:"id"`
	URL     string  `json:"url"`
	Account Account `json:"account"`
}

type provider interface {
	Instance() string
	AuthorizationURL(string, string) string
	Exchange(context.Context, string, string) (string, error)
	Verify(context.Context, string) (Account, error)
	Send(context.Context, string, string, string) (Receipt, error)
}

type mastodon struct {
	instance, callback, clientID, secretFile string
	client                                   *http.Client
}

func newMastodon(c MastodonConfig, baseURL, dataRoot string) (*mastodon, error) {
	u, err := url.Parse(c.Instance)
	if err != nil || !strings.HasPrefix(baseURL, "https://") || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" ||
		u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || u.Host != u.Hostname() ||
		!publichttp.ValidHost(u.Hostname()) ||
		!validOAuthValue(c.ClientID) || !filepath.IsAbs(c.ClientSecretFile) {
		return nil, ErrInvalid
	}
	secret, err := filepath.EvalSymlinks(c.ClientSecretFile)
	if err != nil {
		return nil, ErrInvalid
	}
	relative, err := filepath.Rel(dataRoot, secret)
	if err != nil || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..") {
		return nil, ErrInvalid
	}
	m := &mastodon{instance: c.Instance, callback: baseURL + CallbackPath, clientID: c.ClientID, secretFile: c.ClientSecretFile,
		client: publichttp.NewClient(6 * time.Second)}
	if _, err := m.readSecret(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *mastodon) readSecret() (string, error) {
	f, err := os.OpenFile(m.secretFile, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", ErrUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", ErrUnavailable
	}
	b, err := io.ReadAll(io.LimitReader(f, 1026))
	secret := strings.TrimSpace(string(b))
	if err != nil || len(b) > 1025 || !validOAuthValue(secret) {
		return "", ErrUnavailable
	}
	return secret, nil
}

func (m *mastodon) Instance() string { return m.instance }

func (m *mastodon) AuthorizationURL(state, challenge string) string {
	q := url.Values{"response_type": {"code"}, "client_id": {m.clientID}, "redirect_uri": {m.callback}, "scope": {mastodonScopes},
		"state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	return m.instance + "/oauth/authorize?" + q.Encode()
}

type providerError struct {
	status int
	retry  time.Time
}

func (e *providerError) Error() string { return "Mastodon request failed" }

func (m *mastodon) call(ctx context.Context, method, path, token, id string, values url.Values, out any) error {
	var body io.Reader
	if values != nil {
		body = strings.NewReader(values.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, m.instance+path, body)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Accept", "application/json")
	if values != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id != "" {
		req.Header.Set("Idempotency-Key", id)
	}
	res, err := m.client.Do(req)
	if err != nil {
		return &providerError{}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &providerError{status: res.StatusCode, retry: retryAfter(res.Header.Get("Retry-After"), time.Now())}
	}
	const limit = 256 << 10
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(b) > limit || json.Unmarshal(b, out) != nil {
		// An unreadable POST response may hide a successful delivery.
		return &providerError{}
	}
	return nil
}

func retryAfter(raw string, now time.Time) time.Time {
	n, err := strconv.ParseInt(raw, 10, 32)
	if err == nil && n >= 0 {
		return now.Add(time.Duration(n) * time.Second)
	}
	at, _ := http.ParseTime(raw)
	return at
}

func (m *mastodon) Exchange(ctx context.Context, code, verifier string) (string, error) {
	secret, err := m.readSecret()
	if err != nil {
		return "", err
	}
	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	q := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier},
		"client_id": {m.clientID}, "client_secret": {secret}, "redirect_uri": {m.callback}}
	if err := m.call(ctx, http.MethodPost, "/oauth/token", "", "", q, &out); err != nil {
		return "", err
	}
	scopes := strings.Fields(out.Scope)
	if !strings.EqualFold(out.TokenType, "Bearer") || !validOAuthValue(out.AccessToken) ||
		len(scopes) != 2 || !slices.Contains(scopes, "profile") || !slices.Contains(scopes, "write:statuses") {
		return "", ErrUnavailable
	}
	return out.AccessToken, nil
}

func (m *mastodon) Verify(ctx context.Context, token string) (Account, error) {
	var out Account
	err := m.call(ctx, http.MethodGet, "/api/v1/accounts/verify_credentials", token, "", nil, &out)
	if err == nil && (!numericID.MatchString(out.ID) || !mastodonName.MatchString(out.Name)) {
		err = ErrUnavailable
	}
	out.URL = m.safeURL(out.URL)
	return out, err
}

func (m *mastodon) Send(ctx context.Context, token, id, body string) (Receipt, error) {
	var out Receipt
	err := m.call(ctx, http.MethodPost, "/api/v1/statuses", token, id, url.Values{"status": {body}, "visibility": {"public"}}, &out)
	if err == nil && (!numericID.MatchString(out.ID) || !numericID.MatchString(out.Account.ID)) {
		err = &providerError{}
	}
	out.URL = m.safeURL(out.URL)
	return out, err
}

func (m *mastodon) safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" ||
		len(raw) > 2048 || "https://"+u.Host != m.instance || strings.ContainsFunc(raw, unicode.IsControl) {
		return ""
	}
	return u.String()
}
