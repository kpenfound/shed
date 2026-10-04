package cli

import (
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// outside lists, oldest first, the commits on main's first-parent history
// after shed.toml's outside.since that did not land as a shed unit, with
// charter changes listed apart (S.outside.1, S.outside.2). It brings in main
// and opens the factory to pick up the recovery every shed process runs when
// it opens the repository and the tracker (S.vcs.5, S.vcs.11, S.track.8), and
// otherwise changes nothing (S.outside.3).
func (e env) outside(args []string) int {
	if len(args) > 0 {
		return e.misuse("outside takes no arguments")
	}
	return e.withFactory(func(f *factory.Factory) int {
		since := f.Project.Outside.Since
		if since == "" {
			return e.fail(fmt.Errorf("shed.toml sets no outside.since"))
		}
		sinceCommit, err := revision.Resolve(f.Root, since)
		if err != nil {
			return e.fail(err)
		}
		head, err := f.Repo.MainCommit(e.ctx)
		if err != nil {
			return e.fail(err)
		}
		chain, err := revision.FirstParentSince(f.Root, sinceCommit, head)
		if err != nil {
			return e.fail(err)
		}
		var plain, charter []string
		for _, c := range chain {
			if landedAsUnit(f.Tracker, c.Hash, c.Message) {
				continue
			}
			line := fmt.Sprintf("%s %s %s", c.Hash, c.Author, c.Subject)
			only, err := revision.OnlyChanges(f.Root, c.Hash, "charter.md")
			if err != nil {
				return e.fail(err)
			}
			if only {
				charter = append(charter, line)
			} else {
				plain = append(plain, line)
			}
		}
		for _, line := range plain {
			fmt.Fprintln(e.stdout, line)
		}
		if len(charter) > 0 {
			fmt.Fprintln(e.stdout, "charter changes:")
			for _, line := range charter {
				fmt.Fprintln(e.stdout, line)
			}
		}
		return OK
	})
}

// landedAsUnit reports whether a commit's message holds a Unit: trailer
// naming a unit the tracker records as landed with that very commit
// (S.outside.1).
func landedAsUnit(tr *tracker.Tracker, commit, message string) bool {
	change := unitTrailer(message)
	if change == "" {
		return false
	}
	u, err := tr.Unit(change)
	if err != nil {
		return false
	}
	return u.State == unit.Landed && u.Landed == commit
}

// unitTrailer returns the change ID a commit message's Unit: trailer names,
// or "" when it has none.
func unitTrailer(message string) string {
	for _, line := range strings.Split(message, "\n") {
		if id, ok := strings.CutPrefix(line, "Unit: "); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}
