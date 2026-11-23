package content

import (
	"bytes"
	"encoding/json"
	"html/template"
	"strings"
)

type PageBlockLink struct {
	Title string
	URL   string
}

type PageBlockRenderOptions struct {
	Media       []PublicMedia
	Unavailable string
	Empty       string
	Markdown    func(string) (template.HTML, error)
	Links       func(section string, limit int) ([]PageBlockLink, error)
}

type renderedBlock struct {
	ID, Kind, Text, Heading, Caption, Alt, Label, Href string
	Level                                              int
	Media                                              *PublicMedia
	HTML                                               template.HTML
	Columns                                            [][]renderedBlock
	Links                                              []PageBlockLink
	Fallback                                           string
}

var pageBlockHTML = template.Must(template.New("page-blocks").Parse(`
<div class="page-blocks">{{template "blocks" .}}</div>
{{define "blocks"}}{{range .}}
<div class="page-block page-block-{{.Kind}}" data-block-id="{{.ID}}">
{{if .Fallback}}<p class="public-notice">{{.Fallback}}</p>
{{else if eq .Kind "markdown"}}{{.HTML}}
{{else if eq .Kind "heading"}}
{{if eq .Level 2}}<h2 dir="auto">{{.Text}}</h2>{{else if eq .Level 3}}<h3 dir="auto">{{.Text}}</h3>{{else if eq .Level 4}}<h4 dir="auto">{{.Text}}</h4>{{else if eq .Level 5}}<h5 dir="auto">{{.Text}}</h5>{{else}}<h6 dir="auto">{{.Text}}</h6>{{end}}
{{else if eq .Kind "image"}}<figure>{{template "image" .}}{{if .Caption}}<figcaption dir="auto">{{.Caption}}</figcaption>{{end}}</figure>
{{else if eq .Kind "button"}}{{template "button" .}}
{{else if eq .Kind "columns"}}{{range .Columns}}<div class="page-block-column">{{template "blocks" .}}</div>{{end}}
{{else if eq .Kind "hero"}}<section><h2 dir="auto">{{.Heading}}</h2>{{if .Text}}<p dir="auto">{{.Text}}</p>{{end}}{{if .Media}}{{template "image" .}}{{end}}</section>
{{else if eq .Kind "call_to_action"}}<section><h2 dir="auto">{{.Heading}}</h2>{{if .Text}}<p dir="auto">{{.Text}}</p>{{end}}{{template "button" .}}</section>
{{else if eq .Kind "content_list"}}<ul>{{range .Links}}<li><a href="{{.URL}}"><bdi>{{.Title}}</bdi></a></li>{{end}}</ul>
{{end}}</div>{{end}}{{end}}
{{define "image"}}<img src="{{.Media.URL}}" alt="{{.Alt}}" width="{{.Media.Width}}" height="{{.Media.Height}}" loading="lazy" decoding="async">{{end}}
{{define "button"}}<a class="vb-btn" href="{{.Href}}" dir="auto">{{.Label}}</a>{{end}}
`))

// RenderPageBlocks renders only validated core blocks through host-owned HTML.
func RenderPageBlocks(body []byte, options PageBlockRenderOptions) (template.HTML, error) {
	doc, _, err := parsePageBlocks(body, nil)
	if err != nil {
		return "", err
	}
	mediaByID := make(map[string]PublicMedia, len(options.Media))
	for _, m := range options.Media {
		mediaByID[m.ID] = m
	}
	r := pageBlockRenderer{options, mediaByID}
	blocks, err := r.blocks(doc.Blocks)
	if err != nil {
		return "", err
	}
	var html bytes.Buffer
	if err := pageBlockHTML.Execute(&html, blocks); err != nil {
		return "", err
	}
	return template.HTML(html.String()), nil
}

type pageBlockRenderer struct {
	PageBlockRenderOptions
	media map[string]PublicMedia
}

func (r pageBlockRenderer) blocks(blocks []pageBlock) ([]renderedBlock, error) {
	out := make([]renderedBlock, 0, len(blocks))
	for _, b := range blocks {
		view := renderedBlock{ID: b.ID, Kind: "unavailable"}
		if b.Version != 1 || !knownPageBlock(b.Type) {
			view.Fallback = r.Unavailable
		} else {
			view.Kind = strings.TrimPrefix(b.Type, "core.")
			if err := r.block(&view, b.Properties); err != nil {
				return nil, err
			}
		}
		out = append(out, view)
	}
	return out, nil
}

func (r pageBlockRenderer) block(v *renderedBlock, props json.RawMessage) error {
	// All property shapes were checked by parsePageBlocks above.
	switch v.Kind {
	case "markdown":
		var p markdownBlock
		_ = json.Unmarshal(props, &p)
		if err := walkMarkdownReferences([]byte(p.Markdown), func(id string, _ []byte) error {
			if _, ok := r.media[id]; !ok {
				v.Fallback = r.Unavailable
			}
			return nil
		}); err != nil {
			return err
		}
		if v.Fallback == "" {
			var err error
			v.HTML, err = r.Markdown(p.Markdown)
			return err
		}
	case "heading":
		var p headingBlock
		_ = json.Unmarshal(props, &p)
		v.Text, v.Level = p.Text, p.Level
	case "image":
		var p imageBlock
		_ = json.Unmarshal(props, &p)
		v.Caption = p.Caption
		r.image(v, p.MediaID, p.Alt)
	case "button":
		var p buttonBlock
		_ = json.Unmarshal(props, &p)
		r.button(v, p)
	case "columns":
		var p columnsBlock
		_ = json.Unmarshal(props, &p)
		for _, col := range p.Columns {
			blocks, err := r.blocks(col)
			if err != nil {
				return err
			}
			v.Columns = append(v.Columns, blocks)
		}
	case "hero":
		var p heroBlock
		_ = json.Unmarshal(props, &p)
		v.Heading, v.Text = p.Heading, p.Text
		if p.MediaID != "" {
			r.image(v, p.MediaID, p.Alt)
		}
	case "call_to_action":
		var p callToActionBlock
		_ = json.Unmarshal(props, &p)
		v.Heading, v.Text = p.Heading, p.Text
		r.button(v, p.buttonBlock)
	case "content_list":
		var p contentListBlock
		_ = json.Unmarshal(props, &p)
		links, err := r.Links(p.Section, p.Limit)
		if err != nil {
			return err
		}
		v.Links = links
		if len(links) == 0 {
			v.Fallback = r.Empty
		}
	}
	return nil
}

func (r pageBlockRenderer) image(v *renderedBlock, id, alt string) {
	m, ok := r.media[id]
	if !ok || m.Kind != "image" {
		v.Fallback = r.Unavailable
		return
	}
	v.Media, v.Alt = &m, alt
}

func (r pageBlockRenderer) button(v *renderedBlock, p buttonBlock) {
	if id, managed := strings.CutPrefix(p.Href, "/media/"); managed {
		if _, ok := r.media[id]; !ok {
			v.Fallback = r.Unavailable
			return
		}
	}
	v.Label, v.Href = p.Label, p.Href
}
