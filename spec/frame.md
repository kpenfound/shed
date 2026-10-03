# The frame builder

- **S.frame.1** (H.hz.7) `shed frame <clause>` runs one frame builder
  session on a horizon clause of main that is distant or eventual and not
  marked realised. Shed refuses any other clause, and a clause ID that is not
  in the horizon, before starting a session. The session works in a writable
  copy of main's files. Its bundle holds the charter, the horizon, the named
  clause with the clauses that already refine it (S.horizon.7) and the spec
  clauses that advance it, and the units in flight with their footprints. The
  session reports `framed` or `nothing` through `done`.
- **S.frame.2** (H.hz.7) When the session reports `framed`, shed checks its
  copy against main. The check passes only when `horizon.md` is the only file
  changed, every change is an added horizon clause, at least one clause is
  added, and each added clause is tagged `near` or `soon`, is not marked
  realised, carries `refines` naming the clause given to `shed frame`
  (S.horizon.6), and has an ID that no clause on main holds or has held
  (S.doc.6). When the check fails, `shed frame` names every change that
  breaks it, keeps nothing and opens no unit. When the session reports
  `nothing` or ends without an outcome, `shed frame` says so, keeps nothing
  and opens no unit.
- **S.frame.3** (H.hz.7) When the check passes, shed opens a unit on main
  whose change holds the session's `horizon.md` and nothing else, with the
  frame builder as actor and a title naming the refined clause. It declares
  no horizon clause, so the unit is a draft (S.serve.4) and is not debated.
  Its change modifies no spec clause, so it cannot be declared (S.fp.2), and
  it stays proposed, a record of the framing for the owner to read, until
  the owner lands it under S.frame.4 or discards it; no clause seals it.
  It does not count toward
  `painter.max_proposed` (S.paint.1), and `shed serve -once` never reports it
  as a draft waiting to be declared (S.serve.1). Every rebase of it, by a
  landing under S.vcs.10 or by the recovery sweep under S.vcs.11, is treated
  as S.vcs.10 treats a unit past its seal: shed keeps the rebase only if the
  rebased change holds no conflict in any file, and otherwise undoes it,
  leaving the change and workspace as they were, so the record never holds a
  conflict that no session would resolve. S.serve.8 does not archive it,
  since it archives only a painter's draft. `shed frame`
  prints the unit's short change ID and the ID and tier of each added
  clause, in document order. `shed frame -discard <unit>` archives a unit
  that `shed frame` opened and that is still proposed, as deferred with no
  archive entry, and discards its change. It refuses any other unit.
- **S.frame.4** (H.hz.7) `shed frame -accept <unit>` lands a unit that
  `shed frame` opened and that is still proposed, with the owner as actor,
  and refuses any other unit. It fetches main and rebases the unit's change
  onto it under the checkpoint of S.vcs.5, then runs on the rebased change
  the check of S.frame.2 against that main, for the clause the unit was
  opened for, and requires that clause still be one `shed frame` accepts
  under S.frame.1. When the rebase conflicts or the check fails, it names
  each conflict or each change that breaks the check, undoes the rebase,
  leaving the unit proposed with its change and workspace as they were, and
  lands nothing. Otherwise it lands the change as S.vcs.6 does and moves the
  unit from proposed to landed with the owner as actor, a reason saying the
  owner accepted the framing, and its commit. The commit message holds the
  unit's title, the ID and tier of each horizon clause it adds, in document
  order, and a `Unit:` trailer naming its change ID; it has no
  `Sealed-Against:` trailer, since the unit has no seal. The landing is
  recorded as a horizon amendment under S.owner.11, which never samples it.
  Its actual footprint (S.fp.3) holds no spec clause and no dependency, and
  names the clause the unit was opened for as the horizon clause it
  advances. Every step that follows a landing by `shed land` follows this
  one the same way, with the frame unit as the landed unit: shed records
  the notices, marks and reopens of S.queue.3 to S.queue.6, before the
  rebase sweep as S.queue.4 orders, then rebases the other units as
  S.vcs.10 and S.vcs.11 say, and `shed frame -accept` prints and logs each
  unit's sweep outcome as S.vcs.15 says `shed land` does. It prints the
  landed commit before those lines.
