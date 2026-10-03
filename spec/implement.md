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
  later seals out of the amendment lane then keep. No workflow rule reads
  the estimate: it changes no queue order, footprint, scheduling decision or
  move between states, so two units that differ only in which positive
  estimate they record, and whose debates receive the same objections,
  answers and withdrawals, move the same way.
