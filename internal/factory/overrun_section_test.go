package factory

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// finishedSessions returns the SessionFinished events a unit logged since
// the seal that set its current estimate (S.impl.8), in the order they
// appear in the event log (S.track.3): the sessions an overrun section
// lists. That seal is found the same way the tracker finds it (S.impl.7,
// S.impl.8): walking the log forward, a seal out of the amendment lane that
// carries forward an already-positive estimate leaves the boundary where it
// was; any other seal moves the boundary to itself.
func finishedSessions(t *testing.T, f *Factory, change string) []tracker.Event {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	boundary := -1
	inLane := false
	estimate := 0.0
	for i, e := range events {
		if e.Kind == tracker.UnitMoved && e.To == unit.Proposed && e.Bounce {
			inLane = e.Amendment
		}
		if e.Kind == tracker.UnitMoved && e.To == unit.Sealed && e.Seal != nil {
			if carriedForward := inLane && estimate > 0; !carriedForward {
				boundary = i
			}
			if e.Footprint != nil {
				estimate = e.Footprint.Estimate
			}
		}
	}
	var out []tracker.Event
	for _, e := range events[boundary+1:] {
		if e.Kind == tracker.SessionFinished {
			out = append(out, e)
		}
	}
	return out
}

// checkOverrunSection asserts that an Overrun section's body gives the
// estimate recorded at the unit's most recent seal and the cost since it as
// "$cost of $estimate" (S.impl.8's pattern), lists each given session once,
// in the order it finished, with its ID, outcome and cost, and never names
// the overrun multiple: that only the reopen's own reason gives (S.impl.9),
// and the section is built from the seal and the sessions alone.
func checkOverrunSection(t *testing.T, body, headline string, finished ...tracker.Event) {
	t.Helper()
	if !strings.Contains(body, headline) {
		t.Errorf("overrun section lacks %q:\n%s", headline, body)
	}
	if strings.Contains(body, "multiple") {
		t.Errorf("overrun section names the overrun multiple, which only the reopen's reason gives:\n%s", body)
	}
	last := -1
	for _, e := range finished {
		cost := fmt.Sprintf("$%.2f", e.CostUSD)
		if !strings.Contains(body, e.Session.ID) {
			t.Errorf("overrun section lacks session %s:\n%s", e.Session.ID, body)
			continue
		}
		if !strings.Contains(body, e.Session.Outcome) {
			t.Errorf("overrun section lacks session %s's outcome %q:\n%s", e.Session.ID, e.Session.Outcome, body)
		}
		if !strings.Contains(body, cost) {
			t.Errorf("overrun section lacks session %s's cost %s:\n%s", e.Session.ID, cost, body)
		}
		if i := strings.Index(body, e.Session.ID); i <= last {
			t.Errorf("session %s is not listed in the order it finished:\n%s", e.Session.ID, body)
		} else {
			last = i
		}
	}
}

// sealedWithEstimate proposes and seals the goodbye unit with a given
// estimate, through a clean committee debate.
func sealedWithEstimate(t *testing.T, f *Factory, fake *fakeRunner, estimate float64) string {
	t.Helper()
	change := propose(t, f)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter, estimate))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s, want sealed", out)
	}
	return change
}

// redeclare gives an overrun-reopened unit a fresh estimate, keeping its
// dependencies and horizon advances, so a controller may debate it again
// (S.impl.9: a unit with no estimate is a draft no controller debates).
func redeclare(t *testing.T, f *Factory, change string, estimate float64) {
	t.Helper()
	u, err := f.Tracker.Unit(change)
	must(t, err)
	must(t, f.Declare(ctx, change, "", u.Footprint.Depends, u.Footprint.Advances, unit.Painter, estimate))
}

