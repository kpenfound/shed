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
- **S.vcs.9** (H.vcs.2, H.vcs.6) Shed leaves the owner's git state alone. It
  runs jj from a workspace of its own, never the owner's, so the owner's git
  index and staged changes survive every shed operation. Adding that
  workspace, once, puts the owner's index back as it was. Before it reads or
  moves main, shed brings in commits the owner made with git, and after a
  landing git's main points at the landed commit.
- **S.vcs.10** (H.vcs.7, H.vcs.9) After `shed land` moves main, shed
  rebases the change of every other unit that is neither landed nor archived
  onto the new main. Each change keeps its ID and the unit's workspace is
  updated to hold the rebased files. A proposed or contested unit's change
  keeps any conflict with main stored in its files. Except as S.vcs.16 says,
  a unit past its seal (sealed,
  implementing, verifying or queued) keeps the rebase only if the rebased
  change holds no conflict in any file; otherwise shed undoes that unit's
  rebase, leaving its change and workspace as they were, so no conflict a
  landing brought in reaches a verifier or gate unless a mechanic session
  was first told to resolve it (S.vcs.16), and the unit
  takes main's changes when a later landing rebases it cleanly or at its own
  landing, where the wheelbuilder resolves conflicts against the sealed spec
  (S.queue.2). No unit changes state because of this rebase, and a rebase
  that conflicts, is undone or fails does not fail the landing or stop the
  other units from being rebased.
  A unit with any session running at that moment is not rebased then: shed
  rebases it once none of its sessions is running and any session directory
  has been captured, so a capture applies to the change its session was
  given and the sessions running together on a unit all see one revision.
  Sealing a unit (S.shed.8) first rebases its change the same way onto the
  main commit the seal records, so a sealed unit's change is always based on
  its seal's main, whatever base it had before. A conflict that rebase leaves
  only in files outside `spec/` is stored and the unit is sealed, to be
  resolved against the sealed spec by its mechanics, whose bundles name each
  file holding an unresolved conflict (S.vcs.12) and say to resolve it first,
  as S.vcs.14 says. If that rebase
  fails, or leaves an unresolved conflict (S.vcs.12) in any file under
  `spec/`, nothing is sealed: the unit bounces to its painter as under
  S.shed.6, counting a bounce except as S.vcs.17 says and, unless the
  bounce, counted or not, moves it to contested under S.unit.6 or S.vcs.17,
  staying proposed and starting its next debate afresh, with a reason that names the failure, or each file under `spec/` holding an unresolved conflict and each clause ID
  inside a conflicted region. A conflicted change keeps its rebased files, so
  the painter's next session sees the conflicts and resolves them before any
  member debates the text; a failed rebase leaves the change as it was.
- **S.vcs.11** (H.vcs.7, H.vcs.4) The landing's checkpoint ends once main is
  pushed, and a failure or stop after that never undoes the landing. Shed
  checkpoints each unit's rebase under S.vcs.10 on its own and restores only
  that unit's rebase if it fails or shed stops partway. The next shed process
  to open the repository rebases, as S.vcs.10 says, every unit that is neither
  landed nor archived, has no running session and whose change does not
  descend from main, so an interrupted sweep finishes.
- **S.vcs.12** (H.vcs.9) A file on a unit's change holds an unresolved
  conflict while jj stores it as conflicted, or while it still holds any
  line of the conflict markers shed wrote into it when it gave a session a
  directory of the change's files (S.vcs.2). Shed records, per unit, each
  file it wrote conflict markers into and those markers' lines, so capture
  (S.vcs.4) turning markers into plain file content never hides a
  conflict. Verifying a unit whose change holds an unresolved conflict in
  any file fails its checks (S.verify.1): the unit returns to implementing as
  under S.verify.4, with a notice naming each such file, so no unit reaches
  the queue or lands holding a conflict nobody resolved. The check at
  sealing under S.vcs.10 uses the same test, so markers a painter left in a
  file under `spec/` block the seal as a stored conflict would.
- **S.vcs.13** (H.vcs.8) `shed conflicts` lists every unit that is neither
  landed nor archived and whose change carries stored conflicts, in the order
  the units opened. Each unit takes one line: its short change ID, then the
  path of every conflicted file in it, relative to the repository root and in
  path name order, separated by single spaces. Whether a change is conflicted,
  and which of its files are, comes from the change in jj alone, never from
  the tracker, so a file whose only conflict is marker lines left in it after
  capture (S.vcs.12) is not listed, though bundles (S.vcs.14) and verification
  (S.vcs.12) still count it. With no such unit it prints nothing and exits
  zero.
