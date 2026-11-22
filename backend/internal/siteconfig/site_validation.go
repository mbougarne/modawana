package siteconfig

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/mbougarne/modawana/backend/internal/theme"
)

func siteIssues(s Site) []FieldIssue {
	issues := []FieldIssue{}
	add := func(path, code string) { issues = append(issues, FieldIssue{path, code}) }
	if _, ok := theme.Lookup(s.PublicTheme); !ok && !theme.IsPack(s.PublicTheme) {
		add("public_theme", "public_theme")
	}
	if s.LogoMode != "wordmark" && s.LogoMode != "monogram" && s.LogoMode != "image" {
		add("logo_mode", "logo_mode")
	}
	if !textOK(s.Wordmark, 80) || strings.TrimSpace(s.Wordmark) == "" {
		add("wordmark", "wordmark")
	}
	clusters := 0
	valid := true
	for _, r := range s.Monogram {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			clusters++
		} else if !unicode.IsMark(r) || clusters == 0 {
			valid = false
		}
	}
	if !valid || clusters < 1 || clusters > 3 || len(s.Monogram) > 64 {
		add("monogram", "monogram")
	}
	for _, colour := range []struct{ path, value string }{{"primary", s.Primary}, {"secondary", s.Secondary}} {
		if !hexColour.MatchString(colour.value) {
			add(colour.path, "colour")
		}
	}
	if hexColour.MatchString(s.Primary) && hexColour.MatchString(s.Secondary) {
		if _, err := Derive(s.Primary, s.Secondary); err != nil {
			add("primary", "contrast")
			add("secondary", "contrast")
		}
	}
	for slot, icon := range s.MenuIcons {
		if !strings.Contains("|home|writing|learning|docs|about|", "|"+slot+"|") || !icons[icon] {
			add("menu_icons."+slot, "icon")
		}
	}
	if s.LogoMode == "image" && s.LogoID == "" {
		add("logo_media_id", "logo_image")
	}
	return issues
}

func (s *Service) validateTheme(ctx context.Context, q theme.Query, kind string, snapshot json.RawMessage) error {
	if kind != "site" {
		return nil
	}
	var site Site
	if err := json.Unmarshal(snapshot, &site); err != nil {
		return err
	}
	_, ok, err := s.Themes.Appearance(ctx, q, site.PublicTheme)
	if err != nil {
		return err
	}
	if !ok {
		return invalidField("public_theme", "public_theme")
	}
	return nil
}
