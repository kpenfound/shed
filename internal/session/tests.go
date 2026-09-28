package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/proof"
)

// maxOutput caps the test output a tool returns to a session.
const maxOutput = 16 * 1024

// TestTools are the tools that run tests for a session. Shed runs the tests
// itself, in the session's directory, through the project's runner, so the
// session needs no container engine and no version control. The runner comes
// from the repository shed runs in, never from the session's directory.
func TestTools(dir string, runner proof.Runner) []Tool {
	runner.Root = dir
	type testsIn struct {
		Packages []string `json:"packages,omitempty" jsonschema:"package directories relative to the repository root, such as internal/docs; empty runs every package"`
		Run      string   `json:"run,omitempty" jsonschema:"a go test -run pattern; empty runs every test"`
	}
	type proveIn struct {
		Clauses []string `json:"clauses" jsonschema:"spec clause IDs, such as S.doc.2"`
	}
	return []Tool{
		NewTool("run_tests", "Run the project's Go tests in your working directory and report each test's result, with the output of those that failed.",
			func(ctx context.Context, in testsIn) (string, error) {
				report, err := runner.Test(ctx, in.Packages, in.Run)
				if err != nil {
					return "", err
				}
				return summarise(report), nil
			}),
		NewTool("prove", "Run the proofs of spec clauses in your working directory and report pass or fail for each clause.",
			func(ctx context.Context, in proveIn) (string, error) {
				var ids []clause.ID
				for _, s := range in.Clauses {
					id, err := clause.ParseID(s)
					if err != nil {
						return "", err
					}
					ids = append(ids, id)
				}
				proofs, problems, err := proof.Discover(dir)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				for _, p := range problems {
					fmt.Fprintf(&b, "problem: %s\n", p)
				}
				var selected []proof.Proof
				for _, id := range ids {
					selected = append(selected, proof.For(proofs, id)...)
				}
				results, err := runner.Run(ctx, selected)
				if err != nil {
					return "", err
				}
				for _, cr := range proof.ByClause(ids, proofs, results) {
					switch {
					case cr.Pass:
						fmt.Fprintf(&b, "pass %s\n", cr.ID)
					case len(cr.Proofs) == 0:
						fmt.Fprintf(&b, "fail %s: no proof\n", cr.ID)
					default:
						var notes []string
						for name, s := range cr.Proofs {
							if s != proof.Pass {
								notes = append(notes, name+" "+string(s))
							}
						}
						fmt.Fprintf(&b, "fail %s: %s\n", cr.ID, strings.Join(notes, ", "))
					}
				}
				return b.String(), nil
			}),
	}
}

func summarise(r proof.Report) string {
	var b strings.Builder
	passed := 0
	for _, t := range r.Tests {
		if t.Status == proof.Pass {
			passed++
			continue
		}
		fmt.Fprintf(&b, "%s %s.%s\n%s\n", t.Status, t.Package, t.Test, t.Output)
	}
	fmt.Fprintf(&b, "%d of %d tests passed\n", passed, len(r.Tests))
	if r.Output != "" {
		b.WriteString(r.Output)
	}
	s := b.String()
	if len(s) > maxOutput {
		s = s[:maxOutput/2] + "\n... output cut ...\n" + s[len(s)-maxOutput/2:]
	}
	return s
}
