package vcs

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
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

// ExportMain copies the files of a commit on main into dir, which must not
// exist or be empty. The directory gets plain files only: no .git, no .jj,
// as Export does for a unit's change (S.sweep.1, S.vcs.2). It reads the
// commit straight from git's object store, so it touches neither a jj
// workspace nor the owner's working copy.
func (r *Repo) ExportMain(ctx context.Context, commit, dir string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "archive", commit)
	cmd.Dir = r.root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return &JJError{Args: []string{"git", "archive", commit}, Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return extractTar(bytes.NewReader(out), dir)
}

// extractTar extracts a tar stream into dir, keeping regular files' modes
// and symlinks as symlinks.
func extractTar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
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

// Restore makes the files at paths on a unit's change, each a directory or
// a file relative to the repository root, exactly those of commit rebased
// onto the commit the change is based on: each takes its content there, and
// files at paths absent there are removed. Every other file is left as it
// is. Where main changed a file since commit's parent, main's change stays,
// and a clash between the two is stored in the file as a conflict. It
// returns the change's new commit.
func (r *Repo) Restore(ctx context.Context, change, commit string, paths ...string) (_ string, err error) {
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
	base, err := r.Base(ctx, change)
	if err != nil {
		return "", err
	}
	done, err := r.checkpoint(ctx, "restore "+change)
	if err != nil {
		return "", err
	}
	defer func() { err = done(err) }()

	// A scratch commit holds commit's files on commit's parent, and is
	// rebased onto the change's base to carry them over main's changes.
	marker := "shed: restore " + nonce()
	if _, err := r.jj(ctx, "new", "--no-edit", "-m", marker, commit+"-"); err != nil {
		return "", err
	}
	scratch, err := r.log(ctx, fmt.Sprintf("description(substring:%q)", marker), "commit_id")
	if err != nil {
		return "", err
	}
	if scratch == "" || strings.Contains(scratch, "\n") {
		return "", fmt.Errorf("restoring unit %s: the scratch commit cannot be found", change)
	}
	id, err := r.log(ctx, scratch, "change_id")
	if err != nil {
		return "", err
	}
	if _, err := r.jj(ctx, "restore", "--from", commit, "--into", changeRevset(id)); err != nil {
		return "", err
	}
	if _, err := r.jj(ctx, "rebase", "-r", changeRevset(id), "-d", base); err != nil {
		return "", err
	}
	args := []string{"restore", "--from", changeRevset(id)}
	for _, p := range paths {
		args = append(args, fmt.Sprintf("root:%q", p))
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, args...); err != nil {
		return "", err
	}
	if _, err := r.jj(ctx, "abandon", changeRevset(id)); err != nil {
		return "", err
	}
	return r.Commit(ctx, change)
}

// Changed lists the files a unit's change adds, changes or removes against
// its parent, slash-separated and relative to the repository root, and
// returns the parent commit.
func (r *Repo) Changed(ctx context.Context, change string) ([]string, string, error) {
	parent, err := r.log(ctx, changeRevset(change)+"-", "commit_id")
	if err != nil {
		return nil, "", err
	}
	out, err := r.jj(ctx, "diff", "--name-only", "-r", changeRevset(change))
	if err != nil {
		return nil, "", err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files = append(files, filepath.ToSlash(line))
		}
	}
	return files, parent, nil
}

// DiffNames lists the files that differ between two commits, slash-separated
// and relative to the repository root: a file present on one side and absent
// on the other counts as differing.
func (r *Repo) DiffNames(ctx context.Context, from, to string) ([]string, error) {
	out, err := r.git(ctx, "diff", "--name-only", from, to)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}
