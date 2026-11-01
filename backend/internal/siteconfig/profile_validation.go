package siteconfig

import (
	"fmt"
	"strings"
)

// FieldIssue contains only schema paths and reason codes, never submitted values.
type FieldIssue struct {
	Path string `json:"path"`
	Code string `json:"code"`
}
type ValidationError struct{ Fields []FieldIssue }

func (e *ValidationError) Error() string { return ErrInvalid.Error() }
func (e *ValidationError) Unwrap() error { return ErrInvalid }
func invalidField(path, code string) error {
	return &ValidationError{Fields: []FieldIssue{{path, code}}}
}

func profileIssues(p Profile) []FieldIssue {
	issues := []FieldIssue{}
	add := func(path, code string) {
		for _, issue := range issues {
			if issue.Path == path {
				return
			}
		}
		issues = append(issues, FieldIssue{path, code})
	}
	local := func(path string, v Localized, max int) {
		for _, lang := range []string{"en", "fr", "ar"} {
			if !textOK(v[lang], max) {
				add(path+"."+lang, "text")
			}
			if lang != "en" && v[lang] != "" && strings.TrimSpace(v["en"]) == "" {
				add(path+".en", "english_source")
				break
			}
		}
		for lang := range v {
			if lang != "en" && lang != "fr" && lang != "ar" {
				add(path, "language")
			}
		}
	}
	for _, f := range []struct {
		path  string
		value Localized
		max   int
	}{
		{"name", p.Name, 100}, {"headline", p.Headline, 200}, {"bio", p.Bio, 320}, {"sticker", p.Sticker, 40}, {"portrait_alt", p.PortraitAlt, 200}, {"banner_alt", p.BannerAlt, 200}, {"location", p.Location, 120}, {"copyright", p.Copyright, 100}, {"footer", p.Footer, 200},
	} {
		local(f.path, f.value, f.max)
	}
	if len(p.Now) > 3 {
		add("now", "limit")
	}
	if len(p.Links) > 10 {
		add("links", "limit")
	}
	if len(p.Experience) > 20 {
		add("experience", "limit")
	}
	if p.BannerX < 0 || p.BannerX > 100 {
		add("banner_x", "range")
	}
	if p.BannerY < 0 || p.BannerY > 100 {
		add("banner_y", "range")
	}
	if p.WritingLimit < 1 || p.WritingLimit > 20 {
		add("writing_limit", "range")
	}
	if p.LearningLimit < 1 || p.LearningLimit > 20 {
		add("learning_limit", "range")
	}
	if p.UpdatedMonth != "" && !month.MatchString(p.UpdatedMonth) {
		add("updated_month", "month")
	}
	if p.PublicEmail != "" && !email(p.PublicEmail) {
		add("public_email", "email")
	}
	if p.AboutKey != "" && !stableID.MatchString(p.AboutKey) {
		add("about_key", "reference")
	}
	ids := map[string]bool{}
	unique := func(path, id string) {
		if !stableID.MatchString(id) || ids[id] {
			add(path, "id")
		}
		ids[id] = true
	}
	for i, r := range p.Now {
		path := fmt.Sprintf("now.%d", i)
		unique(path+".id", r.ID)
		local(path+".label", r.Label, 80)
		local(path+".text", r.Text, 160)
	}
	link := func(path string, l Link) {
		unique(path+".id", l.ID)
		local(path+".label", l.Label, 80)
		if strings.TrimSpace(l.Label["en"]) == "" {
			add(path+".label.en", "required")
		}
		if !safeURL(l.URL) {
			add(path+".url", "url")
		}
		if l.Icon != "" && !icons[l.Icon] {
			add(path+".icon", "icon")
		}
	}
	for i, l := range p.Links {
		link(fmt.Sprintf("links.%d", i), l)
	}
	if p.MainAction != nil {
		link("main_action", *p.MainAction)
	}
	for i, e := range p.Experience {
		path := fmt.Sprintf("experience.%d", i)
		unique(path+".id", e.ID)
		local(path+".role", e.Role, 120)
		local(path+".organization", e.Organization, 120)
		local(path+".description", e.Description, 500)
		if !month.MatchString(e.Start) {
			add(path+".start", "month")
		}
		if (e.Current && e.End != "") || (!e.Current && (!month.MatchString(e.End) || e.End < e.Start)) {
			add(path+".end", "date_order")
		}
		if e.URL != "" && !safeURL(e.URL) {
			add(path+".url", "url")
		}
	}
	if p.BannerEnabled {
		if p.BannerID == "" {
			add("banner_media_id", "required")
		}
		if strings.TrimSpace(p.BannerAlt["en"]) == "" {
			add("banner_alt.en", "image_alt")
		}
	}
	if p.PortraitID != "" && strings.TrimSpace(p.PortraitAlt["en"]) == "" {
		add("portrait_alt.en", "image_alt")
	}
	return issues
}
