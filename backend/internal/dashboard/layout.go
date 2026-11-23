// Package dashboard persists cosmetic owner preferences. Screen and widget
// identities are registered menu targets and carry no route permissions.
package dashboard

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/config"
	"github.com/mbougarne/modawana/backend/internal/extension"
)

var ErrInvalid = errors.New("invalid dashboard layout")
var screens = []string{"posts", "pages", "collections", "taxonomy", "media", "themes", "menus", "settings", "labels", "transfer", "plugins", "forms"}
var groups = []string{"", "content", "site", "operations"}
var builtinWidgets = []string{"content", "site"}
var extensionID = regexp.MustCompile(`^extension:[a-z0-9][a-z0-9-]{0,63}\.[a-z][a-z0-9_-]{0,31}$`)

type Item struct {
	ID     string            `json:"id"`
	Rename map[string]string `json:"rename,omitempty"`
	Group  string            `json:"group,omitempty"`
	Hidden bool              `json:"hidden,omitempty"`
}

type Appearance struct {
	Theme   string `json:"theme,omitempty"`
	Density string `json:"density,omitempty"`
}

type Preset struct {
	Name       string     `json:"name"`
	Appearance Appearance `json:"appearance"`
}

type Layout struct {
	Navigation []Item     `json:"navigation,omitempty"`
	Widgets    []Item     `json:"widgets,omitempty"`
	Home       string     `json:"home,omitempty"`
	Appearance Appearance `json:"appearance"`
	Presets    []Preset   `json:"presets,omitempty"`
}

type Choice struct {
	ID    string                  `json:"id"`
	Label extension.LocalizedText `json:"label,omitempty"`
}

type Registry struct {
	Navigation []Choice `json:"navigation"`
	Widgets    []Choice `json:"widgets"`
}

func Choices(host *extension.Host) Registry {
	var r Registry
	for _, id := range screens {
		r.Navigation = append(r.Navigation, Choice{ID: id})
	}
	for _, id := range builtinWidgets {
		r.Widgets = append(r.Widgets, Choice{ID: id})
	}
	for _, item := range host.Contributions("") {
		choice := Choice{ID: "extension:" + item.Extension + "." + item.Key, Label: item.Label}
		switch item.Kind {
		case extension.AdminNavigation:
			r.Navigation = append(r.Navigation, choice)
		case extension.Widget:
			r.Widgets = append(r.Widgets, choice)
		}
	}
	return r
}

func validText(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 60 &&
		!strings.ContainsFunc(s, unicode.IsControl)
}

func validAppearance(a Appearance) bool {
	return slices.Contains([]string{"", "classic", "slate"}, a.Theme) &&
		slices.Contains([]string{"", "comfortable", "compact"}, a.Density)
}

func validItems(items []Item, builtins []string, grouped bool) bool {
	if len(items) > 64 {
		return false
	}
	seen := map[string]bool{}
	for _, row := range items {
		// Keep settings for an unavailable extension. These IDs resolve only
		// while the extension contributes an active item of the correct kind.
		if seen[row.ID] || (!slices.Contains(builtins, row.ID) && !extensionID.MatchString(row.ID)) ||
			!slices.Contains(groups, row.Group) || (!grouped && row.Group != "") {
			return false
		}
		seen[row.ID] = true
		for locale, label := range row.Rename {
			if !slices.Contains(config.SupportedLocales(), locale) || !validText(label) {
				return false
			}
		}
	}
	return true
}

func Validate(l *Layout) error {
	if l == nil {
		return nil
	}
	if !validItems(l.Navigation, screens, true) || !validItems(l.Widgets, builtinWidgets, false) ||
		!validAppearance(l.Appearance) || len(l.Presets) > 8 ||
		(l.Home != "" && l.Home != "dashboard" && !slices.Contains(screens, l.Home)) {
		return ErrInvalid
	}
	for _, row := range l.Navigation {
		if row.ID == l.Home && row.Hidden {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, p := range l.Presets {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if seen[name] || !validText(p.Name) || !validAppearance(p.Appearance) {
			return ErrInvalid
		}
		seen[name] = true
	}
	return nil
}
