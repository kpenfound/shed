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

// TieredHorizonChange is a horizon clause a diff lists, with its tier (S.diff.3).
type TieredHorizonChange struct {
	ID   clause.ID
	Tier string
}

// HorizonDiff lists the horizon clauses that differ between two revisions and
// the tier of the amendment they make.
type HorizonDiff struct {
	Added   []TieredHorizonChange
	Removed []TieredHorizonChange
	Changed []TieredHorizonChange
	// Tier is the highest tier among the clauses the amendment counts, or
	// empty when it counts none (S.diff.4).
	Tier string
	// Counted is every tier each listed clause counts at, in document
	// order: the order of the second revision's horizon, then the first's
	// for clauses it removes.
	Counted []TieredHorizonChange
}

// CountedAt lists the clauses the amendment counts at a tier, in document
// order.
func (d HorizonDiff) CountedAt(tier string) []clause.ID {
	var out []clause.ID
	for _, c := range d.Counted {
		if c.Tier == tier && !slices.Contains(out, c.ID) {
			out = append(out, c.ID)
		}
	}
	return out
}

// DiffHorizonAmendment compares the horizon clauses of two sets. A clause changes when
// its text or its tag list changes; whitespace does not count. An added
// clause takes its tier on to, a removed one its tier on from and a changed
// one the higher of the two.
//
// The amendment's tier counts every listed clause at its own tier, except a
// changed clause whose only change is gaining realised (S.owner.11). A clause
// whose refines tag differs between the revisions also counts at the tier of
// the clause its tag names, on each revision where it carries the tag.
func DiffHorizonAmendment(from, to *Set) HorizonDiff {
	var d HorizonDiff
	old := horizonByID(from)
	cur := horizonByID(to)
	counts := map[clause.ID][]string{}
	var id clause.ID
	count := func(tier string) {
		if tierRank(tier) > tierRank(d.Tier) {
			d.Tier = tier
		}
		if tier != "" && !slices.Contains(counts[id], tier) {
			counts[id] = append(counts[id], tier)
		}
	}
	countParents := func(o, c *TraceEntry) {
		var was, now clause.ID
		if o != nil {
			was = o.Refines
		}
		if c != nil {
			now = c.Refines
		}
		if was == now {
			return
		}
		if parent, ok := old[was]; ok {
			count(parent.Tier)
		}
		if parent, ok := cur[now]; ok {
			count(parent.Tier)
		}
	}
	for _, id = range slices.SortedFunc(maps.Keys(cur), clause.Compare) {
		c := cur[id]
		o, ok := old[id]
		switch {
		case !ok:
			d.Added = append(d.Added, TieredHorizonChange{id, c.Tier})
			count(c.Tier)
			countParents(nil, &c)
		case o.Clause.Text != c.Clause.Text || !slices.Equal(o.Clause.Tags, c.Clause.Tags):
			tier := o.Tier
			if tierRank(c.Tier) > tierRank(tier) {
				tier = c.Tier
			}
			d.Changed = append(d.Changed, TieredHorizonChange{id, tier})
			if !onlyRealised(from, to, id) {
				count(tier)
			}
			countParents(&o, &c)
		}
	}
	for _, id = range slices.SortedFunc(maps.Keys(old), clause.Compare) {
		if _, ok := cur[id]; !ok {
			o := old[id]
			d.Removed = append(d.Removed, TieredHorizonChange{id, o.Tier})
			count(o.Tier)
			countParents(&o, nil)
		}
	}
	for _, s := range []*Set{to, from} {
		for _, e := range Trace(s) {
			for _, tier := range counts[e.Clause.ID] {
				d.Counted = append(d.Counted, TieredHorizonChange{e.Clause.ID, tier})
			}
			delete(counts, e.Clause.ID)
		}
	}
	return d
}

// Listed reports whether the diff lists any horizon clause.
func (d HorizonDiff) Listed() bool {
	return len(d.Added)+len(d.Removed)+len(d.Changed) > 0
}

// tierRank orders tiers nearest first, from 1; an empty or unknown tier is 0.
func tierRank(tier string) int {
	return slices.Index(Tiers, tier) + 1
}

func horizonByID(s *Set) map[clause.ID]TraceEntry {
	out := map[clause.ID]TraceEntry{}
	for _, e := range Trace(s) {
		out[e.Clause.ID] = e
	}
	return out
}
