package factory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// specConflict reports whether a rebase that would leave conflicts in files
// would leave one under spec/, so S.vcs.16's exception does not cover it.
func specConflict(files []string) bool {
	return slices.ContainsFunc(files, func(name string) bool { return strings.HasPrefix(name, "spec/") })
}

// live counts the sessions this process runs on each unit, from before
// their files are exported until after they are captured, so no rebase
// moves a unit's change under a session (S.vcs.10). Its lock is held while
// a unit is rebased, so a session that starts meanwhile sees the rebased
// files.
type live struct {
	mu       sync.Mutex
	sessions map[string]int
}

// enter records a session starting on a unit.
func (f *Factory) enter(change string) {
	f.live.mu.Lock()
	defer f.live.mu.Unlock()
	if f.live.sessions == nil {
		f.live.sessions = map[string]int{}
	}
	f.live.sessions[change]++
}

// leave records a session on a unit ending, after its directory was
// captured. When it was the unit's last session and a landing was recorded
// that the unit's change lacks, its rebase was put off for the session and
// runs now, recording its outcome in the unit's log (S.vcs.15).
func (f *Factory) leave(ctx context.Context, change string) {
	f.live.mu.Lock()
	defer f.live.mu.Unlock()
	f.live.sessions[change]--
	if f.live.sessions[change] > 0 {
		return
	}
	delete(f.live.sessions, change)
	ctx = context.WithoutCancel(ctx)
	latest, err := f.latestLanding()
	if err != nil || latest == nil {
		return
	}
	if descends, err := f.Repo.Descends(ctx, change, latest.Landed); err == nil && !descends {
		if outcome, rebased := f.follow(ctx, change); rebased {
			f.logRebase(change, latest.Change, outcome)
		}
	}
}

// latestLanding returns the unit that landed last, or nil when none has.
func (f *Factory) latestLanding() (*tracker.Unit, error) {
	units, err := f.Tracker.Units()
	if err != nil {
		return nil, err
	}
	var latest *tracker.Unit
	for i, u := range units {
		if u.State == unit.Landed && u.Landed != "" && (latest == nil || u.Updated.After(latest.Updated)) {
			latest = &units[i]
		}
	}
	return latest, nil
}

// Rebased is the outcome of a landing's rebase of one unit in flight.
type Rebased struct {
	// Unit is the unit as it was when it was rebased.
	Unit    tracker.Unit
	Outcome string
}

// Outcomes of a rebase onto a landing's main (S.vcs.15).
const (
	rebasedCleanly  = "rebased cleanly"
	rebasedConflict = "rebased with conflicts stored in its change"
	rebaseUndone    = "rebase undone: the rebase conflicted and the unit is past its seal or is a frame unit"
	rebaseDeferred  = "deferred: a session is running"
	rebaseFailed    = "rebase failed: "
)

// sweep sweeps as sweepReport does, for a caller with no use for the
// report.
func (f *Factory) sweep(ctx context.Context, lander string) error {
	_, err := f.sweepReport(ctx, lander)
	return err
}

