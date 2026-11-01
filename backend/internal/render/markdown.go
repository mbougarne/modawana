package render

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

func newMarkdown(opts Options) goldmark.Markdown {
	manifest := map[string]Media{}
	for _, m := range opts.Media {
		if mediaID.MatchString(m.ID) {
			manifest[m.ID] = m
		}
	}
	presentations := map[string]Presentation{}
	for _, p := range opts.Presentations {
		if _, ok := presentations[p.MediaID]; !ok {
			presentations[p.MediaID] = p
		}
	}
	return goldmark.New(
		goldmark.WithExtensions(
			extension.Strikethrough,
			// align attributes instead of inline styles: the output must stay
			// embeddable under the strict no-inline-style CSP.
			extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
			extension.TaskList,
			extension.NewLinkify(extension.WithLinkifyAllowedProtocols([]string{"http:", "https:"})),
			&mathExtension{},
		),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(
			util.Prioritized(&nodeRenderer{opts: opts, manifest: manifest, presentations: presentations}, 100),
		)),
	)
}

// nodeRenderer overrides the constructs the Markdown reader contract renders
// specially: the link policy, manifest-controlled media, code, tables, math.
type nodeRenderer struct {
	opts          Options
	manifest      map[string]Media
	presentations map[string]Presentation
}

func (r *nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindAutoLink, r.renderAutoLink)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindFencedCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
	reg.Register(east.KindTable, r.renderTable)
	reg.Register(kindMathInline, r.renderMathInline)
	reg.Register(kindMathBlock, r.renderMathBlock)
	// renderBody rejects raw HTML before rendering; these are defense in depth.
	reg.Register(ast.KindRawHTML, rejectRawHTML)
	reg.Register(ast.KindHTMLBlock, rejectRawHTML)
}

func rejectRawHTML(util.BufWriter, []byte, ast.Node, bool) (ast.WalkStatus, error) {
	return ast.WalkStop, ErrRawHTML
}

// mediaFor resolves a destination against the authorized manifest.
func (r *nodeRenderer) mediaFor(destination string) (Media, bool) {
	match := mediaPath.FindStringSubmatch(destination)
	if match == nil {
		return Media{}, false
	}
	m, ok := r.manifest[match[1]]
	return m, ok
}

