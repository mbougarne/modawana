package siteconfig

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/mbougarne/modawana/backend/internal/config"
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
	doc, err := s.Get(ctx, "site")
	if err != nil {
		return nil, err
	}
	var settings Site
	if err := json.Unmarshal(doc.Snapshot, &settings); err != nil {
		return nil, err
	}
	if !validPublicLocales(settings.PublicLocales) {
		return nil, ErrInvalid
	}
	return settings.visibleLocales(), nil
}

func (s Site) visibleLocales() []string {
	locales := config.SupportedLocales()
	if s.PublicLocales == nil {
		return locales
	}
	return slices.DeleteFunc(locales, func(locale string) bool {
		return !slices.Contains(s.PublicLocales, locale)
	})
}
