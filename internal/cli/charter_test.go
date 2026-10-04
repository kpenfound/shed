package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// openCharterDraft opens a charter amendment unit through the command line
// and returns its short change ID and its workspace's directory, exactly
// what `shed charter draft` prints (S.owner.22).
func openCharterDraft(t *testing.T, dir, title string) (change, workspace string) {
	t.Helper()
	out := strings.Fields(strings.TrimSpace(mustRun(t, dir, "charter", "draft", title)))
	if len(out) != 2 || unit.ValidChangeID(out[0]) != nil {
		t.Fatalf("charter draft printed %q", out)
	}
	return out[0], out[1]
}

// TestCharterDraft checks that `shed charter draft <title>` opens a charter
// amendment unit on main as `shed unit open` does, with the owner as actor
// and the given title, that the unit is recorded and stays recorded, after
// a tracker rebuild, as a charter amendment unit, that it stays proposed and
// is refused by `shed unit move`, naming the unit, that it takes no place
// toward painter.max_proposed, and that `-discard` archives it as deferred
// with no archive entry and discards its change, while refusing any other
// unit.
//
//shed:proves S.owner.22 S.track.2 S.track.3 S.track.5 S.unit.8 S.paint.1
func TestCharterDraft(t *testing.T) {
	r := testrepo.Colocated(t)

	for _, args := range [][]string{
		{"charter", "draft"},
		{"charter", "draft", "   "},
	} {
		if _, stderr, code := run(t, r.Dir, args...); code != Misused || !strings.Contains(stderr, "needs a title") {
			t.Errorf("%v = %d, %q; want a misuse naming the missing title", args, code, stderr)
		}
	}
	if out := mustRun(t, r.Dir, "status"); out != "no units\npainter: may propose now\namendments: 0 auto-accepted, 0 sampled (0 agreed, 0 disagreed, 0 unanswered)\nbugs: none\n" {
		t.Fatalf("a refused charter draft opened something:\n%s", out)
	}

	change, dir := openCharterDraft(t, r.Dir, "Make the tool politer")

	if _, err := os.Stat(filepath.Join(dir, "charter.md")); err != nil {
		t.Errorf("workspace: %v", err)
	}
	if got := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change)); got != dir {
		t.Errorf("unit path = %q, want the charter draft's workspace %q", got, dir)
	}

	u := unitNow(t, r, change)
	if !u.CharterAmendment {
		t.Error("the charter draft is not recorded as a charter amendment unit")
	}
	if u.OpenedBy != unit.Owner {
		t.Errorf("the charter draft was opened by %s, want the owner", u.OpenedBy)
	}
	if u.Title != "Make the tool politer" {
		t.Errorf("the charter draft's title = %q", u.Title)
	}
	if u.State != unit.Proposed {
		t.Errorf("the charter draft is %s, want proposed", u.State)
	}

	// It carries no footprint and takes no place toward
	// painter.max_proposed: the painter still reports it may propose now,
	// exactly as it did before any unit opened.
	if status := mustRun(t, r.Dir, "status"); !strings.Contains(status, "painter: may propose now") {
		t.Errorf("status after opening a charter draft does not say the painter may propose now:\n%s", status)
	}

	// The event log names the unit a charter amendment unit (S.track.3).
	tr, err := tracker.Open(filepath.Join(r.Dir, DefaultStateDir), tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	opened, err := tr.Events(change)
	tr.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(opened) == 0 || opened[0].Kind != tracker.UnitOpened || !opened[0].CharterAmendment {
		t.Errorf("the unit's opening event does not name it a charter amendment unit: %+v", opened)
	}

	// `shed unit move` refuses any move of a charter amendment unit, naming
	// the unit (S.unit.8).
	if _, stderr, code := run(t, r.Dir, "unit", "move", change, "implementing", "by hand"); code == OK || !strings.Contains(stderr, unit.Short(change)) {
		t.Errorf("unit move on a charter draft = %d, %q; want a refusal naming the unit", code, stderr)
	}

	// The flag and the refusal both survive a tracker rebuild (S.track.5).
	mustRun(t, r.Dir, "tracker", "rebuild")
	u = unitNow(t, r, change)
	if !u.CharterAmendment || u.State != unit.Proposed {
		t.Errorf("after rebuild: charter amendment=%v, state=%s", u.CharterAmendment, u.State)
	}
	if _, stderr, code := run(t, r.Dir, "unit", "move", change, "implementing", "by hand"); code == OK || !strings.Contains(stderr, unit.Short(change)) {
		t.Errorf("unit move on a charter draft after rebuild = %d, %q", code, stderr)
	}

	// `-discard` archives a proposed charter amendment unit as deferred with
	// no archive entry, and discards its change.
	mustRun(t, r.Dir, "charter", "draft", "-discard", change)
	u = unitNow(t, r, change)
	if u.State != unit.Archived || u.Shelf != unit.Deferred {
		t.Errorf("a discarded charter draft is %s on %q, want archived on deferred", u.State, u.Shelf)
	}
	if entries, err := archive.Read(r.Dir); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Errorf("discarding a charter draft wrote archive entries: %+v", entries)
	}
	if _, _, code := run(t, r.Dir, "unit", "path", change); code == OK {
		t.Error("discarding a charter draft kept its change")
	}

	// `-discard` refuses any other unit.
	ordinary := openUnit(t, r.Dir, "An ordinary unit")
	if _, stderr, code := run(t, r.Dir, "charter", "draft", "-discard", ordinary); code == OK || !strings.Contains(stderr, unit.Short(ordinary)) {
		t.Errorf("charter draft -discard on an ordinary unit = %d, %q; want a refusal naming it", code, stderr)
	}
	if u := unitNow(t, r, ordinary); u.State != unit.Proposed {
		t.Errorf("a refused -discard changed an ordinary unit's state to %s", u.State)
	}

	// It also refuses an already archived charter draft.
	if _, _, code := run(t, r.Dir, "charter", "draft", "-discard", change); code == OK {
		t.Error("charter draft -discard took an already archived unit")
	}
}

