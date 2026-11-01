package siteconfig

import (
	"encoding/json"
	"errors"
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
