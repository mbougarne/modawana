package theme

import (
	"regexp"
	"strings"
)

// Pack CSS accepts simple selectors and bounded presentation declarations.
// Rebuilding each rule scopes every selector and prevents at-rules, resource
// loading, and style-element escapes even when a pack supplies malformed CSS.
var packSelector = regexp.MustCompile(`^(?:\.[a-zA-Z][a-zA-Z0-9_-]*|[a-z][a-z0-9]*)(?:[ \t\r\n]+(?:\.[a-zA-Z][a-zA-Z0-9_-]*|[a-z][a-z0-9]*))*$`)
var packValue = regexp.MustCompile(`^[a-zA-Z0-9#%.+/*(), \t\r\n-]{1,200}$`)
var cssFunction = regexp.MustCompile(`([a-zA-Z-]+)[ \t\r\n]*\(`)
var cssFunctions = map[string]bool{
	"var": true, "calc": true, "clamp": true, "min": true, "max": true,
	"rgb": true, "rgba": true, "hsl": true, "hsla": true, "repeat": true, "minmax": true,
}

var cssProperties = map[string]bool{
	"align-items": true, "align-self": true, "justify-content": true, "justify-items": true,
	"display": true, "flex-direction": true, "flex-wrap": true, "grid-template-columns": true,
	"gap": true, "row-gap": true, "column-gap": true,
	"inline-size": true, "max-inline-size": true, "min-inline-size": true,
	"margin-block": true, "margin-block-start": true, "margin-block-end": true,
	"margin-inline": true, "margin-inline-start": true, "margin-inline-end": true,
	"padding": true, "padding-block": true, "padding-block-start": true, "padding-block-end": true,
	"padding-inline": true, "padding-inline-start": true, "padding-inline-end": true,
	"background-color": true, "color": true, "border-color": true, "border-radius": true,
	"border-style": true, "border-width": true, "border-block-start": true, "border-block-end": true,
	"font-family": true, "font-size": true, "font-style": true, "font-weight": true,
	"letter-spacing": true, "line-height": true, "text-align": true, "text-decoration": true, "text-transform": true,
}

func scopeCSS(id, css string) (string, error) {
	if len(css) > 24<<10 {
		return "", ErrPack
	}
	var out strings.Builder
	for css = strings.TrimSpace(css); css != ""; css = strings.TrimSpace(css) {
		selectors, after, ok := strings.Cut(css, "{")
		if !ok {
			return "", ErrPack
		}
		declarations, remaining, ok := strings.Cut(after, "}")
		if !ok {
			return "", ErrPack
		}
		for i, selector := range strings.Split(selectors, ",") {
			selector = strings.TrimSpace(selector)
			if len(selector) > 160 || !packSelector.MatchString(selector) {
				return "", ErrPack
			}
			if i > 0 {
				out.WriteByte(',')
			}
			out.WriteString("[data-public-theme='" + id + "'] " + strings.Join(strings.Fields(selector), " "))
		}
		out.WriteByte('{')
		for _, declaration := range strings.Split(declarations, ";") {
			declaration = strings.TrimSpace(declaration)
			if declaration == "" {
				continue
			}
			property, value, ok := strings.Cut(declaration, ":")
			property, value = strings.TrimSpace(property), strings.TrimSpace(value)
			if !ok || !cssProperties[property] || !packValue.MatchString(value) ||
				strings.Contains(value, "/*") || strings.Contains(value, "*/") {
				return "", ErrPack
			}
			for _, match := range cssFunction.FindAllStringSubmatch(value, -1) {
				if !cssFunctions[match[1]] {
					return "", ErrPack
				}
			}
			out.WriteString(property + ":" + value + ";")
		}
		out.WriteString("}\n")
		css = remaining
	}
	return out.String(), nil
}
