package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// answer takes a contested unit out of contested on the owner's answer: a
// retry moves it to proposed, a defer archives it on the deferred shelf, a
// reject archives it on the rejected shelf and an approve moves a unit
// contested for the tier of its horizon amendment to proposed to be sealed
// (S.owner.4, S.owner.5, S.owner.8, S.shed.17). An agree or disagree answers
// a sampled amendment, recording whether the owner agreed, without moving
// the unit (S.owner.20). A first argument that parses as a charter citation
// names a charter clause, and the answer keeps it (S.owner.10). A refused
// answer records nothing (S.owner.6).
func (e env) answer(args []string) int {
	if len(args) < 2 {
		return e.misuse("answer needs a unit, retry, defer, reject, approve, agree or disagree, and a reason, or a charter clause, keep and a reason")
	}
	kind, reason := args[1], strings.TrimSpace(strings.Join(args[2:], " "))
	if c, err := clause.ParseCitation(args[0]); err == nil && c.ID.Kind == clause.Charter {
		return e.keep(c, kind, reason)
	}
	if kind != tracker.Retry && kind != tracker.Defer && kind != tracker.Reject && kind != tracker.Approve && kind != tracker.Agree && kind != tracker.Disagree {
		return e.misuse("an answer is %s, %s, %s, %s, %s or %s, not %q", tracker.Retry, tracker.Defer, tracker.Reject, tracker.Approve, tracker.Agree, tracker.Disagree, kind)
	}
	if reason == "" {
		return e.misuse("answer needs a reason")
	}
	if kind == tracker.Agree || kind == tracker.Disagree {
		return e.withTracker(func(t *tracker.Tracker) int {
			u, err := t.Unit(args[0])
			if err != nil {
				return e.fail(err)
			}
			if err := t.SampledAnswer(u.Change, kind, reason); err != nil {
				return e.fail(err)
			}
			verb := "agreed with"
			if kind == tracker.Disagree {
				verb = "disagreed with"
			}
			fmt.Fprintf(e.stdout, "%s %s's sampled amendment\n", verb, unit.Short(u.Change))
			return OK
		})
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
	if kind == tracker.Approve {
		return e.withTracker(func(t *tracker.Tracker) int {
			u, err := t.Unit(args[0])
			if err != nil {
				return e.fail(err)
			}
			if err := t.Approve(u.Change, reason); err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.stdout, "approved %s; now proposed, its next debate seals it\n", unit.Short(u.Change))
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

// keep answers the charter question listed for a clause of the charter on
// main by keeping the clause as it stands (S.owner.10). It refuses a kind
// other than keep, a clause with a revision or not on main's charter, a
// clause with no question listed and an empty reason.
func (e env) keep(c clause.Citation, kind, reason string) int {
	if kind != tracker.Keep {
		return e.misuse("an answer to a charter clause is %s, not %q", tracker.Keep, kind)
	}
	if c.Rev != "" {
		return e.misuse("answer names charter clause %s without a revision, not %s", c.ID, c)
	}
	if reason == "" {
		return e.misuse("answer needs a reason")
	}
	return e.withRepo(func(t *tracker.Tracker, repo *vcs.Repo) int {
		main, err := repo.MainCommit(e.ctx)
		if err != nil {
			return e.fail(err)
		}
		questions, err := charterQuestions(e.root, main, t)
		if err != nil {
			return e.fail(err)
		}
		if !slices.ContainsFunc(questions, func(q charterQuestion) bool { return q.Clause == c.ID }) {
			return e.fail(fmt.Errorf("no charter question is listed for %s", c.ID))
		}
		if err := t.Keep(c.ID.String(), reason); err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "kept %s; its charter question is answered\n", c.ID)
		return OK
	})
}
