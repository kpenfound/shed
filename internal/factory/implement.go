package factory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/landing"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// Mechanic and reviewer outcomes.
const (
	outcomeDone         = "done"
	outcomeReopen       = "reopen"
	outcomeAmend        = "amend"
	outcomePass         = "pass"
	outcomeFail         = "fail"
	outcomeSpecWrong    = "spec-wrong"
	outcomeResolved     = "resolved"
	outcomeUnresolvable = "unresolvable"
)

// maxStepFailures is how many sessions of one step may fail before the unit
// reopens.
const maxStepFailures = 3

// runner returns the project's test runner, from the repository shed runs in.
func (f *Factory) runner() proof.Runner {
	prefix := slices.Clone(f.Project.Proofs.Runner)
	if len(prefix) > 0 && strings.ContainsRune(prefix[0], '/') && !strings.HasPrefix(prefix[0], "/") {
		prefix[0] = f.Root + "/" + prefix[0]
	}
	return proof.Runner{Root: f.Root, Prefix: prefix}
}

// testTools gives a session the project's tests in its directory.
func (f *Factory) testTools(dir string, _ *docs.Set) []session.Tool {
	return session.TestTools(dir, f.runner())
}

// nextStep returns the first step of a formula whose needs have finished
// and that has not finished itself, or false when every step has.
func nextStep(formula config.Formula, finished []string) (config.Step, bool) {
	for _, s := range formula.Steps {
		if slices.Contains(finished, s.Name) {
			continue
		}
		ready := true
		for _, n := range s.Needs {
			ready = ready && slices.Contains(finished, n)
		}
		if ready {
			return s, true
		}
	}
	return config.Step{}, false
}

// Implement runs a sealed or implementing unit's formula: one mechanic
// session per step, in order, each once the steps it needs have finished.
// When every step has finished the unit moves to verifying.
func (f *Factory) Implement(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	switch u.State {
	case unit.Sealed:
		if err := f.Tracker.Move(u.Change, unit.Implementing, unit.Shed, "a mechanic is dispatched"); err != nil {
			return "", err
		}
	case unit.Implementing:
	default:
		return "", fmt.Errorf("unit %s is %s; only sealed and implementing units are implemented", unit.Short(u.Change), u.State)
	}
	formula := f.Operator.Formulas[config.DefaultFormula]
	amended, err := f.amended(change)
	if err != nil {
		return "", err
	}
	for {
		if u, err = f.Tracker.Unit(u.Change); err != nil {
			return "", err
		}
		step, ok := nextStep(formula, u.Steps)
		if !ok {
			if err := f.Tracker.Move(u.Change, unit.Verifying, unit.Mechanic, "every step of the formula finished"); err != nil {
				return "", err
			}
			return Implemented, nil
		}
		failures, err := f.stepFailures(u.Change, step.Name)
		if err != nil {
			return "", err
		}
		if failures >= maxStepFailures {
			return f.reopen(u, unit.Mechanic, fmt.Sprintf("the %s step failed %d times", step.Name, failures), false)
		}
		conflicts, err := f.conflictSection(ctx, u.Change)
		if err != nil {
			return "", err
		}
		extra := append(slices.Clone(amended), conflicts...)
		res, err := f.session(ctx, work{
			Unit: u, Role: unit.Mechanic, Prompt: roles.Mechanic, Step: step.Name, Writable: true,
			Task:     fmt.Sprintf("Work on the %s step of %q.", step.Name, u.Title),
			Tools:    f.testTools,
			Outcomes: []string{outcomeDone, outcomeReopen, outcomeAmend},
			Check:    amendCheck(u.Footprint),
			StepDone: outcomeDone,
			Extra:    append(extra, bundle.Section{Title: "Step", Body: formulaText(formula, step.Name, u.Steps)}),
		})
		if err != nil {
			return "", err
		}
		switch {
		case res.Status == outcomeReopen:
			return f.reopen(u, unit.Mechanic, "the mechanic found the sealed spec wrong: "+res.Note, false)
		case res.Status == outcomeAmend:
			return f.reopen(u, unit.Mechanic, "the mechanic requested an amendment:\n"+res.Note, true)
		case res.Failure != session.NoFailure:
			// Tried again next time, until the step has failed too often.
			return Failed, nil
		}
	}
}

