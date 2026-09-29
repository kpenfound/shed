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

// HorizonChange is a horizon clause added, changed or removed between two
// revisions, with its tier: the tier at the later revision, or at the
// earlier one for a removed clause.
type HorizonChange struct {
	ID   clause.ID
	Tier string
	// Change is "added", "changed" or "removed".
	Change string
}

// DiffHorizon lists the horizon clauses that differ between two sets, in
// document order. Removed clauses sit where they stood in the earlier set.
// A clause changes when its tag list or its text changes; whitespace does
// not count.
func DiffHorizon(from, to *Set) []HorizonChange {
	old := from.Clauses(clause.Horizon)
	oldAt := map[clause.ID]int{}
	for i, c := range old {
		oldAt[c.ID] = i
	}
	cur := map[clause.ID]bool{}
	for _, c := range to.Clauses(clause.Horizon) {
		cur[c.ID] = true
	}
	var out []HorizonChange
	next := 0
	removedBefore := func(end int) {
		for ; next < end; next++ {
			if c := old[next]; !cur[c.ID] {
				out = append(out, HorizonChange{ID: c.ID, Tier: tier(c), Change: "removed"})
			}
		}
	}
	for _, c := range to.Clauses(clause.Horizon) {
		i, ok := oldAt[c.ID]
		switch {
		case !ok:
			out = append(out, HorizonChange{ID: c.ID, Tier: tier(c), Change: "added"})
			continue
		case i >= next:
			removedBefore(i)
			next = i + 1
		}
		if o := old[i]; o.Text != c.Text || !slices.Equal(o.Tags, c.Tags) {
			out = append(out, HorizonChange{ID: c.ID, Tier: tier(c), Change: "changed"})
		}
	}
	removedBefore(len(old))
	return out
}

// AmendedHorizon lists the horizon clauses one revision amends against
// another, in document order: the changes of DiffHorizon, less the clauses
// whose only change is gaining the realised tag (S.owner.11). A clause
// whose text or other tags change as well is amended.
func AmendedHorizon(from, to *Set) []HorizonChange {
	var out []HorizonChange
	for _, c := range DiffHorizon(from, to) {
		if c.Change == "changed" && onlyRealised(from, to, c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// onlyRealised reports whether a clause differs between two sets only by
// gaining the realised tag.
func onlyRealised(from, to *Set, id clause.ID) bool {
	o, _ := from.Lookup(id)
	c, _ := to.Lookup(id)
	if o.Text != c.Text || slices.Contains(o.Tags, Realised) || !slices.Contains(c.Tags, Realised) {
		return false
	}
	rest := slices.DeleteFunc(slices.Clone(c.Tags), func(t string) bool { return t == Realised })
	return slices.Equal(o.Tags, rest)
}
