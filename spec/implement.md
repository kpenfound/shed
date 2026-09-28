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
