package factory

import (
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// landedAt returns the sequence number of a unit's landing event.
func landedAt(t *testing.T, f *Factory, change string) int64 {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	for _, e := range events {
		if e.Kind == tracker.UnitMoved && e.To == unit.Landed {
			return e.Seq
		}
	}
	t.Fatalf("unit %s never landed", unit.Short(change))
	return 0
}

// TestServeLandsAroundAMarkedUnit checks that the landing order of S.queue.7
// is the order the queued units opened, that shed serve's landing (S.serve.4)
// lands the first unit in that order not marked for horizon review, and
// that a marked unit keeps its place while the units behind it land before
// it.
//
//shed:proves S.queue.7
func TestServeLandsAroundAMarkedUnit(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	first := advancing(t, f, "First", unit.Queued, "H.greet.1")
	withFile(t, f, first, "first.txt", "first\n")
	second := advancing(t, f, "Second", unit.Queued, "H.greet.2")
	withFile(t, f, second, "second.txt", "second\n")
	third := advancing(t, f, "Third", unit.Queued, "H.greet.3")
	withFile(t, f, third, "third.txt", "third\n")

	if _, err := f.Tracker.AddReviewNotice(first, unit.Shed, "horizon", "reworded", unit.Shed, true); err != nil {
		t.Fatal(err)
	}

	// The marked unit's review reports no outcome every time it is tried,
	// so the mark never clears.
	fake.on(unit.Wheelbuilder, "review", func(session.Turn) session.Result { return session.Result{CostUSD: 0.1} })
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	must(t, f.Serve(ctx, ServeOptions{Once: true}))

	for _, c := range []string{second, third} {
		if u, _ := f.Tracker.Unit(c); u.State != unit.Landed {
			t.Errorf("unit %s is %s, want landed", unit.Short(c), u.State)
		}
	}
	u, err := f.Tracker.Unit(first)
	must(t, err)
	if u.State != unit.Queued || !u.Review {
		t.Errorf("the marked unit is %+v, want still queued and marked for review", u)
	}

	// The units behind it landed in the order they opened.
	if landedAt(t, f, second) > landedAt(t, f, third) {
		t.Error("the second unit landed after the third, not in the order they opened")
	}
}
