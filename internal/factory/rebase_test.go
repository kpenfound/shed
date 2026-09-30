package factory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kpenfound/shed/internal/landing"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// inFlight opens a unit that adds a spec file of its own and writes files
// into its workspace, then seals it through a clean debate.
func inFlight(t *testing.T, f *Factory, fake *fakeRunner, area string, files map[string]string) string {
	t.Helper()
	change, err := f.Repo.NewUnit(ctx, "Unit "+area)
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Unit "+area, unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/"+area+".md", fmt.Sprintf("# %s\n\n- **S.%s.1** (H.greet.3) Running the tool with --%s says %s.\n", area, area, area, area))
	for name, content := range files {
		write(t, dir, name, content)
	}
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.3"}, unit.Painter))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("debate of %s = %s, %v", area, out, err)
	}
	return change
}

// parent returns the parent of the commit a unit's change is at.
func parent(t *testing.T, f *Factory, r *testrepo.Repo, change string) string {
	t.Helper()
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	return r.Git("rev-parse", commit+"^")
}

func checkpoints(t *testing.T, f *Factory) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.State, vcs.CheckpointsDir))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return len(entries)
}

//shed:proves S.vcs.10 S.vcs.11 S.vcs.16
func TestLandingRebasesUnitsInFlight(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })

	// A unit with no change behind it: rebasing it fails.
	const ghost = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	must(t, f.Tracker.OpenUnit(ghost, "Ghost", unit.Painter))

	// The unit that lands says goodbye and greets warmly.
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// Units in flight: one apart from the landing, one verifying that
	// clashes with it (S.vcs.16 still undoes it, since no mechanic session
	// is left to resolve a stored conflict), a proposed and a contested one
	// that clash with it, and one whose mechanic is at work when it lands.
	idle := inFlight(t, f, fake, "wave", map[string]string{"wave.txt": "wave\n"})
	kindly := "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n"
	clash := inFlight(t, f, fake, "hola", map[string]string{"greet.go": kindly})
	must(t, f.Tracker.Move(clash, unit.Implementing, unit.Shed, "a mechanic is dispatched"))
	must(t, f.Tracker.Move(clash, unit.Verifying, unit.Mechanic, "every step of the formula finished"))
	clashBefore, err := f.Repo.Commit(ctx, clash)
	must(t, err)
	clashBase := parent(t, f, r, clash)
	proposal := func(title string) string {
		t.Helper()
		change, err := f.Repo.NewUnit(ctx, title)
		must(t, err)
		must(t, f.Tracker.OpenUnit(change, title, unit.Painter))
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		write(t, dir, "greet.go", strings.Replace(kindly, "kindly", "gently", 1))
		_, err = f.Repo.Snapshot(ctx, change)
		must(t, err)
		return change
	}
	proposed := proposal("Gently")
	contested := proposal("Softly")
	must(t, f.Tracker.Move(contested, unit.Contested, unit.Shed, "bounced too often"))
	busy := inFlight(t, f, fake, "nod", nil)

	started, release := make(chan struct{}), make(chan struct{})
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "nod.txt", "nod\n")
		close(started)
		<-release
		return done("done")
	})
	sawLanding := false
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		_, err := os.Stat(filepath.Join(turn.Dir, "bye.go"))
		sawLanding = err == nil
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(session.Turn) session.Result { return done("done") })
	implemented := make(chan error, 1)
	go func() {
		out, err := f.Implement(ctx, busy)
		if err == nil && out != Implemented {
			err = fmt.Errorf("implement = %s", out)
		}
		implemented <- err
	}()
	<-started
	busyBefore, err := f.Repo.Commit(ctx, busy)
	must(t, err)

	out, err := f.Land(ctx, lander)
	if err != nil || out != Landed {
		close(release)
		t.Fatalf("land = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(lander)
	must(t, err)
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	if u.State != unit.Landed || u.Landed != main {
		t.Errorf("landed unit = %+v, main %s", u, main)
	}
	// The failed rebase undid nothing of the landing.
	if remote := r.GitRemote("rev-parse", "main"); remote != main {
		t.Errorf("the remote's main is %s, want the landing %s", remote, main)
	}

	// The units in flight sit on the new main with their IDs and states.
	for change, want := range map[string]unit.State{idle: unit.Sealed, proposed: unit.Proposed, contested: unit.Contested} {
		if got := parent(t, f, r, change); got != main {
			t.Errorf("unit %s sits on %s, want the new main %s", change, got, main)
		}
		if u, _ := f.Tracker.Unit(change); u.State != want {
			t.Errorf("unit %s is %s after the rebase, want %s", change, u.State, want)
		}
	}
	commit, err := f.Repo.Commit(ctx, idle)
	must(t, err)
	for _, name := range []string{"wave.txt", "bye.go", "spec/wave.md"} {
		if !strings.Contains(r.Git("ls-tree", "-r", "--name-only", commit), name) {
			t.Errorf("the rebased unit lacks %s", name)
		}
	}
	dir, err := f.Repo.Workspace(ctx, idle)
	must(t, err)
	if _, err := os.Stat(filepath.Join(dir, "bye.go")); err != nil {
		t.Errorf("the rebased unit's workspace lacks the landed file: %v", err)
	}
	// A proposed or contested unit keeps the conflict in its files.
	for _, change := range []string{proposed, contested} {
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); !strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("the clashing unit %s's greet.go keeps no conflict:\n%s", change, got)
		}
	}

	// A verifying unit whose rebase conflicts is left as it was, in the
	// same state, and not reopened: no mechanic session is left to resolve
	// a conflict, so S.vcs.16's exception does not reach it.
	if now, _ := f.Repo.Commit(ctx, clash); now != clashBefore {
		t.Errorf("the clashing verifying unit moved from %s to %s", clashBefore, now)
	}
	if got := parent(t, f, r, clash); got != clashBase {
		t.Errorf("the clashing verifying unit sits on %s, want its old base %s", got, clashBase)
	}
	if u, _ := f.Tracker.Unit(clash); u.State != unit.Verifying || u.Bounces != 0 {
		t.Errorf("the clashing verifying unit = %+v, want verifying with no bounce", u)
	}
	dir, err = f.Repo.Workspace(ctx, clash)
	must(t, err)
	if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); string(got) != kindly {
		t.Errorf("the clashing verifying unit's greet.go = %q, want its own %q", got, kindly)
	}
	if _, err := os.Stat(filepath.Join(dir, "bye.go")); err == nil {
		t.Error("the clashing verifying unit's workspace took the landing")
	}
	if n := checkpoints(t, f); n != 0 {
		t.Errorf("%d checkpoints left behind", n)
	}

	// The unit whose session runs is left alone until its capture, then
	// rebased before its next session.
	if now, _ := f.Repo.Commit(ctx, busy); now != busyBefore {
		t.Errorf("the unit with a running session moved from %s to %s", busyBefore, now)
	}
	close(release)
	must(t, <-implemented)
	if !sawLanding {
		t.Error("the session after the capture did not see the landing")
	}
	if got := parent(t, f, r, busy); got != main {
		t.Errorf("the unit whose session ran sits on %s, want the new main %s", got, main)
	}
	commit, err = f.Repo.Commit(ctx, busy)
	must(t, err)
	if got := r.Git("show", commit+":nod.txt"); got != "nod" {
		t.Errorf("the captured file = %q", got)
	}
}

