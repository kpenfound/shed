package factory

import (
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// openGoodbye opens a unit whose change adds the goodbye clause, as a
// painter would, but declares nothing yet.
func openGoodbye(t *testing.T, f *Factory) string {
	t.Helper()
	change, err := f.Repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Say goodbye", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", goodbyeSpec)
	return change
}

//shed:proves S.impl.6
func TestDeclareEstimate(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := openGoodbye(t, f)

	// The painter's declare refuses a declaration that leaves the unit with
	// no estimate, naming the missing field.
	if err := f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter); err == nil ||
		!strings.Contains(err.Error(), "estimate") {
		t.Fatalf("the painter declared with no estimate: %v", err)
	}

	// Both the owner and the painter refuse an amount that is not a
	// positive number.
	for _, actor := range []unit.Actor{unit.Owner, unit.Painter} {
		for _, bad := range []float64{0, -5} {
			if err := f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, actor, bad); err == nil {
				t.Errorf("%s declared the estimate %v", actor, bad)
			}
		}
	}

	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, 1500))
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1500 {
		t.Fatalf("estimate = %v, want 1500", u.Footprint.Estimate)
	}

	// Like any other field, an estimate omitted from a later declare is
	// preserved (S.shed.5).
	must(t, f.Declare(ctx, change, "Say goodbye politely", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Owner))
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1500 {
		t.Errorf("estimate after an omitted redeclare = %v, want preserved at 1500", u.Footprint.Estimate)
	}
}

//shed:proves S.impl.6
func TestProposeDeclareRequiresAnEstimate(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[painter]\ninterval = \"0s\"\n")
	declareArgs := func(estimate any) map[string]any {
		args := map[string]any{"title": "Say goodbye", "summary": "Add --bye.",
			"depends": []string{"S.core.1"}, "advances": []string{"H.greet.2"}}
		if estimate != nil {
			args["estimate"] = estimate
		}
		return args
	}
	fake.on(unit.Painter, "propose", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "spec/core.md", goodbyeSpec)
		if _, err := call(t, turn, "declare", declareArgs(nil)); err == nil {
			t.Error("declared with no estimate")
		}
		if _, err := call(t, turn, "declare", declareArgs(0)); err == nil {
			t.Error("declared a zero estimate")
		}
		_, err := call(t, turn, "declare", declareArgs(500))
		must(t, err)
		return done(outcomeProposed)
	})
	change, out, err := f.Propose(ctx)
	must(t, err)
	if out != Outcome(outcomeProposed) {
		t.Fatalf("propose = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 500 {
		t.Errorf("estimate = %v, want 500", u.Footprint.Estimate)
	}
}

//shed:proves S.impl.6
func TestDraftWithNoEstimateIsNotDebated(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := openGoodbye(t, f)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Owner))

	if _, err := f.Debate(ctx, change); err == nil ||
		!strings.Contains(err.Error(), unit.Short(change)) || !strings.Contains(err.Error(), "estimate") {
		t.Fatalf("debating a draft with no estimate = %v, want an error naming the unit and the missing estimate", err)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed || u.Round != 0 {
		t.Errorf("a draft with no estimate was debated: %+v", u)
	}
}

//shed:proves S.impl.7
func TestEstimateDoesNotAffectDebateOutcome(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	// Two proposals that differ only in which positive estimate they
	// record, and whose debates receive the same (here, no) objections,
	// answers and withdrawals, move the same way.
	for _, estimate := range []float64{100, 100000} {
		change := openGoodbye(t, f)
		must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, estimate))
		out, err := f.Debate(ctx, change)
		must(t, err)
		if out != Sealed {
			t.Errorf("estimate %v: debate = %s, want sealed", estimate, out)
		}
	}
}

//shed:proves S.impl.7
func TestAmendmentSealKeepsThePreviousEstimate(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	change := openGoodbye(t, f)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, 1000))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("first debate = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1000 {
		t.Fatalf("sealed estimate = %v, want 1000", u.Footprint.Estimate)
	}

	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nmore detail.", true))
	u, err = f.Tracker.Unit(change)
	must(t, err)
	must(t, f.Declare(ctx, change, "", u.Footprint.Depends, u.Footprint.Advances, unit.Painter, 9000))

	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("amendment debate = %s, %v", out, err)
	}
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1000 {
		t.Errorf("amendment seal estimate = %v, want the previous seal's 1000, not the mid-amendment redeclare 9000", u.Footprint.Estimate)
	}
}

//shed:proves S.impl.6 S.impl.7
func TestAmendmentSealAdoptsTheEstimateOfALegacyUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	change := openGoodbye(t, f)
	main, err := f.mainSet(ctx)
	must(t, err)
	// A legacy seal, recorded before seals recorded estimates, has none.
	legacy := tracker.Footprint{Modifies: []string{"S.core.2"}, Depends: []string{"S.core.1"}, Advances: []string{"H.greet.2"}}
	must(t, f.Tracker.Seal(change, "legacymain", "legacycommit", legacy, unit.Committee, "consensus", onMain(main)))

	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nmore detail.", true))

	// The unit is a draft again until the painter declares a positive
	// estimate: the legacy seal recorded none.
	if _, err := f.Debate(ctx, change); err == nil || !strings.Contains(err.Error(), "estimate") {
		t.Fatalf("debating a legacy amendment with no estimate = %v", err)
	}

	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, 1000))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("first amendment debate = %s, %v", out, err)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1000 {
		t.Fatalf("adopted estimate = %v, want the unit's estimate at sealing, 1000", u.Footprint.Estimate)
	}

	// A later amendment seal, with no redeclare, keeps the adopted estimate.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested another amendment:\nstill more.", true))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("second amendment debate = %s, %v", out, err)
	}
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1000 {
		t.Errorf("later amendment seal estimate = %v, want the adopted 1000 kept", u.Footprint.Estimate)
	}
}

//shed:proves S.impl.7
func TestRejectedAmendmentKeepsThePreviousEstimate(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")

	change := openGoodbye(t, f)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, 1000))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("first debate = %s, %v", out, err)
	}

	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nmore detail.", true))
	u, err := f.Tracker.Unit(change)
	must(t, err)
	must(t, f.Declare(ctx, change, "", u.Footprint.Depends, u.Footprint.Advances, unit.Painter, 9000))

	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) != 1 {
			return done("clean")
		}
		_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "not detailed enough"})
		must(t, err)
		return done("objecting")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("the rejected amendment = %s, %v: %+v", out, err, u)
	}
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 1000 {
		t.Errorf("the rejected amendment's seal estimate = %v, want the previous seal's 1000, not the mid-amendment redeclare 9000", u.Footprint.Estimate)
	}
}
