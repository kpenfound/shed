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
