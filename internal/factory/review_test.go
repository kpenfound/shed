package factory

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// farewell rewords the goodbye clause of the test horizon.
var farewell = strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon) The tool waves farewell.", 1)

// withFile writes a file on a unit's change.
func withFile(t *testing.T, f *Factory, change, name, content string) {
	t.Helper()
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, name, content)
	_, err = f.Repo.Snapshot(ctx, change)
	must(t, err)
}

// reviewer plays a wheelbuilder that reviews a unit's horizon notices and
// reports status with note. It checks what every review session is given.
func reviewer(t *testing.T, fake *fakeRunner, status, note string, carries ...string) {
	fake.on(unit.Wheelbuilder, "review", func(turn session.Turn) session.Result {
		if turn.Step != "review" {
			t.Errorf("the review session's step is %q", turn.Step)
		}
		for _, w := range carries {
			if !strings.Contains(turn.Bundle, w) {
				t.Errorf("the review bundle lacks %q:\n%s", w, turn.Bundle)
			}
		}
		if len(turn.Notices) != 0 {
			t.Errorf("the review session delivers notices %v", turn.Notices)
		}
		if got := slices.Sorted(slices.Values(turn.Outcomes)); !slices.Equal(got, []string{"consistent", "reopen"}) {
			t.Errorf("the review's done accepts %v", turn.Outcomes)
		}
		if turn.Check == nil {
			t.Error("the review's done accepts an outcome without a reason")
		} else {
			for _, o := range []string{"consistent", "reopen"} {
				if turn.Check(o, " ") == nil {
					t.Errorf("the review's done accepts %s without a reason", o)
				}
				if err := turn.Check(o, "a reason"); err != nil {
					t.Errorf("the review's done refuses %s with a reason: %v", o, err)
				}
			}
		}
		return session.Result{Status: status, Note: note, CostUSD: 0.1}
	})
}

// reviewsOf returns the wheelbuilder's review sessions of a unit.
func reviewsOf(fake *fakeRunner, change string) []session.Turn {
	var out []session.Turn
	for _, turn := range fake.ran(unit.Wheelbuilder) {
		if turn.Unit == change && turn.Step == "review" {
			out = append(out, turn)
		}
	}
	return out
}

// logs reports whether one of events describes itself, in shed unit log,
// with every one of want.
func logs(events []tracker.Event, want ...string) bool {
	for _, e := range events {
		line := string(e.Actor) + " " + tracker.Describe(e)
		ok := true
		for _, w := range want {
			ok = ok && strings.Contains(line, w)
		}
		if ok {
			return true
		}
	}
	return false
}

