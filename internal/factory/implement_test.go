package factory

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

const byeProof = `package greet

import "testing"

//shed:proves S.core.2
func TestBye(t *testing.T) {
	if Bye() != "goodbye" {
		t.Fatal(Bye())
	}
}
`

const byeCode = "package greet\n\n// Bye says goodbye.\nfunc Bye() string { return \"goodbye\" }\n"

// sealed proposes the goodbye unit and seals it through a clean debate.
func sealed(t *testing.T, f *Factory, fake *fakeRunner) string {
	t.Helper()
	change := propose(t, f)
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s", out)
	}
	return change
}

// mechanic plays a mechanic that writes the proof, then the code, then the
// docs.
func mechanic(t *testing.T, fake *fakeRunner) {
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye.go", byeCode)
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "README.md", "Run the tool with --bye to say goodbye.\n")
		return done("done")
	})
}

//shed:proves S.impl.1 S.impl.2 S.impl.3
func TestImplementWalksTheFormula(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := sealed(t, f, fake)
	var seen []string
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		seen = append(seen, turn.Step)
		if !turn.Writable || !strings.Contains(turn.Bundle, "- proofs (this session)") || !strings.Contains(turn.Bundle, "- implement (to do), needs proofs") {
			t.Errorf("proofs turn: writable %v, bundle:\n%s", turn.Writable, turn.Bundle)
		}
		write(t, turn.Dir, "bye_test.go", byeProof)
		out, err := call(t, turn, "prove", map[string]any{"clauses": []string{"S.core.2"}})
		must(t, err)
		if !strings.Contains(out, "fail S.core.2") {
			t.Errorf("the proof passed before the code existed:\n%s", out)
		}
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		seen = append(seen, turn.Step)
		if _, err := os.Stat(filepath.Join(turn.Dir, "bye_test.go")); err != nil {
			t.Error("the proofs step's work is not in the implement step's directory")
		}
		write(t, turn.Dir, "bye.go", byeCode)
		out, err := call(t, turn, "run_tests", map[string]any{})
		must(t, err)
		if !strings.Contains(out, "2 of 2 tests passed") {
			t.Errorf("tests after implementing:\n%s", out)
		}
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		seen = append(seen, turn.Step)
		return done("done")
	})
	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Implemented || !slices.Equal(seen, []string{"proofs", "implement", "docs"}) {
		t.Fatalf("implement = %s after steps %v", out, seen)
	}
	u, _ := f.Tracker.Unit(change)
	if u.State != unit.Verifying || !slices.Equal(u.Steps, []string{"proofs", "implement", "docs"}) {
		t.Errorf("unit = %+v", u)
	}
	commit, _ := f.Repo.Commit(ctx, change)
	if got := r.Git("show", commit+":bye.go"); !strings.Contains(got, "func Bye()") {
		t.Errorf("the code was not captured onto the change: %q", got)
	}

	// Another formula, from the operator settings, runs its own steps.
	r2 := project(t)
	fake2 := newFake(t)
	f2 := open(t, r2, fake2, "[formulas.default]\nsteps = [{ name = \"all\" }]\n")
	change2 := sealed(t, f2, fake2)
	fake2.on(unit.Mechanic, "all", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		write(t, turn.Dir, "bye.go", byeCode)
		return done("done")
	})
	if out, err := f2.Implement(ctx, change2); err != nil || out != Implemented || len(fake2.ran(unit.Mechanic)) != 1 {
		t.Errorf("one-step formula = %s, %v after %d sessions", out, err, len(fake2.ran(unit.Mechanic)))
	}
}

//shed:proves S.impl.4
func TestMechanicReopens(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		res := done("reopen")
		res.Note = "S.core.2 does not say where goodbye is printed"
		return res
	})
	out, err := f.Implement(ctx, change)
	must(t, err)
	u, _ := f.Tracker.Unit(change)
	if out != Reopened || u.State != unit.Proposed || u.Bounces != 1 || len(u.Steps) != 0 ||
		!strings.Contains(u.Reason, "does not say where goodbye is printed") {
		t.Errorf("reopen = %s, %+v", out, u)
	}

	// A step whose sessions keep failing reopens the unit too.
	change = sealed(t, f, fake)
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result { return session.Result{} })
	for i := 0; i < maxStepFailures; i++ {
		if out, err := f.Implement(ctx, change); err != nil || out != Failed {
			t.Fatalf("attempt %d = %s, %v", i+1, out, err)
		}
	}
	out, err = f.Implement(ctx, change)
	must(t, err)
	if u, _ := f.Tracker.Unit(change); out != Reopened || !strings.Contains(u.Reason, "the implement step failed 3 times") {
		t.Errorf("after repeated failures = %s, %+v", out, u)
	}
}

