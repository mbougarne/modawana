package editorial

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

const testCredential = "editorial-fixture-credential"

type fakeProvider func(context.Context, string, Input) (Output, error)

func (f fakeProvider) Suggest(ctx context.Context, credential string, in Input) (Output, error) {
	return f(ctx, credential, in)
}

func editorialFixture(t *testing.T, p Provider) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	f, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	owner, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec("INSERT INTO owner(id) VALUES(?)", owner); err != nil {
		t.Fatal(err)
	}
	c := &content.Service{DB: d, Files: f, Access: &access.Live{DB: d}}
	return New(d, c, "https://example.test", func() ([]byte, error) { return bytes.Repeat([]byte{42}, 32), nil }, p), owner
}

func publish(t *testing.T, s *Service, owner, key string, in content.DraftInput) content.Result {
	t.Helper()
	saved, err := s.Content.Save(t.Context(), owner, key, "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Content.ChangePublication(t.Context(), owner, key, "en", "", "publish", content.Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func draft(key string, version int64) content.DraftInput {
	return content.DraftInput{ExpectedVersion: &version, Kind: "article", SourceLocale: "en", Title: "Public title", Summary: "Public summary",
		Slug: key, Markdown: "# Public heading\n\nThis is public [link text](https://other.example/hidden-path).\n\n```text\ncode not selected\n```"}
}

func enable(t *testing.T, s *Service, limit int) Settings {
	t.Helper()
	current, err := s.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	value := testCredential
	next, err := s.Update(t.Context(), Update{Version: current.Version, Enabled: true, DailyLimit: limit, Credential: &value}, func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestEditorialUsesOnlyExplicitPublicSelection(t *testing.T) {
	var received []Input
	p := fakeProvider(func(_ context.Context, credential string, in Input) (Output, error) {
		if credential != testCredential {
			t.Error("provider did not receive the configured credential")
		}
		received = append(received, in)
		return Output{Text: "Suggestion"}, nil
	})
	s, owner := editorialFixture(t, p)
	pub := publish(t, s, owner, "article", draft("article", 0))
	in := SourceRequest{Kind: "summary", Key: "article", Locale: "en", Revision: pub.RevisionID, Target: "en", Fields: []string{"title"}}
	if _, err := s.Suggest(t.Context(), in); !errors.Is(err, ErrDisabled) {
		t.Fatal("generation enabled by default", err)
	}
	settings := enable(t, s, 10)
	raw, err := json.Marshal(settings)
	if err != nil || bytes.Contains(raw, []byte(testCredential)) {
		t.Fatal("settings response exposed credentials")
	}
	var stored []byte
	if err = s.DB.QueryRow("SELECT credential FROM editorial_settings WHERE id=1").Scan(&stored); err != nil ||
		len(stored) == 0 || bytes.Contains(stored, []byte(testCredential)) {
		t.Fatal("database stores a plaintext credential", err)
	}
	next := draft("article", pub.Version)
	next.Title, next.Summary, next.Markdown = "Private draft title", "Private draft summary", "Private draft body"
	unpublished, err := s.Content.Save(t.Context(), owner, "article", "en", "", next)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Suggest(t.Context(), in)
	if err != nil || out.Text != "Suggestion" || len(received) != 1 || received[0] != (Input{Kind: "summary", Locale: "en", Target: "en", Title: "Public title"}) {
		t.Fatal(out, received, err)
	}
	in.Revision = unpublished.RevisionID
	if _, err = s.Suggest(t.Context(), in); !errors.Is(err, ErrChanged) {
		t.Fatal("unpublished revision was accepted", err)
	}
	unlisted := draft("unlisted", 0)
	unlisted.SEO = &content.SEO{NoIndex: true}
	hidden := publish(t, s, owner, "unlisted", unlisted)
	in.Key, in.Revision = "unlisted", hidden.RevisionID
	if _, err = s.Suggest(t.Context(), in); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unlisted revision was accepted", err)
	}
	in.Key, in.Revision, in.Kind, in.Target, in.Fields = "article", pub.RevisionID, "translation", "fr", []string{"excerpt"}
	if _, err = s.Suggest(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	want := Input{Kind: "translation", Locale: "en", Target: "fr", Field: "excerpt", Excerpt: "Public heading This is public link text."}
	if len(received) != 2 || received[1] != want {
		t.Fatal("unexpected provider context", received)
	}
	current, err := s.Content.Draft(t.Context(), "article", "en")
	if err != nil || current.PublishedID != pub.RevisionID || current.Revision.ID != unpublished.RevisionID {
		t.Fatal("generation changed publication or draft", err)
	}
	if _, err = s.Content.Draft(t.Context(), "article", "fr"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("generated translation became a draft", err)
	}
}

func TestQuotaIsReservedBeforeProviderAndResetsAtMidnight(t *testing.T) {
	calls := 0
	p := fakeProvider(func(_ context.Context, _ string, _ Input) (Output, error) {
		calls++
		if calls == 1 {
			return Output{}, ErrTimeout
		}
		return Output{Text: "line one\nline two"}, nil
	})
	s, owner := editorialFixture(t, p)
	now := time.Date(2026, 11, 24, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	pub := publish(t, s, owner, "article", draft("article", 0))
	enable(t, s, 1)
	in := SourceRequest{Kind: "summary", Key: "article", Locale: "en", Revision: pub.RevisionID, Target: "en", Fields: []string{"title"}}
	if _, err := s.Suggest(t.Context(), in); !errors.Is(err, ErrTimeout) {
		t.Fatal(err)
	}
	if _, err := s.Suggest(t.Context(), in); !errors.Is(err, ErrQuota) || calls != 1 {
		t.Fatal("timeout refunded or retried a reservation", calls, err)
	}
	now = now.Add(time.Minute)
	if _, err := s.Suggest(t.Context(), in); !errors.Is(err, ErrOutput) || calls != 2 {
		t.Fatal("invalid output accepted or next UTC day not opened", calls, err)
	}
}

func TestCoverRequiresExplicitPublicImageSelection(t *testing.T) {
	var input Input
	s, owner := editorialFixture(t, fakeProvider(func(_ context.Context, _ string, in Input) (Output, error) {
		input = in
		return Output{Text: "A solid dark square"}, nil
	}))
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 400, 400)), nil); err != nil {
		t.Fatal(err)
	}
	image, err := media.New(s.DB, s.Content.Files, s.Content.Access).Upload(t.Context(), owner, "cover.jpg", "image/jpeg", "content", &jpg)
	if err != nil {
		t.Fatal(err)
	}
	body := draft("article", 0)
	body.CoverMediaID = image.ID
	pub := publish(t, s, owner, "article", body)
	enable(t, s, 10)
	in := SourceRequest{Kind: "alt_text", Key: "article", Locale: "en", Revision: pub.RevisionID, Target: "en", Fields: []string{"title"}}
	if _, err = s.Suggest(t.Context(), in); !errors.Is(err, ErrInvalid) {
		t.Fatal("cover sent without explicit selection", err)
	}
	in.Fields = []string{"cover"}
	if _, err = s.Suggest(t.Context(), in); err != nil ||
		input != (Input{Kind: "alt_text", Locale: "en", Target: "en", ImageURL: "https://example.test/media/" + image.ID}) {
		t.Fatal(input, err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }

func TestOpenAIAdapterUsesFixedEndpointAndRedactsFailures(t *testing.T) {
	p := NewOpenAI()
	p.client.Transport = roundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://api.openai.com/v1/responses" ||
			req.Header.Get("Authorization") != "Bearer "+testCredential {
			t.Error("wrong provider destination or authentication")
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Contains(raw, []byte(`"store":false`)) || !bytes.Contains(raw, []byte("Only the selected title")) || bytes.Contains(raw, []byte(testCredential)) {
			t.Fatal("wrong request envelope", err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"text\":\"Review this\",\"description\":\"\"}"}]}]}`))}, nil
	})
	out, err := p.Suggest(t.Context(), testCredential, Input{Kind: "summary", Title: "Only the selected title"})
	if err != nil || out.Text != "Review this" {
		t.Fatal(out, err)
	}
	p.client.Transport = roundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("secret upstream response " + testCredential))}, nil
	})
	if _, err = p.Suggest(t.Context(), testCredential, Input{}); !errors.Is(err, ErrProviderLimit) || strings.Contains(err.Error(), testCredential) {
		t.Fatal("upstream limit not sanitized", err)
	}
	p.client.Transport = roundTripper(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })
	if _, err = p.Suggest(t.Context(), testCredential, Input{}); !errors.Is(err, ErrTimeout) {
		t.Fatal("timeout not classified", err)
	}
	if _, err = parseOpenAI([]byte(`{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"refusal"}]}]}`)); !errors.Is(err, ErrOutput) {
		t.Fatal("provider refusal accepted as output", err)
	}
}
