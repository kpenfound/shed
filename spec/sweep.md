# Sweep

- **S.sweep.1** (H.sweep.3) `shed sweep` brings in main as S.vcs.9 says and
  checks main's current commit out into a fresh directory outside the
  repository's working copy and every unit's workspace, holding exactly that
  commit's files. There it runs the proofs of every spec clause in that
  commit's spec as `shed prove` does under S.proof.4, with the runner that
  commit's `shed.toml` sets under S.proof.5, and prints pass or fail per
  clause. It then prints the failing clauses, one per line, and exits
  non-zero when any fails. It removes the directory before it exits and
  calls no model. Beyond bringing in main and the recovery every shed
  process does on opening the repository and the tracker (S.vcs.5,
  S.vcs.11, S.track.8), it leaves main, every unit and its change, and the
  files of the repository's working copy as they were.
- **S.sweep.2** (H.sweep.3) A sweep whose proofs ran records itself in the
  tracker and appends it to the event log as one event naming no unit: the
  main commit it checked out, the time it started, and pass or fail for each
  spec clause it swept. `shed tracker rebuild` gives back the same sweeps. A
  sweep that cannot bring in or check out main records nothing, says why and
  exits non-zero.
