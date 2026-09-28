package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/vcs"
)

// doctor checks, without changing anything, what running the factory needs.
func (e env) doctor(args []string) int {
	if len(args) > 0 {
		return e.misuse("doctor takes no arguments")
	}
	failed := false
	report := func(name string, err error, ok string) {
		if err != nil {
			failed = true
			fmt.Fprintf(e.stdout, "FAIL  %s: %v\n", name, err)
			return
		}
		fmt.Fprintf(e.stdout, "ok    %s: %s\n", name, ok)
	}

	summary, err := e.documentsHealth()
	report("documents", err, summary)

	op, err := e.operator()
	report("operator settings", err, filepath.Join(e.state, config.OperatorFile))
	if err != nil {
		op = config.Defaults()
	}
	project, err := config.LoadProject(e.root)
	if err == nil {
		err = runnerReady(e.root, project.Proofs.Runner)
	}
	runner := "go test"
	if len(project.Proofs.Runner) > 0 {
		runner = strings.Join(project.Proofs.Runner, " ")
	}
	report("test runner", err, runner)

	version, err := vcs.CheckJJ(e.ctx, op.VCS.JJ)
	report("jj", err, version)
	err = vcs.Check(e.ctx, e.root, e.state, vcs.Options{JJ: op.VCS.JJ, Main: op.VCS.Main,
		Landing: vcs.Identity{Name: op.VCS.LandingName, Email: op.VCS.LandingEmail}})
	report("repository", err, "colocated jj repository with a "+op.VCS.Main+" bookmark")
	if op.VCS.Remote != "" {
		out, err := exec.CommandContext(e.ctx, "git", "-C", e.root, "remote", "get-url", op.VCS.Remote).Output()
		if err != nil {
			err = fmt.Errorf("no git remote %q; add it with `git remote add %s <url>` or clear vcs.remote", op.VCS.Remote, op.VCS.Remote)
		}
		report("remote", err, op.VCS.Remote+" at "+strings.TrimSpace(string(out)))
	}

	out, err := exec.CommandContext(e.ctx, "sbx", "version").CombinedOutput()
	if err != nil {
		err = errors.New("sessions run in Docker Sandboxes and `sbx version` failed; install the sbx CLI")
	}
	report("sbx", err, firstLine(string(out)))

	if failed {
		return Failed
	}
	return OK
}

// documentsHealth checks the documents and proofs as shed check does.
func (e env) documentsHealth() (string, error) {
	set, problems := docs.Load(revision.Worktree(e.root))
	problems = append(problems, docs.Validate(set, docs.NewResolver(e.root, set))...)
	proofs, pp, err := proof.Discover(e.root)
	if err != nil {
		return "", err
	}
	problems = append(problems, pp...)
	problems = append(problems, proof.Check(proofs, set)...)
	if len(problems) > 0 {
		return "", fmt.Errorf("%d problems, first %s; run shed check", len(problems), problems[0])
	}
	return fmt.Sprintf("%d spec clauses, %d proofs", len(set.Clauses(clause.Spec)), len(proofs)), nil
}

// runnerReady checks that the test runner shed.toml names can be run.
func runnerReady(root string, runner []string) error {
	if len(runner) == 0 {
		_, err := exec.LookPath("go")
		return err
	}
	name := runner[0]
	if strings.ContainsRune(name, '/') {
		if !filepath.IsAbs(name) {
			name = filepath.Join(root, name)
		}
		info, err := os.Stat(name)
		if err != nil {
			return fmt.Errorf("the runner %s: %w", runner[0], err)
		}
		if info.Mode()&0o111 == 0 {
			return fmt.Errorf("the runner %s is not executable", runner[0])
		}
		return nil
	}
	_, err := exec.LookPath(name)
	return err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
