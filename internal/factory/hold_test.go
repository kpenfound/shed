package factory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

// waveCore is main's spec/core.md once a landing adds S.core.3 where the
// goodbye clause goes, and resolvedCore holds both clauses.
var (
	waveLine     = "- **S.core.3** (H.greet.3) Running the tool with --wave waves.\n"
	waveCore     = testrepo.Spec + waveLine
	resolvedCore = goodbyeSpec + waveLine
)

// landConflict lands files on main and rebases the units in flight onto it
// as a landing does, checking that each of the given units now holds a
// conflict in the named file.
func landConflict(t *testing.T, f *Factory, files map[string]string, file string, changes ...string) {
	t.Helper()
	landOther(t, f, "Wave", files)
	must(t, f.sweep(ctx, ""))
	for _, change := range changes {
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		if got, _ := os.ReadFile(filepath.Join(dir, file)); !strings.Contains(string(got), "<<<<<<<") {
			t.Fatalf("unit %s's %s holds no conflict after the landing:\n%s", unit.Short(change), file, got)
		}
	}
}

// roundOne checks that the committee sessions after the first n ran one
// round, the first, with every member.
func roundOne(t *testing.T, fake *fakeRunner, n int) {
	t.Helper()
	turns := fake.ran(unit.Committee)[n:]
	if len(turns) != 3 {
		t.Errorf("%d committee sessions, want 3 members for 1 round", len(turns))
	}
	for _, turn := range turns {
		if !strings.HasPrefix(turn.Step, "debate round 1,") {
			t.Errorf("the debate did not start afresh from round one: %q", turn.Step)
		}
	}
}

//shed:proves S.shed.16
func TestPainterSessionEndsAHeldSeal(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// One unit seals and fills the cap, so two near-tier amendments reach
	// consensus and are held back.
	other := propose(t, f)
	if out, err := f.Debate(ctx, other); err != nil || out != Sealed {
		t.Fatalf("the other unit's debate = %s, %v", out, err)
	}
	raised := proposeHorizon(t, f, nearHorizon)
	kept := proposeHorizon(t, f, nearHorizon)
	for _, change := range []string{raised, kept} {
		if out, err := f.Debate(ctx, change); err != nil || out != Waiting {
			t.Fatalf("a near-tier amendment under a full cap = %s, %v", out, err)
		}
	}

	// A landing leaves a conflict under spec/ in both. The painter resolves
	// each; on one it also writes eventual clauses into the horizon.
	landConflict(t, f, map[string]string{"spec/core.md": waveCore}, "spec/core.md", raised, kept)
	resolved := map[string]int{}
	fake.on(unit.Painter, "resolve", func(turn session.Turn) session.Result {
		resolved[turn.Unit]++
		write(t, turn.Dir, "spec/core.md", resolvedCore)
		if turn.Unit == raised {
			write(t, turn.Dir, "horizon.md", eventualHorizon)
		}
		return done("replied")
	})

	// The painter session ends the hold: the debate starts afresh from
	// round one and takes the tier again, which sends the eventual clauses
	// to the owner, counting no bounce.
	committee := len(fake.ran(unit.Committee))
	out, err := f.Debate(ctx, raised)
	must(t, err)
	if out != Contested {
		u, _ := f.Tracker.Unit(raised)
		t.Fatalf("a held unit whose painter wrote eventual clauses = %s: %+v", out, u)
	}
	if resolved[raised] != 1 {
		t.Errorf("the painter resolved the conflict %d times", resolved[raised])
	}
	roundOne(t, fake, committee)
	u, err := f.Tracker.Unit(raised)
	must(t, err)
	if u.State != unit.Contested || u.Bounces != 0 || u.Seal != nil {
		t.Errorf("unit = %+v, want contested with no bounce and no seal", u)
	}
	if ev := lastMoveOf(t, f, raised); ev.To != unit.Contested || ev.Actor != unit.Shed || ev.Bounce || !strings.Contains(ev.Reason, "eventual") {
		t.Errorf("the move = %+v, want shed contesting the eventual tier", ev)
	}

	// A painter session that leaves the tier near ends the hold too: the
	// unit stays proposed with no bounce, its debate runs round one again,
	// and the full cap holds it back once more.
	committee = len(fake.ran(unit.Committee))
	if out, err := f.Debate(ctx, kept); err != nil || out != Waiting {
		t.Fatalf("a held unit after its painter's session = %s, %v", out, err)
	}
	roundOne(t, fake, committee)
	if u, _ := f.Tracker.Unit(kept); u.State != unit.Proposed || u.Bounces != 0 {
		t.Errorf("unit = %+v, want proposed with no bounce", u)
	}

	// With no painter session since that round, the released seal runs no
	// round.
	must(t, f.Tracker.Reopen(other, unit.Wheelbuilder, "the proof is weak", false))
	committee = len(fake.ran(unit.Committee))
	if out, err := f.Debate(ctx, kept); err != nil || out != Sealed {
		t.Fatalf("the released seal = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the released seal ran %d committee sessions", n)
	}
	if resolved[kept] != 1 {
		t.Errorf("the painter resolved the conflict %d times", resolved[kept])
	}
}

