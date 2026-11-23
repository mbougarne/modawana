package portability

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/media"
)

func TestPortablePageBlocksAndLegacyMarkdownArchives(t *testing.T) {
	ctx := t.Context()
	source, owner := fixture(t)
	cs := &content.Service{DB: source.DB, Files: source.Files}
	zero := int64(0)
	in := content.DraftInput{ExpectedVersion: &zero, Kind: "page", PageTemplate: "landing",
		SourceLocale: "en", Slug: "welcome", Title: "Welcome", Markdown: "# Legacy page"}
	legacy, err := cs.Save(ctx, owner, "welcome", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	export := func(schema int) string {
		t.Helper()
		archive := filepath.Join(t.TempDir(), "content.zip")
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		err = source.Export(ctx, f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		root, _, err := bundle.Read(archive, "content")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(root)
		raw, err := os.ReadFile(filepath.Join(root, "data.json"))
		var data Data
		if err != nil || json.Unmarshal(raw, &data) != nil || data.Schema != schema {
			t.Fatal("unexpected portable data schema", data.Schema, err)
		}
		if schema == 1 && (len(data.Revisions) != 1 || data.Revisions[0].BodyPath != "content/"+legacy.RevisionID+".md" ||
			data.Revisions[0].Input.BodyFormat != "") {
			t.Fatal("Markdown-only exports changed the schema-1 body contract", data.Revisions)
		}
		return archive
	}

	legacyTarget, legacyOwner := fixture(t)
	if _, err := legacyTarget.Import(ctx, legacyOwner, export(1), nil); err != nil {
		t.Fatal("legacy archive failed to import", err)
	}
	legacyContent := &content.Service{DB: legacyTarget.DB, Files: legacyTarget.Files}
	got, err := legacyContent.Draft(ctx, "welcome", "en")
	if err != nil || got.Revision.Markdown != in.Markdown || got.Revision.PageTemplate != in.PageTemplate {
		t.Fatal("legacy page changed during import", got, err)
	}

	var imageBytes bytes.Buffer
	if err := png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 10, 10))); err != nil {
		t.Fatal(err)
	}
	m, err := media.New(source.DB, source.Files, nil).Upload(ctx, owner, "banner.png", "image/png", "content", &imageBytes)
	if err != nil {
		t.Fatal(err)
	}
	blockDocument := func(id, text string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"version":1,"blocks":[
			{"id":"columns","type":"core.columns","version":1,"properties":{"columns":[[
				{"id":"intro","type":"core.hero","version":1,"properties":{"heading":%q,"alt":"Banner","media_id":%q}},
				{"id":"photo","type":"core.image","version":1,"properties":{"alt":"Banner","media_id":%q}}
			],[
				{"id":"cta","type":"core.call_to_action","version":1,"properties":{"heading":"Read more","label":"Open","href":"/media/%s"}},
				{"id":"body","type":"core.markdown","version":1,"properties":{"markdown":%q}}
			]]}}
		]}`, text, id, id, id, fmt.Sprintf("[Image](/media/%s) and [reference][image]. Code keeps %s.\n\n[image]: /media/%s", id, "`/media/"+m.ID+"`", id)))
	}
	in.ExpectedVersion, in.BodyFormat, in.Markdown = &legacy.Version, content.BlockBodyFormat, ""
	in.PageBlocks = blockDocument(m.ID, "Welcome")
	blocks, err := cs.Save(ctx, owner, "welcome", "en", "", in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.ChangePublication(ctx, owner, "welcome", "en", "", "publish", content.Action{ExpectedVersion: &blocks.Version, RevisionID: blocks.RevisionID}); err != nil {
		t.Fatal(err)
	}
	fr := in
	fr.ExpectedVersion, fr.SourceRevisionID, fr.Slug, fr.Title = &zero, blocks.RevisionID, "bienvenue", "Bienvenue"
	fr.PageBlocks = blockDocument(m.ID, "Bienvenue")
	if _, err := cs.Save(ctx, owner, "welcome", "fr", "", fr); err != nil {
		t.Fatal(err)
	}
	target, targetOwner := fixture(t)
	result, err := target.Import(ctx, targetOwner, export(2), nil)
	if err != nil || result.Revisions != 3 || result.Media != 1 || !result.ReviewRequired {
		t.Fatal(result, err)
	}
	c := &content.Service{DB: target.DB, Files: target.Files}
	draft, err := c.Draft(ctx, "welcome", "en")
	if err != nil || draft.PublishedID != "" || draft.Revision.BodyFormat != content.BlockBodyFormat ||
		draft.Revision.PageTemplate != "landing" || len(draft.Revision.Media) != 1 || draft.Revision.Media[0].ID == m.ID {
		t.Fatal("mixed-format import did not create an unpublished block draft with remapped media", draft, err)
	}
	compareBlocks := func(actual, want json.RawMessage) {
		t.Helper()
		var a, b any
		if json.Unmarshal(actual, &a) != nil || json.Unmarshal(want, &b) != nil {
			t.Fatal("malformed block documents")
		}
		canonicalActual, _ := json.Marshal(a)
		canonicalWant, _ := json.Marshal(b)
		if !bytes.Equal(canonicalActual, canonicalWant) {
			t.Fatalf("block IDs, contents or references changed\n got %s\nwant %s", canonicalActual, canonicalWant)
		}
	}
	newMedia := draft.Revision.Media[0].ID
	compareBlocks(draft.Revision.PageBlocks, blockDocument(newMedia, "Welcome"))
	translation, err := c.Draft(ctx, "welcome", "fr")
	if err != nil || translation.PublishedID != "" || translation.Reviewed || translation.Revision.SourceID != draft.Revision.ID {
		t.Fatal("source linkage or draft-only import rules changed", translation, err)
	}
	compareBlocks(translation.Revision.PageBlocks, blockDocument(newMedia, "Bienvenue"))
	history, _, err := c.History(ctx, "welcome", "en", "")
	if err != nil || len(history) != 2 {
		t.Fatal(history, err)
	}
	old, err := c.Historical(ctx, "welcome", "en", history[1].ID)
	if err != nil || old.Markdown != "# Legacy page" || old.BodyFormat != "" {
		t.Fatal("historical Markdown revision was not retained", old, err)
	}
}
