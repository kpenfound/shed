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
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() != 1 {
		if *discard {
			return e.misuse("frame -discard needs one unit")
		}
		return e.misuse("frame needs one horizon clause")
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
