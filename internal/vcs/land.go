package vcs

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Message builds a landed commit's message from the main commit the unit
// lands on and the unit's commit once it sits on top of main.
type Message func(ctx context.Context, main, unit string) (string, error)

// Land lands a unit as one commit on main. It fetches main when a remote is
// set, rebases the unit's change onto main, refuses a unit that conflicts
// with main or changes nothing, rewrites the change as the landing identity
// with the given message, moves main forward to it and pushes main. Main
// only ever moves forward. If any step fails the repository is restored to
// where it was. Landing a unit that is already on main returns the unit's
// commit.
func (r *Repo) Land(ctx context.Context, change string, message Message) (commit string, err error) {
	unlock, err := r.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
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
	defer func() { err = done(err) }()

	w, err := r.workspaceOf(ctx, change)
	if err != nil {
		return "", err
	}
	if r.opts.Remote != "" {
		if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "git", "fetch", "--remote", r.opts.Remote); err != nil {
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
	if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "bookmark", "set", r.opts.Main,
		"-r", changeRevset(change)); err != nil {
		return "", err
	}
	if r.opts.Remote != "" {
		if err := r.push(ctx); err != nil {
			return "", err
		}
	}
	commit, err = r.Commit(ctx, change)
	if err != nil {
		return "", err
	}
	if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "workspace", "forget", w.name); err != nil {
		return "", err
	}
	return commit, os.RemoveAll(w.dir)
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
	tracked, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "bookmark", "list", "--tracked",
		"-T", `name ++ "@" ++ remote ++ "\n"`, r.opts.Main)
	if err != nil {
		return err
	}
	if !strings.Contains("\n"+tracked+"\n", "\n"+remoteBookmark+"\n") {
		if _, err := r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "bookmark", "track", remoteBookmark); err != nil &&
			!strings.Contains(err.Error(), "No such remote bookmark") && !strings.Contains(err.Error(), "No matching") {
			return err
		}
	}
	_, err = r.run(ctx, r.root, shedIdentity, "--ignore-working-copy", "git", "push", "--remote", r.opts.Remote, "--bookmark", r.opts.Main)
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
