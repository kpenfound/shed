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
  clauses the landing changed or removed, and that gains no refining clause
  under S.queue.6, is left as it is, and no event is recorded for it. A
  unit with one or more changed clauses gets one notice
  naming the landed unit by its short change ID and giving, for each such
  clause in the order of the parent's horizon, its ID and its tag list and
  text before and after. The notice is in the unit's next non-debate bundle as under
  S.sess.6, appears in `shed unit log` for the unit, and moves nothing.
- **S.queue.4** (H.queue.5) A unit covered by S.queue.3 whose recorded
  horizon clauses include one the landing removed from the horizon reopens,
  with shed as actor and a reason naming the landed unit and each removed
  clause, and counts a bounce as any reopen does. It gets no notice under
  S.queue.3 for that landing. Shed makes these reopens and records these
  notices before the rebase sweep of S.vcs.10 for the same landing, so a
  reopened unit is rebased there as a proposed unit.
- **S.queue.5** (H.queue.5) When a notice under S.queue.3 gives a clause
  whose text differs before and after once runs of whitespace are collapsed
  to one space, the unit is also marked for horizon review. A clause whose
  tag list alone differs does not mark it. For each marked unit the
  wheelbuilder controller runs a review stage: one wheelbuilder session
  whose bundle shows the unit's pending notices without delivering them, and
  whose `done` accepts `consistent` or `reopen` with a written reason. The
  notices stay pending for the unit's next session of another kind, as under
  S.sess.6. A review starts only when the unit has no stage running, and
  each pass of `shed serve` starts reviews before the landing that S.serve.4
  puts first. While a unit is marked, shed starts no implementation,
  verification or landing for it: `shed land` refuses it. On `reopen` the
  unit reopens with the wheelbuilder as actor and that reason, and counts a
  bounce as any reopen does. On `consistent` the unit keeps its state and
  its seal, and a notice giving the wheelbuilder's reason joins its pending
  notices. `shed unit log` records either outcome, and either one clears the
  mark. The mark also clears when the unit leaves sealed, implementing,
  verifying and queued. A session that reports neither outcome leaves the
  mark for a later stage.
- **S.queue.6** (H.queue.5) After a landing, a unit covered by S.queue.3
  also counts as affected when the horizon gained a clause refining one of
  its recorded horizon clauses: a clause on the landed commit whose
  `refines` tag (S.horizon.6) names that recorded clause, and which on the
  parent was absent or carried no `refines` tag naming it. Its notice under
  S.queue.3 then also gives, after any changed clauses and in the order of
  the landed commit's horizon, each such refining clause's ID, tag list and
  text, marked as gained, and names the recorded clause it refines. A unit
  whose only entries are gained clauses still gets that one notice, is not
  marked for horizon review under S.queue.5 because of them, keeps its state
  and its seal, and moves nothing. A unit that reopens under S.queue.4 for
  the same landing gets no notice.
