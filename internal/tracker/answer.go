package tracker

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// The kinds of answer the owner gives a contested unit.
const (
	Retry  = "retry"
	Defer  = "defer"
	Reject = "reject"
	// Approve is the owner's answer to a distant or eventual horizon
	// amendment (S.shed.17).
	Approve = "approve"
	// Keep is the owner's answer to a charter question (S.owner.10).
	Keep = "keep"
)

// Answer is the owner's answer to a contested unit: a move out of
// contested by the owner (S.owner.4, S.owner.5, S.owner.8).
type Answer struct {
	Time   time.Time
	Kind   string
	Reason string
}

// String writes an answer on one line with its time, kind and reason.
func (a Answer) String() string {
	return fmt.Sprintf("%s %s: %s", a.Time.UTC().Format(time.RFC3339), a.Kind, a.Reason)
}

// CheckAnswer reports whether the owner may give a unit an answer of a
// kind with a reason: the unit must be contested, the kind retry, defer,
// reject or approve and the reason not empty (S.owner.6). Approve also
// needs the unit's latest move to contested to be for the tier of its
// horizon amendment (S.shed.17).
func (t *Tracker) CheckAnswer(change, kind, reason string) error {
	if kind != Retry && kind != Defer && kind != Reject && kind != Approve {
		return fmt.Errorf("an answer is %s, %s, %s or %s, not %q", Retry, Defer, Reject, Approve, kind)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("an answer needs a reason")
	}
	u, err := t.Unit(change)
	if err != nil {
		return err
	}
	if u.State != unit.Contested {
		return fmt.Errorf("unit %s is %s; only contested units are answered", unit.Short(u.Change), u.State)
	}
	if kind == Approve {
		events, err := t.Events(u.Change)
		if err != nil {
			return err
		}
		if tier := contestedTier(events); tier == "" {
			return fmt.Errorf("unit %s is not contested for the tier of its horizon amendment; only such a unit is approved", unit.Short(u.Change))
		}
	}
	return nil
}

// contestedTier is the tier of the horizon amendment a unit's latest move
// to contested was made for, or "" when that move was for another reason.
func contestedTier(events []Event) string {
	for i := len(events) - 1; i >= 0; i-- {
		if e := events[i]; e.Kind == UnitMoved && e.To == unit.Contested {
			return e.Tier
		}
	}
	return ""
}

// Approve moves a unit contested for the tier of its horizon amendment to
// proposed on the owner's answer, marking the move as the approval
// (S.shed.17).
func (t *Tracker) Approve(change, reason string) error {
	if err := t.CheckAnswer(change, Approve, reason); err != nil {
		return err
	}
	return t.move(change, Event{To: unit.Proposed, Actor: unit.Owner, Reason: reason, Approved: true})
}

// Retry moves a contested unit to proposed on the owner's answer. The unit
// keeps its bounce count.
func (t *Tracker) Retry(change, reason string) error {
	if err := t.CheckAnswer(change, Retry, reason); err != nil {
		return err
	}
	return t.move(change, Event{To: unit.Proposed, Actor: unit.Owner, Reason: reason})
}

// Answers returns the owner's answers to a unit, oldest first. Only the
// owner's answers take a unit out of contested, so every move out of
// contested by the owner is one.
func (t *Tracker) Answers(change string) ([]Answer, error) {
	events, err := t.Events(change)
	if err != nil {
		return nil, err
	}
	var out []Answer
	for _, e := range events {
		if e.Kind != UnitMoved || e.From != unit.Contested || e.Actor != unit.Owner {
			continue
		}
		kind := Retry
		switch {
		case e.To == unit.Archived && e.Shelf == unit.Rejected:
			kind = Reject
		case e.To == unit.Archived:
			kind = Defer
		case e.Approved:
			kind = Approve
		}
		out = append(out, Answer{Time: e.Time, Kind: kind, Reason: e.Reason})
	}
	return out, nil
}
