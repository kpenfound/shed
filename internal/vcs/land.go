package vcs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Message builds a landed commit's message from the main commit the unit
// lands on and the unit's commit once it sits on top of main.
type Message func(ctx context.Context, main, unit string) (string, error)

// Land lands a unit as one commit on main. It fetches main when a remote is
// set, rebases the unit's change onto main, refuses a unit that conflicts
// with main or changes nothing, rewrites the change as the landing identity
// with the given message, moves main forward to it and pushes main. Main
// only ever moves forward. If any step fails before main is pushed the
// repository is restored to where it was; once main is pushed, nothing
// undoes the landing (S.vcs.11). Landing a unit that is already on main
// returns the unit's commit.
func (r *Repo) Land(ctx context.Context, change string, message Message) (commit string, err error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := r.importGit(ctx); err != nil {
		return "", err
	}
	if landed, err := r.onMain(ctx, change); err != nil || landed {
		if err != nil {
			return "", err
		}
		return r.Commit(ctx, change)
	}

	done, err := r.checkpoint(ctx, "land "+change)
	if err != nil {
		return "", err
	}
	defer func() {
		if done != nil {
			err = done(err)
		}
	}()

	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return "", err
	}
	if r.opts.Remote != "" {
		if _, err := r.jj(ctx, "git", "fetch", "--remote", r.opts.Remote); err != nil {
			return "", err
		}
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, "util", "snapshot"); err != nil {
		return "", err
	}
	onMain, err := r.log(ctx, fmt.Sprintf("%s & children(%s)", changeRevset(change), r.mainRevset()), "change_id")
	if err != nil {
		return "", err
	}
	if onMain == "" {
		if _, err := r.run(ctx, w.dir, shedIdentity, "rebase", "-s", "@", "-d", r.mainRevset()); err != nil {
			return "", err
		}
	}
	if conflicted, err := r.log(ctx, changeRevset(change)+" & conflicts()", "change_id"); err != nil {
		return "", err
	} else if conflicted != "" {
		return "", fmt.Errorf("unit %s %w; resolve it against the sealed spec", change, ErrConflict)
	}
	if empty, err := r.log(ctx, changeRevset(change), "empty"); err != nil {
		return "", err
	} else if empty == "true" {
		return "", fmt.Errorf("unit %s %w", change, ErrEmpty)
	}

	main, err := r.MainCommit(ctx)
	if err != nil {
		return "", err
	}
	unit, err := r.Commit(ctx, change)
	if err != nil {
		return "", err
	}
	msg, err := message(ctx, main, unit)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, w.dir, r.opts.Landing, "metaedit", "-r", "@", "-m", msg,
		"--update-author", "--update-author-timestamp"); err != nil {
		return "", err
	}
	if err := r.detachHead(ctx); err != nil {
		return "", err
	}
	// Without --allow-backwards, jj refuses any move that is not forward.
	if _, err := r.jj(ctx, "bookmark", "set", r.opts.Main,
		"-r", changeRevset(change)); err != nil {
		return "", err
	}
	if r.opts.Remote != "" {
		if err := r.push(ctx); err != nil {
			return "", err
		}
	}
	// The landing's checkpoint ends here: main has moved and is pushed.
	settle := done
	done = nil
	if err := settle(nil); err != nil {
		return "", err
	}
	if err := r.exportGit(ctx); err != nil {
		return "", err
	}
	commit, err = r.Commit(ctx, change)
	if err != nil {
		return "", err
	}
	if _, err := r.jj(ctx, "workspace", "forget", w.name); err != nil {
		return "", err
	}
	return commit, os.RemoveAll(w.dir)
}

// OnMain reports whether a unit's change is main or one of its ancestors.
func (r *Repo) OnMain(ctx context.Context, change string) (bool, error) {
	return r.onMain(ctx, change)
}

// onMain reports whether a change is main or one of its ancestors.
func (r *Repo) onMain(ctx context.Context, change string) (bool, error) {
	out, err := r.log(ctx, fmt.Sprintf("%s & ::%s", changeRevset(change), r.mainRevset()), "change_id")
	return out != "", err
}

// push sends main to the remote, tracking the remote bookmark first. jj
// refuses the push if the remote's main moved since the fetch.
func (r *Repo) push(ctx context.Context) error {
	remoteBookmark := r.opts.Main + "@" + r.opts.Remote
	tracked, err := r.jj(ctx, "bookmark", "list", "--tracked",
		"-T", `name ++ "@" ++ remote ++ "\n"`, r.opts.Main)
	if err != nil {
		return err
	}
	if !strings.Contains("\n"+tracked+"\n", "\n"+remoteBookmark+"\n") {
		if _, err := r.jj(ctx, "bookmark", "track", remoteBookmark); err != nil &&
			!strings.Contains(err.Error(), "No such remote bookmark") && !strings.Contains(err.Error(), "No matching") {
			return err
		}
	}
	_, err = r.jj(ctx, "git", "push", "--remote", r.opts.Remote, "--bookmark", r.opts.Main)
	return err
}

// detachHead detaches the owner's git HEAD when it is on main, at the commit
// it is on, so that moving main does not move the branch under a checkout.
// The owner's files are not touched.
func (r *Repo) detachHead(ctx context.Context) error {
	ref, err := r.git(ctx, "symbolic-ref", "-q", "HEAD")
	if err != nil || ref != "refs/heads/"+r.opts.Main {
		return nil
	}
	head, err := r.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	_, err = r.git(ctx, "update-ref", "--no-deref", "-m", "shed: detach before landing", "HEAD", head)
	return err
}

