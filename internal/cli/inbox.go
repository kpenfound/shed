package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/landing"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// inbox lists the contested units, the horizon clauses changed on main
// since the main commit the last inbox recorded (S.owner.1, S.owner.2), then
// the charter questions (S.owner.9) and the sampled amendments since the
// last recorded inbox (S.owner.12). It marks the units contested, and the
// questions that gained a unit, since the last recorded inbox as new
// (S.owner.7), and records the main commit and the latest event it read
// unless it is a peek (S.owner.3). Each horizon clause names the parents
// its refines tag has it judged at (S.owner.13).
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
			// S.owner.13: name the parents each clause is judged at, as
			// shed diff between the two commits would.
			amendment := docs.DiffHorizonAmendment(from, to)
			parents := map[clause.ID]string{}
			for _, group := range [][]docs.TieredHorizonChange{amendment.Added, amendment.Changed, amendment.Removed} {
				for _, c := range group {
					parents[c.ID] = namedParents(c.Parents)
				}
			}
			w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			for _, c := range changes {
				if named := parents[c.ID]; named != "" {
					fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", c.Change, c.ID, c.Tier, named)
					continue
				}
				fmt.Fprintf(w, "  %s\t%s\t%s\n", c.Change, c.ID, c.Tier)
			}
			if err := w.Flush(); err != nil {
				return e.fail(err)
			}
		}

		questions, err := charterQuestions(e.root, main, t)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stdout)
		if len(questions) == 0 {
			fmt.Fprintln(e.stdout, "No charter questions.")
		} else {
			fmt.Fprintln(e.stdout, "Charter questions:")
			w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			for _, q := range questions {
				mark := ""
				if q.New {
					mark = "new"
				}
				fmt.Fprintf(w, "  %s\t%s\n", q.Clause, mark)
				for _, u := range q.Units {
					fmt.Fprintf(w, "    %s\t%s\n", unit.Short(u.Change), u.Title)
				}
			}
			if err := w.Flush(); err != nil {
				return e.fail(err)
			}
		}

		sampled, err := t.Sampled()
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stdout)
		if len(sampled) == 0 {
			fmt.Fprintln(e.stdout, "No sampled amendments.")
		} else {
			fmt.Fprintln(e.stdout, "Sampled amendments:")
			w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
			for _, u := range sampled {
				fmt.Fprintf(w, "  %s\t%s\t%s\n", unit.Short(u.Change), shortCommit(u.Landed), u.Title)
				for _, c := range landing.AmendedHorizon(e.root, u.Landed) {
					fmt.Fprintf(w, "    %s\t%s\n", c.Change, c.ID)
				}
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

// charterQuestion is a charter clause that the entries of at least two
// units on the rejected shelf cite as violated.
type charterQuestion struct {
	Clause clause.ID
	// Units are the counted rejected units citing the clause, in the order
	// they were archived.
	Units []tracker.RejectedUnit
	// New is set when any of the units was archived since the last
	// recorded inbox.
	New bool
}

// charterQuestions returns the charter questions the rejected shelf
// raises, in charter order (S.owner.9). An entry cites a clause of the
// charter on main through its ID, at a revision or not; other citations
// raise no question, and a unit counts once per clause. For a clause the
// owner has kept, only units archived after its latest keep count
// (S.owner.10).
func charterQuestions(root, main string, t *tracker.Tracker) ([]charterQuestion, error) {
	rejected, err := t.Rejected()
	if err != nil {
		return nil, err
	}
	if len(rejected) < 2 {
		return nil, nil
	}
	entries, err := archive.Read(root)
	if err != nil {
		return nil, err
	}
	citations := map[string][]string{}
	for _, entry := range entries {
		if entry.Shelf == unit.Rejected {
			citations[entry.Change()] = entry.Citations()
		}
	}
	set, _ := docs.Load(revision.Git{Root: root, Rev: main})
	if set == nil || set.Charter == nil {
		return nil, nil
	}
	kept, err := t.Kept()
	if err != nil {
		return nil, err
	}
	citing := map[clause.ID][]tracker.RejectedUnit{}
	for _, u := range rejected {
		cited := map[clause.ID]bool{}
		for _, token := range citations[u.Change] {
			c, err := clause.ParseCitation(token)
			if err != nil || c.ID.Kind != clause.Charter || cited[c.ID] {
				continue
			}
			if _, ok := set.Lookup(c.ID); !ok {
				continue
			}
			if since, ok := kept[c.ID.String()]; ok && u.ArchivedSeq <= since {
				continue
			}
			cited[c.ID] = true
			citing[c.ID] = append(citing[c.ID], u)
		}
	}
	var out []charterQuestion
	for _, c := range set.Clauses(clause.Charter) {
		units := citing[c.ID]
		if len(units) < 2 {
			continue
		}
		q := charterQuestion{Clause: c.ID, Units: units}
		for _, u := range units {
			q.New = q.New || u.New
		}
		out = append(out, q)
	}
	return out, nil
}
