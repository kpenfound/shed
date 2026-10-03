package factory

import (
	"bytes"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// contestAt opens a tracker directly on the factory's state directory with
// a clock fixed at at, seals change and reopens it, so the bounce that
// makes it contested (bounce_threshold 0) is recorded as happening at at.
// It runs alongside the factory's own open tracker, as a crash recovery or
// a second shed process would.
func contestAt(t *testing.T, f *Factory, change string, at time.Time) {
	t.Helper()
	tr, err := tracker.Open(f.State, tracker.Options{Now: func() time.Time { return at }})
	must(t, err)
	defer tr.Close()
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, tr.Seal(change, main, "unitcommit", tracker.Footprint{}, unit.Committee, "consensus", nil))
	must(t, tr.Reopen(change, unit.Owner, "the spec is wrong", false))
}

//shed:proves S.frame.8
func TestServeExpiresOverdueContestedUnitsOldestFirst(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n")
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })

	oldest := propose(t, f)
	contestAt(t, f, oldest, time.Now().Add(-3*time.Hour))
	middle := propose(t, f)
	contestAt(t, f, middle, time.Now().Add(-90*time.Minute))
	fresh := propose(t, f)
	contestAt(t, f, fresh, time.Now().Add(-10*time.Minute))

	var order []string
	fake.on(unit.FrameBuilder, frameExpireStep, func(turn session.Turn) session.Result {
		fields := strings.Fields(turn.Prompt)
		order = append(order, fields[1])
		return session.Result{Status: "keep", CostUSD: 0.1}
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))

	if !slices.Equal(order, []string{unit.Short(oldest), unit.Short(middle)}) {
		t.Fatalf("expiry order = %v, want the two overdue units oldest first", order)
	}
	for _, change := range []string{oldest, middle, fresh} {
		if u, _ := f.Tracker.Unit(change); u.State != unit.Contested {
			t.Errorf("unit %s = %s, want contested (kept, or never due)", unit.Short(change), u.State)
		}
	}
	if !strings.Contains(log.String(), unit.Short(oldest)) || !strings.Contains(log.String(), "kept") {
		t.Errorf("log lacks the kept outcome for %s:\n%s", unit.Short(oldest), log.String())
	}

	// A zero timeout turns expiry off, however overdue a unit is.
	order = nil
	f.Operator.Shed.ContestedTimeout.Duration = 0
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if len(order) != 0 {
		t.Errorf("a zero timeout still dispatched expiry: %v", order)
	}
}

//shed:proves S.frame.8
func TestServeExpiresEachContestedEpisodeAtMostOnce(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n")
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	change := propose(t, f)
	contestAt(t, f, change, time.Now().Add(-2*time.Hour))

	// A session that ends without an outcome still counts the attempt
	// (S.frame.8), as a crash between the claim and the session would.
	ran := 0
	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		ran++
		return session.Result{CostUSD: 0.1}
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if ran != 1 {
		t.Fatalf("ran = %d, want 1", ran)
	}
	if !strings.Contains(log.String(), unit.Short(change)) || !strings.Contains(log.String(), "outcome") {
		t.Errorf("log does not say the session ended without an outcome: %s", log.String())
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Contested {
		t.Errorf("unit = %s, want still contested", u.State)
	}

	// The same contest episode is not retried, even after a tracker
	// rebuild or a restart.
	must(t, f.Tracker.Rebuild())
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if ran != 1 {
		t.Fatalf("retried after a rebuild: ran = %d", ran)
	}
	must(t, f.Close())

	fake2 := newFake(t)
	fake2.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	fake2.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		ran++
		return session.Result{CostUSD: 0.1}
	})
	f2, err := Open(ctx, f.Root, f.State, fake2)
	must(t, err)
	defer f2.Close()
	must(t, f2.Serve(ctx, ServeOptions{Once: true}))
	if ran != 1 {
		t.Fatalf("retried after a restart: ran = %d", ran)
	}

	// Leaving contested and moving to contested again gives the unit a new
	// pair, so it is attempted again.
	must(t, f2.Tracker.Retry(change, "the owner wants another look"))
	contestAt(t, f2, change, time.Now().Add(-2*time.Hour))
	must(t, f2.Serve(ctx, ServeOptions{Once: true}))
	if ran != 2 {
		t.Fatalf("a unit contested again was not retried: ran = %d", ran)
	}
}