// TestOverrunSectionShowsCostPerStepAndSession checks the overrun section a
// committee and painter debate bundle holds once a unit's latest reopen is
// the overrun reopen of S.impl.9: the estimate recorded at its most recent
// seal and the cost since that seal as it stood at the reopen, then every
// session that finished between the seal and the reopen, including retried
// attempts (S.sess.9) each on their own line, in the order they finished,
// and a subtotal for each formula step in the order of its first listed
// session. The figures stay the same across later rounds and in the
// painter's reply, since sessions that finish after the reopen add nothing
// to them.
//
//shed:proves S.impl.10
func TestOverrunSectionShowsCostPerStepAndSession(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n")
	change := sealedWithEstimate(t, f, fake, 100)

	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		if turn.Attempt == 0 {
			return session.Result{Failure: session.Infrastructure, Reason: "rate limited", CostUSD: 5}
		}
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 15}
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		if turn.Attempt == 0 {
			return session.Result{Failure: session.Infrastructure, Reason: "disk full", CostUSD: 30}
		}
		write(t, turn.Dir, "bye.go", byeCode)
		return session.Result{Status: outcomeDone, CostUSD: 60}
	})

	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("implement = %s, want reopened", out)
	}

	finished := finishedSessions(t, f, change)
	if len(finished) != 4 {
		t.Fatalf("finished sessions = %+v, want 2 proofs attempts and 2 implement attempts", finished)
	}

	redeclare(t, f, change, 50)

	const headline = "$110.00 of $100.00"
	var objection string
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		body := sectionBody(turn.Bundle, "Overrun")
		if body == "" {
			t.Fatalf("committee bundle lacks an Overrun section:\n%s", turn.Bundle)
		}
		checkOverrunSection(t, body, headline, finished...)
		if !strings.Contains(body, "proofs") || !strings.Contains(body, "implement") {
			t.Errorf("overrun section lacks a step name:\n%s", body)
		}
		if i, j := strings.Index(body, "$20.00"), strings.Index(body, "$90.00"); i < 0 || j < 0 || i > j {
			t.Errorf("overrun section lacks the proofs and implement subtotals, in step order:\n%s", body)
		}
		if !strings.Contains(body, "S.impl.6") || !strings.Contains(body, "S.shed.7") {
			t.Errorf("overrun section does not point to a revised estimate (S.impl.6) or a footprint split (S.shed.7):\n%s", body)
		}
		if member(turn) != 1 {
			return done("clean")
		}
		if strings.HasPrefix(turn.Step, "debate round 1") {
			res, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "say what goodbye prints"})
			must(t, err)
			objection = strings.Fields(res)[1]
			return done("objecting")
		}
		_, err := call(t, turn, "withdraw", map[string]any{"objection": objection, "reason": "answered"})
		must(t, err)
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
		body := sectionBody(turn.Bundle, "Overrun")
		if body == "" {
			t.Fatalf("painter's reply bundle lacks an Overrun section:\n%s", turn.Bundle)
		}
		checkOverrunSection(t, body, headline, finished...)
		_, err := call(t, turn, "answer", map[string]any{"objection": objection, "text": "it prints goodbye"})
		must(t, err)
		return done("replied")
	})

	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s, want sealed", out)
	}
}

// TestOverrunSectionSpansAnInterveningAmendmentSeal checks that the overrun
// section's estimate-since figure and listed sessions reach back to the
// seal that actually set the unit's estimate (S.impl.8), not merely the
// unit's most recent seal, when an amendment-lane reseal (S.impl.7) sits
// between the two. The amendment-lane seal carries the estimate forward
// from the one before it, so it does not start the cost-since-seal window
// afresh: a session that finished before the amendment-lane reseal still
// counts, and its seal is not where the section's listed sessions begin.
//
//shed:proves S.impl.10
func TestOverrunSectionSpansAnInterveningAmendmentSeal(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n[concurrency]\ncommittee = 1\n")
	change := sealedWithEstimate(t, f, fake, 100)

	// The mechanic asks for an amendment partway through the proofs step,
	// after a session that cost $30.
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		return session.Result{Status: outcomeAmend, Note: "clarify S.core.2 before proving it", CostUSD: 30}
	})
	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("implement = %s, want reopened for the amendment request", out)
	}

	// The amendment debate reseals, carrying the $100 estimate forward
	// (S.impl.7). Its own committee sessions cost nothing, so they do not
	// complicate the cost this test checks below.
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return session.Result{Status: "clean"} })
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("amendment debate = %s, want sealed", out)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.Footprint.Estimate != 100 {
		t.Fatalf("amendment seal estimate = %v, want the carried-forward 100", u.Footprint.Estimate)
	}

	// Implementing resumes the proofs step, which never finished, and this
	// time it finishes, costing $80: $110 since the original seal, past
	// the $100 estimate at the overrun multiple of 1.
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 80}
	})
	out, err = f.Implement(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("implement = %s, want reopened for the overrun", out)
	}

	finished := finishedSessions(t, f, change)
	if len(finished) != 3 {
		t.Fatalf("finished sessions = %+v, want the amend request, the amendment debate's committee session and the finished proofs session", finished)
	}
	redeclare(t, f, change, 50)

	const headline = "$110.00 of $100.00"
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		body := sectionBody(turn.Bundle, "Overrun")
		if body == "" {
			t.Fatalf("committee bundle lacks an Overrun section:\n%s", turn.Bundle)
		}
		checkOverrunSection(t, body, headline, finished...)
		return done("clean")
	})
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s, want sealed", out)
	}
}

