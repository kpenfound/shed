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
// pending change that moves a clause's text from one ID to another. It also
// refuses a pending change that leaves a near or soon horizon clause without a
// refines tag, unless HEAD already holds that clause at near or soon without
// one (S.horizon.11). Outside a git repository, or before the first commit,
// there is nothing to check.
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
	problems = append(problems, moved(head, work.byID)...)
	return append(problems, unrefined(head, work)...), nil
}

// unrefined finds near and soon horizon clauses in the working tree that name
// no clause they refine, other than those HEAD already holds at near or soon
// without a refines tag.
func unrefined(head map[clause.ID]clause.Clause, work *Set) []clause.Problem {
	var problems []clause.Problem
	for _, c := range work.Clauses(clause.Horizon) {
		t := tier(c)
		if parentTier(t) || t == "" || hasRefines(c) {
			continue
		}
		if old, ok := head[c.ID]; ok && !parentTier(tier(old)) && tier(old) != "" && !hasRefines(old) {
			continue
		}
		problems = append(problems, clause.Problem{File: c.File, Line: c.Line,
			Msg: fmt.Sprintf("%s is %s and names no distant or eventual clause it refines", c.ID, t)})
	}
	return problems
}

// hasRefines reports whether a clause carries a refines tag.
func hasRefines(c clause.Clause) bool {
	for _, t := range c.Tags {
		if _, ok := refinesTarget(t); ok {
			return true
		}
	}
	return false
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

// Held returns every clause ID the documents held at rev or at any commit
// before it: the IDs a new clause may never take (S.doc.6).
func Held(root, rev string) (map[clause.ID]bool, error) {
	commits, err := revision.Log(root, rev, CharterPath, SpecDir, HorizonPath)
	if err != nil {
		return nil, err
	}
	held := map[clause.ID]bool{}
	for _, commit := range append(commits, rev) {
		s, _ := Load(revision.Git{Root: root, Rev: commit})
		for id := range s.byID {
			held[id] = true
		}
	}
	return held, nil
}
