// Package render turns stored Markdown into sanitized HTML on the server with
// exact parity to the shared browser reader (docs/markdown-reader.md and
// frontend/src/features/markdown/MarkdownReader.tsx). The parity fixture suite
// under testdata/fixtures is rendered by both pipelines and compared by
// scripts/markdown-parity.mjs; change either side only together with it.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MaxBody mirrors the content service cap on stored Markdown.
const MaxBody = 1 << 20

var ErrTooLarge = errors.New("markdown exceeds the size limit")
var ErrRawHTML = errors.New("markdown contains raw HTML")
var ErrInvalidText = errors.New("markdown is not valid text")

var mediaPath = regexp.MustCompile(`^/media/([0-9a-f]{32})$`)
var mediaID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var controlOrBackslash = regexp.MustCompile(`[\x00-\x20\x7F\\]`)
var mailtoAddress = regexp.MustCompile(`^[^?@\s\p{Z}\x{FEFF}]+@[^?@\s\p{Z}\x{FEFF}]+$`)

// Media is one entry of the authorized manifest for a revision.
type Media struct {
	ID     string
	Kind   string
	Width  int
	Height int
}

// Presentation carries the authored poster/captions/transcript for one video.
type Presentation struct {
	MediaID    string
	PosterID   string
	TrackID    string
	Caption    string
	Transcript string
}

// Labels are the localized reader strings; the caller resolves locale fallback.
type Labels struct {
	Takeaway         string
	Table            string
	MediaUnavailable string
	Captions         string
	Transcript       string
}

// Options configure one rendering pass.
type Options struct {
	// Locale is the page locale (en, ar, fr); "ar" renders dir="rtl".
	Locale        string
	Media         []Media
	Presentations []Presentation
	Labels        Labels
}

// Result is sanitized HTML, safe to interpolate into html/template output.
type Result struct {
	HTML template.HTML
	// HasMath reports marked TeX elements that need the client-side KaTeX
	// island; pages without math ship no rendering JavaScript.
	HasMath bool
}

// Fragment renders one Markdown body without the reader wrapper.
func Fragment(markdown string, opts Options) (Result, error) {
	body, hasMath, err := renderBody(markdown, opts)
	if err != nil {
		return Result{}, err
	}
	return Result{HTML: template.HTML(body), HasMath: hasMath}, nil
}

// Document renders the full reader structure the browser pipeline produces:
// the vb-reader wrapper, an optional authored takeaway aside, and the body.
func Document(markdown, takeaway string, opts Options) (Result, error) {
	dir := "ltr"
	if opts.Locale == "ar" {
		dir = "rtl"
	}
	body, hasMath, err := renderBody(markdown, opts)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	b.WriteString(`<div class="vb-reader" dir="` + dir + `">`)
	if takeaway != "" {
		aside, asideMath, err := renderBody(takeaway, opts)
		if err != nil {
			return Result{}, err
		}
		hasMath = hasMath || asideMath
		b.WriteString(`<aside class="vb-takeaway"><h2>`)
		writeEscaped(&b, opts.Labels.Takeaway)
		b.WriteString(`</h2><div class="vb-reader" dir="` + dir + `"><article class="vb-prose" lang="`)
		writeEscaped(&b, opts.Locale)
		b.WriteString(`">` + aside + `</article></div></aside>`)
	}
	b.WriteString(`<article class="vb-prose" lang="`)
	writeEscaped(&b, opts.Locale)
	b.WriteString(`">` + body + `</article></div>`)
	return Result{HTML: template.HTML(b.String()), HasMath: hasMath}, nil
}

func renderBody(markdown string, opts Options) (string, bool, error) {
	if len(markdown) > MaxBody {
		return "", false, ErrTooLarge
	}
	if !utf8.ValidString(markdown) || strings.ContainsRune(markdown, 0) {
		return "", false, ErrInvalidText
	}
	source := []byte(markdown)
	md := newMarkdown(opts)
	doc := md.Parser().Parse(text.NewReader(source))
	hasMath := false
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkStop, ErrRawHTML
		case *mathInline, *mathBlock:
			hasMath = true
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		return "", false, err
	}
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return "", false, err
	}
	return strings.TrimSuffix(buf.String(), "\n"), hasMath, nil
}

// SafeLink exposes the reader link policy to the theme host for
// configuration-sourced destinations such as footer profile links.
func SafeLink(value string) (string, bool) { return safeLink(value) }

// safeLink ports the reader's link policy exactly: no control bytes or
// backslashes raw or decoded, no scheme-relative URLs, same-site paths and
// fragments, credential-free http(s), and plain mailto addresses.
func safeLink(value string) (string, bool) {
	if value == "" || controlOrBackslash.MatchString(value) {
		return "", false
	}
	decoded, ok := decodeURIComponent(value)
	if !ok {
		return "", false
	}
	if controlOrBackslash.MatchString(decoded) || strings.HasPrefix(decoded, "//") {
		return "", false
	}
	if strings.HasPrefix(value, "#") || (strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")) {
		return value, true
	}
	scheme, rest, found := strings.Cut(value, ":")
	if !found || scheme == "" {
		return "", false
	}
	switch strings.ToLower(scheme) {
	case "http", "https":
		if host, okHost := authorityHost(rest); okHost && host != "" {
			return value, true
		}
	case "mailto":
		if mailtoAddress.MatchString(rest) {
			return value, true
		}
	}
	return "", false
}

// authorityHost accepts only credential-free http(s) URLs, like the browser's
// URL parser combined with the reader's username/password rejection.
func authorityHost(rest string) (string, bool) {
	if !strings.HasPrefix(rest, "//") {
		return "", false
	}
	authority := rest[2:]
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if strings.Contains(authority, "@") {
		return "", false
	}
	return authority, true
}

// decodeURIComponent mirrors the JavaScript function: percent sequences must
// be well formed and decode to valid UTF-8.
func decodeURIComponent(value string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		if i+2 >= len(value) || !isHexDigit(value[i+1]) || !isHexDigit(value[i+2]) {
			return "", false
		}
		b.WriteByte(hexValue(value[i+1])<<4 | hexValue(value[i+2]))
		i += 2
	}
	decoded := b.String()
	if !utf8.ValidString(decoded) {
		return "", false
	}
	return decoded, true
}

// normalizeURI ports micromark's URL sanitizer, which the browser pipeline
// applies to every destination before the link policy runs.
func normalizeURI(value string) string {
	const safe = "!#$&'()*+,-./0123456789:;=?@ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz~"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c == '%' && i+2 < len(value) && isAlphanumeric(value[i+1]) && isAlphanumeric(value[i+2]) {
			b.WriteByte(c)
			continue
		}
		if c < 0x80 && strings.IndexByte(safe, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isAlphanumeric(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func hexValue(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func writeEscaped(b *strings.Builder, s string) {
	htmlEscaper.WriteString(b, s)
}
