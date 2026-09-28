# Verification

- **S.verify.1** (H.verify.1) Verifying a unit first checks its documents
  and proofs as `shed check` does, checks that it changes the horizon only by
  marking clauses it advances as realised, and runs the proofs of every
  clause in its footprint that is still in the spec, or every proof when
  `shed.toml` sets `verify.all_proofs`. Each of those clauses must have a
  proof, and every proof must pass.
- **S.verify.2** (H.verify.2) A committee member who did not work on the unit
  then reviews it, with a read-only copy of its files, the sealed spec in its
  bundle and the `run_tests` and `prove` tools. Each problem it finds is
  recorded with the `finding` tool, citing clause IDs that resolve.
- **S.verify.3** (H.verify.3) The reviewer is asked to check the unit's code
  against every charter clause as well as against its spec, and its bundle
  holds the charter.
- **S.verify.4** (H.verify.4) A unit whose checks fail, or whose reviewer
  reports `fail`, returns to implementing with a notice to the mechanic
  naming the failing clauses and findings, and its steps start over. A
  reviewer that reports `spec-wrong` reopens the unit. A unit that passes
  moves to queued.
