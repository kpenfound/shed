package factory

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// wantOverrunReason checks that a reopen reason names the cost since the
// seal and the estimate, the amounts in USD to the cent, and the multiple
// (S.impl.9).
func wantOverrunReason(t *testing.T, reason string, cost, estimate float64) {
	t.Helper()
	for _, want := range []string{fmt.Sprintf("$%.2f", cost), fmt.Sprintf("$%.2f", estimate), "multiple"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reopen reason %q lacks %q", reason, want)
		}
	}
}

// wantReopenEvent checks that a unit's latest event is a reopen by shed,
// counting a bounce and requesting no amendment (S.impl.9, S.unit.5).
func wantReopenEvent(t *testing.T, f *Factory, change string) {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	last := events[len(events)-1]
	if last.Actor != unit.Shed || last.To != unit.Proposed || !last.Bounce || last.Amendment {
		t.Errorf("the overrun's reopen event = %+v, want shed, to proposed, a bounce and no amendment", last)
	}
}

//shed:proves S.impl.9
func TestOverrunStopsTheFormulaAndReopens(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n")
	change := sealed(t, f, fake) // estimate $100 (propose)

	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 150}
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		t.Error("the implement step ran after the unit overran its estimate")
		write(t, turn.Dir, "bye.go", byeCode)
		return done("done")
	})

	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("implement = %s, want reopened", out)
	}

	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 || u.Amendments != 0 {
		t.Fatalf("after the overrun: %+v", u)
	}
	wantOverrunReason(t, u.Reason, 150, 100)
	wantReopenEvent(t, f, change)

	// The triggering session's own result was still recorded as usual: it
	// is not failed or discarded, and its work was captured onto the
	// change (S.impl.9: "records that session's result ... as usual").
	sessions, err := f.Tracker.Sessions(change)
	must(t, err)
	last := sessions[len(sessions)-1]
	if last.Step != "proofs" || last.Status != tracker.Succeeded || last.CostUSD != 150 {
		t.Errorf("the triggering session = %+v", last)
	}
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	if _, err := os.Stat(dir + "/bye_test.go"); err != nil {
		t.Errorf("the proofs step's work was not captured onto the change: %v", err)
	}
}

//shed:proves S.impl.9
func TestOverrunStartsNoSessionOnAnAlreadyOverrunUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n")
	change := sealed(t, f, fake) // estimate $100 (propose)

	// A session finished, before this call, with a cost that already
	// overruns the estimate: as after a restart, or a lowered multiple,
	// shed finds the unit overrunning with no session running.
	s, err := f.Tracker.StartSession(change, unit.Mechanic, "proofs", 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Succeeded, "done", 150, true))

	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("implement = %s, want reopened", out)
	}
	if n := len(fake.ran(unit.Mechanic)); n != 0 {
		t.Errorf("implement started %d mechanic sessions on an overrunning unit", n)
	}

	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 {
		t.Fatalf("after the overrun: %+v", u)
	}
	wantOverrunReason(t, u.Reason, 150, 100)
	wantReopenEvent(t, f, change)
}

//shed:proves S.impl.9
func TestServeCatchesAnOverrunOnItsNextPass(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\nper_day_usd = 0\n")
	// A harmless painter, in case the gap ever looks open; the unit below
	// still advances H.greet.2, so the gap should stay empty and the
	// painter should not be asked to propose.
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	change := sealed(t, f, fake) // estimate $100 (propose)
	must(t, f.Tracker.Move(change, unit.Implementing, unit.Shed, "a mechanic is dispatched"))
	s, err := f.Tracker.StartSession(change, unit.Mechanic, "proofs", 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Succeeded, "done", 150, true))

	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))

	if n := len(fake.ran(unit.Mechanic)); n != 0 {
		t.Errorf("serve started %d mechanic sessions on an overrunning unit:\n%s", n, log.String())
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 {
		t.Fatalf("serve did not reopen the overrunning unit it found with no session running: %+v\nlog:\n%s", u, log.String())
	}
	wantOverrunReason(t, u.Reason, 150, 100)
	wantReopenEvent(t, f, change)
}

//shed:proves S.impl.9
func TestZeroOverrunMultipleTurnsOverrunOff(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 0\n")
	change := sealed(t, f, fake) // estimate $100 (propose)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 100000}
	})

	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Implemented {
		t.Fatalf("implement = %s, want implemented despite the huge cost: a multiple of zero turns overruns off", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Verifying || u.Bounces != 0 {
		t.Errorf("a multiple of zero still overran: %+v", u)
	}
}

//shed:proves S.impl.9
func TestNoEstimateNeverOverruns(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n")
	change := openGoodbye(t, f)
	main, err := f.mainSet(ctx)
	must(t, err)
	// A legacy seal, recorded before seals recorded estimates (S.impl.7),
	// has no estimate: the unit never overruns, however much it costs.
	legacy := tracker.Footprint{Modifies: []string{"S.core.2"}, Depends: []string{"S.core.1"}, Advances: []string{"H.greet.2"}}
	must(t, f.Tracker.Seal(change, "legacymain", "legacycommit", legacy, unit.Committee, "consensus", onMain(main)))

	mechanic(t, fake)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 100000}
	})

	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Implemented {
		t.Fatalf("implement = %s, want implemented: a unit whose most recent seal recorded no estimate never overruns", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Bounces != 0 {
		t.Errorf("a no-estimate unit overran: %+v", u)
	}
}

//shed:proves S.impl.9 S.unit.6
func TestRepeatedOverrunsCanContestAUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n[shed]\nbounce_threshold = 1\n")
	change := sealed(t, f, fake) // estimate $100 (propose), registers a clean committee debate

	overrun := func() Outcome {
		t.Helper()
		fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
			write(t, turn.Dir, "bye_test.go", byeProof)
			return session.Result{Status: outcomeDone, CostUSD: 150}
		})
		out, err := f.Implement(ctx, change)
		must(t, err)
		return out
	}

	if out := overrun(); out != Reopened {
		t.Fatalf("the first overrun = %s, want reopened", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 1 {
		t.Fatalf("after the first overrun: %+v", u)
	}

	must(t, f.Declare(ctx, change, "", u.Footprint.Depends, u.Footprint.Advances, unit.Painter, 100))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("resealing after the first overrun = %s, %v", out, err)
	}

	if out := overrun(); out != Contested {
		t.Fatalf("the second overrun = %s, want contested", out)
	}
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Contested || u.Bounces != 2 {
		t.Fatalf("after the second overrun: %+v", u)
	}
	notices, err := f.Tracker.Notices(unit.Owner, true)
	must(t, err)
	if len(notices) != 1 || notices[0].Unit != change || notices[0].Kind != "contested" {
		t.Errorf("owner notices = %+v", notices)
	}
}
