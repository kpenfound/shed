package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

//shed:proves S.impl.6
func TestUnitDeclareEstimate(t *testing.T) {
	r := testrepo.Colocated(t)
	change := openUnit(t, r.Dir, "Say goodbye")
	state := filepath.Join(r.Dir, DefaultStateDir)

	for _, bad := range []string{"0", "-50"} {
		if _, stderr, code := run(t, r.Dir, "unit", "declare", "-estimate", bad, change); code == OK {
			t.Errorf("declared the estimate %s: %s", bad, stderr)
		}
	}

	mustRun(t, r.Dir, "unit", "declare", "-estimate", "1500", change)
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	u, err := tr.Unit(change)
	if err != nil {
		t.Fatal(err)
	}
	if u.Footprint.Estimate != 1500 {
		t.Fatalf("estimate = %v, want 1500", u.Footprint.Estimate)
	}

	// A later declare that omits -estimate preserves it, like any other
	// field (S.shed.5).
	mustRun(t, r.Dir, "unit", "declare", "-title", "Say goodbye, revised", change)
	u, err = tr.Unit(change)
	if err != nil {
		t.Fatal(err)
	}
	if u.Footprint.Estimate != 1500 {
		t.Errorf("estimate after an omitted -estimate = %v, want preserved at 1500", u.Footprint.Estimate)
	}
}

//shed:proves S.impl.7
func TestUnitLogShowsTheEstimate(t *testing.T) {
	r := testrepo.Colocated(t)
	change := openUnit(t, r.Dir, "Say goodbye")
	state := filepath.Join(r.Dir, DefaultStateDir)
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Seal(change, "main1", "unitcommit", tracker.Footprint{Estimate: 1500}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	tr.Close()

	out := mustRun(t, r.Dir, "unit", "log", change[:8])
	if !strings.Contains(out, "estimate $1500.00") {
		t.Errorf("unit log lacks the estimate on the seal's line:\n%s", out)
	}
}
