package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/testrepo"
)

func tool(t *testing.T, tools []Tool, name string) Tool {
	t.Helper()
	for _, tl := range tools {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return Tool{}
}

//shed:proves S.sess.10
func TestTestToolsRunInTheSessionDirectory(t *testing.T) {
	view := testrepo.Minimal(t)
	view.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	view.Write("greet/greet_test.go", `package greet

import "testing"

//shed:proves S.core.1
func TestHello(t *testing.T) {}

func TestBroken(t *testing.T) { t.Log("details of the break"); t.Fail() }
`)
	// The session's own copy of a runner is never used.
	view.Write("run", "#!/bin/sh\necho hijacked > hijacked.txt\n")
	trusted := filepath.Join(t.TempDir(), "run")
	log := filepath.Join(t.TempDir(), "runner.log")
	must(t, os.WriteFile(trusted, []byte("#!/bin/sh\npwd >> "+log+"\nexec \"$@\"\n"), 0o755))

	tools := TestTools(view.Dir, proof.Runner{Root: "/elsewhere", Prefix: []string{trusted}})
	out, err := tool(t, tools, "run_tests").Call(ctx, map[string]any{"packages": []string{"greet"}})
	must(t, err)
	for _, want := range []string{"fail example.com/greet/greet.TestBroken", "details of the break", "1 of 2 tests passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("run_tests output lacks %q:\n%s", want, out)
		}
	}
	out, err = tool(t, tools, "run_tests").Call(ctx, map[string]any{"packages": []string{"greet"}, "run": "TestHello"})
	must(t, err)
	if !strings.Contains(out, "1 of 1 tests passed") {
		t.Errorf("run_tests -run output:\n%s", out)
	}
	out, err = tool(t, tools, "prove").Call(ctx, map[string]any{"clauses": []string{"S.core.1", "S.core.2"}})
	must(t, err)
	if !strings.Contains(out, "pass S.core.1") || !strings.Contains(out, "fail S.core.2: no proof") {
		t.Errorf("prove output:\n%s", out)
	}
	if _, err := tool(t, tools, "prove").Call(ctx, map[string]any{"clauses": []string{"bad"}}); err == nil {
		t.Error("prove accepted a malformed ID")
	}

	ran, err := os.ReadFile(log)
	must(t, err)
	for _, dir := range strings.Fields(string(ran)) {
		if resolved, _ := filepath.EvalSymlinks(dir); resolved != evalDir(t, view.Dir) {
			t.Errorf("tests ran in %s, want the session's directory", dir)
		}
	}
	if _, err := os.Stat(filepath.Join(view.Dir, "hijacked.txt")); err == nil {
		t.Error("the session's own runner ran")
	}
}
