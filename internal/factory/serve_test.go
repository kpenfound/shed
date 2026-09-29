package factory

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// painter plays a painter that proposes the goodbye clause.
func painter(t *testing.T, fake *fakeRunner) {
	fake.on(unit.Painter, "propose", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "spec/core.md", goodbyeSpec)
		_, err := call(t, turn, "declare", map[string]any{"title": "Say goodbye", "summary": "Add --bye.",
			"depends": []string{"S.core.1"}, "advances": []string{"H.greet.2"}})
		must(t, err)
		return done("proposed")
	})
}

// everyone plays every role so that a proposal goes all the way to main.
func everyone(t *testing.T, fake *fakeRunner) {
	painter(t, fake)
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "horizon.md", strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1))
		return done("done")
	})
}

//shed:proves S.serve.1 S.serve.3 S.paint.1 S.paint.2 S.hz.1
func TestServeTakesTheGapToMain(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[vcs]\nremote = \"origin\"\n[painter]\ninterval = \"0s\"\n")
	everyone(t, fake)
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))

	units, _ := f.Tracker.Units()
	if len(units) != 1 || units[0].State != unit.Landed || units[0].Title != "Say goodbye" {
		t.Fatalf("units = %+v\nlog:\n%s", units, log.String())
	}
	main, _ := f.Repo.MainCommit(ctx)
	if main != units[0].Landed || r.GitRemote("rev-parse", "main") != main {
		t.Errorf("main %s, landed %s", main, units[0].Landed)
	}
	if !strings.Contains(r.Git("show", main+":horizon.md"), "(soon, realised) The tool says goodbye.") {
		t.Error("the landed horizon does not mark the clause realised")
	}
	painted := fake.ran(unit.Painter)
	if len(painted) != 1 {
		t.Fatalf("the painter ran %d times; with the gap realised it should stop", len(painted))
	}
	for _, want := range []string{"## Gap", "- H.greet.2 (soon) The tool says goodbye."} {
		if !strings.Contains(painted[0].Bundle, want) {
			t.Errorf("painter bundle lacks %q:\n%s", want, painted[0].Bundle)
		}
	}
	if !strings.Contains(painted[0].SystemPrompt, "declare") {
		t.Error("the painter's prompt does not ask it to declare")
	}
	mechanicPrompt := fake.ran(unit.Mechanic)[0].SystemPrompt
	if !strings.Contains(mechanicPrompt, "(soon, realised)") {
		t.Error("the mechanic's prompt does not say how to mark a clause realised")
	}
	for _, want := range []string{"painter: ", " debate: sealed", " implement: implemented", " verify: verified", " land: landed"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
}

//shed:proves S.paint.1
func TestPainterWithNothingLeavesNothing(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	fake.on(unit.Painter, "propose", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "scratch.txt", "thinking")
		return done("nothing")
	})
	change, out, err := f.Propose(ctx)
	must(t, err)
	u, _ := f.Tracker.Unit(change)
	if out != Discarded || u.State != unit.Archived || u.Shelf != unit.Deferred {
		t.Errorf("propose = %s, %+v", out, u)
	}
	if _, err := f.Repo.Workspace(ctx, change); err == nil {
		t.Error("the empty proposal's change was kept")
	}
	if entries, _ := archive.Read(r.Dir); len(entries) != 0 {
		t.Errorf("an empty proposal was archived: %+v", entries)
	}
	if due, _ := f.PainterDue(ctx, time.Now()); due {
		t.Error("the painter is due again within its interval")
	}
	if due, _ := f.PainterDue(ctx, time.Now().Add(2*time.Hour)); !due {
		t.Error("the painter is not due after its interval")
	}
}

