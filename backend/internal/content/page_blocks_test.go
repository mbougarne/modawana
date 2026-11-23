package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestPageBlockRevisionsUseExactSavePublicationAndRestore(t *testing.T) {
	s, owner := fixture(t)
	ctx := t.Context()
	in := input()
	in.Kind, in.PageTemplate = "page", "landing"
	const key = "standalone-page"
	const route = "/en/pages/first"
	legacy, err := s.Save(ctx, owner, key, "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(locale string, saved Result, operation, retryKey, source string) Result {
		t.Helper()
		res, err := s.ChangePublication(ctx, owner, key, locale, retryKey, operation,
			Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID, SourceRevisionID: source})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	old := publish("en", legacy, "publish", "", "")
	in.ExpectedVersion, in.BodyFormat, in.Markdown = &old.Version, BlockBodyFormat, ""
	in.PageBlocks = json.RawMessage(`{"version":1,"blocks":[
		{"id":"intro","type":"core.heading","version":1,"properties":{"text":"Welcome","level":2}},
		{"id":"copy","type":"core.markdown","version":1,"properties":{"markdown":"Details"}}
	]}`)
	saved, err := s.Save(ctx, owner, key, "en", "block-save", in)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.Save(ctx, owner, key, "en", "block-save", in); err != nil || again != saved {
		t.Fatal("retry did not replay the save result", again, err)
	}
	changed := in
	changed.PageBlocks = bytes.ReplaceAll(in.PageBlocks, []byte("Welcome"), []byte("Changed"))
	if _, err := s.Save(ctx, owner, key, "en", "block-save", changed); !errors.Is(err, ErrIdempotency) {
		t.Fatal("changed block data should conflict with the same idempotency key", err)
	}
	if _, err := s.Save(ctx, owner, key, "en", "", changed); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("stale block edit should fail", err)
	}
	legacyWrite := in
	legacyWrite.ExpectedVersion, legacyWrite.BodyFormat, legacyWrite.PageBlocks = &saved.Version, "", nil
	if _, err := s.Save(ctx, owner, key, "en", "", legacyWrite); !errors.Is(err, ErrInvalid) {
		t.Fatal("implicit Markdown save should not replace a blocks revision", err)
	}
	draft, err := s.Draft(ctx, key, "en")
	if err != nil || draft.Revision.BodyFormat != BlockBodyFormat || draft.Revision.PageTemplate != "landing" ||
		!bytes.Equal(in.PageBlocks, draft.Revision.PageBlocks) || draft.PublishedID != legacy.RevisionID {
		t.Fatal("block bytes, legacy publication or page template changed", draft, err)
	}
	page, err := s.ResolvePage(ctx, route)
	if err != nil || page.URL != route || page.Content == nil || page.Content.Markdown != input().Markdown || len(page.Content.PageBlocks) != 0 {
		t.Fatal("the existing page URL did not resolve to the live Markdown revision", page, err)
	}
	live := publish("en", saved, "publish", "block-publish", "")
	if again := publish("en", saved, "publish", "block-publish", ""); again != live {
		t.Fatal("publish did not replay", again)
	}
	page, err = s.ResolvePage(ctx, route)
	if err != nil || page.URL != route || page.Content == nil || page.Content.RevisionID != saved.RevisionID ||
		page.Content.Key != key || !bytes.Equal(page.Content.PageBlocks, in.PageBlocks) {
		t.Fatal("publication did not select the exact blocks at the existing route", page, err)
	}
	publicJSON, err := json.Marshal(page.Content)
	if err != nil || bytes.Contains(publicJSON, []byte("page_blocks")) {
		t.Fatal("storage-only block payload was exposed through the public API", err)
	}
	legacyAction := Action{ExpectedVersion: &live.Version, RevisionID: legacy.RevisionID}
	restoredLegacy, err := s.Restore(ctx, owner, key, "en", "", legacyAction)
	if err != nil {
		t.Fatal(err)
	}
	draft, err = s.Draft(ctx, key, "en")
	if err != nil || draft.Revision.Markdown != input().Markdown || draft.Revision.BodyFormat != "" || draft.PublishedID != saved.RevisionID {
		t.Fatal("legacy restore changed body format or publication", draft, err)
	}
	blockAction := Action{ExpectedVersion: &restoredLegacy.Version, RevisionID: saved.RevisionID}
	restored, err := s.Restore(ctx, owner, key, "en", "block-restore", blockAction)
	if err != nil || restored.RevisionID == saved.RevisionID {
		t.Fatal("restore should create a new revision", restored, err)
	}
	if again, err := s.Restore(ctx, owner, key, "en", "block-restore", blockAction); err != nil || again != restored {
		t.Fatal("restore did not replay", again, err)
	}
	draft, err = s.Draft(ctx, key, "en")
	if err != nil || draft.Revision.ID != restored.RevisionID || draft.PublishedID != saved.RevisionID ||
		!bytes.Equal(draft.Revision.PageBlocks, in.PageBlocks) {
		t.Fatal("restored block artifact differs from history", draft, err)
	}
	entries, _, err := s.History(ctx, key, "en", "")
	if err != nil || len(entries) != 4 {
		t.Fatal("conflicts or retries produced extra revisions", entries, err)
	}

	fr := in
	fr.ExpectedVersion, fr.SourceRevisionID, fr.Slug = number(0), saved.RevisionID, "bienvenue"
	fr.PageBlocks = bytes.ReplaceAll(in.PageBlocks, []byte("Welcome"), []byte("Bienvenue"))
	translation, err := s.Save(ctx, owner, key, "fr", "", fr)
	if err != nil {
		t.Fatal(err)
	}
	action := Action{ExpectedVersion: &translation.Version, RevisionID: translation.RevisionID}
	if _, err := s.ChangePublication(ctx, owner, key, "fr", "", "publish", action); !errors.Is(err, ErrReview) {
		t.Fatal("blocks bypassed translation review", err)
	}
	reviewed := publish("fr", translation, "review", "", saved.RevisionID)
	translated := publish("fr", reviewed, "publish", "", "")
	public, err := s.Public(ctx, "fr", fr.Slug, access.Principal{})
	if err != nil || public.RevisionID != translated.RevisionID || !bytes.Equal(public.PageBlocks, fr.PageBlocks) {
		t.Fatal("published translation did not use its own artifact", public, err)
	}
}