//shed:proves S.impl.5
func TestMechanicRequestsAnAmendment(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	change := sealed(t, f, fake)
	mechanic(t, fake)
	note := "S.core.2 should read: Running the tool with --bye prints goodbye on standard output.\n" +
		"Why: the clause does not say where goodbye is printed, so no proof can check it."
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		if !slices.Contains(turn.Outcomes, "amend") || turn.Check == nil {
			t.Fatalf("the mechanic cannot request an amendment: outcomes %v", turn.Outcomes)
		}
		for _, refused := range []string{
			"The spec is wrong.",
			"S.core.9 should say where goodbye is printed.",
			"C2 should allow printing goodbye anywhere.",
		} {
			if err := turn.Check("amend", refused); err == nil {
				t.Errorf("done accepted an amend citing no clause of the sealed spec: %q", refused)
			}
		}
		for _, status := range []string{"done", "reopen"} {
			if err := turn.Check(status, "The spec is wrong."); err != nil {
				t.Errorf("done refused %s: %v", status, err)
			}
		}
		if err := turn.Check("amend", note); err != nil {
			t.Errorf("done refused an amend citing S.core.2: %v", err)
		}
		res := done("amend")
		res.Note = note
		return res
	})
	out, err := f.Implement(ctx, change)
	must(t, err)
	u, _ := f.Tracker.Unit(change)
	if out != Reopened || u.State != unit.Proposed || u.Bounces != 1 || u.Amendments != 1 || len(u.Steps) != 0 {
		t.Errorf("amend = %s, %+v", out, u)
	}
	var steps []string
	for _, turn := range fake.ran(unit.Mechanic) {
		steps = append(steps, turn.Step)
	}
	if !slices.Equal(steps, []string{"proofs", "implement"}) {
		t.Errorf("mechanic steps = %v", steps)
	}
	commit, _ := f.Repo.Commit(ctx, change)
	if got := r.Git("show", commit+":bye_test.go"); !strings.Contains(got, "func TestBye") {
		t.Errorf("the captured proofs left the change: %q", got)
	}

	// The next bundle says an amendment was requested and carries the note.
	var bundles []string
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		bundles = append(bundles, turn.Bundle)
		return done("clean")
	})
	_, err = f.Debate(ctx, change)
	must(t, err)
	if len(bundles) == 0 {
		t.Fatal("no debate followed the amendment request")
	}
	if b := bundles[0]; !strings.Contains(b, "requested an amendment") || !strings.Contains(b, note) {
		t.Errorf("the next bundle lacks the amendment request:\n%s", b)
	}

	// A reopen counts a bounce but no amendment.
	change = sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		res := done("reopen")
		res.Note = "S.core.2 does not say where goodbye is printed"
		return res
	})
	if out, err := f.Implement(ctx, change); err != nil || out != Reopened {
		t.Fatalf("reopen = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(change); u.Bounces != 1 || u.Amendments != 0 || strings.Contains(u.Reason, "requested an amendment") {
		t.Errorf("reopen = %+v", u)
	}
}

