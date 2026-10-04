package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/landing"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// sweepLog returns the lines of a unit's log that report a rebase after the
// landing of lander.
func sweepLog(t *testing.T, dir, change, lander string) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(mustRun(t, dir, "unit", "log", change), "\n"), "\n") {
		if strings.Contains(line, "after unit "+unit.Short(lander)+" landed: ") {
			lines = append(lines, line)
		}
	}
	return lines
}

//shed:proves S.vcs.15 S.vcs.16
func TestLandReportsTheSweep(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	state := filepath.Join(r.Dir, DefaultStateDir)
	put := func(change, name, content string) {
		t.Helper()
		dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	withTracker := func(fn func(*tracker.Tracker) error) {
		t.Helper()
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer tr.Close()
		if err := fn(tr); err != nil {
			t.Fatal(err)
		}
	}

	lander := openUnit(t, r.Dir, "Say goodbye")
	put(lander, "bye.txt", "bye\n")

	// Units in flight, in the order they open: one apart from the landing,
	// one with no change behind it, a proposal, a sealed unit and a frame
	// unit that clash with the landing, one with a session running, and one
	// archived.
	clean := openUnit(t, r.Dir, "Wave")
	put(clean, "wave.txt", "wave\n")
	const ghost = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	withTracker(func(tr *tracker.Tracker) error { return tr.OpenUnit(ghost, "Ghost", unit.Painter) })
	proposed := openUnit(t, r.Dir, "Say farewell")
	put(proposed, "bye.txt", "farewell\n")
	past := openUnit(t, r.Dir, "Say so long")
	put(past, "bye.txt", "so long\n")
	seal(t, state, past)
	repo, err := vcs.Open(context.Background(), r.Dir, state, vcs.Options{Remote: "origin",
		Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	framed, err := repo.NewUnit(context.Background(), "Frame H.greet.9")
	if err != nil {
		t.Fatal(err)
	}
	withTracker(func(tr *tracker.Tracker) error { return tr.OpenUnit(framed, "Frame H.greet.9", unit.FrameBuilder) })
	put(framed, "bye.txt", "adieu\n")
	busy := openUnit(t, r.Dir, "Nod")
	put(busy, "nod.txt", "nod\n")
	var running tracker.Session
	withTracker(func(tr *tracker.Tracker) (err error) {
		running, err = tr.StartSession(busy, unit.Mechanic, "proofs", os.Getpid())
		return err
	})
	shelved := openUnit(t, r.Dir, "Shrug")
	withTracker(func(tr *tracker.Tracker) error {
		return tr.Archive(shelved, unit.Rejected, unit.Committee, "not wanted")
	})

	seal(t, state, lander)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", lander, s, "by hand")
	}
	stdout, stderr, code := run(t, r.Dir, "land", lander)
	if code != OK {
		t.Fatalf("land = %d\n%s", code, stderr)
	}
	commit := r.GitRemote("rev-parse", "main")
	const undone = "rebase undone: the rebase conflicted and the unit is past its seal or is a frame unit"
	const conflicted = "rebased with conflicts stored in its change"
	want := []string{
		"landed " + unit.Short(lander) + " on main as " + commit,
		"footprint held",
		unit.Short(clean) + " proposed: rebased cleanly",
		`^` + unit.Short(ghost) + ` proposed: rebase failed: \S.*`,
		unit.Short(proposed) + " proposed: " + conflicted,
		// A sealed unit whose clash is only outside spec/ keeps the rebase
		// with the conflict stored (S.vcs.16); only the frame unit, which no
		// session resolves, still has it undone.
		unit.Short(past) + " sealed: " + conflicted,
		unit.Short(framed) + " proposed: " + undone,
		unit.Short(busy) + " proposed: deferred: a session is running",
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("land printed\n%s\nwant %d lines", stdout, len(want))
	}
	for i, w := range want {
		if strings.HasPrefix(w, "^") {
			if !regexp.MustCompile(w + "$").MatchString(lines[i]) {
				t.Errorf("line %d = %q, want %s", i+1, lines[i], w)
			}
		} else if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i+1, lines[i], w)
		}
	}

	// Each unit's log records its outcome as an event naming the landed
	// unit, and no unit changed state.
	byShed := regexp.MustCompile(`^\d+\s+\S+Z\s+shed\s+after unit `)
	for change, outcome := range map[string]string{
		clean:    "rebased cleanly",
		ghost:    "rebase failed: ",
		proposed: conflicted,
		past:     conflicted,
		framed:   undone,
		busy:     "deferred: a session is running",
	} {
		logged := sweepLog(t, r.Dir, change, lander)
		if len(logged) != 1 || !byShed.MatchString(logged[0]) || !strings.Contains(logged[0], "landed: "+outcome) {
			t.Errorf("unit %s logs the sweep as %q, want one event reporting %q", unit.Short(change), logged, outcome)
		}
	}
	if logged := sweepLog(t, r.Dir, shelved, lander); len(logged) != 0 {
		t.Errorf("the archived unit logs the sweep: %q", logged)
	}
	wantStates := map[string]unit.State{
		clean: unit.Proposed, ghost: unit.Proposed, proposed: unit.Proposed,
		past: unit.Sealed, framed: unit.Proposed, busy: unit.Proposed, shelved: unit.Archived,
	}
	withTracker(func(tr *tracker.Tracker) error {
		for change, state := range wantStates {
			if u, err := tr.Unit(change); err != nil || u.State != state {
				t.Errorf("unit %s = %s, %v after the landing, want %s", unit.Short(change), u.State, err, state)
			}
		}
		// Once the session ends, the deferred unit is left for the next
		// shed process to rebase.
		return tr.FinishSession(running.ID, tracker.Succeeded, "done", 0, true)
	})

	// The next shed process rebases the deferred unit, records its outcome
	// naming the landed unit and prints nothing about it.
	stdout, stderr, code = run(t, r.Dir, "land", busy)
	if code != Failed || !strings.Contains(stderr, "only queued units land") {
		t.Errorf("land of a proposed unit = %d, %q", code, stderr)
	}
	if stdout != "" {
		t.Errorf("the next process printed %q", stdout)
	}
	logged := sweepLog(t, r.Dir, busy, lander)
	if len(logged) != 2 || !strings.HasSuffix(logged[1], "landed: rebased cleanly") {
		t.Errorf("the deferred unit logs the sweep as %q, want its deferral then a clean rebase", logged)
	}
	withTracker(func(tr *tracker.Tracker) error {
		if u, err := tr.Unit(busy); err != nil || u.State != unit.Proposed {
			t.Errorf("the deferred unit = %s, %v after its rebase", u.State, err)
		}
		return nil
	})
}

