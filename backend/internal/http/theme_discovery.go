package httpserver

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

type discoveryResolver interface {
	PublicDiscovery(context.Context, content.Filter) (content.Page, error)
}

func (s *Server) themeTerms(ctx context.Context, locale string) ([]content.Term, error) {
	resolver, ok := s.options.Domain.(taxonomyResolver)
	if !ok {
		return nil, errThemeUnavailable
	}
	return resolver.PublicTaxonomy(ctx, locale)
}

func themePosts(entries []content.ListEntry, terms []content.Term, locale string, label func(string) string) []theme.Year {
	categories, tags := map[string]string{}, map[string]string{}
	for _, term := range terms {
		if term.Kind == "category" {
			categories[term.Key] = term.Label
		} else {
			tags[term.Key] = term.Label
		}
	}
	var years []theme.Year
	for _, entry := range entries {
		year, _, _ := strings.Cut(entry.Published, "-")
		if len(years) == 0 || years[len(years)-1].Number != year {
			years = append(years, theme.Year{Number: year})
		}
		post := theme.Post{
			Title: entry.Title, Summary: entry.Summary, URL: entry.URL, Kind: entry.Kind,
			DateISO: entry.Published, DateText: formatPublicDate(entry.Published, locale),
			Badge: categories[entry.CategoryKey],
		}
		if entry.Kind != "article" {
			post.Badge = label("kind." + entry.Kind)
		}
		if entry.Locked {
			post.LockLabel = label("lesson.lockedShort")
			post.URL = ""
		}
		for _, tag := range entry.Tags {
			if name := tags[tag]; name != "" {
				post.Tags = append(post.Tags, name)
			}
		}
		group := &years[len(years)-1]
		group.Posts = append(group.Posts, post)
	}
	return years
}

func (s *Server) themeDiscovery(ctx context.Context, query url.Values, page content.PageRoute, label func(string) string) (*theme.Discovery, error) {
	resolver, ok := s.options.Domain.(discoveryResolver)
	if !ok {
		return nil, errThemeUnavailable
	}
	filter := content.Filter{
		Locale: page.Locale, Kind: query.Get("kind"), Category: query.Get("category"),
		Tag: query.Get("tag"), Query: query.Get("q"), Cursor: query.Get("cursor"), Limit: 20, ExcludePages: page.Kind == "writing",
	}
	filters := url.Values{}
	for _, key := range []string{"kind", "category", "tag", "q"} {
		if value := query.Get(key); value != "" {
			filters.Set(key, value)
		}
	}
	view := &theme.Discovery{
		Title: label("page." + page.Kind), URL: page.URL, Search: page.Kind == "search",
		Query: filter.Query, QueryLabel: label("public.query"), Submit: label("public.search"),
		KindLabel: label("public.kind"), PagesLabel: label("public.pages"), Clear: label("public.clear"),
		LiveError: label("public.error"),
	}
	if view.Search {
		for _, key := range []string{"category", "tag"} {
			if value := filters.Get(key); value != "" {
				view.Fields = append(view.Fields, theme.Field{Name: key, Value: value})
			}
		}
		view.Kinds = append(view.Kinds, theme.Choice{Label: label("public.any"), Selected: filter.Kind == ""})
		for _, kind := range []string{"article", "lesson", "doc", "page"} {
			view.Kinds = append(view.Kinds, theme.Choice{Value: kind, Label: label("public." + kind), Selected: filter.Kind == kind})
		}
	} else {
		view.Intro = label("writing.intro")
	}
	result, err := resolver.PublicDiscovery(ctx, filter)
	if err != nil {
		view.Problem = label("public.error")
		if !errors.Is(err, content.ErrInvalid) {
			view.RetryLabel, view.RetryURL = label("public.retry"), discoveryURL(page.URL, query)
		}
		return view, nil
	}
	terms, err := s.themeTerms(ctx, page.Locale)
	if err != nil {
		return nil, err
	}
	if !view.Search {
		view.Filters = discoveryFilters(page.URL, filters, terms, label)
	}
	var active []string
	for _, key := range []string{"kind", "category", "tag", "q"} {
		value := filters.Get(key)
		if value == "" {
			continue
		}
		if key == "kind" {
			value = label("public." + value)
		}
		for _, term := range terms {
			if key == term.Kind && value == term.Slug {
				value = term.Label
				break
			}
		}
		active = append(active, value)
	}
	view.Active = strings.Join(active, " · ")
	view.Count = interpolate(label("public.results"), map[string]string{"n": strconv.Itoa(result.Total)})
	view.Empty = label("public.noResults")
	view.Posts = themePosts(result.Items, terms, page.Locale, label)
	if filter.Cursor != "" {
		previous := query["previous"]
		if n := len(previous); n > 0 {
			if last := previous[n-1]; last != "" && len(last) <= 512 {
				filters.Set("cursor", last)
				filters["previous"] = previous[:n-1]
			}
		}
		view.Previous, view.PreviousURL = label("public.previous"), discoveryURL(page.URL, filters)
		filters.Del("cursor")
		filters.Del("previous")
	}
	if result.Next != "" {
		for _, cursor := range query["previous"] {
			if len(cursor) <= 512 {
				filters.Add("previous", cursor)
			}
		}
		filters.Add("previous", filter.Cursor)
		filters.Set("cursor", result.Next)
		view.Next, view.NextURL = label("public.next"), discoveryURL(page.URL, filters)
	}
	return view, nil
}

func discoveryFilters(path string, filters url.Values, terms []content.Term, label func(string) string) []theme.FilterGroup {
	var groups []theme.FilterGroup
	for _, kind := range []string{"category", "tag"} {
		tag := kind == "tag"
		group := theme.FilterGroup{Label: label("public.category"), Tag: tag}
		if tag {
			group.Label = label("public.tags")
		} else {
			group.Items = append(group.Items, theme.Filter{Link: theme.Link{
				Label: label("writing.all"), URL: filterURL(path, filters, kind, ""), Current: filters.Get(kind) == "",
			}})
		}
		for _, term := range terms {
			if term.Kind != kind {
				continue
			}
			current := filters.Get(kind) == term.Slug
			value := term.Slug
			if current {
				value = ""
			}
			item := theme.Filter{Link: theme.Link{Label: term.Label, URL: filterURL(path, filters, kind, value), Current: current}}
			if !tag {
				item.Count = &term.Count
			}
			group.Items = append(group.Items, item)
		}
		if len(group.Items) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func filterURL(path string, filters url.Values, key, value string) string {
	next := url.Values{}
	for name, values := range filters {
		if name != key {
			next[name] = values
		}
	}
	if value != "" {
		next.Set(key, value)
	}
	return discoveryURL(path, next)
}

func discoveryURL(path string, values url.Values) string {
	if query := values.Encode(); query != "" {
		return path + "?" + query
	}
	return path
}

func themeArrow(locale string) string {
	if locale == "ar" {
		return "←"
	}
	return "→"
}
