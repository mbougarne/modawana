package httpserver

import (
	"bytes"
	"context"
	"html/template"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/render"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

// themedKinds is the ADR 0002 per-route switch: only resolved page kinds
// listed here render through the built-in theme; everything else keeps the
// SPA shell. BLOG_FORCE_SPA=1 is the operator rollback override.
var themedKinds = map[string]bool{"article": true}

type taxonomyResolver interface {
	PublicTaxonomy(context.Context, string) ([]content.Term, error)
}

// renderThemed assembles the safe article view model and renders the built-in
// theme. It returns ok=false — and the caller serves today's SPA shell — when
// anything needed is unavailable: assembly stays fail-open for delivery while
// access decisions already happened in ResolvePage.
func (s *Server) renderThemed(ctx context.Context, page content.PageRoute, settings siteconfig.Public, head headData) ([]byte, bool) {
	c := page.Content
	// Locked content cannot reach the themed path: only lessons receive the
	// PublicTeaser visibility (access.Live), route() runs this branch at
	// status 200 only, and locked pages resolve to 403. Locked teasers stay
	// on the SPA shell until the lesson stage renders them server-side.
	if c == nil || page.Kind != "article" || c.Locked {
		return nil, false
	}
	locale := page.Locale
	label := func(key string) string { return pageLabel(settings, key, locale) }
	var islandScripts, islandStyles []string
	media := make([]render.Media, 0, len(c.Media))
	for _, m := range c.Media {
		media = append(media, render.Media{ID: m.ID, Kind: m.Kind, Width: m.Width, Height: m.Height})
	}
	presentations := make([]render.Presentation, 0, len(c.Presentations))
	for _, p := range c.Presentations {
		presentations = append(presentations, render.Presentation{MediaID: p.MediaID, PosterID: p.PosterID, TrackID: p.TrackID, Caption: p.Caption, Transcript: p.Transcript})
	}
	result, err := render.Document(c.Markdown, "", render.Options{
		Locale:        locale,
		Media:         media,
		Presentations: presentations,
		Labels: render.Labels{
			Takeaway:         label("reader.takeaway"),
			Table:            label("reader.table"),
			MediaUnavailable: label("reader.mediaUnavailable"),
			Captions:         label("reader.captions"),
			Transcript:       label("reader.transcript"),
		},
	})
	if err != nil {
		s.options.Logger.Error("theme render fallback", "reason", "markdown", "kind", page.Kind)
		return nil, false
	}
	body := result.HTML
	if result.HasMath {
		var ok bool
		// Only pages that contain math ship rendering JavaScript; without
		// a built katex island the SPA keeps rendering this page.
		islandScripts, islandStyles, ok = s.options.Assets.Island("katex")
		if !ok {
			s.options.Logger.Error("theme render fallback", "reason", "missing katex island")
			return nil, false
		}
	}
	var category *content.Term
	tags := []theme.Link{}
	if c.CategoryKey != "" || len(c.Tags) > 0 {
		resolver, ok := s.options.Domain.(taxonomyResolver)
		if !ok {
			return nil, false
		}
		terms, err := resolver.PublicTaxonomy(ctx, locale)
		if err != nil {
			s.options.Logger.Error("theme render fallback", "reason", "taxonomy")
			return nil, false
		}
		for i, term := range terms {
			if term.Kind == "category" && term.Key == c.CategoryKey {
				category = &terms[i]
			}
		}
		for _, key := range c.Tags {
			for _, term := range terms {
				if term.Kind == "tag" && term.Key == key {
					tags = append(tags, theme.Link{Label: term.Label, URL: "/" + locale + "/writing?tag=" + url.QueryEscape(term.Slug)})
				}
			}
		}
	}
	head.Boot = ""
	head.Scripts = islandScripts
	head.Styles = append(append([]string{}, head.Styles...), islandStyles...)
	var headHTML bytes.Buffer
	if err := pageHead.Execute(&headHTML, head); err != nil {
		return nil, false
	}
	sticker := label("public." + c.Kind)
	categoryURL := ""
	if category != nil {
		sticker = category.Label + " · " + sticker
		categoryURL = "/" + locale + "/writing?category=" + url.QueryEscape(category.Slug)
	}
	authorName := settings.Effective["name"].Text
	if authorName == "" {
		authorName = settings.Site.Wordmark
	}
	portraitURL := ""
	if settings.Profile.PortraitID != "" {
		portraitURL = "/media/" + settings.Profile.PortraitID
	}
	article := theme.Article{
		StickerLabel: sticker,
		CategoryURL:  categoryURL,
		Title:        c.Title,
		Summary:      c.Summary,
		AuthorName:   authorName,
		PortraitURL:  portraitURL,
		Monogram:     settings.Site.Monogram,
		DateISO:      c.Published,
		DateText:     formatPublicDate(c.Published, locale),
		Body:         body,
		Tags:         tags,
	}
	if c.ReadingMinutes > 0 {
		// public.readTime is a single-form string in every registry locale
		// (the plural-form labels — writing.count, search.results,
		// post.minutes, quiz.passWith — are not used here), so plain {n}
		// interpolation matches the SPA. pageLabel skips a plural-object
		// owner override (string unmarshal fails) and falls through to the
		// registry form; introducing plural variants for a themed label
		// means porting the SPA's plural-rule selection first.
		article.ReadingEstimate = interpolate(label("public.readTime"), map[string]string{"n": strconv.Itoa(c.ReadingMinutes)})
	}
	if c.Stale {
		article.StaleNotice = label("public.stale")
	}
	for _, m := range c.Media {
		if m.ID == c.CoverID && m.Kind == "image" {
			article.CoverURL = m.URL
			article.CoverWidth, article.CoverHeight = m.Width, m.Height
		}
	}
	if authorName != "" {
		arrow := "→"
		if locale == "ar" {
			arrow = "←"
		}
		article.WrittenBy = interpolate(label("post.writtenBy"), map[string]string{"name": authorName})
		article.Bio = bioSegments(settings.Effective["bio"].Text)
		article.MoreLabel = label("post.moreWriting")
		article.MoreURL = "/" + locale + "/writing"
		article.Arrow = arrow
	}
	view := theme.ArticlePage{
		Locale:        locale,
		Direction:     head.Direction,
		Head:          template.HTML(headHTML.String()),
		SkipLabel:     label("foundation.skip"),
		Brand:         themeBrand(settings.Site, locale),
		Nav:           themeNav(locale, "writing", false, label),
		MenuNav:       themeNav(locale, "writing", true, label),
		NavLabel:      label("nav.main"),
		SearchLabel:   label("nav.search"),
		SearchURL:     "/" + locale + "/search",
		LanguageLabel: label("foundation.language"),
		LanguageShort: languageShort(locale),
		Languages:     themeLanguages(page, locale, label),
		MenuLabel:     label("nav.menu"),
		Article:       article,
		Footer:        s.themeFooter(settings, locale, label),
	}
	var out bytes.Buffer
	if err := theme.RenderArticle(&out, view); err != nil {
		s.options.Logger.Error("theme render fallback", "reason", "template")
		return nil, false
	}
	return out.Bytes(), true
}

func themeNav(locale, current string, home bool, label func(string) string) []theme.Link {
	sections := []struct{ path, key string }{{"writing", "slot.writing"}, {"learn", "slot.learning"}, {"docs", "slot.docs"}, {"about", "slot.about"}}
	out := []theme.Link{}
	if home {
		out = append(out, theme.Link{Label: label("slot.home"), URL: "/" + locale, Current: current == ""})
	}
	for _, s := range sections {
		out = append(out, theme.Link{Label: label(s.key), URL: "/" + locale + "/" + s.path, Current: current == s.path})
	}
	return out
}

// themeLanguages mirrors the SPA language menu without JavaScript: published
// translations link to the alternative, and missing translations offer the
// explicit home page in that site language, never silently falling back.
func themeLanguages(page content.PageRoute, locale string, label func(string) string) []theme.Language {
	out := []theme.Language{}
	for _, l := range []string{"en", "ar", "fr"} {
		dir := "ltr"
		if l == "ar" {
			dir = "rtl"
		}
		entry := theme.Language{Code: l, Dir: dir, Current: l == locale}
		found := false
		for _, a := range page.Alternatives {
			if a.Locale == l {
				entry.Label = label("language." + l)
				entry.URL = a.URL
				found = true
			}
		}
		if !found {
			entry.Label = interpolate(label("public.homeLanguage"), map[string]string{"language": label("language." + l)})
			entry.URL = "/" + l
		}
		out = append(out, entry)
	}
	return out
}

func themeBrand(site siteconfig.Site, locale string) theme.Brand {
	brand := theme.Brand{HomeURL: "/" + locale}
	switch {
	case site.LogoMode == "image" && site.LogoID != "":
		brand.Kind = "image"
		brand.ImageURL = "/media/" + site.LogoID
		brand.ImageAlt = site.Wordmark
	case site.LogoMode == "monogram":
		brand.Kind = "monogram"
		brand.Monogram = site.Monogram
	default:
		brand.Kind = "wordmark"
		if i := strings.LastIndex(site.Wordmark, "."); i > 0 {
			brand.Split = true
			brand.Pre = site.Wordmark[:i]
			brand.Suf = site.Wordmark[i+1:]
		} else {
			brand.Text = site.Wordmark
		}
	}
	return brand
}

func (s *Server) themeFooter(settings siteconfig.Public, locale string, label func(string) string) theme.Footer {
	pick := func(v siteconfig.Localized) string {
		if v[locale] != "" {
			return v[locale]
		}
		return v["en"]
	}
	footer := theme.Footer{
		Text:      pick(settings.Profile.Footer),
		Question:  label("footer.question"),
		SayHi:     label("footer.sayhi"),
		Email:     settings.Profile.PublicEmail,
		Copyright: pick(settings.Profile.Copyright),
		NavLabel:  label("footer.links"),
		Links: []theme.Link{
			{Label: label("slot.writing"), URL: "/" + locale + "/writing"},
			{Label: label("slot.learning"), URL: "/" + locale + "/learn"},
			{Label: label("slot.docs"), URL: "/" + locale + "/docs"},
		},
	}
	candidates := settings.Profile.Links
	if settings.Profile.MainAction != nil {
		candidates = append([]siteconfig.Link{*settings.Profile.MainAction}, candidates...)
	}
	for _, icon := range []string{"github", "linkedin", "rss"} {
		for _, link := range candidates {
			safe, ok := render.SafeLink(link.URL)
			if link.Icon == icon && link.Visible && ok {
				footer.Links = append(footer.Links, theme.Link{Label: label("icon." + icon), URL: safe})
				break
			}
		}
	}
	return footer
}

func languageShort(locale string) string {
	if locale == "ar" {
		return "ع"
	}
	return strings.ToUpper(locale)
}

func interpolate(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

// formatPublicDate mirrors the SPA's Intl formatting: day month year with
// Latin digits, Moroccan Arabic month names (ui/src/i18n/locales.ts).
func formatPublicDate(iso, locale string) string {
	var months = map[string][12]string{
		"en": {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		"fr": {"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
		"ar": {"يناير", "فبراير", "مارس", "أبريل", "ماي", "يونيو", "يوليوز", "غشت", "شتنبر", "أكتوبر", "نونبر", "دجنبر"},
	}
	names, ok := months[locale]
	if !ok {
		names = months["en"]
	}
	t, err := time.Parse(db.TimestampLayout, iso)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, iso); err != nil {
			if t, err = time.Parse("2006-01-02", iso); err != nil {
				return iso
			}
		}
	}
	return strconv.Itoa(t.Day()) + " " + names[t.Month()-1] + " " + strconv.Itoa(t.Year())
}

var bioBold = regexp.MustCompile(`\*\*[^*]+\*\*`)

// bioSegments parses the profile bio's **bold** convention (ui renderBio).
func bioSegments(text string) []theme.BioSegment {
	out := []theme.BioSegment{}
	last := 0
	for _, match := range bioBold.FindAllStringIndex(text, -1) {
		if match[0] > last {
			out = append(out, theme.BioSegment{Text: text[last:match[0]]})
		}
		out = append(out, theme.BioSegment{Text: text[match[0]+2 : match[1]-2], Bold: true})
		last = match[1]
	}
	if last < len(text) {
		out = append(out, theme.BioSegment{Text: text[last:]})
	}
	return out
}
