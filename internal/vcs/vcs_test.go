package vcs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

var ctx = context.Background()

var landingIdentity = Identity{Name: "shed wheelbuilder", Email: "wheelbuilder@shed.localhost"}

func openRepo(t *testing.T, r *testrepo.Repo, remote string) *Repo {
	t.Helper()
	repo, err := Open(ctx, r.Dir, filepath.Join(r.Dir, ".shed"), Options{Remote: remote, Landing: landingIdentity})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// fakeJJ writes an executable that reports a jj version.
func fakeJJ(t *testing.T, version string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jj")
	must(t, os.WriteFile(path, []byte("#!/bin/sh\necho 'jj "+version+"'\n"), 0o755))
	return path
}

// newUnit opens a unit and writes files into its workspace.
func newUnit(t *testing.T, repo *Repo, title string, files map[string]string) string {
	t.Helper()
	change, err := repo.NewUnit(ctx, title)
	must(t, err)
	dir, err := repo.Workspace(ctx, change)
	must(t, err)
	for name, content := range files {
		path := filepath.Join(dir, name)
		must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		must(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return change
}

func message(text string) Message {
	return func(context.Context, string, string) (string, error) { return text, nil }
}

func show(t *testing.T, r *testrepo.Repo, rev, path string) string {
	t.Helper()
	return r.Git("show", rev+":"+path)
}

//shed:proves S.vcs.1
func TestJJVersions(t *testing.T) {
	for version, ok := range map[string]bool{
		"0.45.0": true, "0.45.1": true, "0.45.9-abc123": true,
		"0.44.2": false, "0.46.0": false, "1.0.0": false, "nightly": false,
	} {
		_, err := CheckJJ(ctx, fakeJJ(t, version))
		if (err == nil) != ok {
			t.Errorf("jj %s: %v", version, err)
		}
		if !ok && !errors.Is(err, ErrJJVersion) {
			t.Errorf("jj %s: error %v is not ErrJJVersion", version, err)
		}
	}
	if _, err := CheckJJ(ctx, filepath.Join(t.TempDir(), "no-jj")); !errors.Is(err, ErrJJMissing) {
		t.Errorf("missing jj: %v", err)
	}
}

//shed:proves S.vcs.1
func TestOpenNeedsAColocatedRepository(t *testing.T) {
	testrepo.RequireJJ(t)
	opts := Options{Landing: landingIdentity}

	plain := testrepo.Minimal(t)
	plain.Write(".gitignore", ".shed/\n")
	plain.Init()
	plain.Commit("documents")
	if _, err := Open(ctx, plain.Dir, filepath.Join(plain.Dir, ".shed"), opts); !errors.Is(err, ErrNotColocated) {
		t.Errorf("git without jj: %v", err)
	}

	separate := testrepo.New(t)
	separate.JJ("git", "init", "--no-colocate")
	if _, err := Open(ctx, separate.Dir, filepath.Join(separate.Dir, ".shed"), opts); !errors.Is(err, ErrNotColocated) {
		t.Errorf("jj with its own git store: %v", err)
	}

	r := testrepo.Colocated(t)
	if _, err := Open(ctx, r.Dir, filepath.Join(r.Dir, "state"), opts); err == nil || !strings.Contains(err.Error(), "add state/ to .gitignore") {
		t.Errorf("unignored state directory: %v", err)
	}
	if _, err := Open(ctx, r.Dir, filepath.Join(r.Dir, ".shed"), Options{Landing: landingIdentity, JJ: fakeJJ(t, "0.44.0")}); !errors.Is(err, ErrJJVersion) {
		t.Errorf("old jj: %v", err)
	}
	if _, err := Open(ctx, r.Dir, filepath.Join(t.TempDir(), "outside"), opts); err != nil {
		t.Errorf("state outside the repository: %v", err)
	}
}

//shed:proves S.vcs.3
func TestNewUnitMakesAChangeOnMain(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "")
	main, err := repo.MainCommit(ctx)
	must(t, err)

	change, err := repo.NewUnit(ctx, "Say goodbye")
	must(t, err)
	if len(change) != 32 || strings.Trim(change, "klmnopqrstuvwxyz") != "" {
		t.Errorf("change ID %q", change)
	}
	got := r.JJ("--ignore-working-copy", "log", "--no-graph", "-r", "change_id("+change+")",
		"-T", `description.first_line() ++ " " ++ parents.map(|p| p.commit_id()).join(",")`)
	if got != "unit: Say goodbye "+main {
		t.Errorf("unit change = %q, want a child of %s", got, main)
	}
	dir, err := repo.Workspace(ctx, change)
	must(t, err)
	if filepath.Dir(dir) != filepath.Join(repo.state, WorkspacesDir) {
		t.Errorf("workspace %s is not under the state directory", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "charter.md")); err != nil {
		t.Errorf("workspace lacks the repository's files: %v", err)
	}

	other, err := repo.NewUnit(ctx, "Wave")
	must(t, err)
	if other == change {
		t.Error("two units share a change")
	}
}

//shed:proves S.vcs.2
func TestExportGivesPlainFiles(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "")
	change := newUnit(t, repo, "Say goodbye", map[string]string{"greet/bye.txt": "bye\n"})

	view := filepath.Join(t.TempDir(), "work")
	must(t, repo.Export(ctx, change, view))
	var files []string
	must(t, filepath.WalkDir(view, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(view, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	}))
	slices.Sort(files)
	want := []string{".gitignore", "charter.md", "greet/bye.txt", "horizon.md", "spec/core.md"}
	if !slices.Equal(files, want) {
		t.Errorf("view holds %v, want %v", files, want)
	}
	if err := repo.Export(ctx, change, view); err == nil {
		t.Error("exported into a directory that is not empty")
	}
}

//shed:proves S.vcs.4
func TestCaptureSnapshotsTheDirectory(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "")
	change := newUnit(t, repo, "Say goodbye", map[string]string{"old.txt": "old\n"})
	view := filepath.Join(t.TempDir(), "work")
	must(t, repo.Export(ctx, change, view))

	must(t, os.Remove(filepath.Join(view, "old.txt")))
	must(t, os.WriteFile(filepath.Join(view, "charter.md"), []byte(testrepo.Charter+"- **C3** It waves.\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(view, "bin"), 0o755))
	must(t, os.WriteFile(filepath.Join(view, "bin", "bye"), []byte("#!/bin/sh\necho bye\n"), 0o755))
	must(t, os.Symlink("bin/bye", filepath.Join(view, "bye")))
	must(t, os.MkdirAll(filepath.Join(view, "nested", ".git"), 0o755))
	must(t, os.WriteFile(filepath.Join(view, "nested", ".git", "HEAD"), []byte("ref: x\n"), 0o644))
	must(t, os.MkdirAll(filepath.Join(view, ".jj"), 0o755))
	must(t, os.WriteFile(filepath.Join(view, ".jj", "x"), []byte("x"), 0o644))

	commit, err := repo.Capture(ctx, change, view)
	must(t, err)
	if again, _ := repo.Commit(ctx, change); again != commit {
		t.Errorf("change is at %s, capture returned %s", again, commit)
	}
	files := strings.Fields(r.Git("ls-tree", "-r", "--name-only", commit))
	want := []string{".gitignore", "bin/bye", "bye", "charter.md", "horizon.md", "spec/core.md"}
	if !slices.Equal(files, want) {
		t.Errorf("commit holds %v, want %v", files, want)
	}
	if !strings.Contains(show(t, r, commit, "charter.md"), "**C3** It waves.") {
		t.Error("changed file not captured")
	}
	tree := r.Git("ls-tree", "-r", commit)
	for _, want := range []string{"100755 blob", "120000 blob"} {
		if !strings.Contains(tree, want) {
			t.Errorf("tree lacks %s:\n%s", want, tree)
		}
	}
	if !strings.Contains(r.Git("ls-tree", commit, "bye"), "120000") || show(t, r, commit, "bye") != "bin/bye" {
		t.Error("symlink not captured as a symlink")
	}
}

//shed:proves S.vcs.6 S.vcs.8
func TestLand(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Git("symbolic-ref", "HEAD", "refs/heads/main")
	r.Write("owner-notes.txt", "mine\n")
	repo := openRepo(t, r, "origin")
	before, err := repo.MainCommit(ctx)
	must(t, err)
	change := newUnit(t, repo, "Say goodbye", map[string]string{"bye.txt": "bye\n"})
	dir, err := repo.Workspace(ctx, change)
	must(t, err)

	var gotMain, gotUnit string
	commit, err := repo.Land(ctx, change, func(_ context.Context, main, unit string) (string, error) {
		gotMain, gotUnit = main, unit
		return "Say goodbye\n\nUnit: " + change + "\n", nil
	})
	must(t, err)
	if gotMain != before || gotUnit == "" {
		t.Errorf("message built from main %s and unit %s", gotMain, gotUnit)
	}
	if main, _ := repo.MainCommit(ctx); main != commit {
		t.Errorf("main is at %s, landed %s", main, commit)
	}
	if remote := r.GitRemote("rev-parse", "main"); remote != commit {
		t.Errorf("remote main is at %s, landed %s", remote, commit)
	}
	if parent := r.Git("rev-parse", commit+"^"); parent != before {
		t.Errorf("landed commit's parent is %s, want the old main %s", parent, before)
	}
	if got := r.Git("log", "-1", "--format=%an <%ae>|%cn <%ce>|%B", commit); got !=
		"shed wheelbuilder <wheelbuilder@shed.localhost>|shed wheelbuilder <wheelbuilder@shed.localhost>|Say goodbye\n\nUnit: "+change {
		t.Errorf("landed commit = %q", got)
	}
	if show(t, r, commit, "bye.txt") != "bye" {
		t.Error("landed commit lacks the unit's file")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("workspace still exists: %v", err)
	}
	// The owner's checkout was detached where it was, with its files kept.
	if _, err := os.Stat(filepath.Join(r.Dir, "owner-notes.txt")); err != nil {
		t.Errorf("owner's file: %v", err)
	}
	if head := r.Git("rev-parse", "HEAD"); head != before {
		t.Errorf("HEAD moved to %s", head)
	}
	if out := r.Git("rev-parse", "--symbolic-full-name", "HEAD"); out != "HEAD" {
		t.Errorf("HEAD is still on %s", out)
	}

	again, err := repo.Land(ctx, change, message("unused"))
	if err != nil || again != commit {
		t.Errorf("landing again = %s, %v; want %s", again, err, commit)
	}
}

//shed:proves S.vcs.6 S.vcs.4
func TestLandRebasesOntoMainAndRefusesConflicts(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "origin")
	first := newUnit(t, repo, "First", map[string]string{"greeting.txt": "hello\n"})
	clash := newUnit(t, repo, "Clash", map[string]string{"greeting.txt": "hi\n"})
	apart := newUnit(t, repo, "Apart", map[string]string{"other.txt": "other\n"})
	empty, err := repo.NewUnit(ctx, "Empty")
	must(t, err)

	landed, err := repo.Land(ctx, first, message("First"))
	must(t, err)
	clashBefore, _ := repo.Commit(ctx, clash)
	if _, err := repo.Land(ctx, clash, message("Clash")); !errors.Is(err, ErrConflict) {
		t.Errorf("conflicting unit: %v", err)
	}
	if main, _ := repo.MainCommit(ctx); main != landed {
		t.Errorf("a refused landing moved main to %s", main)
	}
	if after, _ := repo.Commit(ctx, clash); after != clashBefore {
		t.Errorf("the refused landing was not undone: %s became %s", clashBefore, after)
	}
	if dir, err := repo.Workspace(ctx, clash); err != nil || !exists(dir) {
		t.Errorf("conflicting unit's workspace: %v", err)
	}
	if _, err := repo.Land(ctx, empty, message("Empty")); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty unit: %v", err)
	}

	// Someone else's landing reached the remote; landing fetches it first.
	other := t.TempDir()
	r.Git("clone", "-q", r.Remote, other)
	must(t, os.WriteFile(filepath.Join(other, "remote.txt"), []byte("remote\n"), 0o644))
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "remote"}, {"push", "-q", "origin", "main"}} {
		r.Git(append([]string{"-C", other}, args...)...)
	}
	remoteMain := r.GitRemote("rev-parse", "main")

	commit, err := repo.Land(ctx, apart, message("Apart"))
	must(t, err)
	if parent := r.Git("rev-parse", commit+"^"); parent != remoteMain {
		t.Errorf("landed on %s, want the remote's main %s", parent, remoteMain)
	}
	files := strings.Fields(r.Git("ls-tree", "-r", "--name-only", commit))
	for _, f := range []string{"greeting.txt", "other.txt", "remote.txt"} {
		if !slices.Contains(files, f) {
			t.Errorf("landed commit lacks %s", f)
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

//shed:proves S.vcs.5
func TestFailedOperationsAreRestored(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "")
	change := newUnit(t, repo, "Say goodbye", map[string]string{"bye.txt": "bye\n"})
	before, _ := repo.Commit(ctx, change)
	main, _ := repo.MainCommit(ctx)

	// A landing whose message cannot be built fails after the rebase and
	// snapshot, and leaves nothing behind.
	if _, err := repo.Land(ctx, change, func(context.Context, string, string) (string, error) {
		return "", errors.New("no message")
	}); err == nil || !strings.Contains(err.Error(), "no message") {
		t.Fatalf("land: %v", err)
	}
	if now, _ := repo.MainCommit(ctx); now != main {
		t.Errorf("main moved to %s", now)
	}
	if after, _ := repo.Commit(ctx, change); after != before {
		t.Errorf("unit changed from %s to %s", before, after)
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.state, CheckpointsDir)); len(entries) != 0 {
		t.Errorf("checkpoints left behind: %d", len(entries))
	}

	// A shed process stopped midway leaves its checkpoint; the next Open
	// restores the repository to it.
	if _, err := repo.checkpoint(ctx, "interrupted"); err != nil {
		t.Fatal(err)
	}
	crashed, err := repo.NewUnit(ctx, "Half made")
	must(t, err)
	// Midway through, as shed would, main moved in jj and not yet in git.
	r.JJ("-R", filepath.Join(repo.state, WorkspacesDir, baseDir), "--ignore-working-copy",
		"bookmark", "set", "main", "-r", "change_id("+change+")")

	reopened := openRepo(t, r, "")
	if now, _ := reopened.MainCommit(ctx); now != main {
		t.Errorf("after recovery main is at %s, want %s", now, main)
	}
	if _, err := reopened.Workspace(ctx, crashed); err == nil {
		t.Error("the interrupted operation's unit survived recovery")
	}
	if _, err := reopened.Workspace(ctx, change); err != nil {
		t.Errorf("the earlier unit was lost: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.state, WorkspacesDir)); len(entries) != 2 {
		t.Errorf("workspace directories after recovery: %d, want base and one unit", len(entries))
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.state, CheckpointsDir)); len(entries) != 0 {
		t.Errorf("checkpoints left after recovery: %d", len(entries))
	}
}

