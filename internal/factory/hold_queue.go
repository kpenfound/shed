package factory

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// HeldBack reports, for every queued unit among units, the queued units
// entangled with it under S.queue.9 that it waits behind: those whose spec
// footprint holds fewer distinct clauses, or as many and an earlier place
// in the landing order of S.queue.7. units must list the units in the order
// they opened, as Tracker.Units does; a unit's blockers come back in that
// same order. A unit entangled with no other queued unit, or held back by
// none of the units it is entangled with, is absent from the result.
func HeldBack(units []tracker.Unit) map[string][]string {
	queued := oldestFirst(units, unit.Queued)
	held := make(map[string][]string, len(queued))
	for i, u := range queued {
		mine := u.Footprint.SpecClauses()
		var blockers []string
		for j, other := range queued {
			if i == j {
				continue
			}
			theirs := other.Footprint.SpecClauses()
			if !sharesSpecClause(mine, theirs) {
				continue
			}
			if len(theirs) < len(mine) || (len(theirs) == len(mine) && j < i) {
				blockers = append(blockers, other.Change)
			}
		}
		if len(blockers) > 0 {
			held[u.Change] = blockers
		}
	}
	return held
}

// sharesSpecClause reports whether two footprints' distinct spec clauses
// have one in common.
func sharesSpecClause(a, b []string) bool {
	for _, id := range a {
		if slices.Contains(b, id) {
			return true
		}
	}
	return false
}

// heldBackErr refuses a stage on a unit held back under S.queue.9, naming
// the units it waits behind by their short change IDs.
func heldBackErr(change string, blockers []string) error {
	names := make([]string, len(blockers))
	for i, b := range blockers {
		names[i] = unit.Short(b)
	}
	return fmt.Errorf("unit %s is held back under S.queue.9: it waits behind %s", unit.Short(change), strings.Join(names, ", "))
}
