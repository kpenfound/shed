package factory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

// withClauses is the test horizon with clauses added after its last one.
func withClauses(clauses ...string) string {
	return strings.Replace(testHorizon, "\n\n## Milestones", "\n"+strings.Join(clauses, "\n")+"\n\n## Milestones", 1)
}

//shed:proves S.frame.3
func TestFrameUnitsKeepOnlyCleanRebases(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	var framing string
	fake.on(unit.FrameBuilder, "", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "horizon.md", framing)
		return done("framed")
	})
	frame := func(f *Factory, horizon string) string {
		t.Helper()
		framing = horizon
		fr, err := f.Frame(ctx, "H.greet.3")
		must(t, err)
		if !fr.Kept() {
			t.Fatalf("framing kept nothing: %+v", fr)
		}
		return fr.Change
	}
	// holds checks a frame unit sits on base, is still proposed and holds
	// horizon unconflicted in its change and its workspace.
	holds := func(f *Factory, change, base, horizon string) {
		t.Helper()
		if got := parent(t, f, r, change); got != base {
			t.Errorf("frame unit %s sits on %s, want %s", unit.Short(change), got, base)
		}
		commit, err := f.Repo.Commit(ctx, change)
		must(t, err)
		if got := r.Git("show", commit+":horizon.md"); got != strings.TrimSuffix(horizon, "\n") {
			t.Errorf("frame unit %s's change holds the horizon:\n%s", unit.Short(change), got)
		}
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		if got, _ := os.ReadFile(filepath.Join(dir, "horizon.md")); string(got) != horizon {
			t.Errorf("frame unit %s's workspace holds the horizon:\n%s", unit.Short(change), got)
		}
		if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed || u.Bounces != 0 {
			t.Errorf("frame unit %s = %+v, want proposed", unit.Short(change), u)
		}
		if n := checkpoints(t, f); n != 0 {
			t.Errorf("%d checkpoints left behind", n)
		}
	}

	french := withClauses("- **H.greet.4** (near, refines H.greet.3) The tool greets in French.")
	a := frame(f, french)

	// A landing that leaves the horizon alone rebases the frame unit onto
	// the new main.
	mechanic(t, fake)
	goodbye := sealed(t, f, fake)
	must2(t, f.Implement)(goodbye)
	must2(t, f.Verify)(goodbye)
	if out, err := f.Land(ctx, goodbye); err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}
	main1, err := f.Repo.MainCommit(ctx)
	must(t, err)
	holds(f, a, main1, french)

	// Main gains a clause where the frame unit adds its own. The next
	// landing's rebase of the frame unit would conflict, so it is undone.
	spanish := withClauses("- **H.greet.5** (soon, refines H.greet.3) The tool greets in Spanish.")
	landOther(t, f, "Spanish", map[string]string{"horizon.md": spanish})
	aBefore, err := f.Repo.Commit(ctx, a)
	must(t, err)
	wave := inFlight(t, f, fake, "wave", nil)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "wave_test.go", waveProof)
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result { return done("done") })
	fake.on(unit.Mechanic, "docs", func(session.Turn) session.Result { return done("done") })
	must2(t, f.Implement)(wave)
	must2(t, f.Verify)(wave)
	if out, err := f.Land(ctx, wave); err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}
	if now, _ := f.Repo.Commit(ctx, a); now != aBefore {
		t.Errorf("the conflicting frame unit moved from %s to %s", aBefore, now)
	}
	holds(f, a, main1, french)

	// reopen opens the repository as the next shed process would, which
	// sweeps the units behind main.
	reopen := func(f *Factory) *Factory {
		t.Helper()
		must(t, f.Close())
		next, err := Open(ctx, r.Dir, f.State, fake)
		must(t, err)
		t.Cleanup(func() { next.Close() })
		return next
	}

	// The recovery sweep keeps a clean rebase of a frame unit.
	italian := withClauses(
		"- **H.greet.5** (soon, refines H.greet.3) The tool greets in Spanish.",
		"- **H.greet.6** (near, refines H.greet.3) The tool greets in Italian.",
	)
	c := frame(f, italian)
	main4 := landOther(t, f, "Hello", map[string]string{"hello.txt": "hello\n"})
	f = reopen(f)
	holds(f, c, main4, italian)
	holds(f, a, main1, french)

	// It undoes one that would conflict.
	landOther(t, f, "Portuguese", map[string]string{"horizon.md": withClauses(
		"- **H.greet.5** (soon, refines H.greet.3) The tool greets in Spanish.",
		"- **H.greet.7** (soon, refines H.greet.3) The tool greets in Portuguese.",
	)})
	cBefore, err := f.Repo.Commit(ctx, c)
	must(t, err)
	f = reopen(f)
	if now, _ := f.Repo.Commit(ctx, c); now != cBefore {
		t.Errorf("the conflicting frame unit moved from %s to %s", cBefore, now)
	}
	holds(f, c, main4, italian)
	holds(f, a, main1, french)
}