// amendmentSection returns the part of a bundle that states the unit was
// resealed after an amendment, up to the next section, or "" if it has none.
func amendmentSection(bundle string) string {
	i := strings.Index(bundle, "resealed after an amendment")
	if i < 0 {
		return ""
	}
	rest := bundle[i:]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

//shed:proves S.shed.13
func TestResealTellsTheMechanicTheAmendment(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// The unit adds the goodbye clause and a wave clause.
	const bye = "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"
	const wave = "- **S.core.4** (H.greet.2) Running the tool with --wave waves.\n"
	change, err := f.Repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Say goodbye", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", testrepo.Spec+bye+wave)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("first debate = %s, %v", out, err)
	}

	// implement runs the unit's implementation, requesting an amendment at
	// the proofs step when amend is set, and returns every mechanic bundle.
	implement := func(amend bool) []string {
		t.Helper()
		var bundles []string
		step := func(turn session.Turn) session.Result {
			bundles = append(bundles, turn.Bundle)
			if amend {
				res := done("amend")
				res.Note = "S.core.2 should say where goodbye is printed.\nWhy: no proof can check it."
				return res
			}
			return done("done")
		}
		for _, s := range []string{"proofs", "implement", "docs"} {
			fake.on(unit.Mechanic, s, step)
		}
		want := Implemented
		if amend {
			want = Reopened
		}
		if out, err := f.Implement(ctx, change); err != nil || out != want {
			t.Fatalf("implement = %s, %v, want %s", out, err, want)
		}
		if len(bundles) == 0 {
			t.Fatal("no mechanic session ran")
		}
		return bundles
	}
	reseal := func() {
		t.Helper()
		if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
			u, _ := f.Tracker.Unit(change)
			t.Fatalf("amendment debate = %s, %v: %s", out, err, u.Reason)
		}
	}

	// After an ordinary seal the bundle says nothing of an amendment.
	for _, b := range implement(true) {
		if amendmentSection(b) != "" {
			t.Errorf("a bundle after an ordinary seal speaks of an amendment:\n%s", b)
		}
	}

	// Main changes the hello clause, which the unit only depends on, the
	// unit is rebased onto it as after any landing, and the amendment
	// carries main's text, changes the goodbye clause and drops the wave
	// clause.
	other, err := f.Repo.NewUnit(ctx, "Hello")
	must(t, err)
	odir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	hello := strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)
	write(t, odir, "spec/core.md", hello)
	_, err = f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Hello", nil })
	must(t, err)
	must(t, f.sweep(ctx, other))
	const byeAmended = "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye on standard output.\n"
	write(t, dir, "spec/core.md", hello+byeAmended)
	reseal()

	bundles := implement(true)
	if len(bundles) != 1 {
		t.Errorf("%d mechanic sessions before the amendment request, want 1", len(bundles))
	}
	for _, b := range bundles {
		sec := amendmentSection(b)
		if sec == "" {
			t.Fatalf("the bundle after a reseal does not say the unit was resealed after an amendment:\n%s", b)
		}
		for _, want := range []string{
			"S.core.2", "Running the tool with --bye prints goodbye.", "prints goodbye on standard output.",
			"S.core.4", "removed", "Running the tool with --wave waves.",
		} {
			if !strings.Contains(sec, want) {
				t.Errorf("the amendment diff lacks %q:\n%s", want, sec)
			}
		}
		for _, unwanted := range []string{"S.core.1", "prints hello", "changed no clause"} {
			if strings.Contains(sec, unwanted) {
				t.Errorf("the amendment diff has %q, which the amendment did not change:\n%s", unwanted, sec)
			}
		}
	}

	// An amendment that changes no clause says so, in every mechanic session.
	reseal()
	bundles = implement(false)
	if len(bundles) != 3 {
		t.Errorf("%d mechanic sessions, want 3", len(bundles))
	}
	for _, b := range bundles {
		sec := amendmentSection(b)
		if sec == "" || !strings.Contains(sec, "changed no clause") {
			t.Errorf("the bundle after an empty amendment does not say it changed no clause:\n%s", b)
		}
		for _, id := range []string{"S.core.1", "S.core.2", "S.core.4"} {
			if strings.Contains(sec, id) {
				t.Errorf("the empty amendment diff lists %s:\n%s", id, sec)
			}
		}
	}

	// A seal outside the amendment lane carries no such statement.
	must(t, f.Tracker.Reopen(change, unit.Wheelbuilder, "the proof is weak", false))
	reseal()
	for _, b := range implement(false) {
		if amendmentSection(b) != "" {
			t.Errorf("a bundle after an ordinary reseal speaks of an amendment:\n%s", b)
		}
	}
}

