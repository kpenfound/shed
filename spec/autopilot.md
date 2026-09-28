# Autopilot

- **S.paint.1** (H.shed.12) The painter proposes when the gap it may work on
  is not empty, fewer than `painter.max_proposed` units are proposed, and it
  is not backing off. Shed opens a unit on
  main for it. The painter writes a spec diff in a writable copy, declares
  the proposal's title, dependencies and horizon clauses with the `declare`
  tool and reports `proposed`. A painter that reports `nothing`, or ends
  without declaring, leaves no proposal: its unit is archived as deferred,
  with no archive entry, and its change discarded.
- **S.paint.2** (H.shed.12) The painter's bundle holds the gap with the spec
  clauses that already advance each clause, the deferred and rejected
  shelves, and the units in flight with their footprints.
- **S.paint.3** (H.shed.13) The gap the painter may work on holds only near
  and soon horizon clauses that are not realised and that no unit in flight
  advances. `declare` refuses a horizon clause outside it.
- **S.serve.1** (H.sched.2) `shed serve` runs a controller for each role
  that has work: the painter, the shed, the mechanic, the verifier and the
  wheelbuilder, all sharing the tracker. With `-once` it stops when no stage
  is running and no controller has anything to start; if it started nothing
  at all, it says why: drafts waiting to be declared, contested units, a
  full in-flight cap, or what holds the painter back.
- **S.serve.2** (H.sched.3) Every pass reads the tracker's current state, so
  a controller acts on units however they got there. A stage that ends wakes
  every controller, and `serve.tick` wakes them when nothing else does.
- **S.serve.3** (H.sched.4) Controllers start stages in the background and
  never wait on a session. A stage records its transitions in the tracker
  when it ends and wakes the controllers.
- **S.serve.4** (H.sched.5) At most `concurrency.units` units are
  implementing or verifying at once. Each pass starts work downstream first:
  landing, verification, implementation, debate, then proposals. One unit
  lands at a time, one debate runs at a time, and no unit ever has two stages
  running. A proposal that declares no horizon clause is a draft and is not
  debated.
- **S.serve.5** (H.sched.6) The painter, the only controller that creates
  work, is throttled by its results. After a proposal that is sealed it
  proposes again as soon as `painter.max_proposed` allows. After a proposal
  that goes nowhere, archived or contested without being sealed, it waits
  `painter.interval`, doubling for each further one in a row up to
  `painter.max_interval`; the next sealed proposal ends the streak. A
  proposal whose painter session failed before doing anything, without an
  outcome or a cost, does not count. Every other controller is woken by
  stages ending and by the tick.
- **S.serve.6** (H.sched.7) No stage starts while the sessions of the last 24
  hours cost `budget.per_day_usd` or more. `shed status` shows the pause, and
  shows how many of the latest sessions failed for infrastructure reasons in a
  row when any did.
- **S.serve.7** (H.sched.9) Shed seals no unit while `concurrency.in_flight`
  units are sealed through queued; a proposal that reaches consensus waits in
  proposed, and `shed serve` debates no proposal meanwhile. The default cap is
  one.
- **S.serve.8** (H.track.5) After a crash, `shed serve` carries on from what
  the tracker holds: a debate from its last round, an implementation from its
  first unfinished step. A painter's draft that no session was working on is
  archived and discarded.
- **S.hz.1** (H.hz.10) A mechanic may mark horizon clauses the unit advances
  as realised by adding `realised` to their tag list. Verification refuses a
  unit that marks a clause it does not advance or changes the horizon in any
  other way, and the reviewer checks that each marked clause is fully
  satisfied.