// Rebase rebases a unit's change onto main, fetching main first when a
// remote is set. Conflicts stay in the change, with git-style markers in its
// workspace's files, so they can be resolved there. It reports whether the
// change conflicts.
func (r *Repo) Rebase(ctx context.Context, change string) (bool, error) {
	return r.rebase(ctx, change, true, r.mainRevset(), true)
}

// Follow rebases a unit's change onto main as this repository has it,
// without fetching, and updates its workspace to the rebased files. The
// change keeps its ID and any conflict with main stays in it. It reports
// whether the change conflicts.
func (r *Repo) Follow(ctx context.Context, change string) (bool, error) {
	return r.rebase(ctx, change, false, r.mainRevset(), true)
}

// FollowClean rebases a unit's change onto main as Follow does, but only
// when the rebased change would hold no conflict. Otherwise it leaves the
// change and its workspace as they were. It reports whether the rebase would
// conflict and so was not made.
func (r *Repo) FollowClean(ctx context.Context, change string) (bool, error) {
	return r.rebase(ctx, change, false, r.mainRevset(), false)
}

// RebaseOnto rebases a unit's change onto commit, as Follow does onto main,
// and updates its workspace to the rebased files. It reports whether the
// change conflicts.
func (r *Repo) RebaseOnto(ctx context.Context, change, commit string) (bool, error) {
	return r.rebase(ctx, change, false, commit, true)
}

// rebase rebases a unit's change onto dest under a checkpoint of its own, so
// a rebase that fails or is interrupted is restored and nothing else is.
// Unless keep is set, a rebase that would conflict is not made: a copy of
// the change is rebased first, and the change is left alone when the copy
// conflicts.
func (r *Repo) rebase(ctx context.Context, change string, fetch bool, dest string, keep bool) (conflicted bool, err error) {
	unlock, err := r.lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return false, err
	}
	if err := r.importGit(ctx); err != nil {
		return false, err
	}
	if fetch && r.opts.Remote != "" {
		if _, err := r.jj(ctx, "git", "fetch", "--remote", r.opts.Remote); err != nil {
			return false, err
		}
	}
	done, err := r.checkpoint(ctx, "rebase "+change)
	if err != nil {
		return false, err
	}
	defer func() { err = done(err) }()
	if !keep {
		if _, err := r.run(ctx, w.dir, shedIdentity, "util", "snapshot"); err != nil {
			return false, err
		}
		if conflicts, err := r.trialRebase(ctx, change, dest); err != nil || conflicts {
			return conflicts, err
		}
	}
	if _, err := r.run(ctx, w.dir, shedIdentity, "rebase", "-s", "@", "-d", dest); err != nil {
		return false, err
	}
	out, err := r.log(ctx, changeRevset(change)+" & conflicts()", "change_id")
	return out != "", err
}

// trialRebase reports whether rebasing a unit's change onto dest would
// conflict, without rewriting the change: a scratch commit holding the
// change's files on the change's parent is rebased instead, and abandoned.
func (r *Repo) trialRebase(ctx context.Context, change, dest string) (bool, error) {
	marker := "shed: trial rebase " + nonce()
	if _, err := r.jj(ctx, "new", "--no-edit", "-m", marker, "parents("+changeRevset(change)+")"); err != nil {
		return false, err
	}
	scratch, err := r.log(ctx, fmt.Sprintf("description(substring:%q)", marker), "change_id")
	if err != nil {
		return false, err
	}
	if scratch == "" || strings.Contains(scratch, "\n") {
		return false, fmt.Errorf("rebasing unit %s: the scratch commit cannot be found", change)
	}
	if _, err := r.jj(ctx, "restore", "--from", changeRevset(change), "--into", changeRevset(scratch)); err != nil {
		return false, err
	}
	if _, err := r.jj(ctx, "rebase", "-r", changeRevset(scratch), "-d", dest); err != nil {
		return false, err
	}
	out, err := r.log(ctx, changeRevset(scratch)+" & conflicts()", "change_id")
	if err != nil {
		return false, err
	}
	if _, err := r.jj(ctx, "abandon", changeRevset(scratch)); err != nil {
		return false, err
	}
	return out != "", nil
}

// Conflicted lists the files a unit's change holds that jj stores as
// conflicted, slash-separated and relative to the workspace, after
// snapshotting its workspace.
func (r *Repo) Conflicted(ctx context.Context, change string) ([]string, error) {
	unlock, err := r.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, w.dir, shedIdentity, "file", "list", "-r", "@", "-T", `if(conflict, path ++ "\n", "")`)
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

// Behind reports whether a unit's change does not descend from main.
func (r *Repo) Behind(ctx context.Context, change string) (bool, error) {
	if _, err := r.Commit(ctx, change); err != nil {
		return false, err
	}
	out, err := r.log(ctx, fmt.Sprintf("%s & ~(%s::)", changeRevset(change), r.mainRevset()), "change_id")
	return out != "", err
}

// Descends reports whether a unit's change is a descendant of commit.
func (r *Repo) Descends(ctx context.Context, change, commit string) (bool, error) {
	out, err := r.log(ctx, fmt.Sprintf("%s & %s::", changeRevset(change), commit), "change_id")
	return out != "", err
}
