package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// inbox lists the contested units, then the horizon clauses changed on main
// since the main commit the last inbox recorded (S.owner.1, S.owner.2). It
// marks the units contested since the last recorded inbox as new
// (S.owner.7), and records the main commit and the latest event it read
// unless it is a peek (S.owner.3).
func (e env) inbox(args []string) int {
	fs := e.flags("inbox")
	peek := fs.Bool("peek", false, "list the inbox without recording that it was read")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() != 0 {
		return e.misuse("inbox takes no arguments")
	}
	return e.withRepo(func(t *tracker.Tracker, repo *vcs.Repo) int {
		contested, seq, err := t.Contested()
		if err != nil {
			return e.fail(err)
		}
		main, err := repo.MainCommit(e.ctx)
		if err != nil {
			return e.fail(err)
		}
		last, err := t.InboxCommit()
		if err != nil {
			return e.fail(err)
		}

		if len(contested) == 0 {
			fmt.Fprintln(e.stdout, "No contested units.")
		} else {
			fmt.Fprintln(e.stdout, "Contested units:")
			w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			for _, u := range contested {
				mark := ""
				if u.New {
					mark = "new"
				}
				fmt.Fprintf(w, "  %s\t%s\tbounces %d\t%s: %s\n", unit.Short(u.Change), mark, u.Bounces, u.Title, u.ContestedReason)
			}
			if err := w.Flush(); err != nil {
				return e.fail(err)
			}
		}

		fmt.Fprintln(e.stdout)
		switch {
		case last == "":
			fmt.Fprintln(e.stdout, "No horizon changes: this is the first inbox.")
		default:
			ok, err := revision.IsAncestor(e.root, last, main)
			if err != nil {
				return e.fail(err)
			}
			if !ok {
				fmt.Fprintf(e.stdout, "No horizon changes: the last inbox read main at %s, which is not an ancestor of main.\n", shortCommit(last))
				break
			}
			from, _ := docs.Load(revision.Git{Root: e.root, Rev: last})
			to, _ := docs.Load(revision.Git{Root: e.root, Rev: main})
			changes := docs.DiffHorizon(from, to)
			if len(changes) == 0 {
				fmt.Fprintf(e.stdout, "No horizon changes since main at %s.\n", shortCommit(last))
				break
			}
			fmt.Fprintf(e.stdout, "Horizon changes since main at %s:\n", shortCommit(last))
			w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			for _, c := range changes {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", c.Change, c.ID, c.Tier)
			}
			if err := w.Flush(); err != nil {
				return e.fail(err)
			}
		}

		if !*peek {
			if err := t.RecordInbox(main, seq); err != nil {
				return e.fail(err)
			}
		}
		return OK
	})
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
