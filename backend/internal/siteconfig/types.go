// Package siteconfig owns public configuration snapshots, never operator secrets.
package siteconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid configuration")
var ErrReview = errors.New("translation review required")

type Localized map[string]string
type Site struct {
	LogoMode  string            `json:"logo_mode"`
	Wordmark  string            `json:"wordmark"`
	Monogram  string            `json:"monogram"`
	LogoID    string            `json:"logo_media_id"`
	FaviconID string            `json:"favicon_media_id"`
	Primary   string            `json:"primary"`
	Secondary string            `json:"secondary"`
	MenuIcons map[string]string `json:"menu_icons"`
}
type NowRow struct {
	ID    string    `json:"id"`
	Label Localized `json:"label"`
	Text  Localized `json:"text"`
}
type Link struct {
	ID      string    `json:"id"`
	Label   Localized `json:"label"`
	URL     string    `json:"url"`
	Icon    string    `json:"icon"`
	Visible bool      `json:"visible"`
}
type Experience struct {
	ID           string    `json:"id"`
	Role         Localized `json:"role"`
	Organization Localized `json:"organization"`
	Description  Localized `json:"description"`
	Start        string    `json:"start"`
	End          string    `json:"end"`
	Current      bool      `json:"current"`
	URL          string    `json:"url"`
}
type Profile struct {
	Name          Localized    `json:"name"`
	Headline      Localized    `json:"headline"`
	Bio           Localized    `json:"bio"`
	Sticker       Localized    `json:"sticker"`
	PortraitID    string       `json:"portrait_media_id"`
	PortraitAlt   Localized    `json:"portrait_alt"`
	BannerID      string       `json:"banner_media_id"`
	BannerAlt     Localized    `json:"banner_alt"`
	BannerEnabled bool         `json:"banner_enabled"`
	BannerX       float64      `json:"banner_x"`
	BannerY       float64      `json:"banner_y"`
	Now           []NowRow     `json:"now"`
	UpdatedMonth  string       `json:"updated_month"`
	Links         []Link       `json:"links"`
	MainAction    *Link        `json:"main_action"`
	Experience    []Experience `json:"experience"`
	PublicEmail   string       `json:"public_email"`
	Location      Localized    `json:"location"`
	Copyright     Localized    `json:"copyright"`
	Footer        Localized    `json:"footer"`
	AboutKey      string       `json:"about_key"`
	ShowNow       bool         `json:"show_now"`
	ShowWriting   bool         `json:"show_writing"`
	ShowLearning  bool         `json:"show_learning"`
	WritingLimit  int          `json:"writing_limit"`
	LearningLimit int          `json:"learning_limit"`
}
type Labels map[string]map[string]json.RawMessage
type Review struct {
	Path       string `json:"path"`
	Locale     string `json:"locale"`
	SourceHash string `json:"source_hash"`
	ValueHash  string `json:"value_hash"`
}
type Requirement struct {
	Review
	Value json.RawMessage `json:"value"`
}
type Input struct {
	ExpectedVersion *int64          `json:"expected_version"`
	Snapshot        json.RawMessage `json:"snapshot"`
	Reviews         []Review        `json:"reviews"`
}
type Document struct {
	Kind       string          `json:"kind"`
	Version    int64           `json:"version"`
	RevisionID string          `json:"revision_id"`
	Snapshot   json.RawMessage `json:"snapshot"`
}
type Preview struct {
	Stale []Requirement `json:"stale_reviews"`
	Document
	Required []Requirement `json:"required_reviews"`
}
type mediaRef struct {
	ID, Purpose string
	Public      bool
}

var hexColour = regexp.MustCompile(`^#[0-9A-F]{6}$`)
var stableID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)
var month = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
var icons = map[string]bool{"search": true, "moon": true, "menu": true, "github": true, "linkedin": true, "rss": true, "lock": true, "link": true, "code": true, "image": true, "video": true}

func digest(b []byte) string    { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if len(b) == 0 || bytes.Equal(bytes.TrimSpace(b), []byte("null")) || d.Decode(v) != nil {
		return ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func textOK(s string, n int) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) <= n && !strings.ContainsAny(s, "<>\x00") && !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' })
}
func localized(v Localized, n int) bool {
	for l, s := range v {
		if (l != "en" && l != "ar" && l != "fr") || !textOK(s, n) || (l != "en" && s != "" && strings.TrimSpace(v["en"]) == "") {
			return false
		}
	}
	return true
}
func safeURL(s string) bool {
	if s == "" || len(s) > 2048 || strings.ContainsAny(s, "\\ \t\r\n") || strings.ContainsFunc(s, unicode.IsControl) {
		return false
	}
	d, e := url.PathUnescape(s)
	if e != nil || strings.ContainsAny(d, "\\") || strings.ContainsFunc(d, unicode.IsControl) || strings.HasPrefix(d, "//") {
		return false
	}
	u, e := url.Parse(s)
	if e != nil || u.User != nil {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return u.Host == ""
	}
	if u.Scheme == "https" {
		return u.Hostname() != ""
	}
	return u.Scheme == "mailto" && email(s[7:])
}
func email(s string) bool {
	return len(s) <= 254 && regexp.MustCompile(`^[A-Za-z0-9.!#$%&'*+/=?^_{}|~-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`).MatchString(s)
}
func defaults(kind string) json.RawMessage {
	switch kind {
	case "site":
		return raw(Site{LogoMode: "wordmark", Wordmark: "Blog", Monogram: "B", Primary: "#2B44C4", Secondary: "#F2B33D", MenuIcons: map[string]string{"home": "menu", "writing": "code", "learning": "code", "docs": "code", "about": "link"}})
	case "profile":
		e := Localized{"en": "", "ar": "", "fr": ""}
		return raw(Profile{Name: e, Headline: e, Bio: e, Sticker: e, PortraitAlt: e, BannerAlt: e, Now: []NowRow{}, Links: []Link{}, Experience: []Experience{}, Location: e, Copyright: e, Footer: e, BannerX: 50, BannerY: 50, ShowNow: true, ShowWriting: true, ShowLearning: true, WritingLimit: 5, LearningLimit: 5})
	case "labels":
		return raw(Labels{})
	}
	return nil
}
func validateSite(s Site) ([]mediaRef, error) {
	if issues := siteIssues(s); len(issues) > 0 {
		return nil, &ValidationError{Fields: issues}
	}
	return []mediaRef{{s.LogoID, "logo", s.LogoMode == "image"}, {s.FaviconID, "favicon", true}}, nil
}
func validateProfile(p Profile) ([]mediaRef, error) {
	if issues := profileIssues(p); len(issues) > 0 {
		return nil, &ValidationError{Fields: issues}
	}
	return []mediaRef{{p.PortraitID, "portrait", true}, {p.BannerID, "banner", p.BannerEnabled}}, nil
}
