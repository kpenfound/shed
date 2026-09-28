# Version control

- **S.vcs.1** (H.vcs.1) Shed works only in a jj repository colocated with git
  at the repository root, with jj at least 0.45.0 and below 0.46.0. It
  refuses any other repository or jj, and a state directory inside the
  repository that git does not ignore.
- **S.vcs.2** (H.vcs.2) Shed can give a session a plain directory holding
  the files of a unit's change and nothing else: no `.git` and no `.jj`.
- **S.vcs.3** (H.vcs.3, H.unit.1) `shed unit open <title>` makes a jj change
  for the unit on top of main, described `unit: <title>`, with a workspace of
  its own under the state directory. The change's ID is the unit's ID.
  `shed unit path <unit>` prints the workspace's directory.
- **S.vcs.4** (H.vcs.3) Capturing a session's directory makes the unit's
  change hold exactly the files in it, keeping executable bits and symlinks
  and ignoring any `.git` or `.jj` inside. The change keeps its ID.
- **S.vcs.5** (H.vcs.4) Before opening, discarding or landing a unit, shed
  records the jj operation the repository is at. If the operation fails, shed
  restores the repository to that point. If shed stops partway, the next shed
  process to open the repository restores it.
- **S.vcs.6** (H.vcs.5) Landing a unit fetches main from the configured
  remote, rebases the unit's change onto main, and refuses a change that
  conflicts with main or changes nothing. It then rewrites the change as one
  commit under the landing identity, moves main forward to it, pushes main to
  the remote and removes the unit's workspace. Landing a unit already on main
  only reports its commit.
- **S.vcs.7** (H.vcs.5) `shed land <unit>` lands a queued, sealed unit and
  records it as landed with its commit. The commit message holds the unit's
  title, the spec clauses it added, changed and removed, the horizon clauses
  it advances, and `Unit:` and `Sealed-Against:` trailers naming its change
  ID and the main commit it was sealed against.
- **S.vcs.8** (H.vcs.6) Only landing moves main, and only forward. Before a
  landing moves main, shed detaches a git checkout that is on main, where it
  is, without touching its files.