//shed:proves S.queue.5
func TestHorizonTextChangesMarkUnitsForReview(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	reworded := advancing(t, f, "Reworded", unit.Queued, "H.greet.2")
	withFile(t, f, reworded, "reworded.txt", "reworded\n")
	retagged := advancing(t, f, "Retagged", unit.Queued, "H.greet.3")
	withFile(t, f, retagged, "retagged.txt", "retagged\n")
	counts := map[string]int{reworded: eventCount(t, f, reworded), retagged: eventCount(t, f, retagged)}

	// The landing rewords H.greet.2. It moves H.greet.3 to soon and only
	// respaces its text.
	horizon := strings.NewReplacer(
		"(soon) The tool says goodbye.", "(soon) The tool waves farewell.",
		"(distant) The tool greets in any language, within C2.", "(soon) The tool  greets in   any language, within C2.",
	).Replace(testHorizon)
	landHorizon(t, f, horizon)
	for _, c := range []string{reworded, retagged} {
		if n := noticesIn(eventsSince(t, f, c, counts[c])); len(n) != 1 {
			t.Fatalf("unit %s got notices %+v, want one", unit.Short(c), n)
		}
	}

	// The reworded unit is marked: shed land refuses it and it stays queued.
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	if out, err := f.Land(ctx, reworded); err == nil {
		t.Errorf("landing a unit marked for horizon review = %s", out)
	}
	if u, _ := f.Tracker.Unit(reworded); u.State != unit.Queued || u.Bounces != 0 {
		t.Errorf("the marked unit is %s with %d bounces, want queued", u.State, u.Bounces)
	}
	if now, _ := f.Repo.MainCommit(ctx); now != main {
		t.Error("main moved when shed land refused a marked unit")
	}

	// A change to the tag list alone marks nothing.
	if out, err := f.Land(ctx, retagged); err != nil || out != Landed {
		t.Errorf("landing the retagged unit = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Wheelbuilder)); n != 0 {
		t.Errorf("the wheelbuilder ran %d sessions", n)
	}
}

//shed:proves S.queue.5
func TestHorizonReviewFindsUnitConsistent(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	mechanic(t, fake)

	bye := sealed(t, f, fake)
	before, _ := f.Tracker.Unit(bye)
	short := unit.Short(landHorizon(t, f, farewell))
	count := eventCount(t, f, bye)

	// While it is marked, shed starts no implementation.
	if out, err := f.Implement(ctx, bye); err == nil {
		t.Errorf("implementing a unit marked for horizon review = %s", out)
	}
	if n := len(fake.ran(unit.Mechanic)); n != 0 {
		t.Fatalf("the mechanic ran %d sessions on a marked unit", n)
	}

	const reason = "Waving farewell still says goodbye."
	reviewer(t, fake, "consistent", reason, short, "H.greet.2", "The tool says goodbye.", "The tool waves farewell.")
	out, err := f.ReviewHorizon(ctx, bye)
	must(t, err)
	if out != Consistent {
		t.Fatalf("review = %s, want %s", out, Consistent)
	}
	if n := len(reviewsOf(fake, bye)); n != 1 {
		t.Fatalf("the wheelbuilder ran %d reviews, want one", n)
	}
	after, _ := f.Tracker.Unit(bye)
	if after.State != unit.Sealed || after.Bounces != before.Bounces || !reflect.DeepEqual(after.Seal, before.Seal) {
		t.Errorf("after a consistent review the unit is %s with %d bounces and seal %+v, want sealed with %d and %+v",
			after.State, after.Bounces, after.Seal, before.Bounces, before.Seal)
	}
	events := eventsSince(t, f, bye, count)
	if !logs(events, "consistent", reason) {
		t.Errorf("the unit log does not record the consistent review with its reason: %+v", events)
	}

	// The notices stay pending, with the reason, for the mechanic, and the
	// cleared mark lets the unit be implemented without another review.
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		for _, w := range []string{short, "The tool waves farewell.", reason} {
			if !strings.Contains(turn.Bundle, w) {
				t.Errorf("the mechanic's bundle lacks %q:\n%s", w, turn.Bundle)
			}
		}
		write(t, turn.Dir, "bye_test.go", byeProof)
		return done("done")
	})
	must2(t, f.Implement)(bye)
	if n := len(reviewsOf(fake, bye)); n != 1 {
		t.Errorf("the wheelbuilder ran %d reviews, want one", n)
	}
}

//shed:proves S.queue.5
func TestHorizonReviewReopensUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	queued := advancing(t, f, "Queued", unit.Queued, "H.greet.2")
	withFile(t, f, queued, "spec/core.md", goodbyeSpec)
	short := unit.Short(landHorizon(t, f, farewell))
	count := eventCount(t, f, queued)

	// A session that reports neither outcome leaves the mark.
	fake.on(unit.Wheelbuilder, "review", func(session.Turn) session.Result { return session.Result{CostUSD: 0.1} })
	if _, err := f.ReviewHorizon(ctx, queued); err != nil {
		t.Fatal(err)
	}
	if u, _ := f.Tracker.Unit(queued); u.State != unit.Queued || u.Bounces != 0 {
		t.Errorf("after a review with no outcome the unit is %s with %d bounces", u.State, u.Bounces)
	}
	if out, err := f.Land(ctx, queued); err == nil {
		t.Errorf("landing a unit whose review reported nothing = %s", out)
	}

	const reason = "Farewell is not what the unit's goodbye clause promises."
	reviewer(t, fake, "reopen", reason, short, "The tool waves farewell.")
	out, err := f.ReviewHorizon(ctx, queued)
	must(t, err)
	if out != Reopened {
		t.Fatalf("review = %s, want %s", out, Reopened)
	}
	u, _ := f.Tracker.Unit(queued)
	if u.State != unit.Proposed || u.Bounces != 1 || !strings.Contains(u.Reason, reason) {
		t.Errorf("after a reopening review the unit is %s with %d bounces, reason %q", u.State, u.Bounces, u.Reason)
	}
	events := eventsSince(t, f, queued, count)
	var reopen *tracker.Event
	for i, e := range events {
		if e.Kind == tracker.UnitMoved && e.To == unit.Proposed {
			reopen = &events[i]
		}
	}
	if reopen == nil || reopen.Actor != unit.Wheelbuilder || !reopen.Bounce || !strings.Contains(reopen.Reason, reason) {
		t.Errorf("reopen event = %+v, want a bounce by the wheelbuilder with its reason", reopen)
	}
	if !logs(events, string(unit.Wheelbuilder), reason) {
		t.Errorf("the unit log does not record the reopening review: %+v", events)
	}

	// The reopen cleared the mark: sealed again, the unit goes to its
	// mechanic without another review.
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	if out, err := f.Debate(ctx, queued); err != nil || out != Sealed {
		t.Fatalf("debate = %s, %v", out, err)
	}
	fake.on(unit.Mechanic, "", func(session.Turn) session.Result { return done("done") })
	if _, err := f.Implement(ctx, queued); err != nil {
		t.Errorf("implementing the resealed unit: %v", err)
	}
	if n := len(fake.ran(unit.Mechanic)); n == 0 {
		t.Error("the resealed unit ran no mechanic session")
	}
	if n := len(reviewsOf(fake, queued)); n != 2 {
		t.Errorf("the wheelbuilder ran %d reviews, want two", n)
	}
}

