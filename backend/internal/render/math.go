package render

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// mathExtension parses the remark-math delimiters the reader supports: $...$
// (and $$...$$) inline spans with code-span padding rules, and $$ fenced
// display blocks. There is no Go KaTeX, so rendering emits the TeX source in
// marked elements for the client-side KaTeX island.
type mathExtension struct{}

func (e *mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(util.Prioritized(&mathBlockParser{}, 850)),
		parser.WithInlineParsers(util.Prioritized(&mathInlineParser{}, 150)),
	)
}

var kindMathInline = ast.NewNodeKind("MathInline")
var kindMathBlock = ast.NewNodeKind("MathBlock")

type mathInline struct {
	ast.BaseInline
	value []byte
}

func (n *mathInline) Kind() ast.NodeKind { return kindMathInline }
func (n *mathInline) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Value": string(n.value)}, nil)
}

type mathBlock struct {
	ast.BaseBlock
	openLength int
	indent     int
}

func (n *mathBlock) Kind() ast.NodeKind { return kindMathBlock }
func (n *mathBlock) IsRaw() bool        { return true }
func (n *mathBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mathInlineParser struct{}

func (p *mathInlineParser) Trigger() []byte { return []byte{'$'} }

func (p *mathInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, startSegment := block.PeekLine()
	opener := 0
	for opener < len(line) && line[opener] == '$' {
		opener++
	}
	block.Advance(opener)
	resetLine, resetPosition := block.Position()
	var value []byte
	for {
		line, _ := block.PeekLine()
		if line == nil {
			block.SetPosition(resetLine, resetPosition)
			return ast.NewTextSegment(startSegment.WithStop(startSegment.Start + opener))
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '$' {
				continue
			}
			start := i
			for i < len(line) && line[i] == '$' {
				i++
			}
			if i-start != opener {
				i--
				continue
			}
			value = append(value, line[:start]...)
			if len(value) == 0 {
				block.SetPosition(resetLine, resetPosition)
				return ast.NewTextSegment(startSegment.WithStop(startSegment.Start + opener))
			}
			block.Advance(i)
			return &mathInline{value: trimMathPadding(value)}
		}
		value = append(value, line...)
		block.AdvanceLine()
	}
}

// trimMathPadding applies the code-span rule remark-math inherits: one
// leading and trailing space is removed when both exist around content.
func trimMathPadding(value []byte) []byte {
	if len(value) > 1 && value[0] == ' ' && value[len(value)-1] == ' ' && len(bytes.TrimSpace(value)) > 0 {
		return value[1 : len(value)-1]
	}
	return value
}

type mathBlockParser struct{}

func (p *mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (p *mathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	position := pc.BlockOffset()
	if position < 0 || line[position] != '$' {
		return nil, parser.NoChildren
	}
	i := position
	for i < len(line) && line[i] == '$' {
		i++
	}
	length := i - position
	if length < 2 || bytes.IndexByte(line[i:], '$') >= 0 {
		return nil, parser.NoChildren
	}
	node := &mathBlock{openLength: length, indent: position}
	return node, parser.NoChildren
}

func (p *mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	n := node.(*mathBlock)
	line, segment := reader.PeekLine()
	width, position := util.IndentWidth(line, reader.LineOffset())
	if width < 4 {
		i := position
		for i < len(line) && line[i] == '$' {
			i++
		}
		if i-position >= n.openLength && util.IsBlank(line[i:]) {
			newline := 1
			if line[len(line)-1] != '\n' {
				newline = 0
			}
			reader.Advance(segment.Stop - segment.Start - newline)
			return parser.Close
		}
	}
	position, padding := util.IndentPositionPadding(line, reader.LineOffset(), segment.Padding, n.indent)
	if position < 0 {
		position = util.FirstNonSpacePosition(line)
		if position < 0 {
			position = 0
		}
		padding = 0
	}
	node.Lines().Append(text.NewSegmentPadding(segment.Start+position, segment.Stop, padding))
	reader.AdvanceAndSetPadding(segment.Stop-segment.Start-position-1, padding)
	return parser.Continue | parser.NoChildren
}

func (p *mathBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

func (p *mathBlockParser) CanInterruptParagraph() bool { return true }

func (p *mathBlockParser) CanAcceptIndentedLine() bool { return false }