- **S.frame.5** (H.hz.7, H.sched.2) When main has no unrealised near or
  soon clause, `shed serve` starts one frame builder session at a time,
  after the other controllers. It chooses unrealised distant clauses before
  eventual ones, in document order within each tier, skipping any clause
  with a proposed framing. Clauses already assigned to in-flight units still
  count as unrealised until main marks them realised. Before dispatch, shed
  records the chosen clause and main commit in the event log. Each pair is
  attempted at most once automatically, including across restarts and tracker
  rebuilds, even if the attempt is interrupted, fails, reports nothing or
  produces an invalid framing. A new main revision permits another attempt;
  `shed frame <clause>` remains available for an explicit retry. The daily
  budget applies, and framing starts neither alongside another framing nor
  while a painter or lander stage is running. The resulting draft follows
  S.frame.2 to S.frame.4 and waits for owner acceptance, since its new
  refining clauses count at their parent's tier under S.diff.4. Waiting
  framings do not prevent attempts on other parents. `serve` logs the outcome,
  including the unit and acceptance command when a draft is kept.
- **S.frame.6** (H.unit.9) `shed frame -expire <unit>` runs one frame
  builder session on a contested unit that `shed inbox` would mark overdue
  at that moment under S.owner.15. It refuses any other unit before starting
  a session, naming the unit and its state, or its wait and the timeout, so
  with `shed.contested_timeout` zero it refuses every unit. The session works
  in a copy of the unit's files whose changes are thrown away. Its bundle
  holds the charter, the horizon, the unit's spec changes, its debate record,
  the reason of each of its bounces, oldest first, and the owner's answers to
  it (S.owner.4), with the timeout and how long the unit has waited. It also
  holds the reason of the unit's latest move to contested and says whether
  that move was made under S.shed.16 or S.shed.18. The
  session reports through `done` either `keep`, or `archive` with a shelf,
  rejected or deferred, and a reason. The `done` tool refuses an archive
  with an empty reason, and an archive on the rejected shelf whose reason
  names no charter clause, or has a charter citation that S.owner.8 would
  refuse in an owner's `reject`. It also refuses an archive on the rejected
  shelf for a unit whose latest move to contested was made under S.shed.16
  or S.shed.18, since that unit is contested only to wait for the owner's
  `approve` (S.shed.17); such a unit may be kept or deferred. A refused
  report records nothing and the session may report again.
- **S.frame.7** (H.unit.9) When the session of S.frame.6 reports `archive`
  and the unit's latest move to contested is still the one it had when the
  session started, shed archives the unit on the reported shelf as S.shed.10
  does, with the frame builder as actor and the session's reason as the
  move's reason. A rejected entry cites as violated each charter clause the
  reason names, read as under S.owner.8, and a deferred entry gives the
  reason as what would change the decision. The move to archived records in
  the event log, so `shed tracker rebuild` keeps them, that the unit expired,
  the value of `shed.contested_timeout` and how long the unit had waited
  from its latest move to contested to the archive; the archive entry also
  holds both, the wait written as under S.owner.15, and the owner's answers.
  When the session reports `keep` or ends without an outcome, or the unit
  has left contested or moved to contested again since the session started,
  shed moves nothing and writes no archive entry, the unit stays as it is,
  and `shed frame -expire` says which of these happened.
- **S.frame.8** (H.unit.10) `shed serve` starts the session of S.frame.6
  on each contested unit that `shed inbox` would mark overdue at that
  moment under S.owner.15, so with `shed.contested_timeout` zero it starts
  none. It starts them one at a time, oldest first: the unit whose latest
  move to contested has the lowest event sequence number goes first. Each
  is a frame builder session, so no expiry session runs alongside another
  expiry session or a framing session of S.frame.5, in either order, and
  when both are due in the same pass the expiry session starts first. No expiry
  session starts while S.serve.6 pauses stages. Before dispatch, shed
  records in the event log the unit and the event sequence number of its
  latest move to contested. Each such pair is attempted at most once
  automatically, including across restarts and tracker rebuilds, even if
  the session is interrupted, fails, ends without an outcome or reports
  `keep`. A kept unit is therefore attempted again only after it leaves
  contested and moves to contested again, which gives it a new pair;
  `shed frame -expire <unit>` remains available for an explicit retry. The
  session's report is applied as S.frame.7 says, and `serve` logs the
  unit's short change ID with the outcome: archived with its shelf, kept,
  ended without an outcome, or left alone because the unit left contested
  or moved to contested again while the session ran.
