package factory

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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

	// Main changes the hello clause, which the unit only depends on, and the
	// amendment carries main's text, changes the goodbye clause and drops
	// the wave clause.
	other, err := f.Repo.NewUnit(ctx, "Hello")
	must(t, err)
	odir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	hello := strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)
	write(t, odir, "spec/core.md", hello)
	_, err = f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Hello", nil })
	must(t, err)
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
	for _, tc := range []struct {
		name, horizon, want string
	}{
		{"marks an advanced clause", strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1), ""},
		{"marks another clause", strings.Replace(testHorizon, "(distant) The tool greets", "(distant, realised) The tool greets", 1), "marks H.greet.3 realised but does not advance it"},
		{"rewords a clause", strings.Replace(testHorizon, "says goodbye.", "waves goodbye.", 1), "changes H.greet.2 in the horizon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := project(t)
			fake := newFake(t)
			f := open(t, r, fake, "")
			change := sealed(t, f, fake)
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