// sweepProject is a colocated repository whose main holds a Go module with
// one passing proof of S.core.1, and a runner that logs each run of the
// proofs it wraps to an absolute path outside any directory a sweep makes
// and removes, so the log survives the sweep that used it.
func sweepProject(t *testing.T) (*testrepo.Repo, string) {
	t.Helper()
	r := testrepo.Colocated(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {}\n")
	logPath := filepath.Join(t.TempDir(), "runner.log")
	r.Write("run-here", "#!/bin/sh\necho ran >> "+logPath+"\nexec \"$@\"\n")
	if err := os.Chmod(filepath.Join(r.Dir, "run-here"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Write("shed.toml", "[proofs]\nrunner = [\"./run-here\"]\n")
	r.JJ("commit", "-m", "module")
	r.JJ("bookmark", "set", "main", "-r", "@-")
	return r, logPath
}

//shed:proves S.sweep.1
func TestSweepReportsPerClauseAndLeavesEverythingUnchanged(t *testing.T) {
	r, logPath := sweepProject(t)
	mainBefore := r.Git("rev-parse", "main")
	statusBefore := r.Git("status", "--porcelain")

	stdout, _, code := run(t, r.Dir, "sweep")
	if code != OK || stdout != "pass  S.core.1\n" {
		t.Fatalf("sweep = %d, %q", code, stdout)
	}
	if log, err := os.ReadFile(logPath); err != nil || string(log) != "ran\n" {
		t.Errorf("runner log = %q, %v; the sweep did not run the swept commit's runner", log, err)
	}
	if main := r.Git("rev-parse", "main"); main != mainBefore {
		t.Errorf("main moved from %s to %s", mainBefore, main)
	}
	if status := r.Git("status", "--porcelain"); status != statusBefore {
		t.Errorf("the owner's working copy changed: %q", status)
	}

	// Sweeping twice leaves no stray directory in the way of the next sweep.
	if _, _, code := run(t, r.Dir, "sweep"); code != OK {
		t.Errorf("a second sweep = %d", code)
	}

	// A clause with a failing proof is reported, then listed again, and
	// sweep exits non-zero.
	r.Write("spec/core.md", testrepo.Spec+"- **S.core.2** (H.greet.2) Says goodbye.\n")
	r.Write("bye_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.2\nfunc TestBye(t *testing.T) { t.Fatal(\"no goodbye\") }\n")
	r.JJ("commit", "-m", "goodbye spec")
	r.JJ("bookmark", "set", "main", "-r", "@-")

	stdout, _, code = run(t, r.Dir, "sweep")
	want := "pass  S.core.1\nfail  S.core.2  TestBye: fail\nS.core.2\n"
	if code != Failed || stdout != want {
		t.Errorf("sweep = %d\n%s\nwant\n%s", code, stdout, want)
	}
}

//shed:proves S.sweep.1
func TestSweepFinishesAnInterruptedLandingsRebase(t *testing.T) {
	r, _ := sweepProject(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	state := filepath.Join(r.Dir, DefaultStateDir)
	writeFile := func(change, name, content string) {
		t.Helper()
		dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	other := openUnit(t, r.Dir, "Wave")
	writeFile(other, "wave.txt", "wave\n")
	lander := openUnit(t, r.Dir, "Say goodbye")
	writeFile(lander, "bye.txt", "bye\n")
	seal(t, state, lander)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", lander, s, "by hand")
	}

	// The landing pushes main and records itself, but the process stops
	// before rebasing the other unit onto it: the crash S.vcs.11 recovers
	// from.
	repo, err := vcs.Open(context.Background(), r.Dir, state, vcs.Options{Remote: "origin",
		Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := landing.Land(context.Background(), tr, repo, lander, unit.Wheelbuilder)
	if err != nil {
		t.Fatal(err)
	}
	tr.Close()
	if remote := r.GitRemote("rev-parse", "main"); remote != commit {
		t.Fatalf("the landing did not push main: remote is at %s, want %s", remote, commit)
	}

	// shed sweep is the next shed process to open the repository.
	stdout, stderr, code := run(t, r.Dir, "sweep")
	if code != OK || stdout != "pass  S.core.1\n" {
		t.Fatalf("sweep = %d, %q, stderr %q", code, stdout, stderr)
	}

	logged := sweepLog(t, r.Dir, other, lander)
	if len(logged) != 1 || !strings.HasSuffix(logged[0], "landed: rebased cleanly") {
		t.Errorf("the other unit logs the sweep as %q, want one clean rebase", logged)
	}
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", other))
	if data, err := os.ReadFile(filepath.Join(dir, "bye.txt")); err != nil || string(data) != "bye\n" {
		t.Errorf("the other unit's workspace lacks the landed file: %q, %v", data, err)
	}

	tr2, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr2.Close()
	if u, err := tr2.Unit(other); err != nil || u.State != unit.Proposed {
		t.Errorf("the other unit = %s, %v; it should keep its state across the recovery", u.State, err)
	}
	if u, err := tr2.Unit(lander); err != nil || u.State != unit.Landed {
		t.Errorf("the landed unit = %s, %v; sweep should not move it", u.State, err)
	}
	if remote := r.GitRemote("rev-parse", "main"); remote != commit {
		t.Errorf("main moved from %s to %s during sweep", commit, remote)
	}
	sweeps, err := tr2.Sweeps()
	if err != nil {
		t.Fatal(err)
	}
	if len(sweeps) != 1 || sweeps[0].Commit != commit {
		t.Errorf("sweeps = %+v, want one sweep of %s", sweeps, commit)
	}
}

//shed:proves S.sweep.2
func TestSweepRecordsItselfAndRebuildReplays(t *testing.T) {
	r, _ := sweepProject(t)
	commit := r.Git("rev-parse", "main")
	state := filepath.Join(r.Dir, DefaultStateDir)

	before := time.Now()
	if _, _, code := run(t, r.Dir, "sweep"); code != OK {
		t.Fatal("sweep did not succeed")
	}
	after := time.Now()

	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sweeps, err := tr.Sweeps()
	tr.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(sweeps) != 1 {
		t.Fatalf("got %d sweeps, want 1", len(sweeps))
	}
	s := sweeps[0]
	if s.Commit != commit {
		t.Errorf("sweep commit = %s, want %s", s.Commit, commit)
	}
	if s.Started.Before(before) || s.Started.After(after) {
		t.Errorf("sweep started at %s, want between %s and %s", s.Started, before, after)
	}
	if len(s.Clauses) != 1 || s.Clauses[0].Clause != "S.core.1" || !s.Clauses[0].Pass {
		t.Errorf("sweep clauses = %+v, want S.core.1 passing", s.Clauses)
	}

	if out := mustRun(t, r.Dir, "tracker", "rebuild"); !strings.Contains(out, "rebuilt the tracker from events.jsonl") {
		t.Errorf("rebuild = %q", out)
	}
	tr2, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr2.Close()
	after2, err := tr2.Sweeps()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sweeps, after2) {
		t.Errorf("sweeps after rebuild = %+v, want %+v", after2, sweeps)
	}
}

//shed:proves S.sweep.2
func TestSweepRecordsNothingWhenMainIsUnreachable(t *testing.T) {
	r := testrepo.Minimal(t)
	_, stderr, code := run(t, r.Dir, "sweep")
	if code == OK || stderr == "" {
		t.Fatalf("sweep on a repository with no main = %d, stderr %q", code, stderr)
	}
	logPath := filepath.Join(r.Dir, DefaultStateDir, tracker.LogFile)
	if data, err := os.ReadFile(logPath); err == nil && strings.Contains(string(data), tracker.SweepRan) {
		t.Errorf("a sweep that could not check out main recorded itself: %s", data)
	}
}
