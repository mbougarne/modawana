package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/siteconfig"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

type collectionsResolver interface {
	PublicCollections(context.Context, string, string, string, int) (content.CollectionPage, error)
}

const readingPageSize = 20

func needsReaderIsland(page content.PageRoute) bool {
	return page.Kind == "lesson" || page.Kind == "learn" ||
		page.Kind == "collection" && page.Collection != nil && page.Collection.Kind == "learning" ||
		hasQuizControls(page.Content)
}

func hasQuizControls(item *content.PublicContent) bool {
	return item != nil && item.Learning != nil && (item.Learning.Quiz != nil || item.Learning.Unavailable)
}

func themeLocked(state *content.ReaderLearning, label func(string) string) *theme.LockedLesson {
	view := &theme.LockedLesson{
		Title: label("public.locked"), Help: label("public.lockHelp"), Prerequisite: label("progress.prerequisite"),
	}
	if state != nil {
		if state.Unavailable {
			view.Unavailable = label("progress.unavailable")
		}
		for _, link := range state.Prerequisites {
			dir := "ltr"
			if link.Locale == "ar" {
				dir = "rtl"
			}
			view.Links = append(view.Links, theme.Language{URL: link.URL, Code: link.Locale, Dir: dir, Label: label("language." + link.Locale)})
		}
	}
	return view
}

func themeCount(collection content.PublicCollection, label func(string) string) string {
	return interpolate(label("public.counts"), map[string]string{
		"published": strconv.Itoa(collection.PublishedCount), "announced": strconv.Itoa(collection.AnnouncedCount),
	})
}

func themeCollections(items []content.PublicCollection, label func(string) string) []theme.Collection {
	view := make([]theme.Collection, 0, len(items))
	for i, item := range items {
		initials := []rune(strings.TrimSpace(item.Title))
		view = append(view, theme.Collection{
			Title: item.Title, URL: item.URL, Summary: item.Summary, Badge: i%3 + 1,
			Initials: strings.ToUpper(string(initials[:min(2, len(initials))])),
			Counts:   themeCount(item, label),
		})
	}
	return view
}

func themeOutline(c *content.PublicCollection, current string, label func(string) string) *theme.Outline {
	learning := c.Kind == "learning"
	view := &theme.Outline{Title: c.Title, Label: label("lesson.list"), Refresh: learning}
	complete := 0
	for _, item := range c.Items {
		row := theme.OutlineItem{
			Title: item.Title, Position: item.Position, State: item.State,
			Current: current != "" && current == item.Key,
		}
		switch item.State {
		case "open":
			row.URL = item.URL
			if item.Completed && learning {
				row.Status = label("progress.completed")
				complete++
			}
		case "locked":
			row.Status = label("lesson.lockedShort")
		case "soon":
			row.Status = label("lesson.soon")
		}
		view.Items = append(view.Items, row)
	}
	if learning {
		view.Progress = interpolate(label("progress.count"), map[string]string{
			"n": strconv.Itoa(complete), "total": strconv.Itoa(c.PublishedCount),
		})
	} else {
		view.Label = label("public.outline")
	}
	return view
}

func themeNeighbors(c *content.PublicCollection, position int, label func(string) string) *theme.Neighbors {
	view := &theme.Neighbors{Label: label("public.outline"), Refresh: c.Kind == "learning"}
	for _, entry := range []struct {
		index int
		next  bool
		key   string
	}{{position - 1, false, "public.previous"}, {position + 1, true, "public.next"}} {
		if entry.index < 0 || entry.index >= len(c.Items) {
			continue
		}
		item := c.Items[entry.index]
		row := theme.Neighbor{Title: item.Title, Next: entry.next, Label: label(entry.key) + " · " + label("public."+item.State)}
		if item.State == "open" {
			row.URL = item.URL
		}
		view.Items = append(view.Items, row)
	}
	if len(view.Items) == 0 {
		return nil
	}
	return view
}

