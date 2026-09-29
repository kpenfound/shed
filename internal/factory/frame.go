package factory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Frame builder outcomes.
const (
	outcomeFramed = "framed"
	frameStep     = "frame"
)

// Framing is how a frame builder session on a horizon clause ended.
type Framing struct {
	// Clause is the distant or eventual clause framed.
	Clause clause.ID
	// Outcome is what the session reported: "framed", "nothing", or "" when
	// it ended without an outcome.
	Outcome string
	// Reason says why a session ended without an outcome.
	Reason string
	// Problems name every change of a framed copy that breaks the check
	// (S.frame.2). A framing with problems keeps nothing.
	Problems []string
	// Change is the unit opened for a framing that passed the check.
	Change string
	// Added are the clauses the framing adds, in document order.
	Added []docs.HorizonChange
}

// Kept reports whether the framing opened a unit.
func (fr Framing) Kept() bool { return fr.Change != "" }

// frameTitle titles the unit that records a framing of a clause.
func frameTitle(id clause.ID) string {
	return "Frame " + id.String() + " into near and soon clauses"
}

// Frame runs one frame builder session on a distant or eventual horizon
// clause of main that is not realised (S.frame.1). The session works in a
// writable copy of main's files. When it reports framed and its copy passes
// the check (S.frame.2), shed opens a unit on main whose change holds the
// session's horizon.md, as a draft for the owner to read (S.frame.3).
// Otherwise it keeps nothing and opens no unit.
func (f *Factory) Frame(ctx context.Context, ref string) (fr Framing, err error) {
	id, err := clause.ParseID(ref)
	if err != nil {
		return Framing{}, err
	}
	main, err := f.mainSet(ctx)
	if err != nil {
		return Framing{}, err
	}
	entry, err := frameable(main, id)
	if err != nil {
		return Framing{}, err
	}
	sections, err := f.frameSections(main, entry)
	if err != nil {
		return Framing{}, err
	}
	fr.Clause = id

	change, err := f.Repo.NewUnit(ctx, frameTitle(id))
	if err != nil {
		return fr, err
	}
	keep := false
	defer func() {
		if !keep {
			err = errors.Join(err, f.Repo.Discard(ctx, change))
		}
	}()
	view := filepath.Join(f.State, "views", nonce())
	if err := f.Repo.Export(ctx, change, view); err != nil {
		return fr, err
	}
	defer os.RemoveAll(view)

	b, err := f.Provider.Bundle(ctx, bundle.Request{Role: unit.FrameBuilder, Main: main, Extra: sections})
	if err != nil {
		return fr, err
	}
	system, err := roles.System(f.State, roles.FrameBuilder)
	if err != nil {
		return fr, err
	}
	res, err := f.Sessions.Run(ctx, session.Turn{
		Role: unit.FrameBuilder, Step: frameStep, Dir: view, Writable: true,
		SystemPrompt: system, Bundle: b.Render(),
		Prompt:   fmt.Sprintf("Break %s into near and soon horizon clauses that refine it.", id),
		Outcomes: []string{outcomeFramed, outcomeNothing},
	})
	if err != nil {
		return fr, err
	}
	fr.Outcome = res.Status
	if res.Failure != session.NoFailure {
		fr.Outcome, fr.Reason = "", res.Reason
	}
	if fr.Outcome != outcomeFramed {
		return fr, nil
	}

	commit, err := f.Repo.Capture(ctx, change, view)
	if err != nil {
		return fr, fmt.Errorf("capturing the session's work: %w", err)
	}
	files, parent, err := f.Repo.Changed(ctx, change)
	if err != nil {
		return fr, err
	}
	fr.Added, fr.Problems, err = f.checkFraming(id, files, parent, commit)
	if err != nil || len(fr.Problems) > 0 {
		fr.Added = nil
		return fr, err
	}
	if err := f.Tracker.OpenUnit(change, frameTitle(id), unit.FrameBuilder); err != nil {
		return fr, err
	}
	keep, fr.Change = true, change
	return fr, nil
}

