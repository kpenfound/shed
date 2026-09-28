package landing

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

//shed:proves S.vcs.7
func TestMessage(t *testing.T) {
	u := tracker.Unit{
		Change: "qpvuntsmwlqtqpvuntsmwlqt", Title: "Say goodbye",
		Seal:      &tracker.Seal{Main: "abc123", Change: "qpvuntsmwlqtqpvuntsmwlqt"},
		Footprint: tracker.Footprint{Advances: []string{"H.greet.2", "H.greet.3"}},
	}
	diff := docs.SpecDiff{
		Added:   []clause.ID{clause.MustParseID("S.greet.2")},
		Changed: []clause.ID{clause.MustParseID("S.greet.1")},
		Removed: []clause.ID{clause.MustParseID("S.greet.9")},
	}
	want := `Say goodbye

Spec:
  added   S.greet.2
  changed S.greet.1
  removed S.greet.9
Advances: H.greet.2, H.greet.3

Unit: qpvuntsmwlqtqpvuntsmwlqt
Sealed-Against: abc123
`
	if got := Message(u, diff); got != want {
		t.Errorf("message =\n%s\nwant\n%s", got, want)
	}
	u.Footprint.Advances = nil
	if got := Message(u, docs.SpecDiff{}); !strings.HasPrefix(got, "Say goodbye\n\nSpec: unchanged\n\nUnit: ") {
		t.Errorf("message without spec changes =\n%s", got)
	}
}

//shed:proves S.vcs.7
func TestLandRecordsTheLanding(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, ".shed")
	repo, err := vcs.Open(ctx, r.Dir, state, vcs.Options{Remote: "origin",
		Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	must(t, err)
	tr, err := tracker.Open(state, tracker.Options{})
	must(t, err)
	defer tr.Close()
	main, err := repo.MainCommit(ctx)
	must(t, err)

	change, err := repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	must(t, tr.OpenUnit(change, "Say goodbye", unit.Painter))
	dir, err := repo.Workspace(ctx, change)
	must(t, err)
	spec := testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"
	must(t, os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(spec), 0o644))

	if _, err := Land(ctx, tr, repo, change, unit.Wheelbuilder); err == nil || !strings.Contains(err.Error(), "only queued units land") {
		t.Errorf("landing a proposed unit: %v", err)
	}
	must(t, tr.Seal(change, main, "unitcommit", tracker.Footprint{Modifies: []string{"S.core.2"}, Advances: []string{"H.greet.2"}},
		unit.Committee, "consensus", nil))
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, tr.Move(change, s, unit.Mechanic, "next"))
	}

	commit, err := Land(ctx, tr, repo, change, unit.Wheelbuilder)
	must(t, err)
	u, err := tr.Unit(change)
	must(t, err)
	if u.State != unit.Landed || u.Landed != commit {
		t.Errorf("unit after landing = %s at %q, want landed at %s", u.State, u.Landed, commit)
	}
	msg := r.Git("log", "-1", "--format=%B", commit)
	for _, want := range []string{"Say goodbye", "added   S.core.2", "Advances: H.greet.2", "Unit: " + change, "Sealed-Against: " + main} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if got := r.GitRemote("rev-parse", "main"); got != commit {
		t.Errorf("remote main = %s, want %s", got, commit)
	}
}

//shed:proves S.vcs.7
func TestInterruptedLandingCompletes(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, ".shed")
	repo, err := vcs.Open(ctx, r.Dir, state, vcs.Options{Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	must(t, err)
	tr, err := tracker.Open(state, tracker.Options{})
	must(t, err)
	defer tr.Close()
	change, err := repo.NewUnit(ctx, "Wave")
	must(t, err)
	must(t, tr.OpenUnit(change, "Wave", unit.Painter))
	dir, err := repo.Workspace(ctx, change)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, "wave.txt"), []byte("wave\n"), 0o644))
	must(t, tr.Seal(change, "main", "unitcommit", tracker.Footprint{}, unit.Committee, "consensus", nil))
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, tr.Move(change, s, unit.Mechanic, "next"))
	}

	// Main moved, but shed stopped before the tracker recorded it.
	landed, err := repo.Land(ctx, change, func(context.Context, string, string) (string, error) { return "Wave", nil })
	must(t, err)
	commit, err := Land(ctx, tr, repo, change, unit.Wheelbuilder)
	must(t, err)
	if commit != landed {
		t.Errorf("recorded %s, landed %s", commit, landed)
	}
	if u, _ := tr.Unit(change); u.State != unit.Landed {
		t.Errorf("unit is %s", u.State)
	}
}