//shed:proves S.vcs.8
func TestOnlyLandingMovesMain(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "origin")
	main, _ := repo.MainCommit(ctx)
	remote := r.GitRemote("rev-parse", "main")
	unchanged := func(step string) {
		t.Helper()
		if now, _ := repo.MainCommit(ctx); now != main {
			t.Errorf("%s moved main to %s", step, now)
		}
		if now := r.GitRemote("rev-parse", "main"); now != remote {
			t.Errorf("%s moved the remote's main to %s", step, now)
		}
		if now := r.Git("rev-parse", "main"); now != main {
			t.Errorf("%s moved git's main to %s", step, now)
		}
	}
	change := newUnit(t, repo, "Say goodbye", map[string]string{"bye.txt": "bye\n"})
	unchanged("opening a unit")
	view := filepath.Join(t.TempDir(), "work")
	must(t, repo.Export(ctx, change, view))
	must(t, os.WriteFile(filepath.Join(view, "more.txt"), []byte("more\n"), 0o644))
	_, err := repo.Capture(ctx, change, view)
	must(t, err)
	unchanged("exporting and capturing")
	discard := newUnit(t, repo, "Discard me", map[string]string{"x.txt": "x\n"})
	must(t, repo.Discard(ctx, discard))
	unchanged("discarding a unit")
	if _, err := repo.Land(ctx, change, func(context.Context, string, string) (string, error) {
		return "", errors.New("stop")
	}); err == nil {
		t.Fatal("landing without a message succeeded")
	}
	unchanged("a failed landing")

	commit, err := repo.Land(ctx, change, message("Say goodbye"))
	must(t, err)
	if parent := r.Git("rev-parse", commit+"^"); parent != main {
		t.Errorf("landing did not move main forward by one commit: parent %s", parent)
	}
}

