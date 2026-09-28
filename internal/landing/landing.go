// Package landing lands queued units on main and writes the commit message
// that records them: the unit's title, its spec diff, the horizon clauses it
// advances, its change ID and its seal.
package landing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// Message is the commit message of a landed unit.
func Message(u tracker.Unit, diff docs.SpecDiff) string {
	var b strings.Builder
	b.WriteString(u.Title)
	b.WriteString("\n\n")
	if diff.Empty() {
		b.WriteString("Spec: unchanged\n")
	} else {
		b.WriteString("Spec:\n")
		for _, group := range []struct {
			label string
			ids   []clause.ID
		}{{"added", diff.Added}, {"changed", diff.Changed}, {"removed", diff.Removed}} {
			for _, id := range group.ids {
				fmt.Fprintf(&b, "  %-8s%s\n", group.label, id)
			}
		}
	}
	if len(u.Footprint.Advances) > 0 {
		fmt.Fprintf(&b, "Advances: %s\n", strings.Join(u.Footprint.Advances, ", "))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Unit: %s\n", u.Change)
	if u.Seal != nil {
		fmt.Fprintf(&b, "Sealed-Against: %s\n", u.Seal.Main)
	}
	return b.String()
}

// Land lands a queued unit: it lands the unit's change on main as one commit
// and records the landing in the tracker. A unit whose change already landed
// is only recorded, so a landing interrupted after main moved completes.
func Land(ctx context.Context, tr *tracker.Tracker, repo *vcs.Repo, change string, actor unit.Actor) (string, error) {
	u, err := tr.Unit(change)
	if err != nil {
		return "", err
	}
	if u.State != unit.Queued {
		return "", fmt.Errorf("unit %s is %s; only queued units land", unit.Short(u.Change), u.State)
	}
	if u.Seal == nil {
		return "", fmt.Errorf("unit %s has no seal", unit.Short(u.Change))
	}
	commit, err := repo.Land(ctx, u.Change, func(ctx context.Context, main, head string) (string, error) {
		from, _ := docs.Load(revision.Git{Root: repo.Root(), Rev: main})
		to, _ := docs.Load(revision.Git{Root: repo.Root(), Rev: head})
		return Message(u, docs.DiffSpec(from, to)), nil
	})
	if err != nil {
		return "", err
	}
	if err := tr.Land(u.Change, commit, actor, "landed on main"); err != nil {
		return "", errors.Join(fmt.Errorf("unit %s landed as %s but the tracker did not record it; land it again to record it", unit.Short(u.Change), commit), err)
	}
	return commit, nil
}
