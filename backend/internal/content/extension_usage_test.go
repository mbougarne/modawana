package content

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/extension"
)

type notesExtension struct{}

func (notesExtension) Slug() string { return "editorial-notes" }
func (notesExtension) Register(r *extension.Registrar) error {
	return r.Contribution(extension.Contribution{
		Kind: extension.BuilderBlock, Key: "notice", Version: 1, Target: "text_panel",
		Label: extension.LocalizedText{"en": "Editorial note"},
	}, nil)
}

func TestExtensionBlockPublicationAndPageUses(t *testing.T) {
	s, owner := fixture(t)
	ctx := t.Context()
	s.Extensions = extension.NewHost(nil)
	if err := s.Extensions.Install(notesExtension{}); err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Kind, in.PageTemplate, in.BodyFormat, in.Markdown = "page", "landing", BlockBodyFormat, ""
	in.PageBlocks = json.RawMessage(`{"version":1,"blocks":[
		{"id":"columns","type":"core.columns","version":1,"properties":{"columns":[[
			{"id":"notice","type":"editorial-notes.notice","version":1,"properties":{"heading":"An update","text":"<b>Just text</b>"}}
		],[]]}}
	]}`)
	valid := bytes.Clone(in.PageBlocks)
	save := func(key string) Result {
		t.Helper()
		result, err := s.Save(ctx, owner, key, "en", "", in)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	publish := func(key string, draft Result, retry string) (Result, error) {
		return s.ChangePublication(ctx, owner, key, "en", retry, "publish", Action{
			ExpectedVersion: &draft.Version, RevisionID: draft.RevisionID,
		})
	}
	draft := save("notes-page")
	live, err := publish("notes-page", draft, "publish-notes")
	if err != nil {
		t.Fatal(err)
	}
	options := PageBlockRenderOptions{Unavailable: "Block unavailable", ExtensionBlock: s.Extensions.RenderBlock}
	html, err := RenderPageBlocks(valid, options)
	if err != nil || !strings.Contains(string(html), "&lt;b&gt;Just text&lt;/b&gt;") {
		t.Fatal(html, err)
	}
	in.PageBlocks = bytes.Replace(valid, []byte(`"heading":`), []byte(`"markup":`), 1)
	in.Slug = "notes-draft"
	invalid := save("notes-draft")
	if _, err := publish("notes-draft", invalid, ""); err == nil {
		t.Fatal("invalid properties were published")
	}
	s.Extensions.Uninstall("editorial-notes")
	html, err = RenderPageBlocks(valid, options)
	if err != nil || !strings.Contains(string(html), "Block unavailable") || strings.Contains(string(html), "Just text") {
		t.Fatal(html, err)
	}
	uses, err := s.PageUses(ctx, "editorial-notes", "")
	if err != nil || len(uses.Items) != 2 || uses.Next != "" {
		t.Fatal(uses, err)
	}
	for _, item := range uses.Items {
		if !item.Draft || item.Published != (item.Key == "notes-page") {
			t.Fatal(item)
		}
	}
	if replay, err := publish("notes-page", draft, "publish-notes"); err != nil || replay != live {
		t.Fatal("disable changed exact retry", replay, err)
	}
	in.PageBlocks, in.Slug = valid, "new-notes"
	if _, err := publish("new-notes", save("new-notes"), ""); err == nil {
		t.Fatal("disabled block published")
	}
	if err := s.Extensions.Install(notesExtension{}); err != nil {
		t.Fatal(err)
	}
	existing, err := s.Draft(ctx, "notes-page", "en")
	if err != nil || existing.PublishedID != draft.RevisionID || !bytes.Equal(existing.Revision.PageBlocks, valid) {
		t.Fatal("disable changed the immutable page", existing, err)
	}
}
