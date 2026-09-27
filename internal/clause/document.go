package clause

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Clause is one clause of a document.
type Clause struct {
	ID ID
	// Tagged reports whether a parenthesised list followed the ID. Only spec
	// and horizon clauses carry one.
	Tagged bool
	// Tags holds the entries of that list, trimmed.
	Tags []string
	// Text is the clause's text after the ID and tags, with whitespace
	// collapsed so that rewrapping a clause does not change it.
	Text string
	File string
	Line int
}

// Mention is an ID-shaped token found in a document's prose. When the prose
// reads "A to B", the mention is a range and To holds B.
type Mention struct {
	Token string
	To    string
	File  string
	Line  int

	offset int
}

// Document is a parsed clause document.
type Document struct {
	Path     string
	Clauses  []Clause
	Mentions []Mention
}

// Problem is a defect in a document, a proof or a citation.
type Problem struct {
	File string
	Line int
	Msg  string
}

func (p Problem) String() string {
	switch {
	case p.File == "":
		return p.Msg
	case p.Line == 0:
		return p.File + ": " + p.Msg
	}
	return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg)
}

var (
	markdown  = goldmark.New()
	boldIDRaw = regexp.MustCompile(`^(?:\*\*|__)[^*_\s]+(?:\*\*|__)`)
)

// Parse reads a clause document. allowed lists the ID kinds the document may
// hold; spec and horizon clauses have their tag list parsed.
func Parse(path string, src []byte, allowed ...Kind) (*Document, []Problem) {
	doc := &Document{Path: path}
	var problems []Problem
	root := markdown.Parser().Parse(text.NewReader(src))
	var open []string
	type span struct{ start, stop int }
	var headers []span

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		switch n := n.(type) {
		case *ast.ListItem:
			token, ok := leadingBold(n, src)
			if !ok || !IsCandidate(token) {
				return ast.WalkContinue, nil
			}
			if !entering {
				open = open[:len(open)-1]
				return ast.WalkContinue, nil
			}
			start, stop := blockRange(n)
			line := lineOf(src, start)
			id, err := ParseID(token)
			switch {
			case err != nil:
				problems = append(problems, Problem{path, line, err.Error()})
			case !slices.Contains(allowed, id.Kind):
				problems = append(problems, Problem{path, line, fmt.Sprintf("%s is a %s ID and does not belong in this document", id, id.Kind)})
			case len(open) > 0:
				problems = append(problems, Problem{path, line, fmt.Sprintf("clause %s is nested inside clause %s", id, open[len(open)-1])})
			default:
				c, header, p := clauseFrom(id, path, line, src[start:stop])
				doc.Clauses = append(doc.Clauses, c)
				headers = append(headers, span{start, start + header})
				problems = append(problems, p...)
			}
			open = append(open, token)
		case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
			if entering {
				doc.Mentions = append(doc.Mentions, mentions(path, n, src)...)
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})

	// A clause's own ID and tag list are not citations in its prose.
	doc.Mentions = slices.DeleteFunc(doc.Mentions, func(m Mention) bool {
		return slices.ContainsFunc(headers, func(h span) bool { return m.offset >= h.start && m.offset < h.stop })
	})
	return doc, problems
}

// clauseFrom builds a clause from its list item's source. It also returns the
// length of the header, the bold ID and any tag list, within raw.
func clauseFrom(id ID, path string, line int, raw []byte) (Clause, int, []Problem) {
	c := Clause{ID: id, File: path, Line: line}
	afterID := string(raw[len(boldIDRaw.Find(raw)):])
	rest := strings.TrimLeft(afterID, " \t\n")
	if (id.Kind == Spec || id.Kind == Horizon) && strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return c, len(raw) - len(rest), []Problem{{path, line, fmt.Sprintf("clause %s has an unclosed tag list", id)}}
		}
		c.Tagged = true
		for _, t := range strings.Split(rest[1:end], ",") {
			if t = strings.TrimSpace(t); t != "" {
				c.Tags = append(c.Tags, t)
			}
		}
		rest = rest[end+1:]
	}
	c.Text = Normalize(rest)
	return c, len(raw) - len(rest), nil
}

// Normalize collapses runs of whitespace to single spaces.
func Normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// leadingBold returns the text of a strong emphasis that opens a list item.
func leadingBold(item *ast.ListItem, src []byte) (string, bool) {
	block := item.FirstChild()
	if block == nil {
		return "", false
	}
	if _, ok := block.(*ast.Paragraph); !ok {
		if _, ok := block.(*ast.TextBlock); !ok {
			return "", false
		}
	}
	em, ok := block.FirstChild().(*ast.Emphasis)
	if !ok || em.Level != 2 {
		return "", false
	}
	token := inlineText(em, src)
	if strings.ContainsAny(token, " \t\n") {
		return "", false
	}
	return token, true
}

// blockRange returns the byte range covered by the lines of a block and its
// descendants.
func blockRange(n ast.Node) (start, stop int) {
	start = -1
	var visit func(ast.Node)
	visit = func(n ast.Node) {
		if n.Type() != ast.TypeBlock {
			return
		}
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			if start < 0 || seg.Start < start {
				start = seg.Start
			}
			if seg.Stop > stop {
				stop = seg.Stop
			}
		}
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			visit(c)
		}
	}
	visit(n)
	if start < 0 {
		start = 0
	}
	return start, stop
}

func lineOf(src []byte, offset int) int {
	return strings.Count(string(src[:offset]), "\n") + 1
}

func inlineText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Segment.Value(src))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		default:
			b.WriteString(inlineText(c, src))
		}
	}
	return b.String()
}

// piece is a run of prose text and the source offset it starts at.
type piece struct {
	at     int // offset in the joined prose
	source int // offset in the source, or -1
}

// mentions scans a block's prose, outside code spans, for ID-shaped tokens.
func mentions(path string, block ast.Node, src []byte) []Mention {
	var b strings.Builder
	var pieces []piece
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.CodeSpan, *ast.AutoLink, *ast.RawHTML:
				b.WriteByte(' ')
			case *ast.Text:
				pieces = append(pieces, piece{b.Len(), c.Segment.Start})
				b.Write(c.Segment.Value(src))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				pieces = append(pieces, piece{b.Len(), -1})
				b.Write(c.Value)
			default:
				walk(c)
			}
		}
	}
	walk(block)
	prose := b.String()
	fallback := 0
	if lines := block.Lines(); lines.Len() > 0 {
		fallback = lines.At(0).Start
	}
	sourceAt := func(i int) int {
		off := fallback
		for _, p := range pieces {
			if p.at > i {
				break
			}
			if p.source >= 0 {
				off = p.source + i - p.at
			}
		}
		return off
	}

	locs := mentionPattern.FindAllStringIndex(prose, -1)
	var out []Mention
	for i := 0; i < len(locs); i++ {
		offset := sourceAt(locs[i][0])
		m := Mention{
			Token:  strings.TrimRight(prose[locs[i][0]:locs[i][1]], ".:"),
			File:   path,
			Line:   lineOf(src, offset),
			offset: offset,
		}
		if i+1 < len(locs) && Normalize(prose[locs[i][1]:locs[i+1][0]]) == "to" {
			m.To = strings.TrimRight(prose[locs[i+1][0]:locs[i+1][1]], ".:")
			i++
		}
		out = append(out, m)
	}
	return out
}
