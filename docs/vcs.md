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

## Checkpoints

Before opening, discarding or landing a unit, shed records the jj operation
the repository is at in `.shed/checkpoints/`. If the operation fails, shed
restores the repository to that operation with `jj op restore`, updates the
unit workspaces the restore moved and deletes workspaces the restore
removed. If shed stops partway, the next shed process to open the
repository restores it the same way.

A landing that moved main but stopped before the tracker recorded it is
completed by running `shed land` again: the unit's change is already on
main, so shed only records it. It records the actual footprint if none is
recorded yet and reports the drift, as a normal landing does.
