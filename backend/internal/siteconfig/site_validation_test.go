package siteconfig

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func TestSettingsValidationExplainsExactFields(t *testing.T) {
	cases := []struct {
		path, code string
		change     func(*Site)
	}{
		{"wordmark", "wordmark", func(s *Site) { s.Wordmark = "" }},
		{"monogram", "monogram", func(s *Site) { s.Monogram = "four" }},
		{"primary", "colour", func(s *Site) { s.Primary = "blue" }},
		{"secondary", "colour", func(s *Site) { s.Secondary = "#BAD" }},
		{"logo_mode", "logo_mode", func(s *Site) { s.LogoMode = "unknown" }},
		{"logo_media_id", "logo_image", func(s *Site) { s.LogoMode = "image"; s.LogoID = "" }},
		{"menu_icons.home", "icon", func(s *Site) { s.MenuIcons["home"] = "unknown" }},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			var s Site
			json.Unmarshal(defaults("site"), &s)
			tc.change(&s)
			_, err := validateSite(s)
			var v *ValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &v) {
				t.Fatalf("missing issue: %v", err)
			}
			for _, f := range v.Fields {
				if f.Path == tc.path && f.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s/%s: %+v", tc.path, tc.code, v.Fields)
		})
	}
}

func TestPublicMenuValidation(t *testing.T) {
	valid := MenuItem{ID: "contact", Target: "custom", Label: Localized{"en": "Contact"}, URL: "/en/contact"}
	for _, destination := range []string{"javascript:alert(1)", "//example.com", "/%2fexample.com", "https://user@example.com", "/%FF", "http://example.com"} {
		item := valid
		item.URL = destination
		want := []FieldIssue{{"header_menu.0.url", "url"}}
		if got := menuIssues("header_menu", []MenuItem{item}); !slices.Equal(got, want) {
			t.Errorf("%q: issues = %v, want %v", destination, got, want)
		}
	}
	items := []MenuItem{
		valid,
		{ID: valid.ID, Target: "custom", URL: "https://example.com", Label: Localized{"fr": "Contact"}},
		{ID: "invalid-target", Target: "search"},
		{ID: "first-home", Target: "home"},
		{ID: "second-home", Target: "home"},
	}
	want := []FieldIssue{
		{"footer_menu.1.id", "id"}, {"footer_menu.1.label.en", "required"},
		{"footer_menu.2.target", "menu_target"}, {"footer_menu.4.target", "menu_target"},
	}
	if got := menuIssues("footer_menu", items); !slices.Equal(got, want) {
		t.Fatalf("issues = %v, want %v", got, want)
	}
	if got := menuIssues("footer_menu", make([]MenuItem, 13)); !slices.Equal(got, []FieldIssue{{"footer_menu", "limit"}}) {
		t.Fatalf("expected menu length error, got %v", got)
	}
}
