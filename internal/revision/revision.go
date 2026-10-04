// Package revision reads repository files from the working tree or from a
// git revision. It only reads; it never changes the repository.
package revision

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// ErrNotExist reports a file or directory missing from a source.
var ErrNotExist = fs.ErrNotExist

// Source is a read-only view of the repository at one revision. Paths are
// slash-separated and relative to the repository root.
type Source interface {
	ReadFile(name string) ([]byte, error)
	// Files lists the files under dir, recursively, in lexical order.
	Files(dir string) ([]string, error)
	// Name describes the revision, such as "working tree" or a commit.
	Name() string
}

// Worktree is the working tree rooted at a directory.
type Worktree string

func (w Worktree) Name() string { return "working tree" }

func (w Worktree) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(string(w), filepath.FromSlash(name)))
}

func (w Worktree) Files(dir string) ([]string, error) {
	base := filepath.Join(string(w), filepath.FromSlash(dir))
	var out []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(string(w), p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Git is the repository as recorded at a git revision.
type Git struct {
	Root string
	Rev  string
}

func (g Git) Name() string { return g.Rev }

func (g Git) ReadFile(name string) ([]byte, error) {
	out, err := run(g.Root, "cat-file", "blob", g.Rev+":./"+name)
	if err != nil {
		if exists, _ := g.exists(name); !exists {
			return nil, fmt.Errorf("%s at %s: %w", name, g.Rev, ErrNotExist)
		}
		return nil, err
	}
	return out, nil
}

func (g Git) exists(name string) (bool, error) {
	out, err := run(g.Root, "ls-tree", "--name-only", g.Rev, "--", name)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

func (g Git) Files(dir string) ([]string, error) {
	out, err := run(g.Root, "ls-tree", "-r", "--name-only", g.Rev, "--", dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, path.Clean(line))
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s at %s: %w", dir, g.Rev, ErrNotExist)
	}
	sort.Strings(files)
	return files, nil
}

// HasHistory reports whether root is inside a git repository with at least
// one commit.
func HasHistory(root string) bool {
	_, err := run(root, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// Resolve returns the commit a revision names.
func Resolve(root, rev string) (string, error) {
	out, err := run(root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return strings.TrimSpace(string(out)), nil
}

// History lists, oldest first, the commits reachable from HEAD that touched
// any of the paths.
func History(root string, paths ...string) ([]string, error) {
	return Log(root, "HEAD", paths...)
}

// Log lists, oldest first, the commits reachable from rev that touched any
// of the paths.
func Log(root, rev string, paths ...string) ([]string, error) {
	args := append([]string{"log", "--format=%H", "--reverse", rev, "--"}, paths...)
	out, err := run(root, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return out, nil
}

// IsAncestor reports whether commit is an ancestor of rev, or rev itself. A
// commit the repository does not hold is no ancestor.
func IsAncestor(root, commit, rev string) (bool, error) {
	if _, err := run(root, "cat-file", "-e", commit+"^{commit}"); err != nil {
		return false, nil
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", commit, rev)
	cmd.Dir = root
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return err == nil, err
}

// Commit is a commit's hash, author name, subject line and full message, as
// FirstParentSince reads them.
type Commit struct {
	Hash    string
	Author  string
	Subject string
	Message string
}

// FirstParentSince lists, oldest first, the commits on head's first-parent
// history after since, excluding since itself. It fails when since is not on
// that history.
func FirstParentSince(root, since, head string) ([]Commit, error) {
	const field = "\x01"
	const record = "\x02"
	out, err := run(root, "log", "--first-parent",
		"--format="+record+"%H"+field+"%an"+field+"%s"+field+"%B", head)
	if err != nil {
		return nil, err
	}
	var chain []Commit
	for _, rec := range strings.Split(string(out), record) {
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, field, 4)
		if len(parts) != 4 {
			continue
		}
		chain = append(chain, Commit{
			Hash: parts[0], Author: parts[1], Subject: parts[2],
			Message: strings.TrimRight(parts[3], "\n"),
		})
	}
	idx := slices.IndexFunc(chain, func(c Commit) bool { return c.Hash == since })
	if idx == -1 {
		return nil, fmt.Errorf("%s is not on %s's first-parent history", since, head)
	}
	after := chain[:idx]
	slices.Reverse(after)
	return after, nil
}

// OnlyChanges reports whether a commit, against its first parent, changes
// exactly the named file and no other.
func OnlyChanges(root, commit, file string) (bool, error) {
	out, err := run(root, "diff", "--name-only", commit+"^", commit)
	if err != nil {
		return false, err
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return len(files) == 1 && files[0] == file, nil
}
