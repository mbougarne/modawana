package content

import (
	"context"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/extension"
	"github.com/mbougarne/modawana/backend/internal/storage"
)

type ExtensionPageUse struct {
	Key       string `json:"key"`
	Locale    string `json:"locale"`
	Title     string `json:"title"`
	Draft     bool   `json:"draft"`
	Published bool   `json:"published"`
}

type ExtensionPageUses struct {
	Items []ExtensionPageUse `json:"items"`
	Next  string             `json:"next,omitempty"`
}

// PageUses scans the current draft and published pointers, including pages
// using an unavailable block. The cursor bounds work even for sparse matches.
func (s *Service) PageUses(ctx context.Context, slug, cursor string) (ExtensionPageUses, error) {
	out := ExtensionPageUses{Items: []ExtensionPageUse{}}
	if slug == "core" {
		return out, ErrInvalid
	}
	if !extension.BlockNamespace.MatchString(slug) {
		// Such plugin slugs cannot declare builder blocks.
		return out, nil
	}
	const scanLimit = 50
	rows, err := s.DB.QueryContext(ctx, `
		SELECT v.id,i.key,v.locale,d.title,
		       d.markdown_path,d.sha256,d.byte_size,
		       coalesce(p.markdown_path,''),coalesce(p.sha256,''),coalesce(p.byte_size,0)
		FROM content_versions v JOIN content_items i ON i.id=v.item_id
		JOIN revisions d ON d.id=v.draft_revision_id
		LEFT JOIN revisions p ON p.id=v.published_revision_id
		WHERE i.kind='page' AND v.id>? AND (d.markdown_path LIKE '%.blocks.md' OR p.markdown_path LIKE '%.blocks.md')
		ORDER BY v.id LIMIT ?`, cursor, scanLimit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for n := 0; rows.Next(); n++ {
		if n == scanLimit {
			out.Next = cursor
			break
		}
		var item ExtensionPageUse
		var draft, published storage.File
		if err = rows.Scan(&cursor, &item.Key, &item.Locale, &item.Title,
			&draft.Key, &draft.SHA256, &draft.Size, &published.Key, &published.SHA256, &published.Size); err != nil {
			return out, err
		}
		if item.Draft, err = s.usesExtension(draft, slug); err != nil {
			return out, err
		}
		if draft == published {
			item.Published = item.Draft
		} else if item.Published, err = s.usesExtension(published, slug); err != nil {
			return out, err
		}
		if item.Draft || item.Published {
			out.Items = append(out.Items, item)
		}
	}
	return out, rows.Err()
}

func (s *Service) usesExtension(file storage.File, slug string) (bool, error) {
	if !file.HasPageBlocks() {
		return false, nil
	}
	body, err := s.Files.Read(file)
	if err != nil {
		return false, err
	}
	_, refs, err := parsePageBlocks(body, nil)
	if err != nil {
		return false, err
	}
	for _, b := range refs.extensionBlocks {
		if strings.HasPrefix(b.Type, slug+".") {
			return true, nil
		}
	}
	return false, nil
}
