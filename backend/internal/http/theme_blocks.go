package httpserver

import (
	"context"
	"html/template"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/forms"
)

func (s *Server) themePageBlocks(ctx context.Context, c *content.PublicContent, head *headData, label func(string) string) (template.HTML, error) {
	listContext := access.WithPrincipal(ctx, access.Principal{})
	cache := map[string][]content.PageBlockLink{}
	return content.RenderPageBlocks(c.PageBlocks, content.PageBlockRenderOptions{
		ExtensionBlock: s.extensions().RenderBlock,
		Form: func(id string, ref forms.Reference) (template.HTML, error) {
			return s.themeLeadForm(ctx, c, head, label, id, ref)
		},
		Media: c.Media, Unavailable: label("pageBlocks.unavailable"), Empty: label("pageBlocks.emptyList"),
		Markdown: func(markdown string) (template.HTML, error) {
			projection := *c
			projection.Markdown = markdown
			return s.themeMarkdown(&projection, c.Locale, head, label)
		},
		Links: func(section string, limit int) ([]content.PageBlockLink, error) {
			if hiddenLocale(ctx, c.Locale) {
				return nil, nil
			}
			links, ok := cache[section]
			if !ok {
				var err error
				links, err = s.pageBlockLinks(listContext, c, section)
				if err != nil {
					return nil, err
				}
				cache[section] = links
			}
			return links[:min(limit, len(links))], nil
		},
	})
}

func (s *Server) pageBlockLinks(ctx context.Context, c *content.PublicContent, section string) ([]content.PageBlockLink, error) {
	const limit = 12
	var links []content.PageBlockLink
	switch section {
	case "writing", "pages":
		source, ok := s.options.Domain.(discoveryResolver)
		if !ok {
			return nil, errThemeUnavailable
		}
		kind := "article"
		if section == "pages" {
			kind = "page"
		}
		page, err := source.PublicDiscovery(ctx, content.Filter{Locale: c.Locale, Kind: kind, Limit: limit + 1})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			if item.Key != c.Key && !item.Locked && item.URL != "" {
				links = append(links, content.PageBlockLink{Title: item.Title, URL: item.URL})
			}
		}
	case "learning", "docs":
		source, ok := s.options.Domain.(interface {
			PublicCollections(context.Context, string, string, string, int) (content.CollectionPage, error)
		})
		if !ok {
			return nil, errThemeUnavailable
		}
		page, err := source.PublicCollections(ctx, c.Locale, section, "", limit)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			links = append(links, content.PageBlockLink{Title: item.Title, URL: item.URL})
		}
	}
	return links, nil
}
