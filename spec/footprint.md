# Footprints

- **S.fp.1** (H.fp.1) A unit's footprint holds the spec clauses it modifies,
  the spec clauses it depends on and the horizon clauses it advances.
- **S.fp.2** (H.fp.2) Shed computes the clauses a unit modifies from its spec
  diff whenever the unit is declared, debated or sealed. `shed unit declare`
  records the clauses it depends on and advances, which must be on main. A
  proposal that changes no spec clause bounces.
