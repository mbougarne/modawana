package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
)

func TestContributionApprovalAndDisable(t *testing.T) {
	const slug = "editorial-notes"
	raw := fixtureFile(t, "../../../examples/plugins/typescript/contributions/manifest.json")
	manifest, err := ParseManifest(raw, slug)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalidate := range []func(*Manifest){
		func(m *Manifest) { m.HostAPIVersion = 3 },
		func(m *Manifest) { m.Capabilities = []string{"settings"} },
		func(m *Manifest) { m.Contributions[0].Version = 2 },
		func(m *Manifest) { m.Contributions[0].Target = "custom_html" },
		func(m *Manifest) { m.Contributions = append(m.Contributions, m.Contributions[0]) },
		func(m *Manifest) { m.Settings, m.Capabilities = nil, []string{"contributions"} },
	} {
		candidate, _ := ParseManifest(raw, slug)
		invalidate(&candidate)
		bytes, _ := json.Marshal(candidate)
		if _, err := ParseManifest(bytes, slug); !errors.Is(err, ErrManifest) {
			t.Fatal("invalid contribution manifest accepted", err)
		}
	}
	s, host := fixture(t)
	var module []byte
	if path := os.Getenv("MODAWANA_TYPESCRIPT_PLUGIN_WASM"); path != "" {
		module = fixtureFile(t, path)
	} else {
		module = buildWASIFixture(t, "../../cmd/plugin-social-cards")
	}
	record := installStageThreeFixture(t, s, manifest, module)
	if len(host.Contributions("")) != len(manifest.Contributions) {
		t.Fatal("approved contributions unavailable")
	}
	if _, ok := host.RenderBlock(slug+".notice", 1, json.RawMessage(`{"text":"Approved"}`)); !ok {
		t.Fatal("approved block unavailable")
	}
	updated := manifest
	updated.Contributions[0].Key = "different-notice"
	raw, err = json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, slug, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	items, err := s.List(ctx)
	if err != nil || len(items) != 1 || items[0].State != "changed" || items[0].CandidateChecksum == "" {
		t.Fatal(items, err)
	}
	if _, ok := host.RenderBlock(slug+".different-notice", 1, json.RawMessage(`{"text":"Changed"}`)); ok {
		t.Fatal("unapproved contribution became available")
	}
	if _, err := s.ApproveCandidate(ctx, slug, "", record.RowVersion, pass); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("approval without checksum accepted", err)
	}
	record, err = s.ApproveCandidate(ctx, slug, items[0].CandidateChecksum, record.RowVersion, pass)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := host.RenderBlock(slug+".different-notice", 1, json.RawMessage(`{"text":"Changed"}`)); !ok {
		t.Fatal("approved replacement unavailable")
	}
	if _, ok := host.RenderBlock(slug+".notice", 1, json.RawMessage(`{"text":"Old"}`)); ok {
		t.Fatal("old declaration still registered")
	}
	if _, err := s.SetEnabled(ctx, slug, false, record.RowVersion, pass); err != nil {
		t.Fatal(err)
	}
	if len(host.Contributions("")) != 0 {
		t.Fatal("disabled contributions still registered")
	}
}
