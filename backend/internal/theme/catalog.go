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

// Appearance is the validated presentation applied to host-rendered pages.
type Appearance struct {
	ID, CSS, BannerURL string
	Layout             []string
}

func Lookup(id string) (Appearance, bool) {
	switch id {
	case Classic:
		return Appearance{ID: Classic}, true
	case Folio:
		return Appearance{ID: Folio, CSS: folioCSS}, true
	case Studio:
		return Appearance{ID: Studio, CSS: studioCSS}, true
	default:
		return Appearance{ID: Classic}, false
	}
}
