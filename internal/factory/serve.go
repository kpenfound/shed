package factory

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// ServeOptions tune Serve.
type ServeOptions struct {
	// Once returns as soon as no stage is running and no controller has
	// anything to start, instead of waiting for more work.
	Once bool
	// Log receives a line for every stage that ends.
	Log io.Writer
	// Now returns the time; nil is time.Now.
	Now func() time.Time
}

// controller starts the stages of one role. It reads the tracker's current
// state on every pass and returns what it started.
type controller struct {
	name  string
	start func(ctx context.Context, s *scheduler, units []tracker.Unit) error
}

// scheduler tracks the stages running in the background.
type scheduler struct {
	f    *Factory
	opts ServeOptions
	wake chan struct{}

	mu      sync.Mutex
	busy    map[string]bool
	started int
	// reviews counts the horizon reviews running; failed counts each
	// unit's reviews that reported no outcome in this serve.
	reviews int
	failed  map[string]int
	errs    []error
	wg      sync.WaitGroup
}

func (s *scheduler) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *scheduler) isBusy(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy[key]
}

// run starts a stage in the background under a key no other running stage
// holds. The controller does not wait for it: when it ends it records its
// outcome and wakes every controller.
func (s *scheduler) run(ctx context.Context, key, label string, fn func(context.Context) (string, error)) {
	s.mu.Lock()
	if s.busy[key] {
		s.mu.Unlock()
		return
	}
	s.busy[key] = true
	s.started++
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		out, err := fn(ctx)
		s.mu.Lock()
		delete(s.busy, key)
		if err != nil {
			s.errs = append(s.errs, fmt.Errorf("%s: %w", label, err))
		}
		s.mu.Unlock()
		if s.opts.Log != nil {
			if err != nil {
				fmt.Fprintf(s.opts.Log, "%s: error: %v\n", label, err)
			} else {
				fmt.Fprintf(s.opts.Log, "%s: %s\n", label, out)
			}
		}
		s.notify()
	}()
}

// reviewing reports whether a horizon review is running.
func (s *scheduler) reviewing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reviews > 0
}

// review starts the horizon review of a unit whose key no running stage
// holds (S.queue.5). A review that reports no outcome is tried again, up to
// maxStepFailures times in one serve.
func (s *scheduler) review(ctx context.Context, change string) {
	key := unit.Short(change)
	s.mu.Lock()
	skip := s.busy[key] || s.failed[change] >= maxStepFailures
	if !skip {
		s.reviews++
	}
	s.mu.Unlock()
	if skip {
		return
	}
	s.run(ctx, key, key+" review", func(ctx context.Context) (string, error) {
		out, err := s.f.ReviewHorizon(ctx, change)
		s.mu.Lock()
		s.reviews--
		if err == nil && out == Failed {
			s.failed[change]++
		}
		s.mu.Unlock()
		return string(out), err
	})
}

const (
	painterKey = "painter"
	landerKey  = "lander"
	debateKey  = "debate"
	// frameBuilderKey is shared by startExpiry and startFraming, since no
	// expiry session ever runs alongside another expiry session or a
	// framing session, in either order (S.frame.8).
	frameBuilderKey = "frame-builder"
)

// controllers are in the order each pass runs them: downstream first, so
// that work in flight finishes before new work starts.
func (f *Factory) controllers() []controller {
	return []controller{
		{name: "wheelbuilder", start: func(ctx context.Context, s *scheduler, units []tracker.Unit) error {
			// Horizon reviews start before the landing, and nothing lands
			// while one runs.
			for _, u := range units {
				if u.Review && unit.PastSeal(u.State) {
					s.review(ctx, u.Change)
				}
			}
			if s.reviewing() {
				return nil
			}
			held := HeldBack(units)
			for _, u := range oldestFirst(units, unit.Queued) {
				if s.isBusy(landerKey) {
					return nil
				}
				if u.Review || len(held[u.Change]) > 0 {
					continue
				}
				change := u.Change
				s.run(ctx, landerKey, unit.Short(change)+" land", func(ctx context.Context) (string, error) {
					out, err := f.Land(ctx, change)
					return string(out), err
				})
			}
			return nil
		}},
		{name: "verifier", start: func(ctx context.Context, s *scheduler, units []tracker.Unit) error {
			for _, u := range oldestFirst(units, unit.Verifying) {
				if u.Review {
					continue
				}
				change := u.Change
				s.run(ctx, unit.Short(change), unit.Short(change)+" verify", func(ctx context.Context) (string, error) {
					out, err := f.Verify(ctx, change)
					return string(out), err
				})
			}
			return nil
		}},
		{name: "mechanic", start: func(ctx context.Context, s *scheduler, units []tracker.Unit) error {
			working := 0
			for _, u := range units {
				if u.State == unit.Implementing || u.State == unit.Verifying {
					working++
				}
			}
			for _, u := range oldestFirst(units, unit.Implementing, unit.Sealed) {
				if u.Review {
					continue
				}
				if u.State == unit.Sealed {
					if working >= f.Operator.Concurrency.Units {
						continue
					}
					working++
				}
				change := u.Change
				s.run(ctx, unit.Short(change), unit.Short(change)+" implement", func(ctx context.Context) (string, error) {
					out, err := f.Implement(ctx, change)
					return string(out), err
				})
			}
			return nil
		}},
		{name: "shed", start: func(ctx context.Context, s *scheduler, units []tracker.Unit) error {
			// One debate at a time; a proposal that declares no horizon
			// clause, or no estimate, is still a draft.
			if s.isBusy(debateKey) {
				return nil
			}
			for _, u := range oldestFirst(units, unit.Proposed) {
				if len(u.Footprint.Advances) == 0 || u.Footprint.Estimate <= 0 {
					continue
				}
				if full, err := f.inFlightFull(u.Change); err != nil || full {
					return err
				}
				change := u.Change
				s.run(ctx, debateKey, unit.Short(change)+" debate", func(ctx context.Context) (string, error) {
					out, err := f.Debate(ctx, change)
					return string(out), err
				})
				return nil
			}
			return nil
		}},
		{name: "painter", start: func(ctx context.Context, s *scheduler, units []tracker.Unit) error {
			if s.isBusy(painterKey) {
				return nil
			}
			// A painter's draft no session is working on was left by a
			// crash: it holds nothing worth keeping.
			for _, u := range oldestFirst(units, unit.Proposed) {
				if u.Title == painterDraft && len(u.Footprint.Advances) == 0 {
					if err := f.Tracker.Archive(u.Change, unit.Deferred, unit.Shed, "the painter stopped before declaring its proposal"); err != nil {
						return err
					}
					if err := f.Repo.Discard(ctx, u.Change); err != nil {
						return err
					}
				}
			}
			due, err := f.PainterDue(ctx, s.now())
			if err != nil || !due {
				return err
			}
			s.run(ctx, painterKey, "painter", func(ctx context.Context) (string, error) {
				change, out, err := f.Propose(ctx)
				if change != "" {
					return fmt.Sprintf("%s %s", unit.Short(change), out), err
				}
				return string(out), err
			})
			return nil
		}},
		{name: "expiry", start: f.startExpiry},
		{name: "frame-builder", start: f.startFraming},
	}
}

