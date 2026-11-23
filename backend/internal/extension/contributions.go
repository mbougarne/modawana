package extension

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	locales "github.com/mbougarne/modawana/backend/internal/locale"
)

const MaxContributions = 16

const (
	BuilderBlock     = "builder_block"
	AdminNavigation  = "admin_navigation"
	Settings         = "settings"
	Widget           = "widget"
	PublicProjection = "public_projection"
	ProviderAdapter  = "provider_adapter"
)

// BlockNamespace matches the first segment of a stored page block type.
var BlockNamespace = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type LocalizedText map[string]string

func (t LocalizedText) For(locale string) string {
	if text := t[locale]; text != "" {
		return text
	}
	return t["en"]
}

// Contribution is declarative metadata interpreted by the host. A manifest
// cannot specify a renderer, remote endpoint, browser code or a write action.
type Contribution struct {
	Kind    string        `json:"kind"`
	Key     string        `json:"key"`
	Version int           `json:"version"`
	Label   LocalizedText `json:"label"`
	Target  string        `json:"target,omitempty"`
	Text    LocalizedText `json:"text,omitempty"`
}

type ContributionRegistration struct {
	Extension string `json:"extension"`
	Contribution
	available func() bool
}

func textWithin(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

func validLocalized(text LocalizedText, limit int) bool {
	if strings.TrimSpace(text["en"]) == "" {
		return false
	}
	for locale, value := range text {
		if !locales.Valid(locale) ||
			strings.TrimSpace(value) == "" || !textWithin(value, limit) {
			return false
		}
	}
	return true
}

func (c Contribution) validate(slug string) error {
	if !slugPattern.MatchString(slug) || slug == "core" || !BlockNamespace.MatchString(c.Key) ||
		c.Version != 1 || !validLocalized(c.Label, 80) {
		return ErrInvalid
	}
	var valid bool
	switch c.Kind {
	case BuilderBlock:
		valid = BlockNamespace.MatchString(slug) && c.Target == "text_panel"
	case AdminNavigation:
		valid = c.Target == "overview" || c.Target == "settings"
	case Settings:
		valid = c.Target == "configuration"
	case Widget:
		valid = c.Target == "extensions" && validLocalized(c.Text, 500)
	case PublicProjection:
		valid = c.Target == "footer" && validLocalized(c.Text, 500)
	case ProviderAdapter:
		valid = slices.Contains([]string{"payments", "analytics", "social", "notifications", "editorial_ai"}, c.Target)
	}
	if !valid || (c.Kind != Widget && c.Kind != PublicProjection && c.Text != nil) {
		return ErrInvalid
	}
	return nil
}

func ValidateContributions(slug string, contributions []Contribution) error {
	if len(contributions) > MaxContributions {
		return fmt.Errorf("%w: too many contributions", ErrInvalid)
	}
	seen := make(map[string]bool, len(contributions))
	for i, c := range contributions {
		if c.validate(slug) != nil || seen[c.Key] {
			return fmt.Errorf("%w: contribution %d", ErrInvalid, i+1)
		}
		seen[c.Key] = true
	}
	return nil
}

func (c Contribution) clone() Contribution {
	c.Label, c.Text = maps.Clone(c.Label), maps.Clone(c.Text)
	return c
}

// Contribution registers a declaration whose availability follows its extension.
func (r *Registrar) Contribution(c Contribution, available func() bool) error {
	if len(r.contributions) >= MaxContributions || c.validate(r.slug) != nil ||
		slices.ContainsFunc(r.contributions, func(item ContributionRegistration) bool {
			return item.Key == c.Key
		}) {
		return ErrInvalid
	}
	r.contributions = append(r.contributions, ContributionRegistration{
		Extension: r.slug, Contribution: c.clone(), available: available,
	})
	return nil
}

// Contributions snapshots the active declarations in stable order. An empty
// kind includes all supported contribution kinds.
func (h *Host) Contributions(kind string) []ContributionRegistration {
	out := []ContributionRegistration{}
	if h == nil {
		return out
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.contributions {
		if (kind == "" || c.Kind == kind) && (c.available == nil || c.available()) {
			c.Contribution = c.Contribution.clone()
			out = append(out, c)
		}
	}
	return out
}

// TextPanel is the only builder block contract in version 1. Every value is
// plain text and is escaped by the host's public and preview templates.
type TextPanel struct {
	Heading string `json:"heading,omitempty"`
	Text    string `json:"text"`
}

func (h *Host) RenderBlock(name string, version int, properties json.RawMessage) (TextPanel, bool) {
	if h == nil || version != 1 || len(properties) > 8192 {
		return TextPanel{}, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.contributions {
		if c.Kind == BuilderBlock && c.Extension+"."+c.Key == name && c.Version == version &&
			(c.available == nil || c.available()) {
			return decodeTextPanel(properties)
		}
	}
	return TextPanel{}, false
}

func decodeTextPanel(raw json.RawMessage) (TextPanel, bool) {
	var panel TextPanel
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return panel, false
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		field, ok := key.(string)
		if err != nil || !ok || seen[field] {
			return TextPanel{}, false
		}
		seen[field] = true
		var value *string
		if decoder.Decode(&value) != nil || value == nil {
			return TextPanel{}, false
		}
		switch field {
		case "heading":
			panel.Heading = *value
		case "text":
			panel.Text = *value
		default:
			return TextPanel{}, false
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return TextPanel{}, false
	}
	return panel, strings.TrimSpace(panel.Text) != "" &&
		textWithin(panel.Heading, 200) && textWithin(panel.Text, 500)
}