// TestOverrunSectionSubtotalsAReviewSessionByRole checks that a session with
// no formula step, such as the committee's review of an implemented unit, is
// subtotaled by its role instead of a step, alongside the formula steps'
// subtotals.
//
//shed:proves S.impl.10
func TestOverrunSectionSubtotalsAReviewSessionByRole(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\noverrun_multiple = 1\n")
	change := sealedWithEstimate(t, f, fake, 1)

	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye_test.go", byeProof)
		return session.Result{Status: outcomeDone, CostUSD: 0.05}
	})
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye.go", byeCode)
		return session.Result{Status: outcomeDone, CostUSD: 0.08}
	})
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "README.md", "Run the tool with --bye to say goodbye.\n")
		return session.Result{Status: outcomeDone, CostUSD: 0.07}
	})
	out, err := f.Implement(ctx, change)
	must(t, err)
	if out != Implemented {
		t.Fatalf("implement = %s, want implemented", out)
	}

	fake.on(unit.Committee, "review", func(session.Turn) session.Result {
		return session.Result{Status: outcomePass, CostUSD: 1.00}
	})
	out, err = f.Verify(ctx, change)
	must(t, err)
	if out != Verified {
		t.Fatalf("verify = %s, want verified", out)
	}

	// Verify moves the unit on as usual even though the review session's
	// cost makes it overrun (S.impl.9); the next stage that checks, landing,
	// finds it overrunning with no session running and reopens it.
	out, err = f.Land(ctx, change)
	must(t, err)
	if out != Reopened {
		t.Fatalf("land = %s, want reopened", out)
	}

	finished := finishedSessions(t, f, change)
	if len(finished) != 4 {
		t.Fatalf("finished sessions = %+v, want proofs, implement, docs and the review", finished)
	}
	redeclare(t, f, change, 2)

	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		body := sectionBody(turn.Bundle, "Overrun")
		if body == "" {
			t.Fatalf("committee bundle lacks an Overrun section:\n%s", turn.Bundle)
		}
		checkOverrunSection(t, body, "$1.20 of $1.00", finished...)
		if n := strings.Count(strings.ToLower(body), "committee"); n < 2 {
			t.Errorf("overrun section does not subtotal the review session, which has no formula step, by its role (saw %q %d times):\n%s", "committee", n, body)
		}
		if strings.Contains(body, "review: $1.00") {
			t.Errorf("overrun section subtotals the review session as if \"review\" were a formula step:\n%s", body)
		}
		return done("clean")
	})
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s, want sealed", out)
	}
}

// TestNoOverrunSectionForAnOrdinaryReopen checks that a unit whose latest
// reopen is not the overrun reopen of S.impl.9, such as one the owner
// reopened by hand, has no Overrun section in its debate bundles.
//
//shed:proves S.impl.10
func TestNoOverrunSectionForAnOrdinaryReopen(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := sealed(t, f, fake)
	must(t, f.Tracker.Reopen(change, unit.Owner, "needs more detail", false))

	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if body := sectionBody(turn.Bundle, "Overrun"); body != "" {
			t.Errorf("a unit reopened for a reason other than an overrun holds an Overrun section:\n%s", body)
		}
		return done("clean")
	})
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s, want sealed", out)
	}
}
