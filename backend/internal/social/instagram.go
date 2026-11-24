package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type instagram struct{ *remote }

func (p *remote) instagramLongToken(ctx context.Context, token string) (string, error) {
	secret, err := readOAuthSecret(p.config.ClientSecretFile)
	if err != nil {
		return "", err
	}
	q := url.Values{"grant_type": {"ig_exchange_token"}, "client_secret": {secret}, "access_token": {token}}
	var long oauthToken
	if _, err := p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.origin + "/access_token", query: q}, &long); err != nil {
		return "", err
	}
	return saveToken(long)
}

func (p *instagram) Refresh(ctx context.Context, raw string) (string, error) {
	token, err := storedToken(raw)
	if err != nil {
		return "", err
	}
	if time.Until(token.Expires) > 7*24*time.Hour {
		return raw, nil
	}
	q := url.Values{"grant_type": {"ig_refresh_token"}, "access_token": {token.Access}}
	var fresh oauthToken
	if _, err := p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.origin + "/refresh_access_token", query: q}, &fresh); err != nil {
		return "", err
	}
	return saveToken(fresh)
}

func (p *remote) verifyInstagram(ctx context.Context, raw string) (Account, error) {
	token, err := storedToken(raw)
	if err != nil {
		return Account{}, err
	}
	var out struct {
		UserID   json.RawMessage `json:"user_id"`
		Username string          `json:"username"`
	}
	q := url.Values{"fields": {"user_id,username"}}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.graphPath("/me"), token: token.Access, query: q}, &out); err != nil {
		return Account{}, err
	}
	var id string
	if json.Unmarshal(out.UserID, &id) != nil {
		id = string(out.UserID)
	}
	if !numericID.MatchString(id) || !instagramHandle.MatchString(out.Username) {
		return Account{}, ErrUnavailable
	}
	return Account{ID: id, Name: "@" + out.Username, URL: "https://www.instagram.com/" + out.Username + "/"}, nil
}

var instagramHandle = regexp.MustCompile(`^[A-Za-z0-9_.]{1,30}$`)

func (p *instagram) PrepareMedia(ctx context.Context, raw string, post Message) (string, error) {
	token, err := storedToken(raw)
	if err != nil {
		return "", err
	}
	if !numericID.MatchString(post.AccountID) || post.Image == "" {
		return "", ErrImage
	}
	var out struct{ ID string }
	f := url.Values{"image_url": {post.Image}, "caption": {post.Text}}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodPost, endpoint: p.graphPath("/" + post.AccountID + "/media"),
		token: token.Access, form: f}, &out); err != nil {
		return "", err
	}
	if !numericID.MatchString(out.ID) {
		return "", &providerError{}
	}
	return out.ID, nil
}

func (p *instagram) MediaReady(ctx context.Context, raw, container string) error {
	status, err := p.mediaStatus(ctx, raw, container)
	if err != nil {
		return err
	}
	switch status {
	case "FINISHED":
		return nil
	case "IN_PROGRESS":
		return &providerError{status: http.StatusTooManyRequests, retry: time.Now().Add(10 * time.Second)}
	default:
		return &providerError{status: http.StatusBadRequest}
	}
}

func (p *instagram) mediaStatus(ctx context.Context, raw, container string) (string, error) {
	token, err := storedToken(raw)
	if err != nil {
		return "", err
	}
	if !numericID.MatchString(container) {
		return "", ErrInvalid
	}
	var out struct {
		Status string `json:"status_code"`
	}
	_, err = p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.graphPath("/" + container), token: token.Access, query: url.Values{"fields": {"status_code"}}}, &out)
	return out.Status, err
}

func (p *instagram) Reconcile(ctx context.Context, raw string, post Message) bool {
	status, err := p.mediaStatus(ctx, raw, post.Container)
	return err == nil && status == "PUBLISHED"
}

func (p *remote) sendInstagram(ctx context.Context, raw string, post Message) (Receipt, error) {
	token, err := storedToken(raw)
	if err != nil {
		return Receipt{}, err
	}
	if !numericID.MatchString(post.AccountID) || !numericID.MatchString(post.Container) {
		return Receipt{}, ErrInvalid
	}
	var out struct{ ID string }
	f := url.Values{"creation_id": {post.Container}}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodPost, endpoint: p.graphPath("/" + post.AccountID + "/media_publish"),
		token: token.Access, form: f}, &out); err != nil {
		return Receipt{}, err
	}
	if !numericID.MatchString(out.ID) {
		return Receipt{}, &providerError{}
	}
	return receipt(out.ID, "", post.AccountID), nil
}
