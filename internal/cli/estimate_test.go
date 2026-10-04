package cli

import (
	"path/filepath"
	"reflect"
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

// TestStatusShowsEstimateAndCostSinceSeal checks the ESTIMATE column shed
// status adds beside COST: it shows the estimate recorded at a unit's most
// recent seal next to the cost of the sessions that finished since the seal
// that set that estimate. A seal that only carries the estimate forward
// (S.impl.7) leaves that cost where it was; any other seal starts it afresh.
// The column stays empty for a unit that was never sealed, or whose most
// recent seal recorded no estimate, while COST keeps totaling every
// session's cost regardless.
//
//shed:proves S.impl.8
func TestStatusShowsEstimateAndCostSinceSeal(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	unsealed := openUnit(t, r.Dir, "Say goodbye")
	legacy := openUnit(t, r.Dir, "Shrug")
	fresh := openUnit(t, r.Dir, "Wave")
	carried := openUnit(t, r.Dir, "Nod")
	reset := openUnit(t, r.Dir, "Bow")

	tr, err := tracker.Open(state, tracker.Options{BounceThreshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	cost := func(change string, amount float64) {
		t.Helper()
		s, err := tr.StartSession(change, unit.Mechanic, "implement", 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.FinishSession(s.ID, tracker.Succeeded, "done", amount, true); err != nil {
			t.Fatal(err)
		}
	}

	// Never sealed: ESTIMATE is empty, COST still totals its sessions.
	cost(unsealed, 0.75)

	// Sealed with no estimate recorded, as a seal made before seals
	// recorded estimates (S.impl.7): ESTIMATE stays empty.
	if err := tr.Seal(legacy, "main1", "legacycommit", tracker.Footprint{}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	cost(legacy, 0.40)

	// Sealed with an estimate and no sessions finished since: the cost
	// since the seal is $0.00.
	if err := tr.Seal(fresh, "main1", "wavecommit", tracker.Footprint{Estimate: 50}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}

	// Sealed with an estimate, costed, then reopened and resealed out of
	// the amendment lane (S.shed.11) with the estimate carried forward
	// (S.impl.7): the cost since the seal that set the estimate keeps
	// adding across the carried-forward seal instead of starting afresh.
	if err := tr.Seal(carried, "main1", "nodcommit1", tracker.Footprint{Estimate: 1000}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	cost(carried, 1.20)
	if err := tr.Reopen(carried, unit.Mechanic, "the mechanic requested an amendment:\nmore detail.", true); err != nil {
		t.Fatal(err)
	}
	if err := tr.Seal(carried, "main2", "nodcommit2", tracker.Footprint{Estimate: 1000}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	cost(carried, 2.00)

	// Sealed with an estimate, costed, then reopened (not for an
	// amendment) and resealed with a newly declared estimate: the seal
	// sets the estimate again, so the cost since it starts afresh
	// (S.impl.7).
	if err := tr.Seal(reset, "main1", "bowcommit1", tracker.Footprint{Estimate: 300}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	cost(reset, 5.00)
	if err := tr.Reopen(reset, unit.Owner, "needs more work", false); err != nil {
		t.Fatal(err)
	}
	if err := tr.Seal(reset, "main2", "bowcommit2", tracker.Footprint{Estimate: 600}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	cost(reset, 0.50)
	tr.Close()

	want := []string{
		"UNIT STATE BOUNCES AMENDMENTS COST ESTIMATE WAIT OVERDUE LAND TITLE",
		unit.Short(unsealed) + " proposed 0 0 $0.75 Say goodbye",
		unit.Short(legacy) + " sealed 0 0 $0.40 Shrug",
		unit.Short(fresh) + " sealed 0 0 $0.00 $0.00 of $50.00 Wave",
		unit.Short(carried) + " sealed 1 1 $3.20 $3.20 of $1000.00 Nod",
		unit.Short(reset) + " sealed 1 0 $5.50 $0.50 of $600.00 Bow",
		"painter: 1 proposals are waiting in the shed (painter.max_proposed = 1)",
		"amendments: 0 auto-accepted, 0 sampled (0 agreed, 0 disagreed, 0 unanswered)",
	}
	if got := collapsed(mustRun(t, r.Dir, "status")); !reflect.DeepEqual(got, want) {
		t.Errorf("status =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