//shed:proves S.vcs.11
func TestInterruptedSweepFinishes(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	config := "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n"
	f := open(t, r, fake, config)
	idle := inFlight(t, f, fake, "wave", map[string]string{"wave.txt": "wave\n"})
	busy := inFlight(t, f, fake, "nod", map[string]string{"nod.txt": "nod\n"})
	running, err := f.Tracker.StartSession(busy, unit.Mechanic, "proofs", os.Getpid())
	must(t, err)
	busyBefore, err := f.Repo.Commit(ctx, busy)
	must(t, err)

	// Main moved and was pushed, and shed stopped before rebasing anything.
	other, err := f.Repo.NewUnit(ctx, "Hello")
	must(t, err)
	dir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	write(t, dir, "hello.txt", "hello\n")
	landed, err := f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Hello", nil })
	must(t, err)
	must(t, f.Close())

	// reopen opens the repository as the next shed process would.
	reopen := func() *Factory {
		t.Helper()
		next, err := Open(ctx, r.Dir, f.State, fake)
		must(t, err)
		t.Cleanup(func() { next.Close() })
		return next
	}
	f2 := reopen()
	if main, _ := f2.Repo.MainCommit(ctx); main != landed {
		t.Errorf("main is at %s after reopening, want the landing %s", main, landed)
	}
	if remote := r.GitRemote("rev-parse", "main"); remote != landed {
		t.Errorf("the remote's main is at %s, want the landing %s", remote, landed)
	}
	if got := parent(t, f2, r, idle); got != landed {
		t.Errorf("the idle unit sits on %s, want the landing %s", got, landed)
	}
	dir, err = f2.Repo.Workspace(ctx, idle)
	must(t, err)
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Errorf("the idle unit's workspace lacks the landed file: %v", err)
	}
	if u, _ := f2.Tracker.Unit(idle); u.State != unit.Sealed {
		t.Errorf("the idle unit is %s", u.State)
	}
	if now, _ := f2.Repo.Commit(ctx, busy); now != busyBefore {
		t.Errorf("the unit with a running session moved from %s to %s", busyBefore, now)
	}
	if n := checkpoints(t, f2); n != 0 {
		t.Errorf("%d checkpoints left behind", n)
	}

	// Once its session has ended, the next process rebases it too.
	must(t, f2.Tracker.FinishSession(running.ID, tracker.Succeeded, "done", 0, true))
	must(t, f2.Close())
	f3 := reopen()
	if got := parent(t, f3, r, busy); got != landed {
		t.Errorf("the unit sits on %s once its session ended, want the landing %s", got, landed)
	}
	commit, err := f3.Repo.Commit(ctx, busy)
	must(t, err)
	if got := r.Git("show", commit+":nod.txt"); got != "nod" {
		t.Errorf("the unit's own file = %q", got)
	}
}

// landOther lands a unit of other work straight through the repository,
// moving main without sweeping the units in flight, and returns main.
func landOther(t *testing.T, f *Factory, title string, files map[string]string) string {
	t.Helper()
	other, err := f.Repo.NewUnit(ctx, title)
	must(t, err)
	dir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	for name, content := range files {
		write(t, dir, name, content)
	}
	landed, err := f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return title, nil })
	must(t, err)
	return landed
}