func (s *Server) themeReading(ctx context.Context, query url.Values, page content.PageRoute, settings siteconfig.Public, head *headData, label func(string) string) (*theme.Reading, error) {
	switch page.Kind {
	case "learn":
		resolver, ok := s.options.Domain.(collectionsResolver)
		if !ok {
			return nil, errThemeUnavailable
		}
		index, err := resolver.PublicCollections(ctx, page.Locale, "learning", query.Get("cursor"), readingPageSize)
		if err != nil {
			return nil, err
		}
		return &theme.Reading{
			Kind: "learning-index", Title: label("page.learn"), Intro: label("learning.intro"), Empty: label("public.empty"),
			Progress: true, Collections: themeCollections(index.Items, label),
			Pager: themeReadingPager(page.URL, query, "cursor", "previous", index.Next, label),
		}, nil
	case "collection":
		if page.Collection == nil {
			return nil, errThemeUnavailable
		}
		return &theme.Reading{
			Kind: "collection", Title: page.Collection.Title, Intro: page.Collection.Summary,
			Counts: themeCount(*page.Collection, label), Progress: page.Collection.Kind == "learning",
			Outline: themeOutline(page.Collection, "", label),
		}, nil
	case "docs":
		return s.themeDocs(ctx, query, page.Locale, nil, settings, head, label)
	}
	if page.Content == nil {
		return nil, errThemeUnavailable
	}
	if page.Kind == "doc" && page.Collection == nil {
		return s.themeDocs(ctx, nil, page.Locale, page.Content, settings, head, label)
	}
	reader, err := s.themeReader(ctx, page.Content, settings, head, label)
	if err != nil {
		return nil, err
	}
	view := &theme.Reading{Reader: reader}
	if c := page.Collection; c != nil {
		view.Outline = themeOutline(c, page.Content.Key, label)
		for i, item := range c.Items {
			if item.Key == page.Content.Key {
				view.Neighbors = themeNeighbors(c, i, label)
				if page.Kind == "lesson" {
					reader.Position = interpolate(label("public.lessonPosition"), map[string]string{
						"n": strconv.Itoa(item.Position), "total": strconv.Itoa(c.Total),
					})
				}
				break
			}
		}
	}
	return view, nil
}

func (s *Server) themeDocs(ctx context.Context, query url.Values, locale string, current *content.PublicContent, settings siteconfig.Public, head *headData, label func(string) string) (*theme.Reading, error) {
	discovery, ok := s.options.Domain.(discoveryResolver)
	if !ok {
		return nil, errThemeUnavailable
	}
	path := "/" + locale + "/docs"
	index, err := discovery.PublicDiscovery(ctx, content.Filter{Locale: locale, Kind: "doc", Cursor: query.Get("cursor"), Limit: readingPageSize})
	if err != nil {
		return nil, err
	}
	view := &theme.Reading{Title: label("page.docs"), Empty: label("public.empty")}
	outline := &theme.Outline{
		Title: label("page.docs"), Label: label("page.docs"),
		Pager: themeReadingPager(path, query, "cursor", "previous", index.Next, label),
	}
	selected := query.Get("doc")
	if current != nil {
		selected = current.Slug
	} else {
		if selected == "" && len(index.Items) > 0 {
			selected = index.Items[0].Slug
		}
		if selected != "" {
			resolved, err := s.options.Domain.(pageResolver).ResolvePage(ctx, "/"+locale+"/doc/"+selected)
			if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, content.ErrLocked) {
				return nil, err
			}
			if err == nil && resolved.Kind == "doc" && resolved.Content != nil && !resolved.Content.Locked {
				current = resolved.Content
			} else {
				view.DocumentError = label("foundation.notFound")
			}
		}
	}
	for _, item := range index.Items {
		if !item.Locked {
			outline.Items = append(outline.Items, theme.OutlineItem{Title: item.Title, URL: item.URL, Current: item.Slug == selected})
		}
	}
	if len(outline.Items) > 0 || outline.Pager.PreviousURL != "" {
		view.Outline = outline
	}
	if current != nil {
		view.Reader, err = s.themeReader(ctx, current, settings, head, label)
		if err != nil {
			return nil, err
		}
		view.Title = ""
	}
	if query != nil {
		if collections, ok := s.options.Domain.(collectionsResolver); ok {
			guides, err := collections.PublicCollections(ctx, locale, "docs", query.Get("guide_cursor"), readingPageSize)
			if err != nil {
				return nil, err
			}
			view.CollectionTitle = label("page.docs")
			view.Collections = themeCollections(guides.Items, label)
			view.Pager = themeReadingPager(path, query, "guide_cursor", "guide_previous", guides.Next, label)
		}
	}
	return view, nil
}

func themeReadingPager(path string, query url.Values, cursorKey, previousKey, next string, label func(string) string) theme.Pager {
	pager := theme.Pager{Label: label("public.pages"), Previous: label("public.previous"), Next: label("public.next")}
	values := url.Values{}
	for _, key := range []string{"doc", "cursor", "previous", "guide_cursor", "guide_previous"} {
		if key != cursorKey && key != previousKey && query.Has(key) {
			values[key] = query[key]
		}
	}
	previous := query[previousKey]
	if cursor := query.Get(cursorKey); cursor != "" {
		if n := len(previous); n > 0 && previous[n-1] != "" {
			values.Set(cursorKey, previous[n-1])
			values[previousKey] = previous[:n-1]
		}
		pager.PreviousURL = discoveryURL(path, values)
		values.Del(cursorKey)
		values.Del(previousKey)
	}
	if next != "" {
		for _, cursor := range previous {
			values.Add(previousKey, cursor)
		}
		values.Add(previousKey, query.Get(cursorKey))
		values.Set(cursorKey, next)
		pager.NextURL = discoveryURL(path, values)
	}
	return pager
}
