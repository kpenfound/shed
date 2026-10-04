# The owner

- **S.owner.1** (H.owner.1) `shed inbox` lists, in the order they became
  contested, every contested unit with its short change ID, title, bounce
  count and the reason it was contested. Units that have left contested are
  not listed.
- **S.owner.2** (H.owner.1) `shed inbox` then lists the horizon clauses
  added, changed or removed on main since the main commit recorded by the
  previous `shed inbox`, in document order, each with its ID, its tier and
  whether it was added, changed or removed. A removed clause shows its tier
  at the recorded commit. A clause counts as changed when its tag list
  differs or its text differs once runs of whitespace are collapsed to one
  space. The first `shed inbox` lists no horizon changes. If the recorded
  commit is not an ancestor of main, `shed inbox` says so, lists no horizon
  changes, and the next recorded commit starts afresh.
- **S.owner.3** (H.owner.1) Every `shed inbox` records the main commit it
  read in the tracker, so that `shed tracker rebuild` keeps it. With
  `-peek` it lists the same entries and records nothing. Reading the inbox
  never moves a unit and never starts or stops a stage.
- **S.owner.4** (H.owner.1) `shed answer <unit> retry <reason>` moves a
  contested unit to proposed with the owner as actor and the reason as the
  move's reason. The unit keeps its bounce count, so under S.unit.6 its
  next bounce moves it back to contested. Every later bundle of the unit except committee debate bundles (S.shed.1)
  holds the owner's answers to it, oldest first, each with its time, kind
  and reason.
- **S.owner.5** (H.owner.1) `shed answer <unit> defer <reason>` archives a
  contested unit on the deferred shelf as under S.shed.10, with the owner as
  actor and the reason as what would change the decision; the archive entry
  also holds the owner's answers.
- **S.owner.6** (H.owner.1) `shed answer` reads its first argument as a
  charter clause when it parses as a charter citation under S.cite.1, with
  or without a revision, and as a unit otherwise; an answer to a clause is
  governed by S.owner.10. Shed refuses an answer to a unit with a kind
  other than `retry`, `defer`, `reject`, `approve` (S.shed.17), `agree` or
  `disagree` (S.owner.20), an answer of the first four kinds to a unit that
  is not contested, and an answer with an empty reason. A refused answer
  records nothing and moves nothing.
- **S.owner.7** (H.owner.1) `shed inbox` marks as new each contested unit
  it lists whose latest move to contested has a higher event sequence number
  (S.track.3, S.track.4) than the one stored by the previous recorded
  `shed inbox`, and leaves the others unmarked. Every contested unit is new
  when no `shed inbox` has been recorded. `-peek` marks against the previous
  recorded `shed inbox` just as a recording read does. Each recorded
  `shed inbox` stores in the tracker, beside the main commit of S.owner.3,
  the sequence number of the latest tracker event at the moment it read the
  contested units, so `shed tracker rebuild` keeps it and a unit contested
  after that moment is new at the next `shed inbox`. So a unit that is
  retried and becomes contested again after the previous recorded
  `shed inbox` is new again.
- **S.owner.8** (H.owner.1) `shed answer <unit> reject <reason>` archives a
  contested unit on the rejected shelf as under S.shed.10, with the owner as
  actor and the reason as the move's reason. The entry cites as violated
  every charter clause the reason names, in the order they first appear and
  without repeats, and also holds the owner's answers. The reason names a
  clause through its ID-shaped tokens, read whole as citations under
  S.cite.1 anywhere in the text: `C12` names C12 and not C1, `XC1` names
  nothing, and punctuation around a token, as in `(C3)` or `C3,`, does
  not change it. Two charter citations joined by "to" name every clause of
  the charter on main from the first to the second, in increasing order.
  Tokens of other kinds, such as spec or horizon IDs, name no charter
  clause. Shed refuses a `reject` whose reason names no charter clause, has
  a charter citation with a revision, has a charter range whose ends do not
  increase, or has a charter ID that is not a clause of the charter on main,
  as a retired ID is not; a refused answer records nothing and moves
  nothing.
- **S.owner.9** (H.owner.1) `shed inbox` then lists charter questions: each
  clause of the charter on main that the entries of at least two units on
  the rejected shelf cite as violated. For each clause shed counts only
  the units whose move to archived has a higher event sequence number than
  the one recorded by the clause's latest keep (S.owner.10), or the whole
  shelf when the clause has never been kept. Questions are listed in
  charter order, each with its clause ID and, in the order they were
  archived, the short change ID and title of every counted unit. An entry
  cites a clause when one of its citations is that clause's ID, with or
  without a revision; a unit whose entry cites a clause more than once counts once.
  Citations of other kinds, such as spec or horizon IDs, and charter IDs
  that are not clauses of the charter on main, as a retired ID is not,
  raise no question. A question is marked new when at least one of its
  counted units' moves to archived has a higher event sequence number than
  the one stored by the previous recorded `shed inbox` (S.owner.7), and every
  question is new when no `shed inbox` has been recorded. `-peek` lists and
  marks the same questions. Listing a question moves no unit and holds back
  no session or proposal.
