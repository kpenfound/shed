// Package factory runs change units through the shed: debate and sealing,
// implementation, verification and landing, and the painter that proposes
// new units. It joins the tracker, the repository, sessions and bundles; the
// state machine and every decision between sessions are its Go code.
package factory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kpenfound/busybees/core/agent"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// Factory is an open factory: the repository, its state directory and
// everything that runs sessions on its units.
type Factory struct {
	Root     string
	State    string
	Operator config.Operator
	Project  config.Project
	Tracker  *tracker.Tracker
	Repo     *vcs.Repo
	Sessions *session.Sessions
	Provider bundle.Provider
}

// Open opens the factory of a repository with its state directory. Runner
// runs sessions; nil runs them in Docker Sandboxes through busybees/core.
func Open(ctx context.Context, root, state string, runner session.Runner) (*Factory, error) {
	// Session directories and views are granted to sessions by path, and a
	// grant must be absolute.
	state, err := filepath.Abs(state)
	if err != nil {
		return nil, err
	}
	op, err := config.LoadOperator(filepath.Join(state, config.OperatorFile))
	if err != nil {
		return nil, err
	}
	project, err := config.LoadProject(root)
	if err != nil {
		return nil, err
	}
	repo, err := vcs.Open(ctx, root, state, vcs.Options{
		JJ: op.VCS.JJ, Main: op.VCS.Main, Remote: op.VCS.Remote,
		Landing: vcs.Identity{Name: op.VCS.LandingName, Email: op.VCS.LandingEmail},
	})
	if err != nil {
		return nil, err
	}
	tr, err := tracker.Open(state, tracker.Options{BounceThreshold: op.Shed.BounceThreshold})
	if err != nil {
		return nil, err
	}
	if runner == nil {
		runner = &session.Core{Operator: op, Runner: agent.Runner{
			SessionsDir: filepath.Join(state, tracker.SessionsDir), NamePrefix: "shed-",
		}}
	}
	return &Factory{
		Root: repo.Root(), State: state, Operator: op, Project: project,
		Tracker: tr, Repo: repo,
		Sessions: &session.Sessions{Tracker: tr, Runner: runner, Retries: 2},
		Provider: bundle.Files{},
	}, nil
}

// Close closes the tracker.
func (f *Factory) Close() error { return f.Tracker.Close() }

// work is one session on a unit.
type work struct {
	Unit   tracker.Unit
	Role   unit.Actor
	Prompt string
	Step   string
	Task   string
	// Writable sessions change the unit's files; shed captures them onto
	// the unit's change when the session ends.
	Writable bool
	// Tools builds the session's tools for its working directory and the
	// documents in it.
	Tools    func(dir string, head *docs.Set) []session.Tool
	Outcomes []string
	// Check vets each outcome before the done tool accepts it.
	Check    func(status, note string) error
	StepDone string
	Extra    []bundle.Section
}

// session runs one session on a unit: it exports the unit's files into a
// fresh directory, builds the bundle, runs the session and, for a writable
// one, captures the directory back onto the unit's change.
func (f *Factory) session(ctx context.Context, w work) (session.Result, error) {
	view := filepath.Join(f.State, "views", nonce())
	if err := f.Repo.Export(ctx, w.Unit.Change, view); err != nil {
		return session.Result{}, err
	}
	defer os.RemoveAll(view)

	main, err := f.mainSet(ctx)
	if err != nil {
		return session.Result{}, err
	}
	head, _ := docs.Load(revision.Worktree(view))
	proofs, _, _ := proof.Discover(view)
	pending, err := f.Tracker.Notices(w.Role, true)
	if err != nil {
		return session.Result{}, err
	}
	pending = slices.DeleteFunc(pending, func(n tracker.Notice) bool { return n.Unit != w.Unit.Change })
	record, err := f.record(w.Unit.Change)
	if err != nil {
		return session.Result{}, err
	}
	answers, err := f.Tracker.Answers(w.Unit.Change)
	if err != nil {
		return session.Result{}, err
	}
	b, err := f.Provider.Bundle(ctx, bundle.Request{Role: w.Role, Unit: w.Unit, Main: main, Head: head,
		Proofs: proofs, Debate: record, Answers: answers, Notices: pending, Extra: w.Extra})
	if err != nil {
		return session.Result{}, err
	}
	system, err := roles.System(f.State, w.Prompt)
	if err != nil {
		return session.Result{}, err
	}
	var tools []session.Tool
	if w.Tools != nil {
		tools = w.Tools(view, head)
	}
	var ids []string
	for _, n := range pending {
		ids = append(ids, n.ID)
	}
	res, err := f.Sessions.Run(ctx, session.Turn{
		Unit: w.Unit.Change, Role: w.Role, Step: w.Step, Dir: view, Writable: w.Writable,
		SystemPrompt: system, Prompt: w.Task, Bundle: b.Render(), Notices: ids, Tools: tools,
		Outcomes: w.Outcomes, Check: w.Check, StepDone: w.StepDone,
	})
	if err != nil {
		return res, err
	}
	if w.Writable {
		if _, err := f.Repo.Capture(ctx, w.Unit.Change, view); err != nil {
			return res, fmt.Errorf("capturing the session's work: %w", err)
		}
	}
	return res, nil
}

// mainSet loads the documents on main.
func (f *Factory) mainSet(ctx context.Context) (*docs.Set, error) {
	main, err := f.Repo.MainCommit(ctx)
	if err != nil {
		return nil, err
	}
	set, _ := docs.Load(revision.Git{Root: f.Root, Rev: main})
	return set, nil
}

// headSet loads the documents on a unit's change, after snapshotting its
// workspace.
func (f *Factory) headSet(ctx context.Context, change string) (*docs.Set, error) {
	commit, err := f.Repo.Snapshot(ctx, change)
	if err != nil {
		return nil, err
	}
	set, _ := docs.Load(revision.Git{Root: f.Root, Rev: commit})
	return set, nil
}

// onMain reports whether a clause is in the spec on main.
func onMain(main *docs.Set) func(clause.ID) bool {
	return func(id clause.ID) bool {
		_, ok := main.Lookup(id)
		return ok
	}
}

// record renders a unit's debate record: every objection of every debate,
// with its answer and withdrawal.
func (f *Factory) record(change string) (string, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	all, err := f.Tracker.Objections(change, -1)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	cycle := -1
	for _, o := range all {
		if o.Cycle != cycle {
			cycle = o.Cycle
			label := "an earlier debate"
			if cycle == u.Bounces {
				label = "the current debate"
			}
			fmt.Fprintf(&b, "\n### Debate %d, %s\n\n", cycle+1, label)
		}
		state := "standing"
		if !o.Standing() {
			state = "withdrawn"
		}
		fmt.Fprintf(&b, "- %s, round %d, member %d, %s, citing %s (%s): %s\n",
			o.ID, o.Round, o.Member, o.Kind, strings.Join(o.Citations, ", "), state, o.Text)
		if o.Answer != "" {
			fmt.Fprintf(&b, "  - Answer: %s\n", o.Answer)
		}
		if o.Withdrawn != "" {
			fmt.Fprintf(&b, "  - Withdrawn: %s\n", o.Withdrawn)
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func nonce() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