//shed:proves S.frame.8 S.serve.4
func TestServeExpiryRunsBeforeFramingAndNeverAlongsideIt(t *testing.T) {
	r := projectWith(t, map[string]string{"horizon.md": exhaustedHorizon(), "spec/core.md": goodbyeSpec})
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n")
	change := propose(t, f)
	contestAt(t, f, change, time.Now().Add(-2*time.Hour))

	var mu sync.Mutex
	var order []string
	active, maxActive := 0, 0
	track := func(name string) func() {
		mu.Lock()
		order = append(order, name)
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		return func() {
			mu.Lock()
			active--
			mu.Unlock()
		}
	}
	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		defer track("expire")()
		return session.Result{Status: "keep", CostUSD: 0.1}
	})
	fake.on(unit.FrameBuilder, frameStep, func(session.Turn) session.Result {
		defer track("frame")()
		return done("nothing")
	})
	must(t, f.Serve(ctx, ServeOptions{Once: true}))

	if !slices.Equal(order, []string{"expire", "frame"}) {
		t.Fatalf("order = %v, want the expiry session to start before framing", order)
	}
	if maxActive > 1 {
		t.Fatalf("an expiry session ran alongside a framing session")
	}
}

//shed:proves S.frame.8 S.serve.6
func TestServeExpirySkipsWhileTheDailyBudgetIsSpent(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n[budget]\nper_day_usd = 1\n")
	change := propose(t, f)
	contestAt(t, f, change, time.Now().Add(-2*time.Hour))
	s, err := f.Tracker.StartSession(change, unit.Painter, "earlier", 1)
	must(t, err)
	must(t, f.Tracker.FinishSession(s.ID, tracker.Failed, "infrastructure: rate limited", 1.5, false))

	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		t.Error("an expiry session started while the daily budget was spent")
		return session.Result{Status: "keep", CostUSD: 0.1}
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if !strings.Contains(log.String(), "paused: the daily budget is spent") {
		t.Errorf("log lacks the pause: %s", log.String())
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Contested {
		t.Errorf("an overdue unit moved while the budget was paused: %s", u.State)
	}
}

//shed:proves S.frame.8
func TestServeExpiryArchivesAndLogsTheShelf(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n")
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })
	change := propose(t, f)
	contestAt(t, f, change, time.Now().Add(-2*time.Hour))

	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		return session.Result{Status: "rejected", Note: "breaks C2", CostUSD: 0.2}
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))

	u, _ := f.Tracker.Unit(change)
	if u.State != unit.Archived || u.Shelf != unit.Rejected {
		t.Fatalf("unit = %+v, want archived on the rejected shelf", u)
	}
	ev := lastMoveOf(t, f, change)
	if !ev.Expired {
		t.Errorf("the move does not record that the unit expired: %+v", ev)
	}
	if !strings.Contains(log.String(), unit.Short(change)) || !strings.Contains(log.String(), string(unit.Rejected)) {
		t.Errorf("log lacks the archived outcome and its shelf: %s", log.String())
	}
}

//shed:proves S.frame.8
func TestServeExpiryLeavesAUnitThatChangedMeanwhileAlone(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nbounce_threshold = 0\ncontested_timeout = \"1h\"\n")
	fake.on(unit.Painter, "propose", func(session.Turn) session.Result { return done("nothing") })

	left := propose(t, f)
	contestAt(t, f, left, time.Now().Add(-2*time.Hour))
	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		must(t, f.Tracker.Retry(left, "the owner got there first"))
		return session.Result{Status: "rejected", Note: "breaks C2", CostUSD: 0.1}
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if u, _ := f.Tracker.Unit(left); u.State != unit.Proposed {
		t.Errorf("unit = %s, want proposed: the owner's retry stands", u.State)
	}
	if !strings.Contains(log.String(), unit.Short(left)) || !strings.Contains(log.String(), "left contested") {
		t.Errorf("log does not say the unit left contested meanwhile: %s", log.String())
	}

	again := propose(t, f)
	contestAt(t, f, again, time.Now().Add(-2*time.Hour))
	fake.on(unit.FrameBuilder, frameExpireStep, func(session.Turn) session.Result {
		must(t, f.Tracker.Retry(again, "try again"))
		must(t, f.Tracker.Move(again, unit.Contested, unit.Committee, "bounced again, meanwhile"))
		return session.Result{Status: "rejected", Note: "breaks C2", CostUSD: 0.1}
	})
	log.Reset()
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	u, _ := f.Tracker.Unit(again)
	if u.State != unit.Contested || u.Reason != "bounced again, meanwhile" {
		t.Errorf("unit = %+v, want contested with the newer reason", u)
	}
	if !strings.Contains(log.String(), unit.Short(again)) || !strings.Contains(log.String(), "contested again") {
		t.Errorf("log does not say the unit moved to contested again meanwhile: %s", log.String())
	}
}