- **S.owner.10** (H.owner.1) `shed answer <clause> keep <reason>`, where
  `<clause>` is a charter clause ID, answers the charter question raised for
  that clause (S.owner.9) by keeping the clause as it stands. It records in
  the event log, with the owner as actor and the reason as the event's
  reason, the clause ID and the sequence number of the latest tracker event
  before the answer, so `shed tracker rebuild` keeps it. Under S.owner.9
  the question then leaves the inbox and is raised again only when at
  least two units archived after that keep cite the clause. Questions for
  other clauses are unaffected. Shed refuses an answer to a clause whose
  kind is not `keep`, whose clause is not a clause of the charter on main
  or carries a revision, whose clause has no question listed for it at
  that moment, or whose reason is empty; a refused answer records nothing.
  A keep moves no unit and changes no archive entry.
- **S.owner.11** (H.owner.1, H.hz.11) A landed unit's commit amends a horizon
  clause in `horizon.md` when, against its parent on main, it adds or
  removes the clause, or changes it as under S.owner.2 other than only by
  adding `realised` to its tag list (S.hz.1); a clause whose text or other
  tags also change is amended. A horizon amendment is a landed unit whose
  commit amends at least one horizon clause. The landing of every unit
  records in the event log whether it is a horizon amendment, so
  `shed tracker rebuild` keeps the count of horizon amendments, which is the
  number of landings recorded as horizon amendments; a landing whose event
  records neither, because it landed before shed recorded this, does not
  count. The seal that follows the owner's `approve` (S.shed.17) records in
  the event log, beside what S.shed.8 records, that it follows that
  approval, so `shed tracker rebuild` keeps it; no other seal records this,
  and a seal whose event records nothing either way, because it was made
  before shed recorded this, counts as not following an approval. A horizon
  amendment is owner-accepted when it lands by `shed frame -accept`
  (S.frame.4), or when the unit's latest seal before its landing records
  that it follows the owner's `approve`; a unit sealed again after such a
  seal, as when it is resealed out of the amendment lane (S.shed.11), is
  judged by that later seal alone. Every other horizon amendment is
  auto-accepted. The landing of a horizon amendment
  records in the event log whether it is owner-accepted, so
  `shed tracker rebuild` keeps the sampling count, which is the number of
  auto-accepted horizon amendments landed; a horizon amendment whose
  landing records neither, because it landed before shed recorded this, is
  owner-accepted when it landed by `shed frame -accept` and auto-accepted
  otherwise. The operator setting `owner.sample_every`, a
  whole number that defaults to 0, samples horizon amendments to the owner:
  when it is N above 0, the landing of an auto-accepted horizon amendment
  whose place in the sampling count is a multiple of N, counted from one in
  landing order, also records that the unit is sampled; when it is 0
  nothing is sampled, and auto-accepted horizon amendments landed meanwhile
  still count. Changing the setting does not restart the count. Shed
  refuses a negative `owner.sample_every` as under S.config.2. An
  owner-accepted horizon amendment is never recorded as sampled and takes
  no place in the sampling count, so with N above 0 one in every N
  auto-accepted horizon amendments is sampled. Sampling moves no unit and
  never holds back or fails a landing.
- **S.owner.12** (H.owner.1) `shed inbox` then lists sampled amendments:
  each unit sampled (S.owner.11) after the sequence number stored by the
  previous recorded `shed inbox` (S.owner.7), or every sampled unit when no
  `shed inbox` has been recorded, in landing order. Each is listed with its
  short change ID, title and landed commit, and with the horizon clauses its
  commit amended, in document order, each with its ID and whether it was
  added, changed or removed; a clause its commit only marked realised is not
  listed. `-peek` lists the same sampled amendments.
- **S.owner.13** (H.hz.3) On the line of each horizon clause it lists under
  S.owner.2, `shed inbox` names each clause at whose tier `shed diff`
  between the recorded commit and main would count the listed clause
  because of its `refines` tag (S.horizon.6, S.diff.4), with that clause's
  ID and its tier on the commit where the tag names it: first the clause
  the tag names on the recorded commit, then the one it names on main. A
  listed clause whose `refines` tag is the same on both commits, or that
  carries none on either, names none. `-peek` lists the same names.
