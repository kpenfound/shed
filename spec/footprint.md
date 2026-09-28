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
