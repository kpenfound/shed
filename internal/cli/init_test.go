package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

// bareGit returns an empty git repository with no commits and none of the
// factory documents: init must not need them (S.doc.1 is used only to pick
// the root), and unlike S.vcs.1 no jj repository is involved.
func bareGit(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Init()
	return r
}

//shed:proves S.init.1
func TestInitQualifies(t *testing.T) {
	r := bareGit(t)
	r.Write("design.md", "# Design\n\nBuild a thing.\n")

	stdout, stderr, code := run(t, r.Dir, "init", filepath.Join(r.Dir, "design.md"))
	if code != OK || stderr != "" {
		t.Fatalf("init = %d, stderr %q", code, stderr)
	}
	if want := "ok: the repository and design document qualify for initialising\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".shed")); err == nil {
		t.Error("init created the state directory")
	}
}

//shed:proves S.init.1
func TestInitArgumentCount(t *testing.T) {
	r := bareGit(t)
	r.Write("design.md", "# Design\n")

	if _, stderr, code := run(t, r.Dir, "init"); code != Misused || !strings.Contains(stderr, "design document") {
		t.Errorf("init with no arguments = %d, %q", code, stderr)
	}
	if _, stderr, code := run(t, r.Dir, "init", "design.md", "other.md"); code != Misused || !strings.Contains(stderr, "design document") {
		t.Errorf("init with two arguments = %d, %q", code, stderr)
	}
}

//shed:proves S.init.1
func TestInitRequiresTopOfGitWorkingTree(t *testing.T) {
	plain := t.TempDir()
	doc := filepath.Join(plain, "design.md")
	if err := os.WriteFile(doc, []byte("# Design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, plain, "init", doc); code != Failed || !strings.Contains(stderr, "not the top of a git working tree") {
		t.Errorf("init outside a git repository = %d, %q", code, stderr)
	}

	r := bareGit(t)
	r.Write("sub/design.md", "# Design\n")
	sub := filepath.Join(r.Dir, "sub")
	if _, stderr, code := run(t, sub, "init", filepath.Join(sub, "design.md")); code != Failed ||
		!strings.Contains(stderr, "not the top of a git working tree") {
		t.Errorf("init from a subdirectory of a git repository = %d, %q", code, stderr)
	}
}

//shed:proves S.init.1
func TestInitChecksTheDesignDocument(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, r *testrepo.Repo) string
		want  string
	}{
		{"missing", func(t *testing.T, r *testrepo.Repo) string {
			return filepath.Join(r.Dir, "missing.md")
		}, "does not exist"},
		{"not a regular file", func(t *testing.T, r *testrepo.Repo) string {
			dir := filepath.Join(r.Dir, "adir")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}, "is not a regular file"},
		{"empty", func(t *testing.T, r *testrepo.Repo) string {
			r.Write("empty.md", "")
			return filepath.Join(r.Dir, "empty.md")
		}, "is empty"},
		{"not valid UTF-8", func(t *testing.T, r *testrepo.Repo) string {
			path := filepath.Join(r.Dir, "bad.md")
			if err := os.WriteFile(path, []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
				t.Fatal(err)
			}
			return path
		}, "is not valid UTF-8 text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := bareGit(t)
			doc := tc.setup(t, r)
			_, stderr, code := run(t, r.Dir, "init", doc)
			if code != Failed {
				t.Fatalf("init with a document that %s = %d, stderr %q", tc.name, code, stderr)
			}
			lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
			if len(lines) != 1 || !strings.Contains(lines[0], tc.want) || !strings.Contains(lines[0], doc) {
				t.Errorf("stderr = %q, want one line containing %q and %q", stderr, tc.want, doc)
			}
		})
	}
}

//shed:proves S.init.1
func TestInitReportsEveryFailingCheck(t *testing.T) {
	plain := t.TempDir()
	doc := filepath.Join(plain, "missing.md")

	_, stderr, code := run(t, plain, "init", doc)
	if code != Failed {
		t.Fatalf("init = %d", code)
	}
	lines := strings.Split(strings.TrimRight(stderr, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("stderr =\n%s\nwant 2 lines", stderr)
	}
	if !strings.Contains(lines[0], "not the top of a git working tree") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "does not exist") {
		t.Errorf("second line = %q", lines[1])
	}
}

