package content

import (
	"strings"

	"github.com/mbougarne/modawana/backend/internal/media"
)

// SEO is an optional override of the metadata derived from a saved revision.
type ProductSEO struct {
	SKU   string `json:"sku"`
	Brand string `json:"brand,omitempty"`
}

type SEO struct {
	Product       *ProductSEO `json:"product,omitempty"`
	Title         string      `json:"title,omitempty"`
	Description   string      `json:"description,omitempty"`
	SocialImageID string      `json:"social_image_id,omitempty"`
	NoIndex       bool        `json:"no_index,omitempty"`
	Schema        string      `json:"schema,omitempty"`
}

func (s *SEO) valid(kind string) bool {
	if s == nil {
		return true
	}
	if !strOK(s.Title, 120, true) || !strOK(s.Description, 320, true) ||
		strings.ContainsAny(s.Title+s.Description, "\r\n\t") ||
		(s.SocialImageID != "" && !media.ID.MatchString(s.SocialImageID)) {
		return false
	}
	if s.Schema != "product" && s.Product != nil {
		return false
	}
	switch s.Schema {
	case "product":
		return kind == "page" && s.Product != nil && strOK(s.Product.SKU, 80, false) && strOK(s.Product.Brand, 100, true) &&
			!strings.ContainsAny(s.Product.SKU+s.Product.Brand, "\r\n\t")
	case "", "none", "web_page":
		return true
	case "article":
		return kind == "article"
	default:
		return false
	}
}
