package vcs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ArchiveBranch holds the archive: what the factory decided not to do. It
// shares no history with main, so writing it never moves main.
const ArchiveBranch = "shed/archive"

// ArchiveRef is the archive branch's full ref.
const ArchiveRef = "refs/heads/" + ArchiveBranch

// WriteArchive commits a file to the archive branch, creating the branch the
// first time, and pushes it when a remote is set. The owner's checkout and
// index are not touched.
func (r *Repo) WriteArchive(ctx context.Context, path string, content []byte, message string) (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	index, err := os.CreateTemp(r.state, "archive-index-*")
	if err != nil {
		return "", err
	}
	index.Close()
	os.Remove(index.Name())
	defer os.Remove(index.Name())
	env := []string{"GIT_INDEX_FILE=" + index.Name(),
		"GIT_AUTHOR_NAME=" + shedIdentity.Name, "GIT_AUTHOR_EMAIL=" + shedIdentity.Email,
		"GIT_COMMITTER_NAME=" + shedIdentity.Name, "GIT_COMMITTER_EMAIL=" + shedIdentity.Email}

	parent, _ := r.gitEnv(ctx, env, nil, "rev-parse", "--verify", "-q", ArchiveRef)
	if parent != "" {
		if _, err := r.gitEnv(ctx, env, nil, "read-tree", parent); err != nil {
			return "", err
		}
	}
	blob, err := r.gitEnv(ctx, env, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	if _, err := r.gitEnv(ctx, env, nil, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+filepath.ToSlash(path)); err != nil {
		return "", err
	}
	tree, err := r.gitEnv(ctx, env, nil, "write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	commit, err := r.gitEnv(ctx, env, nil, args...)
	if err != nil {
		return "", err
	}
	old := parent
	if old == "" {
		old = strings.Repeat("0", len(commit))
	}
	if _, err := r.gitEnv(ctx, env, nil, "update-ref", ArchiveRef, commit, old); err != nil {
		return "", err
	}
	if r.opts.Remote != "" {
		if _, err := r.gitEnv(ctx, nil, nil, "push", "-q", r.opts.Remote, ArchiveRef+":"+ArchiveRef); err != nil {
			return "", fmt.Errorf("pushing the archive: %w", err)
		}
	}
	return commit, nil
}

// gitEnv runs git in the repository root with extra environment and input.
func (r *Repo) gitEnv(ctx context.Context, env []string, stdin []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(), env...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", &JJError{Args: append([]string{"git"}, args...), Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(stdout.String()), nil
}