//shed:proves S.paint.3
func TestPainterGap(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[painter]\nmax_proposed = 3\ninterval = \"0s\"\n")
	gap, err := f.Gap(ctx)
	must(t, err)
	var ids []string
	for _, e := range gap {
		ids = append(ids, e.Clause.ID.String())
	}
	// H.greet.1 is realised and H.greet.3 is distant.
	if !slices.Equal(ids, []string{"H.greet.2"}) {
		t.Errorf("gap = %v", ids)
	}
	fake.on(unit.Painter, "propose", func(turn session.Turn) session.Result {
		if _, err := call(t, turn, "declare", map[string]any{"title": "Greet in French", "advances": []string{"H.greet.3"}}); err == nil {
			t.Error("declared a distant clause")
		}
		write(t, turn.Dir, "spec/core.md", goodbyeSpec)
		_, err := call(t, turn, "declare", map[string]any{"title": "Say goodbye", "advances": []string{"H.greet.2"}})
		must(t, err)
		return done("proposed")
	})
	_, out, err := f.Propose(ctx)
	must(t, err)
	if out != Outcome("proposed") {
		t.Fatalf("propose = %s", out)
	}
	if gap, _ := f.Gap(ctx); len(gap) != 0 {
		t.Errorf("the gap still offers a clause a unit in flight advances: %v", gap)
	}
	sections, err := f.painterSections(nil)
	must(t, err)
	var flight string
	for _, sec := range sections {
		if sec.Title == "Units in flight" {
			flight = sec.Body
		}
	}
	if !strings.Contains(flight, `"Say goodbye" (proposed), modifies S.core.2, advances H.greet.2`) {
		t.Errorf("units in flight = %q", flight)
	}
	if due, _ := f.PainterDue(ctx, time.Now()); due {
		t.Error("the painter is due with nothing in its gap")
	}
}

//shed:proves S.horizon.10
func TestPainterGapShowsRefinement(t *testing.T) {
	r := projectWith(t, map[string]string{"horizon.md": `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon, refines H.greet.3) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (near, refines H.greet.5) The tool bows.
- **H.greet.5** (eventual) The tool sings.
- **H.greet.6** (soon) The tool waves.
`})
	fake := newFake(t)
	f := open(t, r, fake, "")
	gap, err := f.Gap(ctx)
	must(t, err)
	sections, err := f.painterSections(gap)
	must(t, err)
	var body string
	for _, sec := range sections {
		if sec.Title == "Gap" {
			body = sec.Body
		}
	}
	lines := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if fields := strings.Fields(line); len(fields) > 1 {
			lines[fields[1]] = strings.ToLower(line)
		}
	}
	for id, want := range map[string][]string{
		"H.greet.2": {"(soon)", "refines h.greet.3 (distant)"},
		"H.greet.4": {"(near)", "refines h.greet.5 (eventual)"},
		"H.greet.6": {"(soon)"},
	} {
		line, ok := lines[id]
		if !ok {
			t.Errorf("the gap lacks %s:\n%s", id, body)
			continue
		}
		for _, w := range want {
			if !strings.Contains(line, w) {
				t.Errorf("the gap line for %s lacks %q: %q", id, w, line)
			}
		}
	}
	if strings.Contains(lines["H.greet.6"], "refine") {
		t.Errorf("the gap line for a clause with no refines tag names a parent: %q", lines["H.greet.6"])
	}
}

//shed:proves S.serve.4 S.serve.5
func TestServeFinishesBeforeStarting(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nunits = 1\nin_flight = 0\n")
	everyone(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "README.md", turn.Unit)
		return done("done")
	})
	var order []string
	fake.on(unit.Committee, "review", func(turn session.Turn) session.Result {
		order = append(order, "review "+turn.Unit)
		return done("pass")
	})
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		order = append(order, "implement "+turn.Unit)
		write(t, turn.Dir, "bye_test.go", byeProof)
		return done("done")
	})
	fake.on(unit.Wheelbuilder, "resolve", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "README.md", turn.Unit)
		return done("resolved")
	})
	first := sealed(t, f, fake)
	must2(t, f.Implement)(first)
	second := sealed(t, f, fake)
	draft, err := f.Repo.NewUnit(ctx, "Draft")
	must(t, err)
	must(t, f.Tracker.OpenUnit(draft, "Draft", unit.Owner))
	order = nil

	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	want := []string{"review " + first, "implement " + second, "review " + second}
	if !slices.Equal(order, want) {
		t.Errorf("stages ran in the order %v, want %v", order, want)
	}
	if u, _ := f.Tracker.Unit(first); u.State != unit.Landed {
		t.Errorf("first unit = %s", u.State)
	}
	if u, _ := f.Tracker.Unit(draft); u.State != unit.Proposed || u.Round != 0 {
		t.Errorf("a draft was debated: %+v", u)
	}
	if n := len(fake.ran(unit.Painter)); n != 0 {
		t.Errorf("the painter proposed %d times while proposals waited", n)
	}
}

