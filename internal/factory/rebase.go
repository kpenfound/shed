package factory

import (
	"context"
	"fmt"
	"sync"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

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
// runs now.
func (f *Factory) leave(ctx context.Context, change string) {
	f.live.mu.Lock()
	defer f.live.mu.Unlock()
	f.live.sessions[change]--
	if f.live.sessions[change] > 0 {
		return
	}
	delete(f.live.sessions, change)
	ctx = context.WithoutCancel(ctx)
	if lacks, err := f.lacksLanding(ctx, change); err == nil && lacks {
		f.follow(ctx, change)
	}
}

// lacksLanding reports whether a unit's change does not descend from the
// latest landing the tracker recorded.
func (f *Factory) lacksLanding(ctx context.Context, change string) (bool, error) {
	units, err := f.Tracker.Units()
	if err != nil {
		return false, err
	}
	var latest *tracker.Unit
	for i, u := range units {
		if u.State == unit.Landed && u.Landed != "" && (latest == nil || u.Updated.After(latest.Updated)) {
			latest = &units[i]
		}
	}
	if latest == nil {
		return false, nil
	}
	descends, err := f.Repo.Descends(ctx, change, latest.Landed)
	return !descends, err
}

// sweep rebases onto main every unit that is neither landed nor archived,
// except one, leaving any unit with a session running to be rebased once
// its sessions end (S.vcs.10, S.vcs.11). A rebase that conflicts or fails
// stops nothing: a proposed or contested unit keeps conflicts in its change,
// a unit past its seal or opened by shed frame has a conflicting rebase
// undone, and a failed rebase is restored; either is tried again by the
// next sweep.
func (f *Factory) sweep(ctx context.Context, except string) error {
	units, err := f.Tracker.Units()
	if err != nil {
		return err
	}
	for _, u := range units {
		if u.Change == except || u.State == unit.Landed || u.State == unit.Archived {
			continue
		}
		f.live.mu.Lock()
		if f.live.sessions[u.Change] == 0 {
			f.follow(ctx, u.Change)
		}
		f.live.mu.Unlock()
	}
	return nil
}

// follow rebases a unit onto main when its change does not descend from
// main and no session, of this process or another, is running on it. The
// caller holds the lock of live.
func (f *Factory) follow(ctx context.Context, change string) {
	sessions, err := f.Tracker.Sessions(change)
	if err != nil {
		return
	}
	for _, s := range sessions {
		if s.Status == tracker.Running {
			return
		}
	}
	if behind, err := f.Repo.Behind(ctx, change); err != nil || !behind {
		return
	}
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return
	}
	switch {
	case u.State == unit.Sealed, u.State == unit.Implementing, u.State == unit.Verifying, u.State == unit.Queued:
		// Past its seal, a unit keeps only a rebase that holds no conflict.
		_, _ = f.Repo.FollowClean(ctx, change)
	case u.OpenedBy == unit.FrameBuilder:
		// No session resolves a framing's conflicts, so it too keeps only a
		// clean rebase (S.frame.3).
		_, _ = f.Repo.FollowClean(ctx, change)
	default:
		_, _ = f.Repo.Follow(ctx, change)
	}
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
