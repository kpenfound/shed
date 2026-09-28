package landing

import (
	"context"
	"os"
	"path/filepath"
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
	must(t, tr.Seal(change, main, tracker.Footprint{Modifies: []string{"S.core.2"}, Advances: []string{"H.greet.2"}},
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
	must(t, tr.Seal(change, "main", tracker.Footprint{}, unit.Committee, "consensus", nil))
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
