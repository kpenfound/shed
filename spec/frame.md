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
  it stays proposed, a record of the framing for the owner to read, until a
  later clause under H.hz.7 gives frame units a route to main; no clause
  here seals or lands it. It does not count toward
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
