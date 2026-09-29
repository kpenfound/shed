# Spec diff

- **S.diff.1** (H.doc.6) `shed diff <from> [<to>]` lists the spec clauses
  added, removed and changed between two git revisions, or between a revision
  and the working tree.
- **S.diff.2** (H.doc.6) A spec clause changes when its text or the horizon
  clauses it advances change. Rewrapping or reindenting a clause is not a
  change.
- **S.diff.3** (H.hz.1) After the spec clauses, `shed diff` lists the
  horizon clauses added, removed and changed between the same two
  revisions, each with its tier: a removed clause's tier on the first
  revision, an added clause's tier on the second, and a changed clause's
  higher tier of the two, counting near below soon, soon below distant and
  distant below eventual. A horizon clause changes when its text or its tag
  list changes. Rewrapping or reindenting a clause is not a change.
- **S.diff.4** (H.hz.1, H.hz.3) When it lists any horizon clause,
  `shed diff` ends with the tier of the horizon amendment: the highest tier
  among the horizon clauses it counts. Each listed clause counts at its tier
  from S.diff.3, except a changed clause whose only change is gaining
  `realised`, which is listed but not counted, as under S.owner.11. A
  listed clause whose `refines` tag (S.horizon.6) is added, removed or
  names a different clause between the two revisions, including an added
  clause that carries one and a removed clause that carried one, also
  counts at the tier of the clause its tag names on each revision where it
  carries the tag. When it counts no horizon clause, `shed diff` gives no
  tier.
- **S.diff.5** (H.hz.3) On the line of each horizon clause it lists,
  `shed diff` names each clause at whose tier S.diff.4 counts the listed
  clause because of its `refines` tag (S.horizon.6), with that clause's ID
  and its tier on the revision where the tag names it: first the clause the
  tag names on the first revision, then the one it names on the second. A
  listed clause that S.diff.4 counts only at its own tier, or not at all,
  names none.