//shed:proves S.shed.14
func TestRejectedAmendmentKeepsTheSealedSpec(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")
	change := sealed(t, f, fake)
	u, err := f.Tracker.Unit(change)
	must(t, err)
	seal := u.Seal.Commit

	// The mechanic writes the proof, then requests an amendment.
	mechanic(t, fake)
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result {
		res := done("amend")
		res.Note = "S.core.2 should say where goodbye is printed.\nWhy: no proof can check it."
		return res
	})
	if out, err := f.Implement(ctx, change); err != nil || out != Reopened {
		t.Fatalf("amend = %s, %v", out, err)
	}

	// Another unit seals meanwhile and fills the cap on units in flight.
	other := propose(t, f)
	if out, err := f.Debate(ctx, other); err != nil || out != Sealed {
		t.Fatalf("the other unit's debate = %s, %v", out, err)
	}

	// The amendment changes the goodbye clause, adds a spec file, adds an
	// eventual clause to the horizon and changes a file outside spec/.
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	amended := strings.Replace(goodbyeSpec, "prints goodbye.", "prints goodbye on standard output.", 1)
	write(t, dir, "spec/core.md", amended)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	write(t, dir, "horizon.md", strings.Replace(testHorizon, "within C2.\n", "within C2.\n- **H.greet.4** (eventual) The tool waves.\n", 1))
	write(t, dir, "NOTES.md", "Goodbye goes to standard output.\n")

	const text = "Standard output is not the tool's concern."
	var mu sync.Mutex
	objection := ""
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) != 1 {
			return done("clean")
		}
		out, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": text})
		must(t, err)
		mu.Lock()
		objection = strings.Fields(out)[1]
		mu.Unlock()
		return done("objecting")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// While the cap holds sealing back, nothing is restored and the unit
	// waits in proposed, in the amendment lane.
	committee := len(fake.ran(unit.Committee))
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Waiting {
		t.Fatalf("a rejected amendment under a full cap = %s", out)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 3 {
		t.Errorf("%d committee sessions, want 3 members for 1 round", n)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("while waiting = %+v", u)
	}
	if lane, _ := f.lane(change); lane == nil {
		t.Error("the waiting unit left the amendment lane")
	}
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	if got := r.Git("show", commit+":spec/core.md"); !strings.Contains(got, "standard output") {
		t.Errorf("the amended spec was restored while sealing was held back: %q", got)
	}
	if !strings.Contains(r.Git("ls-tree", "-r", "--name-only", commit, "spec/"), "spec/wave.md") {
		t.Error("the added spec file was removed while sealing was held back")
	}
	if got := r.Git("show", commit+":horizon.md"); !strings.Contains(got, "H.greet.4") {
		t.Errorf("the amended horizon was restored while sealing was held back: %q", got)
	}

	// A unit lands a spec clause of its own meanwhile, and the next shed
	// process rebases the unit onto it.
	hola, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	holaDir, err := f.Repo.Workspace(ctx, hola)
	must(t, err)
	write(t, holaDir, "spec/hola.md", "# Hola\n\n- **S.hola.1** (H.greet.3) Running the tool with --hola prints hola.\n")
	if _, err := f.Repo.Land(ctx, hola, func(context.Context, string, string) (string, error) { return "Hola", nil }); err != nil {
		t.Fatal(err)
	}
	must(t, f.Close())
	f, err = Open(ctx, r.Dir, f.State, fake)
	must(t, err)
	t.Cleanup(func() { f.Close() })

	// Once the cap frees, the next debate runs no further round and rejects
	// the amendment: the spec is restored and the unit sealed.
	must(t, f.Tracker.Reopen(other, unit.Wheelbuilder, "the proof is weak", false))
	committee = len(fake.ran(unit.Committee))
	painter := len(fake.ran(unit.Painter))
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("a rejected amendment = %s: %+v", out, u)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the debate ran %d committee sessions after the cap was reached", n)
	}
	if n := len(fake.ran(unit.Painter)) - painter; n != 0 {
		t.Errorf("the painter was asked to reply %d times", n)
	}
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	commit, err = f.Repo.Commit(ctx, change)
	must(t, err)
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Sealed || u.Seal == nil || u.Seal.Main != main || u.Seal.Change != change || u.Seal.Commit != commit {
		t.Errorf("unit = %+v, want a seal at main %s and unit commit %s", u, main, commit)
	}
	if u.Bounces != 1 || u.Amendments != 1 {
		t.Errorf("the rejection counted a bounce or amendment: %+v", u)
	}
	if !strings.Contains(u.Reason, objection) {
		t.Errorf("the reason does not give the standing objection %s: %q", objection, u.Reason)
	}
	if lane, _ := f.lane(change); lane != nil {
		t.Error("the sealed unit is still in the amendment lane")
	}
	if entries, _ := archive.Read(r.Dir); len(entries) != 0 {
		t.Errorf("rejecting the amendment archived %+v", entries)
	}
	// spec/ is the sealed spec rebased onto main: the clause main gained
	// after the seal stays, and the spec file the amendment added goes.
	wantSpec := strings.Split(r.Git("ls-tree", "-r", seal, "spec/")+"\n"+r.Git("ls-tree", "-r", main, "spec/hola.md"), "\n")
	slices.SortFunc(wantSpec, func(a, b string) int {
		return strings.Compare(a[strings.Index(a, "\t"):], b[strings.Index(b, "\t"):])
	})
	if got, want := r.Git("ls-tree", "-r", commit, "spec/"), strings.Join(wantSpec, "\n"); got != want {
		t.Errorf("spec/ on the change:\n%s\nwant it as sealed on main:\n%s", got, want)
	}
	if parent := r.Git("rev-parse", commit+"^"); parent != main {
		t.Errorf("the change sits on %s, want main %s", parent, main)
	}
	if got := r.Git("show", commit+":bye_test.go"); !strings.Contains(got, "func TestBye") {
		t.Errorf("the captured proof left the change: %q", got)
	}
	if got := r.Git("show", commit+":NOTES.md"); got != "Goodbye goes to standard output." {
		t.Errorf("a file outside spec/ changed: %q", got)
	}
	// The horizon is the sealed horizon, and its seal took no tier.
	if got, want := r.Git("show", commit+":horizon.md"), r.Git("show", seal+":horizon.md"); got != want {
		t.Errorf("horizon.md on the change:\n%s\nwant it as sealed:\n%s", got, want)
	}

	// Every mechanic session of the next implementation is told the
	// amendment was rejected and the objections that stood.
	var bundles []string
	for _, s := range []string{"proofs", "implement", "docs"} {
		fake.on(unit.Mechanic, s, func(turn session.Turn) session.Result {
			bundles = append(bundles, turn.Bundle)
			return done("done")
		})
	}
	if out, err := f.Implement(ctx, change); err != nil || out != Implemented {
		t.Fatalf("implement = %s, %v", out, err)
	}
	if len(bundles) != 3 {
		t.Errorf("%d mechanic sessions, want 3", len(bundles))
	}
	for _, b := range bundles {
		for _, want := range []string{"amendment was rejected", "stands as written", text} {
			if !strings.Contains(b, want) {
				t.Errorf("the bundle lacks %q:\n%s", want, b)
			}
		}
		if amendmentSection(b) != "" {
			t.Errorf("the bundle after a rejected amendment says the unit was resealed after one:\n%s", b)
		}
	}

	// A charter objection standing at the amendment cap still rejects the
	// proposal.
	r2 := project(t)
	fake2 := newFake(t)
	f2 := open(t, r2, fake2, "[shed]\namendment_rounds = 1\nbounce_threshold = 10\n")
	vetoed := sealed(t, f2, fake2)
	must(t, f2.Tracker.Reopen(vetoed, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	fake2.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": text})
			must(t, err)
		}
		if member(turn) == 2 {
			_, err := call(t, turn, "object", map[string]any{"kind": "charter", "citations": []string{"C2"}, "text": "Goodbye is shouted."})
			must(t, err)
		}
		return done("objecting")
	})
	if out, err := f2.Debate(ctx, vetoed); err != nil || out != Rejected {
		t.Fatalf("a charter objection at the amendment cap = %s, %v", out, err)
	}
	if u, _ := f2.Tracker.Unit(vetoed); u.State != unit.Archived || u.Shelf != unit.Rejected {
		t.Errorf("unit = %+v", u)
	}
}