//shed:proves S.vcs.10
func TestSealingRebasesOntoTheSealsMain(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// A proposal whose change fell behind main is sealed on main, and its
	// mechanic's directory holds main's files at the seal.
	behind := propose(t, f)
	main := landOther(t, f, "Hello", map[string]string{"hello.txt": "hello\n"})
	if got := parent(t, f, r, behind); got == main {
		t.Fatal("the proposal already sits on the new main")
	}
	if out, err := f.Debate(ctx, behind); err != nil || out != Sealed {
		t.Fatalf("debate of a proposal behind main = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(behind)
	must(t, err)
	commit, err := f.Repo.Commit(ctx, behind)
	must(t, err)
	if u.Seal == nil || u.Seal.Main != main || u.Seal.Commit != commit {
		t.Errorf("seal = %+v, want main %s and unit commit %s", u.Seal, main, commit)
	}
	if got := parent(t, f, r, behind); got != main {
		t.Errorf("the sealed unit sits on %s, want its seal's main %s", got, main)
	}
	if got := r.Git("show", commit+":spec/core.md"); got != strings.TrimSuffix(goodbyeSpec, "\n") {
		t.Errorf("the sealed spec = %q", got)
	}
	mechanic(t, fake)
	sawMain := false
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		got, err := os.ReadFile(filepath.Join(turn.Dir, "hello.txt"))
		sawMain = err == nil && string(got) == "hello\n"
		write(t, turn.Dir, "bye_test.go", byeProof)
		return done("done")
	})
	if _, err := f.Implement(ctx, behind); err != nil {
		t.Fatal(err)
	}
	if !sawMain {
		t.Error("the mechanic's directory lacks main's files at the seal")
	}

	// A conflict only outside spec/ is stored in the change, and the unit
	// is sealed.
	code, err := f.Repo.NewUnit(ctx, "Kindly")
	must(t, err)
	must(t, f.Tracker.OpenUnit(code, "Kindly", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, code)
	must(t, err)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	write(t, dir, "greet.go", "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n")
	must(t, f.Declare(ctx, code, "", nil, []string{"H.greet.3"}, unit.Painter))
	main = landOther(t, f, "Warmly", map[string]string{
		"greet.go": "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n"})
	if out, err := f.Debate(ctx, code); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(code)
		t.Fatalf("debate of a proposal that conflicts outside spec/ = %s, %v: %q", out, err, u.Reason)
	}
	if u, _ := f.Tracker.Unit(code); u.State != unit.Sealed || u.Seal == nil || u.Seal.Main != main {
		t.Errorf("unit conflicting outside spec/ = %+v, want sealed on %s", u, main)
	}
	if got := parent(t, f, r, code); got != main {
		t.Errorf("the unit conflicting outside spec/ sits on %s, want %s", got, main)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); !strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("greet.go keeps no conflict:\n%s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "spec", "wave.md")); strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("spec/wave.md holds a conflict:\n%s", got)
	}

	// A conflict under spec/ seals nothing: the unit bounces to its painter,
	// naming the file and the clause, and keeps the rebased files.
	hola, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(hola, "Hola", unit.Painter))
	dir, err = f.Repo.Workspace(ctx, hola)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	must(t, f.Declare(ctx, hola, "", nil, []string{"H.greet.1"}, unit.Painter))
	main = landOther(t, f, "Newline", map[string]string{
		"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)})
	if out, err := f.Debate(ctx, hola); err != nil || out != Bounced {
		t.Fatalf("debate of a proposal that conflicts under spec/ = %s, %v", out, err)
	}
	u, err = f.Tracker.Unit(hola)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 || u.Seal != nil || u.Round != 0 {
		t.Errorf("unit conflicting under spec/ = %+v, want proposed, one bounce and no seal", u)
	}
	for _, want := range []string{"spec/core.md", "S.core.1"} {
		if !strings.Contains(u.Reason, want) {
			t.Errorf("the bounce does not name %s: %q", want, u.Reason)
		}
	}
	if got := parent(t, f, r, hola); got != main {
		t.Errorf("the conflicted unit sits on %s, want the rebased change on %s", got, main)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "spec", "core.md")); !strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("the conflicted unit's spec/core.md keeps no conflict:\n%s", got)
	}

	// The painter's next session sees the conflict and resolves it before
	// any member debates the text.
	resolved := strings.Replace(testrepo.Spec, "prints hello.", "prints hola and a newline.", 1)
	var mu sync.Mutex
	var order []string
	fake.on(unit.Painter, "", func(turn session.Turn) session.Result {
		got, _ := os.ReadFile(filepath.Join(turn.Dir, "spec", "core.md"))
		mu.Lock()
		order = append(order, "painter")
		mu.Unlock()
		if !strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("the painter's session does not see the conflict:\n%s", got)
		}
		write(t, turn.Dir, "spec/core.md", resolved)
		return done("replied")
	})
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		got, _ := os.ReadFile(filepath.Join(turn.Dir, "spec", "core.md"))
		mu.Lock()
		order = append(order, "committee")
		mu.Unlock()
		if string(got) != resolved {
			t.Errorf("a member debates %q, want the resolved text", got)
		}
		return done("clean")
	})
	if out, err := f.Debate(ctx, hola); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(hola)
		t.Fatalf("debate after the painter resolved the conflict = %s, %v: %q", out, err, u.Reason)
	}
	if len(order) < 2 || order[0] != "painter" || !slices.Contains(order, "committee") {
		t.Errorf("sessions ran in the order %v, want the painter before any member", order)
	}
	commit, err = f.Repo.Commit(ctx, hola)
	must(t, err)
	if got := r.Git("show", commit+":spec/core.md"); got != strings.TrimSuffix(resolved, "\n") {
		t.Errorf("the sealed spec = %q", got)
	}
}

