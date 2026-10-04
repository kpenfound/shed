package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// painterLine returns the last line of shed status's output, trimmed: the
// painter's readiness line S.serve.9 adds after the units and notices.
func painterLine(t *testing.T, dir string) string {
	t.Helper()
	out := strings.TrimRight(mustRun(t, dir, "status"), "\n")
	lines := strings.Split(out, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

//shed:proves S.serve.9
func TestStatusShowsThePaintersReadiness(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\n")
	state := filepath.Join(r.Dir, DefaultStateDir)

	// A unit that has left proposed does not hold the painter back, and the
	// gap (H.greet.2) is not empty: the painter may propose now.
	a := openUnit(t, r.Dir, "Warm up")
	seal(t, state, a)
	mustRun(t, r.Dir, "unit", "move", a, "implementing", "start work")

	// A contested unit prints a notice; the painter line still comes after
	// the units and the notices.
	contested := openUnit(t, r.Dir, "Say goodbye")
	seal(t, state, contested)
	mustRun(t, r.Dir, "unit", "reopen", "-amendment", contested, "the spec is ambiguous")

	out := mustRun(t, r.Dir, "status")
	noticesAt := strings.Index(out, "Waiting for the owner:")
	painterAt := strings.Index(out, "painter: may propose now")
	if noticesAt < 0 || painterAt < 0 || painterAt < noticesAt {
		t.Fatalf("status =\n%s\nwant the painter line after the units and the notices", out)
	}
	if got := painterLine(t, r.Dir); got != "painter: may propose now" {
		t.Errorf("painter line = %q, want %q", got, "painter: may propose now")
	}

	// A framing proposal waiting for owner acceptance is not a proposal
	// (S.frame.3) and does not hold the painter back.
	framing := "kzkzkzkzkzkz"
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.OpenUnit(framing, "Framing", unit.FrameBuilder); err != nil {
		t.Fatal(err)
	}
	tr.Close()
	if got := painterLine(t, r.Dir); got != "painter: may propose now" {
		t.Errorf("a framing proposal held the painter back: %q", got)
	}

	// An ordinary proposal waiting in the shed reaches painter.max_proposed
	// (1 by default) and holds the painter back, word for word as `shed
	// serve -once` reports it under S.serve.1 (S.paint.1).
	c := openUnit(t, r.Dir, "Third proposal")
	want := "painter: 1 proposals are waiting in the shed (painter.max_proposed = 1)"
	if got := painterLine(t, r.Dir); got != want {
		t.Errorf("painter line at the cap = %q, want %q", got, want)
	}

	// Once it leaves proposed, the painter may propose again.
	seal(t, state, c)
	mustRun(t, r.Dir, "unit", "move", c, "implementing", "by hand")
	if got := painterLine(t, r.Dir); got != "painter: may propose now" {
		t.Errorf("painter line after the cap clears = %q, want %q", got, "painter: may propose now")
	}
}

// TestStatusPainterLineAfterABackoff checks that once a painter proposal has
// gone nowhere, shed status reports the same backoff wait that shed serve
// -once reports under S.serve.1.
//
//shed:proves S.serve.9
func TestStatusPainterLineAfterABackoff(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	change := "kzkzkzkzkzkz"
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.OpenUnit(change, "Proposal from the painter", unit.Painter); err != nil {
		t.Fatal(err)
	}
	s, err := tr.StartSession(change, unit.Painter, "propose", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.FinishSession(s.ID, tracker.Succeeded, "nothing", 0.3, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.Archive(change, unit.Deferred, unit.Shed, "done with it"); err != nil {
		t.Fatal(err)
	}
	tr.Close()

	got := painterLine(t, r.Dir)
	if !strings.HasPrefix(got, "painter: the last proposal went nowhere, so the next is due at ") ||
		!strings.Contains(got, "(a 15m0s wait, doubling from painter.interval up to painter.max_interval)") {
		t.Errorf("status painter line after a backoff = %q", got)
	}
}

// TestStatusPainterLineWhenTheGapIsEmpty checks that once the horizon clause
// the painter may still work on is realised, shed status reports the
// painter idle, word for word as shed serve -once reports it.
//
//shed:proves S.serve.9
func TestStatusPainterLineWhenTheGapIsEmpty(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	change := openUnit(t, r.Dir, "Realise goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	realised := strings.Replace(testrepo.Horizon, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1)
	if err := os.WriteFile(filepath.Join(dir, "horizon.md"), []byte(realised), 0o644); err != nil {
		t.Fatal(err)
	}
	seal(t, state, change)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
	}
	mustRun(t, r.Dir, "land", change)

	want := "painter: the gap holds no near or soon horizon clause that no unit in flight advances"
	if got := painterLine(t, r.Dir); got != want {
		t.Errorf("status painter line with an empty gap = %q, want %q", got, want)
	}
}

// TestStatusPainterLineDuringTheBudgetPause checks that while S.serve.6
// pauses stages, shed status reports the painter waiting for the budget
// pause even though nothing else holds it back.
//
//shed:proves S.serve.9
func TestStatusPainterLineDuringTheBudgetPause(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write(".shed/config.toml", "[budget]\nper_day_usd = 1\n")
	state := filepath.Join(r.Dir, DefaultStateDir)

	change := openUnit(t, r.Dir, "Costly review")
	seal(t, state, change)
	mustRun(t, r.Dir, "unit", "move", change, "implementing", "by hand")

	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := tr.StartSession(change, unit.Mechanic, "proofs", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.FinishSession(s.ID, tracker.Succeeded, "done", 1.5, true); err != nil {
		t.Fatal(err)
	}
	tr.Close()

	want := "painter: waits, like every stage, for the budget pause"
	if got := painterLine(t, r.Dir); got != want {
		t.Errorf("status painter line while paused = %q, want %q", got, want)
	}
}
