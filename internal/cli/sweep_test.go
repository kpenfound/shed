package cli

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

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
