package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/store"
	"github.com/mbougarne/modawana/backend/internal/theme"
)

type catalogResolver interface {
	StoreCatalog(context.Context, store.Filter) (store.PublicPage, error)
	StoreEnabled(context.Context) bool
}

func (d *Domain) StoreEnabled(ctx context.Context) bool {
	if d.Store == nil || !d.extensions().Installed(store.Slug) {
		return false
	}
	cfg, err := d.Store.Settings(ctx)
	return err == nil && cfg.Enabled
}

func (s *Server) storeEnabled(ctx context.Context) bool {
	resolver, ok := s.options.Domain.(catalogResolver)
	return ok && resolver.StoreEnabled(ctx)
}

func displayPrice(minor int64, currency string, digits int) string {
	if digits == 0 {
		return currency + " " + strconv.FormatInt(minor, 10)
	}
	var divisor int64 = 1
	for range digits {
		divisor *= 10
	}
	return fmt.Sprintf("%s %d.%0*d", currency, minor/divisor, digits, minor%divisor)
}

func themeProduct(item store.PublicProduct, currency string, digits int, label func(string) string) theme.StoreProduct {
	out := theme.StoreProduct{Title: item.Title, Description: item.Description, URL: item.URL, Price: displayPrice(item.PriceMinor, currency, digits)}
	for _, image := range item.Images {
		out.Images = append(out.Images, theme.StoreImage{URL: "/media/" + image.ID, Alt: image.Alt})
	}
	for _, variant := range item.Variants {
		status := label("store.outOfStock")
		if variant.InStock {
			status = label("store.inStock")
		}
		out.Variants = append(out.Variants, theme.StoreVariant{SKU: variant.SKU, Name: variant.Label, Price: displayPrice(variant.PriceMinor, currency, digits), Availability: status, InStock: variant.InStock})
	}
	return out
}

func (s *Server) themeStore(ctx context.Context, r *http.Request, page content.PageRoute, label func(string) string) (*theme.StoreView, error) {
	if page.Store == nil {
		return nil, errThemeUnavailable
	}
	path := "/" + page.Locale + "/store"
	data := *page.Store
	view := &theme.StoreView{Title: label("page.store"), URL: path, Back: label("page.store"), BackURL: path, SearchLabel: label("store.search"),
		Submit: label("public.search"), CategoryLabel: label("public.category"), Empty: label("store.empty"), Next: label("public.next")}
	if data.CheckoutEnabled {
		view.CartLabel, view.CartURL, view.AddLabel = label("page.checkout"), path+"/checkout/cart", label("checkout.add")
		view.AddedLabel, view.CartFull, view.CartFailed = label("checkout.added"), label("checkout.cartFull"), label("checkout.cartFailed")
	}
	if data.Product != nil {
		product := themeProduct(*data.Product, data.Currency, data.Digits, label)
		view.Product = &product
		return view, nil
	}
	query := r.URL.Query()
	view.Search = query.Get("q")
	category := query.Get("category")
	cursor := query.Get("cursor")
	if view.Search != "" || category != "" || cursor != "" {
		resolver, ok := s.options.Domain.(catalogResolver)
		if !ok {
			return nil, errThemeUnavailable
		}
		filtered, err := resolver.StoreCatalog(ctx, store.Filter{Locale: page.Locale, Query: view.Search, Category: category, Cursor: cursor, Limit: 24})
		if errors.Is(err, store.ErrInvalid) {
			view.Error = label("store.invalidSearch")
			return view, nil
		}
		if err != nil {
			return nil, err
		}
		data = filtered
	}
	view.Categories = append(view.Categories, theme.Choice{Label: label("public.any"), Selected: category == ""})
	for _, item := range data.Categories {
		view.Categories = append(view.Categories, theme.Choice{Value: item.Key, Label: item.Title, Selected: category == item.Key})
	}
	for _, item := range data.Products {
		summary, _, _ := strings.Cut(item.Description, "\n")
		item.Description = summary
		view.Products = append(view.Products, themeProduct(item, data.Currency, data.Digits, label))
	}
	if data.Next != "" {
		next := url.Values{"cursor": {data.Next}}
		if category != "" {
			next.Set("category", category)
		}
		if view.Search != "" {
			next.Set("q", view.Search)
		}
		view.NextURL = path + "?" + next.Encode()
	}
	return view, nil
}
