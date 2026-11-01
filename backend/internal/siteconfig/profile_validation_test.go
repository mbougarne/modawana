package siteconfig

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestProfileOptionalFieldsAndConditionalIssues(t *testing.T) {
	var empty Profile
	if err := json.Unmarshal(defaults("profile"), &empty); err != nil {
		t.Fatal(err)
	}
	if _, err := validateProfile(empty); err != nil {
		t.Fatalf("empty optional profile rejected: %v", err)
	}
	cases := []struct {
		name, path, code string
		change           func(*Profile)
	}{
		{"translation", "name.en", "english_source", func(p *Profile) { p.Name = Localized{"ar": "نور"} }},
		{"portrait alt", "portrait_alt.en", "image_alt", func(p *Profile) { p.PortraitID = "image" }},
		{"banner", "banner_media_id", "required", func(p *Profile) { p.BannerEnabled = true }},
		{"link label", "links.0.label.en", "required", func(p *Profile) { p.Links = []Link{{ID: "link", URL: "https://example.test"}} }},
		{"link url", "links.0.url", "url", func(p *Profile) { p.Links = []Link{{ID: "link", Label: Localized{"en": "Link"}, URL: "example.test"}} }},
		{"experience", "experience.0.start", "month", func(p *Profile) { p.Experience = []Experience{{ID: "job", Current: true}} }},
		{"end", "experience.0.end", "date_order", func(p *Profile) { p.Experience = []Experience{{ID: "job", Start: "2026-10", End: "2026-09"}} }},
		{"text", "bio.en", "text", func(p *Profile) { p.Bio = Localized{"en": "<script>"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p Profile
			json.Unmarshal(defaults("profile"), &p)
			tc.change(&p)
			_, err := validateProfile(p)
			var validation *ValidationError
			if !errors.Is(err, ErrInvalid) || !errors.As(err, &validation) {
				t.Fatalf("want structured invalid: %v", err)
			}
			for _, issue := range validation.Fields {
				if issue.Path == tc.path && issue.Code == tc.code {
					return
				}
			}
			t.Fatalf("missing %s/%s: %+v", tc.path, tc.code, validation.Fields)
		})
	}
}
func TestProfileFailedPreviewAndApplyPreserveSnapshot(t *testing.T) {
	s, owner := setup(t)
	ctx := context.Background()
	before, err := s.Get(ctx, "profile")
	if err != nil {
		t.Fatal(err)
	}
	var p Profile
	json.Unmarshal(before.Snapshot, &p)
	p.PortraitID = "missing"
	p.Name = Localized{"fr": "Profil"}
	in := Input{ExpectedVersion: num(before.Version), Snapshot: raw(p)}
	for _, apply := range []bool{false, true} {
		var err error
		if apply {
			_, err = s.Apply(ctx, owner, "profile", in)
		} else {
			_, err = s.Preview(ctx, "profile", in)
		}
		var v *ValidationError
		if !errors.As(err, &v) || len(v.Fields) < 2 {
			t.Fatalf("missing actionable issues: %v", err)
		}
	}
	after, _ := s.Get(ctx, "profile")
	if after.Version != before.Version || string(after.Snapshot) != string(before.Snapshot) {
		t.Fatal("invalid profile changed saved state")
	}
	p.Name = Localized{}
	p.PortraitAlt = Localized{"en": "Portrait"}
	in.Snapshot = raw(p)
	_, err = s.Preview(ctx, "profile", in)
	var v *ValidationError
	if !errors.As(err, &v) || v.Fields[0].Path != "portrait_media_id" {
		t.Fatalf("missing media issue: %v", err)
	}
}
