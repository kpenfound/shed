package factory

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Outcomes shed frame -expire reports (S.frame.6, S.frame.7).
const (
	// ExpiryArchived is a unit the session archived.
	ExpiryArchived = "archived"
	// ExpiryKept is a unit the session left contested.
	ExpiryKept = "kept"
	// ExpiryLeft is a unit that left contested while the session ran.
	ExpiryLeft = "left"
	// ExpiryAgain is a unit that moved to contested again while the
	// session ran.
	ExpiryAgain = "again"

	frameExpireStep = "frame-expire"
)

// Expiry is how shed frame -expire ended.
type Expiry struct {
	Change string
	// Outcome is one of the Expiry consts above, or "" when the session
	// ended without an outcome.
	Outcome string
	// Shelf is set when Outcome is ExpiryArchived.
	Shelf unit.Shelf
	// Reason is the session's reason for an archive, or why it ended
	// without an outcome.
	Reason string
}

// ExpireContested runs one frame builder session on a contested unit that
// shed inbox would mark overdue at now under S.owner.15, and refuses any
// other unit before starting a session, naming the unit and its state, or
// its wait and the timeout (S.frame.6). The session works in a copy of the
// unit's files whose changes are thrown away. When it reports archive and
// the unit's latest move to contested is still the one it had when the
// session started, shed archives the unit on the reported shelf as
// S.shed.10 does, with the frame builder as actor (S.frame.7). When it
// reports keep, ends without an outcome, or the unit left contested or
// moved to contested again while the session ran, shed moves nothing.
func (f *Factory) ExpireContested(ctx context.Context, ref string, now time.Time) (Expiry, error) {
	before, err := f.Tracker.Unit(ref)
	if err != nil {
		return Expiry{}, err
	}
	ex := Expiry{Change: before.Change}
	if before.State != unit.Contested {
		return ex, fmt.Errorf("unit %s is %s; shed frame -expire only takes a contested unit", unit.Short(before.Change), before.State)
	}
	timeout := f.Operator.Shed.ContestedTimeout.Duration
	wait := now.Sub(before.ContestedAt)
	if timeout <= 0 || wait <= timeout {
		return ex, fmt.Errorf("unit %s has waited %s; shed.contested_timeout is %s, so it is not overdue",
			unit.Short(before.Change), wait, timeout)
	}

	main, err := f.mainSet(ctx)
	if err != nil {
		return ex, err
	}
	events, err := f.Tracker.Events(before.Change)
	if err != nil {
		return ex, err
	}
	latest := tracker.LatestContest(events)
	tiered := latest.Tier != ""

	head, err := f.headSet(ctx, before.Change)
	if err != nil {
		return ex, err
	}
	record, err := f.record(before.Change)
	if err != nil {
		return ex, err
	}
	answers, err := f.Tracker.Answers(before.Change)
	if err != nil {
		return ex, err
	}

	// The session's directory is never captured back onto the unit's
	// change (S.frame.6), so it is left for the caller to inspect or clean
	// up, unlike a writable session's view.
	view := filepath.Join(f.State, "views", nonce())
	if err := f.Repo.Export(ctx, before.Change, view); err != nil {
		return ex, err
	}

	b, err := f.Provider.Bundle(ctx, bundle.Request{
		Role: unit.FrameBuilder, Unit: before, Main: main, Head: head,
		Debate: record, Answers: answers, Extra: expireSections(main, events, latest, timeout, wait),
	})
	if err != nil {
		return ex, err
	}
	system, err := roles.System(f.State, roles.FrameBuilder)
	if err != nil {
		return ex, err
	}
	res, err := f.Sessions.Run(ctx, session.Turn{
		Role: unit.FrameBuilder, Step: frameExpireStep, Dir: view, Writable: false,
		SystemPrompt: system, Bundle: b.Render(),
		Prompt:   fmt.Sprintf("Unit %s is overdue in contested. Decide whether to keep it waiting or archive it.", unit.Short(before.Change)),
		Outcomes: []string{"keep", "rejected", "deferred"},
		Check:    func(status, note string) error { return checkExpiry(main, tiered, status, note) },
	})
	if err != nil {
		return ex, err
	}
	if res.Failure != session.NoFailure {
		ex.Reason = res.Reason
		return ex, nil
	}
	if res.Status == "keep" {
		ex.Outcome = ExpiryKept
		return ex, nil
	}

	after, err := f.Tracker.Unit(before.Change)
	if err != nil {
		return ex, err
	}
	switch {
	case after.State != unit.Contested:
		ex.Outcome = ExpiryLeft
		return ex, nil
	case after.ContestedSeq != before.ContestedSeq:
		ex.Outcome, ex.Reason = ExpiryAgain, after.Reason
		return ex, nil
	}

	shelf := unit.Deferred
	if res.Status == "rejected" {
		shelf = unit.Rejected
	}
	var citations []string
	if shelf == unit.Rejected {
		citations, err = charterNamed(main, res.Note)
		if err != nil {
			return ex, err
		}
	}
	if _, err := f.shelve(ctx, before, shelf, citations, res.Note, unit.FrameBuilder, res.Note, nil, &expireInfo{Timeout: timeout, Wait: wait}); err != nil {
		return ex, err
	}
	ex.Outcome, ex.Shelf, ex.Reason = ExpiryArchived, shelf, res.Note
	return ex, nil
}