// amended tells a unit's mechanics of the amendment it was last sealed
// after, if it was sealed out of the amendment lane (S.shed.13): the spec
// clauses the amendment changed between the unit's commits at its earlier
// seal and at this one. When the seal rejected the amendment, it tells them
// instead that the sealed spec stands and which objections stood
// (S.shed.14).
func (f *Factory) amended(change string) ([]bundle.Section, error) {
	events, err := f.Tracker.Events(change)
	if err != nil {
		return nil, err
	}
	last := -1
	for i, e := range events {
		if e.Kind == tracker.UnitMoved && e.To == unit.Sealed && e.Seal != nil {
			last = i
		}
	}
	if last < 0 {
		return nil, nil
	}
	if events[last].Rejected {
		body := "The mechanic requested an amendment, and the amendment was rejected: the sealed spec stands as written. " +
			"Implement it as it is. These objections stood at the debate's round cap:\n\n" + events[last].Reason + "\n"
		return []bundle.Section{{Title: "Rejected amendment", Body: body}}, nil
	}
	earlier := laneOf(events[:last])
	if earlier == nil {
		return nil, nil
	}
	was, now := earlier.Seal, events[last].Seal
	body := "This unit was resealed after an amendment. "
	if was.Commit == "" || now.Commit == "" {
		body += "Its seals do not record the unit's commits, so the amendment's diff cannot be given.\n"
	} else {
		load := func(rev string) *docs.Set {
			set, _ := docs.Load(revision.Git{Root: f.Root, Rev: rev})
			return set
		}
		diff := bundle.Amendment(load(was.Commit), load(now.Commit), load(was.Main), load(now.Main))
		if diff == "" {
			body += "The amendment changed no clause.\n"
		} else {
			body += "The amendment changed these spec clauses, from the unit's spec at its earlier seal to its spec at this one:\n\n" + diff
		}
	}
	return []bundle.Section{{Title: "Amendment", Body: body}}, nil
}

// amendCheck refuses an amend whose note cites no clause of the unit's
// sealed spec: the clauses its footprint modifies or depends on.
func amendCheck(fp tracker.Footprint) func(status, note string) error {
	return func(status, note string) error {
		if status != outcomeAmend {
			return nil
		}
		for _, id := range clause.Mentioned(note) {
			if slices.Contains(fp.Modifies, id.String()) || slices.Contains(fp.Depends, id.String()) {
				return nil
			}
		}
		sealed := append(slices.Clone(fp.Modifies), fp.Depends...)
		return fmt.Errorf("an amend names each sealed clause it wants changed; the note cites none of %s", strings.Join(sealed, ", "))
	}
}

