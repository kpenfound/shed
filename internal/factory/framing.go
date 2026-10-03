package factory

import (
	"context"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// startFraming replenishes an exhausted near/soon tier. Work already
// assigned to a painter still counts until it is realised on main.
func (f *Factory) startFraming(ctx context.Context, s *scheduler, units []tracker.Unit) error {
	if s.isBusy(frameBuilderKey) || s.isBusy(painterKey) || s.isBusy(landerKey) {
		return nil
	}
	commit, err := f.Repo.MainCommit(ctx)
	if err != nil {
		return err
	}
	main, _ := docs.Load(revision.Git{Root: f.Root, Rev: commit})
	gap := docs.Gap(main)
	for _, e := range gap {
		if e.Tier == "near" || e.Tier == "soon" {
			return nil
		}
	}
	pending := map[string]bool{}
	for _, u := range units {
		if u.OpenedBy == unit.FrameBuilder && u.State == unit.Proposed {
			if id, ok := frameClause(u.Title); ok {
				pending[id.String()] = true
			}
		}
	}
	for _, tier := range []string{"distant", "eventual"} {
		for _, e := range gap {
			id := e.Clause.ID.String()
			if e.Tier != tier || pending[id] {
				continue
			}
			claimed, err := f.Tracker.ClaimFraming(id, commit)
			if err != nil {
				return err
			}
			if !claimed {
				continue
			}
			s.run(ctx, frameBuilderKey, "frame "+id, func(ctx context.Context) (string, error) {
				fr, err := f.Frame(ctx, id)
				if err != nil {
					return "", err
				}
				if fr.Kept() {
					return fmt.Sprintf("%s waits for owner acceptance: shed frame -accept %s", unit.Short(fr.Change), unit.Short(fr.Change)), nil
				}
				if len(fr.Problems) > 0 {
					return "kept nothing: " + strings.Join(fr.Problems, "; "), nil
				}
				if fr.Outcome == "" {
					return "kept nothing: session ended without an outcome: " + fr.Reason, nil
				}
				return "nothing to frame", nil
			})
			return nil
		}
	}
	return nil
}