//shed:proves S.vcs.10 S.vcs.17
func TestFailedSealingRebaseBounces(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	jjPath, err := exec.LookPath("jj")
	must(t, err)
	bin := t.TempDir()
	refuse := filepath.Join(bin, "refuse")
	wrapper := filepath.Join(bin, "jj")
	must(t, os.WriteFile(wrapper, []byte(fmt.Sprintf(`#!/bin/sh
if [ -e %q ]; then
	for a in "$@"; do
		if [ "$a" = rebase ]; then
			echo "jj refused to rebase" >&2
			exit 1
		fi
	done
fi
exec %q "$@"
`, refuse, jjPath)), 0o755))
	f := open(t, r, fake, fmt.Sprintf("[shed]\nbounce_threshold = 10\n[vcs]\njj = %q\n", wrapper))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	change := propose(t, f)
	base := parent(t, f, r, change)
	landOther(t, f, "Hello", map[string]string{"hello.txt": "hello\n"})
	before, err := f.Repo.Commit(ctx, change)
	must(t, err)
	must(t, os.WriteFile(refuse, nil, 0o644))

	if out, err := f.Debate(ctx, change); err != nil || out != Bounced {
		t.Fatalf("debate whose sealing rebase fails = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 || u.Seal != nil || u.Round != 0 {
		t.Errorf("unit whose sealing rebase failed = %+v, want proposed, one bounce and no seal", u)
	}
	if !strings.Contains(u.Reason, "jj refused to rebase") {
		t.Errorf("the bounce does not name the failure: %q", u.Reason)
	}
	if after, _ := f.Repo.Commit(ctx, change); after != before {
		t.Errorf("the failed rebase moved the change from %s to %s", before, after)
	}
	if got := parent(t, f, r, change); got != base {
		t.Errorf("the change sits on %s, want its old base %s", got, base)
	}
	if n := checkpoints(t, f); n != 0 {
		t.Errorf("%d checkpoints left behind", n)
	}
}

// unfence drops the lines that close a file's conflicted regions, so the
// rest of the markers stay in the file as plain content.
func unfence(t *testing.T, path string) string {
	t.Helper()
	got, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("%s holds no conflict:\n%s", path, got)
	}
	var kept []string
	for _, line := range strings.SplitAfter(string(got), "\n") {
		if !strings.HasPrefix(line, ">>>>>>>") {
			kept = append(kept, line)
		}
	}
	must(t, os.WriteFile(path, []byte(strings.Join(kept, "")), 0o644))
	return string(got)
}

const waveProof = "package greet\n\nimport \"testing\"\n\n//shed:proves S.wave.1\nfunc TestWave(t *testing.T) {}\n"

//shed:proves S.vcs.12 S.vcs.10
func TestUnresolvedConflictsFailVerification(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	landOther(t, f, "Docs", map[string]string{"docs/greet.md": "Hello greets.\n"})

	// Sealing rebases the unit onto a main whose docs clash with its own,
	// and stores the conflict outside spec/.
	change, err := f.Repo.NewUnit(ctx, "Wave")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Wave", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	write(t, dir, "docs/greet.md", "Hello greets kindly.\n")
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.3"}, unit.Painter))
	landOther(t, f, "Warmly", map[string]string{"docs/greet.md": "Hello greets warmly.\n"})
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("debate = %s, %v: %q", out, err, u.Reason)
	}

	// Every mechanic session is told of the conflict. The first leaves its
	// markers in the file, broken so they are no longer a conflict jj
	// stores but plain content.
	var mu sync.Mutex
	var told []string
	var conflicted string
	tell := func(turn session.Turn) {
		mu.Lock()
		defer mu.Unlock()
		for _, want := range []string{"docs/greet.md", "resolve", "sealed spec"} {
			if !strings.Contains(turn.Bundle, want) {
				t.Errorf("the mechanic's %s bundle does not name %q:\n%s", turn.Step, want, turn.Bundle)
			}
		}
		told = append(told, turn.Step)
	}
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		tell(turn)
		write(t, turn.Dir, "wave_test.go", waveProof)
		conflicted = unfence(t, filepath.Join(turn.Dir, "docs", "greet.md"))
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result { tell(turn); return done("done") })
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result { tell(turn); return done("done") })
	reviewed := false
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { reviewed = true; return done("pass") })
	must2(t, f.Implement)(change)
	if len(told) != 3 {
		t.Errorf("mechanic sessions told of the conflict: %v", told)
	}

	// Verification fails on the markers left in the file and returns the
	// unit to implementing, naming it.
	out, err := f.Verify(ctx, change)
	must(t, err)
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if out != Failed || u.State != unit.Implementing {
		t.Fatalf("verify of a unit holding markers = %s, %+v", out, u)
	}
	if reviewed {
		t.Error("the reviewer ran although the unit holds an unresolved conflict")
	}
	notices, _ := f.Tracker.Notices(unit.Mechanic, true)
	if len(notices) != 1 || !strings.Contains(notices[0].Body, "docs/greet.md") {
		t.Errorf("notices = %+v", notices)
	}

	// Once the mechanic resolves the file the unit verifies, though another
	// file quotes the same marker lines.
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		tell(turn)
		write(t, turn.Dir, "docs/greet.md", "Hello greets kindly and warmly.\n")
		write(t, turn.Dir, "docs/markers.md", conflicted)
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result { return done("done") })
	fake.on(unit.Mechanic, "docs", func(session.Turn) session.Result { return done("done") })
	must2(t, f.Implement)(change)
	if out, err := f.Verify(ctx, change); err != nil || out != Verified {
		notices, _ := f.Tracker.Notices(unit.Mechanic, true)
		t.Fatalf("verify once resolved = %s, %v: %+v", out, err, notices)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Queued {
		t.Errorf("unit = %s, want queued", u.State)
	}
}

//shed:proves S.vcs.12 S.vcs.10
func TestPainterMarkersBlockTheSeal(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	hola, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(hola, "Hola", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, hola)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	must(t, f.Declare(ctx, hola, "", nil, []string{"H.greet.1"}, unit.Painter))
	landOther(t, f, "Newline", map[string]string{
		"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)})
	if out, err := f.Debate(ctx, hola); err != nil || out != Bounced {
		t.Fatalf("debate of a proposal that conflicts under spec/ = %s, %v", out, err)
	}

	// The painter leaves the markers in spec/core.md as plain content: the
	// seal is blocked as a stored conflict would block it.
	fake.on(unit.Painter, "", func(turn session.Turn) session.Result {
		unfence(t, filepath.Join(turn.Dir, "spec", "core.md"))
		return done("replied")
	})
	if out, err := f.Debate(ctx, hola); err != nil || out != Bounced {
		t.Fatalf("debate with markers left under spec/ = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(hola)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 2 || u.Seal != nil {
		t.Errorf("unit with markers left under spec/ = %+v, want proposed, two bounces and no seal", u)
	}
	if !strings.Contains(u.Reason, "spec/core.md") {
		t.Errorf("the bounce does not name spec/core.md: %q", u.Reason)
	}

	// Once the painter resolves the file the unit is sealed.
	resolved := strings.Replace(testrepo.Spec, "prints hello.", "prints hola and a newline.", 1)
	fake.on(unit.Painter, "", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "spec/core.md", resolved)
		return done("replied")
	})
	if out, err := f.Debate(ctx, hola); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(hola)
		t.Fatalf("debate once the painter resolved the markers = %s, %v: %q", out, err, u.Reason)
	}
}

