package theme

import _ "embed"

const (
	Classic = "classic"
	Folio   = "folio"
	Studio  = "studio"
)

//go:embed styles/folio.css
var folioCSS string

//go:embed styles/studio.css
var studioCSS string

// Builtin pairs the public layout identifier with its embedded CSS.
type Builtin struct {
	ID, CSS string
}

func Lookup(id string) (Builtin, bool) {
	switch id {
	case Classic:
		return Builtin{ID: Classic}, true
	case Folio:
		return Builtin{ID: Folio, CSS: folioCSS}, true
	case Studio:
		return Builtin{ID: Studio, CSS: studioCSS}, true
	default:
		return Builtin{ID: Classic}, false
	}
}
