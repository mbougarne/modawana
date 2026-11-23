package content

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/mbougarne/modawana/backend/internal/media"
	"github.com/mbougarne/modawana/backend/internal/render"
)

const (
	BlockBodyFormat    = "blocks"
	MarkdownBodyFormat = "markdown"
	MaxPageBlockBytes  = 256 << 10
	maxPageBlocks      = 100
	maxBlockDepth      = 3
)

var blockID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

type pageBlockDocument struct {
	Version int         `json:"version"`
	Blocks  []pageBlock `json:"blocks"`
}

type pageBlock struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	Properties json.RawMessage `json:"properties"`
}

type blockReferences struct {
	media  map[string]bool
	images map[string]bool
}

type blockValidation struct {
	refs         blockReferences
	ids          map[string]bool
	rewriteMedia map[string]string
	rewrites     int
}

type markdownBlock struct {
	Markdown string `json:"markdown"`
}
type headingBlock struct {
	Text  string `json:"text"`
	Level int    `json:"level"`
}
type imageBlock struct {
	MediaID string `json:"media_id"`
	Alt     string `json:"alt"`
	Caption string `json:"caption,omitempty"`
}
type buttonBlock struct {
	Label string `json:"label"`
	Href  string `json:"href"`
}
type columnsBlock struct {
	Columns [][]pageBlock `json:"columns"`
}
type heroBlock struct {
	Heading string `json:"heading"`
	Text    string `json:"text,omitempty"`
	MediaID string `json:"media_id,omitempty"`
	Alt     string `json:"alt,omitempty"`
}
type callToActionBlock struct {
	Heading string `json:"heading"`
	Text    string `json:"text,omitempty"`
	buttonBlock
}
type contentListBlock struct {
	Section string `json:"section"`
	Limit   int    `json:"limit"`
}

func draftBlockReferences(in DraftInput) (blockReferences, error) {
	switch in.BodyFormat {
	case "", MarkdownBodyFormat:
		if len(in.PageBlocks) == 0 {
			return blockReferences{}, nil
		}
	case BlockBodyFormat:
		if in.Kind == "page" && in.Markdown == "" {
			_, refs, err := parsePageBlocks(in.PageBlocks, nil)
			return refs, err
		}
	}
	return blockReferences{}, ErrInvalid
}

func parsePageBlocks(body []byte, remap map[string]string) (pageBlockDocument, blockReferences, error) {
	var doc pageBlockDocument
	if len(body) > MaxPageBlockBytes || !utf8.Valid(body) || !uniqueBlockJSON(json.NewDecoder(bytes.NewReader(body)), 0) ||
		bundle.Decode(body, &doc) != nil || doc.Version != 1 || doc.Blocks == nil {
		return doc, blockReferences{}, ErrInvalid
	}
	w := blockValidation{
		refs:         blockReferences{media: map[string]bool{}, images: map[string]bool{}},
		ids:          map[string]bool{},
		rewriteMedia: remap,
	}
	if err := w.walk(doc.Blocks, 1); err != nil {
		return doc, blockReferences{}, err
	}
	return doc, w.refs, nil
}

// Keep ambiguous member names and unbounded nesting out of stored documents.
// The typed decoders check the allowed members and their values separately.
func uniqueBlockJSON(decoder *json.Decoder, depth int) bool {
	if depth > 16 {
		return false
	}
	token, err := decoder.Token()
	if err != nil || token == nil {
		return false
	}
	if token != json.Delim('{') && token != json.Delim('[') {
		return true
	}
	var seen map[string]bool
	if token == json.Delim('{') {
		seen = map[string]bool{}
	}
	for decoder.More() {
		if seen != nil {
			member, err := decoder.Token()
			name, ok := member.(string)
			if err != nil || !ok || name != strings.ToLower(name) || seen[name] {
				return false
			}
			seen[name] = true
		}
		if !uniqueBlockJSON(decoder, depth+1) {
			return false
		}
	}
	_, err = decoder.Token()
	return err == nil
}

// ValidatePageBlocks checks the schema of a stored block artifact independently of SQLite.
func ValidatePageBlocks(body []byte) error {
	_, _, err := parsePageBlocks(body, nil)
	return err
}

// RemapPageBlockMedia imports a document while preserving its block IDs and order.
// Only managed media references change when the destination assigns new media IDs.
func RemapPageBlockMedia(body []byte, ids map[string]string) (json.RawMessage, error) {
	if len(ids) == 0 {
		return body, ValidatePageBlocks(body)
	}
	doc, refs, err := parsePageBlocks(body, ids)
	if err != nil {
		return nil, err
	}
	if len(refs.media) == 0 {
		return body, nil
	}
	return encodeBlockJSON(doc)
}

func encodeBlockJSON(value any) (json.RawMessage, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
}

func (w *blockValidation) walk(blocks []pageBlock, depth int) error {
	if depth > maxBlockDepth || len(blocks) > maxPageBlocks-len(w.ids) {
		return ErrInvalid
	}
	for i := range blocks {
		b := &blocks[i]
		props := bytes.TrimSpace(b.Properties)
		if b.Version != 1 || !blockID.MatchString(b.ID) || w.ids[b.ID] || len(w.ids) == maxPageBlocks ||
			len(props) == 0 || props[0] != '{' {
			return ErrInvalid
		}
		w.ids[b.ID] = true
		updated, err := w.properties(b.Type, props, depth)
		if err != nil {
			return err
		}
		if updated != nil {
			b.Properties = updated
			w.rewrites++
		}
	}
	return nil
}

