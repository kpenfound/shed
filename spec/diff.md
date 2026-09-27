# Spec diff

- **S.diff.1** (H.doc.6) `shed diff <from> [<to>]` lists the spec clauses
  added, removed and changed between two git revisions, or between a revision
  and the working tree.
- **S.diff.2** (H.doc.6) A spec clause changes when its text or the horizon
  clauses it advances change. Rewrapping or reindenting a clause is not a
  change.
