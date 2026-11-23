package theme

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

var packAssetName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}\.png$`)

type layoutManifest struct {
	Slots  []string `json:"slots"`
	Banner string   `json:"banner,omitempty"`
}

type viewLayout struct {
	slots     []string
	bannerURL string
}

var viewKinds = []string{"home", "page", "article", "storefront"}

// Token values are names of host choices. Pack strings are never interpolated
// into a property, value, selector, or markup fragment.
var tokenStyles = map[string]map[string]string{
	"content_width": {
		"narrow":   ".public-main { max-inline-size: 62rem; margin-inline: auto; }",
		"standard": ".public-main { max-inline-size: 72rem; margin-inline: auto; }",
		"wide":     ".public-main { max-inline-size: 88rem; margin-inline: auto; }",
	},
	"section_spacing": {
		"compact":     ".public-section { padding-block: var(--space-4); }",
		"comfortable": ".public-section { padding-block: var(--space-6); }",
		"spacious":    ".public-section { padding-block: var(--space-8); }",
	},
	"radius": {
		"square": ".public-banner { border-radius: 0; }",
		"soft":   ".public-banner { border-radius: var(--radius-md); }",
		"round":  ".public-banner { border-radius: var(--radius-lg); }",
	},
	"typeface": {
		"system": ".public-main { font-family: system-ui, sans-serif; }",
		"serif":  ".public-main { font-family: Georgia, serif; }",
	},
	"surface": {
		"paper":   ".public-main { background-color: var(--paper); }",
		"surface": ".public-main { background-color: var(--surface); }",
	},
}

func parsePackV2(id, checksum string, m manifest, raw []byte) (Pack, error) {
	if m.Layout != "" || m.CSS != "" || m.Banner != nil ||
		len(m.Layouts) == 0 || len(m.Layouts) > len(viewKinds) || len(m.Assets) > 4 {
		return Pack{}, ErrPack
	}
	for name, asset := range m.Assets {
		if !packAssetName.MatchString(name) || !validBanner(asset) {
			return Pack{}, ErrPack
		}
	}
	views := make(map[string]viewLayout, len(m.Layouts))
	for kind, layout := range m.Layouts {
		if !slices.Contains(viewKinds, kind) || !validViewSlots(layout.Slots, layout.Banner != "") {
			return Pack{}, ErrPack
		}
		view := viewLayout{slots: layout.Slots}
		if layout.Banner != "" {
			if _, ok := m.Assets[layout.Banner]; !ok {
				return Pack{}, ErrPack
			}
			view.bannerURL = "/theme-assets/" + id + "/" + checksum + "/" + layout.Banner
		}
		views[kind] = view
	}
	var styles strings.Builder
	for _, name := range slices.Sorted(maps.Keys(m.Tokens)) {
		rule := tokenStyles[name][m.Tokens[name]]
		if rule == "" {
			return Pack{}, ErrPack
		}
		styles.WriteString(rule)
	}
	css, err := scopeCSS(id, styles.String())
	if err != nil {
		return Pack{}, err
	}
	return Pack{
		Appearance: Appearance{ID: id, CSS: css, views: views},
		Name:       m.Name, Checksum: checksum, Raw: raw, assets: m.Assets,
	}, nil
}

func validViewSlots(slots []string, banner bool) bool {
	required := map[string]bool{"header": true, "content": true, "footer": true, "banner": banner}
	if len(slots) != 3 && !(banner && len(slots) == 4) {
		return false
	}
	for _, slot := range slots {
		if !required[slot] {
			return false
		}
		required[slot] = false
	}
	for _, missing := range required {
		if missing {
			return false
		}
	}
	return true
}
