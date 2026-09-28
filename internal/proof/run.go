package proof

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
)

// Status is the outcome of one proof.
type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
	// NoResult means the test never reported, for example because its
	// package did not build.
	NoResult Status = "no result"
)

// Runner runs proofs and tests with go test.
type Runner struct {
	// Root is the module the tests run in.
	Root string
	// Prefix is prepended to the go test command line, so that tests can
	// run somewhere other than the host. A relative first element is
	// resolved from Root.
	Prefix []string
	Stderr io.Writer
}

// TestResult is one top-level test's outcome and output.
type TestResult struct {
	Package string
	Test    string
	Status  Status
	Output  string
}

// Report is what a go test run produced: each top-level test, and output
// that belongs to no test, such as build errors.
type Report struct {
	Tests  []TestResult
	Output string
}

// Failed reports whether any test failed or the run produced no tests.
func (r Report) Failed() bool {
	if len(r.Tests) == 0 {
		return true
	}
	return slices.ContainsFunc(r.Tests, func(t TestResult) bool { return t.Status != Pass && t.Status != Skip })
}

// Test runs go test in package directories, relative to Root, limited to the
// tests the run pattern matches when it is not empty.
func (r Runner) Test(ctx context.Context, dirs []string, run string) (Report, error) {
	args := append([]string{}, r.Prefix...)
	if len(args) > 0 && strings.ContainsRune(args[0], '/') && !filepath.IsAbs(args[0]) {
		args[0] = filepath.Join(r.Root, args[0])
	}
	args = append(args, "go", "test", "-json", "-count=1")
	if run != "" {
		args = append(args, "-run", run)
	}
	if len(dirs) == 0 {
		dirs = []string{"./..."}
	}
	for _, d := range dirs {
		switch {
		case d == "." || strings.HasPrefix(d, "./"):
			args = append(args, d)
		default:
			args = append(args, "./"+d)
		}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = r.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.Stderr)
	}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return Report{}, fmt.Errorf("running tests: %w", err)
	}

	var report Report
	index := map[string]int{}
	var loose strings.Builder
	scan := bufio.NewScanner(bytes.NewReader(out))
	scan.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scan.Scan() {
		var ev struct {
			Action  string
			Package string
			Test    string
			Output  string
		}
		if json.Unmarshal(scan.Bytes(), &ev) != nil {
			continue
		}
		name, _, _ := strings.Cut(ev.Test, "/")
		if name == "" {
			if ev.Action == "output" || ev.Action == "build-output" {
				loose.WriteString(ev.Output)
			}
			continue
		}
		key := ev.Package + "." + name
		i, ok := index[key]
		if !ok {
			i = len(report.Tests)
			index[key] = i
			report.Tests = append(report.Tests, TestResult{Package: ev.Package, Test: name, Status: NoResult})
		}
		t := &report.Tests[i]
		switch {
		case ev.Action == "output":
			t.Output += ev.Output
		case ev.Test != name:
			// A subtest's outcome does not decide its parent's.
		case ev.Action == "pass":
			t.Status = Pass
		case ev.Action == "fail":
			t.Status = Fail
		case ev.Action == "skip":
			t.Status = Skip
		}
	}
	report.Output = loose.String()
	if len(report.Tests) == 0 && report.Output == "" {
		report.Output = stderr.String()
	}
	return report, scan.Err()
}

// Run runs the proofs and returns each one's status by Name.
func (r Runner) Run(ctx context.Context, proofs []Proof) (map[string]Status, error) {
	results := map[string]Status{}
	if len(proofs) == 0 {
		return results, nil
	}
	module, err := modulePath(r.Root)
	if err != nil {
		return nil, err
	}
	var tests, dirs []string
	byTest := map[string]string{}
	for _, p := range proofs {
		results[p.Name()] = NoResult
		if !slices.Contains(tests, p.Test) {
			tests = append(tests, p.Test)
		}
		if !slices.Contains(dirs, p.Dir) {
			dirs = append(dirs, p.Dir)
		}
		byTest[importPath(module, p.Dir)+"."+p.Test] = p.Name()
	}
	slices.Sort(tests)
	slices.Sort(dirs)
	report, err := r.Test(ctx, dirs, "^("+strings.Join(quoteAll(tests), "|")+")$")
	if err != nil {
		return nil, err
	}
	for _, t := range report.Tests {
		if name, ok := byTest[t.Package+"."+t.Test]; ok {
			results[name] = t.Status
		}
	}
	return results, nil
}

// ClauseResult is the outcome of a clause's proofs. A clause passes only when
// it has proofs and every one of them passes.
type ClauseResult struct {
	ID     clause.ID
	Pass   bool
	Proofs map[string]Status
}

// ByClause folds proof results into clause results.
func ByClause(ids []clause.ID, proofs []Proof, results map[string]Status) []ClauseResult {
	var out []ClauseResult
	for _, id := range ids {
		cr := ClauseResult{ID: id, Proofs: map[string]Status{}}
		for _, p := range For(proofs, id) {
			cr.Proofs[p.Name()] = results[p.Name()]
		}
		cr.Pass = len(cr.Proofs) > 0
		for _, s := range cr.Proofs {
			if s != Pass {
				cr.Pass = false
			}
		}
		out = append(out, cr)
	}
	return out
}

var modulePattern = regexp.MustCompile(`(?m)^module\s+"?([^\s"]+)"?`)

func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("proofs need a Go module at the repository root: %w", err)
	}
	m := modulePattern.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("go.mod names no module")
	}
	return string(m[1]), nil
}

func importPath(module, dir string) string {
	if dir == "." || dir == "" {
		return module
	}
	return module + "/" + dir
}

func quoteAll(tests []string) []string {
	out := make([]string, len(tests))
	for i, t := range tests {
		out[i] = regexp.QuoteMeta(t)
	}
	return out
}
