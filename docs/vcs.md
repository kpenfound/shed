# Version control

Shed performs every version control operation itself. Agents never hold git
or jj; they get plain directories of files.

## Setting up

Shed works in a jj repository colocated with git at the repository root,
with jj at least 0.45.0 and below 0.46.0. jj is pre-1.0 and its command line
still moves, so shed refuses other releases.

```sh
jj git init --colocate
```

When the state directory is inside the repository, as `.shed` is by default,
`.gitignore` must ignore it. Shed refuses to work otherwise, because jj would
snapshot the tracker and nest unit workspaces inside the owner's working copy.

Shed runs jj with none of the owner's jj configuration. Git's configuration
and credentials still apply, so fetch and push work as they do for the owner.

## Units and workspaces

`shed unit open <title>` makes a jj change on top of main, described
`unit: <title>`, and gives it a workspace under `.shed/workspaces/`. The
change's ID is the unit's ID, and it survives rebases. `shed unit path
<unit>` prints the workspace's directory, so the owner can work on a unit by
hand while shed is not yet running sessions.

Shed runs jj from a workspace of its own, `.shed/workspaces/base`, never
from the owner's. In a colocated repository jj keeps git's index in step
with what it believes HEAD is, so a jj command run in the owner's workspace
after they committed with git would reset their index to an older tree.
From shed's workspace, the owner's index, staged changes and files are never
touched. Adding the base workspace, the first time shed opens the
repository, has to run in the owner's workspace; shed saves git's index
first and puts it back afterwards.

Because shed's workspace does not see git's refs move by itself, shed runs
`jj git import` before it reads or moves main, so a commit the owner made
with git is the main that units start from and land on. After a landing it
runs `jj git export`, so git's `main` points at the landed commit.

A session gets a copy of the unit's files in a directory of its own, with no
`.git` and no `.jj`. When the session ends, shed copies the directory back
onto the unit's workspace: new and changed files are written, missing files
are deleted, executable bits and symlinks are kept, and any `.git` or `.jj`
the session made is ignored. Then shed snapshots the workspace onto the
unit's change.

## Landing

`shed land <unit>` lands a queued, sealed unit:

1. Fetch main from the configured remote.
2. Rebase the unit's change onto main. A change that conflicts with main, or
   changes nothing, is refused.
3. Rewrite the change under the landing identity with the landing message.
4. Detach the owner's git checkout if it is on main, at the commit it is on,
   so moving main does not move the branch under it. No files change.
5. Move main forward to the change. jj refuses any move that is not forward.
6. Push main to the remote. jj refuses the push if the remote's main moved
   after the fetch.
7. Remove the unit's workspace and record the unit as landed, with its
   commit and its actual footprint, and report how that footprint drifted
   from the sealed one. See [the tracker](tracker.md#footprints-and-seals).

A unit lands as one commit, so `git log main` reads as the spec changelog:

```
Say goodbye

Spec:
  added   S.greet.2
Advances: H.greet.2

Unit: qpvuntsmwlqtqpvuntsmwlqtqpvuntsm
Sealed-Against: 0c4b933e61ab29d0bf3dc6ee5f8b2b9c35e8d0a1
```

Only landing moves main. Opening, capturing and discarding units never do.

## Keeping units on main

After `shed land` moves main, shed rebases the change of every other unit
that is neither landed nor archived onto the new main. Each change keeps its
ID, and its workspace is updated to hold the rebased files. What happens to a
conflict with main depends on how far the unit has come:

- A proposed or contested unit keeps the rebase, conflicts and all. The
  conflict stays stored in the change's files, and the painter's next session
  sees it and resolves it before the committee debates the text.
- A unit past its seal (sealed, implementing, verifying or queued) keeps the
  rebase only if the rebased change holds no conflict in any file. Otherwise
  shed undoes that unit's rebase and leaves its change and workspace as they
  were, so no mechanic, verifier or gate meets a conflict a landing brought
  in. The unit takes main's changes when a later landing rebases it cleanly,
  or at its own landing, where the wheelbuilder resolves conflicts against
  the sealed spec.

No unit changes state because of the rebase, and a rebase that conflicts, is
undone or fails neither fails the landing nor stops the other units from
being rebased.

A unit with a session running when main moves is left alone until none of
its sessions is running and every session directory has been captured. Only
then is it rebased. A capture therefore always applies to the change its
session was given, so it never reverts what the landing brought in, and the
sessions running together on a unit, such as a committee's members, all see
one revision.

Sealing a unit rebases its change the same way onto the main commit the seal
records, so a sealed unit's change is always based on its seal's main,
whatever base it had before. A conflict that rebase leaves only in files
outside `spec/` is stored and the unit is sealed: its mechanics resolve it
against the sealed spec, and every mechanic's bundle names each file holding
an unresolved conflict and says to resolve it before any other work. A
rebase that fails, or that leaves an unresolved conflict in any file under
`spec/`, seals nothing and bounces the unit to its painter, as
[debate](autopilot.md#debate) describes. A failed rebase leaves the change as
it was; a conflicted one keeps its rebased files for the painter to resolve.

## Unresolved conflicts

A file on a unit's change holds an unresolved conflict while jj stores it as
conflicted, or while it still holds any line of the conflict markers shed
wrote into it when it gave a session a directory of the change's files.
Sessions get conflicts as git-style markers in plain files, and capture turns
those markers into ordinary content. So before handing a session a directory,
shed records, per unit, each file it wrote markers into and the marker lines
it wrote, under `conflicts/` in the state directory. A marker line a session
leaves in place still counts as a conflict after capture, while a file that
merely quotes marker syntax shed never wrote into it does not.

Verifying a unit whose change holds an unresolved conflict in any file fails
its checks: the unit returns to implementing with a notice naming each such
file, so no unit reaches the queue or lands holding a conflict nobody
resolved. Sealing uses the same test, so markers a painter left in a file
under `spec/` block the seal as a stored conflict does.

## Checkpoints

Before opening, discarding, landing or rebasing a unit, shed records the jj
operation the repository is at in `.shed/checkpoints/`. If the operation
fails, shed restores the repository to that operation with `jj op restore`,
updates the unit workspaces the restore moved and deletes workspaces the
restore removed. If shed stops partway, the next shed process to open the
repository restores it the same way.

A landing's checkpoint ends once main is pushed, so nothing after the push
undoes the landing. Each unit's rebase after a landing has a checkpoint of
its own, and a failure or stop restores only that unit's rebase. The next
shed process to open the repository rebases every unit that is neither
landed nor archived, has no session running and whose change does not
descend from main, by the same rules as after a landing, so a sweep that was
interrupted finishes.

A landing that moved main but stopped before the tracker recorded it is
completed by running `shed land` again: the unit's change is already on
main, so shed only records it. It records the actual footprint if none is
recorded yet and reports the drift, as a normal landing does.
