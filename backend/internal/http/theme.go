package httpserver

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"net/http"
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
var themedKinds = map[string]bool{"article": true, "home": true, "about": true, "writing": true, "search": true}

var errThemeUnavailable = errors.New("theme view unavailable")

type taxonomyResolver interface {
	PublicTaxonomy(context.Context, string) ([]content.Term, error)
}

// renderThemed renders the public projection resolved for this request.
// Returning false leaves the route available through the React fallback.
func (s *Server) renderThemed(ctx context.Context, r *http.Request, page content.PageRoute, settings siteconfig.Public, head headData) ([]byte, bool) {
	locale := page.Locale
	label := func(key string) string { return pageLabel(settings, key, locale) }
	current := page.Kind
	switch current {
	case "home":
		current = ""
	case "article":
		current = "writing"
	}
	view := theme.Page{
		Locale: locale, Direction: head.Direction, SkipLabel: label("foundation.skip"),
		Brand: themeBrand(settings.Site, locale), Nav: themeNav(locale, current, false, label),
		MenuNav: themeNav(locale, current, true, label), NavLabel: label("nav.main"),
		SearchLabel: label("nav.search"), SearchURL: "/" + locale + "/search",
		LanguageLabel: label("foundation.language"), LanguageShort: languageShort(locale),
		Languages: themeLanguages(page, locale, label), MenuLabel: label("nav.menu"),
		Footer: s.themeFooter(settings, locale, label),
	}
	head.Boot = ""
	head.Scripts = nil
	if page.Kind != "article" {
		if s.addThemeIsland(&head, "public-controls") {
			view.ThemeLabel = label("nav.theme")
		}
		if guard, ok := s.options.Auth.(interface{ Authorized(*http.Request) bool }); ok && guard.Authorized(r) {
			view.Footer.Links = append(view.Footer.Links, theme.Link{Label: label("public.owner"), URL: "/" + locale + "/admin"})
		}
	}
	var err error
	switch page.Kind {
	case "article":
		view.Article, err = s.themeArticle(ctx, page, settings, &head, label)
	case "home":
		view.Home, head.ExtraCSS, err = s.themeHome(ctx, settings, locale, label)
	case "about":
		view.About, err = s.themeAbout(ctx, settings, locale, &head, label)
	case "writing", "search":
		view.Discovery, err = s.themeDiscovery(ctx, r.URL.Query(), page, label)
	}
	if err != nil {
		s.options.Logger.Error("theme view unavailable", "kind", page.Kind)
		return nil, false
	}
	var headHTML, out bytes.Buffer
	if err := pageHead.Execute(&headHTML, head); err != nil {
		return nil, false
	}
	view.Head = template.HTML(headHTML.String())
	if err := theme.Render(&out, view); err != nil {
		s.options.Logger.Error("theme render fallback", "reason", "template", "kind", page.Kind)
		return nil, false
	}
	return out.Bytes(), true
}

func (s *Server) addThemeIsland(head *headData, name string) bool {
	scripts, styles, ok := s.options.Assets.Island(name)
	if !ok || len(scripts) == 0 {
		return false
	}
	head.Scripts = append(head.Scripts, scripts...)
	head.Styles = append(append([]string{}, head.Styles...), styles...)
	return true
}

func (s *Server) themeMarkdown(c *content.PublicContent, locale string, head *headData, label func(string) string) (template.HTML, error) {
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
		return "", err
	}
	if result.HasMath && !s.addThemeIsland(head, "katex") {
		return "", errThemeUnavailable
	}
	return result.HTML, nil
}

func (s *Server) themeArticle(ctx context.Context, page content.PageRoute, settings siteconfig.Public, head *headData, label func(string) string) (*theme.Article, error) {
	c := page.Content
	if c == nil || c.Locked {
		return nil, errThemeUnavailable
	}
	locale := page.Locale
	body, err := s.themeMarkdown(c, locale, head, label)
	if err != nil {
		return nil, err
	}
	var category *content.Term
	tags := []theme.Link{}
	if c.CategoryKey != "" || len(c.Tags) > 0 {
		terms, err := s.themeTerms(ctx, locale)
		if err != nil {
			return nil, err
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
		article.WrittenBy = interpolate(label("post.writtenBy"), map[string]string{"name": authorName})
		article.Bio = bioSegments(settings.Effective["bio"].Text)
		article.MoreLabel = label("post.moreWriting")
		article.MoreURL = "/" + locale + "/writing"
		article.Arrow = themeArrow(locale)
	}
	return &article, nil
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

func formatPublicMonth(date time.Time, locale string) string {
	switch locale {
	case "fr":
		return [...]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}[date.Month()-1] + " " + strconv.Itoa(date.Year())
	case "ar":
		return strings.TrimPrefix(formatPublicDate(date.Format("2006-01-02"), locale), strconv.Itoa(date.Day())+" ")
	default:
		return date.Format("January 2006")
	}
}

var bioBold = regexp.MustCompile(`\*\*[^*]+\*\*`)

// bioSegments parses the profile bio's **bold** convention (ui renderBio).
func bioSegments(text string) []theme.BioSegment {
	return markedSegments(text, bioBold, 2)
}

func markedSegments(text string, pattern *regexp.Regexp, marks int) []theme.BioSegment {
	var out []theme.BioSegment
	last := 0
	for _, match := range pattern.FindAllStringIndex(text, -1) {
		if match[0] > last {
			out = append(out, theme.BioSegment{Text: text[last:match[0]]})
		}
		out = append(out, theme.BioSegment{Text: text[match[0]+marks : match[1]-marks], Bold: true})
		last = match[1]
	}
	if last < len(text) {
		out = append(out, theme.BioSegment{Text: text[last:]})
	}
	return out
}