func (r *nodeRenderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	destination := normalizeURI(string(n.Destination))
	if strings.HasPrefix(destination, "/media/") {
		m, ok := r.mediaFor(destination)
		switch {
		case ok && entering:
			_, _ = w.WriteString(`<a href="/media/` + m.ID + `"`)
			if m.Kind == "pdf" {
				_, _ = w.WriteString(` download=""`)
			}
			_ = w.WriteByte('>')
		case ok:
			_, _ = w.WriteString("</a>")
		case entering:
			_, _ = w.WriteString("<span>")
		default:
			_, _ = w.WriteString(" (" + escape(r.opts.Labels.MediaUnavailable) + ")</span>")
		}
		return ast.WalkContinue, nil
	}
	if url, ok := safeLink(destination); ok {
		if entering {
			_, _ = w.WriteString(`<a href="` + escape(url) + `" rel="noopener noreferrer">`)
		} else {
			_, _ = w.WriteString("</a>")
		}
	} else if entering {
		_, _ = w.WriteString("<span>")
	} else {
		_, _ = w.WriteString("</span>")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	m, ok := r.mediaFor(normalizeURI(string(n.Destination)))
	if !ok || m.Kind != "image" {
		_, _ = w.WriteString("<span>" + escape(r.opts.Labels.MediaUnavailable) + "</span>")
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<img src="/media/` + m.ID + `" alt="` + escape(textOf(n, source)) + `" loading="lazy"`)
	if m.Width > 0 {
		_, _ = w.WriteString(` width="` + strconv.Itoa(m.Width) + `"`)
	}
	if m.Height > 0 {
		_, _ = w.WriteString(` height="` + strconv.Itoa(m.Height) + `"`)
	}
	_ = w.WriteByte('>')
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderAutoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.AutoLink)
	url := string(n.URL(source))
	if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(url, "mailto:") {
		url = "mailto:" + url
	}
	label := escape(string(n.Label(source)))
	if safe, ok := safeLink(normalizeURI(url)); ok {
		_, _ = w.WriteString(`<a href="` + escape(safe) + `" rel="noopener noreferrer">` + label + "</a>")
	} else {
		_, _ = w.WriteString("<span>" + label + "</span>")
	}
	return ast.WalkContinue, nil
}

// standaloneVideo detects a paragraph whose only child links to manifest
// video; the reader renders it as a media figure instead of a paragraph.
func (r *nodeRenderer) standaloneVideo(node ast.Node) (*ast.Link, Media, bool) {
	if node.ChildCount() != 1 {
		return nil, Media{}, false
	}
	link, ok := node.FirstChild().(*ast.Link)
	if !ok {
		return nil, Media{}, false
	}
	m, found := r.mediaFor(normalizeURI(string(link.Destination)))
	if !found || m.Kind != "video" {
		return nil, Media{}, false
	}
	return link, m, true
}

func (r *nodeRenderer) renderParagraph(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	link, m, ok := r.standaloneVideo(node)
	if !ok {
		if entering {
			_, _ = w.WriteString("<p>")
		} else {
			_, _ = w.WriteString("</p>\n")
		}
		return ast.WalkContinue, nil
	}
	if !entering {
		return ast.WalkContinue, nil
	}
	r.writeVideoFigure(w, source, link, m)
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) writeVideoFigure(w util.BufWriter, source []byte, link *ast.Link, m Media) {
	presentation := r.presentations[m.ID]
	_, _ = w.WriteString(`<figure class="vb-media"><video controls="" playsinline="" preload="metadata" src="/media/` +
		m.ID + `" title="` + escape(textOf(link, source)) + `"`)
	if poster, ok := r.manifest[presentation.PosterID]; ok && poster.Kind == "image" {
		_, _ = w.WriteString(` poster="/media/` + poster.ID + `"`)
	}
	_ = w.WriteByte('>')
	if track, ok := r.manifest[presentation.TrackID]; ok && track.Kind == "captions" {
		_, _ = w.WriteString(`<track kind="captions" src="/media/` + track.ID + `" srclang="` +
			escape(r.opts.Locale) + `" label="` + escape(r.opts.Labels.Captions) + `" default="">`)
	}
	_, _ = w.WriteString("</video>")
	if presentation.Caption != "" {
		_, _ = w.WriteString("<figcaption>" + escape(presentation.Caption) + "</figcaption>")
	}
	if presentation.Transcript != "" {
		_, _ = w.WriteString("<details><summary>" + escape(r.opts.Labels.Transcript) + "</summary><p>" +
			escape(presentation.Transcript) + "</p></details>")
	}
	_, _ = w.WriteString("</figure>\n")
}

func (r *nodeRenderer) renderCodeBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<figure class="vb-code" dir="ltr"><pre><code`)
	if fenced, ok := node.(*ast.FencedCodeBlock); ok {
		if language := fenced.Language(source); language != nil {
			_, _ = w.WriteString(` class="language-` + escape(string(language)) + `"`)
		}
	}
	_ = w.WriteByte('>')
	var code bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		code.Write(line.Value(source))
	}
	_, _ = w.WriteString(escape(strings.TrimSuffix(code.String(), "\n")))
	_, _ = w.WriteString("</code></pre></figure>\n")
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderCodeSpan(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</code>")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<code dir="ltr">`)
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		value := c.(*ast.Text).Segment.Value(source)
		if bytes.HasSuffix(value, []byte("\n")) {
			_, _ = w.WriteString(escape(string(value[:len(value)-1])) + " ")
		} else {
			_, _ = w.WriteString(escape(string(value)))
		}
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderTable(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString(`<div class="vb-reader-table" role="region" aria-label="` +
			escape(r.opts.Labels.Table) + `" tabindex="0"><table>` + "\n")
	} else {
		_, _ = w.WriteString("</table></div>\n")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderMathInline(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*mathInline)
		_, _ = w.WriteString(`<span class="vb-math" data-math="inline">` + escape(string(n.value)) + "</span>")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderMathBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var tex bytes.Buffer
	lines := node.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		tex.Write(line.Value(source))
	}
	_, _ = w.WriteString(`<div class="vb-math" data-math="display">` +
		escape(strings.TrimSuffix(tex.String(), "\n")) + "</div>\n")
	return ast.WalkContinue, nil
}

// textOf collects the plain text of inline children, like the reader's
// codeText helper over rendered children.
func textOf(node ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch t := n.(type) {
			case *ast.Text:
				b.Write(t.Segment.Value(source))
				if !t.HardLineBreak() && t.SoftLineBreak() {
					b.WriteByte('\n')
				}
			case *ast.String:
				b.Write(t.Value)
			}
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func escape(s string) string {
	return htmlEscaper.Replace(s)
}
