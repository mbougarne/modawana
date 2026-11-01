package siteconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type Effective struct {
	Text     string `json:"text"`
	Locale   string `json:"locale"`
	Fallback bool   `json:"fallback"`
	Stale    bool   `json:"stale"`
}
type Public struct {
	Palette   Palette              `json:"palette"`
	Site      Site                 `json:"site"`
	Profile   Profile              `json:"profile"`
	Effective map[string]Effective `json:"effective"`
	Labels    Labels               `json:"labels"`
	AboutURL  string               `json:"about_url"`
	Versions  map[string]int64     `json:"versions"`
}

func (s *Service) Public(ctx context.Context, locale string) (Public, error) {
	out := Public{Versions: map[string]int64{}, Effective: map[string]Effective{}}
	if locale != "en" && locale != "ar" && locale != "fr" {
		return out, ErrInvalid
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	a, e := get(ctx, tx, "site")
	if e != nil {
		return out, e
	}
	p, e := get(ctx, tx, "profile")
	if e != nil {
		return out, e
	}
	l, e := get(ctx, tx, "labels")
	if e != nil {
		return out, e
	}
	_ = json.Unmarshal(a.Snapshot, &out.Site)
	_ = json.Unmarshal(p.Snapshot, &out.Profile)
	_ = json.Unmarshal(l.Snapshot, &out.Labels)
	out.Palette, e = Derive(out.Site.Primary, out.Site.Secondary)
	if e != nil {
		return out, e
	}
	out.Versions = map[string]int64{"site": a.Version, "profile": p.Version, "labels": l.Version}
	old, e := reviews(ctx, tx, p.RevisionID)
	if e != nil {
		return out, e
	}
	if out.Site.LogoMode != "image" {
		out.Site.LogoID = ""
	}
	if !out.Profile.BannerEnabled {
		out.Profile.BannerID = ""
		out.Profile.BannerAlt = Localized{}
	}
	visible := []Link{}
	for _, l := range out.Profile.Links {
		if l.Visible {
			visible = append(visible, l)
		}
	}
	out.Profile.Links = visible
	if out.Profile.MainAction != nil && !out.Profile.MainAction.Visible {
		out.Profile.MainAction = nil
	}
	if !out.Profile.ShowNow {
		out.Profile.Now = []NowRow{}
	}
	if out.Profile.AboutKey != "" {
		var slug string
		err := tx.QueryRowContext(ctx, `SELECT v.slug FROM content_items i JOIN content_versions v ON v.item_id=i.id WHERE i.key=? AND i.kind='doc' AND v.locale=? AND v.published_revision_id IS NOT NULL`, out.Profile.AboutKey, locale).Scan(&slug)
		if err == nil {
			out.AboutURL = "/api/v1/public/content/" + locale + "/" + slug
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		out.Profile.AboutKey = ""
	}
	for path, v := range translations("profile", raw(out.Profile)) {
		en, text := "", ""
		_ = json.Unmarshal(v["en"], &en)
		_ = json.Unmarshal(v[locale], &text)
		actual := locale
		if text == "" {
			text = en
			actual = "en"
		}
		r := old[path+"/"+locale]
		out.Effective[path] = Effective{text, actual, actual != locale, r.SourceHash != "" && r.SourceHash != digest(v["en"])}
	}
	return out, nil
}
