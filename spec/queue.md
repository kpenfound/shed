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
- **S.queue.7** (H.queue.6) Shed orders the queued units for landing from
  the tracker alone: the landing order is the order the units opened, as
  `shed status` lists them under S.track.9. When `shed serve` lands
  (S.serve.4), it lands the first queued unit in the landing order that is
  neither marked for horizon review (S.queue.5) nor held back (S.queue.9).
  A marked or held back unit keeps its place, and the units behind it may
  land before it.
- **S.queue.8** (H.queue.6) On the line of each queued unit it lists under
  S.track.9, `shed status` shows the unit's place in the landing order of
  S.queue.7 as `land #<n>`, counting from 1 and counting every queued unit,
  marked for horizon review or not. Units in other states show no place.
- **S.queue.9** (H.queue.7) Two queued units are entangled when the spec
  footprints recorded at their last seals, the clauses each modifies and
  depends on, share a clause, as S.fp.5 counts it; horizon clauses never
  count. A queued unit waits behind every queued unit entangled with it
  whose spec footprint holds fewer distinct clauses, or as many and an
  earlier place in the landing order of S.queue.7. A unit that waits behind
  at least one unit is held back: `shed serve` does not land it, and
  `shed land` refuses it, naming the units it waits behind by their short
  change IDs, and lands nothing. Only queued units count, so a unit stops
  waiting behind another once that unit lands or leaves queued. A held back
  unit keeps its place in the landing order, as does a unit entangled with
  no other queued unit. Being held back moves no unit and records no event.
- **S.queue.10** (H.queue.7) On the line of each queued unit held back
  under S.queue.9, `shed status` shows, after its place in the landing
  order (S.queue.8), `waits behind` followed by the short change IDs of the
  units it waits behind, in the landing order and separated by commas. A
  unit that is not held back shows no such list.
