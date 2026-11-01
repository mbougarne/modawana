package siteconfig

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

//go:embed labels.json
var registryJSON []byte

type LabelSpec struct {
	Placeholders []string                   `json:"placeholders"`
	Section      string                     `json:"section"`
	Description  string                     `json:"description"`
	Values       map[string]json.RawMessage `json:"values"`
}

var Registry = func() map[string]LabelSpec {
	m := map[string]LabelSpec{}
	if json.Unmarshal(registryJSON, &m) != nil {
		panic("invalid label registry")
	}
	return m
}()
var customKey = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){1,3}$`)
var placeholder = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)

func parts(b json.RawMessage) (map[string]string, bool) {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return map[string]string{"": s}, false
	}
	var p map[string]string
	if json.Unmarshal(b, &p) != nil || len(p) == 0 {
		return nil, true
	}
	return p, true
}
func params(s string) string {
	seen := map[string]bool{}
	for _, p := range placeholder.FindAllStringSubmatch(s, -1) {
		seen[p[1]] = true
	}
	a := []string{}
	for p := range seen {
		a = append(a, p)
	}
	sort.Strings(a)
	return strings.Join(a, ",")
}
func validateLabels(labels Labels) error {
	if len(labels) > 1000 {
		return ErrInvalid
	}
	for key, values := range labels {
		for locale, b := range values {
			var value any
			if json.Unmarshal(b, &value) != nil {
				return ErrInvalid
			}
			values[locale] = raw(value)
		}
		base, core := Registry[key]
		if len(key) > 160 || (!core && !customKey.MatchString(key)) || len(values) > 3 || values == nil {
			return ErrInvalid
		}
		en := values["en"]
		if len(en) == 0 {
			en = base.Values["en"]
		}
		ep, plural := parts(en)
		if len(ep) == 0 {
			return ErrInvalid
		}
		if _, ok := ep["other"]; plural && !ok {
			return ErrInvalid
		}
		expected := ""
		first := true
		for _, s := range ep {
			if strings.TrimSpace(s) == "" {
				return ErrInvalid
			}
			if first {
				expected = params(s)
				first = false
			} else if params(s) != expected {
				return ErrInvalid
			}
		}
		if core {
			bp, bplural := parts(base.Values["en"])
			if plural != bplural {
				return ErrInvalid
			}
			for _, s := range bp {
				if params(s) != expected {
					return ErrInvalid
				}
			}
		}
		for locale, b := range values {
			if locale != "en" && locale != "ar" && locale != "fr" {
				return ErrInvalid
			}
			if locale != "en" && string(b) == `""` {
				continue
			}
			p, isPlural := parts(b)
			if p == nil || isPlural != plural {
				return ErrInvalid
			}
			if plural {
				forms := []string{"one", "other"}
				if locale == "ar" {
					forms = []string{"zero", "one", "two", "few", "many", "other"}
				}
				for _, f := range forms {
					if strings.TrimSpace(p[f]) == "" {
						return ErrInvalid
					}
				}
			}
			for form, s := range p {
				if isPlural && !strings.Contains("|zero|one|two|few|many|other|", "|"+form+"|") {
					return ErrInvalid
				}
				if !textOK(s, 1000) || strings.TrimSpace(s) == "" || params(s) != expected || strings.ContainsAny(placeholder.ReplaceAllString(s, ""), "{}") {
					return ErrInvalid
				}
			}
		}
	}
	return nil
}

// Review paths follow stable row IDs, so reordering does not invalidate translations.
func translations(kind string, snapshot json.RawMessage) map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	if kind == "labels" {
		var labels Labels
		_ = json.Unmarshal(snapshot, &labels)
		for k, v := range labels {
			copy := map[string]json.RawMessage{}
			for l, b := range v {
				copy[l] = b
			}
			if len(copy["en"]) == 0 {
				var value any
				_ = json.Unmarshal(Registry[k].Values["en"], &value)
				copy["en"] = raw(value)
			}
			out[k] = copy
		}
		return out
	}
	if kind != "profile" {
		return out
	}
	var root any
	_ = json.Unmarshal(snapshot, &root)
	var walk func(any, string)
	walk = func(v any, path string) {
		switch v := v.(type) {
		case map[string]any:
			if _, ok := v["en"]; ok {
				l := map[string]json.RawMessage{}
				for k, x := range v {
					l[k] = raw(x)
				}
				out[path] = l
				return
			}
			for k, x := range v {
				next := k
				if path != "" {
					next = path + "." + k
				}
				walk(x, next)
			}
		case []any:
			for _, x := range v {
				if m, ok := x.(map[string]any); ok {
					if id, ok := m["id"].(string); ok {
						walk(x, path+"."+id)
					}
				}
			}
		}
	}
	walk(root, "")
	return out
}
