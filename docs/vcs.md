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

## Stored conflicts

jj stores a conflict inside a change instead of stopping the rebase that
caused it, so a unit's change can carry conflicts while its work goes on.
`shed conflicts` lists every unit that is neither landed nor archived and
whose change carries stored conflicts, in the order the units opened. Each
unit takes one line: its short change ID, then its conflicted files,
relative to the repository root and in path name order:

```
qpvuntsmwlqt internal/greet/greet.go spec/greet.md
```

It asks jj which changes and files are conflicted, never the tracker, so a
file whose only conflict is marker lines left over after a capture (see
[unresolved conflicts](#unresolved-conflicts)) is not listed here, even
though a session's bundle and verification still count it. With no
conflicted unit it prints nothing.

A session on a unit with an unresolved conflict finds every such file in its
bundle, under "Stored conflicts", in the same form `shed conflicts` prints.
When the session's work on the unit's files is kept, the bundle also says to
resolve those conflicts before any other work, and against what depends on
the unit's state: past its seal (sealed, implementing, verifying or queued)
it says to resolve them against the sealed spec, which for a mechanic is the
one bundle instruction the unit needs; proposed or contested, which has no
sealed spec, it says to keep main's text for any file under `spec/` or
`horizon.md` and re-apply the unit's own spec changes, and to resolve any
other file against the unit's proposed spec. A committee member or a
verifier's reviewer gets the list without that instruction, since what it
writes is thrown away. See [sessions](sessions.md#bundles-and-prompts).

## Landing

`shed land <unit>` lands a queued, sealed unit, and refuses one marked for
[horizon review](autopilot.md#landing):

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
8. Reconcile the other units in flight against the horizon changes the
   landing made: a unit advancing a removed clause reopens, and one advancing
   a changed clause, or a clause a new `refines` tag now refines, gets a
   notice. A notice that gives a change in a
   clause's text also marks the unit for horizon review. See
   [autopilot](autopilot.md#landing).
   This happens before the other units are rebased onto the new main, so a
   unit reopened here is rebased as a proposed unit.

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
- A sealed, implementing or queued unit keeps the rebase when its conflicts
  lie only in files outside `spec/`: the conflicts stay stored in the change
  and its workspace holds the rebased files, so the unit is on the new main.
  Its state, seal and footprint are unchanged. A sealed or implementing
  unit's next mechanic session's bundle names each conflicted file and says
  to resolve it against the sealed spec before any other work, the same as a
  conflict stored at sealing, and a conflict its mechanics leave unresolved
  fails its verification and returns it to implementing. A queued unit's
  stored conflict waits instead for its own landing: the wheelbuilder session
  there ([landing](#landing)) resolves whatever conflict the change still
  holds, even one a sweep stored well before that landing began and even
  when main has not moved since. If the wheelbuilder reports `unresolvable`,
  or any file still holds an unresolved conflict once its directory is
  captured, the unit reopens and nothing lands. A rebase that leaves an
  unresolved conflict in a file under `spec/` is undone instead, like any
  other conflict past the seal.
- A unit that is verifying, or a proposed unit that `shed frame` opened,
  keeps the rebase only if the rebased change holds no conflict in any file.
  Otherwise shed undoes that unit's rebase and leaves its change and
  workspace as they were, so no verifier ever meets a conflict a landing
  brought in unless a mechanic session was first told to resolve it, and a
  framing never holds a conflict no session would resolve. A verifying
  unit takes main's changes when a later landing rebases it cleanly, or once
  it reaches the queue and can keep a non-`spec/` conflict as above. A frame
  unit has no seal and no wheelbuilder to resolve a conflict for it: `shed
  frame -accept` rebases it onto main itself and, on a conflict, undoes the
  rebase and refuses to land, leaving the unit proposed for the owner to
  accept again once main stops conflicting, or to discard (see [framing the
  horizon](autopilot.md#framing-the-horizon)).

No unit changes state because of the rebase, and a rebase that conflicts, is
undone or fails neither fails the landing nor stops the other units from
being rebased.

A unit with a session running when main moves is left alone until none of
its sessions is running and every session directory has been captured. Only
then is it rebased. A capture therefore always applies to the change its
session was given, so it never reverts what the landing brought in, and the
sessions running together on a unit, such as a committee's members, all see
one revision.

After the sweep, `shed land` prints one line for every other unit that is
neither landed nor archived, in the order the units opened. Each line gives
the unit's short change ID, its state and the outcome:

```
landed qpvuntsm on main as 3f2a9c1d7e4b5a6f8c9d0e1f2a3b4c5d6e7f8091
footprint held
zsxkmwqp proposed: rebased cleanly
rlvkpnrz proposed: rebased with conflicts stored in its change
wtsrvvxp implementing: rebased with conflicts stored in its change
yostqsxw sealed: rebase undone: the rebase conflicted and the unit is past its seal or is a frame unit
mzvwutvl implementing: deferred: a session is running
kmnoplrs verifying: rebase failed: <reason>
```

These outcomes never change the landing's exit status. Each unit's
`shed unit log` records the same outcome as an event by shed that names the
landed unit, such as `after unit qpvuntsm landed: rebased cleanly`. None of
these events changes a unit's state. When a deferred unit is rebased later,
once its sessions end, and when the next shed process finishes an
interrupted sweep, each rebase records its outcome in the unit's log the
same way, naming the unit whose landing made the commit the change was
rebased onto. Those rebases print nothing. If shed stops partway through the
sweep, `shed land` prints no line for the units it had not reached; they get
their event when the sweep is finished.

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
Whether a `spec/` conflict's bounce counts toward the bounce threshold
depends on whether the painter's latest session already saw it and left it,
as [debate](autopilot.md#debate) describes; a conflict a landing brought in
after that session counts no bounce.

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
interrupted finishes. `shed sweep` (see [clauses and proofs](clauses.md#sweeping-main))
opens the repository like any other command, so running it after an
interrupted landing finishes that rebase sweep too, beyond the main commit
it checks out and the sweep of spec clauses it records.

A landing that moved main but stopped before the tracker recorded it is
completed by running `shed land` again: the unit's change is already on
main, so shed only records it. It records the actual footprint if none is
recorded yet and reports the drift, as a normal landing does.

## Commits outside shed

A unit lands as one commit carrying a `Unit:` trailer, as
[landing](#landing) shows, so most of main's history is explained by its
units. `shed outside` lists the commits that are not: it brings in main,
then walks main's first-parent history, oldest first, after the commit
`shed.toml`'s `outside.since` names:

```toml
[outside]
since = "a17b7815"
```

A commit counts as landed as a shed unit only when its message holds a
`Unit:` trailer naming a unit the tracker records as landed with that very
commit. A trailer naming no unit, a unit that never landed, or a unit landed
with a different commit does not count, so a message that merely looks like
a landing, or a cherry-pick of one, still shows up. Every other commit after
`outside.since` is listed, one line each giving its full hash, its author's
name and the first line of its message; the commit `outside.since` names is
never listed itself.

A commit that, against its first parent, changes `charter.md` and nothing
else is a charter change. `shed outside` lists charter changes apart from
the rest: after them, in the same line form, under a `charter changes:`
line it prints only when it has at least one charter change to list.

`shed outside` fails, naming why and listing no commit, when it cannot bring
in main, when `shed.toml` sets no `outside.since`, when that value names no
commit, or when the commit it names is not on main's first-parent history.
Beyond bringing in main and the recovery every shed command runs when it
opens the repository and the tracker, it changes nothing: it moves no unit,
records nothing, and leaves main and the remote as they were.