// bounceNoBounceLine returns the description of a unit's events that says a
// bounce counted no bounce, or "" when none does.
func bounceNoBounceLine(t *testing.T, f *Factory, change string) string {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	for _, e := range events {
		if line := tracker.Describe(e); strings.Contains(line, "no bounce") {
			return line
		}
	}
	return ""
}

//shed:proves S.vcs.17
func TestSealingBounceCountsOnlyIfThePainterSawTheConflict(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\ncommittee = 1\n[shed]\nmax_rounds = 2\nbounce_threshold = 10\n")

	// A proposal whose painter's latest captured session found spec/ clean:
	// answering an unrelated round-1 objection, the painter's session sees
	// no conflict, because the landing that will conflict has not yet been
	// rebased onto the change. The conflict surfaces only when sealing
	// rebases the change, so it came in after the painter's latest capture
	// and the bounce counts no bounce.
	change, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Hola", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.1"}, unit.Painter))

	var main string
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		switch {
		case strings.HasPrefix(turn.Step, "debate round 1"):
			_, err := call(t, turn, "object", map[string]any{"kind": tracker.SizeObjection,
				"citations": []string{"S.core.1", "H.greet.1"}, "text": "reconsider the wording"})
			must(t, err)
			// The landing happens once the round is under way. The change
			// is not rebased onto it until sealing, so the painter's reply
			// session below still sees spec/core.md clean.
			main = landOther(t, f, "Newline", map[string]string{
				"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)})
			return done("objecting")
		case strings.HasPrefix(turn.Step, "debate round 2"):
			objs, err := f.Tracker.Standing(change)
			must(t, err)
			must(t, f.Tracker.Withdraw(objs[0].ID, 1, "reconsidered"))
			return done("clean")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
		got, err := os.ReadFile(filepath.Join(turn.Dir, "spec", "core.md"))
		must(t, err)
		if strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("the painter's reply session already sees a conflict:\n%s", got)
		}
		return done("replied")
	})

	if out, err := f.Debate(ctx, change); err != nil || out != Bounced {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("debate whose sealing rebase meets a landing-brought conflict = %s, %v: %q", out, err, u.Reason)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 0 || u.Seal != nil || u.Round != 0 {
		t.Errorf("an uncounted bounce = %+v, want proposed at round 0, no bounce counted and no seal", u)
	}
	for _, want := range []string{"spec/core.md", "S.core.1"} {
		if !strings.Contains(u.Reason, want) {
			t.Errorf("the bounce does not name %s: %q", want, u.Reason)
		}
	}
	if line := bounceNoBounceLine(t, f, change); line == "" {
		t.Error("the log does not state that the bounce counted no bounce")
	}
	if got := parent(t, f, r, change); got != main {
		t.Errorf("the conflicted unit sits on %s, want the rebased change on %s", got, main)
	}

	// A fresh proposal with no painter session at all behaves as it did
	// before S.vcs.17: nothing establishes that its conflict is
	// landing-brought, so its sealing bounce counts normally.
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	baseline, err := f.Repo.NewUnit(ctx, "Baseline")
	must(t, err)
	must(t, f.Tracker.OpenUnit(baseline, "Baseline", unit.Painter))
	bdir, err := f.Repo.Workspace(ctx, baseline)
	must(t, err)
	write(t, bdir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola nicely.", 1))
	must(t, f.Declare(ctx, baseline, "", nil, []string{"H.greet.1"}, unit.Painter))
	landOther(t, f, "Warmer", map[string]string{
		"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello warmly.", 1)})
	if out, err := f.Debate(ctx, baseline); err != nil || out != Bounced {
		t.Fatalf("debate of a fresh proposal with no painter session = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(baseline); u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("a bounce with no prior painter capture = %+v, want one counted bounce", u)
	}
}

//shed:proves S.vcs.17 S.unit.6
func TestUncountedBouncesPastTheThresholdContest(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\ncommittee = 1\n[shed]\nmax_rounds = 2\nbounce_threshold = 1\n")

	change, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Hola", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.1"}, unit.Painter))

	// The first bounce: the painter's latest captured session, answering an
	// unrelated objection, found spec/ clean. A landing conflicts with the
	// change only afterwards, discovered when sealing rebases: it counts no
	// bounce.
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		switch {
		case strings.HasPrefix(turn.Step, "debate round 1"):
			_, err := call(t, turn, "object", map[string]any{"kind": tracker.SizeObjection,
				"citations": []string{"S.core.1", "H.greet.1"}, "text": "reconsider the wording"})
			must(t, err)
			landOther(t, f, "Newline", map[string]string{
				"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1)})
			return done("objecting")
		case strings.HasPrefix(turn.Step, "debate round 2"):
			objs, err := f.Tracker.Standing(change)
			must(t, err)
			must(t, f.Tracker.Withdraw(objs[0].ID, 1, "reconsidered"))
			return done("clean")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	if out, err := f.Debate(ctx, change); err != nil || out != Bounced {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("first debate = %s, %v: %q", out, err, u.Reason)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed || u.Bounces != 0 {
		t.Fatalf("after the first uncounted bounce = %+v", u)
	}

	// The second bounce: the painter's own resolving session fixes the
	// stored conflict cleanly, and a fresh landing conflicts with the fix
	// only afterwards, again discovered only when sealing rebases: it also
	// counts no bounce. But two uncounted bounces since the unit opened
	// exceed the threshold of one, so the unit is contested for the owner.
	resolved := strings.Replace(testrepo.Spec, "prints hello.", "prints hola and a newline.", 1)
	fake.on(unit.Painter, "", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "spec/core.md", resolved)
		landOther(t, f, "Warmly", map[string]string{
			"spec/core.md": strings.Replace(resolved, "prints hola and a newline.", "prints hola and a newline, warmly.", 1)})
		return done("replied")
	})
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Contested {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("second debate = %s, want contested: %q", out, u.Reason)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Contested || u.Bounces != 0 {
		t.Errorf("after two uncounted bounces past the threshold = %+v", u)
	}
	if line := bounceNoBounceLine(t, f, change); line == "" {
		t.Error("the log does not state that the second bounce counted no bounce")
	}
	notices, err := f.Tracker.Notices(unit.Owner, true)
	must(t, err)
	if len(notices) != 1 || notices[0].Unit != change ||
		!strings.Contains(notices[0].Body, "spec/") || !strings.Contains(notices[0].Body, "landings") {
		t.Errorf("owner notices = %+v", notices)
	}
}

// swept returns how a unit's log describes the rebases after the landing of
// lander, failing the test if any of those events changes the unit's state.
func swept(t *testing.T, f *Factory, change, lander string) []string {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	prefix := "after unit " + unit.Short(lander) + " landed: "
	var out []string
	for _, e := range events {
		if outcome, ok := strings.CutPrefix(tracker.Describe(e), prefix); ok {
			if e.Kind == tracker.UnitMoved || e.To != "" || e.Actor != unit.Shed {
				t.Errorf("the rebase event %+v is not shed's or moves the unit", e)
			}
			out = append(out, outcome)
		}
	}
	return out
}

//shed:proves S.vcs.15
func TestDeferredRebaseIsLogged(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// A unit whose mechanic is at work when the other lands.
	busy := inFlight(t, f, fake, "nod", nil)
	started, release := make(chan struct{}), make(chan struct{})
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "nod.txt", "nod\n")
		close(started)
		<-release
		return done("done")
	})
	var atImplement []string
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result {
		atImplement = swept(t, f, busy, lander)
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(session.Turn) session.Result { return done("done") })
	implemented := make(chan error, 1)
	go func() {
		_, err := f.Implement(ctx, busy)
		implemented <- err
	}()
	<-started

	out, err := f.Land(ctx, lander)
	if err != nil || out != Landed {
		close(release)
		t.Fatalf("land = %s, %v", out, err)
	}
	if got := swept(t, f, busy, lander); !slices.Equal(got, []string{"deferred: a session is running"}) {
		t.Errorf("the busy unit logs %q after the landing, want its deferral", got)
	}
	close(release)
	must(t, <-implemented)

	// Its rebase once the session was captured records the outcome, naming
	// the landed unit.
	want := []string{"deferred: a session is running", "rebased cleanly"}
	if !slices.Equal(atImplement, want) {
		t.Errorf("the busy unit logs %q before its next session, want %q", atImplement, want)
	}
	if got := swept(t, f, busy, lander); !slices.Equal(got, want) {
		t.Errorf("the busy unit logs %q, want %q", got, want)
	}
}

