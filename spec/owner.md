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
- **S.owner.6** (H.owner.1) Shed refuses a `shed answer` to a unit that is
  not contested, with a kind other than `retry` or `defer`, or with an empty
  reason. A refused answer records nothing and moves nothing.
