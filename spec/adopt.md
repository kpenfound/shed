# Adoption

- **S.adopt.1** (H.adopt.4) `shed uncovered` runs every proof the repository
  holds (S.proof.3) once, with `go test` behind `proofs.runner` when
  `shed.toml` sets it (S.proof.5), measuring statement coverage over every
  package of the Go module at the repository root. A statement counts as
  executed when any proof executes it, whichever package holds that proof.
  Every other statement counts as unexecuted, including each statement of
  a package that no proof reaches. It calls no model and leaves the
  repository's files, the state directory and the tracker as they were.
- **S.adopt.2** (H.adopt.4) `shed uncovered` prints one line for each
  non-test Go source file that the go tool, run behind the same runner,
  builds into a package of the module, skipping the directories the go
  tool skips and nested modules. A file that build constraints exclude
  from that build gets no line and counts toward no total. Each line holds
  the file's path from the repository root, the number of its statements
  that no proof executed, its number of statements, and the unexecuted
  share as a whole percentage. A file with no statements shows a share of
  0%. Lines are ordered by unexecuted statements, most first, then by
  path. A last line gives the same three figures for the whole module.
- **S.adopt.3** (H.adopt.4) `shed uncovered` exits zero once every proof
  has run, whether it passes, fails or skips, and a failing proof still
  counts the statements it executed. It exits non-zero, saying why, when
  it cannot run the proofs, such as when the runner cannot start or the
  run yields no coverage profile, and when any proof never reports, such
  as because its package does not build, naming each such proof, even if
  other proofs ran and yielded coverage.
