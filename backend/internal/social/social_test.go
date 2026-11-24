package social

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/outbox"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (rt roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return rt(r) }

const testOrigin = "https://social.example"
const testToken = "secret_test_access_token"
const accountJSON = `{"id":"123","username":"editor","url":"https://social.example/@editor"}`
const receiptJSON = `{"id":"461","url":"https://social.example/@editor/461","account":{"id":"123"}}`

func fixture(t *testing.T) (*Service, *mastodon, string, content.Result) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	files, err := storage.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	owner := newID(t)
	if _, err := database.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	contents := &content.Service{DB: database, Files: files, Access: &access.Live{DB: database}}
	version := int64(0)
	saved, err := contents.Save(ctx, owner, "article", "en", "", content.DraftInput{
		ExpectedVersion: &version, Kind: "article", SourceLocale: "en", Slug: "an-article",
		Title: "An article", Summary: "Summary", Markdown: "# Heading\n\nBody.",
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := contents.ChangePublication(ctx, owner, "article", "en", "", "publish",
		content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "client-secret")
	if err := os.WriteFile(secret, []byte("test_secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := newMastodon(MastodonConfig{testOrigin, "test_client", secret}, "https://blog.example", root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: database, Content: contents, provider: p, baseURL: "https://blog.example",
		key: func() ([]byte, error) { return bytes.Repeat([]byte{42}, 32), nil }}
	return s, p, owner, published
}

func newID(t *testing.T) string {
	t.Helper()
	id, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fresh(_ context.Context, _ *sql.Tx, session string) error {
	if session != "fresh-session" {
		return errors.New("session revoked")
	}
	return nil
}

func connect(t *testing.T, s *Service, p *mastodon, delivery roundTrip) string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.Start(ctx, "en", "revoked-session", fresh); err == nil {
		t.Fatal("accepted a revoked session")
	}
	begin, err := s.Start(ctx, "en", "fresh-session", fresh)
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(begin.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := link.Query()
	if link.Path != "/oauth/authorize" || link.Scheme+"://"+link.Host != testOrigin || query.Get("scope") != mastodonScopes ||
		query.Get("code_challenge_method") != "S256" || query.Get("redirect_uri") != "https://blog.example"+CallbackPath {
		t.Fatal("invalid OAuth authorization request")
	}
	calls := 0
	p.client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/oauth/token":
			calls++
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != query.Get("code_challenge") || r.Form.Get("client_secret") != "test_secret" ||
				r.Form.Get("code") != "provider-code" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != query.Get("redirect_uri") {
				t.Error("OAuth exchange did not validate the bound request")
			}
			return reply(http.StatusOK, `{"access_token":"`+testToken+`","token_type":"Bearer","scope":"profile write:statuses"}`), nil
		case "/api/v1/accounts/verify_credentials":
			if r.Header.Get("Authorization") != "Bearer "+testToken {
				t.Error("missing credentials")
			}
			return reply(http.StatusOK, accountJSON), nil
		default:
			t.Errorf("unexpected OAuth route %s", r.URL.Path)
			return nil, errors.New("unexpected request")
		}
	})
	state := query.Get("state")
	if _, err := s.Complete(ctx, state, randomProof(), "provider-code", fresh); err == nil {
		t.Fatal("accepted an unrelated browser")
	}
	if lang, err := s.Complete(ctx, state, begin.BrowserProof, "provider-code", fresh); err != nil || lang != "en" {
		t.Fatal(lang, err)
	}
	if _, err := s.Complete(ctx, state, begin.BrowserProof, "provider-code", fresh); err == nil || calls != 1 {
		t.Fatal("accepted a replayed callback", calls, err)
	}
	cfg, err := s.Configuration(ctx)
	if err != nil || cfg.Connection == nil || cfg.Connection.Account != "editor" || bytes.Contains(cfg.Connection.token, []byte(testToken)) {
		t.Fatal("invalid sealed connection")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil || bytes.Contains(encoded, []byte(testToken)) || bytes.Contains(encoded, []byte("token")) {
		t.Fatal("credential was serialized")
	}
	p.client.Transport = delivery
	return cfg.Connection.ID
}

func reply(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func schedule(t *testing.T, s *Service, connection, revision, at string) (Schedule, Preview) {
	t.Helper()
	ctx := context.Background()
	if candidates, err := s.Content.SocialCandidates(ctx, "en", ""); err != nil || len(candidates.Items) != 1 || candidates.Items[0].Revision != revision {
		t.Fatal(candidates, err)
	}
	draft := Draft{Connection: connection, Key: "article", Locale: "en", Revision: revision, Text: " A new article "}
	show, err := s.Preview(ctx, draft)
	if err != nil || show.Body != "A new article\n\nhttps://blog.example"+show.Path || show.Destination != "editor@social.example" {
		t.Fatal(show, err)
	}
	in := Schedule{Draft: draft, ID: newID(t), Path: show.Path, At: at}
	if err := s.Schedule(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := s.Schedule(ctx, in); err != nil {
		t.Fatal("exact retry:", err)
	}
	changed := in
	changed.Text = "Different"
	if err := s.Schedule(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("modified retry:", err)
	}
	return in, show
}

func runWorker(t *testing.T, s *Service) func() {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w := &outbox.Worker{DB: s.DB, Handle: s.Prepare}
		w.Run(ctx)
	}()
	closeWorker := func() { stop(); <-done }
	t.Cleanup(closeWorker)
	return closeWorker
}

func waitFor(t *testing.T, s *Service, want string) Post {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		history, err := s.History(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if len(history.Items) == 1 && history.Items[0].Status == want {
			return history.Items[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	history, _ := s.History(context.Background(), "")
	t.Fatalf("waiting for %s, got %+v", want, history)
	return Post{}
}

func TestDeliveryReconcilesLostResponseAcrossWorkerRestart(t *testing.T) {
	s, p, _, published := fixture(t)
	var operationID, body string
	remote := map[string]string{}
	calls := 0
	connection := connect(t, s, p, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/accounts/verify_credentials" && r.Header.Get("Authorization") == "Bearer "+testToken {
			return reply(http.StatusOK, accountJSON), nil
		}
		if r.URL.Path != "/api/v1/statuses" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("unexpected send %s %s", r.Method, r.URL.Path)
			return nil, errors.New("unexpected send")
		}
		key := r.Header.Get("Idempotency-Key")
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		if key != operationID || r.Form.Get("status") != body || r.Form.Get("visibility") != "public" {
			t.Error("the requested post changed")
		}
		calls++
		if value, ok := remote[key]; ok {
			return reply(http.StatusOK, value), nil
		}
		remote[key] = receiptJSON
		return nil, errors.New("response lost after remote success")
	})
	due := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	in, preview := schedule(t, s, connection, published.RevisionID, due.Format(time.RFC3339))
	operationID, body = in.ID, preview.Body
	var available int64
	if err := s.DB.QueryRow("SELECT available_at FROM outbox_deliveries WHERE id=?", in.ID).Scan(&available); err != nil || available != due.UnixMilli() {
		t.Fatal("job does not respect its scheduled time", available, err)
	}
	makeDue := func() {
		if _, err := s.DB.Exec("UPDATE outbox_deliveries SET available_at=? WHERE id=?", time.Now().UnixMilli(), in.ID); err != nil {
			t.Fatal(err)
		}
	}
	makeDue()
	stop := runWorker(t, s)
	attempt := waitFor(t, s, "retrying")
	stop()
	if attempt.Attempts != 1 || attempt.CanCancel || calls != 1 {
		t.Fatal(attempt, calls)
	}
	makeDue()
	stop = runWorker(t, s)
	sent := waitFor(t, s, "sent")
	stop()
	if len(remote) != 1 || calls != 2 || sent.Attempts != 2 || sent.RemoteID != "461" || sent.RemoteURL != testOrigin+"/@editor/461" {
		t.Fatal(sent, len(remote), calls)
	}
}

func TestDeliveryChecksPublicationAgainAfterProviderVerification(t *testing.T) {
	s, p, owner, published := fixture(t)
	connection := connect(t, s, p, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/accounts/verify_credentials" {
			t.Error("post was sent after unpublishing the revision")
			return nil, errors.New("unexpected send")
		}
		_, err := s.Content.ChangePublication(r.Context(), owner, "article", "en", "", "unpublish",
			content.Action{ExpectedVersion: &published.Version, RevisionID: published.RevisionID})
		return reply(http.StatusOK, accountJSON), err
	})
	schedule(t, s, connection, published.RevisionID, "")
	runWorker(t, s)
	if post := waitFor(t, s, "blocked"); post.Reason != "publication_changed" || post.RemoteID != "" {
		t.Fatal(post)
	}
}