func formulaText(formula config.Formula, current string, finished []string) string {
	var b strings.Builder
	for _, s := range formula.Steps {
		state := "to do"
		switch {
		case s.Name == current:
			state = "this session"
		case slices.Contains(finished, s.Name):
			state = "finished"
		}
		fmt.Fprintf(&b, "- %s (%s)", s.Name, state)
		if len(s.Needs) > 0 {
			fmt.Fprintf(&b, ", needs %s", strings.Join(s.Needs, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// stepFailures counts the failed sessions of a step since the unit last
// started its work.
func (f *Factory) stepFailures(change, step string) (int, error) {
	sessions, err := f.Tracker.Sessions(change)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range sessions {
		switch {
		case s.Step == step && s.Status == tracker.Failed:
			n++
		case s.Step == step && s.Status == tracker.Succeeded:
			n = 0
		}
	}
	return n, nil
}

func (f *Factory) reopen(u tracker.Unit, actor unit.Actor, reason string, amendment bool) (Outcome, error) {
	if err := f.Tracker.Reopen(u.Change, actor, reason, amendment); err != nil {
		return "", err
	}
	after, err := f.Tracker.Unit(u.Change)
	if err != nil {
		return "", err
	}
	if after.State == unit.Contested {
		return Contested, nil
	}
	return Reopened, nil
}

// Verify verifies an implemented unit. The unit's documents must pass the
// checks of shed check, no file on its change may hold an unresolved
// conflict (S.vcs.12), its horizon changes may only mark clauses it
// advances as realised, and every proof of its footprint, or every proof
// when the project asks, must pass. A committee member who did not work on
// the unit then reviews it. A unit that passes is queued; one that fails
// goes back to implementing with a notice to the mechanic; one whose spec
// the reviewer finds wrong reopens.
func (f *Factory) Verify(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if u.State != unit.Verifying {
		return "", fmt.Errorf("unit %s is %s; only verifying units are verified", unit.Short(u.Change), u.State)
	}
	problems, err := f.checkUnit(ctx, u)
	if err != nil {
		return "", err
	}
	if len(problems) > 0 {
		return f.sendBack(u, "Verification found problems before review:\n"+strings.Join(problems, "\n"))
	}

	var mu sync.Mutex
	var findings []string
	type findingIn struct {
		Citations []string `json:"citations" jsonschema:"the clause IDs the finding concerns"`
		Text      string   `json:"text" jsonschema:"what is wrong"`
	}
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Committee, Prompt: roles.CommitteeReview, Step: "review",
		Task: fmt.Sprintf("Verify %q before it lands.", u.Title),
		Tools: func(dir string, head *docs.Set) []session.Tool {
			resolver := docs.NewResolver(f.Root, head)
			return append(f.testTools(dir, head), session.NewTool("finding", "Record a problem with the unit, citing clause IDs.",
				func(_ context.Context, in findingIn) (string, error) {
					if len(in.Citations) == 0 || strings.TrimSpace(in.Text) == "" {
						return "", errors.New("a finding cites at least one clause and says what is wrong")
					}
					for _, c := range in.Citations {
						cit, err := clause.ParseCitation(c)
						if err != nil {
							return "", err
						}
						if _, err := resolver.Resolve(cit); err != nil {
							return "", err
						}
					}
					mu.Lock()
					findings = append(findings, fmt.Sprintf("- %s: %s", strings.Join(in.Citations, ", "), in.Text))
					mu.Unlock()
					return "finding recorded", nil
				}))
		},
		Outcomes: []string{outcomePass, outcomeFail, outcomeSpecWrong},
	})
	if err != nil {
		return "", err
	}
	switch res.Status {
	case outcomePass:
		if err := f.Tracker.Move(u.Change, unit.Queued, unit.Committee, "verified"); err != nil {
			return "", err
		}
		return Verified, nil
	case outcomeSpecWrong:
		return f.reopen(u, unit.Committee, "the reviewer found the sealed spec wrong:\n"+strings.Join(findings, "\n"), false)
	case outcomeFail:
		return f.sendBack(u, "The reviewer found problems:\n"+strings.Join(findings, "\n")+"\n"+res.Note)
	}
	return Failed, nil
}

// sendBack returns a verifying unit to implementing with a notice for the
// mechanic.
func (f *Factory) sendBack(u tracker.Unit, why string) (Outcome, error) {
	if _, err := f.Tracker.AddNotice(u.Change, unit.Mechanic, "verification", why, unit.Committee); err != nil {
		return "", err
	}
	first := strings.SplitN(why, "\n", 2)[0]
	if err := f.Tracker.Move(u.Change, unit.Implementing, unit.Committee, first); err != nil {
		return "", err
	}
	return Failed, nil
}

// checkUnit runs the mechanical checks of verification on a unit's change
// and returns each problem found.
func (f *Factory) checkUnit(ctx context.Context, u tracker.Unit) ([]string, error) {
	view := f.State + "/views/" + nonce()
	if err := f.Repo.Export(ctx, u.Change, view); err != nil {
		return nil, err
	}
	defer os.RemoveAll(view)
	var out []string
	add := func(p clause.Problem) { out = append(out, "- "+p.String()) }
	conflicted, _, err := f.unresolved(ctx, u.Change)
	if err != nil {
		return nil, err
	}
	for _, name := range conflicted {
		out = append(out, fmt.Sprintf("- %s holds an unresolved conflict with main; resolve it against the sealed spec", name))
	}

	head, problems := docs.Load(revision.Worktree(view))
	for _, p := range problems {
		add(p)
	}
	for _, p := range docs.Validate(head, docs.NewResolver(view, head)) {
		add(p)
	}
	proofs, problems, err := proof.Discover(view)
	if err != nil {
		return nil, err
	}
	for _, p := range problems {
		add(p)
	}
	for _, p := range proof.Check(proofs, head) {
		add(p)
	}
	main, err := f.mainSet(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range horizonChanges(main, head, u.Footprint.Advances) {
		out = append(out, "- "+p)
	}

	var ids []clause.ID
	if f.Project.Verify.AllProofs {
		for _, c := range head.Clauses(clause.Spec) {
			ids = append(ids, c.ID)
		}
	} else {
		for _, s := range append(slices.Clone(u.Footprint.Modifies), u.Footprint.Depends...) {
			id, err := clause.ParseID(s)
			if err != nil {
				continue
			}
			if _, ok := head.Lookup(id); ok && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	var selected []proof.Proof
	for _, id := range ids {
		selected = append(selected, proof.For(proofs, id)...)
	}
	runner := f.runner()
	runner.Root = view
	results, err := runner.Run(ctx, selected)
	if err != nil {
		return nil, err
	}
	for _, cr := range proof.ByClause(ids, proofs, results) {
		if cr.Pass {
			continue
		}
		var notes []string
		for name, s := range cr.Proofs {
			if s != proof.Pass {
				notes = append(notes, name+" "+string(s))
			}
		}
		if len(notes) == 0 {
			notes = []string{"no proof"}
		}
		slices.Sort(notes)
		out = append(out, fmt.Sprintf("- %s fails: %s", cr.ID, strings.Join(notes, ", ")))
	}
	return out, nil
}

// horizonChanges checks that a unit changes the horizon only by marking
// clauses it advances as realised.
func horizonChanges(main, head *docs.Set, advances []string) []string {
	var out []string
	before := map[string]clauseText{}
	for _, c := range main.Clauses(clause.Horizon) {
		before[c.ID.String()] = textOf(c)
	}
	for _, c := range main.Clauses(clause.Milestone) {
		before[c.ID.String()] = textOf(c)
	}
	after := map[string]clauseText{}
	for _, kind := range []clause.Kind{clause.Horizon, clause.Milestone} {
		for _, c := range head.Clauses(kind) {
			id := c.ID.String()
			after[id] = textOf(c)
			old, ok := before[id]
			switch {
			case !ok:
				out = append(out, fmt.Sprintf("the unit adds %s to the horizon; units only mark clauses realised", id))
			case old == after[id]:
			case old.text != after[id].text || !markedRealised(old.tags, after[id].tags):
				out = append(out, fmt.Sprintf("the unit changes %s in the horizon; units only mark clauses they advance realised", id))
			case !slices.Contains(advances, id):
				out = append(out, fmt.Sprintf("the unit marks %s realised but does not advance it", id))
			}
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			out = append(out, fmt.Sprintf("the unit removes %s from the horizon", id))
		}
	}
	slices.Sort(out)
	return out
}

type clauseText struct{ text, tags string }

func textOf(c clause.Clause) clauseText {
	return clauseText{text: c.Text, tags: strings.Join(c.Tags, ",")}
}

// markedRealised reports whether after is before with realised added.
func markedRealised(before, after string) bool {
	return after == before+","+docs.Realised && !strings.Contains(before, docs.Realised)
}

// Land lands a queued unit. The unit is first rebased onto main with any
// conflicts kept in its files, and a wheelbuilder resolves them against the
// sealed spec; if they cannot be resolved the unit reopens. A unit that
// changes nothing reopens. Once it lands, every other unit in flight is
// rebased onto the new main (S.vcs.10).
func (f *Factory) Land(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if u.State != unit.Queued {
		return "", fmt.Errorf("unit %s is %s; only queued units land", unit.Short(u.Change), u.State)
	}
	// A unit already on main only needs recording.
	if landed, err := f.Repo.OnMain(ctx, u.Change); err != nil || !landed {
		if err != nil {
			return "", err
		}
		conflicted, err := f.Repo.Rebase(ctx, u.Change)
		if err != nil {
			return "", err
		}
		if conflicted {
			res, err := f.session(ctx, work{
				Unit: u, Role: unit.Wheelbuilder, Prompt: roles.Wheelbuilder, Step: "resolve", Writable: true,
				Task:     fmt.Sprintf("Resolve the conflicts between %q and main.", u.Title),
				Tools:    f.testTools,
				Outcomes: []string{outcomeResolved, outcomeUnresolvable},
			})
			if err != nil {
				return "", err
			}
			if res.Status != outcomeResolved {
				return f.reopen(u, unit.Wheelbuilder, "the unit conflicts with main and the wheelbuilder could not resolve it: "+res.Note, false)
			}
		}
	}
	_, err = landing.Land(ctx, f.Tracker, f.Repo, u.Change, unit.Wheelbuilder)
	switch {
	case err == nil:
		_ = f.sweep(ctx, u.Change)
		return Landed, nil
	case errors.Is(err, vcs.ErrConflict):
		return f.reopen(u, unit.Wheelbuilder, "conflicts with main remain after resolution", false)
	case errors.Is(err, vcs.ErrEmpty):
		return f.reopen(u, unit.Wheelbuilder, "the unit changes nothing", false)
	}
	return "", err
}

// Run takes a unit through the shed, implementation, verification and
// landing, stopping when it lands, leaves the shed's path, or a stage makes
// no progress.
func (f *Factory) Run(ctx context.Context, change string) (Outcome, error) {
	for {
		u, err := f.Tracker.Unit(change)
		if err != nil {
			return "", err
		}
		var out Outcome
		switch u.State {
		case unit.Proposed:
			out, err = f.Debate(ctx, u.Change)
			if out == Sealed {
				continue
			}
		case unit.Sealed, unit.Implementing:
			out, err = f.Implement(ctx, u.Change)
			if out == Implemented {
				continue
			}
		case unit.Verifying:
			out, err = f.Verify(ctx, u.Change)
			if out == Verified {
				continue
			}
		case unit.Queued:
			out, err = f.Land(ctx, u.Change)
		case unit.Landed:
			return Landed, nil
		default:
			return Outcome(u.State), nil
		}
		return out, err
	}
}
