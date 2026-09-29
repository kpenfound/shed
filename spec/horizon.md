# Horizon and trace

- **S.horizon.1** (H.doc.7) A horizon clause opens, after its ID, with a
  parenthesised tag list holding exactly one tier (`near`, `soon`, `distant`
  or `eventual`), optionally `realised` and optionally a `refines` tag
  (S.horizon.6). Shed refuses a missing tier, a second tier and an unknown
  tag.
- **S.horizon.2** (H.doc.8) A spec clause opens, after its ID, with a
  parenthesised list of the horizon clauses it advances. Shed refuses a spec
  clause that names none, or names anything other than a horizon clause in
  the horizon.
- **S.horizon.3** (H.doc.7) Shed refuses a horizon clause marked realised
  that no spec clause advances.
- **S.horizon.4** (H.doc.8) `shed trace` lists every horizon clause in
  document order with its tier, whether it is realised, and the spec clauses
  that advance it.
- **S.horizon.5** (H.doc.9) `shed gap` lists, in document order, the horizon
  clauses not marked realised, with their tier and the spec clauses that
  already advance them.
- **S.horizon.6** (H.hz.3) A near or soon horizon clause may carry, in its
  tag list, `refines` followed by the ID of the distant or eventual horizon
  clause it refines, such as `(soon, refines H.vision.1)`. Shed refuses a
  `refines` tag on a distant or eventual clause, a second `refines` tag, and
  one that names anything other than a distant or eventual clause in the
  horizon.
- **S.horizon.7** (H.hz.3) `shed trace` shows, for each horizon clause that
  refines another, the clause it refines, and for each distant or eventual
  clause, the clauses that refine it.
- **S.horizon.10** (H.hz.3) `shed gap`, and the gap in the painter's bundle
  (S.paint.2), show for each listed clause that refines another (S.horizon.6)
  the ID and tier of the clause it refines. A listed clause with no
  `refines` tag shows none.
- **S.horizon.11** (H.hz.3) In a git repository, shed refuses a working
  tree change that leaves a near or soon horizon clause without a `refines`
  tag (S.horizon.6) when that clause, on HEAD, was absent, was distant or
  eventual, or carried a `refines` tag. A near or soon clause that HEAD
  already holds at near or soon without a `refines` tag may keep lacking
  one, whatever else in it changes.
- **S.horizon.12** (H.hz.3) After the clauses it lists, `shed trace` ends
  with a line naming, in document order, the ID of every near or soon
  horizon clause, realised or not, that carries no `refines` tag
  (S.horizon.6), and giving their count. When every near and soon clause
  carries one, `shed trace` prints no such line.