func (w *blockValidation) properties(kind string, raw []byte, depth int) (json.RawMessage, error) {
	switch kind {
	case "core.markdown":
		var p markdownBlock
		if bundle.Decode(raw, &p) != nil || !strOK(p.Markdown, 64<<10, false) || len(p.Markdown) > 64<<10 {
			return nil, ErrInvalid
		}
		rewritten, err := w.markdown(p.Markdown)
		if err != nil {
			return nil, err
		}
		return replaceBlockString(raw, "markdown", p.Markdown, rewritten)
	case "core.heading":
		var p headingBlock
		if bundle.Decode(raw, &p) != nil || !strOK(p.Text, 200, false) || p.Level < 2 || p.Level > 6 {
			return nil, ErrInvalid
		}
		return nil, nil
	case "core.image":
		var p imageBlock
		if bundle.Decode(raw, &p) != nil || !strOK(p.Alt, 400, false) || !strOK(p.Caption, 500, true) {
			return nil, ErrInvalid
		}
		id, err := w.reference(p.MediaID, true)
		if err != nil {
			return nil, err
		}
		return replaceBlockString(raw, "media_id", p.MediaID, id)
	case "core.button":
		var p buttonBlock
		if bundle.Decode(raw, &p) != nil {
			return nil, ErrInvalid
		}
		return w.button(raw, p)
	case "core.columns":
		var p columnsBlock
		if bundle.Decode(raw, &p) != nil || len(p.Columns) < 2 || len(p.Columns) > 3 {
			return nil, ErrInvalid
		}
		before := w.rewrites
		for _, column := range p.Columns {
			if column == nil {
				return nil, ErrInvalid
			}
			if err := w.walk(column, depth+1); err != nil {
				return nil, err
			}
		}
		if before != w.rewrites {
			return encodeBlockJSON(p)
		}
		return nil, nil
	case "core.hero":
		var p heroBlock
		if bundle.Decode(raw, &p) != nil || !strOK(p.Heading, 200, false) || !strOK(p.Text, 1000, true) {
			return nil, ErrInvalid
		}
		if p.MediaID == "" && p.Alt == "" {
			return nil, nil
		}
		if !strOK(p.Alt, 400, false) {
			return nil, ErrInvalid
		}
		id, err := w.reference(p.MediaID, true)
		if err != nil {
			return nil, err
		}
		return replaceBlockString(raw, "media_id", p.MediaID, id)
	case "core.call_to_action":
		var p callToActionBlock
		if bundle.Decode(raw, &p) != nil || !strOK(p.Heading, 200, false) || !strOK(p.Text, 1000, true) {
			return nil, ErrInvalid
		}
		return w.button(raw, p.buttonBlock)
	case "core.content_list":
		var p contentListBlock
		if bundle.Decode(raw, &p) != nil || p.Limit < 1 || p.Limit > 12 {
			return nil, ErrInvalid
		}
		switch p.Section {
		case "writing", "pages", "learning", "docs":
			return nil, nil
		}
	}
	return nil, ErrInvalid
}

func (w *blockValidation) markdown(body string) (string, error) {
	source := []byte(body)
	edits := map[*byte]string{}
	err := walkMarkdownReferences(source, func(id string, destination []byte) error {
		next, err := w.reference(id, false)
		if err == nil && next != id {
			edits[&destination[0]] = "/media/" + next
		}
		return err
	})
	if err != nil || len(edits) == 0 {
		return body, err
	}
	// Goldmark supplies canonical destinations as slices of the parsed source.
	// Match their starts so identical text in prose and code stays intact.
	for i := range source {
		if replacement, ok := edits[&source[i]]; ok {
			copy(source[i:], replacement)
			delete(edits, &source[i])
		}
	}
	if len(edits) != 0 {
		return "", ErrInvalid
	}
	return string(source), nil
}

func replaceBlockString(raw []byte, key, original, updated string) (json.RawMessage, error) {
	if original == updated {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	value, err := encodeBlockJSON(updated)
	if err != nil {
		return nil, err
	}
	fields[key] = value
	return encodeBlockJSON(fields)
}

func (w *blockValidation) reference(id string, image bool) (string, error) {
	if !media.ID.MatchString(id) {
		return "", ErrInvalid
	}
	if w.rewriteMedia != nil {
		id = w.rewriteMedia[id]
		if !media.ID.MatchString(id) {
			return "", ErrInvalid
		}
	}
	w.refs.media[id] = true
	if image {
		w.refs.images[id] = true
	}
	return id, nil
}

func (w *blockValidation) button(raw []byte, p buttonBlock) (json.RawMessage, error) {
	if !strOK(p.Label, 80, false) || !strOK(p.Href, 2048, false) {
		return nil, ErrInvalid
	}
	if _, ok := render.SafeLink(p.Href); !ok {
		return nil, ErrInvalid
	}
	target, err := url.Parse(p.Href)
	if err != nil {
		return nil, ErrInvalid
	}
	if target.Scheme == "" && target.Host == "" && strings.HasPrefix(target.Path, "/media/") {
		id, err := w.reference(strings.TrimPrefix(target.Path, "/media/"), false)
		if err != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || target.RawPath != "" {
			return nil, ErrInvalid
		}
		return replaceBlockString(raw, "href", p.Href, "/media/"+id)
	}
	return nil, nil
}
