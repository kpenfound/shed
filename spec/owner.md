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
  next bounce moves it back to contested. Every later bundle of the unit
  holds the owner's answers to it, oldest first, each with its time, kind
  and reason.
- **S.owner.5** (H.owner.1) `shed answer <unit> defer <reason>` archives a
  contested unit on the deferred shelf as under S.shed.10, with the owner as
  actor and the reason as what would change the decision; the archive entry
  also holds the owner's answers.
- **S.owner.6** (H.owner.1) `shed answer` reads its first argument as a
  charter clause when it parses as a charter citation under S.cite.1, with
  or without a revision, and as a unit otherwise; an answer to a clause is
  governed by S.owner.10. Shed refuses an answer to a unit that is not
  contested, with a kind other than `retry`, `defer`, `reject` or
  `approve` (S.shed.17), or with an empty reason. A refused answer records
  nothing and moves nothing.
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
- **S.owner.11** (H.owner.1) A landed unit's commit amends a horizon
  clause in `horizon.md` when, against its parent on main, it adds or
  removes the clause, or changes it as under S.owner.2 other than only by
  adding `realised` to its tag list (S.hz.1); a clause whose text or other
  tags also change is amended. A horizon amendment is a landed unit whose
  commit amends at least one horizon clause. The landing of every unit
  records in the event log whether it is a horizon amendment, so
  `shed tracker rebuild` keeps the count of horizon amendments, which is the
  number of landings recorded as horizon amendments; a landing whose event
  records neither, because it landed before shed recorded this, does not
  count. The operator setting `owner.sample_every`, a whole
  number that defaults to 0, samples horizon amendments to the owner: when
  it is N above 0, the landing of a horizon amendment whose place in that
  count is a multiple of N, counted from one in landing order, also records
  that the unit is sampled; when it is 0 nothing is sampled, and horizon
  amendments landed meanwhile still count. Changing the setting does not
  restart the count. Shed refuses a negative `owner.sample_every` as under
  S.config.2. Sampling moves no unit and never holds back or fails a
  landing.
- **S.owner.12** (H.owner.1) `shed inbox` then lists sampled amendments:
  each unit sampled (S.owner.11) after the sequence number stored by the
  previous recorded `shed inbox` (S.owner.7), or every sampled unit when no
  `shed inbox` has been recorded, in landing order. Each is listed with its
  short change ID, title and landed commit, and with the horizon clauses its
  commit amended, in document order, each with its ID and whether it was
  added, changed or removed; a clause its commit only marked realised is not
  listed. `-peek` lists the same sampled amendments.
