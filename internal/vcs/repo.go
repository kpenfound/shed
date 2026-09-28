package vcs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Identity is a name and email that commits are made under.
type Identity struct {
	Name  string
	Email string
}

// shedIdentity makes the commits of units in flight. Landing rewrites the
// unit's commit under the landing identity.
var shedIdentity = Identity{Name: "shed", Email: "shed@localhost"}

// Options configure a repository.
type Options struct {
	// JJ is the jj executable; empty runs jj from PATH.
	JJ string
	// Main is the bookmark units land on. It defaults to main.
	Main string
	// Remote, when set, is fetched before a landing and receives main after
	// it.
	Remote string
	// Landing is the identity landed commits are made under.
	Landing Identity
}

// Repo is a colocated jj repository and the unit workspaces shed keeps in
// its state directory.
type Repo struct {
	root  string
	state string
	jj    string
	opts  Options
}

// Directories under the state directory.
const (
	WorkspacesDir  = "workspaces"
	CheckpointsDir = "checkpoints"
	lockFile       = "vcs.lock"
	// baseName is a workspace shed keeps so that adding unit workspaces
	// never snapshots the owner's working copy.
	baseName = "shed-base"
	baseDir  = "base"
)

var (
	// ErrNotColocated reports a repository that is not a jj repository
	// colocated with git at its root.
	ErrNotColocated = errors.New("not a colocated jj repository")
	// ErrConflict reports a unit whose change conflicts with main.
	ErrConflict = errors.New("conflicts with main")
	// ErrEmpty reports a unit that changes nothing.
	ErrEmpty = errors.New("changes nothing")
)

// Open checks the jj release and the repository, then restores any
// operation a crash interrupted.
func Open(ctx context.Context, root, state string, opts Options) (*Repo, error) {
	if opts.Main == "" {
		opts.Main = "main"
	}
	if opts.Landing == (Identity{}) {
		return nil, errors.New("a landing identity is required")
	}
	root, err := canonical(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(state, WorkspacesDir), 0o755); err != nil {
		return nil, err
	}
	state, err = canonical(state)
	if err != nil {
		return nil, err
	}
	if _, err := CheckJJ(ctx, opts.JJ); err != nil {
		return nil, err
	}
	jj := opts.JJ
	if jj == "" {
		jj = "jj"
	}
	r := &Repo{root: root, state: state, jj: jj, opts: opts}
	if err := r.checkColocated(ctx); err != nil {
		return nil, err
	}
	if _, err := r.MainCommit(ctx); err != nil {
		return nil, err
	}
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := r.recover(ctx); err != nil {
		return nil, fmt.Errorf("restoring an interrupted operation: %w", err)
	}
	return r, nil
}

func canonical(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func (r *Repo) checkColocated(ctx context.Context) error {
	jjRoot, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "root")
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrNotColocated, r.root, err)
	}
	gitDir, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "git", "root")
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrNotColocated, r.root, err)
	}
	jjRoot, _ = canonical(jjRoot)
	gitDir, _ = canonical(gitDir)
	if jjRoot != r.root || gitDir != filepath.Join(r.root, ".git") {
		return fmt.Errorf("%w: %s; run `jj git init --colocate` at the repository root", ErrNotColocated, r.root)
	}
	if rel, err := filepath.Rel(r.root, r.state); err == nil && !strings.HasPrefix(rel, "..") {
		if _, err := r.git(ctx, "check-ignore", "-q", rel+"/"); err != nil {
			return fmt.Errorf("the state directory %s is inside the repository and not ignored; add %s/ to .gitignore", rel, rel)
		}
	}
	return nil
}

// Root is the repository root.
func (r *Repo) Root() string { return r.root }

// mainRevset names the main bookmark exactly.
func (r *Repo) mainRevset() string { return fmt.Sprintf("bookmarks(exact:%q)", r.opts.Main) }

// MainCommit returns the commit main is at.
func (r *Repo) MainCommit(ctx context.Context) (string, error) {
	out, err := r.log(ctx, r.mainRevset(), "commit_id")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("the repository has no %s bookmark", r.opts.Main)
	}
	return out, nil
}

