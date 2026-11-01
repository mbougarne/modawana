package portability

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/mbougarne/modawana/backend/internal/bundle"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type notionEdit struct {
	start, end int
	value      string
}

var notionReference = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]\n]+\]:[ \t]*(<[^>\n]*>|[^ \t\r\n]+)`)

// Rewrite only Markdown destinations, preserving code samples and surrounding prose.
// The normal content validator rechecks the rewritten draft before it can commit.
func (a *notionArchive) rewrite(page *NotionPage, mediaIDs map[string]string) (string, error) {
	source := []byte(notionCallouts(page.Markdown))
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	replacements := map[string]string{}
	protected := make([]bool, len(source))
	protect := func(start, end int) {
		for i := start; i < end && i < len(protected); i++ {
			protected[i] = true
		}
	}
	e := ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := node.(type) {
		case *ast.FencedCodeBlock, *ast.CodeBlock:
			lines := node.Lines()
			for i := 0; i < lines.Len(); i++ {
				segment := lines.At(i)
				protect(segment.Start, segment.Stop)
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				if t, ok := child.(*ast.Text); ok {
					protect(t.Segment.Start, t.Segment.Stop)
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.HTMLBlock, *ast.RawHTML:
			return ast.WalkStop, bundle.ErrInvalid
		}
		var destination string
		image := false
		switch n := node.(type) {
		case *ast.Image:
			destination, image = string(n.Destination), true
		case *ast.Link:
			destination = string(n.Destination)
		default:
			return ast.WalkContinue, nil
		}
		u, e := url.Parse(destination)
		if e != nil {
			return ast.WalkStop, bundle.ErrInvalid
		}
		if u.IsAbs() || u.Host != "" || strings.HasPrefix(destination, "/") || strings.HasPrefix(destination, "#") {
			if image || u.Scheme != "" && u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" {
				return ast.WalkStop, bundle.ErrInvalid
			}
			return ast.WalkContinue, nil
		}
		if u.RawQuery != "" || strings.ContainsAny(u.Path, "\\\x00\r\n") {
			return ast.WalkStop, bundle.ErrInvalid
		}
		target := path.Clean(path.Join(path.Dir(page.Path), u.Path))
		if !notionPath(target) {
			return ast.WalkStop, bundle.ErrInvalid
		}
		if p := a.pages[target]; p != nil && !image {
			value := p.URL
			if u.Fragment != "" {
				value += "#" + url.PathEscape(u.Fragment)
			}
			replacements[destination] = value
			return ast.WalkContinue, nil
		}
		f := a.files[target]
		if f == nil || u.Fragment != "" || strings.ToLower(path.Ext(target)) == ".md" {
			return ast.WalkStop, bundle.ErrInvalid
		}
		a.assets[target] = true
		id := mediaIDs[target]
		if mediaIDs == nil {
			id = strings.Repeat("0", 32)
		}
		if id == "" {
			return ast.WalkStop, bundle.ErrInvalid
		}
		replacements[destination] = "/media/" + id
		return ast.WalkContinue, nil
	})
	if e != nil {
		return "", e
	}
	edits := []notionEdit{}
	add := func(start, end int) {
		if start >= end || protected[start] {
			return
		}
		if value, ok := replacements[string(source[start:end])]; ok {
			edits = append(edits, notionEdit{start, end, value})
		}
	}
	for i := 0; i+1 < len(source); i++ {
		if protected[i] || source[i] != ']' || source[i+1] != '(' {
			continue
		}
		start := i + 2
		for start < len(source) && (source[start] == ' ' || source[start] == '\t') {
			start++
		}
		end := start
		if start < len(source) && source[start] == '<' {
			start++
			end = start
			for end < len(source) && source[end] != '>' && source[end] != '\n' {
				end++
			}
		} else {
			depth := 0
			for end < len(source) {
				c := source[end]
				if c == '\\' && end+1 < len(source) {
					end += 2
					continue
				}
				if c == ')' && depth == 0 || c == ' ' || c == '\t' || c == '\r' || c == '\n' {
					break
				}
				if c == '(' {
					depth++
				} else if c == ')' {
					depth--
				}
				end++
			}
		}
		add(start, end)
	}
	for _, match := range notionReference.FindAllSubmatchIndex(source, -1) {
		start, end := match[2], match[3]
		if source[start] == '<' && source[end-1] == '>' {
			start++
			end--
		}
		add(start, end)
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	last := 0
	for _, edit := range edits {
		if edit.start < last {
			continue
		}
		out.Write(source[last:edit.start])
		out.WriteString(edit.value)
		last = edit.end
	}
	out.Write(source[last:])
	return out.String(), nil
}

// Convert only bare Notion callout wrappers and recognizable unquoted C++ types.
// Reparse their Markdown contents through the existing strict validator.
func notionCallouts(markdown string) string {
	source := []byte(markdown)
	root := goldmark.DefaultParser().Parse(text.NewReader(source))
	edits := []notionEdit{}
	cppType := regexp.MustCompile("^<(?:const )?[A-Za-z_][A-Za-z_0-9: ]*>$")
	cppPrefix := regexp.MustCompile("std::[A-Za-z_][A-Za-z_0-9]*$")
	_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if raw, ok := node.(*ast.RawHTML); ok {
			for i := 0; i < raw.Segments.Len(); i++ {
				segment := raw.Segments.At(i)
				value := string(segment.Value(source))
				if cppType.MatchString(value) && cppPrefix.Match(source[:segment.Start]) {
					edits = append(edits, notionEdit{segment.Start, segment.Stop, strings.ReplaceAll(strings.ReplaceAll(value, "<", "&lt;"), ">", "&gt;")})
				}
			}
		}
		if _, ok := node.(*ast.HTMLBlock); !ok {
			return ast.WalkContinue, nil
		}
		lines := node.Lines()
		for i := 0; i < lines.Len(); i++ {
			segment := lines.At(i)
			line := strings.TrimSpace(string(segment.Value(source)))
			if line == "<aside>" || line == "</aside>" {
				edits = append(edits, notionEdit{segment.Start, segment.Stop, "\n"})
			}
		}
		return ast.WalkSkipChildren, nil
	})
	var out bytes.Buffer
	last := 0
	for _, edit := range edits {
		out.Write(source[last:edit.start])
		out.WriteString(edit.value)
		last = edit.end
	}
	out.Write(source[last:])
	return out.String()
}
