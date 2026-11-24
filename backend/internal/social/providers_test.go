package social

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/media"
)

func configuredAdapter(t *testing.T, s *Service, key string) (Provider, *remote) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "client-secret")
	if err := os.WriteFile(file, []byte("test-app-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	c := AppConfig{ClientID: "test-app-id", ClientSecretFile: file}
	switch key {
	case facebookKey:
		c.TargetID, c.APIVersion = "121", "v27.0"
	case instagramKey:
		c.APIVersion = "v27.0"
	case linkedinPageKey:
		c.TargetID, c.APIVersion = "456", "202611"
	}
	p, err := NewRemote(key, c, s.baseURL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.providers[key] = p
	switch v := p.(type) {
	case *xAdapter:
		return p, v.remote
	case *instagram:
		return p, v.remote
	default:
		return p, p.(*remote)
	}
}

func connectRemote(t *testing.T, s *Service, p *remote) string {
	t.Helper()
	ctx := context.Background()
	flow, err := s.Start(ctx, p.key, "en", "fresh-session", fresh)
	if err != nil {
		t.Fatal(err)
	}
	q, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	if q.Query().Get("scope") != p.scope || !strings.HasPrefix(flow.URL, p.authorize+"?") {
		t.Fatal("incorrect OAuth authorization request")
	}
	transport := p.client.Transport
	p.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if p.key == xKey && r.URL.Path == "/2/oauth2/token" {
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			verifier := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			client, secret, basic := r.BasicAuth()
			if !basic || client != "test-app-id" || secret != "test-app-secret" ||
				base64.RawURLEncoding.EncodeToString(verifier[:]) != q.Query().Get("code_challenge") {
				t.Error("X did not use the registered confidential PKCE app")
			}
		}
		return transport.RoundTrip(r)
	})
	_, err = s.Complete(ctx, q.Query().Get("state"), flow.BrowserProof, "one-code", fresh)
	p.client.Transport = transport
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Configuration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range cfg.Destinations {
		if d.Provider == p.key && d.Connection != nil {
			return d.Connection.ID
		}
	}
	t.Fatal("missing connection for", p.key)
	return ""
}

