package social

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var facebookPostID = regexp.MustCompile(`^[0-9]{1,32}_[0-9]{1,32}$`)

func (p *remote) graphPath(path string) string { return p.origin + "/" + p.config.APIVersion + path }

func (p *remote) facebookPageToken(ctx context.Context, token string) (string, error) {
	secret, err := readOAuthSecret(p.config.ClientSecretFile)
	if err != nil {
		return "", err
	}
	q := url.Values{"grant_type": {"fb_exchange_token"}, "client_id": {p.config.ClientID},
		"client_secret": {secret}, "fb_exchange_token": {token}}
	var long oauthToken
	if _, err = p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.exchange, query: q}, &long); err != nil {
		return "", err
	}
	if !bearer(long.Access) {
		return "", ErrUnavailable
	}
	var page struct {
		ID    string `json:"id"`
		Token string `json:"access_token"`
	}
	q = url.Values{"fields": {"id,access_token"}}
	if _, err = p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.graphPath("/" + p.config.TargetID), token: long.Access, query: q}, &page); err != nil {
		return "", err
	}
	if page.ID != p.config.TargetID || !bearer(page.Token) {
		return "", ErrUnavailable
	}
	return page.Token, nil
}

func (p *remote) verifyFacebook(ctx context.Context, token string) (Account, error) {
	var a struct {
		ID, Name string
	}
	req := remoteRequest{method: http.MethodGet, endpoint: p.graphPath("/me"), token: token, query: url.Values{"fields": {"id,name"}}}
	if _, err := p.request(ctx, req, &a); err != nil {
		return Account{}, err
	}
	if !numericID.MatchString(a.ID) || !validAccountName(a.Name) {
		return Account{}, ErrUnavailable
	}
	if a.ID != p.config.TargetID {
		return Account{}, &providerError{status: http.StatusForbidden}
	}
	return Account{ID: a.ID, Name: a.Name, URL: "https://www.facebook.com/" + a.ID}, nil
}

func (p *remote) sendFacebook(ctx context.Context, token string, post Message) (Receipt, error) {
	if post.AccountID != p.config.TargetID {
		return Receipt{}, &providerError{status: http.StatusForbidden}
	}
	q := url.Values{"message": {textWithoutLink(post)}, "link": {post.Link}}
	var result struct{ ID string }
	if _, err := p.request(ctx, remoteRequest{method: http.MethodPost, endpoint: p.graphPath("/" + post.AccountID + "/feed"),
		token: token, form: q}, &result); err != nil {
		return Receipt{}, err
	}
	if !facebookPostID.MatchString(result.ID) || !strings.HasPrefix(result.ID, post.AccountID+"_") {
		return Receipt{}, &providerError{}
	}
	return receipt(result.ID, "https://www.facebook.com/"+result.ID, post.AccountID), nil
}
