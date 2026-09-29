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
  to its painter. It counts a bounce, and unless that bounce moves it to
  contested under S.unit.6 it stays proposed and its next debate starts
  afresh from round one.
- **S.shed.7** (H.shed.7) A size objection says the footprint is too large and
  how to split it by clause. It stands like any other objection until its
  member withdraws it.
- **S.shed.8** (H.shed.8) Sealing records main's current commit, the unit's
  change ID and the commit that change points to as the unit's seal, records
  its footprint and moves it to sealed.
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
- **S.shed.12** (H.shed.11) In the amendment lane (S.shed.11) the proposal's
  scope is the spec clauses in the footprint recorded at the unit's last seal,
  both those it modified and those it depended on, and every member's and the
  painter's session is told those clauses. A clause is outside the scope when
  it is not in the scope and its text in the proposal differs from its text
  on the main commit the unit's change is based on, which is the main commit
  recorded in that seal (S.shed.8), onto which sealing rebased the change
  (S.vcs.10), until a later landing rebases it again under S.vcs.10, so
  clauses that main changed after the seal never count against the proposal.
  When a round ends with no objection standing but a clause is outside the
  scope, the unit is not sealed: it bounces to its painter as under S.shed.6,
  with a reason naming each clause outside the scope.
- **S.shed.13** (H.shed.11) When a unit is sealed out of the amendment lane
  (S.shed.11), every mechanic session of its next implementation has a bundle
  that states the unit was resealed after an amendment and gives the
  amendment's diff. The diff compares the clauses of `spec/` on the unit's
  commit recorded in its most recent earlier seal with those on the unit's
  commit recorded in this seal (S.shed.8). It lists each clause whose text
  differs, giving both texts, with a clause that was added or removed shown as
  such. It leaves out a clause whose text on each of those two unit commits is
  the same as on the main commit recorded in the same seal, a clause absent
  from both counting as the same, because main changed that clause and the
  amendment did not. When the diff lists no clause, the bundle says the
  amendment changed no clause. Mechanic bundles after a seal outside the
  amendment lane carry no such statement.
- **S.shed.14** (H.shed.11) In the amendment lane (S.shed.11), a debate that
  reaches its round cap with objections standing, none of them a charter
  objection, rejects the amendment, in place of the bounce of S.shed.6 and the
  deferral of S.shed.4; a charter objection standing still rejects the
  proposal under S.shed.3. Rejecting the amendment archives nothing and keeps
  the unit's change. Shed makes the files under `spec/` on the unit's change
  exactly those of the unit's commit recorded at the last seal (S.shed.8)
  rebased onto the main commit the change is based on: each takes its
  content there, any file under `spec/` absent there is removed, and every
  other file on the change is left as it is. Clauses that main added, changed
  or removed after that seal therefore stay as main has them, and a conflict
  between main and the sealed spec is stored in the files as under S.vcs.10.
  It records the standing objections as the reason and seals the unit as
  under S.shed.8. If a restored file under `spec/` holds a conflict, or
  sealing's rebase fails or conflicts under `spec/` (S.vcs.10), the unit is
  not sealed: it keeps the restored files and bounces to its painter as
  S.vcs.10 says, naming those conflicts or the failure, and stays in the
  amendment lane.
  The restore and the seal happen together: while S.serve.7 holds sealing
  back, nothing is restored and the unit waits in proposed in the amendment
  lane like a proposal that reached consensus, its debate not started afresh,
  so its next debate runs no further round and rejects the amendment as here.
  Once sealed the unit leaves the amendment lane and counts no further
  bounce. In place of the statement of S.shed.13, every mechanic session of
  its next implementation has a bundle that states the requested amendment
  was rejected, that the sealed spec stands as written, and gives the
  objections that stood at the cap.
- **S.shed.15** (H.owner.4) When a proposal archived under S.shed.10 adds,
  changes or removes horizon clauses in `horizon.md` against the latest main
  commit its change descends from, its archive entry lists those clauses in
  document order beside its spec changes and debate. Each is listed with its
  ID and whether it was added, changed or removed, with its tag list and text
  before the proposal, after it, or both, as each exists. A horizon clause
  counts as changed when its tag list differs or its text differs once runs
  of whitespace are collapsed to one space. The entry of a proposal that
  changes no horizon clause lists no horizon changes.
