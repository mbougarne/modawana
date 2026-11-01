package siteconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/storage"
	"os"
	"sync"
	"testing"
)

func setup(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	os.Chmod(root, 0700)
	d, e := db.Open(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	f, e := storage.Open(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.Close(); d.Close() })
	owner, _ := db.NewID()
	if _, e = d.Exec("INSERT INTO owner(id) VALUES(?)", owner); e != nil {
		t.Fatal(e)
	}
	return &Service{d, f}, owner
}
func num(n int64) *int64 { return &n }
func TestConfigurationReviewConflictAndRestore(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	d, e := s.Get(ctx, "profile")
	if e != nil || d.Version != 0 {
		t.Fatal(d, e)
	}
	var p Profile
	json.Unmarshal(d.Snapshot, &p)
	p.Name = Localized{"en": "Second Owner", "ar": "مالك آخر"}
	in := Input{ExpectedVersion: num(0), Snapshot: raw(p)}
	preview, e := s.Preview(ctx, "profile", in)
	if e != nil || len(preview.Required) != 1 {
		t.Fatal(preview, e)
	}
	if _, e = s.Apply(ctx, owner, "profile", in); !errors.Is(e, ErrReview) {
		t.Fatal(e)
	}
	if public, e := s.Public(ctx, "ar"); e != nil || public.Effective["name"].Text != "" {
		t.Fatal(public, e)
	}
	in.Reviews = []Review{preview.Required[0].Review}
	saved, e := s.Apply(ctx, owner, "profile", in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, owner, "profile", in); !errors.Is(e, db.ErrVersionConflict) {
		t.Fatal(e)
	}
	p.Name["en"] = "New source"
	in = Input{ExpectedVersion: num(saved.Version), Snapshot: raw(p)}
	next, e := s.Apply(ctx, owner, "profile", in)
	if e != nil {
		t.Fatal(e)
	}
	public, e := s.Public(ctx, "ar")
	if e != nil || public.Effective["name"].Text != "مالك آخر" || !public.Effective["name"].Stale {
		t.Fatal(public, e)
	}
	restored, e := s.Restore(ctx, "profile", saved.RevisionID, next.Version)
	if e != nil {
		t.Fatal(e)
	}
	if public, _ = s.Public(ctx, "en"); public.Effective["name"].Text != "New source" {
		t.Fatal("restore applied implicitly")
	}
	if _, e = s.Apply(ctx, owner, "profile", Input{ExpectedVersion: &restored.Version, Snapshot: restored.Snapshot}); e != nil {
		t.Fatal(e)
	}
	h, _, e := s.History(ctx, "profile", "")
	if e != nil || len(h) != 3 {
		t.Fatal(h, e)
	}
}
func TestConcurrentApplyAndPrivateMedia(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	policy := &access.Live{DB: s.DB}
	ms := media.New(s.DB, s.Files, policy)
	b, e := os.ReadFile("../media/testdata/poster.png")
	if e != nil {
		t.Fatal(e)
	}
	m, e := ms.Upload(ctx, owner, "logo.png", "image/png", "logo", bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	d, _ := s.Get(ctx, "site")
	var p Site
	json.Unmarshal(d.Snapshot, &p)
	p.LogoID = m.ID
	in := Input{ExpectedVersion: num(0), Snapshot: raw(p)}
	if _, e = s.Preview(ctx, "site", in); e != nil {
		t.Fatal(e)
	}
	r := access.Request{Surface: access.Media, ResourceID: m.ID}
	if dec, _ := policy.Resolve(ctx, r); dec.AllowsBody() {
		t.Fatal("preview made image public")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Apply(ctx, owner, "site", in); results <- e }()
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, db.ErrVersionConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
	if dec, _ := policy.Resolve(ctx, r); dec.AllowsBody() {
		t.Fatal("unused image made public")
	}
	p.LogoMode = "image"
	d, e = s.Apply(ctx, owner, "site", Input{ExpectedVersion: num(1), Snapshot: raw(p)})
	if e != nil {
		t.Fatal(e)
	}
	if dec, _ := policy.Resolve(ctx, r); !dec.AllowsBody() {
		t.Fatal("applied image inaccessible")
	}
	p.LogoID = ""
	p.LogoMode = "wordmark"
	_, e = s.Apply(ctx, owner, "site", Input{ExpectedVersion: &d.Version, Snapshot: raw(p)})
	if e != nil {
		t.Fatal(e)
	}
	if dec, _ := policy.Resolve(ctx, r); dec.AllowsBody() {
		t.Fatal("old image still public")
	}
	if e = ms.Delete(ctx, m.ID); !errors.Is(e, media.ErrReferenced) {
		t.Fatal(e)
	}
}
func TestLabelValidationAndExactReview(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	for _, labels := range []Labels{{"nav.articles": {"en": raw("")}}, {"new.key": {"fr": raw("Oui")}}, {"post.writtenBy": {"en": raw("Missing placeholder")}}, {"nav.articles": {"en": raw("<script>x</script>")}}, {"writing.count": {"ar": raw(map[string]string{"other": "{n}"})}}} {
		if _, _, e := validate("labels", raw(labels)); e == nil {
			t.Fatal("invalid labels accepted", labels)
		}
	}
	labels := Labels{"nav.articles": {"en": raw("Notes"), "fr": raw(""), "ar": raw("ملاحظات")}}
	in := Input{ExpectedVersion: num(0), Snapshot: raw(labels)}
	pr, e := s.Preview(ctx, "labels", in)
	if e != nil || len(pr.Required) != 1 {
		t.Fatal(pr, e)
	}
	in.Reviews = []Review{pr.Required[0].Review}
	labels["nav.articles"]["ar"] = raw("كتابات")
	in.Snapshot = raw(labels)
	if _, e = s.Apply(ctx, owner, "labels", in); !errors.Is(e, ErrReview) {
		t.Fatal(e)
	}
	for _, u := range []string{"//evil.test", "https://user:pw@example.org", "javascript:x", "/\\evil.test", "/%2fevil.test", "mailto:a@example.com?subject=x"} {
		if safeURL(u) {
			t.Fatal(u)
		}
	}
}

func TestCompleteSnapshotsAndCanonicalPluralReview(t *testing.T) {
	var fields map[string]json.RawMessage
	json.Unmarshal(defaults("profile"), &fields)
	delete(fields, "show_now")
	if _, _, err := validate("profile", raw(fields)); !errors.Is(err, ErrInvalid) {
		t.Fatal("partial snapshot accepted", err)
	}
	fields["show_now"] = json.RawMessage("null")
	if _, _, err := validate("profile", raw(fields)); !errors.Is(err, ErrInvalid) {
		t.Fatal("null boolean accepted", err)
	}
	for _, url := range []string{"https://example.test/%09bad", "/%00bad", "//example.test", "https://user:pass@example.test"} {
		if safeURL(url) {
			t.Fatal("unsafe URL", url)
		}
	}
	first := json.RawMessage(`{"custom.count":{"en":{"one":"{n} item","other":"{n} items"},"fr":{"one":"{n} élément","other":"{n} éléments"}}}`)
	second := json.RawMessage(`{"custom.count":{"fr":{"other":"{n} éléments","one":"{n} élément"},"en":{"other":"{n} items","one":"{n} item"}}}`)
	a, _, e := validate("labels", first)
	if e != nil {
		t.Fatal(e)
	}
	b, _, e := validate("labels", second)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("JSON formatting changed translation identity")
	}
}