func TestDeliveryBlocksRevokedAccess(t *testing.T) {
	s, p, _, published := fixture(t)
	connection := connect(t, s, p, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/accounts/verify_credentials" {
			t.Error("post was sent with revoked access")
		}
		return reply(http.StatusUnauthorized, ""), nil
	})
	schedule(t, s, connection, published.RevisionID, "")
	runWorker(t, s)
	if post := waitFor(t, s, "blocked"); post.Reason != "access_revoked" {
		t.Fatal(post)
	}
	cfg, err := s.Configuration(context.Background())
	if err != nil || cfg.Connection != nil {
		t.Fatal("revoked connection stayed active", err)
	}
}

func TestExpiredReplayCannotDispatch(t *testing.T) {
	s, p, _, published := fixture(t)
	connection := connect(t, s, p, func(r *http.Request) (*http.Response, error) {
		t.Error("provider was called after the replay window")
		return nil, errors.New("unexpected request")
	})
	in, _ := schedule(t, s, connection, published.RevisionID, "")
	if _, err := s.DB.Exec("UPDATE social_posts SET first_dispatch_at=? WHERE id=?", time.Now().Add(-replayWindow-time.Minute).UnixMilli(), in.ID); err != nil {
		t.Fatal(err)
	}
	runWorker(t, s)
	if post := waitFor(t, s, "uncertain"); post.Reason != "replay_window_expired" || post.CanCancel {
		t.Fatal(post)
	}
}