//shed:proves S.vcs.15 S.vcs.16
func TestInterruptedSweepIsLogged(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// A unit apart from the landing and a sealed one that clashes with it
	// outside spec/: S.vcs.16 keeps its rebase, with the conflict stored.
	idle := inFlight(t, f, fake, "wave", map[string]string{"wave.txt": "wave\n"})
	clash := inFlight(t, f, fake, "hola", map[string]string{"bye.go": "package greet\n\n// Bye says adios.\nfunc Bye() string { return \"adios\" }\n"})

	// The landing is recorded and shed stops before the sweep.
	_, err := landing.Land(ctx, f.Tracker, f.Repo, lander, unit.Wheelbuilder)
	must(t, err)
	for _, change := range []string{idle, clash} {
		if got := swept(t, f, change, lander); len(got) != 0 {
			t.Errorf("unit %s logs %q before the sweep ran", unit.Short(change), got)
		}
	}
	must(t, f.Close())

	next, err := Open(ctx, r.Dir, f.State, fake)
	must(t, err)
	t.Cleanup(func() { next.Close() })
	for change, want := range map[string]string{
		idle:  rebasedCleanly,
		clash: rebasedConflict,
	} {
		if got := swept(t, next, change, lander); len(got) != 1 || got[0] != want {
			t.Errorf("unit %s logs %q after the next process opened, want %q", unit.Short(change), got, want)
		}
		if u, _ := next.Tracker.Unit(change); u.State != unit.Sealed {
			t.Errorf("unit %s is %s", unit.Short(change), u.State)
		}
	}
	dir, err := next.Repo.Workspace(ctx, clash)
	must(t, err)
	if got, _ := os.ReadFile(filepath.Join(dir, "bye.go")); !strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("the clashing sealed unit's bye.go keeps no conflict:\n%s", got)
	}
}

