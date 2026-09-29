// Package archive reads and writes the archive: what the factory decided not
// to do, on two shelves. Rejected entries violated the charter; deferred
// entries were clean but off the horizon. Each entry is a Markdown file under
// archive/<shelf>/ on the archive branch, which painters read.
package archive

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// Entry is one archived proposal.
type Entry struct {
	Shelf unit.Shelf
	// Path is the entry's path on the archive branch.
	Path string
	Text string
}

// Path is where a unit's entry lives on a shelf.
func Path(shelf unit.Shelf, change string) string {
	return path.Join(docs.ArchiveDir, string(shelf), change+".md")
}

// Record is what an entry says about an archived proposal.
type Record struct {
	Title  string
	Change string
	Shelf  unit.Shelf
	// Citations are the clauses the decision rests on: the charter clauses
	// violated, or the horizon clauses the proposal did not advance.
	Citations []string
	// Reason says why; for a deferred entry, what would change the
	// decision.
	Reason   string
	Proposal string
	// Horizon lists the horizon clauses the proposal adds, changes or
	// removes, one per item.
	Horizon string
	// Answers are the owner's answers to the unit, one per line.
	Answers string
	Debate  string
}

// Format writes a record as an entry.
func Format(r Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- Unit: %s\n- Shelf: %s\n", r.Title, r.Change, r.Shelf)
	if len(r.Citations) > 0 {
		fmt.Fprintf(&b, "- Citations: %s\n", strings.Join(r.Citations, ", "))
	}
	label := "Why"
	if r.Shelf == unit.Deferred {
		label = "What would change the decision"
	}
	fmt.Fprintf(&b, "\n## %s\n\n%s\n", label, strings.TrimSpace(r.Reason))
	if r.Proposal != "" {
		fmt.Fprintf(&b, "\n## Proposal\n\n%s\n", strings.TrimSpace(r.Proposal))
	}
	if r.Horizon != "" {
		fmt.Fprintf(&b, "\n## Horizon changes\n\n%s\n", strings.TrimSpace(r.Horizon))
	}
	if r.Answers != "" {
		fmt.Fprintf(&b, "\n## Owner's answers\n\n%s\n", strings.TrimSpace(r.Answers))
	}
	if r.Debate != "" {
		fmt.Fprintf(&b, "\n## Debate\n\n%s\n", strings.TrimSpace(r.Debate))
	}
	return b.String()
}

// Read returns every entry on the archive branch, rejected first. A
// repository with no archive yet has no entries.
func Read(root string) ([]Entry, error) {
	if _, err := revision.Resolve(root, vcs.ArchiveRef); err != nil {
		return nil, nil
	}
	src := revision.Git{Root: root, Rev: vcs.ArchiveRef}
	var out []Entry
	for _, shelf := range []unit.Shelf{unit.Rejected, unit.Deferred} {
		files, err := src.Files(path.Join(docs.ArchiveDir, string(shelf)))
		if errors.Is(err, revision.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			data, err := src.ReadFile(f)
			if err != nil {
				return nil, err
			}
			out = append(out, Entry{Shelf: shelf, Path: f, Text: string(data)})
		}
	}
	return out, nil
}

// Shelf renders the entries on one shelf for a bundle.
func Shelf(entries []Entry, shelf unit.Shelf) string {
	var b strings.Builder
	for _, e := range entries {
		if e.Shelf == shelf {
			fmt.Fprintf(&b, "### %s\n\n%s\n", e.Path, strings.TrimSpace(e.Text))
		}
	}
	return b.String()
}
