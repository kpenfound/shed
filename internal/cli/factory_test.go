package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

// crew plays every role: the committee finds nothing wrong, the mechanic
// writes a proof and the code, and the reviewer passes the unit.
type crew struct{ t *testing.T }

func (c crew) Run(_ context.Context, turn session.Turn) (session.Result, error) {
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(turn.Dir, name), []byte(content), 0o644); err != nil {
			c.t.Fatal(err)
		}
	}
	switch {
	case turn.Role == unit.Committee && turn.Step == "review":
		return session.Result{Status: "pass"}, nil
	case turn.Role == unit.Committee:
		return session.Result{Status: "clean"}, nil
	case turn.Role == unit.Mechanic && turn.Step == "proofs":
		write("bye_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.2\nfunc TestBye(t *testing.T) {\n\tif Bye() != \"goodbye\" {\n\t\tt.Fatal()\n\t}\n}\n")
	case turn.Role == unit.Mechanic && turn.Step == "implement":
		write("bye.go", "package greet\n\n// Bye says goodbye.\nfunc Bye() string { return \"goodbye\" }\n")
	}
	return session.Result{Status: "done"}, nil
}

func runWith(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := RunWith(context.Background(), append([]string{"-C", dir}, args...), &out, &errOut, crew{t})
	return out.String(), errOut.String(), code
}

//shed:proves S.sched.1 S.fp.2 S.shed.1
func TestOwnerDrivesAUnitToMain(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet.go", "package greet\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {}\n")
	r.JJ("commit", "-m", "module")
	r.JJ("bookmark", "set", "main", "-r", "@-")

	change := openUnit(t, r.Dir, "Say goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(testrepo.Spec+"- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runWith(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", "-estimate", "100", change)
	if code != OK || out != unit.Short(change)+" modifies S.core.2; depends on S.core.1; advances H.greet.2\n" {
		t.Fatalf("declare = %d, %q, %q", code, out, stderr)
	}
	out, stderr, code = runWith(t, r.Dir, "run", change)
	if code != OK || out != unit.Short(change)+" landed\n" {
		t.Fatalf("run = %d, %q, %q", code, out, stderr)
	}
	msg := r.Git("log", "-1", "--format=%B", "main")
	if !strings.Contains(msg, "Say goodbye") || !strings.Contains(msg, "added   S.core.2") {
		t.Errorf("main's commit:\n%s", msg)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".shed", "sessions")); err != nil {
		t.Error(err)
	}
	// H.greet.2 is not marked realised, so the painter runs; this crew has
	// nothing to propose.
	if out, _, code := runWith(t, r.Dir, "serve", "-once"); code != OK || !strings.HasPrefix(out, "painter: ") || !strings.HasSuffix(out, " discarded\n") {
		t.Errorf("serve = %d, %q", code, out)
	}
}

//shed:proves S.track.1
func TestServeFromInsideTheRepository(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {}\n")
	r.JJ("commit", "-m", "module")
	r.JJ("bookmark", "set", "main", "-r", "@-")
	// Run as the owner would, from the repository root with its defaults:
	// the root is "." and the state directory ".shed".
	t.Chdir(r.Dir)
	var out, errOut bytes.Buffer
	code := RunWith(context.Background(), []string{"serve", "-once"}, &out, &errOut, crew{t})
	if code != OK || strings.Contains(out.String(), "error") {
		t.Fatalf("serve = %d\n%s%s", code, out.String(), errOut.String())
	}
	if !strings.HasPrefix(out.String(), "painter: ") {
		t.Errorf("serve ran no painter session:\n%s", out.String())
	}
}
