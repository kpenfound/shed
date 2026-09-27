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

// Runner runs proofs with go test.
type Runner struct {
	Root string
	// Prefix is prepended to the go test command line, so that proofs can
	// run somewhere other than the host.
	Prefix []string
	Stderr io.Writer
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

	args := append([]string{}, r.Prefix...)
	if len(args) > 0 && strings.ContainsRune(args[0], '/') && !filepath.IsAbs(args[0]) {
		args[0] = filepath.Join(r.Root, args[0])
	}
	args = append(args, "go", "test", "-json", "-count=1", "-run", "^("+strings.Join(quoteAll(tests), "|")+")$")
	for _, d := range dirs {
		if d == "." {
			args = append(args, ".")
		} else {
			args = append(args, "./"+d)
		}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = r.Root
	cmd.Stderr = r.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return nil, fmt.Errorf("running proofs: %w", err)
	}

	scan := bufio.NewScanner(bytes.NewReader(out))
	scan.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scan.Scan() {
		var ev struct {
			Action  string
			Package string
			Test    string
		}
		if json.Unmarshal(scan.Bytes(), &ev) != nil || ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue
		}
		name, ok := byTest[ev.Package+"."+ev.Test]
		if !ok {
			continue
		}
		switch ev.Action {
		case "pass":
			results[name] = Pass
		case "fail":
			results[name] = Fail
		case "skip":
			results[name] = Skip
		}
	}
	return results, scan.Err()
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