//shed:proves S.shed.14 S.vcs.10
func TestRejectedAmendmentBouncesOnASpecConflict(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")
	change := sealed(t, f, fake)
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints goodbye on standard output.", 1))

	// Main gains a clause where the sealed spec added its own.
	main := landOther(t, f, "Wave", map[string]string{
		"spec/core.md": testrepo.Spec + "- **S.core.3** (H.greet.3) Running the tool with --wave waves.\n"})

	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Standard output is not the tool's concern."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// The rejected amendment's restored spec conflicts with main, so the
	// unit is not sealed: it keeps the restored files and bounces, naming
	// the conflict, and stays in the amendment lane.
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Bounced {
		t.Fatalf("a rejected amendment whose restored spec conflicts = %s", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 2 {
		t.Errorf("unit = %+v, want proposed with a second bounce", u)
	}
	for _, want := range []string{"spec/core.md", "S.core.2"} {
		if !strings.Contains(u.Reason, want) {
			t.Errorf("the bounce does not name %s: %q", want, u.Reason)
		}
	}
	if lane, _ := f.lane(change); lane == nil {
		t.Error("the bounced unit left the amendment lane")
	}
	if got := parent(t, f, r, change); got != main {
		t.Errorf("the change sits on %s, want main %s", got, main)
	}
	got, err := os.ReadFile(filepath.Join(dir, "spec", "core.md"))
	must(t, err)
	for _, want := range []string{"<<<<<<<", "Running the tool with --bye prints goodbye.", "Running the tool with --wave waves."} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the restored spec/core.md lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "standard output") {
		t.Errorf("the rejected amendment's text stayed in spec/core.md:\n%s", got)
	}
}

//shed:proves S.shed.14
func TestRejectedAmendmentBouncesOnAHorizonConflict(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// The unit seals a change to a soon clause.
	change := propose(t, f)
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "horizon.md", strings.Replace(testHorizon, "says goodbye.", "says goodbye politely.", 1))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("debate = %s, %v", out, err)
	}
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	write(t, dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints goodbye on standard output.", 1))

	// Main changes the same clause another way.
	main := landOther(t, f, "Shout", map[string]string{
		"horizon.md": strings.Replace(testHorizon, "says goodbye.", "says goodbye loudly.", 1)})

	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Standard output is not the tool's concern."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// The restored horizon conflicts with main, so the unit is not sealed:
	// it keeps the restored files and bounces, naming the conflict, and
	// stays in the amendment lane.
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Bounced {
		t.Fatalf("a rejected amendment whose restored horizon conflicts = %s", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 2 || !strings.Contains(u.Reason, "horizon.md") {
		t.Errorf("unit = %+v, want proposed with a second bounce naming horizon.md", u)
	}
	if lane, _ := f.lane(change); lane == nil {
		t.Error("the bounced unit left the amendment lane")
	}
	if got := parent(t, f, r, change); got != main {
		t.Errorf("the change sits on %s, want main %s", got, main)
	}
	got, err := os.ReadFile(filepath.Join(dir, "horizon.md"))
	must(t, err)
	for _, want := range []string{"<<<<<<<", "says goodbye politely.", "says goodbye loudly."} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the restored horizon.md lacks %q:\n%s", want, got)
		}
	}
	spec, err := os.ReadFile(filepath.Join(dir, "spec", "core.md"))
	must(t, err)
	if strings.Contains(string(spec), "standard output") {
		t.Errorf("the rejected amendment's text stayed in spec/core.md:\n%s", spec)
	}
}

