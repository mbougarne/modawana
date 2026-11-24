package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/db"
	"github.com/mbougarne/modawana/backend/internal/extension"
	locales "github.com/mbougarne/modawana/backend/internal/locale"
	"github.com/mbougarne/modawana/backend/internal/render"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

var errThemeUnavailable = errors.New("theme view unavailable")

type taxonomyResolver interface {
	PublicTaxonomy(context.Context, string) ([]content.Term, error)
}

// renderThemed renders the public projection resolved for this request.
// The route serves a localized error if rendering fails.
func (s *Server) renderThemed(ctx context.Context, r *http.Request, page content.PageRoute, settings siteconfig.Public, head headData) ([]byte, bool) {
	locale := page.Locale
	label := func(key string) string { return pageLabel(settings, key, locale) }
	current := page.Kind
	switch current {
	case "home":
		current = ""
	case "article":
		current = "writing"
	case "lesson":
		current = "learn"
	case "doc", "collection":
		current = "docs"
		if page.Collection != nil && page.Collection.Kind == "learning" {
			current = "learn"
		}
	}
	appearance := settings.Theme
	if appearance.ID == "" {
		appearance, _ = theme.Lookup(settings.Site.PublicTheme)
	}
	viewKind := page.Kind
	if page.URL == "/"+locale && page.Content != nil && page.Content.Kind == "page" {
		viewKind, current = "home", ""
	}
	appearance = appearance.ForView(viewKind)
	head.ThemeCSS = template.CSS(appearance.CSS)
	navigation := themeNavigation{locale: locale, frontKey: settings.Site.FrontPages[locale], section: current, path: page.URL, pages: settings.MenuPages, label: label,
		localizedLabel: func(key string) (string, string) { return localizedPageLabel(settings, key, locale) }}
	view := theme.Page{
		ThemeID: appearance.ID, Layout: appearance.Layout, BannerURL: appearance.BannerURL, Locale: locale, Direction: head.Direction, SkipLabel: label("foundation.skip"),
		Brand: themeBrand(settings.Site, locale), Nav: navigation.headerLinks(settings.Site.HeaderMenu, false),
		MenuNav: navigation.headerLinks(settings.Site.HeaderMenu, true), NavLabel: label("nav.main"),
		SearchLabel: label("nav.search"), SearchURL: "/" + locale + "/search",
		LanguageLabel: label("foundation.language"), LanguageShort: languageShort(locale),
		Languages: themeLanguages(page, locale, publicLocales(r.Context()), label), MenuLabel: label("nav.menu"),
		Footer: s.themeFooter(settings, navigation),
	}
	view.Footer.Analytics = head.Analytics
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
	if needsReaderIsland(page) {
		if s.addThemeIsland(&head, "reader-learning") && len(settings.Labels) > 0 {
			raw, err := json.Marshal(settings.Labels)
			if err != nil {
				return nil, false
			}
			head.ReaderLabels = template.JS(raw)
		}
	}
	var err error
	switch page.Kind {
	case "article", "page":
		view.Article, err = s.themeReader(ctx, page.Content, settings, &head, label)
	case "home":
		view.Home, head.ExtraCSS, err = s.themeHome(ctx, settings, locale, label)
	case "about":
		view.About, err = s.themeAbout(ctx, settings, locale, &head, label)
	case "writing", "search":
		view.Discovery, err = s.themeDiscovery(ctx, r.URL.Query(), page, label)
	case "learn", "docs", "collection", "doc", "lesson":
		view.Reading, err = s.themeReading(ctx, r.URL.Query(), page, settings, &head, label)
	case "error":
		view.Error, view.HomeLabel = head.Title, label("slot.home")
	default:
		err = errThemeUnavailable
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
	for _, script := range scripts {
		if !slices.Contains(head.Scripts, script) {
			head.Scripts = append(head.Scripts, script)
		}
	}
	for _, style := range styles {
		if !slices.Contains(head.Styles, style) {
			head.Styles = append(append([]string{}, head.Styles...), style)
		}
	}
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
	if result.HasMath {
		s.addThemeIsland(head, "katex")
	}
	return result.HTML, nil
}

func (s *Server) themeReader(ctx context.Context, c *content.PublicContent, settings siteconfig.Public, head *headData, label func(string) string) (*theme.Article, error) {
	if c == nil {
		return nil, errThemeUnavailable
	}
	locale := c.Locale
	if c.Locked {
		if c.Kind != "lesson" {
			return nil, errThemeUnavailable
		}
		return &theme.Article{
			Kind: c.Kind, StickerLabel: label("public.lesson"), Title: c.Title, Summary: c.Summary,
			Locked: themeLocked(c.Learning, label),
		}, nil
	}
	var body template.HTML
	var err error
	if c.Kind == "page" && c.BodyFormat == content.BlockBodyFormat {
		body, err = s.themePageBlocks(ctx, c, head, label)
	} else {
		body, err = s.themeMarkdown(c, locale, head, label)
	}
	if err != nil {
		return nil, err
	}
	if c.Kind == "page" {
		view := &theme.Article{Kind: c.Kind, Title: c.Title, Summary: c.Summary, Body: body, PageTemplate: c.PageTemplate}
		if view.PageTemplate == "" {
			view.PageTemplate = "standard"
		}
		setReaderDetails(view, c, label)
		return view, nil
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
		Kind:         c.Kind,
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
	setReaderDetails(&article, c, label)
	if c.Kind == "lesson" || hasQuizControls(c) {
		article.Learning = &theme.LearningMount{Key: c.Key, Locale: locale, Kind: c.Kind}
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

func setReaderDetails(article *theme.Article, c *content.PublicContent, label func(string) string) {
	if c.Stale {
		article.StaleNotice = label("public.stale")
	}
	for _, m := range c.Media {
		if m.ID == c.CoverID && m.Kind == "image" {
			article.CoverURL = m.URL
			article.CoverWidth, article.CoverHeight = m.Width, m.Height
		}
	}
}

type themeNavigation struct {
	footer                bool
	frontKey              string
	locale, section, path string
	pages                 map[string]siteconfig.MenuPage
	label                 func(string) string
	localizedLabel        func(string) (string, string)
}

func (n themeNavigation) headerLinks(menu *[]siteconfig.MenuItem, home bool) []theme.Link {
	if menu != nil {
		return n.links(*menu)
	}
	items := []siteconfig.MenuItem{{Target: "writing"}, {Target: "learning"}, {Target: "docs"}, {Target: "about"}}
	if home {
		items = append([]siteconfig.MenuItem{{Target: "home"}}, items...)
	}
	return n.links(items)
}

func (n themeNavigation) links(items []siteconfig.MenuItem) []theme.Link {
	out := make([]theme.Link, 0, len(items))
	for _, item := range items {
		previous := len(out)
		switch item.Target {
		case "page":
			if page, ok := n.pages[item.PageKey]; ok {
				destination := content.StandalonePagePath(n.locale, page.Slug)
				if item.PageKey == n.frontKey {
					destination = "/" + n.locale
				}
				out = append(out, theme.Link{Label: page.Title, URL: destination, Language: n.locale, Current: !n.footer && n.path == destination})
			}
		case "custom":
			destination, ok := render.SafeLink(item.URL)
			if !ok {
				continue
			}
			language := n.locale
			text := item.Label[language]
			if strings.TrimSpace(text) == "" {
				language, text = "en", item.Label["en"]
			}
			if strings.TrimSpace(text) != "" {
				out = append(out, theme.Link{Label: text, URL: destination, Language: language})
			}
		case "home", "writing", "learning", "docs", "about":
			section := item.Target
			if section == "learning" {
				section = "learn"
			} else if section == "home" {
				section = ""
			}
			destination := "/" + n.locale
			if section != "" {
				destination += "/" + section
			}
			key := "slot." + item.Target
			text, language := n.label(key), ""
			if n.localizedLabel != nil {
				var actual string
				text, actual = n.localizedLabel(key)
				if actual != n.locale {
					language = actual
				}
			}
			out = append(out, theme.Link{Label: text, Language: language, URL: destination, Current: !n.footer && n.section == section})
		}
		if len(out) > previous && len(item.Children) > 0 {
			out[previous].Children = n.links(item.Children)
		}
	}
	return out
}

// themeLanguages mirrors the SPA language menu without JavaScript: published
// translations link to the alternative, and missing translations offer the
// explicit home page in that site language, never silently falling back.
func themeLanguages(page content.PageRoute, locale string, availableLocales []string, label func(string) string) []theme.Language {
	out := []theme.Language{}
	for _, l := range availableLocales {
		dir := locales.Direction(l)
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

func (s *Server) themeFooter(settings siteconfig.Public, navigation themeNavigation) theme.Footer {
	locale, label := navigation.locale, navigation.label
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
	}
	for _, projection := range s.extensions().Contributions(extension.PublicProjection) {
		footer.Projections = append(footer.Projections, projection.Text.For(locale))
	}
	items := []siteconfig.MenuItem{{Target: "writing"}, {Target: "learning"}, {Target: "docs"}}
	if settings.Site.FooterMenu != nil {
		items = *settings.Site.FooterMenu
	}
	navigation.footer = true
	footer.Links = navigation.links(items)
	candidates := settings.Profile.Links
	if settings.Profile.MainAction != nil {
		candidates = append([]siteconfig.Link{*settings.Profile.MainAction}, candidates...)
	}
	for _, icon := range []string{"github", "linkedin", "rss"} {
		var destination string
		for _, link := range candidates {
			safe, ok := render.SafeLink(link.URL)
			if link.Icon == icon && link.Visible && ok {
				destination = safe
				break
			}
		}
		if destination == "" && icon == "rss" && s.syndicationSource() != nil {
			destination = feedPath(locale)
		}
		if destination != "" {
			footer.Links = append(footer.Links, theme.Link{Label: label("icon." + icon), URL: destination})
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
		"es": {"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"},
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
