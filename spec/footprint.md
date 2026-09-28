# Footprints

- **S.fp.1** (H.fp.1) A unit's footprint holds the spec clauses it modifies,
  the spec clauses it depends on and the horizon clauses it advances.
- **S.fp.2** (H.fp.2) Shed computes the clauses a unit modifies from its spec
  diff whenever the unit is declared, debated or sealed. `shed unit declare`
  records the clauses it depends on and advances, which must be on main. A
  proposal that changes no spec clause bounces.
- **S.fp.3** (H.fp.3) Landing a unit records its actual footprint: the spec
  clauses the landed commit adds, changes or removes against its parent,
  with the dependencies and horizon clauses recorded at its last seal. The
  tracker keeps the actual footprint beside the one recorded at that seal,
  and `shed tracker rebuild` gives both back. Landing a unit already on main
  records its actual footprint if none is recorded yet.
- **S.fp.4** (H.fp.3) `shed land` and the landing's line in `shed unit log`
  report the footprint drift: the modified clauses in the actual footprint
  but not the sealed one, and those in the sealed footprint but not the
  actual one. When both lists are empty the report says the footprint held.
  Landing a unit already on main reports the drift too.
- **S.fp.5** (H.fp.4) When a unit seals, shed compares its spec footprint,
  the clauses it modifies and depends on, with the footprint recorded at the
  last seal of every other unit that is sealed, implementing, verifying or
  queued. For each such unit whose spec footprint shares a clause with it,
  shed records one entanglement advisory event on the newly sealed unit. The
  advisory names the other unit by its short change ID and lists the shared
  clauses in the order they appear in main's `spec/` at the seal, taking its
  files in name order, followed by the shared clauses absent from main in
  the order they appear in the newly sealed unit's `spec/`. When several units are entangled with it, their advisories are
  recorded in the order those units opened, as `shed status` lists them.
  The advisories appear in `shed unit log` for the newly sealed unit and
  block nothing: the seal and both units' states are as they would be
  without them. Horizon clauses never count towards entanglement.