func TestPageBlockSchemaBoundsAndValidation(t *testing.T) {
	wrap := func(blocks string) []byte { return []byte(`{"version":1,"blocks":[` + blocks + `]}`) }
	const heading = `{"id":"intro","type":"core.heading","version":1,"properties":{"level":2,"text":"Welcome"}}`
	const columns = `{"id":"layout","type":"core.columns","version":1,"properties":{"columns":[[` + heading + `],[]]}}`
	if err := ValidatePageBlocks(wrap(columns)); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"duplicate nested id":      wrap(columns + "," + heading),
		"missing properties":       wrap(strings.Replace(heading, `,"properties":{"level":2,"text":"Welcome"}`, "", 1)),
		"duplicate field":          wrap(strings.Replace(heading, `"level":2`, `"level":2,"level":3`, 1)),
		"unknown property":         wrap(strings.Replace(heading, `"text":"Welcome"`, `"text":"Welcome","markup":"<p>"`, 1)),
		"unknown document version": []byte(`{"version":2,"blocks":[]}`),
		"null blocks":              []byte(`{"version":1,"blocks":null}`),
		"unsafe link":              wrap(`{"id":"go","type":"core.button","version":1,"properties":{"label":"Open","href":"javascript:example"}}`),
		"raw HTML":                 wrap(`{"id":"copy","type":"core.markdown","version":1,"properties":{"markdown":"<p>Raw</p>"}}`),
		"too large":                append(wrap(heading), bytes.Repeat([]byte(" "), MaxPageBlockBytes)...),
	}
	var oversized []pageBlock
	deep := heading
	for i := 1; i <= maxPageBlocks+1; i++ {
		oversized = append(oversized, pageBlock{ID: "heading-" + strconv.Itoa(i), Type: "core.heading", Version: 1, Properties: json.RawMessage(`{"level":2,"text":"Welcome"}`)})
	}
	for _, id := range []string{"layer3", "layer2", "layer1"} {
		deep = `{"id":"` + id + `","type":"core.columns","version":1,"properties":{"columns":[[` + deep + `],[]]}}`
	}
	cases["too many blocks"], _ = json.Marshal(pageBlockDocument{Version: 1, Blocks: oversized})
	cases["too deeply nested"] = wrap(deep)
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePageBlocks(payload); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid block document accepted", err)
			}
		})
	}
	in := input()
	in.BodyFormat, in.PageBlocks = BlockBodyFormat, wrap(heading)
	if _, err := draftBlockReferences(in); !errors.Is(err, ErrInvalid) {
		t.Fatal("non-page drafts accepted blocks", err)
	}
	in.Kind = "page"
	if _, err := draftBlockReferences(in); !errors.Is(err, ErrInvalid) {
		t.Fatal("save accepted both body formats", err)
	}
}

func TestUnknownBlockIsPreservedButRequiresSupportToPublish(t *testing.T) {
	s, owner := fixture(t)
	in := input()
	in.Kind, in.BodyFormat, in.Markdown = "page", BlockBodyFormat, ""
	in.PageBlocks = json.RawMessage(`{"version":1,"blocks":[{"id":"future","type":"extension.banner","version":3,"properties":{"body":"Private extension payload"}}]}`)
	saved, err := s.Save(t.Context(), owner, "future", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Historical(t.Context(), "future", "en", saved.RevisionID)
	if err != nil || !bytes.Equal(got.PageBlocks, in.PageBlocks) {
		t.Fatal("unknown block was lost", err)
	}
	action := Action{ExpectedVersion: &saved.Version, RevisionID: saved.RevisionID}
	if _, err := s.ChangePublication(t.Context(), owner, "future", "en", "", "publish", action); !errors.Is(err, ErrInvalid) {
		t.Fatal("unavailable block dependency did not block publication", err)
	}
}
