# Proofs

- **S.proof.1** (H.doc.10) A proof is a top-level Go test function whose doc
  comment holds a `//shed:proves` directive followed by the spec clause IDs it
  proves.
- **S.proof.2** (H.doc.10) Shed refuses a spec clause without a proof, a proof
  that names anything other than a spec clause in the spec, and a directive
  outside a test function's doc comment.
- **S.proof.3** (H.doc.10) Shed looks for proofs in the Go module at the
  repository root. It skips the directories the go tool skips and nested
  modules.
- **S.proof.4** (H.doc.11) `shed prove` runs the proofs of the named spec
  clauses, or of every spec clause, with `go test -json`, and prints pass or
  fail per clause. A clause passes only when it has proofs and all of them
  pass. A skipped proof, or one that never reports, fails its clause. Any
  failure makes shed exit non-zero.
- **S.proof.5** (H.doc.11) When `shed.toml` sets `proofs.runner`, shed puts
  that command in front of the `go test` command line.
