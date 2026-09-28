package factory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// player plays one role in a test. It may change the turn's directory and
// call the turn's tools.
type player func(t session.Turn) session.Result

// fakeRunner runs every session with the agent registered for its role and
// step, and remembers the turns it ran.
type fakeRunner struct {
	t      *testing.T
	mu     sync.Mutex
	agents map[string]player
	turns  []session.Turn
}

func newFake(t *testing.T) *fakeRunner { return &fakeRunner{t: t, agents: map[string]player{}} }

// on registers the agent of a role for steps starting with prefix.
func (f *fakeRunner) on(role unit.Actor, prefix string, a player) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[string(role)+"|"+prefix] = a
}

func (f *fakeRunner) Run(_ context.Context, t session.Turn) (session.Result, error) {
	f.mu.Lock()
	f.turns = append(f.turns, t)
	var found player
	best := -1
	for key, a := range f.agents {
		role, prefix, _ := strings.Cut(key, "|")
		if role == string(t.Role) && strings.HasPrefix(t.Step, prefix) && len(prefix) > best {
			found, best = a, len(prefix)
		}
	}
	f.mu.Unlock()
	if found == nil {
		f.t.Errorf("no fake agent for %s step %q", t.Role, t.Step)
		return session.Result{}, nil
	}
	return found(t), nil
}

// ran returns the turns of a role, in the order they ran.
func (f *fakeRunner) ran(role unit.Actor) []session.Turn {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []session.Turn
	for _, t := range f.turns {
		if t.Role == role {
			out = append(out, t)
		}
	}
	return out
}

func done(status string) session.Result { return session.Result{Status: status, CostUSD: 0.1} }

// call calls a turn's tool as the agent would.
func call(t *testing.T, turn session.Turn, name string, input any) (string, error) {
	t.Helper()
	for _, tool := range turn.Tools {
		if tool.Name == name {
			return tool.Call(ctx, input)
		}
	}
	t.Fatalf("%s's turn %q has no tool %s", turn.Role, turn.Step, name)
	return "", nil
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(content), 0o644))
}

const goodbyeSpec = testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"

// project is a colocated repository with a Go module and a proof of the
// spec's one clause.
func project(t *testing.T) *testrepo.Repo {
	t.Helper()
	testrepo.RequireJJ(t)
	r := testrepo.Minimal(t)
	r.Write(".gitignore", ".shed/\n")
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet.go", "package greet\n\n// Hello greets.\nfunc Hello() string { return \"hello\" }\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {\n\tif Hello() != \"hello\" {\n\t\tt.Fatal(Hello())\n\t}\n}\n")
	r.Init()
	r.Commit("greet")
	r.Remote = t.TempDir()
	r.GitRemoteInit()
	r.Git("remote", "add", "origin", r.Remote)
	r.Git("push", "-q", "origin", "main")
	r.JJ("git", "init", "--colocate")
	return r
}

// open opens a factory on a project with a fake runner.
func open(t *testing.T, r *testrepo.Repo, fake *fakeRunner, config string) *Factory {
	t.Helper()
	state := filepath.Join(r.Dir, ".shed")
	write(t, state, "config.toml", config)
	f, err := Open(ctx, r.Dir, state, fake)
	must(t, err)
	t.Cleanup(func() { f.Close() })
	return f
}

// propose opens a unit whose spec diff adds the goodbye clause, as a
// painter or the owner would.
func propose(t *testing.T, f *Factory) string {
	t.Helper()
	change, err := f.Repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Say goodbye", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", goodbyeSpec)
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter))
	return change
}

// fmtSscanf reads the round and member of a committee member's step.
func fmtSscanf(step string, round, member *int) (int, error) {
	return fmt.Sscanf(step, "debate round %d, member %d", round, member)
}

func mustID(s string) clause.ID { return clause.MustParseID(s) }

type ctxType = context.Context

const testHorizon = testrepo.Horizon