//shed:proves S.vcs.16
func TestSealedAndImplementingUnitsKeepNonSpecConflicts(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// The unit that lands touches greet.go.
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// A sealed unit and an implementing unit both edit greet.go in a way
	// that clashes with the landing, but neither touches spec/.
	kindly := "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n"
	sealedUnit := inFlight(t, f, fake, "hola", map[string]string{"greet.go": kindly})
	sealedBefore, err := f.Tracker.Unit(sealedUnit)
	must(t, err)

	gently := strings.Replace(kindly, "kindly", "gently", 1)
	implUnit := inFlight(t, f, fake, "nod", map[string]string{"greet.go": gently})
	must(t, f.Tracker.Move(implUnit, unit.Implementing, unit.Shed, "a mechanic is dispatched"))
	implBefore, err := f.Tracker.Unit(implUnit)
	must(t, err)

	out, err := f.Land(ctx, lander)
	if err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)

	// Both units are on the new main, with the conflict stored and their
	// state, seal and footprint unchanged (S.vcs.16).
	for change, before := range map[string]tracker.Unit{sealedUnit: sealedBefore, implUnit: implBefore} {
		u, err := f.Tracker.Unit(change)
		must(t, err)
		if u.State != before.State {
			t.Errorf("unit %s is %s after the landing, want %s unchanged", unit.Short(change), u.State, before.State)
		}
		if !reflect.DeepEqual(u.Seal, before.Seal) {
			t.Errorf("unit %s's seal = %+v, want %+v unchanged", unit.Short(change), u.Seal, before.Seal)
		}
		if !reflect.DeepEqual(u.Footprint, before.Footprint) {
			t.Errorf("unit %s's footprint = %+v, want %+v unchanged", unit.Short(change), u.Footprint, before.Footprint)
		}
		if got := parent(t, f, r, change); got != main {
			t.Errorf("unit %s sits on %s, want the new main %s", unit.Short(change), got, main)
		}
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); !strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("unit %s's greet.go keeps no conflict:\n%s", unit.Short(change), got)
		}
		if got := swept(t, f, change, lander); !slices.Equal(got, []string{rebasedConflict}) {
			t.Errorf("unit %s logs %q, want %q", unit.Short(change), got, []string{rebasedConflict})
		}
	}

	// The implementing unit's next mechanic session is told of the
	// conflict, and leaves it unresolved.
	const nodProof = "package greet\n\nimport \"testing\"\n\n//shed:proves S.nod.1\nfunc TestNod(t *testing.T) {}\n"
	var toldBundle string
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		toldBundle = turn.Bundle
		write(t, turn.Dir, "nod_test.go", nodProof)
		return done("done")
	})
	fake.on(unit.Mechanic, "implement", func(session.Turn) session.Result { return done("done") })
	fake.on(unit.Mechanic, "docs", func(session.Turn) session.Result { return done("done") })
	must2(t, f.Implement)(implUnit)
	for _, want := range []string{"greet.go", "resolve", "sealed spec"} {
		if !strings.Contains(toldBundle, want) {
			t.Errorf("the mechanic's bundle does not name %q:\n%s", want, toldBundle)
		}
	}

	// Left unresolved, it fails verification and returns to implementing,
	// the same backstop a conflict stored at sealing gets (S.vcs.12).
	reviewed := false
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { reviewed = true; return done("pass") })
	out, err = f.Verify(ctx, implUnit)
	must(t, err)
	if out != Failed {
		t.Fatalf("verify with the conflict unresolved = %s", out)
	}
	if u, _ := f.Tracker.Unit(implUnit); u.State != unit.Implementing {
		t.Errorf("unit = %s, want implementing", u.State)
	}
	if reviewed {
		t.Error("the reviewer ran although the unit holds an unresolved conflict")
	}
	notices, _ := f.Tracker.Notices(unit.Mechanic, true)
	if len(notices) != 1 || !strings.Contains(notices[0].Body, "greet.go") {
		t.Errorf("notices = %+v", notices)
	}

	// Once the mechanic resolves the file, the unit verifies normally.
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets kindly and warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	must2(t, f.Implement)(implUnit)
	if out, err := f.Verify(ctx, implUnit); err != nil || out != Verified {
		t.Fatalf("verify once resolved = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(implUnit); u.State != unit.Queued {
		t.Errorf("unit = %s, want queued", u.State)
	}
}

