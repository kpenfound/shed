# Verification

- **S.verify.1** (H.verify.1) Verifying a unit first checks its documents
  and proofs as `shed check` does, checks that it changes the horizon only as
  sealed or by marking clauses it advances as realised, and runs the proofs of every
  clause in its footprint that is still in the spec, or every proof when
  `shed.toml` sets `verify.all_proofs`. Each of those clauses must have a
  proof, and every proof must pass. A horizon clause whose text or tag list
  on the unit's change differs from main's, a clause absent from one counting
  as differing, passes only when it is as on the unit's commit recorded at its
  latest seal (S.shed.8) and differs there from the main commit recorded in
  that seal, or when it is as on main or on that sealed commit apart from
  gaining `realised` and the unit advances it.
- **S.verify.2** (H.verify.2) A committee member who did not work on the unit
  then reviews it, with a copy of its files whose changes are thrown away,
  the sealed spec in its bundle and the `run_tests` and `prove` tools. Each problem it finds is
  recorded with the `finding` tool, citing clause IDs that resolve.
- **S.verify.3** (H.verify.3) The reviewer is asked to check the unit's code
  against every charter clause as well as against its spec, and its bundle
  holds the charter.
- **S.verify.4** (H.verify.4) A unit whose checks fail, or whose reviewer
  reports `fail`, returns to implementing with a notice to the mechanic
  naming the failing clauses and findings, and its steps start over. A
  reviewer that reports `spec-wrong` reopens the unit. A unit that passes
  moves to queued.