// Commit returns the commit a unit's change is at.
func (r *Repo) Commit(ctx context.Context, change string) (string, error) {
	out, err := r.log(ctx, changeRevset(change), "commit_id")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", fmt.Errorf("no change %s", change)
	}
	return out, nil
}

func changeRevset(change string) string { return fmt.Sprintf("change_id(%s)", change) }

// log evaluates a template over a revset without touching any working copy.
func (r *Repo) log(ctx context.Context, revset, template string) (string, error) {
	return r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "log", "--no-graph", "-r", revset, "-T", template+` ++ "\n"`)
}

// workspace is a unit workspace: its jj name and its directory.
type workspace struct {
	name string
	dir  string
}

// workspaces returns every workspace shed keeps, by the change its working
// copy is on.
func (r *Repo) workspaces(ctx context.Context) (map[string]workspace, error) {
	out, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "workspace", "list",
		"-T", `name ++ "\t" ++ self.target().change_id() ++ "\n"`)
	if err != nil {
		return nil, err
	}
	found := map[string]workspace{}
	for _, line := range strings.Split(out, "\n") {
		name, change, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasPrefix(name, "unit-") {
			continue
		}
		found[change] = workspace{name: name, dir: filepath.Join(r.state, WorkspacesDir, name)}
	}
	return found, nil
}

func (r *Repo) workspaceOf(ctx context.Context, change string) (workspace, error) {
	all, err := r.workspaces(ctx)
	if err != nil {
		return workspace{}, err
	}
	w, ok := all[change]
	if !ok {
		return workspace{}, fmt.Errorf("unit %s has no workspace", change)
	}
	return w, nil
}

// Workspace returns the directory of a unit's workspace.
func (r *Repo) Workspace(ctx context.Context, change string) (string, error) {
	w, err := r.workspaceOf(ctx, change)
	return w.dir, err
}

// base returns the directory of shed's base workspace, adding it the first
// time. Adding it snapshots the owner's working copy once; after that, unit
// workspaces are added from the base workspace instead.
func (r *Repo) base(ctx context.Context) (string, error) {
	dir := filepath.Join(r.state, WorkspacesDir, baseDir)
	out, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "workspace", "list", "-T", `name ++ "\n"`)
	if err != nil {
		return "", err
	}
	for _, name := range strings.Split(out, "\n") {
		if name == baseName {
			return dir, nil
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", err
	}
	_, err = r.run(ctx, r.root, shedIdentity, "workspace", "add", dir, "--name", baseName, "-r", r.mainRevset())
	return dir, err
}

// NewUnit makes a change for a unit on top of main, with a workspace of its
// own, and returns its change ID.
func (r *Repo) NewUnit(ctx context.Context, title string) (change string, err error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	done, err := r.checkpoint(ctx, "open unit")
	if err != nil {
		return "", err
	}
	defer func() { err = done(err) }()

	base, err := r.base(ctx)
	if err != nil {
		return "", err
	}
	name := "unit-" + nonce()
	dir := filepath.Join(r.state, WorkspacesDir, name)
	if _, err := r.run(ctx, base, shedIdentity, "workspace", "add", dir, "--name", name,
		"-r", r.mainRevset(), "-m", "unit: "+title); err != nil {
		return "", err
	}
	return r.log(ctx, name+"@", "change_id")
}

// Discard abandons a unit's change and removes its workspace.
func (r *Repo) Discard(ctx context.Context, change string) (err error) {
	unlock, err := r.lock()
	if err != nil {
		return err
	}
	defer unlock()
	done, err := r.checkpoint(ctx, "discard "+change)
	if err != nil {
		return err
	}
	defer func() { err = done(err) }()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return err
	}
	if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "workspace", "forget", w.name); err != nil {
		return err
	}
	if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "abandon", changeRevset(change)); err != nil {
		return err
	}
	return os.RemoveAll(w.dir)
}

func nonce() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// lock takes an exclusive lock so that one shed process changes the
// repository at a time.
func (r *Repo) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(r.state, lockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
