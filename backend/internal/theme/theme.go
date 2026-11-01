// Package theme renders public pages from embed-compiled Go templates: the
// first built-in theme of ADR 0002. Templates receive only safe view models
// assembled by the HTTP host — resolved labels, public URLs, and host-rendered
// HTML fragments. They hold no database handles, no secrets, and no draft or
// gated content: a template can never leak what the view model does not
// contain. Themes contain no JavaScript; interactivity is host-owned island
// bundles referenced from the host-rendered head.
package theme

import (
	"embed"
	"html/template"
	"io"
)

//go:embed templates/*.tmpl
var files embed.FS

var templates = template.Must(template.ParseFS(files, "templates/*.tmpl"))

// Link is one resolved navigation or tag destination.
type Link struct {
	Label   string
	URL     string
	Current bool
}

// Language is one entry of the language menu. URL points at the published
// translation, or at that language's home page when the translation is not
// published (the label then says so explicitly).
type Language struct {
	Code    string
	Label   string
	URL     string
	Dir     string
	Current bool
}

// Brand mirrors the owner's logo configuration: wordmark (the last "." takes
// the primary colour), monogram, or an uploaded image.
type Brand struct {
	Kind     string // "wordmark", "monogram" or "image"
	Text     string // full wordmark when there is no highlighted dot
	Pre, Suf string // wordmark split around the highlighted dot
	Split    bool
	Monogram string
	ImageURL string
	ImageAlt string
	HomeURL  string
}

// BioSegment is author-bio text with the **bold** marks already parsed.
type BioSegment struct {
	Text string
	Bold bool
}

// Article is the reader view of one published article.
type Article struct {
	StickerLabel    string
	CategoryURL     string
	Title           string
	Summary         string
	AuthorName      string
	PortraitURL     string
	Monogram        string
	DateISO         string
	DateText        string
	ReadingEstimate string
	StaleNotice     string
	LockedTitle     string // nonempty renders the safe locked teaser instead of a body
	LockedHelp      string
	CoverURL        string
	CoverWidth      int
	CoverHeight     int
	Body            template.HTML
	Tags            []Link
	WrittenBy       string
	Bio             []BioSegment
	MoreLabel       string
	MoreURL         string
	Arrow           string
}

// Footer is the static public footer: configured text or the default
// email call to action, copyright, and safe visible destinations.
type Footer struct {
	Text      string
	Question  string
	SayHi     string
	Email     string
	Copyright string
	NavLabel  string
	Links     []Link
}

// ArticlePage is everything the article template may render.
type ArticlePage struct {
	Locale        string
	Direction     string
	Head          template.HTML // host-rendered metadata, styles and island scripts
	SkipLabel     string
	Brand         Brand
	Nav           []Link
	MenuNav       []Link // the phone menu adds the home entry
	NavLabel      string
	SearchLabel   string
	SearchURL     string
	LanguageLabel string
	LanguageShort string
	Languages     []Language
	MenuLabel     string
	Article       Article
	Footer        Footer
}

// RenderArticle writes the complete article document.
func RenderArticle(w io.Writer, page ArticlePage) error {
	return templates.ExecuteTemplate(w, "article.tmpl", page)
}
