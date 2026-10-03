# Implementation

- **S.impl.1** (H.impl.1) The steps inside implementing come from the
  operator's `default` formula: each step has a name and the steps it needs.
  Shed reads the formula when it implements a unit, so changing it needs no
  new build of shed.
- **S.impl.2** (H.impl.2) The default formula is `proofs`, then `implement`,
  which needs `proofs`, then `docs`, which needs `implement`.
- **S.impl.3** (H.impl.3) Implementing a sealed unit moves it to implementing
  and runs one mechanic session per step, each with a writable copy of the
  unit's files and the `run_tests` and `prove` tools, starting a step once
  the steps it needs have finished. A step finishes when its session reports
  `done`, and shed captures each session's files onto the unit's change. When
  every step has finished the unit moves to verifying.
- **S.impl.4** (H.impl.3) A mechanic that finds the sealed spec wrong reports
  `reopen` with its reason, and the unit reopens. A step whose sessions fail
  three times in a row reopens the unit.
- **S.impl.5** (H.impl.4, H.shed.11) Instead of `done`, a mechanic may report
  `amend` with a note that names each sealed clause it wants changed, gives
  the wording it wants for each, and says why. `done` refuses an `amend`
  whose note cites no clause of the unit's sealed spec. The step does not
  finish and the unit reopens as under S.impl.4: no further step starts and
  the files already captured stay on the unit's change. Unlike `reopen`, the
  reopen requests an amendment, so it counts an amendment as well as a
  bounce (S.unit.5), and the unit's next bundle states that the mechanic
  requested an amendment and gives the mechanic's note in full.
- **S.impl.6** (H.impl.7) A unit's estimate is a positive amount in USD of
  what taking it from sealed to landed will cost. The painter's `declare`
  tool records it with an `estimate` field and `shed unit declare` with
  `-estimate`; both refuse an amount that is not a positive number, and the
  painter's `declare` refuses a declaration that leaves its unit with no
  estimate, naming the missing field. Like any other field, an estimate
  omitted from a later `declare` is preserved (S.shed.5). A proposal with no
  recorded estimate is a draft, as one that declares no horizon clause is
  (S.serve.4): it is not debated, and `shed debate` refuses it, naming the
  unit and the missing estimate.
- **S.impl.7** (H.impl.7) Every committee and painter debate bundle shows
  the proposal's recorded estimate, and a member may object to it as to any
  other part of the proposal. Sealing records the unit's estimate with its
  seal (S.unit.4, S.shed.8), `shed unit log` shows it on the seal's line,
  and `shed tracker rebuild` gives it back, giving back with no estimate a
  seal recorded before seals recorded estimates. A seal out of the amendment
  lane (S.shed.11), including the seal of S.shed.14, records the estimate
  recorded at the unit's previous seal, whatever was declared since; when
  that seal recorded none, it records the unit's estimate at sealing, which
  later seals out of the amendment lane then keep. No workflow rule but
  the overrun reopen of S.impl.9 reads the estimate: it changes no queue
  order, footprint, scheduling decision or move between states, so with
  `budget.overrun_multiple` zero, two units that differ only in which
  positive estimate they record, and whose debates receive the same
  objections, answers and withdrawals, move the same way.
- **S.impl.8** (H.impl.8) On each unit's line under S.track.9, `shed status`
  shows, in a column headed `ESTIMATE` of its own, the estimate recorded at
  the unit's most recent seal (S.impl.7) beside the unit's cost since the
  seal that set that estimate: the unit's most recent seal that did not
  carry its estimate forward from the seal before it under S.impl.7. A seal
  out of the amendment lane (S.shed.11), including the seal of S.shed.14,
  that carries the estimate forward therefore leaves the cost where it was,
  while any other seal starts it afresh. The cost is the summed cost of the
  unit's sessions that finished after the seal that set the estimate in the
  event log (S.track.3), in any role and whatever state the unit has moved
  to since, so a unit reopened after that seal keeps adding to it until a
  seal sets the estimate again. A session still running adds nothing until
  it finishes. Both amounts are written in USD to the cent, cost since that
  seal first, as `$1.20 of $5.00`. The column adds to the line and replaces
  nothing on it: the cost so far of S.track.9 keeps its own column, headed
  `COST`, on every unit's line. A unit that was never sealed, or whose most
  recent seal recorded no estimate, leaves its `ESTIMATE` column empty and
  still shows its cost so far. After `shed tracker rebuild` (S.track.5)
  every unit shows the same amounts as before it.
- **S.impl.9** (H.impl.9) A unit that is sealed, implementing, verifying or
  queued overruns when the estimate recorded at its most recent seal times
  `budget.overrun_multiple` is less than its cost since the seal that set
  that estimate, reckoned as under S.impl.8 from finished sessions only.
  The multiple defaults to 3, zero turns overruns off, and S.config.2
  refuses any other multiple below 1. A unit whose most recent seal
  recorded no estimate never overruns. Shed starts no
  session, in any role, on a unit that overruns. A stage running on it when
  a session's cost makes it overrun records that session's result and any
  move it makes as usual, but starts no further session and lands nothing.
  Once no session on the unit is running, shed reopens it if it is still
  sealed, implementing, verifying or queued, with shed as actor and a
  reason naming the cost since that seal, the estimate and the multiple,
  the amounts in USD to the cent. The reopen counts a bounce (S.unit.5),
  and may move the unit on to contested under S.unit.6, but requests no
  amendment. `shed serve` reopens in the same way, on its next pass, any
  overrunning unit it finds with no session running, as after a restart or
  a lower multiple. An overrun never ends a running session and never
  moves a unit other than by that reopen.
