package docs

import (
	"maps"
	"slices"

	"github.com/kpenfound/shed/internal/clause"
)

// SpecDiff lists the spec clauses that differ between two revisions.
type SpecDiff struct {
	Added   []clause.ID
	Removed []clause.ID
	Changed []clause.ID
}

// Empty reports whether the revisions have the same spec clauses.
func (d SpecDiff) Empty() bool {
	return len(d.Added)+len(d.Removed)+len(d.Changed) == 0
}

// Modified lists the clauses the diff adds, changes or removes, sorted.
func (d SpecDiff) Modified() []string {
	var out []string
	for _, id := range slices.Concat(d.Added, d.Changed, d.Removed) {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}

// DiffSpec compares the spec clauses of two sets. A clause changes when its
// text or the horizon clauses it advances change; whitespace does not count.
func DiffSpec(from, to *Set) SpecDiff {
	var d SpecDiff
	old := specByID(from)
	cur := specByID(to)
	for _, id := range slices.SortedFunc(maps.Keys(cur), clause.Compare) {
		o, ok := old[id]
		switch {
		case !ok:
			d.Added = append(d.Added, id)
		case o.Text != cur[id].Text || !slices.Equal(o.Tags, cur[id].Tags):
			d.Changed = append(d.Changed, id)
		}
	}
	for _, id := range slices.SortedFunc(maps.Keys(old), clause.Compare) {
		if _, ok := cur[id]; !ok {
			d.Removed = append(d.Removed, id)
		}
	}
	return d
}

func specByID(s *Set) map[clause.ID]clause.Clause {
	out := map[clause.ID]clause.Clause{}
	for _, c := range s.Clauses(clause.Spec) {
		out[c.ID] = c
	}
	return out
}
