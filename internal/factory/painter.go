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

// PainterDue reports whether the painter may propose now: the gap it may
// work on is not empty, fewer than painter.max_proposed units are proposed,
// and painter.interval has passed since its last proposal.
func (f *Factory) PainterDue(ctx context.Context, now time.Time) (bool, error) {
	units, err := f.Tracker.Units()
	if err != nil {
		return false, err
	}
	proposed := 0
	for _, u := range units {
		if u.State == unit.Proposed {
			proposed++
		}
	}
	if proposed >= f.Operator.Painter.MaxProposed {
		return false, nil
	}
	last, ok, err := f.Tracker.LastSession(unit.Painter, proposeStep)
	if err != nil {
		return false, err
	}
	if ok && now.Sub(last) < f.Operator.Painter.Interval.Duration {
		return false, nil
	}
	gap, err := f.Gap(ctx)
	return len(gap) > 0, err
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
	if err := f.Declare(ctx, change, d.Title, d.Depends, d.Advances, unit.Painter); err != nil {
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
