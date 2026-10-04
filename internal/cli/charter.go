package cli

import (
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/unit"
)

// charter runs shed charter: its one subcommand, draft.
func (e env) charter(args []string) int {
	if len(args) == 0 {
		return e.misuse("charter needs a subcommand: draft")
	}
	switch args[0] {
	case "draft":
		return e.charterDraft(args[1:])
	}
	return e.misuse("unknown charter subcommand %q", args[0])
}

// charterDraft runs shed charter draft: opening a charter amendment unit on
// main for the owner to edit charter.md in (S.owner.22), or, with -discard,
// discarding one that is still proposed.
func (e env) charterDraft(args []string) int {
	fs := e.flags("charter draft")
	discard := fs.Bool("discard", false, "discard a charter amendment unit that is still proposed")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if *discard {
		if fs.NArg() != 1 {
			return e.misuse("charter draft -discard needs one unit")
		}
		return e.withFactory(func(f *factory.Factory) int {
			u, err := f.DiscardCharterDraft(e.ctx, fs.Arg(0))
			if err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.stdout, "discarded %s\n", unit.Short(u.Change))
			return OK
		})
	}
	title := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(title) == "" {
		return e.misuse("charter draft needs a title")
	}
	return e.withFactory(func(f *factory.Factory) int {
		change, dir, err := f.CharterDraft(e.ctx, title)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "%s %s\n", change, dir)
		return OK
	})
}