func (s *scheduler) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

// oldestFirst returns the units in the given states, oldest first.
func oldestFirst(units []tracker.Unit, states ...unit.State) []tracker.Unit {
	var out []tracker.Unit
	for _, u := range units {
		for _, s := range states {
			if u.State == s {
				out = append(out, u)
			}
		}
	}
	return out
}

// Paused reports why dispatch is paused, if it is: the sessions of the last
// 24 hours cost the daily budget or more.
func (f *Factory) Paused(now time.Time) (string, error) {
	budget := f.Operator.Budget.PerDayUSD
	if budget <= 0 {
		return "", nil
	}
	spent, err := f.Tracker.Spend(now.Add(-24 * time.Hour))
	if err != nil {
		return "", err
	}
	if spent >= budget {
		return fmt.Sprintf("the daily budget is spent: $%.2f of $%.2f in the last 24 hours", spent, budget), nil
	}
	return "", nil
}

// Serve runs the factory: one controller per role, each starting the
// stages its units need, until ctx ends. Controllers act on the tracker's
// current state on every pass; a stage that ends wakes them, and so does
// serve.tick. Nothing new starts while the daily budget is spent.
func (f *Factory) Serve(ctx context.Context, opts ServeOptions) error {
	s := &scheduler{f: f, opts: opts, wake: make(chan struct{}, 1), busy: map[string]bool{}, failed: map[string]int{}}
	defer s.wg.Wait()
	startedAny := false
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tick := f.Operator.Serve.Tick.Duration
	for {
		s.mu.Lock()
		s.started = 0
		failed := len(s.errs) > 0
		s.mu.Unlock()
		if opts.Once && failed {
			cancel()
			s.wg.Wait()
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.errs[0]
		}
		paused, err := f.Paused(s.now())
		if err != nil {
			return err
		}
		if paused == "" {
			if err := f.pass(ctx, s); err != nil {
				// A controller cut short by the end of ctx has not failed.
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
		} else if opts.Log != nil {
			fmt.Fprintf(opts.Log, "paused: %s\n", paused)
		}
		s.mu.Lock()
		quiet := len(s.busy) == 0 && (s.started == 0 || paused != "")
		startedAny = startedAny || s.started > 0
		s.mu.Unlock()
		if opts.Once && quiet {
			if !startedAny && paused == "" && opts.Log != nil {
				f.explainIdle(ctx, s.now(), opts.Log)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wake:
		case <-time.After(tick):
		}
	}
}

// explainIdle says why a serve that started nothing had nothing to start.
func (f *Factory) explainIdle(ctx context.Context, now time.Time, w io.Writer) {
	fmt.Fprintln(w, "nothing to start")
	units, err := f.Tracker.Units()
	if err == nil {
		for _, u := range units {
			if u.State == unit.Proposed && len(u.Footprint.Advances) == 0 && u.OpenedBy != unit.FrameBuilder {
				fmt.Fprintf(w, "  %s is a draft: declare the horizon clauses it advances with shed unit declare\n", unit.Short(u.Change))
			}
			if u.State == unit.Proposed && u.OpenedBy == unit.FrameBuilder {
				fmt.Fprintf(w, "  %s waits for owner acceptance: shed frame -accept %s\n", unit.Short(u.Change), unit.Short(u.Change))
			}
			if u.State == unit.Contested {
				fmt.Fprintf(w, "  %s is contested and waits for the owner\n", unit.Short(u.Change))
			}
		}
	}
	if full, err := f.inFlightFull(""); err == nil && full {
		fmt.Fprintf(w, "  shed: %d units are in flight (concurrency.in_flight)\n", f.Operator.Concurrency.InFlight)
	}
	if why, err := f.PainterWait(ctx, now); err != nil {
		fmt.Fprintf(w, "  painter: %v\n", err)
	} else if why != "" {
		fmt.Fprintf(w, "  painter: %s\n", why)
	}
}

// pass runs every controller once, downstream first.
func (f *Factory) pass(ctx context.Context, s *scheduler) error {
	for _, c := range f.controllers() {
		units, err := f.Tracker.Units()
		if err != nil {
			return err
		}
		if err := c.start(ctx, s, units); err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
	}
	return nil
}
