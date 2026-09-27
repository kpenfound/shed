# Horizon and trace

- **S.horizon.1** (H.doc.7) A horizon clause opens, after its ID, with a
  parenthesised tag list holding exactly one tier (`near`, `soon`, `distant`
  or `eventual`) and optionally `realised`. Shed refuses a missing tier, a
  second tier and an unknown tag.
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