//shed:proves S.vcs.9
func TestTheOwnersGitStateIsLeftAlone(t *testing.T) {
	r := testrepo.Colocated(t)
	// The owner has something staged when shed first opens the repository.
	r.Write("staged.txt", "staged\n")
	r.Git("add", "staged.txt")
	index := r.Git("write-tree")
	repo := openRepo(t, r, "origin")
	if got := r.Git("write-tree"); got != index {
		t.Fatalf("opening the repository changed the owner's index")
	}
	r.Git("rm", "-q", "--cached", "staged.txt")
	must(t, os.Remove(filepath.Join(r.Dir, "staged.txt")))

	// The owner commits with git, behind jj's back, and stages more.
	r.Write("owner.txt", "owner\n")
	r.Git("add", "owner.txt")
	r.Git("commit", "-q", "-m", "owner's commit")
	ownerCommit := r.Git("rev-parse", "HEAD")
	r.Write("later.txt", "later\n")
	r.Git("add", "later.txt")
	index = r.Git("write-tree")
	status := r.Git("status", "--porcelain")
	intact := func(step string) {
		t.Helper()
		if got := r.Git("write-tree"); got != index {
			t.Errorf("%s changed the owner's index", step)
		}
		if got := r.Git("status", "--porcelain"); got != status {
			t.Errorf("%s changed git status:\n%s\nwant\n%s", step, got, status)
		}
	}

	if main, _ := repo.MainCommit(ctx); main != ownerCommit {
		t.Errorf("shed sees main at %s, not the owner's commit %s", main, ownerCommit)
	}
	intact("reading main")
	change := newUnit(t, repo, "Say goodbye", map[string]string{"bye.txt": "bye\n"})
	intact("opening a unit")
	if parent := r.JJ("--ignore-working-copy", "log", "--no-graph", "-r", "change_id("+change+")-", "-T", "commit_id"); parent != ownerCommit {
		t.Errorf("the unit is on %s, not the owner's commit", parent)
	}
	view := filepath.Join(t.TempDir(), "work")
	must(t, repo.Export(ctx, change, view))
	_, err := repo.Capture(ctx, change, view)
	must(t, err)
	intact("capturing a session")
	must(t, repo.Discard(ctx, newUnit(t, repo, "Discard", map[string]string{"x.txt": "x\n"})))
	intact("discarding a unit")
	if _, err := repo.Land(ctx, change, func(context.Context, string, string) (string, error) {
		return "", errors.New("stop")
	}); err == nil {
		t.Fatal("a landing without a message succeeded")
	}
	intact("a failed landing")

	commit, err := repo.Land(ctx, change, message("Say goodbye"))
	must(t, err)
	if got := r.Git("rev-parse", "refs/heads/main"); got != commit {
		t.Errorf("git's main is at %s after landing %s", got, commit)
	}
	if got := r.Git("rev-parse", commit+"^"); got != ownerCommit {
		t.Errorf("the unit landed on %s, not the owner's commit", got)
	}
	// Landing detached HEAD where it was; the index and files are the owner's.
	if got := r.Git("write-tree"); got != index {
		t.Error("landing changed the owner's index")
	}
}

