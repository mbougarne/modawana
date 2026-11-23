package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/render"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

var headlineEmphasis = regexp.MustCompile(`\*[^*]+\*`)

func profileText(text siteconfig.Localized, locale string) theme.Text {
	if text[locale] == "" {
		locale = "en"
	}
	dir := "ltr"
	if locale == "ar" {
		dir = "rtl"
	}
	return theme.Text{Value: text[locale], Lang: locale, Dir: dir}
}

func themePortrait(p siteconfig.Profile, site siteconfig.Site, locale, fallback string) theme.Portrait {
	alt := profileText(p.PortraitAlt, locale).Value
	if alt == "" {
		alt = profileText(p.Name, locale).Value
	}
	if alt == "" {
		alt = fallback
	}
	portrait := theme.Portrait{Alt: alt, Monogram: site.Monogram, Sticker: profileText(p.Sticker, locale), RTL: locale == "ar"}
	if p.PortraitID != "" {
		portrait.URL = "/media/" + p.PortraitID
	}
	return portrait
}

func themeActions(p siteconfig.Profile, locale string) []theme.Action {
	var actions []theme.Action
	add := func(link siteconfig.Link, variant string) {
		text := profileText(link.Label, locale)
		if safe, ok := render.SafeLink(link.URL); ok && link.Visible && text.Value != "" {
			actions = append(actions, theme.Action{Text: text, URL: safe, Icon: link.Icon, Variant: variant})
		}
	}
	if p.MainAction != nil {
		add(*p.MainAction, "primary")
	}
	for _, link := range p.Links {
		variant := "secondary"
		if link.Icon == "rss" {
			variant = "quiet public-rss"
		}
		add(link, variant)
	}
	return actions
}

func (s *Server) themeHome(ctx context.Context, settings siteconfig.Public, locale string, label func(string) string) (*theme.Home, template.CSS, error) {
	p := settings.Profile
	view := &theme.Home{
		Heading: profileText(p.Headline, locale), Bio: profileText(p.Bio, locale),
		Actions: themeActions(p, locale), NowTitle: label("home.now"),
		MoreLabel: label("home.everything"), Arrow: themeArrow(locale), Empty: label("public.empty"),
	}
	view.BioSegments = bioSegments(view.Bio.Value)
	if view.Heading.Value == "" {
		view.Heading = profileText(siteconfig.Localized{locale: label("page.home")}, locale)
	}
	for _, line := range strings.Split(view.Heading.Value, "\n") {
		view.Headline = append(view.Headline, markedSegments(line, headlineEmphasis, 1))
	}
	if p.PortraitID != "" || profileText(p.Name, locale).Value != "" {
		portrait := themePortrait(p, settings.Site, locale, label("config.portrait"))
		view.Portrait = &portrait
	}
	var css template.CSS
	if p.BannerEnabled && p.BannerID != "" {
		view.Banner = &theme.Banner{URL: "/media/" + p.BannerID, Alt: profileText(p.BannerAlt, locale).Value}
		css = template.CSS(fmt.Sprintf("#public-profile-banner{object-position:%g%% %g%%}", max(0, min(100, p.BannerX)), max(0, min(100, p.BannerY))))
	}
	if p.ShowNow {
		for _, row := range p.Now {
			if value := profileText(row.Text, locale); value.Value != "" {
				view.Now = append(view.Now, theme.NowRow{Label: profileText(row.Label, locale), Text: value})
			}
		}
		if date, err := time.Parse("2006-01", p.UpdatedMonth); err == nil {
			view.UpdatedMonth = p.UpdatedMonth
			view.UpdatedText = interpolate(label("public.updated"), map[string]string{"date": formatPublicMonth(date, locale)})
		}
	}
	if p.ShowWriting {
		discovery, ok := s.options.Domain.(discoveryResolver)
		if !ok || p.WritingLimit < 1 {
			return nil, "", errThemeUnavailable
		}
		result, err := discovery.PublicDiscovery(ctx, content.Filter{Locale: locale, Limit: p.WritingLimit, ExcludePages: true})
		if err != nil {
			return nil, "", err
		}
		terms, err := s.themeTerms(ctx, locale)
		if err != nil {
			return nil, "", err
		}
		view.WritingTitle, view.WritingURL = label("page.writing"), "/"+locale+"/writing"
		view.Posts = themePosts(result.Items, terms, locale, label)
	}
	if p.ShowLearning {
		collections, ok := s.options.Domain.(collectionsResolver)
		if !ok || p.LearningLimit < 1 {
			return nil, "", errThemeUnavailable
		}
		result, err := collections.PublicCollections(ctx, locale, "learning", "", p.LearningLimit)
		if err != nil {
			return nil, "", err
		}
		view.LearningTitle, view.LearningURL = label("page.learn"), "/"+locale+"/learn"
		view.Collections = themeCollections(result.Items, label)
	}
	return view, css, nil
}

func (s *Server) themeAbout(ctx context.Context, settings siteconfig.Public, locale string, head *headData, label func(string) string) (*theme.About, error) {
	p := settings.Profile
	view := &theme.About{
		Title: label("page.about"), ExperienceLabel: label("config.experience"), WebsiteLabel: label("config.website"),
		Portrait: themePortrait(p, settings.Site, locale, label("config.portrait")),
		Bio:      profileText(p.Bio, locale), Location: profileText(p.Location, locale),
		Actions: themeActions(p, locale), Email: p.PublicEmail,
	}
	view.BioSegments = bioSegments(view.Bio.Value)
	if view.Email != "" && len(head.Scripts) > 0 {
		view.Copy, view.Copied, view.CopyFailed = label("public.copyEmail"), label("public.emailCopied"), label("public.emailCopyFailed")
	}
	for _, item := range p.Experience {
		role, org := profileText(item.Role, locale), profileText(item.Organization, locale)
		if role.Value == "" && org.Value == "" {
			continue
		}
		safe, _ := render.SafeLink(item.URL)
		row := theme.Experience{Role: role, Organization: org, Description: profileText(item.Description, locale), Start: item.Start, End: item.End, URL: safe}
		if item.Current {
			row.Current = label("config.current")
		}
		view.Experience = append(view.Experience, row)
	}
	if slug, ok := strings.CutPrefix(settings.AboutURL, "/api/v1/public/content/"+locale+"/"); ok && slug != "" {
		resolver := s.options.Domain.(pageResolver)
		document, err := resolver.ResolvePage(ctx, "/"+locale+"/doc/"+slug)
		if errors.Is(err, sql.ErrNoRows) {
			return view, nil
		}
		if err != nil {
			return nil, err
		}
		if document.Content != nil && document.Kind == "doc" && !document.Content.Locked {
			view.Body, err = s.themeMarkdown(document.Content, locale, head, label)
			if err != nil {
				return nil, err
			}
		}
	}
	return view, nil
}