// sweepReport rebases onto main every unit that is neither landed nor archived,
// except one, in the order they opened, leaving any unit with a session
// running to be rebased once its sessions end (S.vcs.10, S.vcs.11). A
// rebase that conflicts or fails stops nothing: a proposed or contested
// unit keeps conflicts in its change, a unit past its seal or opened by
// shed frame has a conflicting rebase undone, and a failed rebase is
// restored; either is tried again by the next sweep.
//
// A sweep after the landing of lander reports and records every unit's
// outcome, naming lander. A sweep with no lander finishes one a stopped
// process left undone: it records the outcome of each rebase it makes,
// naming the unit that landed last, and reports nothing. A sweep stops at
// the first unit it reaches once ctx is done.
func (f *Factory) sweepReport(ctx context.Context, lander string) ([]Rebased, error) {
	units, err := f.Tracker.Units()
	if err != nil {
		return nil, err
	}
	name := lander
	if name == "" {
		latest, err := f.latestLanding()
		if err != nil {
			return nil, err
		}
		if latest != nil {
			name = latest.Change
		}
	}
	var out []Rebased
	for _, u := range units {
		if u.Change == lander || u.State == unit.Landed || u.State == unit.Archived {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		f.live.mu.Lock()
		outcome, rebased := rebaseDeferred, false
		if f.live.sessions[u.Change] == 0 {
			outcome, rebased = f.follow(ctx, u.Change)
		}
		f.live.mu.Unlock()
		switch {
		case lander != "":
			f.logRebase(u.Change, lander, outcome)
			out = append(out, Rebased{Unit: u, Outcome: outcome})
		case rebased && name != "" && !f.loggedLast(u.Change, name, outcome):
			f.logRebase(u.Change, name, outcome)
		}
	}
	return out, nil
}

// failed is the outcome of a rebase that failed with err, on one line.
func failed(err error) string {
	return rebaseFailed + strings.Join(strings.Fields(err.Error()), " ")
}

// logRebase records a rebase's outcome in a unit's log. A unit's log is a
// report, so failing to write it fails nothing.
func (f *Factory) logRebase(change, lander, outcome string) {
	_ = f.Tracker.RecordRebase(change, lander, outcome)
}

// loggedLast reports whether the last rebase a unit's log records is this
// one, so a rebase each process retries is recorded once.
func (f *Factory) loggedLast(change, lander, outcome string) bool {
	events, err := f.Tracker.Events(change)
	if err != nil {
		return false
	}
	for i := len(events) - 1; i >= 0; i-- {
		if e := events[i]; e.Kind == tracker.UnitRebased {
			return e.Rebased.Lander == lander && e.Rebased.Outcome == outcome
		}
	}
	return false
}

// follow rebases a unit onto main when no session, of this process or
// another, is running on it. It returns the rebase's outcome and whether a
// rebase was tried: a unit whose change already descends from main counts
// as rebased cleanly, and one with a session running is deferred. The
// caller holds the lock of live.
func (f *Factory) follow(ctx context.Context, change string) (string, bool) {
	sessions, err := f.Tracker.Sessions(change)
	if err != nil {
		return failed(err), true
	}
	for _, s := range sessions {
		if s.Status == tracker.Running {
			return rebaseDeferred, false
		}
	}
	behind, err := f.Repo.Behind(ctx, change)
	if err != nil {
		return failed(err), true
	}
	if !behind {
		return rebasedCleanly, false
	}
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return failed(err), true
	}
	switch {
	case u.State == unit.Sealed, u.State == unit.Implementing, u.State == unit.Queued:
		// A sealed, implementing or queued unit keeps a rebase whose
		// conflicts lie only outside spec/ (S.vcs.16); one that conflicts
		// under spec/, like one that conflicts anywhere for a unit past
		// that, is undone.
		kept, conflicted, err := f.Repo.FollowUnless(ctx, change, specConflict)
		switch {
		case err != nil:
			return failed(err), true
		case !kept:
			return rebaseUndone, true
		case conflicted:
			return rebasedConflict, true
		}
	case u.State == unit.Verifying:
		// A verifying unit keeps only a rebase that holds no conflict.
		conflicted, err := f.Repo.FollowClean(ctx, change)
		switch {
		case err != nil:
			return failed(err), true
		case conflicted:
			return rebaseUndone, true
		}
	case u.OpenedBy == unit.FrameBuilder:
		// No session resolves a framing's conflicts, so it too keeps only a
		// clean rebase (S.frame.3).
		conflicted, err := f.Repo.FollowClean(ctx, change)
		switch {
		case err != nil:
			return failed(err), true
		case conflicted:
			return rebaseUndone, true
		}
	default:
		conflicted, err := f.Repo.Follow(ctx, change)
		switch {
		case err != nil:
			return failed(err), true
		case conflicted:
			return rebasedConflict, true
		}
	}
	return rebasedCleanly, true
}

// onSeal rebases a unit's change onto the main commit its seal is to
// record (S.vcs.10). It returns why the unit may not be sealed: the rebase
// failed, which leaves the change as it was, or a file under spec/ holds a
// conflict, which stays in the change. It returns "" when the unit may be
// sealed, conflicts outside spec/ included.
func (f *Factory) onSeal(ctx context.Context, change, main string) (string, error) {
	if _, err := f.Repo.RebaseOnto(ctx, change, main); err != nil {
		return fmt.Sprintf("sealing could not rebase the change onto main %s: %v", main, err), nil
	}
	conflicts, err := f.specConflicted(ctx, change)
	if err != nil || conflicts == "" {
		return "", err
	}
	return fmt.Sprintf("sealing rebased the change onto main %s and left conflicts under spec/ to resolve: %s", main, conflicts), nil
}

// unreported returns the outcome of a landing that did not land, which has
// no sweep to report.
func unreported(out Outcome, err error) (Outcome, []Rebased, error) {
	return out, nil, err
}
