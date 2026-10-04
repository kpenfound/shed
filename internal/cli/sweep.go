package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
)

// sweep brings in main and checks its current commit out into a fresh
// directory, runs the proofs of every spec clause there as `shed prove`
// does, reports pass or fail per clause and lists the failing clauses, and
// records the sweep in the tracker (S.sweep.1, S.sweep.2). It opens the
// factory to pick up the recovery every shed process runs when it opens the
// repository and the tracker (S.vcs.5, S.vcs.11, S.track.8), and nothing
// else it does changes main, a unit or the repository's working copy.
func (e env) sweep(args []string) int {
	if len(args) > 0 {
		return e.misuse("sweep takes no arguments")
	}
	started := e.clock()
	return e.withFactory(func(f *factory.Factory) int {
		commit, err := f.Repo.MainCommit(e.ctx)
		if err != nil {
			return e.fail(err)
		}
		dir, err := os.MkdirTemp(e.state, "sweep-")
		if err != nil {
			return e.fail(err)
		}
		defer os.RemoveAll(dir)
		if err := f.Repo.ExportMain(e.ctx, commit, dir); err != nil {
			return e.fail(err)
		}

		results, err := sweepProofs(e.ctx, dir, e.stderr)
		if err != nil {
			return e.fail(err)
		}
		code := OK
		var failing []string
		var sweptClauses []tracker.SweepClause
		w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
		for _, cr := range results {
			sweptClauses = append(sweptClauses, tracker.SweepClause{Clause: cr.ID.String(), Pass: cr.Pass, Output: cr.Output})
			if cr.Pass {
				fmt.Fprintf(w, "pass\t%s\n", cr.ID)
				continue
			}
			code = Failed
			failing = append(failing, cr.ID.String())
			if len(cr.Proofs) == 0 {
				fmt.Fprintf(w, "fail\t%s\tno proof\n", cr.ID)
				continue
			}
			var notes []string
			for _, name := range slices.Sorted(maps.Keys(cr.Proofs)) {
				if s := cr.Proofs[name]; s != proof.Pass {
					notes = append(notes, name+": "+string(s))
				}
			}
			fmt.Fprintf(w, "fail\t%s\t%s\n", cr.ID, strings.Join(notes, ", "))
		}
		if err := w.Flush(); err != nil {
			return e.fail(err)
		}
		for _, id := range failing {
			fmt.Fprintln(e.stdout, id)
		}
		if err := f.Tracker.RecordSweep(commit, started, sweptClauses); err != nil {
			return e.fail(err)
		}
		return code
	})
}

// sweepProofs loads the documents at root and runs the proofs of every spec
// clause there, as `shed prove` with no clauses named does, using the
// runner root's shed.toml sets (S.proof.4, S.proof.5).
func sweepProofs(ctx context.Context, root string, stderr io.Writer) ([]proof.ClauseResult, error) {
	set, problems := docs.Load(revision.Worktree(root))
	if len(problems) > 0 {
		return nil, fmt.Errorf("the swept commit's documents have %d problems", len(problems))
	}
	var ids []clause.ID
	for _, c := range set.Clauses(clause.Spec) {
		ids = append(ids, c.ID)
	}
	cfg, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	proofs, problems, err := proof.Discover(root)
	if err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("the swept commit's proofs have %d problems", len(problems))
	}
	var selected []proof.Proof
	for _, id := range ids {
		for _, p := range proof.For(proofs, id) {
			if !slices.ContainsFunc(selected, func(q proof.Proof) bool { return q.Name() == p.Name() }) {
				selected = append(selected, p)
			}
		}
	}
	runner := proof.Runner{Root: root, Prefix: cfg.Proofs.Runner, Stderr: stderr}
	results, err := runner.RunDetailed(ctx, selected)
	if err != nil {
		return nil, err
	}
	return proof.ByClauseDetailed(ids, proofs, results), nil
}
