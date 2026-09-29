# Merge queue

- **S.queue.1** (H.queue.1) Shed lands queued units one at a time through
  `shed land`.
- **S.queue.2** (H.queue.2) Landing rebases a queued unit onto main first,
  keeping any conflicts in its files. A wheelbuilder session resolves them
  against the sealed spec in a writable copy. If the wheelbuilder reports
  `unresolvable`, or conflicts remain, the unit reopens. A unit that changes
  nothing once rebased reopens.
- **S.queue.3** (H.queue.5) After a landing, shed compares the horizon on
  the landed commit with the horizon on its parent, counting a clause as
  changed as under S.owner.2. For every other unit that is sealed,
  implementing, verifying or queued, it takes the horizon clauses recorded
  in the footprint at that unit's last seal. A unit none of whose horizon
  clauses the landing changed or removed is left as it is, and no event is
  recorded for it. A unit with one or more changed clauses gets one notice
  naming the landed unit by its short change ID and giving, for each such
  clause in the order of the parent's horizon, its ID and its tag list and
  text before and after. The notice is in the unit's next bundle as under
  S.sess.6, appears in `shed unit log` for the unit, and moves nothing.
- **S.queue.4** (H.queue.5) A unit covered by S.queue.3 whose recorded
  horizon clauses include one the landing removed from the horizon reopens,
  with shed as actor and a reason naming the landed unit and each removed
  clause, and counts a bounce as any reopen does. It gets no notice under
  S.queue.3 for that landing. Shed makes these reopens and records these
  notices before the rebase sweep of S.vcs.10 for the same landing, so a
  reopened unit is rebased there as a proposed unit.