func withCover(t *testing.T, s *Service, owner string, previous content.Result) content.Result {
	t.Helper()
	ctx := context.Background()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 800, 800)), nil); err != nil {
		t.Fatal(err)
	}
	cover, err := media.New(s.DB, s.Content.Files, s.Content.Access).Upload(ctx, owner, "cover.jpg", "image/jpeg", "content", &b)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Content.Save(ctx, owner, "article", "en", "", content.DraftInput{
		ExpectedVersion: &previous.Version, Kind: "article", SourceLocale: "en", Slug: "an-article",
		Title: "With cover", Summary: "Summary", Markdown: "# Heading\n\nBody.", CoverMediaID: cover.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Content.ChangePublication(ctx, owner, "article", "en", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRemoteProviderOAuthAndDelivery(t *testing.T) {
	for _, key := range []string{facebookKey, xKey, instagramKey, linkedinProfileKey, linkedinPageKey} {
		t.Run(key, func(t *testing.T) {
			s, _, owner, published := fixture(t)
			ctx := context.Background()
			_, wire := configuredAdapter(t, s, key)
			var sent, prepared, rotated int
			wire.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				q := r.URL.Query()
				p := r.URL.Path
				if r.Method == http.MethodPost {
					if err := r.ParseForm(); err != nil {
						return nil, err
					}
				}
				switch p {
				case "/v27.0/oauth/access_token":
					if key == facebookKey && q.Get("grant_type") == "fb_exchange_token" {
						if q.Get("fb_exchange_token") != "short-token" {
							t.Error("Facebook exchanged the wrong user token")
						}
						return reply(200, `{"access_token":"long-token"}`), nil
					}
					return reply(200, `{"access_token":"short-token"}`), nil
				case "/oauth/access_token":
					return reply(200, `{"data":[{"access_token":"short-token"}]}`), nil
				case "/access_token":
					if q.Get("grant_type") != "ig_exchange_token" || q.Get("access_token") != "short-token" {
						t.Error("Instagram long-lived token request is incorrect")
					}
					return reply(200, `{"access_token":"ig-token","expires_in":5184000}`), nil
				case "/oauth/v2/accessToken":
					if r.Form.Get("code") != "one-code" || r.Form.Get("redirect_uri") != wire.callback {
						t.Error("LinkedIn exchange lost the authorized redirect URI")
					}
					return reply(200, `{"access_token":"li-token","expires_in":5184000}`), nil
				case "/2/oauth2/token":
					if r.Form.Get("grant_type") == "refresh_token" {
						rotated++
						if r.Form.Get("refresh_token") != "x-refresh" {
							t.Error("X refreshed a different grant")
						}
						return reply(200, `{"access_token":"x-rotated-token","refresh_token":"x-new-refresh","expires_in":7200}`), nil
					}
					return reply(200, `{"access_token":"x-token","refresh_token":"x-refresh","expires_in":1}`), nil
				case "/v27.0/121":
					if r.Header.Get("Authorization") != "Bearer long-token" || q.Get("fields") != "id,access_token" {
						t.Error("Facebook did not look up the configured Page grant")
					}
					return reply(200, `{"id":"121","access_token":"page-token"}`), nil
				case "/v27.0/me":
					if key == facebookKey {
						return reply(200, `{"id":"121","name":"Facebook page"}`), nil
					}
					return reply(200, `{"user_id":321,"username":"creator"}`), nil
				case "/2/users/me":
					return reply(200, `{"data":{"id":"291","username":"my_user"}}`), nil
				case "/v2/userinfo":
					return reply(200, `{"sub":"personX","name":"My profile"}`), nil
				case "/rest/organizationAcls":
					if r.Header.Get("LinkedIn-Version") != "202611" || q.Get("state") != "APPROVED" {
						t.Error("LinkedIn skipped the current Page authority check")
					}
					return reply(200, `{"elements":[{"organizationTarget":"urn:li:organization:456","role":"CONTENT_ADMINISTRATOR","state":"APPROVED"}]}`), nil
				case "/rest/organizations/456":
					return reply(200, `{"id":456,"localizedName":"My company"}`), nil
				case "/v27.0/121/feed":
					sent++
					if r.Header.Get("Authorization") != "Bearer page-token" || r.Form.Get("message") != "A new article" ||
						r.Form.Get("link") != s.baseURL+"/en/articles/an-article" {
						t.Error("wrong Facebook Page publication")
					}
					return reply(200, `{"id":"121_500"}`), nil
				case "/2/tweets":
					sent++
					if r.Header.Get("Authorization") != "Bearer x-rotated-token" {
						t.Error("X did not use the renewed credential")
					}
					var payload map[string]string
					if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["text"] != "A new article\n\n"+s.baseURL+"/en/articles/an-article" {
						t.Error("X text or canonical route changed")
					}
					return nil, errors.New("X response lost after publication")
				case "/v27.0/321/media":
					prepared++
					if !strings.HasPrefix(r.Form.Get("image_url"), s.baseURL+"/media/") || !strings.HasPrefix(r.Form.Get("caption"), "A new article\n\n") {
						t.Error("Instagram container was created with unreviewed media or caption")
					}
					return reply(200, `{"id":"999"}`), nil
				case "/v27.0/999":
					if sent != 0 {
						return reply(200, `{"status_code":"PUBLISHED"}`), nil
					}
					if r.Header.Get("Authorization") != "Bearer ig-token" {
						t.Error("Instagram did not use its long-lived token")
					}
					return reply(200, `{"status_code":"FINISHED"}`), nil
				case "/v27.0/321/media_publish":
					sent++
					if r.Form.Get("creation_id") != "999" {
						t.Error("wrong Instagram container was published")
					}
					return nil, errors.New("Instagram response lost after publication")
				case "/rest/posts", "/v2/ugcPosts":
					sent++
					var payload map[string]any
					if json.NewDecoder(r.Body).Decode(&payload) != nil {
						t.Error("invalid LinkedIn post")
					}
					if key == linkedinProfileKey && (p != "/v2/ugcPosts" || payload["author"] != "urn:li:person:personX") {
						t.Error("personal post was routed to a Page")
					}
					if key == linkedinPageKey && (p != "/rest/posts" || payload["author"] != "urn:li:organization:456") {
						t.Error("Page post was routed to a personal profile")
					}
					resp := reply(201, "")
					resp.Header.Set("X-RestLi-Id", "urn:li:share:777")
					return resp, nil
				default:
					t.Errorf("unexpected %s request %s", key, p)
					return nil, errors.New("unexpected endpoint")
				}
			})
			id := connectRemote(t, s, wire)
			if key == instagramKey {
				in := Draft{Connection: id, Key: "article", Locale: "en", Revision: published.RevisionID, Text: "A new article"}
				if _, err := s.Preview(ctx, in); !errors.Is(err, ErrImage) {
					t.Fatal("accepted Instagram without a public cover", err)
				}
				published = withCover(t, s, owner, published)
			}
			in := Draft{Connection: id, Key: "article", Locale: "en", Revision: published.RevisionID, Text: "A new article"}
			preview, err := s.Preview(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			job := Schedule{ID: newID(t), Draft: in, Path: preview.Path, Image: preview.Image}
			if err := s.Schedule(ctx, job); err != nil {
				t.Fatal(err)
			}
			stop := runWorker(t, s)
			wanted := "sent"
			if key == xKey {
				wanted = "uncertain"
			}
			post := waitFor(t, s, wanted)
			stop()
			if sent != 1 || post.Provider != key || key == instagramKey && prepared != 1 || key == xKey && rotated != 1 {
				t.Fatal("unexpected provider outcome", post, sent, prepared, rotated)
			}
			if key == xKey && (post.Attempts != 1 || post.Reason != "delivery_unconfirmed") {
				t.Fatal("the job tried to replay an ambiguous X send", post)
			}
		})
	}
}

func TestLinkedInDestinationsKeepIndependentConnections(t *testing.T) {
	ctx := context.Background()
	s, _, _, published := fixture(t)
	_, profile := configuredAdapter(t, s, linkedinProfileKey)
	_, page := configuredAdapter(t, s, linkedinPageKey)
	profile.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/oauth/v2/accessToken":
			return reply(200, `{"access_token":"profile-token"}`), nil
		case "/v2/userinfo":
			return reply(200, `{"sub":"personX","name":"My profile"}`), nil
		default:
			return nil, errors.New("profile provider called unexpectedly")
		}
	})
	page.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/oauth/v2/accessToken":
			return reply(200, `{"access_token":"company-token"}`), nil
		case "/rest/organizationAcls":
			return reply(200, `{"elements":[{"organization":"urn:li:organization:456","role":"ADMINISTRATOR","state":"APPROVED"}]}`), nil
		case "/rest/organizations/456":
			return reply(200, `{"localizedName":"My company"}`), nil
		default:
			return nil, errors.New("company provider called unexpectedly")
		}
	})
	profileID := connectRemote(t, s, profile)
	pageID := connectRemote(t, s, page)
	if profileID == pageID {
		t.Fatal("the profile and Page shared a connection")
	}
	in := Draft{Connection: profileID, Key: "article", Locale: "en", Revision: published.RevisionID, Text: "Share my article"}
	preview, err := s.Preview(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(ctx, Schedule{Draft: in, ID: newID(t), Path: preview.Path}); err != nil {
		t.Fatal(err)
	}
	if err := s.Disconnect(ctx, linkedinProfileKey, "fresh-session", fresh); err != nil {
		t.Fatal(err)
	}
	in.Connection = pageID
	if _, err := s.Preview(ctx, in); err != nil {
		t.Fatal("disconnecting the profile affected the Page", err)
	}
	runWorker(t, s)
	if post := waitFor(t, s, "blocked"); post.Provider != linkedinProfileKey || post.Destination != "My profile" || post.Reason != "connection_changed" {
		t.Fatal("scheduled profile post was rerouted", post)
	}
}
