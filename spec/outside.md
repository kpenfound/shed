# Commits outside shed

- **S.outside.1** (H.vision.5) `shed.toml` may set `outside.since` to a git
  commit hash, full or abbreviated. `shed outside` brings in main as S.vcs.9
  says, then lists, oldest first, each commit on main's first-parent history
  after that commit that did not land as a shed unit, one line each giving
  the commit's full hash, its author's name and the first line of its
  message. A commit landed as a shed unit when its message holds a `Unit:`
  trailer naming the change ID of a unit the tracker, in the state directory
  S.track.1 names, records as landed with that very commit (S.vcs.7,
  S.frame.4). A `Unit:` trailer naming no unit, a unit that has not landed,
  or a unit recorded as landed with another commit does not make a commit
  landed. The commit `outside.since` names is not itself listed. When every
  commit after it landed as a shed unit, or none follows it, `shed outside`
  prints nothing and exits zero.
- **S.outside.2** (H.vision.5) A commit that did not land as a shed unit
  (S.outside.1) and that, against its first parent, changes `charter.md` and
  no other file is a charter change. `shed outside` lists charter changes
  apart from the other commits: after them, oldest first, in the same line
  form, under a line reading `charter changes:`. It prints that line only
  when it lists at least one charter change.
- **S.outside.3** (H.vision.5) `shed outside` fails, exiting non-zero with a
  message saying why and listing no commit, when it cannot bring in main,
  when `shed.toml` is missing or sets no `outside.since`, when the value
  names no commit, and when the commit it names is not on main's
  first-parent history. Whether it succeeds or fails, beyond bringing in
  main and the recovery every shed process does on opening the repository
  and the tracker (S.vcs.5, S.vcs.11, S.track.8), it moves no unit, records
  nothing in the tracker, and leaves main and the remote as they were.
