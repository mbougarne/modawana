package social

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type xAdapter struct{ *remote }

var xHandle = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

func (p *xAdapter) Refresh(ctx context.Context, raw string) (string, error) {
	t, err := storedToken(raw)
	if err != nil {
		return "", err
	}
	if time.Until(t.Expires) > time.Minute {
		return raw, nil
	}
	f := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {t.Refresh}}
	var fresh oauthToken
	if _, err = p.request(ctx, remoteRequest{method: http.MethodPost, endpoint: p.exchange, form: f, basic: true}, &fresh); err != nil {
		return "", err
	}
	if fresh.Refresh == "" {
		fresh.Refresh = t.Refresh
	}
	return saveToken(fresh)
}

func (p *remote) verifyX(ctx context.Context, raw string) (Account, error) {
	token, err := storedToken(raw)
	if err != nil {
		return Account{}, err
	}
	var out struct {
		Data struct{ ID, Username string }
	}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodGet, endpoint: p.origin + "/2/users/me", token: token.Access}, &out); err != nil {
		return Account{}, err
	}
	if !numericID.MatchString(out.Data.ID) || !xHandle.MatchString(out.Data.Username) {
		return Account{}, ErrUnavailable
	}
	return Account{ID: out.Data.ID, Name: "@" + out.Data.Username, URL: "https://x.com/" + out.Data.Username}, nil
}

// Count the canonical URL with X's fixed URL weight. Use conservative weights
// for additional URLs and emoji sequences in the free-form commentary.
func (p *xAdapter) ValidLength(text, _ string) bool {
	length := 25
	for _, r := range text {
		switch {
		case r <= 0x10ff || r >= 0x2000 && r <= 0x200d || r >= 0x2010 && r <= 0x201f || r >= 0x2032 && r <= 0x2037:
			length++
		default:
			length += 2
		}
	}
	for word := range strings.FieldsSeq(text) {
		if (strings.HasPrefix(word, "https://") || strings.HasPrefix(word, "http://")) && utf8.RuneCountInString(word) < 23 {
			length += 23 - utf8.RuneCountInString(word)
		}
	}
	// Domain-only links may also be transformed. Be conservative in their presence.
	for word := range strings.FieldsSeq(text) {
		if strings.Contains(word, ".") && !strings.Contains(word, "://") {
			bare := strings.TrimFunc(word, unicode.IsPunct)
			if strings.Contains(bare, ".") && utf8.RuneCountInString(bare) < 23 {
				length += 23 - utf8.RuneCountInString(bare)
			}
		}
	}
	return length <= p.limit
}

func (p *remote) sendX(ctx context.Context, raw string, post Message) (Receipt, error) {
	token, err := storedToken(raw)
	if err != nil {
		return Receipt{}, err
	}
	var out struct {
		Data struct{ ID string }
	}
	if _, err := p.request(ctx, remoteRequest{method: http.MethodPost, endpoint: p.origin + "/2/tweets",
		token: token.Access, payload: map[string]string{"text": post.Text}}, &out); err != nil {
		return Receipt{}, err
	}
	if !numericID.MatchString(out.Data.ID) {
		return Receipt{}, &providerError{}
	}
	return receipt(out.Data.ID, "https://x.com/i/web/status/"+out.Data.ID, post.AccountID), nil
}