//shed:proves S.queue.5
func TestLeavingTheSealClearsTheReviewMark(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	verifying := advancing(t, f, "Verifying", unit.Verifying, "H.greet.2")
	landHorizon(t, f, farewell)
	if out, err := f.Verify(ctx, verifying); err == nil {
		t.Errorf("verifying a unit marked for horizon review = %s", out)
	}

	// The owner reopens it; sealed again, it is implemented without review.
	must(t, f.Tracker.Reopen(verifying, unit.Owner, "rethink", false))
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, f.Tracker.Seal(verifying, main, "unitcommit", tracker.Footprint{Advances: []string{"H.greet.2"}}, unit.Committee, "consensus", nil))
	fake.on(unit.Mechanic, "", func(session.Turn) session.Result { return done("done") })
	if _, err := f.Implement(ctx, verifying); err != nil {
		t.Errorf("implementing the resealed unit: %v", err)
	}
	if n := len(fake.ran(unit.Mechanic)); n == 0 {
		t.Error("the resealed unit ran no mechanic session")
	}
	if n := len(fake.ran(unit.Wheelbuilder)); n != 0 {
		t.Errorf("the wheelbuilder ran %d sessions", n)
	}
}

//shed:proves S.queue.5
func TestServeReviewsBeforeLanding(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	mechanic(t, fake)
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })

	// The goodbye unit is verifying when a landing rewords its clause.
	bye := sealed(t, f, fake)
	must2(t, f.Implement)(bye)
	short := unit.Short(landHorizon(t, f, farewell))
	// Another unit waits to land.
	queued := advancing(t, f, "Queued", unit.Queued, "H.greet.1")
	withFile(t, f, queued, "queued.txt", "queued\n")

	reviewer(t, fake, "consistent", "Farewell is goodbye.", short)
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	must(t, f.Serve(ctx, ServeOptions{Once: true}))

	for _, c := range []string{bye, queued} {
		if u, _ := f.Tracker.Unit(c); u.State != unit.Landed {
			t.Errorf("unit %s is %s, want landed", unit.Short(c), u.State)
		}
	}
	if n := len(reviewsOf(fake, bye)); n != 1 {
		t.Fatalf("the wheelbuilder ran %d reviews, want one", n)
	}

	// The review started before the other unit landed, and the goodbye
	// unit was verified only after its review.
	seq := func(change string, match func(tracker.Event) bool) int64 {
		t.Helper()
		events, err := f.Tracker.Events(change)
		must(t, err)
		for _, e := range events {
			if match(e) {
				return e.Seq
			}
		}
		t.Fatalf("no matching event for unit %s", unit.Short(change))
		return 0
	}
	review := seq(bye, func(e tracker.Event) bool {
		return e.Kind == tracker.SessionStarted && e.Session.Role == unit.Wheelbuilder && e.Session.Step == "review"
	})
	verified := seq(bye, func(e tracker.Event) bool {
		return e.Kind == tracker.SessionStarted && e.Session.Role == unit.Committee && e.Session.Step == "review"
	})
	landed := seq(queued, func(e tracker.Event) bool { return e.Kind == tracker.UnitMoved && e.To == unit.Landed })
	if review > landed {
		t.Errorf("the review started at event %d, after the other unit landed at %d", review, landed)
	}
	if verified < review {
		t.Errorf("the marked unit's verification started at event %d, before its review at %d", verified, review)
	}
}
