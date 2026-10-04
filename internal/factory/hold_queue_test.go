package factory

import (
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// queuedWithFootprint opens a unit, seals it with the given footprint and
// moves it through to queued, giving it a file so it is not empty once
// rebased (S.queue.2).
func queuedWithFootprint(t *testing.T, f *Factory, title string, fp tracker.Footprint) string {
	t.Helper()
	change, err := f.Repo.NewUnit(ctx, title)
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, title, unit.Painter))
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, f.Tracker.Seal(change, main, "unitcommit", fp, unit.Committee, "consensus", nil))
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, f.Tracker.Move(change, s, unit.Mechanic, "next"))
	}
	withFile(t, f, change, title+".txt", title+"\n")
	return change
}

// TestServeSkipsAHeldBackUnit checks that shed serve does not land a queued
// unit held back under S.queue.9 — one entangled with another queued unit
// whose spec footprint holds fewer distinct clauses — while that other unit
// stays queued, and that it lands the units around the held back unit,
// keeping its place, the way it already does for a unit marked for horizon
// review (S.queue.7).
//
//shed:proves S.queue.9
func TestServeSkipsAHeldBackUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	blocker := queuedWithFootprint(t, f, "Blocker", tracker.Footprint{Modifies: []string{"S.core.1"}})
	held := queuedWithFootprint(t, f, "Held", tracker.Footprint{Modifies: []string{"S.core.1", "S.core.2"}})
	independent := queuedWithFootprint(t, f, "Independent", tracker.Footprint{Modifies: []string{"S.other.1"}})

	if _, err := f.Tracker.AddReviewNotice(blocker, unit.Shed, "horizon", "reworded", unit.Shed, true); err != nil {
		t.Fatal(err)
	}
	// The blocker's review reports no outcome every time it is tried, so
	// its mark never clears and it stays queued for the whole pass.
	fake.on(unit.Wheelbuilder, "review", func(session.Turn) session.Result { return session.Result{CostUSD: 0.1} })
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	must(t, f.Serve(ctx, ServeOptions{Once: true}))

	if u, _ := f.Tracker.Unit(independent); u.State != unit.Landed {
		t.Errorf("the independent unit is %s, want landed", u.State)
	}
	u, err := f.Tracker.Unit(held)
	must(t, err)
	if u.State != unit.Queued || u.Review {
		t.Errorf("the held back unit is %+v, want still queued and unmarked", u)
	}
	bu, err := f.Tracker.Unit(blocker)
	must(t, err)
	if bu.State != unit.Queued || !bu.Review {
		t.Errorf("the blocker is %+v, want still queued and marked for review", bu)
	}

	if out, err := f.Land(ctx, held); err == nil {
		t.Fatalf("landing a held back unit = %s", out)
	} else if !strings.Contains(err.Error(), unit.Short(blocker)) {
		t.Errorf("the refusal = %v, want it to name %s", err, unit.Short(blocker))
	}
}

// TestLandRefusesHeldBackUnits checks that shed land refuses a queued unit
// held back under S.queue.9, naming every unit it waits behind, moving no
// unit and recording no event, that a tie in the number of distinct spec
// clauses is broken by the landing order of S.queue.7, and that a unit
// stops waiting behind another once that unit lands.
//
//shed:proves S.queue.9
func TestLandRefusesHeldBackUnits(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	// a and b share a clause and are both sized two; a opened first, so b
	// waits behind it. c shares a clause with each and is sized three, so
	// it waits behind both, since it is larger than each.
	a := queuedWithFootprint(t, f, "A", tracker.Footprint{Modifies: []string{"S.shared.1", "S.a.1"}})
	b := queuedWithFootprint(t, f, "B", tracker.Footprint{Modifies: []string{"S.shared.1", "S.b.1"}})
	c := queuedWithFootprint(t, f, "C", tracker.Footprint{Modifies: []string{"S.shared.1", "S.a.1", "S.b.1"}})

	events := map[string]int{}
	for _, change := range []string{a, b, c} {
		es, err := f.Tracker.Events(change)
		must(t, err)
		events[change] = len(es)
	}
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)

	for _, held := range []string{b, c} {
		out, err := f.Land(ctx, held)
		if err == nil {
			t.Errorf("landing %s = %s, want it held back behind %s", unit.Short(held), out, unit.Short(a))
			continue
		}
		if !strings.Contains(err.Error(), unit.Short(a)) {
			t.Errorf("refusing %s = %v, want it to name %s", unit.Short(held), err, unit.Short(a))
		}
	}
	if out, err := f.Land(ctx, c); err == nil || !strings.Contains(err.Error(), unit.Short(b)) {
		t.Errorf("refusing %s = %v, want it to also name %s", unit.Short(c), out, unit.Short(b))
	}

	if now, _ := f.Repo.MainCommit(ctx); now != main {
		t.Error("main moved when shed land refused held back units")
	}
	for _, change := range []string{a, b, c} {
		u, err := f.Tracker.Unit(change)
		must(t, err)
		if u.State != unit.Queued || u.Bounces != 0 {
			t.Errorf("unit %s = %+v, want still queued with no bounce", unit.Short(change), u)
		}
		es, err := f.Tracker.Events(change)
		must(t, err)
		if len(es) != events[change] {
			t.Errorf("unit %s recorded %d events from a refused land, want %d: being held back records no event", unit.Short(change), len(es), events[change])
		}
	}

	// Once a lands, b is no longer held back; once b also lands, neither
	// is left for c to wait behind.
	if out, err := f.Land(ctx, a); err != nil || out != Landed {
		t.Fatalf("landing %s = %s, %v", unit.Short(a), out, err)
	}
	if out, err := f.Land(ctx, b); err != nil || out != Landed {
		t.Fatalf("landing %s once its blocker left queued = %s, %v", unit.Short(b), out, err)
	}
	if out, err := f.Land(ctx, c); err != nil || out != Landed {
		t.Fatalf("landing %s once every unit it waited behind left queued = %s, %v", unit.Short(c), out, err)
	}
}

// TestEntanglementCountsOnlyQueuedUnits checks that a queued unit is not
// held back under S.queue.9 by an entangled unit that is not itself queued:
// only queued units count.
//
//shed:proves S.queue.9
func TestEntanglementCountsOnlyQueuedUnits(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	implementing, err := f.Repo.NewUnit(ctx, "Implementing")
	must(t, err)
	must(t, f.Tracker.OpenUnit(implementing, "Implementing", unit.Painter))
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, f.Tracker.Seal(implementing, main, "unitcommit", tracker.Footprint{Modifies: []string{"S.shared.1"}}, unit.Committee, "consensus", nil))
	must(t, f.Tracker.Move(implementing, unit.Implementing, unit.Mechanic, "next"))

	queued := queuedWithFootprint(t, f, "Queued", tracker.Footprint{Modifies: []string{"S.shared.1", "S.shared.2"}})
	if out, err := f.Land(ctx, queued); err != nil || out != Landed {
		t.Errorf("landing a unit entangled only with a non-queued unit = %s, %v, want landed", out, err)
	}
}