//shed:proves S.serve.6
func TestServePausesOnTheDailyBudget(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\nper_day_usd = 1\n")
	everyone(t, fake)
	change := propose(t, f)
	s, err := f.Tracker.StartSession(change, unit.Painter, "earlier", 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Failed, "infrastructure: rate limited", 1.5, false))

	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if len(fake.turns) != 0 || !strings.Contains(log.String(), "paused: the daily budget is spent: $1.50 of $1.00") {
		t.Errorf("serve ran %d sessions; log:\n%s", len(fake.turns), log.String())
	}
	if why, _ := f.Paused(time.Now().Add(25 * time.Hour)); why != "" {
		t.Errorf("still paused a day later: %s", why)
	}
	if streak, _ := f.Tracker.InfraStreak(); streak != 1 {
		t.Errorf("infrastructure streak = %d", streak)
	}
}

//shed:proves S.serve.8
func TestServeResumesAfterACrash(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	everyone(t, fake)
	change := sealed(t, f, fake)
	must(t, f.Tracker.Move(change, unit.Implementing, unit.Shed, "dispatched"))
	proofs, err := f.Tracker.StartSession(change, unit.Mechanic, "proofs", 1)
	must(t, err)
	dir, _ := f.Repo.Workspace(ctx, change)
	write(t, dir, "bye_test.go", byeProof)
	must(t, f.Tracker.FinishSession(proofs.ID, tracker.Succeeded, "done", 0, true))
	// The process running the implement step died with it.
	_, err = f.Tracker.StartSession(change, unit.Mechanic, "implement", 999999)
	must(t, err)
	// A painter's draft was left half made.
	draft, err := f.Repo.NewUnit(ctx, "proposal")
	must(t, err)
	must(t, f.Tracker.OpenUnit(draft, painterDraft, unit.Painter))
	f.Close()

	fake2 := newFake(t)
	everyone(t, fake2)
	f2, err := Open(ctx, f.Root, f.State, fake2)
	must(t, err)
	defer f2.Close()
	sessions, _ := f2.Tracker.Sessions(change)
	if sessions[len(sessions)-1].Status != tracker.Interrupted {
		t.Errorf("the dead session is %s", sessions[len(sessions)-1].Status)
	}
	must(t, f2.Serve(ctx, ServeOptions{Once: true}))
	var steps []string
	for _, turn := range fake2.ran(unit.Mechanic) {
		steps = append(steps, turn.Step)
	}
	if !slices.Equal(steps, []string{"implement", "docs"}) {
		t.Errorf("after the crash the mechanic ran %v", steps)
	}
	if u, _ := f2.Tracker.Unit(change); u.State != unit.Landed {
		t.Errorf("unit = %s", u.State)
	}
	if u, _ := f2.Tracker.Unit(draft); u.State != unit.Archived {
		t.Errorf("the painter's draft = %s", u.State)
	}
}