func TestSupersededAndCancelledRevisionsCannotDispatch(t *testing.T) {
	for _, action := range []string{"superseded", "cancelled", "disconnected"} {
		t.Run(action, func(t *testing.T) {
			ctx := context.Background()
			s, p, owner, published := fixture(t)
			connection := connect(t, s, p, func(r *http.Request) (*http.Response, error) {
				t.Errorf("provider called after %s", action)
				return nil, errors.New("unexpected request")
			})
			in, _ := schedule(t, s, connection, published.RevisionID, "")
			switch action {
			case "superseded":
				next, err := s.Content.Save(ctx, owner, "article", "en", "", content.DraftInput{
					ExpectedVersion: &published.Version, Kind: "article", SourceLocale: "en", Slug: "an-article",
					Title: "Updated", Summary: "Summary", Markdown: "# Updated\n\nNew body.",
				})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.Content.ChangePublication(ctx, owner, "article", "en", "", "publish",
					content.Action{ExpectedVersion: &next.Version, RevisionID: next.RevisionID}); err != nil {
					t.Fatal(err)
				}
				runWorker(t, s)
				if post := waitFor(t, s, "blocked"); post.Reason != "publication_changed" {
					t.Fatal(post)
				}
			case "cancelled":
				if err := s.Cancel(ctx, in.ID); err != nil {
					t.Fatal(err)
				}
				post := waitFor(t, s, "cancelled")
				if post.Attempts != 0 || post.CanCancel {
					t.Fatal(post)
				}
				var state string
				if err := s.DB.QueryRow("SELECT state FROM outbox_deliveries WHERE id=?", in.ID).Scan(&state); err != nil || state != "failed" {
					t.Fatal("cancelled job remained runnable", state, err)
				}
			case "disconnected":
				if err := s.Disconnect(ctx, "fresh-session", fresh); err != nil {
					t.Fatal(err)
				}
				runWorker(t, s)
				if post := waitFor(t, s, "blocked"); post.Reason != "connection_changed" {
					t.Fatal(post)
				}
			}
		})
	}
}
