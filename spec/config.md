# Operator settings

- **S.config.1** (H.track.6) Operator settings live in `config.toml` in the
  state directory. They cover budgets, concurrency, the shed's round caps,
  bounce threshold and contested timeout, agent profiles, the profile each
  role uses, formulas, and version control: the jj executable, the main
  bookmark, the remote and the landing identity. A missing file or setting
  takes its default.
- **S.config.2** (H.track.6) Shed refuses operator settings with an unknown
  key, a negative budget, a cap below 1, a `shed.amendment_rounds` greater
  than `shed.max_rounds`, a profile naming an unknown agent or profile, a
  fallback loop, a role with an unknown profile, a formula step that needs
  an unknown step or needs itself through a cycle, a missing default formula,
  and an empty main bookmark or landing identity.
- **S.config.3** (H.track.6) `shed config` prints the operator settings in
  effect as TOML.
