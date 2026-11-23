package dashboard

import (
	"errors"
	"os"
	"testing"

	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
)

func TestDashboardDefaultsAndPersonalResetAreVersionedSeparately(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	owner, err := db.NewID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec("INSERT INTO owner(id,login_email) VALUES(?,?)", owner, "layout@example.test"); err != nil {
		t.Fatal(err)
	}
	s := Service{DB: d}
	zero := int64(0)
	site := &Layout{Appearance: Appearance{Theme: "classic"}, Home: "posts"}
	personal := &Layout{Appearance: Appearance{Theme: "slate", Density: "compact"}, Home: "dashboard",
		Navigation: []Item{{ID: "posts", Rename: map[string]string{"ar": "المنشورات"}, Group: "content"}},
		Presets:    []Preset{{Name: "Compact", Appearance: Appearance{Theme: "slate", Density: "compact"}}},
	}
	for _, input := range []Input{
		{Scope: "site", Expected: &zero, Layout: site},
		{Scope: "personal", Expected: &zero, Layout: personal},
	} {
		if rev, err := s.Save(ctx, owner, input); err != nil || rev.Version != 1 {
			t.Fatal(rev, err)
		}
		if _, err := s.Save(ctx, owner, input); !errors.Is(err, db.ErrVersionConflict) {
			t.Fatal("stale layout proposal", err)
		}
	}
	state, err := s.Get(ctx, owner, Choices(nil))
	if err != nil || state.Site.Layout.Home != "posts" || state.Personal.Layout.Home != "dashboard" {
		t.Fatal(state, err)
	}
	if _, err := s.Save(ctx, owner, Input{Scope: "personal", Expected: &state.Personal.Version}); !errors.Is(err, ErrInvalid) {
		t.Fatal("incomplete request cleared the layout", err)
	}
	if rev, err := s.Save(ctx, owner, Input{Scope: "personal", Expected: &state.Personal.Version, Reset: true}); err != nil || rev.Version != 2 || rev.Layout != nil {
		t.Fatal(rev, err)
	}
	state, err = s.Get(ctx, owner, Choices(nil))
	if err != nil || state.Personal.Layout != nil || state.Site.Layout.Home != "posts" || state.Site.Version != 1 {
		t.Fatal(state, err)
	}
}

func TestDashboardLayoutCannotHideSecurityOrNavigateOutsideRegistry(t *testing.T) {
	for _, layout := range []Layout{
		{Home: "https://example.test"},
		{Home: "posts", Navigation: []Item{{ID: "posts", Hidden: true}}},
		{Navigation: []Item{{ID: "security", Hidden: true}}},
		{Navigation: []Item{{ID: "profile", Hidden: true}}},
		{Navigation: []Item{{ID: "layout", Hidden: true}}},
		{Navigation: []Item{{ID: "posts"}, {ID: "posts"}}},
		{Widgets: []Item{{ID: "content", Group: "content"}}},
		{Appearance: Appearance{Theme: "arbitrary"}},
	} {
		if err := Validate(&layout); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid layout was accepted", layout)
		}
	}
	host := extension.NewHost(nil)
	if err := host.Install(dashboardFixture{}); err != nil {
		t.Fatal(err)
	}
	registry := Choices(host)
	if tail := registry.Navigation[len(registry.Navigation)-1]; tail.ID != "extension:example.navigation" || tail.Label["ar"] != "مثال" {
		t.Fatal(tail)
	}
	if tail := registry.Widgets[len(registry.Widgets)-1]; tail.ID != "extension:example.widget" {
		t.Fatal(tail)
	}
	host.Uninstall("example")
	registry = Choices(host)
	if len(registry.Navigation) != len(screens) || len(registry.Widgets) != len(builtinWidgets) {
		t.Fatal("disabled entries remained discoverable", registry)
	}
}

type dashboardFixture struct{}

func (dashboardFixture) Slug() string { return "example" }
func (dashboardFixture) Register(r *extension.Registrar) error {
	label := extension.LocalizedText{"en": "Example", "ar": "مثال"}
	if err := r.Contribution(extension.Contribution{
		Kind: extension.AdminNavigation, Key: "navigation", Version: 1, Target: "overview", Label: label,
	}, nil); err != nil {
		return err
	}
	return r.Contribution(extension.Contribution{
		Kind: extension.Widget, Key: "widget", Version: 1, Target: "extensions", Label: label, Text: label,
	}, nil)
}
