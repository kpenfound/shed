package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

// coverageModule builds a module whose packages exercise every corner of
// S.adopt.1 and S.adopt.2: a proof that executes another package's
// statements (lib), a package no proof reaches at all (deadcode, unused), a
// file with no statements (types), a file a build constraint excludes
// (lib/excluded.go), and a nested module (nested) that must be skipped like
// shed.prove skips it (S.proof.3).
func coverageModule(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.Minimal(t)
	r.Write("go.mod", "module example.com/cover\n\ngo 1.21\n")
	r.Write("root_test.go", `package cover

import (
	"testing"

	"example.com/cover/lib"
)

//shed:proves S.core.1
func TestUsesLib(t *testing.T) {
	if lib.Used() != 1 {
		t.Fatal("wrong")
	}
}
`)
	r.Write("lib/lib.go", `package lib

func Used() int {
	return 1
}

func Unused() int {
	return 2
}
`)
	r.Write("lib/excluded.go", `//go:build never

package lib

func Never() int {
	return 1
}
`)
	r.Write("deadcode/deadcode.go", `package deadcode

func Ghost() int {
	return 0
}
`)
	r.Write("unused/many.go", `package unused

func Dead() int {
	x := 1
	return x
}
`)
	r.Write("types/types.go", `package types

type Point struct {
	X, Y int
}
`)
	r.Write("nested/go.mod", "module example.com/nested\n\ngo 1.21\n")
	r.Write("nested/nested.go", `package nested

func F() int {
	return 1
}
`)
	return r
}

// withRunner adds a runner wrapper to r that records every time it is
// invoked, so a test can confirm shed put it in front of the go test
// command line (S.proof.5).
func withRunner(t *testing.T, r *testrepo.Repo) {
	t.Helper()
	r.Write("run-here", "#!/bin/sh\necho ran >> runner.log\nexec \"$@\"\n")
	if err := os.Chmod(filepath.Join(r.Dir, "run-here"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Write("shed.toml", "[proofs]\nrunner = [\"./run-here\"]\n")
}

//shed:proves S.adopt.1 S.adopt.2
func TestUncoveredReportsWholeModuleCoverage(t *testing.T) {
	r := coverageModule(t)
	withRunner(t, r)

	stdout, stderr, code := run(t, r.Dir, "uncovered")
	if code != OK {
		t.Fatalf("uncovered = %d, stderr %q", code, stderr)
	}

	// S.adopt.2: one line per non-test source file the go tool builds,
	// ordered by unexecuted statements most first, then by path, with a
	// build-constrained file, a nested module and files with no non-test
	// source all absent, and a final module total.
	want := []string{
		"unused/many.go 2 2 100%",
		"deadcode/deadcode.go 1 1 100%",
		"lib/lib.go 1 2 50%",
		"types/types.go 0 0 0%",
		"total 4 5 80%",
	}
	if got := collapsed(stdout); !slices.Equal(got, want) {
		t.Errorf("uncovered stdout =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// S.proof.5, cited by S.adopt.1: the proofs ran behind proofs.runner.
	log, err := os.ReadFile(filepath.Join(r.Dir, "runner.log"))
	if err != nil || !strings.Contains(string(log), "ran") {
		t.Errorf("runner log = %q, %v, want it to show the runner ran", log, err)
	}
}

// baseExitModule builds a module with one proof each that passes, fails
// after executing another package's statement, and skips, all of which
// report an outcome.
func baseExitModule(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.Minimal(t)
	r.Write("go.mod", "module example.com/exit\n\ngo 1.21\n")
	r.Write("pkglib/pkglib.go", `package pkglib

func Work() int {
	return 7
}
`)
	r.Write("pkgfail/pkgfail_test.go", `package pkgfail

import (
	"testing"

	"example.com/exit/pkglib"
)

//shed:proves S.core.1
func TestFailsButRuns(t *testing.T) {
	if pkglib.Work() != 7 {
		t.Fatal("bad")
	}
	t.Fatal("deliberate failure")
}
`)
	r.Write("pkgskip/pkgskip_test.go", `package pkgskip

import "testing"

//shed:proves S.core.1
func TestSkipsOK(t *testing.T) { t.Skip("later") }
`)
	r.Write("pkgpass/pkgpass_test.go", `package pkgpass

import "testing"

//shed:proves S.core.1
func TestPasses(t *testing.T) {}
`)
	return r
}

//shed:proves S.adopt.3
func TestUncoveredExitCode(t *testing.T) {
	t.Run("passing failing and skipping proofs all exit zero", func(t *testing.T) {
		r := baseExitModule(t)
		stdout, stderr, code := run(t, r.Dir, "uncovered")
		if code != OK {
			t.Fatalf("uncovered = %d, stderr %q", code, stderr)
		}
		// The failing proof still counted the statement it executed
		// before failing.
		want := []string{
			"pkglib/pkglib.go 0 1 0%",
			"total 0 1 0%",
		}
		if got := collapsed(stdout); !slices.Equal(got, want) {
			t.Errorf("uncovered stdout =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("a proof that never reports fails the run even with other coverage", func(t *testing.T) {
		r := baseExitModule(t)
		r.Write("pkgbroken/pkgbroken_test.go", `package pkgbroken

import "testing"

//shed:proves S.core.1
func TestBroken(t *testing.T) { undefined() }
`)
		_, stderr, code := run(t, r.Dir, "uncovered")
		if code != Failed {
			t.Errorf("uncovered = %d, want %d", code, Failed)
		}
		if !strings.Contains(stderr, "TestBroken") {
			t.Errorf("stderr = %q, want it to name TestBroken", stderr)
		}
	})

	t.Run("a runner that cannot start fails the run", func(t *testing.T) {
		r := baseExitModule(t)
		r.Write("shed.toml", "[proofs]\nrunner = [\"/no/such/binary-xyz\"]\n")
		_, stderr, code := run(t, r.Dir, "uncovered")
		if code != Failed || stderr == "" {
			t.Errorf("uncovered = %d, stderr %q, want %d and a reason", code, stderr, Failed)
		}
	})

	t.Run("a run that yields no coverage profile fails the run", func(t *testing.T) {
		r := baseExitModule(t)
		r.Write("noop", "#!/bin/sh\nexit 0\n")
		if err := os.Chmod(filepath.Join(r.Dir, "noop"), 0o755); err != nil {
			t.Fatal(err)
		}
		r.Write("shed.toml", "[proofs]\nrunner = [\"./noop\"]\n")
		_, stderr, code := run(t, r.Dir, "uncovered")
		if code != Failed || stderr == "" {
			t.Errorf("uncovered = %d, stderr %q, want %d and a reason", code, stderr, Failed)
		}
	})
}
