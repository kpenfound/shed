package factory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Horizon review outcomes.
const (
	outcomeConsistent = "consistent"
	reviewStep        = "review"
)

// ReviewHorizon runs the horizon review of a unit marked for it (S.queue.5):
// one wheelbuilder session whose bundle shows the unit's pending notices
// without delivering them. On consistent the unit keeps its state and its
// seal, and a notice giving the wheelbuilder's reason joins its pending
// notices; on reopen the unit reopens with that reason. Either clears the
// mark. A session that reports neither leaves it.
func (f *Factory) ReviewHorizon(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if !u.Review || !unit.PastSeal(u.State) {
		return "", fmt.Errorf("unit %s is not marked for horizon review", unit.Short(u.Change))
	}
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Wheelbuilder, Prompt: roles.WheelbuilderReview, Step: reviewStep,
		Task:     fmt.Sprintf("Review %q against the horizon clauses a landing changed.", u.Title),
		Outcomes: []string{outcomeConsistent, outcomeReopen},
		Check:    reasoned,
		Show:     true,
	})
	if err != nil {
		return "", err
	}
	switch res.Status {
	case outcomeConsistent:
		if err := f.Tracker.Consistent(u.Change, unit.Wheelbuilder, nextSession, res.Note); err != nil {
			return "", err
		}
		return Consistent, nil
	case outcomeReopen:
		return f.reopen(u, unit.Wheelbuilder, "the horizon review found the unit inconsistent with the changed horizon: "+res.Note, false)
	}
	return Failed, nil
}

// reasoned refuses an outcome without a written reason.
func reasoned(_, note string) error {
	if strings.TrimSpace(note) == "" {
		return errors.New("give the reason for this outcome in the note")
	}
	return nil
}

// unmarked refuses to start a stage on a unit marked for horizon review.
func unmarked(u tracker.Unit, stage string) error {
	if u.Review {
		return fmt.Errorf("unit %s is marked for horizon review; it is not %s until the review clears the mark", unit.Short(u.Change), stage)
	}
	return nil
}
