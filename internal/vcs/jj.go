// Package vcs performs every version control operation shed makes. Shed works
// in a jj repository colocated with git: each change unit is a jj change
// descending from main, with a jj workspace of its own under the state
// directory. Agent sessions never touch version control; they get plain
// directories of files, and this package copies their output back onto the
// unit's change.
package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// The supported jj releases: at least MinJJ and below BelowJJ. jj is
// pre-1.0 and its command line still moves, so shed pins it.
const (
	MinJJ   = "0.45.0"
	BelowJJ = "0.46.0"
)

var (
	// ErrJJMissing reports a jj that cannot be found or run.
	ErrJJMissing = errors.New("jj is missing")
	// ErrJJVersion reports a jj outside the supported releases.
	ErrJJVersion = errors.New("unsupported jj version")
)

// CheckJJ runs the jj executable, or jj from PATH when executable is empty,
// and returns the version it reports. It refuses a jj outside the supported
// releases.
func CheckJJ(ctx context.Context, executable string) (string, error) {
	if executable == "" {
		executable = "jj"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrJJMissing, err)
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = jjEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s --version: %v", ErrJJMissing, path, err)
	}
	reported := strings.TrimSpace(string(out))
	version, _ := strings.CutPrefix(reported, "jj ")
	found, ok := parseVersion(version)
	if !ok {
		return "", fmt.Errorf("%w: %s reports %q, which is not a jj release", ErrJJVersion, path, reported)
	}
	low, _ := parseVersion(MinJJ)
	high, _ := parseVersion(BelowJJ)
	if slices.Compare(found, low) < 0 || slices.Compare(found, high) >= 0 {
		return version, fmt.Errorf("%w: found jj %s, shed needs at least %s and below %s", ErrJJVersion, version, MinJJ, BelowJJ)
	}
	return version, nil
}

// parseVersion reads the major, minor and patch numbers of a release such as
// 0.45.1 or 0.45.1-7c41cdeb.
func parseVersion(version string) ([]int, bool) {
	release, _, _ := strings.Cut(version, "-")
	release, _, _ = strings.Cut(release, "+")
	fields := strings.Split(release, ".")
	if len(fields) != 3 {
		return nil, false
	}
	numbers := make([]int, 3)
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return nil, false
		}
		numbers[i] = n
	}
	return numbers, true
}

// jjSettings apply to every jj command; the owner's own jj configuration
// does not. Conflicts are written with git's markers, every file is tracked
// whatever its size, and a moved bookmark abandons nothing a workspace
// stands on.
var jjSettings = []string{
	"ui.conflict-marker-style=git",
	"snapshot.auto-track=all()",
	"snapshot.max-new-file-size=1099511627776",
	"git.abandon-unreachable-commits=false",
}

// jjEnv is the environment jj runs in: the process's own, without jj's
// variables, and with the owner's jj configuration replaced by none. Git
// credentials and configuration stay, so fetch and push work.
func jjEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "JJ_") {
			env = append(env, kv)
		}
	}
	return append(env, "JJ_CONFIG="+os.DevNull, "GIT_TERMINAL_PROMPT=0")
}

// JJError is a jj or git command that failed, with what it said.
type JJError struct {
	Args   []string
	Err    error
	Stderr string
}

func (e *JJError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: %v", strings.Join(e.Args, " "), e.Err)
	}
	return fmt.Sprintf("%s: %s", strings.Join(e.Args, " "), e.Stderr)
}

func (e *JJError) Unwrap() error { return e.Err }

// run runs jj in a workspace directory as the given identity and returns
// its trimmed standard output.
func (r *Repo) run(ctx context.Context, dir string, id Identity, args ...string) (string, error) {
	full := []string{"--no-pager", "--color=never", "-R", dir,
		"--config", "user.name=" + id.Name, "--config", "user.email=" + id.Email}
	for _, s := range jjSettings {
		full = append(full, "--config", s)
	}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, r.jj, full...)
	cmd.Dir = dir
	cmd.Env = jjEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("jj %s: %w", args[0], ctxErr)
		}
		return "", &JJError{Args: append([]string{"jj"}, args...), Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(stdout.String()), nil
}

// git runs git in the repository root and returns its trimmed output.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", &JJError{Args: append([]string{"git"}, args...), Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(stdout.String()), nil
}