//shed:proves S.verify.1 S.verify.4
func TestVerifyChecksProofsFirst(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye.go", strings.Replace(byeCode, `"goodbye"`, `"bye"`, 1))
		return done("done")
	})
	must2(t, f.Implement)(change)

	out, err := f.Verify(ctx, change)
	must(t, err)
	u, _ := f.Tracker.Unit(change)
	if out != Failed || u.State != unit.Implementing || len(u.Steps) != 0 {
		t.Fatalf("verify = %s, %+v", out, u)
	}
	if len(fake.ran(unit.Committee)) != 3 {
		t.Error("the reviewer ran although the proofs fail")
	}
	notices, _ := f.Tracker.Notices(unit.Mechanic, true)
	if len(notices) != 1 || !strings.Contains(notices[0].Body, "S.core.2 fails: TestBye fail") {
		t.Errorf("notices = %+v", notices)
	}

	// The mechanic's next session carries the notice.
	mechanic(t, fake)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		if !strings.Contains(turn.Bundle, "S.core.2 fails") {
			t.Errorf("the mechanic's bundle lacks the verification notice:\n%s", turn.Bundle)
		}
		return done("done")
	})
	must2(t, f.Implement)(change)
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	if out, err := f.Verify(ctx, change); err != nil || out != Verified {
		t.Fatalf("second verify = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Queued {
		t.Errorf("unit = %s", u.State)
	}
}

func must2(t *testing.T, stage func(ctxType, string) (Outcome, error)) func(string) {
	return func(change string) {
		t.Helper()
		out, err := stage(ctx, change)
		must(t, err)
		if out == Failed || out == Reopened {
			t.Fatalf("stage = %s", out)
		}
	}
}

//shed:proves S.verify.2 S.verify.3 S.verify.4
func TestReviewerDecides(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	mechanic(t, fake)

	change := sealed(t, f, fake)
	must2(t, f.Implement)(change)
	fake.on(unit.Committee, "review", func(turn session.Turn) session.Result {
		if turn.Writable || !strings.Contains(turn.SystemPrompt, "charter clause") || !strings.Contains(turn.Bundle, "## Charter") {
			t.Errorf("review turn: writable %v", turn.Writable)
		}
		if _, err := call(t, turn, "finding", map[string]any{"citations": []string{"S.core.9"}, "text": "x"}); err == nil {
			t.Error("a finding cited a clause that does not exist")
		}
		_, err := call(t, turn, "finding", map[string]any{"citations": []string{"S.core.2", "C2"}, "text": "Bye shouts."})
		must(t, err)
		return done("fail")
	})
	out, err := f.Verify(ctx, change)
	must(t, err)
	notices, _ := f.Tracker.Notices(unit.Mechanic, true)
	if out != Failed || len(notices) != 1 || !strings.Contains(notices[0].Body, "- S.core.2, C2: Bye shouts.") {
		t.Errorf("reviewer fail = %s, %+v", out, notices)
	}

	must2(t, f.Implement)(change)
	fake.on(unit.Committee, "review", func(turn session.Turn) session.Result {
		_, err := call(t, turn, "finding", map[string]any{"citations": []string{"S.core.2"}, "text": "Goodbye contradicts C2."})
		must(t, err)
		return done("spec-wrong")
	})
	out, err = f.Verify(ctx, change)
	must(t, err)
	if u, _ := f.Tracker.Unit(change); out != Reopened || u.State != unit.Proposed || !strings.Contains(u.Reason, "Goodbye contradicts C2.") {
		t.Errorf("reviewer spec-wrong = %s, %+v", out, u)
	}
}