// frameable returns the trace entry of a clause shed frame may take: a
// horizon clause of main that is distant or eventual and not realised.
func frameable(main *docs.Set, id clause.ID) (docs.TraceEntry, error) {
	if id.Kind != clause.Horizon {
		return docs.TraceEntry{}, fmt.Errorf("%s is not a horizon clause", id)
	}
	for _, e := range docs.Trace(main) {
		if e.Clause.ID != id {
			continue
		}
		if e.Tier != "distant" && e.Tier != "eventual" {
			return e, fmt.Errorf("%s is a %s clause; only distant and eventual clauses are framed", id, e.Tier)
		}
		if e.Realised {
			return e, fmt.Errorf("%s is realised", id)
		}
		return e, nil
	}
	return docs.TraceEntry{}, fmt.Errorf("%s is not in the horizon on main", id)
}

// frameSections are the horizon, the clause to frame with the clauses that
// refine it and the spec clauses that advance it, and the units in flight,
// for the frame builder's bundle.
func (f *Factory) frameSections(main *docs.Set, e docs.TraceEntry) ([]bundle.Section, error) {
	var h strings.Builder
	for _, c := range main.Clauses(clause.Horizon) {
		fmt.Fprintf(&h, "- %s (%s) %s\n", c.ID, strings.Join(c.Tags, ", "), c.Text)
	}
	var target strings.Builder
	fmt.Fprintf(&target, "- %s (%s) %s\n", e.Clause.ID, strings.Join(e.Clause.Tags, ", "), e.Clause.Text)
	fmt.Fprintf(&target, "\nRefined by:\n\n")
	if len(e.RefinedBy) == 0 {
		target.WriteString("- nothing yet\n")
	}
	for _, id := range e.RefinedBy {
		c, _ := main.Lookup(id)
		fmt.Fprintf(&target, "- %s (%s) %s\n", c.ID, strings.Join(c.Tags, ", "), c.Text)
	}
	fmt.Fprintf(&target, "\nAdvanced by:\n\n")
	if len(e.AdvancedBy) == 0 {
		target.WriteString("- nothing yet\n")
	}
	for _, id := range e.AdvancedBy {
		c, _ := main.Lookup(id)
		fmt.Fprintf(&target, "- %s (%s) %s\n", c.ID, strings.Join(c.Tags, ", "), c.Text)
	}
	units, err := f.Tracker.Units()
	if err != nil {
		return nil, err
	}
	var flight strings.Builder
	for _, u := range units {
		if u.State.InFlight() {
			fmt.Fprintf(&flight, "- %s %q (%s), modifies %s, depends on %s, advances %s\n", unit.Short(u.Change), u.Title, u.State,
				orNone(u.Footprint.Modifies), orNone(u.Footprint.Depends), orNone(u.Footprint.Advances))
		}
	}
	if flight.Len() == 0 {
		flight.WriteString("- none\n")
	}
	return []bundle.Section{
		{Title: "Horizon", Body: h.String()},
		{Title: "Clause to frame", Body: target.String()},
		{Title: "Units in flight", Body: flight.String()},
	}, nil
}

// checkFraming checks a framed change against the main commit it sits on
// (S.frame.2): horizon.md is the only file changed, every change is an added
// horizon clause, at least one is added, and each added clause is near or
// soon, not realised, refines the framed clause and has an ID main never
// held. It returns the added clauses and every problem it finds.
func (f *Factory) checkFraming(target clause.ID, files []string, parent, commit string) ([]docs.HorizonChange, []string, error) {
	var problems []string
	for _, name := range files {
		if name != docs.HorizonPath {
			problems = append(problems, fmt.Sprintf("%s changed; a framing changes horizon.md alone", name))
		}
	}
	before, _ := docs.Load(revision.Git{Root: f.Root, Rev: parent})
	after, _ := docs.Load(revision.Git{Root: f.Root, Rev: commit})
	held, err := docs.Held(f.Root, parent)
	if err != nil {
		return nil, nil, err
	}
	var added []docs.HorizonChange
	for _, ch := range docs.DiffHorizon(before, after) {
		if ch.Change != "added" {
			problems = append(problems, fmt.Sprintf("%s is %s; a framing only adds clauses", ch.ID, ch.Change))
			continue
		}
		added = append(added, ch)
		c, _ := after.Lookup(ch.ID)
		var why []string
		if ch.Tier != "near" && ch.Tier != "soon" {
			why = append(why, "is not tagged near or soon")
		}
		var refines []string
		for _, t := range c.Tags {
			if t == docs.Realised {
				why = append(why, "is marked realised")
			}
			if rest, ok := strings.CutPrefix(t, docs.Refines); ok && (strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
				refines = append(refines, strings.TrimSpace(rest))
			}
		}
		if len(refines) != 1 || refines[0] != target.String() {
			why = append(why, "does not carry refines "+target.String())
		}
		if held[ch.ID] {
			why = append(why, "takes an ID main holds or has held")
		}
		if len(why) > 0 {
			problems = append(problems, fmt.Sprintf("added %s %s", ch.ID, strings.Join(why, ", ")))
		}
	}
	problems = append(problems, milestoneChanges(before, after)...)
	problems = append(problems, repeatedIDs(after)...)
	if len(added) == 0 {
		problems = append(problems, "the framing adds no horizon clause")
	}
	if slices.Contains(files, docs.HorizonPath) && !f.sameProse(parent, commit) {
		problems = append(problems, "horizon.md changes text outside its clauses; a framing only adds clauses")
	}
	return added, problems, nil
}

