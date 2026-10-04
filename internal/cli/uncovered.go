package cli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/proof"
)

// uncovered runs every proof the repository holds once, measuring statement
// coverage over the whole Go module, and reports each non-test source
// file's share of unexecuted statements, most unexecuted first, then the
// module's totals (S.adopt.1, S.adopt.2). It calls no model and changes
// nothing (S.adopt.1).
func (e env) uncovered(args []string) int {
	if len(args) > 0 {
		return e.misuse("uncovered takes no arguments")
	}
	proofs, problems, err := proof.Discover(e.root)
	if err != nil {
		return e.fail(err)
	}
	if len(problems) > 0 {
		return e.report(problems)
	}
	cfg, err := config.LoadProject(e.root)
	if err != nil {
		return e.fail(err)
	}
	runner := proof.Runner{Root: e.root, Prefix: cfg.Proofs.Runner, Stderr: e.stderr}

	dir, err := os.MkdirTemp("", "shed-uncovered-")
	if err != nil {
		return e.fail(err)
	}
	defer os.RemoveAll(dir)
	profilePath := filepath.Join(dir, "cover.out")

	results, err := runner.Coverage(e.ctx, proofs, profilePath)
	if err != nil {
		return e.fail(err)
	}

	// S.adopt.3: every proof must have reported, even if other proofs ran
	// and yielded coverage.
	var unreported []string
	for _, p := range proofs {
		if results[p.Name()] == proof.NoResult {
			unreported = append(unreported, p.Name())
		}
	}
	if len(unreported) > 0 {
		slices.Sort(unreported)
		return e.fail(fmt.Errorf("proofs never reported: %s", strings.Join(unreported, ", ")))
	}

	covered, err := proof.ParseProfile(profilePath)
	if err != nil {
		return e.fail(err)
	}

	files, err := runner.SourceFiles(e.ctx)
	if err != nil {
		return e.fail(err)
	}

	type row struct {
		path              string
		unexecuted, total int
	}
	var rows []row
	for _, f := range files {
		var total, executed int
		if fc, ok := covered[f.Key]; ok {
			total, executed = fc.Total, fc.Executed
		} else {
			total, err = proof.CountStatements(filepath.Join(e.root, f.Path))
			if err != nil {
				return e.fail(err)
			}
		}
		rows = append(rows, row{path: f.Path, unexecuted: total - executed, total: total})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].unexecuted != rows[j].unexecuted {
			return rows[i].unexecuted > rows[j].unexecuted
		}
		return rows[i].path < rows[j].path
	})

	w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	var totalUnexecuted, total int
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", r.path, r.unexecuted, r.total, unexecutedShare(r.unexecuted, r.total))
		totalUnexecuted += r.unexecuted
		total += r.total
	}
	fmt.Fprintf(w, "total\t%d\t%d\t%s\n", totalUnexecuted, total, unexecutedShare(totalUnexecuted, total))
	if err := w.Flush(); err != nil {
		return e.fail(err)
	}
	return OK
}

// unexecutedShare reports unexecuted out of total as a whole percentage,
// 0% for a file with no statements (S.adopt.2).
func unexecutedShare(unexecuted, total int) string {
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", int(math.Round(float64(unexecuted)/float64(total)*100)))
}
