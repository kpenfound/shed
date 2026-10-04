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
- **S.sweep.3** (H.sweep.4) Recording a sweep (S.sweep.2) also files bugs
  in the tracker, in the same event. Each spec clause that fails the sweep
  and has no open bug gets a new open bug naming the clause, the main
  commit the sweep checked out and the output of the clause's proofs in
  that sweep. That output is what the `go test -json` run of S.proof.4
  reported as each proof's output, kept by the sweep as it runs them and
  joined proof by proof in the order the run reported them; it is empty for
  a clause with no proofs. A failing clause that already has an open bug gets
  no other, and its bug keeps the commit and output it was filed with. Each
  clause whose open bug the sweep finds passing has that bug closed,
  recording the commit the sweep checked out. A clause absent from the
  sweep, as one removed from the spec is, leaves its bug as it is. A sweep
  that records nothing files and closes nothing, and `shed tracker rebuild`
  gives back the same bugs, open and closed.
- **S.sweep.4** (H.sweep.4) After the amendments line of S.owner.21,
  `shed status` lists each open bug, oldest filed first, with the clause it
  names, the commit it was filed at, and how long it has been open: the
  time from the start of the sweep that filed it to the moment the status
  is read, written as under S.owner.15. It prints `bugs: none` when no bug
  is open. Closed bugs are not listed. Printing the list moves no unit and
  records nothing.