// TestInitDocumentResolvesAgainstCwdNotRoot checks that a relative document
// path is resolved against the directory shed runs in, not the root named
// with -C: the same relative argument finds the document when the process
// runs in the root, and misses it when the process runs one level up, even
// though -C still names the root both times.
//
//shed:proves S.init.1
func TestInitDocumentResolvesAgainstCwdNotRoot(t *testing.T) {
	r := bareGit(t)
	r.Write("sub/design.md", "# Design\n")

	t.Chdir(r.Dir)
	stdout, stderr, code := run(t, r.Dir, "init", "sub/design.md")
	if code != OK || stderr != "" {
		t.Fatalf("init run from the root = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "qualify") {
		t.Errorf("stdout = %q", stdout)
	}

	t.Chdir(filepath.Dir(r.Dir))
	_, stderr, code = run(t, r.Dir, "init", "sub/design.md")
	if code != Failed || !strings.Contains(stderr, "does not exist") {
		t.Errorf("init run one level up with the same relative path = %d, %q", code, stderr)
	}
}

// TestInitMatchesDocumentThroughSymlinkedRoot checks that S.init.2's
// comparison resolves symbolic links on both the root and the document
// path, so a root reached through a symlink, or a document named through
// one, still matches the document inside the real directory.
//
//shed:proves S.init.1
func TestInitMatchesDocumentThroughSymlinkedRoot(t *testing.T) {
	r := bareGit(t)
	r.Write("design.md", "# Design\n")

	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(r.Dir, link); err != nil {
		t.Fatal(err)
	}

	// The root is named through the symlink; the document is named
	// through the real directory. Only resolving the root's symlinks
	// makes these match.
	stdout, stderr, code := run(t, link, "init", filepath.Join(r.Dir, "design.md"))
	if code != OK || stderr != "" {
		t.Fatalf("init with a symlinked root = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "qualify") {
		t.Errorf("stdout = %q", stdout)
	}

	// The root is named directly; the document is named through the
	// symlink. Only resolving the document's symlinks makes these match.
	stdout, stderr, code = run(t, r.Dir, "init", filepath.Join(link, "design.md"))
	if code != OK || stderr != "" {
		t.Fatalf("init with a symlinked document path = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "qualify") {
		t.Errorf("stdout = %q", stdout)
	}
}

//shed:proves S.init.1
func TestInitChangesNothing(t *testing.T) {
	r := bareGit(t)
	r.Write("design.md", "# Design\n")
	doc := filepath.Join(r.Dir, "design.md")

	beforeStatus := r.Git("status", "--porcelain")
	beforeRefs := r.Git("for-each-ref")
	beforeCount := r.Git("rev-list", "--all", "--count")

	if _, _, code := run(t, r.Dir, "init", doc); code != OK {
		t.Fatalf("init succeeding = %d", code)
	}
	if _, _, code := run(t, r.Dir, "init", filepath.Join(r.Dir, "missing.md")); code != Failed {
		t.Fatalf("init failing = %d", code)
	}

	afterStatus := r.Git("status", "--porcelain")
	afterRefs := r.Git("for-each-ref")
	afterCount := r.Git("rev-list", "--all", "--count")
	if beforeStatus != afterStatus || beforeRefs != afterRefs || beforeCount != afterCount {
		t.Errorf("init changed git state: status %q -> %q, refs %q -> %q, commits %q -> %q",
			beforeStatus, afterStatus, beforeRefs, afterRefs, beforeCount, afterCount)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, ".shed")); err == nil {
		t.Error("init created the state directory")
	}
}

//shed:proves S.init.2
func TestInitNearlyEmptyWithNoCommits(t *testing.T) {
	r := bareGit(t)
	if count := r.Git("rev-list", "--all", "--count"); count != "0" {
		t.Fatalf("fresh repository has %s commits", count)
	}
	r.Write("design.md", "# Design\n")
	if _, stderr, code := run(t, r.Dir, "init", filepath.Join(r.Dir, "design.md")); code != OK || stderr != "" {
		t.Errorf("init in a repository with no commits = %d, stderr %q", code, stderr)
	}
}

//shed:proves S.init.2
func TestInitNearlyEmpty(t *testing.T) {
	r := bareGit(t)
	r.Write("design.md", "# Design\n")
	r.Write("README", "readme\n")
	r.Write("README.md", "readme\n")
	r.Write("LICENSE", "license\n")
	r.Write("LICENSE.txt", "license\n")
	r.Write(".gitignore", "ignored.log\n")
	r.Write(".gitattributes", "* text\n")
	r.Write("ignored.log", "noise\n")
	doc := filepath.Join(r.Dir, "design.md")

	stdout, stderr, code := run(t, r.Dir, "init", doc)
	if code != OK || stderr != "" {
		t.Fatalf("init with only exempt files = %d, stderr %q\n%s", code, stderr, stdout)
	}

	// A README nested in a subdirectory, an untracked file and a tracked
	// file are not exempt, and none is ignored.
	r.Write("sub/README.md", "nested, not exempt\n")
	r.Write("b.go", "package main\n")
	r.Write("a.txt", "notes\n")
	r.Git("add", "b.go")

	_, stderr, code = run(t, r.Dir, "init", doc)
	if code != Failed {
		t.Fatalf("init with extra files = %d", code)
	}
	want := "an existing codebase is adopted rather than initialised: a.txt, b.go, sub/README.md\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