- **S.vcs.14** (H.vcs.9) When a session starts on a unit whose change holds an
  unresolved conflict (S.vcs.12) in any file, the session's bundle names every
  such file, relative to the repository root and in path name order, separated
  by single spaces. When the session's work on the unit's files is kept
  (S.sess.3), the bundle also says to resolve those conflicts before any other
  work, and against what, by the unit's state (S.unit.2). For a unit past its
  seal (sealed, implementing, verifying or queued) it says to resolve them
  against the sealed spec; for a mechanic this is the bundle S.vcs.10
  requires, given once. For a proposed or contested unit, which has no sealed
  spec, it says to resolve a file under `spec/` or `horizon.md` by keeping
  main's text and re-applying the unit's own spec changes, and any other file
  against the unit's proposed spec. A session whose copy of the files is
  thrown away, such as a committee member's or a verifier's reviewer, gets the
  list without that instruction. A bundle for a unit whose change holds no
  unresolved conflict says nothing about conflicts.
- **S.vcs.15** (H.vcs.7) After the rebase sweep that follows a landing
  (S.vcs.10), `shed land` prints one line for every other unit that is
  neither landed nor archived, naming it by its short change ID and its
  state and giving the sweep's outcome for it: rebased cleanly, rebased
  with conflicts stored in its change, rebase undone because the rebase
  conflicted and the unit is past its seal or is a frame unit (S.frame.3),
  deferred because a session was running, or failed, with the failure's
  reason. The lines follow
  the order the units opened, and the landing's exit status is the same
  whatever outcomes they report. Each such unit's `shed unit log` records
  the same outcome as an event naming the landed unit. A deferred unit's
  later rebase under S.vcs.10, and each rebase the next shed process does
  under S.vcs.11 to finish an interrupted sweep, also records its outcome
  in the unit's log as such an event, naming the unit whose landing made
  the commit the change was rebased onto, and prints nothing. If shed
  stops partway through the sweep, `shed land` prints no line for the units
  it had not reached. None of these events changes a unit's state.
- **S.vcs.16** (H.vcs.7) A unit that is sealed, implementing or queued
  keeps a rebase under S.vcs.10 or S.vcs.11 whose rebased change holds
  unresolved conflicts (S.vcs.12) only in files outside `spec/`: the
  conflicts stay stored in its change and its workspace holds the rebased
  files, so the unit is on the new main. Its state, seal and footprint are
  unchanged. A sealed or implementing unit's next mechanic session's bundle
  names each conflicted file as S.vcs.10 says, and a conflict its mechanics
  leave unresolved fails its verification and returns it to implementing
  (S.vcs.12), as a conflict stored at its sealing does. A queued unit's
  conflicts are resolved at its landing: when its change holds an
  unresolved conflict after the landing's rebase (S.queue.2), even one
  stored before that landing began and even when main has not moved since,
  the wheelbuilder session of S.queue.2 runs before anything lands, and if
  it reports `unresolvable`, or any file still holds an unresolved conflict
  once its directory is captured, the unit reopens and nothing lands. A
  rebase that leaves an unresolved conflict in any file under `spec/`, or
  of a unit that is verifying, is still undone as S.vcs.10 says. `shed
  land` and the unit's log report a kept rebase as rebased with conflicts
  stored in its change (S.vcs.15).
- **S.vcs.17** (H.vcs.7, H.unit.5) Each time shed captures the directory
  of one of a unit's painter sessions (S.vcs.4), it records whether any file
  under `spec/` then holds an unresolved conflict (S.vcs.12). No rebase runs
  while a session runs (S.vcs.10), so such a conflict is one the painter was
  given and left. A sealing that S.vcs.10 stops because its rebase leaves an
  unresolved conflict (S.vcs.12) in a file under `spec/` counts a bounce
  only if the latest such capture found one. Otherwise every conflict
  under `spec/` came in by a landing after the painter's latest capture, and
  the bounce leaves the unit's bounce count as it was: its reason is as
  S.vcs.10 says, and `shed unit log` records the bounce and states that it
  counted no bounce. An uncounted bounce that leaves the unit with more
  uncounted bounces since it opened or was last sealed than the operator's
  bounce threshold still counts no bounce, but moves the unit to contested
  and leaves a notice for the owner as S.unit.6 says, saying that landings
  kept bringing conflicts under `spec/`, so a unit that landings keep
  conflicting with still reaches the owner. A sealing whose rebase fails
  counts a bounce as S.vcs.10 says.