// queuedUnit opens a unit whose change adds S.core.2 and S.core.4 to the
// spec, and seals and queues it with a footprint predicting S.core.2 and
// S.core.3.
func queuedUnit(t *testing.T, tr *tracker.Tracker, repo *vcs.Repo, main string) (string, tracker.Footprint) {
	t.Helper()
	change, err := repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	must(t, tr.OpenUnit(change, "Say goodbye", unit.Painter))
	dir, err := repo.Workspace(ctx, change)
	must(t, err)
	spec := testrepo.Spec +
		"- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n" +
		"- **S.core.4** (H.greet.2) Running the tool with --wave prints a wave.\n"
	must(t, os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(spec), 0o644))
	sealed := tracker.Footprint{Modifies: []string{"S.core.2", "S.core.3"}, Depends: []string{"S.core.1"}, Advances: []string{"H.greet.2"}}
	must(t, tr.Seal(change, main, "unitcommit", sealed, unit.Committee, "consensus", nil))
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, tr.Move(change, s, unit.Mechanic, "next"))
	}
	return change, sealed
}

// checkActual checks a landed unit's sealed and actual footprints and the
// drift its landing reports.
func checkActual(t *testing.T, tr *tracker.Tracker, change string, sealed tracker.Footprint) {
	t.Helper()
	u, err := tr.Unit(change)
	must(t, err)
	want := tracker.Footprint{Modifies: []string{"S.core.2", "S.core.4"}, Depends: []string{"S.core.1"}, Advances: []string{"H.greet.2"}}
	if u.Actual == nil || !reflect.DeepEqual(*u.Actual, want) {
		t.Errorf("actual footprint = %+v, want %+v", u.Actual, want)
	}
	if !reflect.DeepEqual(u.Footprint, sealed) {
		t.Errorf("sealed footprint = %+v, want %+v", u.Footprint, sealed)
	}
	drift := "footprint drifted: not sealed S.core.4; not modified S.core.3"
	if u.Actual != nil {
		if got := tracker.FootprintDrift(u.Footprint, *u.Actual).String(); got != drift {
			t.Errorf("drift = %q, want %q", got, drift)
		}
	}
	events, err := tr.Events(change)
	must(t, err)
	if last := tracker.Describe(events[len(events)-1]); !strings.Contains(last, drift) {
		t.Errorf("landing event = %q, want it to report %q", last, drift)
	}
}

//shed:proves S.fp.3 S.fp.4
func TestLandingRecordsTheActualFootprint(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, ".shed")
	repo, err := vcs.Open(ctx, r.Dir, state, vcs.Options{Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	must(t, err)
	tr, err := tracker.Open(state, tracker.Options{})
	must(t, err)
	defer tr.Close()
	main, err := repo.MainCommit(ctx)
	must(t, err)
	change, sealed := queuedUnit(t, tr, repo, main)

	_, err = Land(ctx, tr, repo, change, unit.Wheelbuilder)
	must(t, err)
	checkActual(t, tr, change, sealed)
	must(t, tr.Rebuild())
	checkActual(t, tr, change, sealed)
}

//shed:proves S.fp.3 S.fp.4
func TestLandingAUnitOnMainRecordsTheActualFootprint(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, ".shed")
	repo, err := vcs.Open(ctx, r.Dir, state, vcs.Options{Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	must(t, err)
	tr, err := tracker.Open(state, tracker.Options{})
	must(t, err)
	defer tr.Close()
	main, err := repo.MainCommit(ctx)
	must(t, err)
	change, sealed := queuedUnit(t, tr, repo, main)

	// Main moved, but shed stopped before the tracker recorded it.
	landed, err := repo.Land(ctx, change, func(context.Context, string, string) (string, error) { return "Say goodbye", nil })
	must(t, err)
	commit, err := Land(ctx, tr, repo, change, unit.Wheelbuilder)
	must(t, err)
	if commit != landed {
		t.Errorf("recorded %s, landed %s", commit, landed)
	}
	checkActual(t, tr, change, sealed)
}
