package factory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// CharterDraft opens a charter amendment unit on main as shed unit open does
// (S.vcs.3), with the owner as actor and the given title, and returns the
// unit's change ID and its workspace's directory, where the owner edits
// charter.md (S.owner.22). It refuses an empty title and opens nothing.
func (f *Factory) CharterDraft(ctx context.Context, title string) (change, dir string, err error) {
	if strings.TrimSpace(title) == "" {
		return "", "", errors.New("a charter draft needs a title")
	}
	change, err = f.Repo.NewUnit(ctx, title)
	if err != nil {
		return "", "", err
	}
	if err := f.Tracker.OpenCharterUnit(change, title); err != nil {
		return "", "", errors.Join(err, f.Repo.Discard(ctx, change))
	}
	dir, err = f.Repo.Workspace(ctx, change)
	if err != nil {
		return change, "", err
	}
	return change, dir, nil
}

// DiscardCharterDraft archives a charter amendment unit that is still
// proposed, with the owner as actor, as deferred with no archive entry, and
// discards its change (S.owner.22). It refuses any other unit.
func (f *Factory) DiscardCharterDraft(ctx context.Context, ref string) (tracker.Unit, error) {
	u, err := f.Tracker.Unit(ref)
	if err != nil {
		return u, err
	}
	if !u.CharterAmendment {
		return u, fmt.Errorf("unit %s is not a charter amendment unit", unit.Short(u.Change))
	}
	if u.State != unit.Proposed {
		return u, fmt.Errorf("unit %s is %s; only a proposed charter amendment unit is discarded", unit.Short(u.Change), u.State)
	}
	if err := f.Tracker.Archive(u.Change, unit.Deferred, unit.Owner, "the owner discarded the charter amendment"); err != nil {
		return u, err
	}
	return u, f.Repo.Discard(ctx, u.Change)
}

// CharterDebateResult is what shed debate reports for a charter amendment
// unit (S.owner.22), judged under S.owner.23 instead of debated in
// committee rounds (S.shed.1). Problems names each problem found; with none,
// the amendment is ready, but charter amendments are not yet debated.
type CharterDebateResult struct {
	Problems []string
}

func (r *CharterDebateResult) Error() string {
	if len(r.Problems) == 0 {
		return "the amendment is ready, but charter amendments are not yet debated"
	}
	return strings.Join(r.Problems, "\n")
}

// judgeCharterAmendment judges the files of a charter amendment unit's
// workspace, as they stand, against charter.md on the latest main commit
// the unit's change descends from (S.owner.23). It names each problem: each
// file other than charter.md that differs from that commit, a file present
// on one side and absent on the other counting as differing; that
// charter.md is byte for byte the same as on that commit, when it is; and
// each problem shed check would report in charter.md, including a
// malformed, duplicate or misplaced ID (S.doc.5) and an ID main's history
// has retired (S.doc.6).
func (f *Factory) judgeCharterAmendment(ctx context.Context, change string) ([]string, error) {
	commit, err := f.Repo.Snapshot(ctx, change)
	if err != nil {
		return nil, err
	}
	base, err := f.Repo.Base(ctx, change)
	if err != nil {
		return nil, err
	}
	names, err := f.Repo.DiffNames(ctx, base, commit)
	if err != nil {
		return nil, err
	}

	var problems []string
	charterChanged := false
	for _, name := range names {
		if name == docs.CharterPath {
			charterChanged = true
			continue
		}
		problems = append(problems, fmt.Sprintf("%s differs from main; a charter amendment changes charter.md alone", name))
	}
	if !charterChanged {
		problems = append(problems, "charter.md is byte for byte the same as on main; a charter amendment must change it")
	}

	work, loadProblems := docs.Load(revision.Git{Root: f.Root, Rev: commit})
	for _, p := range loadProblems {
		if p.File == docs.CharterPath {
			problems = append(problems, p.String())
		}
	}
	history, err := docs.History(f.Root, work)
	if err != nil {
		return nil, err
	}
	for _, p := range history {
		if p.File == docs.CharterPath {
			problems = append(problems, p.String())
		}
	}
	return problems, nil
}