- **S.owner.14** (H.owner.1) `shed inbox`, including `-peek`, lists every
  proposed framing in opening order, with its short change ID, title, and
  commands to review its workspace, accept it or discard it. Reading the
  inbox does not dismiss a framing; it stays listed until accepted or
  discarded.
- **S.owner.15** (H.unit.8) Beside each contested unit it lists under
  S.owner.1, `shed inbox` shows how long the unit has waited: the time from
  its latest move to contested, as recorded in the event log (S.track.3),
  to the moment the inbox is read. The tracker keeps that time with the
  unit when it records the move, so `shed tracker rebuild` (S.track.5)
  restores it and reading it needs no scan of the log. The wait is written
  rounded down to the whole minute, as a duration such as `26h5m`; the
  rounding applies only to what is written. The inbox marks the unit
  `overdue` when the unrounded wait is strictly longer than the operator
  setting `shed.contested_timeout`, a duration such as `72h` that defaults
  to 72 hours, so a wait equal to the timeout is not overdue. When the
  timeout is zero, expiry is off and no unit is ever marked overdue.
  `-peek` shows the same waits and marks. Showing a wait or an overdue
  mark moves no unit and starts no session.
- **S.owner.16** (H.unit.12) `shed inbox` then lists expired units: each
  unit the frame builder archived under S.frame.7 whose move to archived
  has a higher event sequence number than the one stored by the previous
  recorded `shed inbox` (S.owner.7), or every such unit when no
  `shed inbox` has been recorded, in the order they were archived. Each is
  listed with its short change ID, title, shelf, rejected or deferred, the
  frame builder's reason, and how long it had waited from its latest move
  to contested to the archive, as recorded by S.frame.7 and written as
  under S.owner.15. Units archived by any other actor are not listed here.
  `-peek` lists the same expired units. Listing an expired unit moves no
  unit and changes no archive entry.
- **S.owner.17** (H.owner.5) A unit's change alters the charter when
  `charter.md` on the change differs byte for byte from `charter.md` on the
  latest main commit the change descends from, the file being present on
  one and absent on the other counting as differing. Shed seals no unit
  whose change alters the charter at the moment it would seal it, whichever
  clause would seal it (S.shed.6, S.shed.14, S.shed.17). In place of the
  seal the unit bounces to its painter as under S.shed.6, with a reason
  naming the unit's short change ID and `charter.md`. After a debate round
  this check comes before those of S.shed.12 and S.shed.16, so such a unit
  is neither moved to contested by S.shed.16 nor held back by S.serve.7. A
  unit whose `charter.md` matches that main commit is sealed as before, even
  when main's charter has changed since the unit opened.
- **S.owner.18** (H.owner.5) Verifying a unit (S.verify.1) fails its checks
  when its change alters the charter under S.owner.17. The unit returns to
  implementing under S.verify.4, and the notice to the mechanic names the
  unit's short change ID and `charter.md`.
- **S.owner.19** (H.owner.5) Landing a queued unit (S.queue.2, S.vcs.6)
  checks its change after the rebase onto main and after any wheelbuilder
  session that resolves its conflicts has been captured (S.vcs.4), and
  before S.vcs.6 rewrites the change as one commit, so the check sees the
  files main would take, including any edit that session made to
  `charter.md`. It checks the same way when no wheelbuilder session runs.
  The landing lands nothing when that change alters the charter under
  S.owner.17 against main as it stands at the check: it rewrites no commit,
  neither moves nor pushes main, keeps the unit's workspace and records no
  landing under S.vcs.7. The unit then reopens with shed as actor and a
  reason naming its short change ID and `charter.md`, and counts a bounce
  as any reopen does under S.unit.5; `shed land` prints that reason. Main
  stays at the commit it had before the landing.
- **S.owner.20** (H.hz.12) `shed answer <unit> agree <reason>` and
  `shed answer <unit> disagree <reason>` answer a sampled amendment: a unit
  whose landing recorded that it is sampled (S.owner.11). The answer is
  recorded in the event log with the owner as actor, the reason as the
  event's reason, the unit, and whether the owner agreed, so
  `shed tracker rebuild` keeps it. Shed refuses either kind for a unit
  whose landing did not record it as sampled, as for a unit that has not
  landed, and for a unit that already has an `agree` or `disagree` answer,
  including one recorded before a `shed tracker rebuild`; a refused answer
  records nothing. An `agree` or `disagree` answer moves no unit, changes
  no archive entry and makes no commit, so main is unchanged.