//shed:proves S.vcs.11
func TestInterruptedUnitRebaseIsRestored(t *testing.T) {
	r := testrepo.Colocated(t)
	repo := openRepo(t, r, "")
	x := newUnit(t, repo, "Wave", map[string]string{"wave.txt": "wave\n"})
	landed, err := repo.Land(ctx, newUnit(t, repo, "Hello", map[string]string{"hello.txt": "hello\n"}), message("Hello"))
	must(t, err)
	before, err := repo.Snapshot(ctx, x)
	must(t, err)
	dir, err := repo.Workspace(ctx, x)
	must(t, err)

	// Shed stopped partway through rebasing the unit after the landing:
	// the change moved and its workspace was not yet updated.
	if _, err := repo.checkpoint(ctx, "rebase "+x); err != nil {
		t.Fatal(err)
	}
	r.JJ("-R", filepath.Join(repo.state, WorkspacesDir, baseDir), "--ignore-working-copy",
		"rebase", "-s", changeRevset(x), "-d", "main")

	reopened := openRepo(t, r, "")
	if main, _ := reopened.MainCommit(ctx); main != landed {
		t.Errorf("recovery moved main to %s, want the landing %s", main, landed)
	}
	if after, _ := reopened.Commit(ctx, x); after != before {
		t.Errorf("the unit's rebase was not restored: %s became %s", before, after)
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.state, CheckpointsDir)); len(entries) != 0 {
		t.Errorf("checkpoints left after recovery: %d", len(entries))
	}

	// Rebasing it again finishes with the workspace on the new main.
	conflicted, err := reopened.Rebase(ctx, x)
	must(t, err)
	if conflicted {
		t.Error("the rebase conflicts")
	}
	commit, err := reopened.Commit(ctx, x)
	must(t, err)
	if parent := r.Git("rev-parse", commit+"^"); parent != landed {
		t.Errorf("the unit sits on %s, want the landing %s", parent, landed)
	}
	for _, name := range []string{"wave.txt", "hello.txt"} {
		if !exists(filepath.Join(dir, name)) {
			t.Errorf("the rebased workspace lacks %s", name)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(repo.state, CheckpointsDir)); len(entries) != 0 {
		t.Errorf("checkpoints left after the rebase: %d", len(entries))
	}
}
