package social

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/publichttp"
)

const (
	facebookKey        = "facebook"
	instagramKey       = "instagram"
	xKey               = "x"
	linkedinProfileKey = "linkedin-profile"
	linkedinPageKey    = "linkedin-page"
)

type AppConfig struct {
	ClientID, ClientSecretFile, TargetID, APIVersion string
}

type remote struct {
	key, origin, authorize, exchange, scope, callback string
	label                                             extension.LocalizedText
	config                                            AppConfig
	limit                                             int
	pkce                                              bool
	basic                                             bool
	client                                            *http.Client
}

var graphVersion = regexp.MustCompile(`^v[0-9]{2}\.0$`)
var linkedinVersion = regexp.MustCompile(`^20[0-9]{2}(0[1-9]|1[0-2])$`)
var remoteID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)
var signedToken = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]+$`)

func NewRemote(key string, c AppConfig, baseURL, dataRoot string) (Provider, error) {
	if c == (AppConfig{}) {
		return nil, nil
	}
	if !validOAuthValue(c.ClientID) || !strings.HasPrefix(baseURL, "https://") {
		return nil, ErrInvalid
	}
	if err := checkOAuthSecret(c.ClientSecretFile, dataRoot); err != nil {
		return nil, err
	}
	p := &remote{key: key, config: c, callback: baseURL + CallbackPath, client: publichttp.NewClient(4 * time.Second)}
	switch key {
	case facebookKey:
		if !numericID.MatchString(c.TargetID) || !graphVersion.MatchString(c.APIVersion) {
			return nil, ErrInvalid
		}
		p.origin = "https://graph.facebook.com"
		p.authorize = "https://www.facebook.com/" + c.APIVersion + "/dialog/oauth"
		p.exchange = p.origin + "/" + c.APIVersion + "/oauth/access_token"
		p.scope, p.limit = "pages_show_list,pages_read_engagement,pages_manage_posts", 5000
		p.label = extension.LocalizedText{"en": "Facebook Page", "fr": "Page Facebook", "ar": "صفحة فيسبوك"}
	case instagramKey:
		if c.TargetID != "" || !graphVersion.MatchString(c.APIVersion) {
			return nil, ErrInvalid
		}
		p.origin = "https://graph.instagram.com"
		p.authorize, p.exchange = "https://www.instagram.com/oauth/authorize", "https://api.instagram.com/oauth/access_token"
		p.scope, p.limit = "instagram_business_basic,instagram_business_content_publish", 2200
		p.label = extension.LocalizedText{"en": "Instagram", "fr": "Instagram", "ar": "إنستغرام"}
		return &instagram{remote: p}, nil
	case xKey:
		if c.TargetID != "" || c.APIVersion != "" {
			return nil, ErrInvalid
		}
		p.origin = "https://api.x.com"
		p.authorize, p.exchange = "https://x.com/i/oauth2/authorize", p.origin+"/2/oauth2/token"
		p.scope, p.limit, p.pkce, p.basic = "tweet.read tweet.write users.read offline.access", 280, true, true
		p.label = extension.LocalizedText{"en": "X", "fr": "X", "ar": "إكس"}
		return &xAdapter{remote: p}, nil
	case linkedinProfileKey, linkedinPageKey:
		p.origin = "https://api.linkedin.com"
		p.authorize, p.exchange = "https://www.linkedin.com/oauth/v2/authorization", "https://www.linkedin.com/oauth/v2/accessToken"
		p.limit = 3000
		if key == linkedinProfileKey {
			if c.TargetID != "" || c.APIVersion != "" {
				return nil, ErrInvalid
			}
			p.scope = "openid profile w_member_social"
			p.label = extension.LocalizedText{"en": "LinkedIn profile", "fr": "Profil LinkedIn", "ar": "الملف الشخصي على لينكدإن"}
		} else {
			if !numericID.MatchString(c.TargetID) || !linkedinVersion.MatchString(c.APIVersion) {
				return nil, ErrInvalid
			}
			p.scope = "r_organization_admin w_organization_social"
			p.label = extension.LocalizedText{"en": "LinkedIn company Page", "fr": "Page d’entreprise LinkedIn", "ar": "صفحة شركة على لينكدإن"}
		}
	default:
		return nil, ErrInvalid
	}
	return p, nil
}

func (p *remote) Descriptor() extension.Contribution {
	return extension.Contribution{Kind: extension.ProviderAdapter, Key: p.key, Version: 1, Target: "social", Label: p.label}
}

func (p *remote) Instance() string          { return p.origin }
func (p *remote) MaxTextLength() int        { return p.limit }
func (*remote) ReplayWindow() time.Duration { return 0 }

func (p *remote) AuthorizationURL(state, challenge string) string {
	q := url.Values{"response_type": {"code"}, "client_id": {p.config.ClientID}, "redirect_uri": {p.callback}, "scope": {p.scope}, "state": {state}}
	if p.pkce {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	return p.authorize + "?" + q.Encode()
}

type remoteRequest struct {
	method, endpoint, token string
	query, form             url.Values
	payload                 any
	basic                   bool
	linkedin                bool
}

func (p *remote) request(ctx context.Context, in remoteRequest, out any) (http.Header, error) {
	var body io.Reader
	if in.form != nil {
		body = strings.NewReader(in.form.Encode())
	}
	if in.payload != nil {
		b, err := json.Marshal(in.payload)
		if err != nil {
			return nil, ErrInvalid
		}
		body = bytes.NewReader(b)
	}
	endpoint := in.endpoint
	if len(in.query) > 0 {
		endpoint += "?" + in.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, in.method, endpoint, body)
	if err != nil {
		return nil, ErrInvalid
	}
	req.Header.Set("Accept", "application/json")
	if in.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if in.payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if in.token != "" {
		req.Header.Set("Authorization", "Bearer "+in.token)
	}
	if in.basic {
		secret, err := readOAuthSecret(p.config.ClientSecretFile)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(p.config.ClientID, secret)
	}
	if in.linkedin {
		req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
		if p.config.APIVersion != "" {
			req.Header.Set("LinkedIn-Version", p.config.APIVersion)
		}
	}
	res, err := p.client.Do(req)
	if err != nil {
		return nil, &providerError{}
	}
	defer res.Body.Close()
	const maxBody = 512 << 10
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, p.statusError(res, data)
	}
	if err != nil || len(data) > maxBody || out != nil && json.Unmarshal(data, out) != nil {
		return nil, &providerError{}
	}
	return res.Header, nil
}

func (p *remote) statusError(res *http.Response, body []byte) error {
	status := res.StatusCode
	if status == http.StatusBadRequest {
		var oauth struct{ Error string }
		if json.Unmarshal(body, &oauth) == nil && oauth.Error == "invalid_grant" {
			status = http.StatusUnauthorized
		}
	}
	if p.key == facebookKey || p.key == instagramKey {
		var graph struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &graph) == nil {
			switch graph.Error.Code {
			case 190:
				status = http.StatusUnauthorized
			case 10, 200:
				status = http.StatusForbidden
			case 4, 17, 32, 613:
				status = http.StatusTooManyRequests
			}
		}
	}
	return &providerError{status: status, retry: retryAfter(res.Header.Get("Retry-After"), time.Now())}
}

type oauthToken struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token,omitempty"`
	Expires int64  `json:"expires_in,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Data    []struct {
		Access string `json:"access_token"`
	} `json:"data,omitempty"`
}

type renewable struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expires time.Time `json:"expires"`
}

func bearer(value string) bool { return len(value) <= 8192 && signedToken.MatchString(value) }

func storedToken(raw string) (renewable, error) {
	var t renewable
	if json.Unmarshal([]byte(raw), &t) != nil || !bearer(t.Access) || (t.Refresh != "" && !bearer(t.Refresh)) || t.Expires.IsZero() {
		return t, ErrUnavailable
	}
	return t, nil
}

func saveToken(t oauthToken) (string, error) {
	if !bearer(t.Access) || (t.Refresh != "" && !bearer(t.Refresh)) || t.Expires < 1 || t.Expires > 365*24*3600 {
		return "", ErrUnavailable
	}
	b, err := json.Marshal(renewable{t.Access, t.Refresh, time.Now().Add(time.Duration(t.Expires) * time.Second)})
	return string(b), err
}

func (p *remote) Exchange(ctx context.Context, code, verifier string) (string, error) {
	f := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.callback}}
	req := remoteRequest{method: http.MethodPost, endpoint: p.exchange, form: f, basic: p.basic}
	if p.pkce {
		f.Set("code_verifier", verifier)
	}
	if !p.basic {
		secret, err := readOAuthSecret(p.config.ClientSecretFile)
		if err != nil {
			return "", err
		}
		f.Set("client_id", p.config.ClientID)
		f.Set("client_secret", secret)
	}
	if p.key == facebookKey {
		req.method, req.query, req.form = http.MethodGet, f, nil
	}
	var t oauthToken
	if _, err := p.request(ctx, req, &t); err != nil {
		return "", err
	}
	if p.key == instagramKey && t.Access == "" && len(t.Data) == 1 {
		t.Access = t.Data[0].Access
	}
	if !bearer(t.Access) {
		return "", ErrUnavailable
	}
	switch p.key {
	case xKey:
		if !bearer(t.Refresh) {
			return "", ErrUnavailable
		}
		return saveToken(t)
	case facebookKey:
		return p.facebookPageToken(ctx, t.Access)
	case instagramKey:
		return p.instagramLongToken(ctx, t.Access)
	default:
		return t.Access, nil
	}
}

func validAccountName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 400 && utf8.ValidString(name) && !strings.ContainsFunc(name, unicode.IsControl)
}

func (p *remote) Verify(ctx context.Context, token string) (Account, error) {
	switch p.key {
	case facebookKey:
		return p.verifyFacebook(ctx, token)
	case instagramKey:
		return p.verifyInstagram(ctx, token)
	case xKey:
		return p.verifyX(ctx, token)
	case linkedinProfileKey:
		return p.verifyLinkedInProfile(ctx, token)
	case linkedinPageKey:
		return p.verifyLinkedInPage(ctx, token)
	default:
		return Account{}, ErrUnavailable
	}
}

func (p *remote) Send(ctx context.Context, token string, post Message) (Receipt, error) {
	switch p.key {
	case facebookKey:
		return p.sendFacebook(ctx, token, post)
	case instagramKey:
		return p.sendInstagram(ctx, token, post)
	case xKey:
		return p.sendX(ctx, token, post)
	case linkedinProfileKey, linkedinPageKey:
		return p.sendLinkedIn(ctx, token, post)
	default:
		return Receipt{}, ErrUnavailable
	}
}

func receipt(id, url, author string) Receipt {
	return Receipt{ID: id, URL: url, Account: Account{ID: author}}
}

func textWithoutLink(post Message) string {
	return strings.TrimSuffix(post.Text, "\n\n"+post.Link)
}