//shed:proves S.shed.17
func TestPainterSessionEndsAnApproval(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// The owner approves two eventual-tier amendments.
	kept := proposeHorizon(t, f, eventualHorizon)
	lowered := proposeHorizon(t, f, eventualHorizon)
	for _, change := range []string{kept, lowered} {
		if out, err := f.Debate(ctx, change); err != nil || out != Contested {
			t.Fatalf("an eventual-tier amendment = %s, %v", out, err)
		}
		must(t, f.Tracker.Approve(change, "Singing and waving are where the tool is going."))
	}

	// A landing leaves a conflict under spec/ in both before they are
	// sealed. The painter resolves each; on one it also lowers the horizon
	// amendment to the near tier.
	landConflict(t, f, map[string]string{"spec/core.md": waveCore}, "spec/core.md", kept, lowered)
	resolved := map[string]int{}
	fake.on(unit.Painter, "resolve", func(turn session.Turn) session.Result {
		resolved[turn.Unit]++
		write(t, turn.Dir, "spec/core.md", resolvedCore)
		if turn.Unit == lowered {
			write(t, turn.Dir, "horizon.md", nearHorizon)
		}
		return done("replied")
	})

	// The painter session ends the approval and counts no bounce: the
	// debate runs from round one and takes the tier of the horizon as the
	// painter left it, which goes back to the owner.
	committee := len(fake.ran(unit.Committee))
	out, err := f.Debate(ctx, kept)
	must(t, err)
	if out != Contested {
		u, _ := f.Tracker.Unit(kept)
		t.Fatalf("an approved unit after its painter's session = %s: %+v", out, u)
	}
	if resolved[kept] != 1 {
		t.Errorf("the painter resolved the conflict %d times", resolved[kept])
	}
	roundOne(t, fake, committee)
	u, err := f.Tracker.Unit(kept)
	must(t, err)
	if u.State != unit.Contested || u.Bounces != 0 || u.Seal != nil {
		t.Errorf("unit = %+v, want contested with no bounce and no seal", u)
	}
	if ev := lastMoveOf(t, f, kept); ev.To != unit.Contested || ev.Actor != unit.Shed || ev.Bounce || !strings.Contains(ev.Reason, "eventual") {
		t.Errorf("the move = %+v, want shed contesting the eventual tier", ev)
	}

	// A near tier as the painter left it seals after its round.
	committee = len(fake.ran(unit.Committee))
	out, err = f.Debate(ctx, lowered)
	must(t, err)
	if out != Sealed {
		u, _ := f.Tracker.Unit(lowered)
		t.Fatalf("an approved unit lowered to the near tier = %s: %+v", out, u)
	}
	roundOne(t, fake, committee)
	u, err = f.Tracker.Unit(lowered)
	must(t, err)
	if u.Bounces != 0 || u.Seal == nil {
		t.Fatalf("unit = %+v, want sealed with no bounce", u)
	}
	got := r.Git("show", u.Seal.Commit+":horizon.md")
	if !strings.Contains(got, "The tool bows.") || strings.Contains(got, "H.greet.5") {
		t.Errorf("the sealed horizon is not the painter's near-tier one:\n%s", got)
	}
}

//shed:proves S.shed.16 S.shed.14
func TestPainterSessionKeepsARejectedAmendmentWaiting(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")
	change := sealed(t, f, fake)
	u, err := f.Tracker.Unit(change)
	must(t, err)
	seal := u.Seal.Commit
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))

	// Another unit seals meanwhile and fills the cap on units in flight.
	other := propose(t, f)
	if out, err := f.Debate(ctx, other); err != nil || out != Sealed {
		t.Fatalf("the other unit's debate = %s, %v", out, err)
	}

	// The amendment changes the goodbye clause and adds a spec file, and an
	// objection stands at the cap, so the rejection waits for the cap.
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints goodbye on standard output.", 1))
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if turn.Unit == change && member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Standard output is not the tool's concern."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	if out, err := f.Debate(ctx, change); err != nil || out != Waiting {
		t.Fatalf("a rejected amendment under a full cap = %s, %v", out, err)
	}

	// A landing adds its own spec/wave.md, and the painter resolves the
	// conflict, writing eventual clauses into the horizon as it does.
	landConflict(t, f, map[string]string{"spec/wave.md": "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves hello.\n"}, "spec/wave.md", change)
	resolved := 0
	fake.on(unit.Painter, "resolve", func(turn session.Turn) session.Result {
		resolved++
		write(t, turn.Dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves hello.\n")
		write(t, turn.Dir, "horizon.md", eventualHorizon)
		return done("replied")
	})

	// The painter session does not end the wait: once the cap frees, the
	// debate runs no round, rejects the amendment and seals the unit with
	// the horizon of its last seal, taking no tier.
	must(t, f.Tracker.Reopen(other, unit.Wheelbuilder, "the proof is weak", false))
	committee := len(fake.ran(unit.Committee))
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("a rejected amendment after its painter's session = %s: %+v", out, u)
	}
	if resolved != 1 {
		t.Errorf("the painter resolved the conflict %d times", resolved)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the debate ran %d committee sessions after the cap was reached", n)
	}
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Sealed || u.Bounces != 1 {
		t.Errorf("unit = %+v, want sealed with only the amendment's bounce", u)
	}
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	if got, want := r.Git("show", commit+":horizon.md"), r.Git("show", seal+":horizon.md"); got != want {
		t.Errorf("horizon.md on the change:\n%s\nwant it as sealed:\n%s", got, want)
	}
}
