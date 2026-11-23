package httpserver

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/content"
	"github.com/mbougarne/modawana/backend/internal/locale"
)

type publicLocalesKey struct{}

func (s *Server) withPublicLocales(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	if s.options.SiteConfig == nil {
		return r, true
	}
	locales, err := s.options.SiteConfig.VisibleLocales(r.Context())
	if err != nil {
		writeError(w, Unavailable)
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), publicLocalesKey{}, locales)), true
}

func publicLocales(ctx context.Context) []string {
	if locales, ok := ctx.Value(publicLocalesKey{}).([]string); ok {
		return locales
	}
	return locale.PublicDefaults()
}

func visibleLocale(ctx context.Context, locale string) bool {
	return slices.Contains(publicLocales(ctx), locale)
}

func hiddenLocale(ctx context.Context, locale string) bool {
	return pageLocale(locale) && !visibleLocale(ctx, locale)
}

func pathLocale(path string) string {
	locale, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	return locale
}

func hideLocaleLinks(ctx context.Context, alternatives []content.Alternative) []content.Alternative {
	return slices.DeleteFunc(slices.Clone(alternatives), func(link content.Alternative) bool {
		return !visibleLocale(ctx, link.Locale)
	})
}

func localizePublicContent(ctx context.Context, item content.PublicContent) content.PublicContent {
	item.Alternatives = hideLocaleLinks(ctx, item.Alternatives)
	if item.Learning != nil {
		state := localizeReaderLearning(ctx, *item.Learning)
		item.Learning = &state
	}
	return item
}

func localizeReaderLearning(ctx context.Context, state content.ReaderLearning) content.ReaderLearning {
	hadPrerequisites := len(state.Prerequisites) > 0
	state.Alternatives = hideLocaleLinks(ctx, state.Alternatives)
	state.Prerequisites = hideLocaleLinks(ctx, state.Prerequisites)
	state.Unavailable = state.Unavailable || (hadPrerequisites && len(state.Prerequisites) == 0)
	return state
}

func localizePublicPage(ctx context.Context, page content.PageRoute) content.PageRoute {
	page.Alternatives = hideLocaleLinks(ctx, page.Alternatives)
	if page.Content != nil {
		item := localizePublicContent(ctx, *page.Content)
		page.Content = &item
	}
	return page
}