//shed:proves S.verify.1
func TestVerifyGuardsTheHorizon(t *testing.T) {
	// The sealed horizon rewords the goodbye clause, which the unit
	// advances, and adds a near clause.
	sealedHorizon := strings.Replace(strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon) The tool says goodbye and waves.", 1),
		"within C2.\n", "within C2.\n- **H.greet.4** (near) The tool bows.\n", 1)
	// Main rewords the distant clause after the seal.
	mainHorizon := strings.Replace(testHorizon, "greets in any language", "greets in every language", 1)
	for _, tc := range []struct {
		name, sealed, main, horizon, want string
	}{
		{"marks an advanced clause", "", "", strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1), ""},
		{"marks another clause", "", "", strings.Replace(testHorizon, "(distant) The tool greets", "(distant, realised) The tool greets", 1), "marks H.greet.3 realised but does not advance it"},
		{"rewords a clause", "", "", strings.Replace(testHorizon, "says goodbye.", "waves goodbye.", 1), "H.greet.2"},
		{"keeps the sealed horizon", sealedHorizon, "", sealedHorizon, ""},
		{"marks a sealed clause it advances", sealedHorizon, "", strings.Replace(sealedHorizon, "(soon) The tool says goodbye and waves.", "(soon, realised) The tool says goodbye and waves.", 1), ""},
		{"rewords a sealed clause", sealedHorizon, "", strings.Replace(sealedHorizon, "The tool bows.", "The tool bows low.", 1), "H.greet.4"},
		{"adds a clause after the seal", sealedHorizon, "", strings.Replace(sealedHorizon, "The tool bows.\n", "The tool bows.\n- **H.greet.5** (near) The tool nods.\n", 1), "H.greet.5"},
		{"removes a clause after the seal", sealedHorizon, "", strings.Replace(sealedHorizon, "- **H.greet.3** (distant) The tool greets in any language, within C2.\n", "", 1), "H.greet.3"},
		{"keeps main's change", "", mainHorizon, mainHorizon, ""},
		{"reverts main's change", "", mainHorizon, testHorizon, "H.greet.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := project(t)
			fake := newFake(t)
			f := open(t, r, fake, "")
			change := proposeHorizon(t, f, tc.sealed)
			fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
			if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
				t.Fatalf("debate = %s, %v", out, err)
			}
			if tc.main != "" {
				landOther(t, f, "Every language", map[string]string{"horizon.md": tc.main})
				must(t, f.Close())
				var err error
				f, err = Open(ctx, r.Dir, f.State, fake)
				must(t, err)
				t.Cleanup(func() { f.Close() })
			}
			mechanic(t, fake)
			fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
				write(t, turn.Dir, "horizon.md", tc.horizon)
				return done("done")
			})
			must2(t, f.Implement)(change)
			fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
			out, err := f.Verify(ctx, change)
			must(t, err)
			notices, _ := f.Tracker.Notices(unit.Mechanic, true)
			if tc.want == "" {
				if out != Verified {
					t.Errorf("verify = %s, %+v", out, notices)
				}
				return
			}
			if out != Failed || len(notices) != 1 || !strings.Contains(notices[0].Body, tc.want) {
				t.Errorf("verify = %s, %+v", out, notices)
			}
		})
	}
}

// sealedAmending seals a unit that adds the goodbye clause and rewords
// H.greet.2 in the horizon.
func sealedAmending(t *testing.T, f *Factory, fake *fakeRunner) string {
	t.Helper()
	change := propose(t, f)
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "horizon.md", strings.Replace(testHorizon, "says goodbye.", "waves goodbye.", 1))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s", out)
	}
	return change
}

//shed:proves S.verify.1
func TestVerifyPassesTheSealedHorizon(t *testing.T) {
	sealedText := strings.Replace(testHorizon, "says goodbye.", "waves goodbye.", 1)
	for _, tc := range []struct {
		name, horizon, want string
	}{
		{"keeps the sealed amendment", sealedText, ""},
		{"marks the sealed clause realised", strings.Replace(sealedText, "(soon) The tool waves goodbye.", "(soon, realised) The tool waves goodbye.", 1), ""},
		{"rewords the sealed clause again", strings.Replace(sealedText, "waves goodbye.", "bows goodbye.", 1), "changes H.greet.2 in the horizon"},
		{"rewords a clause the seal left alone", strings.Replace(sealedText, "greets in any language", "greets in every language", 1), "changes H.greet.3 in the horizon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := project(t)
			fake := newFake(t)
			f := open(t, r, fake, "")
			change := sealedAmending(t, f, fake)
			mechanic(t, fake)
			fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
				write(t, turn.Dir, "horizon.md", tc.horizon)
				return done("done")
			})
			must2(t, f.Implement)(change)
			fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
			out, err := f.Verify(ctx, change)
			must(t, err)
			notices, _ := f.Tracker.Notices(unit.Mechanic, true)
			if tc.want == "" {
				if out != Verified {
					t.Errorf("verify = %s, %+v", out, notices)
				}
				return
			}
			if out != Failed || len(notices) != 1 || !strings.Contains(notices[0].Body, tc.want) {
				t.Errorf("verify = %s, %+v", out, notices)
			}
		})
	}
}

