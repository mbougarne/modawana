package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"

	"github.com/mbougarne/modawana/backend/internal/access"
	"github.com/mbougarne/modawana/backend/internal/content"
)

func (s *Server) resolveEditorialRedirect(ctx context.Context, resolver pageResolver, path string) (content.PageRoute, error) {
	if s.options.SiteConfig == nil || !visibleLocale(ctx, pathLocale(path)) {
		return content.PageRoute{}, sql.ErrNoRows
	}
	target, err := s.options.SiteConfig.RedirectTarget(ctx, path)
	if err != nil {
		return content.PageRoute{}, err
	}
	if target == "" || !visibleLocale(ctx, pathLocale(target)) {
		return content.PageRoute{}, sql.ErrNoRows
	}
	page, err := resolver.ResolvePage(access.WithPrincipal(ctx, access.Principal{}), target)
	if errors.Is(err, content.ErrLocked) {
		return content.PageRoute{}, sql.ErrNoRows
	}
	return page, err
}

type structuredBrand struct {
	Type string `json:"@type"`
	Name string `json:"name"`
}

type structuredPage struct {
	SKU         string           `json:"sku,omitempty"`
	Brand       *structuredBrand `json:"brand,omitempty"`
	Context     string           `json:"@context"`
	Type        string           `json:"@type"`
	URL         string           `json:"url"`
	Language    string           `json:"inLanguage"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Published   string           `json:"datePublished,omitempty"`
	Image       string           `json:"image,omitempty"`
}

func structuredMetadata(item *content.PublicContent, head headData) template.JS {
	if item == nil || item.Locked || item.Kind == "lesson" || item.SEO == nil || head.NoIndex || head.Canonical == "" {
		return ""
	}
	doc := structuredPage{Context: "https://schema.org", URL: head.Canonical, Language: item.Locale, Name: head.Title,
		Description: head.Description, Image: head.Social.Image}
	switch item.SEO.Schema {
	case "web_page":
		doc.Type = "WebPage"
	case "product":
		if item.Kind != "page" || item.SEO.Product == nil {
			return ""
		}
		doc.Type, doc.SKU = "Product", item.SEO.Product.SKU
		if item.SEO.Product.Brand != "" {
			doc.Brand = &structuredBrand{Type: "Brand", Name: item.SEO.Product.Brand}
		}
	case "article":
		if item.Kind != "article" {
			return ""
		}
		doc.Type, doc.Published = "Article", item.Published
	default:
		return ""
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return ""
	}
	// Marshal escapes angle brackets and line separators for the JSON script body.
	return template.JS(raw)
}