//shed:proves S.serve.2
func TestServeActsOnStateWhateverChangedIt(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[serve]\ntick = \"50ms\"\n")
	everyone(t, fake)
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- f.Serve(ctx, ServeOptions{}) }()

	// The owner opens and declares a unit while shed is serving; nothing
	// wakes the controllers but the tick.
	time.Sleep(100 * time.Millisecond)
	change := propose(t, f)
	for {
		u, _ := f.Tracker.Unit(change)
		if u.State == unit.Landed {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the unit never landed: %+v", u)
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	if err := <-served; err != nil && err != context.Canceled {
		t.Errorf("serve = %v", err)
	}
}

//shed:proves S.paint.1 S.serve.1
func TestServeSaysWhyNothingStarted(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	painter(t, fake)

	// A painter session that failed before doing anything is not a
	// proposal, and does not hold the painter back.
	draft, err := f.Repo.NewUnit(ctx, "proposal")
	must(t, err)
	must(t, f.Tracker.OpenUnit(draft, painterDraft, unit.Painter))
	s, err := f.Tracker.StartSession(draft, unit.Painter, proposeStep, 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Failed, "the session's grants: refused", 0, false))
	must(t, f.Tracker.Archive(draft, unit.Deferred, unit.Shed, "the painter stopped"))
	if why, err := f.PainterWait(ctx, time.Now()); err != nil || why != "" {
		t.Errorf("after a failed start the painter waits: %q, %v", why, err)
	}

	// One that worked and went nowhere holds it back for painter.interval,
	// and serve says so.
	draft, err = f.Repo.NewUnit(ctx, "proposal")
	must(t, err)
	must(t, f.Tracker.OpenUnit(draft, painterDraft, unit.Painter))
	s, err = f.Tracker.StartSession(draft, unit.Painter, proposeStep, 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Succeeded, "proposed", 0.3, true))
	must(t, f.Tracker.Archive(draft, unit.Deferred, unit.Shed, "done with it"))
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if len(fake.ran(unit.Painter)) != 0 {
		t.Error("the painter ran within its interval")
	}
	if !strings.HasPrefix(log.String(), "nothing to start\n  painter: the last proposal went nowhere, so the next is due at ") ||
		!strings.Contains(log.String(), "(a 15m0s wait, doubling from painter.interval up to painter.max_interval)") {
		t.Errorf("serve said:\n%s", log.String())
	}

	// A draft is named, with what to do about it.
	owner, err := f.Repo.NewUnit(ctx, "draft")
	must(t, err)
	must(t, f.Tracker.OpenUnit(owner, "Owner's draft", unit.Owner))
	log.Reset()
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if !strings.Contains(log.String(), unit.Short(owner)+" is a draft: declare the horizon clauses it advances") {
		t.Errorf("serve said:\n%s", log.String())
	}
}

//shed:proves S.serve.5 S.paint.1
func TestPainterBacksOffOnlyAfterProposalsThatGoNowhere(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[painter]\ninterval = \"10m\"\nmax_interval = \"25m\"\n")
	nothing := func() {
		t.Helper()
		fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
		if _, out, err := f.Propose(ctx); err != nil || out != Discarded {
			t.Fatalf("propose = %s, %v", out, err)
		}
	}
	waits := func(after time.Duration) bool {
		t.Helper()
		why, err := f.PainterWait(ctx, time.Now().Add(after))
		must(t, err)
		return why != ""
	}
	if waits(0) {
		t.Fatal("the painter waits before it has proposed anything")
	}

	// Each proposal that goes nowhere doubles the wait, up to the cap.
	for i, wait := range []time.Duration{10 * time.Minute, 20 * time.Minute, 25 * time.Minute, 25 * time.Minute} {
		nothing()
		if !waits(wait - time.Minute) {
			t.Errorf("after %d proposals that went nowhere the painter is due before %s", i+1, wait)
		}
		if waits(wait + time.Minute) {
			t.Errorf("after %d proposals that went nowhere the painter still waits after %s", i+1, wait)
		}
	}

	// A proposal that is sealed ends the streak: no wait at all.
	painter(t, fake)
	change, out, err := f.Propose(ctx)
	must(t, err)
	if out != Outcome("proposed") {
		t.Fatalf("propose = %s", out)
	}
	if why, _ := f.PainterWait(ctx, time.Now()); !strings.Contains(why, "1 proposals are waiting") {
		t.Errorf("with a proposal in the shed the painter waits because %q", why)
	}
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("debate = %s, %v", out, err)
	}
	if why, _ := f.PainterWait(ctx, time.Now()); strings.Contains(why, "went nowhere") {
		t.Errorf("after a sealed proposal the painter still backs off: %q", why)
	}
}
