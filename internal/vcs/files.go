package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// vcsDirs are directory names never copied into or out of a session's
// directory, at any depth.
var vcsDirs = map[string]bool{".git": true, ".jj": true}

// Export copies the files of a unit's change into dir, which must not
// exist or be empty. The directory gets plain files only: no .git, no .jj,
// nothing a session could run version control against.
func (r *Repo) Export(ctx context.Context, change, dir string) error {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	files, err := r.tracked(ctx, w)
	if err != nil {
		return err
	}
	for _, name := range files {
		if err := copyEntry(filepath.Join(w.dir, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return os.MkdirAll(dir, 0o755)
}

// Capture makes a unit's change hold exactly the files in dir: new and
// changed files are copied onto the unit's workspace, files missing from dir
// are deleted, and the workspace is snapshotted. .git and .jj directories in
// dir are ignored. It returns the change's new commit.
func (r *Repo) Capture(ctx context.Context, change, dir string) (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return "", err
	}
	tracked, err := r.tracked(ctx, w)
	if err != nil {
		return "", err
	}
	present := map[string]bool{}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if vcsDirs[d.Name()] && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		present[filepath.ToSlash(rel)] = true
		return syncEntry(path, filepath.Join(w.dir, rel))
	})
	if err != nil {
		return "", err
	}
	for _, name := range tracked {
		if !present[name] {
			if err := os.Remove(filepath.Join(w.dir, filepath.FromSlash(name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
		}
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, "util", "snapshot"); err != nil {
		return "", err
	}
	return r.Commit(ctx, change)
}

// tracked snapshots a workspace and lists the files its change holds,
// slash-separated and relative to the workspace.
func (r *Repo) tracked(ctx context.Context, w workspace) ([]string, error) {
	out, err := r.run(ctx, w.dir, shedIdentity, "file", "list", "-r", "@", "-T", `path ++ "\n"`)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	return files, nil
}

// copyEntry copies a regular file, keeping its mode, or a symlink as a
// symlink.
func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode().Perm())
}

// syncEntry makes dst the same as src, writing only when they differ.
func syncEntry(src, dst string) error {
	si, err := os.Lstat(src)
	if err != nil {
		return err
	}
	di, err := os.Lstat(dst)
	if err == nil {
		same, err := sameEntry(src, dst, si, di)
		if err != nil || same {
			return err
		}
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return copyEntry(src, dst)
}

func sameEntry(src, dst string, si, di fs.FileInfo) (bool, error) {
	if si.Mode().Type() != di.Mode().Type() {
		return false, nil
	}
	if si.Mode()&fs.ModeSymlink != 0 {
		a, err := os.Readlink(src)
		if err != nil {
			return false, err
		}
		b, err := os.Readlink(dst)
		return a == b, err
	}
	if si.Mode().Perm() != di.Mode().Perm() || si.Size() != di.Size() {
		return false, nil
	}
	a, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(dst)
	return bytes.Equal(a, b), err
}

// Snapshot records what a unit's workspace holds onto its change, such as
// edits the owner made there by hand, and returns the change's commit.
func (r *Repo) Snapshot(ctx context.Context, change string) (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, "util", "snapshot"); err != nil {
		return "", err
	}
	return r.Commit(ctx, change)
}

// Restore makes the files under dir on a unit's change exactly those on
// commit, each with its content there: files under dir absent from commit
// are removed and every other file is left as it is. It returns the
// change's new commit.
func (r *Repo) Restore(ctx context.Context, change, commit, dir string) (string, error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, "restore", "--from", commit, fmt.Sprintf("root:%q", dir)); err != nil {
		return "", err
	}
	return r.Commit(ctx, change)
}
