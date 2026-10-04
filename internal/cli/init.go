package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// initCmd checks that the repository root and a design document qualify for
// initialising (S.init.1, S.init.2). It never changes the working tree,
// git's state or the state directory.
func (e env) initCmd(args []string) int {
	if len(args) != 1 {
		return e.misuse("init needs exactly one design document argument")
	}
	docAbs, err := filepath.Abs(args[0])
	if err != nil {
		return e.fail(err)
	}

	var problems []string

	resolvedRoot, rootErr := filepath.EvalSymlinks(e.root)
	topOK := rootErr == nil
	if topOK {
		top, err := gitTopLevel(e.ctx, resolvedRoot)
		topOK = err == nil && top == resolvedRoot
	}
	if !topOK {
		problems = append(problems, fmt.Sprintf("%s is not the top of a git working tree", e.root))
	}

	switch info, statErr := os.Stat(docAbs); {
	case statErr != nil:
		problems = append(problems, fmt.Sprintf("%s does not exist", docAbs))
	case !info.Mode().IsRegular():
		problems = append(problems, fmt.Sprintf("%s is not a regular file", docAbs))
	case info.Size() == 0:
		problems = append(problems, fmt.Sprintf("%s is empty", docAbs))
	default:
		data, err := os.ReadFile(docAbs)
		if err != nil {
			return e.fail(err)
		}
		if !utf8.Valid(data) {
			problems = append(problems, fmt.Sprintf("%s is not valid UTF-8 text", docAbs))
		}
	}

	if topOK {
		extra, err := otherRepositoryFiles(e.ctx, resolvedRoot, docAbs)
		if err != nil {
			return e.fail(err)
		}
		if len(extra) > 0 {
			problems = append(problems, fmt.Sprintf(
				"an existing codebase is adopted rather than initialised: %s", strings.Join(extra, ", ")))
		}
	}

	if len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintln(e.stderr, p)
		}
		return Failed
	}
	fmt.Fprintln(e.stdout, "ok: the repository and design document qualify for initialising")
	return OK
}

// gitTopLevel returns the top of the git working tree dir is in.
func gitTopLevel(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// otherRepositoryFiles lists, in path name order, every file under root that
// git does not ignore, tracked or not, except the design document and the
// files S.init.2 exempts at the root.
func otherRepositoryFiles(ctx context.Context, root, docAbs string) ([]string, error) {
	docCompare := docAbs
	if resolved, err := filepath.EvalSymlinks(docAbs); err == nil {
		docCompare = resolved
	}
	rels, err := gitWorkingTreeFiles(ctx, root)
	if err != nil {
		return nil, err
	}
	var extra []string
	for _, rel := range rels {
		if filepath.Join(root, filepath.FromSlash(rel)) == docCompare {
			continue
		}
		if exemptAtRoot(rel) {
			continue
		}
		extra = append(extra, rel)
	}
	sort.Strings(extra)
	return extra, nil
}

// exemptAtRoot reports whether rel, slash-separated and relative to the
// root, is a README, LICENSE, .gitignore or .gitattributes file at the root.
func exemptAtRoot(rel string) bool {
	if strings.Contains(rel, "/") {
		return false
	}
	if rel == ".gitignore" || rel == ".gitattributes" {
		return true
	}
	for _, stem := range []string{"README", "LICENSE"} {
		if rel == stem || strings.HasPrefix(rel, stem+".") {
			return true
		}
	}
	return false
}

// gitWorkingTreeFiles lists the files under root that git does not ignore,
// tracked or not, slash-separated and relative to root.
func gitWorkingTreeFiles(ctx context.Context, root string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", root,
		"ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if f == "" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f))); err != nil {
			continue
		}
		files = append(files, filepath.ToSlash(f))
	}
	return files, nil
}
