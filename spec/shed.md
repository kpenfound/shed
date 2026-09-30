# The shed

- **S.shed.1** (H.shed.1) `shed debate <unit>` debates a proposed unit in
  rounds. In each round every committee member, `concurrency.committee` of
  them, runs a session at the same time against the same revision of the
  proposal, with a copy of its files whose changes are thrown away.
  Each member receives only its own objections, answers and withdrawals from
  the current debate; retries start with no earlier debate history. Committee
  debate bundles omit the unit's latest reason, owner's answers and pending
  notices, which can quote shared history; those notices are not delivered by
  debate sessions. An amendment debate includes the mechanic's amendment
  request as current proposal input. The painter and owner retain the complete
  debate record.
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
  files. Its `declare` tool replaces the dependencies or horizon advances
  supplied, preserving omitted fields and clearing a list supplied empty.
  After a successful reply is captured, shed validates the declaration under
  S.unit.7 and S.fp.2 and records it with the painter as actor, computing
  modified clauses from the captured files before the next round. A failed
  reply records no declaration; an invalid declaration stops the debate
  without replacing the footprint. Declaration updates do not widen the
  amendment scope recorded at the last seal (S.shed.12).
  An objection stands until the member who raised it withdraws it with
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
  in the tracker. The debate record, grouped by debate, is in later bundles except that committee debate sessions receive only the
  member's current-debate record under S.shed.1. The archive keeps the complete
  record.
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
  the unit's change. Shed makes the files under `spec/` and the file
  `horizon.md` on the unit's change exactly those of the unit's commit
  recorded at the last seal (S.shed.8) rebased onto the main commit the
  change is based on: each takes its content there, any file under `spec/`
  absent there is removed, and every other file on the change is left as it
  is. Clauses that main added, changed or removed after that seal therefore
  stay as main has them, and a conflict between main and the sealed spec or
  horizon is stored in the files as under S.vcs.10.
  It records the standing objections as the reason and seals the unit as
  under S.shed.8. If a restored file holds a conflict, or
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
- **S.shed.16** (H.hz.1) When a debate round ends with no objection
  standing and no clause outside the scope under S.shed.12, shed takes the
  tier of the proposal's horizon amendment before the unit is sealed and
  before S.serve.7 can hold its seal back. The tier is the one `shed diff`
  gives under S.diff.4 between the latest main commit the unit's change
  descends from and the unit's change. When that tier is distant or
  eventual, the unit is neither sealed nor held: it moves to contested with
  shed as actor and a reason naming the tier and, in document order, each
  horizon clause counted at that tier. This move counts no bounce. A
  proposal with no tier, or a near or soon tier, is sealed or held as
  before. A painter session on a unit that S.serve.7 holds back after such
  a round, such as one resolving conflicts a landing stored in its files
  under S.vcs.10, ends the hold: the unit stays proposed, counts no bounce,
  and its next debate starts afresh from round one, so the tier is taken
  again before it is sealed. A held seal released with no painter session
  run on the unit since its round ended takes no tier again, because its
  change then holds only what that round saw, rebased. A unit bounced under
  S.shed.12 takes no tier. The seal of S.shed.14 takes no tier, and a
  painter session does not end its wait, because it restores the horizon of
  the last seal whatever a painter wrote meanwhile. The seal of S.shed.17
  takes no tier.
- **S.shed.17** (H.hz.1) `shed answer <unit> approve <reason>` moves a
  contested unit whose latest move to contested was made under S.shed.16 or
  S.shed.18 to proposed, with the owner as actor and the reason as the
  move's reason. Its next debate runs no round and seals it as under S.shed.8, without the
  check of S.shed.16; while S.serve.7 holds sealing back it waits in proposed
  like a proposal that reached consensus. A unit in the amendment lane
  (S.shed.11) stays in it until that seal, which is a seal out of the
  amendment lane under S.shed.13. A bounce before that seal ends the
  approval, and so does a painter session on the unit before that seal,
  such as one resolving conflicts a landing stored in its files under
  S.vcs.10; the painter session counts no bounce. Once the approval ends,
  the unit's later debates run as for any proposal, starting afresh from
  round one, so S.shed.16 takes the tier of its horizon amendment as the
  painter left it. Shed
  refuses `approve` for a unit whose latest move to contested was not made
  under S.shed.16 or S.shed.18.
- **S.shed.18** (H.hz.1) When a debate outside the amendment lane
  (S.shed.11) reaches its round cap with objections standing, every one of
  them a horizon objection, and at least one committee member of its last
  round has no objection standing, the debate is split: shed takes the tier
  of the proposal's horizon amendment as under S.shed.16. When that tier is
  soon, the unit moves to contested with shed as actor and a reason naming
  the tier and each standing objection, in place of the deferral of
  S.shed.4. This move counts no bounce. A debate that is not split, or a
  split one whose proposal has no tier or a near, distant or eventual tier,
  is deferred, bounced or rejected as before under S.shed.3, S.shed.4 and
  S.shed.6; a distant or eventual amendment is accepted only through
  S.shed.16 and S.shed.17 in any case. When the owner answers `retry`
  (S.owner.4) to a unit whose latest move to contested was made under this
  clause, the unit goes back to its painter as the bounce of S.shed.6 would,
  with the objections that stood at the cap as the reason, but counts no
  bounce, and its next debate starts afresh from round one. The amendment
  lane is left out because a debate there that reaches its cap with
  objections standing rejects the amendment under S.shed.14, restoring the
  horizon of the last seal, so no split horizon amendment is accepted there
  without the owner.
- **S.shed.19** (H.hz.3) In the reason of a move to contested under
  S.shed.16, each horizon clause counted at the tier only because of its
  `refines` tag (S.horizon.6, S.diff.4) is followed by each clause at whose
  tier it counts, with that clause's ID and its tier, as `shed diff` names
  them under S.diff.5. A clause counted at the tier by its own tier names
  none, even when its tag also names a clause of that tier.

- **S.shed.20** (H.shed.1, H.sess.2, H.track.6) Committee debate members
  receive a shared committee prompt plus a review perspective. The operator's
  `committee.perspectives` list defaults to `correctness`, `integration`,
  `scope`, covering correctness and charter compliance, integration and direct
  dependencies, and scope and materiality of objections. Every perspective
  retains authority to raise any valid objection. Member numbers starting at
  one cycle through the list in order, keeping their assignments across rounds.
  Files `committee-correctness.md`, `committee-integration.md` and
  `committee-scope.md` in the state directory's `prompts` override the shipped
  perspectives. Independently, members cycle through `committee.profiles`;
  an empty list uses the committee role's profile. Each selected profile keeps
  its own fallback chain. These assignments apply to debate, not code review.
