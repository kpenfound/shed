package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

// withTools puts only git, jj, go and, when sbx is set, a fake sbx on PATH.
func withTools(t *testing.T, sbx bool) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"git", "jj", "go"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if sbx {
		if err := os.WriteFile(filepath.Join(dir, "sbx"), []byte("#!/bin/sh\necho 'sbx version: v0.45.1'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

//shed:proves S.doctor.1
func TestDoctor(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {}\n")
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	withTools(t, true)

	out, stderr, code := run(t, r.Dir, "doctor")
	if code != OK {
		t.Fatalf("doctor = %d\n%s%s", code, out, stderr)
	}
	for _, want := range []string{
		"ok    documents: 1 spec clauses, 1 proofs\n",
		"ok    test runner: go test\n",
		"ok    jj: 0.45",
		"ok    repository: colocated jj repository with a main bookmark\n",
		"ok    remote: origin at " + r.Remote,
		"ok    sbx: sbx version: v0.45.1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}

	r.Write(".shed/config.toml", "[vcs]\nremote = \"upstream\"\n")
	r.Write("spec/core.md", testrepo.Spec+"- **S.core.2** (H.greet.2) Unproven.\n")
	r.Write("shed.toml", "[proofs]\nrunner = [\"scripts/missing\"]\n")
	withTools(t, false)
	out, _, code = run(t, r.Dir, "doctor")
	if code != Failed {
		t.Errorf("doctor with failures = %d", code)
	}
	for _, want := range []string{
		"FAIL  documents: 1 problems, first spec/core.md:4: S.core.2 has no proof; run shed check\n",
		"FAIL  test runner: the runner scripts/missing:",
		"FAIL  remote: no git remote \"upstream\"",
		"FAIL  sbx: sessions run in Docker Sandboxes and `sbx version` failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}

	plain := testrepo.Minimal(t)
	plain.Init()
	plain.Commit("documents")
	out, _, code = run(t, plain.Dir, "doctor")
	if code != Failed || !strings.Contains(out, "FAIL  repository: not a colocated jj repository: ") ||
		!strings.Contains(out, "run `jj git init --colocate` at the repository root") {
		t.Errorf("doctor outside a jj repository = %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(plain.Dir, ".shed")); err == nil {
		t.Error("doctor created the state directory")
	}
}
