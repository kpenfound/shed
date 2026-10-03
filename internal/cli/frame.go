package cli

import (
	"fmt"

	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/unit"
)

// frame runs shed frame: a frame builder session on a distant or eventual
// horizon clause (S.frame.1 to S.frame.3), or, with -discard, discarding a
// framing it opened.
func (e env) frame(args []string) int {
	fs := e.flags("frame")
	discard := fs.Bool("discard", false, "discard a framing that shed frame opened")
	accept := fs.Bool("accept", false, "land a framing that shed frame opened")
	expire := fs.Bool("expire", false, "expire an overdue contested unit by hand")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() != 1 {
		switch {
		case *discard:
			return e.misuse("frame -discard needs one unit")
		case *accept:
			return e.misuse("frame -accept needs one unit")
		case *expire:
			return e.misuse("frame -expire needs one unit")
		}
		return e.misuse("frame needs one horizon clause")
	}
	if *expire {
		return e.withFactory(func(f *factory.Factory) int {
			ex, err := f.ExpireContested(e.ctx, fs.Arg(0), e.clock())
			if err != nil {
				return e.fail(err)
			}
			switch ex.Outcome {
			case factory.ExpiryArchived:
				fmt.Fprintf(e.stdout, "expired %s to the %s shelf: %s\n", unit.Short(ex.Change), ex.Shelf, ex.Reason)
				return OK
			case factory.ExpiryKept:
				fmt.Fprintf(e.stdout, "kept %s contested\n", unit.Short(ex.Change))
				return OK
			case factory.ExpiryLeft:
				fmt.Fprintf(e.stdout, "%s left contested while the session ran; archived nothing\n", unit.Short(ex.Change))
				return OK
			case factory.ExpiryAgain:
				fmt.Fprintf(e.stdout, "%s moved to contested again while the session ran; archived nothing\n", unit.Short(ex.Change))
				return OK
			}
			reason := ""
			if ex.Reason != "" {
				reason = ": " + ex.Reason
			}
			fmt.Fprintf(e.stderr, "shed: the frame builder's session ended without an outcome%s; archived nothing\n", reason)
			return Failed
		})
	}
	if *discard {
		return e.withFactory(func(f *factory.Factory) int {
			u, err := f.DiscardFraming(e.ctx, fs.Arg(0))
			if err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.stdout, "discarded %s\n", unit.Short(u.Change))
			return OK
		})
	}
	if *accept {
		return e.withFactory(func(f *factory.Factory) int {
			u, err := f.Tracker.Unit(fs.Arg(0))
			if err != nil {
				return e.fail(err)
			}
			acc, err := f.AcceptFraming(e.ctx, u.Change)
			if err != nil {
				return e.fail(err)
			}
			if !acc.Landed() {
				fmt.Fprintf(e.stderr, "shed: the framing of %s no longer fits the check; kept nothing:\n", acc.Clause)
				for _, p := range acc.Problems {
					fmt.Fprintf(e.stderr, "  %s\n", p)
				}
				return Failed
			}
			fmt.Fprintf(e.stdout, "accepted %s, landed on main as %s\n", unit.Short(u.Change), acc.Commit)
			for _, r := range acc.Swept {
				fmt.Fprintf(e.stdout, "%s %s: %s\n", unit.Short(r.Unit.Change), r.Unit.State, r.Outcome)
			}
			return OK
		})
	}
	return e.withFactory(func(f *factory.Factory) int {
		fr, err := f.Frame(e.ctx, fs.Arg(0))
		if err != nil {
			return e.fail(err)
		}
		switch {
		case fr.Kept():
			fmt.Fprintf(e.stdout, "opened %s in proposed, framing %s\n", unit.Short(fr.Change), fr.Clause)
			for _, a := range fr.Added {
				fmt.Fprintf(e.stdout, "  %s\t%s\n", a.ID, a.Tier)
			}
			return OK
		case fr.Outcome == "nothing":
			fmt.Fprintf(e.stdout, "the frame builder found nothing to add to %s; kept nothing\n", fr.Clause)
			return OK
		case fr.Outcome == "":
			reason := ""
			if fr.Reason != "" {
				reason = ": " + fr.Reason
			}
			fmt.Fprintf(e.stderr, "shed: the frame builder's session ended without an outcome%s; kept nothing\n", reason)
			return Failed
		}
		fmt.Fprintf(e.stderr, "shed: the framing of %s fails the check; kept nothing:\n", fr.Clause)
		for _, p := range fr.Problems {
			fmt.Fprintf(e.stderr, "  %s\n", p)
		}
		return Failed
	})
}
