package factory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Painter outcomes.
const (
	outcomeProposed = "proposed"
	outcomeNothing  = "nothing"
	proposeStep     = "propose"
	// painterDraft titles a unit the painter has not declared yet.
	painterDraft = "Proposal from the painter"
)

// Gap returns the horizon clauses a painter may propose against: near and
// soon clauses that are not realised and that no unit in flight advances.
func (f *Factory) Gap(ctx context.Context) ([]docs.TraceEntry, error) {
	main, err := f.mainSet(ctx)
	if err != nil {
		return nil, err
	}
	units, err := f.Tracker.Units()
	if err != nil {
		return nil, err
	}
	var taken []string
	for _, u := range units {
		if u.State.InFlight() {
			taken = append(taken, u.Footprint.Advances...)
		}
	}
	var out []docs.TraceEntry
	for _, e := range docs.Gap(main) {
		if (e.Tier == "near" || e.Tier == "soon") && !slices.Contains(taken, e.Clause.ID.String()) {
			out = append(out, e)
		}
	}
	return out, nil
}

// PainterDue reports whether the painter may propose now.
func (f *Factory) PainterDue(ctx context.Context, now time.Time) (bool, error) {
	why, err := f.PainterWait(ctx, now)
	return why == "" && err == nil, err
}

// PainterWait says why the painter may not propose now, or returns "" when
// it may: fewer than painter.max_proposed units are proposed, it is not
// backing off, and the gap it may work on is not empty. A framing opened by
// shed frame does not count as a proposal (S.frame.3).
func (f *Factory) PainterWait(ctx context.Context, now time.Time) (string, error) {
	units, err := f.Tracker.Units()
	if err != nil {
		return "", err
	}
	proposed := 0
	for _, u := range units {
		if u.State == unit.Proposed && u.OpenedBy != unit.FrameBuilder {
			proposed++
		}
	}
	if max := f.Operator.Painter.MaxProposed; proposed >= max {
		return fmt.Sprintf("%d proposals are waiting in the shed (painter.max_proposed = %d)", proposed, max), nil
	}
	streak, since, err := f.unproductive(units)
	if err != nil {
		return "", err
	}
	if streak > 0 {
		wait := f.backoff(streak)
		if next := since.Add(wait); now.Before(next) {
			what := "the last proposal"
			if streak > 1 {
				what = fmt.Sprintf("the last %d proposals", streak)
			}
			return fmt.Sprintf("%s went nowhere, so the next is due at %s (a %s wait, doubling from painter.interval up to painter.max_interval)",
				what, next.Local().Format("15:04"), wait), nil
		}
	}
	gap, err := f.Gap(ctx)
	if err != nil {
		return "", err
	}
	if len(gap) == 0 {
		return "the gap holds no near or soon horizon clause that no unit in flight advances", nil
	}
	return "", nil
}

// unproductive counts the painter's latest proposals that went nowhere, in
// a row, and returns when the most recent of them did. A proposal went
// nowhere when it was archived or contested without ever being sealed; the
// streak ends at the last one that was sealed. A proposal still in the shed
// has no answer yet and is passed over, and so is one whose painter session
// failed before doing anything.
func (f *Factory) unproductive(units []tracker.Unit) (int, time.Time, error) {
	streak := 0
	var since time.Time
	for i := len(units) - 1; i >= 0; i-- {
		u := units[i]
		if u.OpenedBy != unit.Painter {
			continue
		}
		if u.Seal != nil {
			break
		}
		if u.State != unit.Archived && u.State != unit.Contested {
			continue
		}
		worked, err := f.painterWorked(u.Change)
		if err != nil {
			return 0, time.Time{}, err
		}
		if !worked {
			continue
		}
		streak++
		if u.Updated.After(since) {
			since = u.Updated
		}
	}
	return streak, since, nil
}

// painterWorked reports whether a painter session on a unit did some work:
// reported an outcome, or cost money.
func (f *Factory) painterWorked(change string) (bool, error) {
	sessions, err := f.Tracker.Sessions(change)
	if err != nil {
		return false, err
	}
	for _, s := range sessions {
		if s.Role == unit.Painter && s.Step == proposeStep && (s.Status == tracker.Succeeded || s.CostUSD > 0) {
			return true, nil
		}
	}
	return false, nil
}

// backoff is the painter's wait after a streak of proposals that went
// nowhere: painter.interval, doubled for each further one, up to
// painter.max_interval.
func (f *Factory) backoff(streak int) time.Duration {
	wait, max := f.Operator.Painter.Interval.Duration, f.Operator.Painter.MaxInterval.Duration
	for i := 1; i < streak && wait < max; i++ {
		wait *= 2
	}
	return min(wait, max)
}

