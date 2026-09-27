package docs

import (
	"fmt"
	"maps"
	"slices"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/revision"
)

// History checks that IDs stay permanent across the repository's history. It
// refuses an ID that was removed in an earlier commit and appears again, and a
// pending change that moves a clause's text from one ID to another. Outside a
// git repository, or before the first commit, there is nothing to check.
func History(root string, work *Set) ([]clause.Problem, error) {
	if !revision.HasHistory(root) {
		return nil, nil
	}
	commits, err := revision.History(root, CharterPath, SpecDir, HorizonPath)
	if err != nil {
		return nil, err
	}

	retired := map[clause.ID]string{}
	reused := map[clause.ID]string{}
	var prev map[clause.ID]clause.Clause
	step := func(cur map[clause.ID]clause.Clause, label string) {
		for id := range prev {
			if _, ok := cur[id]; !ok {
				retired[id] = label
			}
		}
		for id := range cur {
			if _, ok := prev[id]; ok {
				continue
			}
			if at, ok := retired[id]; ok {
				reused[id] = at
			}
		}
		prev = cur
	}
	var head map[clause.ID]clause.Clause
	for _, commit := range commits {
		s, _ := Load(revision.Git{Root: root, Rev: commit})
		step(s.byID, commit[:12])
		head = s.byID
	}
	step(work.byID, "the working tree")

	var problems []clause.Problem
	for _, id := range slices.SortedFunc(maps.Keys(reused), clause.Compare) {
		if c, ok := work.byID[id]; ok {
			problems = append(problems, clause.Problem{File: c.File, Line: c.Line,
				Msg: fmt.Sprintf("%s reuses an ID retired in %s; give the clause a new ID", id, reused[id])})
		}
	}
	return append(problems, moved(head, work.byID)...), nil
}

// moved finds clauses whose text now sits under a different ID than it did
// at HEAD.
func moved(head, work map[clause.ID]clause.Clause) []clause.Problem {
	byText := map[string]clause.ID{}
	for id, c := range head {
		if c.Text != "" {
			byText[c.Text] = id
		}
	}
	var problems []clause.Problem
	for _, id := range slices.SortedFunc(maps.Keys(work), clause.Compare) {
		c := work[id]
		if old, ok := head[id]; ok && old.Text == c.Text {
			continue
		}
		from, ok := byText[c.Text]
		if !ok || from == id || from.Kind != id.Kind {
			continue
		}
		if still, ok := work[from]; ok && still.Text == c.Text {
			continue
		}
		problems = append(problems, clause.Problem{File: c.File, Line: c.Line,
			Msg: fmt.Sprintf("%s takes the text of %s; IDs are never renumbered", id, from)})
	}
	return problems
}