//shed:proves S.vcs.16 S.queue.2
func TestQueuedUnitsKeepNonSpecConflictsForTheWheelbuilder(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// The unit that lands touches greet.go.
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("done")
	})
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// Three queued units each edit greet.go in a way that clashes with the
	// landing, but none touches spec/ outside its own new clause.
	queued := map[string]string{}
	befores := map[string]tracker.Unit{}
	for _, area := range []string{"hola", "nod", "wave"} {
		proof := fmt.Sprintf("package greet\n\nimport \"testing\"\n\n//shed:proves S.%s.1\nfunc Test%s(t *testing.T) {}\n", area, strings.ToUpper(area[:1])+area[1:])
		greet := fmt.Sprintf("package greet\n\n// Hello greets with a %s.\nfunc Hello() string { return \"hello\" }\n", area)
		change := inFlight(t, f, fake, area, map[string]string{"greet.go": greet, area + "_test.go": proof})
		for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
			must(t, f.Tracker.Move(change, s, unit.Shed, "by hand"))
		}
		before, err := f.Tracker.Unit(change)
		must(t, err)
		queued[area], befores[change] = change, before
	}

	out, err := f.Land(ctx, lander)
	if err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)

	// Each queued unit keeps its rebase with the conflict stored, on the new
	// main, its state, seal and footprint unchanged (S.vcs.16).
	for change, before := range befores {
		u, err := f.Tracker.Unit(change)
		must(t, err)
		if u.State != unit.Queued {
			t.Errorf("unit %s is %s after the landing, want queued", unit.Short(change), u.State)
		}
		if !reflect.DeepEqual(u.Seal, before.Seal) {
			t.Errorf("unit %s's seal = %+v, want %+v unchanged", unit.Short(change), u.Seal, before.Seal)
		}
		if !reflect.DeepEqual(u.Footprint, before.Footprint) {
			t.Errorf("unit %s's footprint = %+v, want %+v unchanged", unit.Short(change), u.Footprint, before.Footprint)
		}
		if got := parent(t, f, r, change); got != main {
			t.Errorf("unit %s sits on %s, want the new main %s", unit.Short(change), got, main)
		}
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); !strings.Contains(string(got), "<<<<<<<") {
			t.Errorf("unit %s's greet.go keeps no conflict:\n%s", unit.Short(change), got)
		}
		if got := swept(t, f, change, lander); !slices.Equal(got, []string{rebasedConflict}) {
			t.Errorf("unit %s logs %q, want %q", unit.Short(change), got, []string{rebasedConflict})
		}
	}

	// Main has not moved since the sweep stored those conflicts, so each
	// landing's own rebase changes nothing; the wheelbuilder still runs on
	// the stored conflict before anything lands (S.queue.2).
	wheel := func(change string, resolve func(dir string) session.Result) *bool {
		ran := false
		fake.on(unit.Wheelbuilder, "resolve", func(turn session.Turn) session.Result {
			if turn.Unit != change {
				t.Errorf("the wheelbuilder ran for %s, want %s", unit.Short(turn.Unit), unit.Short(change))
			}
			ran = true
			if got, _ := os.ReadFile(filepath.Join(turn.Dir, "greet.go")); !strings.Contains(string(got), "<<<<<<<") {
				t.Errorf("the wheelbuilder's greet.go has no conflict markers:\n%s", got)
			}
			return resolve(turn.Dir)
		})
		return &ran
	}
	reopens := func(change, why string) {
		t.Helper()
		out, err := f.Land(ctx, change)
		must(t, err)
		if out != Reopened {
			t.Errorf("land of the %s unit = %s, want reopened", why, out)
		}
		if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed {
			t.Errorf("the %s unit is %s, want proposed", why, u.State)
		}
		if now, _ := f.Repo.MainCommit(ctx); now != main {
			t.Errorf("main moved to %s when the %s unit reopened, want %s", now, why, main)
		}
	}

	// A wheelbuilder that reports unresolvable reopens the unit and nothing
	// lands.
	ran := wheel(queued["nod"], func(string) session.Result {
		res := done("unresolvable")
		res.Note = "both reword Hello's comment"
		return res
	})
	reopens(queued["nod"], "unresolvable")
	if !*ran {
		t.Error("the wheelbuilder never ran for the unresolvable unit")
	}

	// A wheelbuilder that reports resolved but leaves the markers in place
	// also reopens the unit, and nothing lands.
	ran = wheel(queued["wave"], func(string) session.Result { return done("resolved") })
	reopens(queued["wave"], "still conflicted")
	if !*ran {
		t.Error("the wheelbuilder never ran for the still-conflicted unit")
	}

	// A wheelbuilder that resolves the file lets the unit land.
	ran = wheel(queued["hola"], func(dir string) session.Result {
		write(t, dir, "greet.go", "package greet\n\n// Hello greets warmly with a hola.\nfunc Hello() string { return \"hello\" }\n")
		return done("resolved")
	})
	if out, err := f.Land(ctx, queued["hola"]); err != nil || out != Landed {
		t.Fatalf("land of the resolved unit = %s, %v", out, err)
	}
	if !*ran {
		t.Error("the wheelbuilder never ran for the resolved unit")
	}
	landed, err := f.Repo.MainCommit(ctx)
	must(t, err)
	if got := r.Git("show", landed+":greet.go"); !strings.Contains(got, "warmly with a hola") || strings.Contains(got, "<<<<<<<") {
		t.Errorf("main's greet.go = %q", got)
	}
}

//shed:proves S.vcs.16
func TestSpecConflictStillUndoesAnImplementingUnitsRebase(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// A unit sealed on the old main, editing the same clause the landing
	// edits, and moved past sealing to implementing.
	hola, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(hola, "Hola", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, hola)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	must(t, f.Declare(ctx, hola, "", nil, []string{"H.greet.1"}, unit.Painter))
	if out, err := f.Debate(ctx, hola); err != nil || out != Sealed {
		t.Fatalf("debate of hola = %s, %v", out, err)
	}
	must(t, f.Tracker.Move(hola, unit.Implementing, unit.Shed, "a mechanic is dispatched"))
	before, err := f.Repo.Commit(ctx, hola)
	must(t, err)
	base := parent(t, f, r, hola)

	// The landing edits the very same line.
	newline, err := f.Repo.NewUnit(ctx, "Newline")
	must(t, err)
	must(t, f.Tracker.OpenUnit(newline, "Newline", unit.Painter))
	ndir, err := f.Repo.Workspace(ctx, newline)
	must(t, err)
	write(t, ndir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1))
	must(t, f.Declare(ctx, newline, "", nil, []string{"H.greet.1"}, unit.Painter))
	if out, err := f.Debate(ctx, newline); err != nil || out != Sealed {
		t.Fatalf("debate of newline = %s, %v", out, err)
	}
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, f.Tracker.Move(newline, s, unit.Shed, "by hand"))
	}

	out, err := f.Land(ctx, newline)
	if err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}

	// The implementing unit's rebase conflicts under spec/, so S.vcs.16's
	// exception does not apply: it is undone as S.vcs.10 says, left as it
	// was.
	if now, _ := f.Repo.Commit(ctx, hola); now != before {
		t.Errorf("the implementing unit conflicting under spec/ moved from %s to %s", before, now)
	}
	if got := parent(t, f, r, hola); got != base {
		t.Errorf("the implementing unit sits on %s, want its old base %s", got, base)
	}
	if u, _ := f.Tracker.Unit(hola); u.State != unit.Implementing {
		t.Errorf("unit = %s, want implementing", u.State)
	}
	if got := swept(t, f, hola, newline); !slices.Equal(got, []string{rebaseUndone}) {
		t.Errorf("the unit logs %q, want %q", got, []string{rebaseUndone})
	}
}
