package siteconfig

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/locale"
)

func validPublicLocales(locales []string) bool {
	if locales == nil {
		return true
	}
	if len(locales) == 0 || len(locales) > len(config.SupportedLocales()) {
		return false
	}
	seen := map[string]bool{}
	for _, locale := range locales {
		if seen[locale] || !slices.Contains(config.SupportedLocales(), locale) {
			return false
		}
		seen[locale] = true
	}
	return true
}

// VisibleLocales returns the published languages in default selection order.
func (s *Service) VisibleLocales(ctx context.Context) ([]string, error) {
	return visibleLocalesIn(ctx, s.DB)
}

func LocaleVisibleIn(ctx context.Context, q query, code string) (bool, error) {
	locales, err := visibleLocalesIn(ctx, q)
	return slices.Contains(locales, code), err
}

func visibleLocalesIn(ctx context.Context, q query) ([]string, error) {
	doc, err := get(ctx, q, "site")
	if err != nil {
		return nil, err
	}
	var settings Site
	if err := json.Unmarshal(doc.Snapshot, &settings); err != nil {
		return nil, err
	}
	if !settings.validPublicLanguageSettings() {
		return nil, ErrInvalid
	}
	return settings.visibleLocales(), nil
}

func (s Site) visibleLocales() []string {
	visible := locale.PublicDefaults()
	if s.PublicLocales != nil {
		visible = slices.DeleteFunc(locale.Codes(), func(code string) bool {
			return !slices.Contains(s.PublicLocales, code)
		})
	}
	if index := slices.Index(visible, s.PublicDefaultLocale); index > 0 {
		visible = slices.Insert(slices.Delete(visible, index, index+1), 0, s.PublicDefaultLocale)
	}
	return visible
}

func (s Site) validPublicLanguageSettings() bool {
	return validPublicLocales(s.PublicLocales) &&
		(s.PublicDefaultLocale == "" || slices.Contains(s.visibleLocales(), s.PublicDefaultLocale))
}