// milestoneChanges names every milestone of horizon.md that a framing adds,
// changes or removes, since a framing only adds horizon clauses.
func milestoneChanges(before, after *docs.Set) []string {
	old := map[clause.ID]clause.Clause{}
	for _, c := range before.Clauses(clause.Milestone) {
		old[c.ID] = c
	}
	var problems []string
	seen := map[clause.ID]bool{}
	for _, c := range after.Clauses(clause.Milestone) {
		seen[c.ID] = true
		o, ok := old[c.ID]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("%s is added; a framing only adds horizon clauses", c.ID))
		case o.Text != c.Text || !slices.Equal(o.Tags, c.Tags):
			problems = append(problems, fmt.Sprintf("%s is changed; a framing only adds horizon clauses", c.ID))
		}
	}
	for _, c := range before.Clauses(clause.Milestone) {
		if !seen[c.ID] {
			problems = append(problems, fmt.Sprintf("%s is removed; a framing only adds horizon clauses", c.ID))
		}
	}
	return problems
}

// repeatedIDs names every ID that horizon.md holds more than once.
func repeatedIDs(s *docs.Set) []string {
	if s.Horizon == nil {
		return nil
	}
	count := map[clause.ID]int{}
	var problems []string
	for _, c := range s.Horizon.Clauses {
		if count[c.ID]++; count[c.ID] == 2 {
			problems = append(problems, fmt.Sprintf("%s is added twice; each added clause takes an ID of its own", c.ID))
		}
	}
	return problems
}

// sameProse reports whether horizon.md's text outside its clauses is the
// same at two commits.
func (f *Factory) sameProse(before, after string) bool {
	a, errA := prose(revision.Git{Root: f.Root, Rev: before})
	b, errB := prose(revision.Git{Root: f.Root, Rev: after})
	return errA == nil && errB == nil && slices.Equal(a, b)
}

// prose returns the non-blank lines of horizon.md outside its clauses. A
// clause runs from its first line to the line before the next blank line,
// list item or heading.
func prose(src revision.Source) ([]string, error) {
	data, err := src.ReadFile(docs.HorizonPath)
	if err != nil {
		return nil, err
	}
	doc, _ := clause.Parse(docs.HorizonPath, data, clause.Horizon, clause.Milestone)
	starts := map[int]bool{}
	for _, c := range doc.Clauses {
		starts[c.Line] = true
	}
	var out []string
	inClause := false
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case starts[i+1]:
			inClause = true
		case trimmed == "", strings.HasPrefix(trimmed, "#"), strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			inClause = false
		}
		if !inClause && trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// DiscardFraming archives a unit shed frame opened that is still proposed,
// as deferred with no archive entry, and discards its change (S.frame.3).
func (f *Factory) DiscardFraming(ctx context.Context, ref string) (tracker.Unit, error) {
	u, err := f.Tracker.Unit(ref)
	if err != nil {
		return u, err
	}
	if u.OpenedBy != unit.FrameBuilder {
		return u, fmt.Errorf("unit %s was not opened by shed frame", unit.Short(u.Change))
	}
	if u.State != unit.Proposed {
		return u, fmt.Errorf("unit %s is %s; only a proposed framing is discarded", unit.Short(u.Change), u.State)
	}
	if err := f.Tracker.Archive(u.Change, unit.Deferred, unit.Owner, "the owner discarded the framing"); err != nil {
		return u, err
	}
	return u, f.Repo.Discard(ctx, u.Change)
}
