package cli

import (
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// answer takes a contested unit out of contested on the owner's answer: a
// retry moves it to proposed, a defer archives it on the deferred shelf and
// a reject archives it on the rejected shelf (S.owner.4, S.owner.5,
// S.owner.8). A refused answer records nothing (S.owner.6).
func (e env) answer(args []string) int {
	if len(args) < 2 {
		return e.misuse("answer needs a unit, retry, defer or reject, and a reason")
	}
	kind, reason := args[1], strings.TrimSpace(strings.Join(args[2:], " "))
	if kind != tracker.Retry && kind != tracker.Defer && kind != tracker.Reject {
		return e.misuse("an answer is %s, %s or %s, not %q", tracker.Retry, tracker.Defer, tracker.Reject, kind)
	}
	if reason == "" {
		return e.misuse("answer needs a reason")
	}
	if kind == tracker.Retry {
		return e.withTracker(func(t *tracker.Tracker) int {
			u, err := t.Unit(args[0])
			if err != nil {
				return e.fail(err)
			}
			if err := t.Retry(u.Change, reason); err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.stdout, "retried %s; bounces %d, now proposed\n", unit.Short(u.Change), u.Bounces)
			return OK
		})
	}
	return e.withFactory(func(f *factory.Factory) int {
		u, err := f.Tracker.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		if kind == tracker.Reject {
			if err := f.Reject(e.ctx, u.Change, reason); err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.stdout, "rejected %s to the rejected shelf\n", unit.Short(u.Change))
			return OK
		}
		if err := f.Defer(e.ctx, u.Change, reason); err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "deferred %s to the deferred shelf\n", unit.Short(u.Change))
		return OK
	})
}
