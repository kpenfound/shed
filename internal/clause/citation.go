package clause

import (
	"fmt"
	"slices"
	"strings"
)

// Citation names a clause ID, optionally at a git revision. Without a
// revision it refers to the working tree.
type Citation struct {
	ID  ID
	Rev string
}

// ParseCitation parses "ID" or "ID@revision".
func ParseCitation(s string) (Citation, error) {
	idPart, rev, hasRev := strings.Cut(s, "@")
	id, err := ParseID(idPart)
	if err != nil {
		return Citation{}, err
	}
	if hasRev && rev == "" {
		return Citation{}, fmt.Errorf("citation %q names an empty revision", s)
	}
	return Citation{ID: id, Rev: rev}, nil
}

func (c Citation) String() string {
	if c.Rev == "" {
		return c.ID.String()
	}
	return c.ID.String() + "@" + c.Rev
}

// Mentioned returns the well-formed clause IDs a text mentions, in order and
// without repeats. Revisions are dropped.
func Mentioned(text string) []ID {
	var ids []ID
	for _, m := range mentionPattern.FindAllString(text, -1) {
		c, err := ParseCitation(m)
		if err != nil || slices.Contains(ids, c.ID) {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// Cited returns the citations a plain text makes, in order: its whole
// ID-shaped tokens, each with any @revision, and ranges of two tokens
// joined by "to". A token that runs on into a letter or digit is not whole
// and cites nothing. Unlike a document's mentions, the text is not
// Markdown, so nothing in it is skipped as code.
func Cited(text string) []Mention {
	var locs [][]int
	for _, loc := range mentionPattern.FindAllStringIndex(text, -1) {
		if loc[1] < len(text) && isWordByte(text[loc[1]]) {
			continue
		}
		locs = append(locs, loc)
	}
	token := func(loc []int) string { return strings.TrimRight(text[loc[0]:loc[1]], ".:") }
	var out []Mention
	for i := 0; i < len(locs); i++ {
		m := Mention{Token: token(locs[i])}
		if i+1 < len(locs) && Normalize(text[locs[i][1]:locs[i+1][0]]) == "to" {
			m.To = token(locs[i+1])
			i++
		}
		out = append(out, m)
	}
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}