// TestServeSkipsACharterAmendmentUnit checks that no controller of
// `shed serve` starts a session on a charter amendment unit, that `-once`
// never calls it a draft waiting to be declared, and that repeated passes,
// as after a crash, neither move nor archive it (S.owner.22, S.serve.1,
// S.serve.8).
//
//shed:proves S.owner.22 S.serve.1 S.serve.8
func TestServeSkipsACharterAmendmentUnit(t *testing.T) {
	r := testrepo.Colocated(t)
	change, _ := openCharterDraft(t, r.Dir, "Make the tool politer")

	rn := &guardedRunner{t: t}
	for range 2 {
		out, errOut, code := runWithRunner(t, r.Dir, rn, "serve", "-once")
		if code != OK {
			t.Fatalf("serve -once = %d, %q, %q", code, out, errOut)
		}
		if strings.Contains(out, unit.Short(change)+" is a draft") {
			t.Errorf("serve calls the charter draft a draft to declare:\n%s", out)
		}
	}
	if u := unitNow(t, r, change); u.State != unit.Proposed {
		t.Errorf("serving moved the charter draft to %s", u.State)
	}
}

// guardedRunner plays the painter, reporting nothing, and fails the test if
// any other role runs a session.
type guardedRunner struct{ t *testing.T }

func (g *guardedRunner) Run(_ context.Context, turn session.Turn) (session.Result, error) {
	if turn.Role == unit.Painter {
		return session.Result{Status: "nothing", CostUSD: 0.1}, nil
	}
	g.t.Errorf("%s ran step %q", turn.Role, turn.Step)
	return session.Result{}, nil
}

func runWithRunner(t *testing.T, dir string, r session.Runner, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = RunWith(context.Background(), append([]string{"-C", dir}, args...), &out, &errOut, r)
	return out.String(), errOut.String(), code
}

