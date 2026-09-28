# The shed

- **S.shed.1** (H.shed.1) `shed debate <unit>` debates a proposed unit in
  rounds. In each round every committee member, `concurrency.committee` of
  them, runs a session at the same time against the same revision of the
  proposal, with a copy of its files whose changes are thrown away.
- **S.shed.2** (H.shed.2) A member objects with the `object` tool, giving a
  kind (`charter`, `horizon`, `size` or `spec`), the clause IDs it cites and
  its text. Shed refuses an objection of an unknown kind, one with no
  citation, and one citing a clause that does not resolve against the
  proposal's documents.
- **S.shed.3** (H.shed.3) A charter objection standing at the end of a round
  rejects the proposal, whatever the other members say. The painter is not
  asked to answer it.
- **S.shed.4** (H.shed.4) A proposal whose only standing objections at the
  round cap are horizon objections is deferred, with those objections as what
  would change the decision.
- **S.shed.5** (H.shed.5) Between rounds the painter answers the standing
  objections once, with the `answer` tool, and may revise the proposal's
  files. An objection stands until the member who raised it withdraws it with
  the `withdraw` tool; no one else can withdraw it.
- **S.shed.6** (H.shed.6) When no objection stands after a round, the unit is
  sealed. The round cap is `shed.max_rounds`, and reaching it approves
  nothing: a proposal with other objections standing at the cap bounces back
  to its painter. It stays proposed, counts a bounce, and its next debate
  starts afresh from round one.
- **S.shed.7** (H.shed.7) A size objection says the footprint is too large and
  how to split it by clause. It stands like any other objection until its
  member withdraws it.
- **S.shed.8** (H.shed.8) Sealing records main's current commit and the unit's
  change ID as the unit's seal, records its footprint and moves it to sealed.
- **S.shed.9** (H.shed.9) Shed records every objection, answer and withdrawal
  in the tracker. The debate record, grouped by debate, is in every later
  bundle of the unit and in its archive entry.
- **S.shed.10** (H.shed.10, H.doc.1) Archiving a proposal writes an entry to
  `archive/rejected/<change>.md` or `archive/deferred/<change>.md` on the
  `shed/archive` branch, which shares no history with main, and pushes the
  branch to the configured remote. It then records the unit as archived and
  discards its change. A rejected entry cites the charter clauses violated
  and a deferred one says what would change the decision; both hold the
  proposal's spec changes and its debate.
- **S.shed.11** (H.shed.11) A proposed unit whose latest reopen requested an
  amendment (S.impl.5) is debated in the amendment lane: every rule of the
  debate holds as for any proposal, but the round cap is
  `shed.amendment_rounds` in place of `shed.max_rounds`, and each member's
  session is told that cap. The unit stays in the amendment lane,
  including after a bounce at the cap, until it is next sealed.