// expireSections are the bundle sections particular to shed frame -expire
// (S.frame.6): the whole horizon, the timeout and the wait, the reason of
// each bounce oldest first, and the reason of the unit's latest move to
// contested, saying whether that move was made under S.shed.16 or
// S.shed.18.
func expireSections(main *docs.Set, events []tracker.Event, latest tracker.Event, timeout, wait time.Duration) []bundle.Section {
	var horizon strings.Builder
	for _, c := range main.Clauses(clause.Horizon) {
		fmt.Fprintf(&horizon, "- %s (%s) %s\n", c.ID, strings.Join(c.Tags, ", "), c.Text)
	}

	var overdue strings.Builder
	fmt.Fprintf(&overdue, "- Timeout: %s\n- Wait: %s\n", tracker.FormatWait(timeout), tracker.FormatWait(wait))

	var bounces strings.Builder
	for _, e := range events {
		if e.Kind == tracker.UnitMoved && e.Bounce {
			fmt.Fprintf(&bounces, "- %s\n", e.Reason)
		}
	}
	if bounces.Len() == 0 {
		bounces.WriteString("- none\n")
	}

	var contest strings.Builder
	switch {
	case latest.Split:
		fmt.Fprintf(&contest, "- Made under S.shed.18: tier %s\n- Reason: %s\n", latest.Tier, latest.Reason)
	case latest.Tier != "":
		fmt.Fprintf(&contest, "- Made under S.shed.16: tier %s\n- Reason: %s\n", latest.Tier, latest.Reason)
	default:
		fmt.Fprintf(&contest, "- Reason: %s\n", latest.Reason)
	}

	return []bundle.Section{
		{Title: "Horizon", Body: horizon.String()},
		{Title: "Overdue", Body: overdue.String()},
		{Title: "Bounces, oldest first", Body: bounces.String()},
		{Title: "Latest move to contested", Body: contest.String()},
	}
}

// checkExpiry vets a status and note the done tool of shed frame -expire's
// session received (S.frame.6). It refuses an archive with an empty reason,
// a rejected archive whose reason names no charter clause or names one
// badly (S.owner.8), and a rejected archive for a unit whose latest move to
// contested was made under S.shed.16 or S.shed.18, since such a unit is
// contested only to wait for the owner's approve (S.shed.17) and may only
// be kept or deferred.
func checkExpiry(main *docs.Set, tiered bool, status, note string) error {
	switch status {
	case "keep":
		return nil
	case "deferred", "rejected":
		if strings.TrimSpace(note) == "" {
			return errors.New("an archive needs a reason")
		}
		if status == "rejected" {
			if tiered {
				return errors.New("this unit is contested only to wait for the owner's approve; it may be kept or deferred, not rejected")
			}
			if _, err := charterNamed(main, note); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("an outcome is keep, rejected or deferred, not %q", status)
}

// startExpiry dispatches ExpireContested's session on the oldest contested
// unit shed inbox would mark overdue at now, one at a time, before framing
// (S.frame.8, S.serve.4). It shares frameBuilderKey with startFraming, so no
// expiry session ever runs alongside another expiry session or a framing
// session, in either order; S.serve.6's pause is enforced by Serve skipping
// the whole pass, so no claim is recorded while it holds.
func (f *Factory) startExpiry(ctx context.Context, s *scheduler, units []tracker.Unit) error {
	if s.isBusy(frameBuilderKey) {
		return nil
	}
	timeout := f.Operator.Shed.ContestedTimeout.Duration
	if timeout <= 0 {
		return nil
	}
	now := s.now()
	var overdue []tracker.Unit
	for _, u := range units {
		if u.State == unit.Contested && now.Sub(u.ContestedAt) > timeout {
			overdue = append(overdue, u)
		}
	}
	sort.Slice(overdue, func(i, j int) bool { return overdue[i].ContestedSeq < overdue[j].ContestedSeq })
	for _, u := range overdue {
		claimed, err := f.Tracker.ClaimExpiry(u.Change, u.ContestedSeq)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		change := u.Change
		s.run(ctx, frameBuilderKey, unit.Short(change)+" expire", func(ctx context.Context) (string, error) {
			ex, err := f.ExpireContested(ctx, change, s.now())
			if err != nil {
				return "", err
			}
			return describeExpiry(ex), nil
		})
		return nil
	}
	return nil
}

// describeExpiry renders an expiry's outcome for serve's log: archived with
// its shelf, kept, ended without an outcome, or left alone because the unit
// left contested or moved to contested again while the session ran
// (S.frame.8).
func describeExpiry(ex Expiry) string {
	switch ex.Outcome {
	case ExpiryArchived:
		return fmt.Sprintf("archived on the %s shelf: %s", ex.Shelf, ex.Reason)
	case ExpiryKept:
		return "kept"
	case ExpiryLeft:
		return "left alone: the unit left contested while the session ran"
	case ExpiryAgain:
		return fmt.Sprintf("left alone: the unit moved to contested again while the session ran: %s", ex.Reason)
	default:
		return "ended without an outcome: " + ex.Reason
	}
}