// TestDebateOnACharterAmendmentUnit checks that `shed debate` on a charter
// amendment unit starts no session and moves nothing, instead judging the
// files in the unit's workspace against charter.md on the latest main
// commit the unit's change descends from: a file other than charter.md that
// differs, the file being present on one side and absent on the other
// counting as differing; charter.md being byte for byte unchanged; and a
// problem shed check would report in charter.md, as file:line: message.
// With no problem, it says the amendment is ready but that charter
// amendments are not yet debated. It exits non-zero either way (S.owner.23),
// and, since S.shed.1 hands a charter amendment unit to S.owner.23 instead
// of debating it in committee rounds, no committee round ever runs.
//
//shed:proves S.owner.23 S.shed.1
func TestDebateOnACharterAmendmentUnit(t *testing.T) {
	r := testrepo.Colocated(t)
	change, dir := openCharterDraft(t, r.Dir, "Make the tool politer")

	wantUnmoved := func() {
		if u := unitNow(t, r, change); u.State != unit.Proposed || u.Round != 0 || u.Bounces != 0 {
			t.Errorf("debate moved the charter draft: %+v", u)
		}
	}

	// charter.md untouched: the amendment changes nothing.
	stdout, _, code := run(t, r.Dir, "debate", change)
	if code == OK {
		t.Fatalf("debate succeeded on an untouched charter draft: %q", stdout)
	}
	if !strings.Contains(stdout, "charter.md") {
		t.Errorf("debate on an unchanged charter draft = %q, want it to name charter.md", stdout)
	}
	wantUnmoved()

	// A stray file other than charter.md, present in the workspace and
	// absent from main, is named as a problem.
	polite := testrepo.Charter + "- **C3** The tool is polite.\n"
	if err := os.WriteFile(filepath.Join(dir, "charter.md"), []byte(polite), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("stray\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = run(t, r.Dir, "debate", change)
	if code == OK {
		t.Fatalf("debate succeeded with a stray file: %q", stdout)
	}
	if !strings.Contains(stdout, "extra.txt") {
		t.Errorf("debate = %q, want it to name extra.txt", stdout)
	}
	wantUnmoved()

	// A file present on main and absent from the workspace also counts as
	// differing.
	if err := os.Remove(filepath.Join(dir, "extra.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "spec", "core.md")); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = run(t, r.Dir, "debate", change)
	if code == OK {
		t.Fatalf("debate succeeded with a missing file: %q", stdout)
	}
	if !strings.Contains(stdout, filepath.Join("spec", "core.md")) && !strings.Contains(stdout, "spec/core.md") {
		t.Errorf("debate = %q, want it to name the missing spec/core.md", stdout)
	}
	wantUnmoved()

	// Restoring the spec and leaving charter.md with a problem shed check
	// would report (a duplicate ID, S.doc.5) names it as file:line: message.
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(testrepo.Spec), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := testrepo.Charter + "- **C3** The tool is polite.\n- **C3** A duplicate.\n"
	if err := os.WriteFile(filepath.Join(dir, "charter.md"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = run(t, r.Dir, "debate", change)
	if code == OK {
		t.Fatalf("debate succeeded with a duplicate ID: %q", stdout)
	}
	if !regexp.MustCompile(`charter\.md:\d+: duplicate ID C3`).MatchString(stdout) {
		t.Errorf("debate = %q, want a file:line: message naming the duplicate ID", stdout)
	}
	wantUnmoved()

	// A clean amendment: charter.md differs from main, no other file does,
	// and charter.md holds no problem. Debate reports it ready, but that
	// charter amendments are not yet debated, and still exits non-zero.
	if err := os.WriteFile(filepath.Join(dir, "charter.md"), []byte(polite), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, _, code = run(t, r.Dir, "debate", change)
	if code == OK {
		t.Fatalf("debate on a clean amendment exited zero: %q", stdout)
	}
	for _, want := range []string{"ready", "not yet debated"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("debate on a clean amendment = %q, want it to contain %q", stdout, want)
		}
	}
	wantUnmoved()
}
