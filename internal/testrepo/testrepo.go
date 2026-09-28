// Package testrepo builds throwaway repositories for tests: documents on disk
// and, when asked, a git history.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a temporary repository directory.
type Repo struct {
	t   testing.TB
	Dir string
	// Remote is the bare repository origin points at, when there is one.
	Remote string
}

// New returns an empty repository directory.
func New(t testing.TB) *Repo {
	t.Helper()
	return &Repo{t: t, Dir: t.TempDir()}
}

// Minimal returns a repository holding a valid charter, horizon and spec.
func Minimal(t testing.TB) *Repo {
	r := New(t)
	r.Write("charter.md", Charter)
	r.Write("horizon.md", Horizon)
	r.Write("spec/core.md", Spec)
	return r
}

// Charter, Horizon and Spec are small valid documents that cite each other.
const (
	Charter = `# Charter

- **C1** The tool greets people.
- **C2** It never shouts.
`
	Horizon = `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`
	Spec = `# Core

- **S.core.1** (H.greet.1) Running the tool prints hello.
`
)

// Write writes a file, creating its directories.
func (r *Repo) Write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// Remove deletes a file or directory.
func (r *Repo) Remove(path string) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.Dir, filepath.FromSlash(path))); err != nil {
		r.t.Fatal(err)
	}
}

// RequireJJ skips the test when jj is not installed. When SHED_REQUIRE_JJ is
// 1, as in the Dagger checks, a missing jj fails the test instead.
func RequireJJ(t testing.TB) {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("SHED_REQUIRE_JJ") == "1" {
			t.Fatalf("jj is required: %v", err)
		}
		t.Skip("jj is not installed")
	}
}

// Colocated returns a repository holding the Minimal documents and a
// .gitignore for the state directory, committed on main, pushed to a bare
// remote named origin and colocated with jj.
func Colocated(t testing.TB) *Repo {
	t.Helper()
	RequireJJ(t)
	r := Minimal(t)
	r.Write(".gitignore", ".shed/\n")
	r.Init()
	r.Commit("documents")
	r.Remote = t.TempDir()
	r.gitIn(r.Remote, "init", "-q", "--bare", "-b", "main")
	r.Git("remote", "add", "origin", r.Remote)
	r.Git("push", "-q", "origin", "main")
	r.JJ("git", "init", "--colocate")
	return r
}

// JJ runs jj in the repository without the user's configuration and
// returns its trimmed output.
func (r *Repo) JJ(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("jj", append([]string{"--no-pager", "--color=never",
		"--config", "user.name=owner", "--config", "user.email=owner@example.com"}, args...)...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), "JJ_CONFIG="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Git runs git in the repository with a fixed identity and no user or
// system configuration, and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return r.gitIn(r.Dir, args...)
}

// GitRemote runs git in the bare remote.
func (r *Repo) GitRemote(args ...string) string {
	r.t.Helper()
	return r.gitIn(r.Remote, args...)
}

func (r *Repo) gitIn(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=shed", "GIT_AUTHOR_EMAIL=shed@example.com",
		"GIT_COMMITTER_NAME=shed", "GIT_COMMITTER_EMAIL=shed@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Init makes the directory a git repository on branch main.
func (r *Repo) Init() {
	r.t.Helper()
	r.Git("init", "-q", "-b", "main")
}

// Commit stages everything and commits it, returning the commit hash.
func (r *Repo) Commit(msg string) string {
	r.t.Helper()
	r.Git("add", "-A")
	r.Git("commit", "-q", "--allow-empty", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}