// Propose runs the painter: shed opens a unit on main and the painter writes
// a spec diff for part of the gap and declares it. A painter that finds
// nothing worth proposing leaves nothing behind: its unit is archived as
// deferred, with no archive entry, and its change discarded.
func (f *Factory) Propose(ctx context.Context) (string, Outcome, error) {
	gap, err := f.Gap(ctx)
	if err != nil {
		return "", "", err
	}
	if len(gap) == 0 {
		return "", Discarded, nil
	}
	sections, err := f.painterSections(gap)
	if err != nil {
		return "", "", err
	}
	change, err := f.Repo.NewUnit(ctx, "proposal")
	if err != nil {
		return "", "", err
	}
	if err := f.Tracker.OpenUnit(change, painterDraft, unit.Painter); err != nil {
		return "", "", errors.Join(err, f.Repo.Discard(ctx, change))
	}
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", "", err
	}

	type declareIn struct {
		Title    string   `json:"title" jsonschema:"a short title for the proposal"`
		Summary  string   `json:"summary" jsonschema:"what the proposal adds and why"`
		Depends  []string `json:"depends,omitempty" jsonschema:"spec clauses on main the proposal depends on"`
		Advances []string `json:"advances" jsonschema:"horizon clauses from the gap the proposal advances"`
		Estimate float64  `json:"estimate" jsonschema:"a positive estimate in USD of what taking the unit from sealed to landed will cost"`
	}
	var mu sync.Mutex
	var declared *declareIn
	offered := map[string]bool{}
	for _, e := range gap {
		offered[e.Clause.ID.String()] = true
	}
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Painter, Prompt: roles.Painter, Step: proposeStep, Writable: true,
		Task: "Propose the next increment of the gap as a spec diff.",
		Tools: func(_ string, _ *docs.Set) []session.Tool {
			return []session.Tool{session.NewTool("declare", "Declare the proposal's title, summary, dependencies and the horizon clauses it advances.",
				func(_ context.Context, in declareIn) (string, error) {
					if strings.TrimSpace(in.Title) == "" || len(in.Advances) == 0 {
						return "", errors.New("a proposal needs a title and at least one horizon clause it advances")
					}
					if in.Estimate <= 0 {
						return "", errors.New("a proposal needs a positive estimate in USD")
					}
					for _, a := range in.Advances {
						if !offered[a] {
							return "", fmt.Errorf("%s is not in the gap you may propose against", a)
						}
					}
					for _, d := range in.Depends {
						if id, err := clause.ParseID(d); err != nil || id.Kind != clause.Spec {
							return "", fmt.Errorf("%s is not a spec clause", d)
						}
					}
					mu.Lock()
					declared = &in
					mu.Unlock()
					return "declared", nil
				})}
		},
		Outcomes: []string{outcomeProposed, outcomeNothing},
		StepDone: outcomeProposed,
		Extra:    sections,
	})
	if err != nil {
		return change, "", err
	}
	mu.Lock()
	d := declared
	mu.Unlock()
	if res.Status != outcomeProposed || d == nil {
		reason := "the painter found nothing worth proposing"
		if res.Status == outcomeProposed {
			reason = "the painter proposed without declaring its proposal"
		} else if res.Failure != session.NoFailure {
			reason = "the painter's session failed: " + res.Reason
		}
		if err := f.Tracker.Archive(change, unit.Deferred, unit.Painter, reason); err != nil {
			return change, "", err
		}
		return change, Discarded, f.Repo.Discard(ctx, change)
	}
	if err := f.Declare(ctx, change, d.Title, d.Depends, d.Advances, unit.Painter, d.Estimate); err != nil {
		out, berr := f.bounce(u, "the painter's declaration was refused: "+err.Error())
		return change, out, berr
	}
	return change, Outcome(outcomeProposed), nil
}

// painterSections are the gap, the shelves and the units in flight, for the
// painter's bundle.
func (f *Factory) painterSections(gap []docs.TraceEntry) ([]bundle.Section, error) {
	var g strings.Builder
	for _, e := range gap {
		fmt.Fprintf(&g, "- %s (%s) %s", e.Clause.ID, e.Tier, e.Clause.Text)
		if len(e.AdvancedBy) > 0 {
			fmt.Fprintf(&g, " Already advanced by %s.", docs.JoinIDs(e.AdvancedBy))
		}
		if e.Refines != (clause.ID{}) {
			fmt.Fprintf(&g, " Refines %s (%s).", e.Refines, e.RefinesTier)
		}
		g.WriteString("\n")
	}
	entries, err := archive.Read(f.Root)
	if err != nil {
		return nil, err
	}
	units, err := f.Tracker.Units()
	if err != nil {
		return nil, err
	}
	var flight strings.Builder
	for _, u := range units {
		if u.State.InFlight() {
			fmt.Fprintf(&flight, "- %s %q (%s), modifies %s, advances %s\n", unit.Short(u.Change), u.Title, u.State,
				orNone(u.Footprint.Modifies), orNone(u.Footprint.Advances))
		}
	}
	return []bundle.Section{
		{Title: "Gap", Body: g.String()},
		{Title: "Deferred shelf", Body: archive.Shelf(entries, unit.Deferred)},
		{Title: "Rejected shelf", Body: archive.Shelf(entries, unit.Rejected)},
		{Title: "Units in flight", Body: flight.String()},
	}, nil
}

func orNone(ids []string) string {
	if len(ids) == 0 {
		return "nothing"
	}
	return strings.Join(ids, ", ")
}
