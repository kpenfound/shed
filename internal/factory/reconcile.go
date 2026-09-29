package factory

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/unit"
)

// reconcileHorizon compares the horizon on a landed commit with the horizon
// on its parent and reconciles every other in-flight unit against the
// clauses its sealed footprint advances. A unit advancing a clause the
// landing removed reopens (S.queue.4). A unit advancing a clause the landing
// changed gets a notice for its next session, whichever role runs it
// (S.queue.3), and one whose clause changed text is also marked for horizon
// review (S.queue.5). A unit advancing a clause that a clause on the landed
// commit newly refines gets that refining clause in its notice too
// (S.queue.6), which marks nothing.
// Every other unit is left alone.
func (f *Factory) reconcileHorizon(landed, commit string) error {
	root := f.Repo.Root()
	from, _ := docs.Load(revision.Git{Root: root, Rev: commit + "^"})
	to, _ := docs.Load(revision.Git{Root: root, Rev: commit})
	if from == nil || to == nil {
		return nil
	}
	after := map[clause.ID]clause.Clause{}
	for _, c := range to.Clauses(clause.Horizon) {
		after[c.ID] = c
	}
	// Changes in the order of the parent's horizon. Clause text is already
	// whitespace-normalized, as S.owner.2 compares it.
	var changed, removed []clause.Clause
	for _, c := range from.Clauses(clause.Horizon) {
		now, ok := after[c.ID]
		switch {
		case !ok:
			removed = append(removed, c)
		case now.Text != c.Text || !slices.Equal(now.Tags, c.Tags):
			changed = append(changed, c)
		}
	}
	before := map[clause.ID]clause.Clause{}
	for _, c := range from.Clauses(clause.Horizon) {
		before[c.ID] = c
	}
	// Refining clauses gained, in the order of the landed horizon: a
	// refines tag the clause did not carry on the parent.
	var gained []refinement
	for _, c := range to.Clauses(clause.Horizon) {
		was, existed := before[c.ID]
		for _, target := range docs.RefinesOf(c) {
			if existed && slices.Contains(docs.RefinesOf(was), target) {
				continue
			}
			gained = append(gained, refinement{clause: c, refines: target})
		}
	}
	if len(changed) == 0 && len(removed) == 0 && len(gained) == 0 {
		return nil
	}
	units, err := f.Tracker.Units()
	if err != nil {
		return err
	}
	short := unit.Short(landed)
	for _, u := range units {
		if u.Change == landed {
			continue
		}
		if !unit.PastSeal(u.State) {
			continue
		}
		gone := advanced(removed, u.Footprint.Advances)
		if len(gone) > 0 {
			ids := make([]string, len(gone))
			for i, c := range gone {
				ids[i] = c.ID.String()
			}
			reason := fmt.Sprintf("unit %s removed %s from the horizon, which this unit advances", short, strings.Join(ids, ", "))
			if err := f.Tracker.Reopen(u.Change, unit.Shed, reason, false); err != nil {
				return err
			}
			continue
		}
		touched := advanced(changed, u.Footprint.Advances)
		var refining []refinement
		for _, g := range gained {
			if slices.Contains(u.Footprint.Advances, g.refines.String()) {
				refining = append(refining, g)
			}
		}
		if len(touched) == 0 && len(refining) == 0 {
			continue
		}
		reworded := false
		var b strings.Builder
		fmt.Fprintf(&b, "Unit %s landed and changed the horizon around clauses this unit advances:", short)
		for _, c := range touched {
			now := after[c.ID]
			reworded = reworded || now.Text != c.Text
			fmt.Fprintf(&b, "\n- %s\n  before: (%s) %s\n  after: (%s) %s", c.ID,
				strings.Join(c.Tags, ", "), c.Text, strings.Join(now.Tags, ", "), now.Text)
		}
		for _, g := range refining {
			fmt.Fprintf(&b, "\n- %s gained, refining %s\n  (%s) %s", g.clause.ID, g.refines,
				strings.Join(g.clause.Tags, ", "), g.clause.Text)
		}
		if _, err := f.Tracker.AddReviewNotice(u.Change, nextSession, "horizon", b.String(), unit.Shed, reworded); err != nil {
			return err
		}
	}
	return nil
}

// refinement is a horizon clause and a clause it refines.
type refinement struct {
	clause  clause.Clause
	refines clause.ID
}

// advanced returns the clauses among cs that advances names, keeping the
// order of cs.
func advanced(cs []clause.Clause, advances []string) []clause.Clause {
	var out []clause.Clause
	for _, c := range cs {
		if slices.Contains(advances, c.ID.String()) {
			out = append(out, c)
		}
	}
	return out
}
