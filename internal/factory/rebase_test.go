package factory

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

//shed:proves S.vcs.10 S.vcs.11
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

	// Units in flight: one apart from the landing, one past its seal that
	// clashes with it, a proposed and a contested one that clash with it,
	// and one whose mechanic is at work when it lands.
	idle := inFlight(t, f, fake, "wave", map[string]string{"wave.txt": "wave\n"})
	kindly := "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n"
	clash := inFlight(t, f, fake, "hola", map[string]string{"greet.go": kindly})
	must(t, f.Tracker.Move(clash, unit.Implementing, unit.Shed, "a mechanic is dispatched"))
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

	// A unit past its seal whose rebase conflicts is left as it was, in
	// the same state, and not reopened.
	if now, _ := f.Repo.Commit(ctx, clash); now != clashBefore {
		t.Errorf("the clashing implementing unit moved from %s to %s", clashBefore, now)
	}
	if got := parent(t, f, r, clash); got != clashBase {
		t.Errorf("the clashing implementing unit sits on %s, want its old base %s", got, clashBase)
	}
	if u, _ := f.Tracker.Unit(clash); u.State != unit.Implementing || u.Bounces != 0 {
		t.Errorf("the clashing implementing unit = %+v, want implementing with no bounce", u)
	}
	dir, err = f.Repo.Workspace(ctx, clash)
	must(t, err)
	if got, _ := os.ReadFile(filepath.Join(dir, "greet.go")); string(got) != kindly {
		t.Errorf("the clashing implementing unit's greet.go = %q, want its own %q", got, kindly)
	}
	if _, err := os.Stat(filepath.Join(dir, "bye.go")); err == nil {
		t.Error("the clashing implementing unit's workspace took the landing")
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

//shed:proves S.vcs.10
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

//shed:proves S.vcs.15
func TestInterruptedSweepIsLogged(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[vcs]\nremote = \"origin\"\n")
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })
	lander := sealed(t, f, fake)
	mechanic(t, fake)
	must2(t, f.Implement)(lander)
	must2(t, f.Verify)(lander)

	// A unit apart from the landing and one past its seal that clashes with
	// it.
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
		idle:  "rebased cleanly",
		clash: "rebase undone: the rebase conflicted and the unit is past its seal or is a frame unit",
	} {
		if got := swept(t, next, change, lander); len(got) != 1 || got[0] != want {
			t.Errorf("unit %s logs %q after the next process opened, want %q", unit.Short(change), got, want)
		}
		if u, _ := next.Tracker.Unit(change); u.State != unit.Sealed {
			t.Errorf("unit %s is %s", unit.Short(change), u.State)
		}
	}
}
