// Package docs loads the charter, spec and horizon of a repository as one set
// of clauses, and answers questions about them: citations, the gap, the trace
// from horizon to spec, and the spec diff between revisions.
package docs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/revision"
)

// Locations of the documents, relative to the repository root.
const (
	CharterPath = "charter.md"
	SpecDir     = "spec"
	HorizonPath = "horizon.md"
	ArchiveDir  = "archive"
)

// Set is every clause document at one revision.
type Set struct {
	Revision string
	Charter  *clause.Document
	Spec     []*clause.Document
	Horizon  *clause.Document
	byID     map[clause.ID]clause.Clause
}

// Load reads and parses the documents from a source. It returns the set even
// when there are problems, holding every clause it could parse.
func Load(src revision.Source) (*Set, []clause.Problem) {
	s := &Set{Revision: src.Name(), byID: map[clause.ID]clause.Clause{}}
	var problems []clause.Problem

	read := func(path string, kinds ...clause.Kind) *clause.Document {
		data, err := src.ReadFile(path)
		if err != nil {
			problems = append(problems, missing(path, err))
			return &clause.Document{Path: path}
		}
		doc, p := clause.Parse(path, data, kinds...)
		problems = append(problems, p...)
		return doc
	}

	s.Charter = read(CharterPath, clause.Charter)
	files, err := src.Files(SpecDir)
	if err != nil {
		problems = append(problems, missing(SpecDir+"/", err))
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			s.Spec = append(s.Spec, read(f, clause.Spec))
		}
	}
	s.Horizon = read(HorizonPath, clause.Horizon, clause.Milestone)

	for _, doc := range s.Documents() {
		for _, c := range doc.Clauses {
			if first, dup := s.byID[c.ID]; dup {
				problems = append(problems, clause.Problem{File: c.File, Line: c.Line,
					Msg: fmt.Sprintf("duplicate ID %s, first used at %s:%d", c.ID, first.File, first.Line)})
				continue
			}
			s.byID[c.ID] = c
		}
	}
	return s, problems
}

func missing(path string, err error) clause.Problem {
	if errors.Is(err, revision.ErrNotExist) {
		return clause.Problem{File: path, Msg: "missing"}
	}
	return clause.Problem{File: path, Msg: err.Error()}
}

// Missing reports whether a problem Load returns says a document or file is
// simply absent, as opposed to malformed or unreadable for another reason.
func Missing(p clause.Problem) bool {
	return p.Line == 0 && p.Msg == "missing"
}

// Documents returns the charter, the spec files and the horizon.
func (s *Set) Documents() []*clause.Document {
	docs := []*clause.Document{s.Charter}
	docs = append(docs, s.Spec...)
	return append(docs, s.Horizon)
}

// Lookup returns the clause with an ID.
func (s *Set) Lookup(id clause.ID) (clause.Clause, bool) {
	c, ok := s.byID[id]
	return c, ok
}

// Clauses returns every clause of a kind in document order: the order of
// the files, then of the lines within each file.
func (s *Set) Clauses(kind clause.Kind) []clause.Clause {
	var out []clause.Clause
	for _, doc := range s.Documents() {
		for _, c := range doc.Clauses {
			if first, ok := s.byID[c.ID]; ok && c.ID.Kind == kind && first.File == c.File && first.Line == c.Line {
				out = append(out, c)
			}
		}
	}
	return out
}

// Series returns the clauses from one ID to another in the same series,
// inclusive, skipping retired numbers.
func (s *Set) Series(from, to clause.ID) []clause.Clause {
	var out []clause.Clause
	for _, c := range s.Clauses(from.Kind) {
		if clause.SameSeries(c.ID, from) && c.ID.N >= from.N && c.ID.N <= to.N {
			out = append(out, c)
		}
	}
	return out
}