//shed:proves S.queue.1 S.queue.2 S.serve.7
func TestLandResolvesConflicts(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })

	// Two units change greet.go the same way apart from one line.
	first := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	must2(t, f.Implement)(first)
	must2(t, f.Verify)(first)

	second, err := f.Repo.NewUnit(ctx, "Wave")
	must(t, err)
	must(t, f.Tracker.OpenUnit(second, "Wave", unit.Painter))
	dir, _ := f.Repo.Workspace(ctx, second)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave prints goodbye.\n")
	must(t, f.Declare(ctx, second, "", nil, []string{"H.greet.3"}, unit.Painter))
	if out, err := f.Debate(ctx, second); err != nil || out != Waiting {
		t.Fatalf("with one unit in flight the second debate = %s, %v", out, err)
	}
	if out, err := f.Land(ctx, first); err != nil || out != Landed {
		t.Fatalf("land first = %s, %v", out, err)
	}
	if out, err := f.Debate(ctx, second); err != nil || out != Sealed {
		t.Fatalf("second debate = %s, %v", out, err)
	}
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "wave_test.go", strings.ReplaceAll(strings.ReplaceAll(byeProof, "S.core.2", "S.wave.1"), "Bye", "Wave"))
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "wave.go", "package greet\n\n// Wave says goodbye.\nfunc Wave() string { return \"goodbye\" }\n")
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	must2(t, f.Implement)(second)
	must2(t, f.Verify)(second)

	// Main moves under the queued unit, changing the line it changed.
	third, err := f.Repo.NewUnit(ctx, "Warmly")
	must(t, err)
	tdir, err := f.Repo.Workspace(ctx, third)
	must(t, err)
	write(t, tdir, "greet.go", "package greet\n\n// Hello greets most warmly.\nfunc Hello() string { return \"hello\" }\n")
	_, err = f.Repo.Land(ctx, third, func(context.Context, string, string) (string, error) { return "Warmly", nil })
	must(t, err)
	fake.on(unit.Wheelbuilder, "resolve", func(turn session.Turn) session.Result {
		got, _ := os.ReadFile(filepath.Join(turn.Dir, "greet.go"))
		if !strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("the wheelbuilder's greet.go has no conflict markers:\n%s", got)
		}
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets warmly and kindly.\nfunc Hello() string { return \"hello\" }\n")
		return done("resolved")
	})
	out, err := f.Land(ctx, second)
	must(t, err)
	if out != Landed {
		t.Fatalf("land second = %s", out)
	}
	main, _ := f.Repo.MainCommit(ctx)
	if got := r.Git("show", main+":greet.go"); !strings.Contains(got, "warmly and kindly") {
		t.Errorf("main's greet.go = %q", got)
	}
}

//shed:proves S.queue.2
func TestUnresolvableConflictsReopen(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "README.md", "Say goodbye with --bye, as unit "+turn.Unit+" says.\n")
		return done("done")
	})
	a := sealed(t, f, fake)
	b := sealed(t, f, fake)
	for _, c := range []string{a, b} {
		must2(t, f.Implement)(c)
		must2(t, f.Verify)(c)
	}
	if out, err := f.Land(ctx, a); err != nil || out != Landed {
		t.Fatalf("land a = %s, %v", out, err)
	}
	fake.on(unit.Wheelbuilder, "resolve", func(session.Turn) session.Result {
		res := done("unresolvable")
		res.Note = "both add S.core.2"
		return res
	})
	out, err := f.Land(ctx, b)
	must(t, err)
	if u, _ := f.Tracker.Unit(b); out != Reopened || u.State != unit.Proposed || !strings.Contains(u.Reason, "both add S.core.2") {
		t.Errorf("unresolvable = %s, %+v", out, u)
	}
}

//shed:proves S.sched.1
func TestRunTakesAUnitToMain(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	mechanic(t, fake)
	change := propose(t, f)
	out, err := f.Run(ctx, change)
	must(t, err)
	if out != Landed {
		t.Fatalf("run = %s", out)
	}
	u, _ := f.Tracker.Unit(change)
	if u.State != unit.Landed || r.GitRemote("rev-parse", "main") != u.Landed {
		t.Errorf("unit = %+v", u)
	}
	msg := r.Git("log", "-1", "--format=%B", u.Landed)
	if !strings.Contains(msg, "added   S.core.2") || !strings.Contains(msg, "Unit: "+change) {
		t.Errorf("landed commit:\n%s", msg)
	}
}
