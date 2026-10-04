# Horizon

Where shed is going. The backlog is the set of clauses here that the spec does
not yet realise. The frame builder proposes changes to this document and the
owner may veto them after the fact. Nothing here may exceed what the
[charter](charter.md) permits.

Each clause carries one tier, by distance from the spec rather than by date.
Near clauses are being sharpened into spec clauses by units in flight. Soon
clauses are precise enough to propose against. Distant clauses state intent and
need sharpening before a painter can footprint them. Eventual clauses are
vision that may never fully resolve.

Nothing is in flight, so the near tier is empty. M1 to M6 and M15 are made of
soon clauses. The other milestones are provisional: most of their clauses are
distant and get promoted as the soon tier drains.

## Documents

- **H.doc.1** (soon, realised) The repository holds the charter in `charter.md`, the spec
  in `spec/`, the horizon in `horizon.md` and the archive in `archive/`.
- **H.doc.2** (soon, realised) Every clause in the charter, spec and horizon is a Markdown
  list item that starts with its ID in bold. A clause's text is the rest of
  its list item.
- **H.doc.3** (soon, realised) Charter IDs are `C<n>`. Spec IDs are `S.<area>.<n>`.
  Horizon IDs are `H.<area>.<n>`. Milestone IDs are `M<n>`.
- **H.doc.4** (soon, realised) Clause IDs are permanent. Removing a clause retires its ID,
  and no ID is reused or renumbered.
- **H.doc.5** (soon, realised) Shed parses each document into clauses and refuses a
  document with a duplicate, malformed or reused ID, naming the line.
- **H.doc.6** (soon, realised) Given two revisions of the spec, shed lists the added,
  removed and changed clause IDs without calling a model.
- **H.doc.7** (soon, realised) Every horizon clause has exactly one tier: near, soon,
  distant or eventual. A horizon clause the spec fully satisfies is marked
  realised and stays in the document as history.
- **H.doc.8** (soon, realised) Every spec clause names the horizon clauses it advances.
  Shed reports, for each horizon clause, the spec clauses that advance it.
- **H.doc.9** (soon, realised) The gap is the set of horizon clauses that are not
  realised. Shed computes it from the documents alone.
- **H.doc.10** (soon, realised) Every spec clause has at least one proof. A proof names
  the clauses it proves in its own source. Shed reports clauses without a proof
  and proofs that name unknown clauses.
- **H.doc.11** (soon, realised) Shed runs the proofs for any set of clause IDs and
  reports pass or fail per clause.
- **H.doc.12** (soon, realised) A citation names a clause ID and resolves against a named
  revision of its document. Shed refuses any citation that does not resolve.
- **H.doc.13** (soon, realised) `shed check` validates the documents, the proof mapping
  and every citation, and exits non-zero on any error.

## Tracker

- **H.track.1** (soon, realised) Shed keeps workflow state in a SQLite tracker in a state
  directory outside version control. It holds unit states, seals, footprints,
  bounce counters, amendment counts and pending notices.
- **H.track.2** (soon, realised) Shed appends every transition, session and cost to a
  JSONL event log beside the tracker. Each entry names the unit, the actor, the
  reason and the states before and after.
- **H.track.3** (soon, realised) Shed can rebuild the tracker from the event log.
- **H.track.4** (soon, realised) Each session has its own directory holding its bundle,
  transcript, outcome and result.
- **H.track.5** (soon, realised) After a crash, shed rereads the tracker and resumes
  without the owner. It treats a session that was running as interrupted and
  resumes the unit from its last finished step.
- **H.track.6** (soon, realised) Operator settings live in one operator configuration
  file, outside the charter and spec. They include budgets, concurrency, agent
  profiles, round caps, the bounce threshold and formulas.
- **H.track.7** (soon, realised) `shed status` shows every unit with its state, bounce
  count, amendment count and cost so far.

## Change units

- **H.unit.1** (soon, realised) A change unit is one spec diff with its proofs and
  implementation. It lives on its own jj change, and shed identifies it by that
  change ID.
- **H.unit.2** (soon, realised) A unit is in one state at a time: proposed, sealed,
  implementing, verifying, queued, landed, contested or archived. Landed and
  archived are terminal.
- **H.unit.3** (soon, realised) Shed allows only these transitions and records each one.
  Proposed to sealed, archived or contested. Sealed to implementing.
  Implementing to verifying. Verifying to queued or back to implementing.
  Queued to landed. Sealed, implementing, verifying or queued back to proposed,
  which is a reopen. Contested to proposed or archived.
- **H.unit.4** (soon, realised) Any role may reopen a unit with a written reason.
  Reopening increments the unit's bounce counter.
- **H.unit.5** (soon, realised) A unit whose bounce counter passes the configured
  threshold becomes contested. Shed raises it to the owner and keeps working on
  other units.
- **H.unit.6** (soon, realised) Shed refuses a unit whose spec depends on another unit
  that has not landed. The dependent waits for the landing, or the two merge
  into one unit.
- **H.unit.7** (distant) The frame builder may archive a contested unit the
  owner has not answered within the configured timeout. Only the frame builder
  discards units.
- **H.unit.8** (near, refines H.unit.7, realised) `shed inbox` and `shed status` show how
  long each contested unit has waited since its latest move to contested, and
  mark it overdue once that wait passes `shed.contested_timeout`. A timeout of
  zero turns expiry off, and no unit is ever overdue.
- **H.unit.9** (near, refines H.unit.7, realised) A shed command runs one frame builder
  session on an overdue contested unit and refuses any other unit. Its bundle
  holds the charter, the horizon, the unit's spec diff, debate record, bounce
  reasons and owner answers. The session reports `archive` with a shelf, or
  `keep`. An archive on the rejected shelf cites the charter clauses violated,
  and one on the deferred shelf says what would change the decision. Shed
  archives the unit as S.shed.10 does, with the frame builder as actor and an
  entry that records the timeout and how long the unit waited. A `keep`, or a
  session that ends without an outcome, leaves the unit contested.
- **H.unit.10** (soon, refines H.unit.7, realised) `shed serve` starts the session of
  H.unit.9 for overdue contested units, one at a time, oldest first, under the
  daily budget. It records the unit and its latest move to contested before
  dispatch and attempts each such pair at most once, even across restarts and
  tracker rebuilds. A unit that is kept is tried again only after it leaves
  contested and becomes contested again.
- **H.unit.11** (soon, refines H.unit.7, realised) A contested unit leaves contested only
  through an owner answer or a frame builder archive under H.unit.9. Shed
  refuses a move out of contested by any other actor or role outcome, naming
  the unit and the actor.
- **H.unit.12** (soon, refines H.unit.7, realised) `shed inbox` lists each unit the frame
  builder archived on timeout since the previous recorded `shed inbox`, with
  its short change ID, title, shelf, the frame builder's reason and how long
  it had waited. `-peek` lists the same units.

## Version control

- **H.vcs.1** (soon, realised) Shed works in a jj repository colocated with git and
  refuses to run with a jj version outside the pinned range.
- **H.vcs.2** (soon, realised) Shed performs every VCS operation itself. A session gets a
  directory with no `.git` or `.jj`, and no VCS tools or credentials.
- **H.vcs.3** (soon, realised) Each unit is a jj change descending from main. Shed
  snapshots session output onto that change.
- **H.vcs.4** (soon, realised) Shed checkpoints the jj operation log before each
  multi-step VCS operation and restores it if the operation fails.
- **H.vcs.5** (soon, realised) Landing squashes a unit into one commit on main and pushes
  main fast-forward only, under the landing identity. The commit message comes
  from the unit's seal and spec diff and includes its change ID.
- **H.vcs.6** (soon, realised) Nothing but landing moves main.
- **H.vcs.7** (soon, realised) After every landing shed rebases every in-flight unit
  onto the new main. Conflicts are stored in the change and block nothing.
- **H.vcs.8** (soon, realised) Shed lists the in-flight units that carry stored
  conflicts, and their files, from jj alone.
- **H.vcs.9** (soon, realised) The bundle of a unit with stored conflicts names the
  conflicted files and says to resolve them against the sealed spec first.

## Sessions

- **H.sess.1** (soon, realised) Every role runs as an ephemeral headless agent session
  through busybees/core. No session outlives its step.
- **H.sess.2** (soon, realised) Each role has its own prompt and grants. A session gets
  only the files and tools its role needs.
- **H.sess.3** (soon, realised) A session reports through its role's outcome tool, and
  shed accepts only the outcomes valid for that role and step.
- **H.sess.4** (soon, realised) Before every session shed assembles the unit's bundle:
  sealed spec, proofs, footprint, debate record, unit notes and pending
  notices.
- **H.sess.5** (soon, realised) Shed delivers notices between sessions, never during one.
- **H.sess.6** (soon, realised) Shed gets context through a context provider interface.
  The default provider supplies the three documents and the unit's own record.
- **H.sess.7** (soon, realised) Each session runs under a per-session cost cap. A session
  that reaches it ends, and shed records an infrastructure failure.
- **H.sess.8** (soon, realised) Shed classifies each failure as infrastructure or
  behavioural. It retries infrastructure failures or falls back to another
  profile. Behavioural failures are outcomes that feed the state machine.
- **H.sess.9** (soon, realised) A session that implements or verifies can run the
  project's proofs and tests the way the project runs them, through the runner
  `shed.toml` configures, even when that runner needs a container engine such
  as Dagger. The session still holds no version control.

## The shed

- **H.shed.1** (soon, realised) A committee of several members debates every proposed
  unit. The members run in parallel against the same revision of the proposal.
- **H.shed.2** (soon, realised) Every objection cites clause IDs. Shed refuses an
  objection whose citations do not resolve.
- **H.shed.3** (soon, realised) An objection citing a charter clause the proposal
  violates is a veto. Shed archives the unit on the rejected shelf with the
  citation, whatever other members say.
- **H.shed.4** (soon, realised) A proposal that is clean against the charter but does not
  move the spec toward the horizon goes to the deferred shelf, with a note on
  what would change the decision.
- **H.shed.5** (soon, realised) The proposer answers once per round. An objection stands
  until the member who raised it withdraws it.
- **H.shed.6** (soon, realised) Consensus is zero standing objections. Rounds are capped,
  and reaching the cap approves nothing. A proposal that still carries
  objections at the cap goes back to its painter and counts a bounce.
- **H.shed.7** (soon, realised) Any member may object that a proposal's footprint is too
  large. The split it asks for is by footprint, not by cost.
- **H.shed.8** (soon, realised) On consensus shed seals the unit. It records the seal
  `(main commit, change ID)`, computes the unit's footprints and moves it to
  sealed.
- **H.shed.9** (soon, realised) Shed records every debate in full. The record joins the
  unit's bundle, and its archive entry if it has one.
- **H.shed.10** (soon, realised) The archive has two shelves. A rejected entry records
  the charter clauses it violated. A deferred entry records what would change
  the decision. Each entry is a file painters can read.
- **H.shed.11** (soon, realised) A mechanic's amendment request reopens the unit into a
  fast lane with the same rules, a shorter round cap and scope limited to the
  sealed spec. If the amendment is accepted, shed revises the spec, records a
  new seal and returns the unit to implementing with the mechanic told the
  diff. If it is rejected, the mechanic implements as written or the unit is
  discarded.
- **H.shed.12** (soon, realised) The painter reads the gap and the deferred shelf and
  drafts proposals, throttled by a proposal rate.
- **H.shed.13** (soon, realised) The painter proposes against near and soon clauses
  only. It does not propose against a horizon clause an in-flight unit already
  advances, or re-propose an idea on the rejected shelf.

## Footprints

- **H.fp.1** (soon, realised) A unit's spec footprint is the clauses its spec diff
  modifies plus the clauses it depends on. Its horizon footprint is the horizon
  clauses it advances.
- **H.fp.2** (soon, realised) Shed computes modified clauses from the spec diff. The
  painter declares dependencies and the horizon footprint, and the committee
  checks them.
- **H.fp.3** (soon, realised) Shed records the footprint predicted at sealing and the
  actual footprint at landing, and reports the difference.
- **H.fp.4** (soon, realised) Two in-flight units whose spec footprints intersect are
  entangled. Shed reports entanglement as an advisory as soon as the second
  unit seals.

## Implementation

- **H.impl.1** (soon, realised) A formula is the DAG of steps inside implementing for one
  unit type, with `needs` edges. Formulas are operator configuration and change
  without rebuilding shed.
- **H.impl.2** (soon, realised) The default formula writes proofs before implementation.
- **H.impl.3** (soon, realised) A mechanic session works one formula step and ends. Shed
  records the result and dispatches the next step whose needs are met.
- **H.impl.4** (soon, realised) A mechanic may request an amendment instead of finishing
  a step (H.shed.11). Shed counts amendments per unit.
- **H.impl.5** (distant) A unit whose cost passes a configured multiple of its
  estimate reopens for debate. Cost alone never stops a unit.
- **H.impl.6** (distant) Steps whose needs are met run at the same time, each
  in its own jj workspace descending from the unit change. A wheelbuilder
  scoped to the unit squashes them back into it.
- **H.impl.7** (near, refines H.impl.5, realised) Every proposal declares an estimate in
  USD of what taking the unit from sealed to landed will cost. Shed refuses a
  proposal without one, the committee may object to it like any other part of
  the proposal, and sealing records it with the seal. A seal out of the
  amendment lane keeps the estimate it had. The estimate never orders or sizes
  work.
- **H.impl.8** (near, refines H.impl.5, realised) `shed status` shows each unit's
  recorded estimate beside its cost since the seal that recorded that
  estimate.
- **H.impl.9** (soon, refines H.impl.5, realised) When a unit's cost since the seal that
  recorded its estimate passes `budget.overrun_multiple` times that estimate,
  shed starts no further session on the unit, lets running sessions finish,
  and reopens it with a reason naming the cost, the estimate and the multiple.
  The reopen counts a bounce. A multiple of zero turns this off. Shed never
  ends a session, pauses a unit or archives a unit on its cost alone.
- **H.impl.10** (soon, refines H.impl.5, realised) The debate bundle of a unit reopened
  for an overrun shows its cost per step and per session against its estimate.
  The committee may keep the scope with a revised estimate or split the unit
  by footprint (H.shed.7), and the next seal records the new estimate.

## Verification

- **H.verify.1** (soon, realised) Verification covers the unit's spec footprint. Every
  affected clause has a proof, and every one of those proofs passes.
- **H.verify.2** (soon, realised) A committee member who did not work on the unit reads
  the sealed spec against the unit's behaviour. Every finding cites clause IDs.
- **H.verify.3** (soon, realised) The committee checks the charter against the unit's
  code as well as its spec.
- **H.verify.4** (soon, realised) A unit that fails returns to implementing with the
  failing clause IDs. If the spec itself is in question it reopens instead.

## Merge queue and reconcile

- **H.queue.1** (soon, realised) Verified units wait in a merge queue. One lander lands
  them one at a time.
- **H.queue.2** (soon, realised) Before landing, the wheelbuilder rebases the unit onto
  main. A unit whose conflicts cannot be resolved against its sealed spec
  reopens.
- **H.queue.3** (distant) The wheelbuilder orders the queue to reduce
  entanglement churn. Disjoint units land in any order, batched. Among
  entangled units the smaller footprint lands first.
- **H.queue.4** (distant) After every landing shed reconciles every unit from
  sealed through queued. A disjoint unit is re-sealed silently. A unit whose
  dependencies only gained clauses is re-sealed, and its mechanic is told the
  diff and which proofs are suspect. A unit whose dependency changed text goes
  to the wheelbuilder, which reopens it with a written conflict if the meaning
  changed and otherwise re-seals it with a notice.
- **H.queue.5** (soon, realised) The same three outcomes apply when the horizon
  changes, using horizon footprints.
- **H.queue.6** (near, refines H.queue.3, realised) Shed computes a landing order for
  the queued units from the tracker alone, and `shed serve` lands the first
  unit in that order that nothing holds back, such as a horizon review. Until
  another rule moves a unit, the order is the order the units opened, as
  `shed status` lists them. `shed status` shows each queued unit's place in
  the landing order.
- **H.queue.7** (near, refines H.queue.3) Two queued units are entangled
  when the spec footprints recorded at their last seals share a clause, as
  S.fp.5 counts it. A queued unit lands only after every queued unit
  entangled with it whose spec footprint has fewer clauses, or as many and
  an earlier place in the opening order. A unit entangled with no other
  queued unit keeps its place. `shed status` names, for each queued unit
  held back this way, the units it waits behind.
- **H.queue.8** (soon, refines H.queue.3) When two or more queued units are
  pairwise disjoint, none is held back, and none would change, remove or
  gain a refining clause for a horizon clause recorded at another's seal,
  `shed serve` lands them back to back as one batch, in the landing order,
  starting no other stage in between. Each lands through S.queue.2 as its
  own commit. The notices, reopens, review marks and rebase sweep that
  follow a landing run once, after the batch, comparing the last landed
  commit with main before the batch. A member that reopens or fails to land
  ends the batch there, and what already landed is reconciled.

## Scheduling

- **H.sched.1** (soon, realised) The owner can take one unit from proposed to landed with
  shed commands and no VCS work of their own.
- **H.sched.2** (soon, realised) `shed serve` runs one controller for each role that has
  work, all sharing the tracker.
- **H.sched.3** (soon, realised) Controllers are level-triggered. They reconcile
  against current tracker state, and events only wake them early. A missed
  event costs latency, never correctness.
- **H.sched.4** (soon, realised) The scheduler never waits on a model. Sessions report
  by writing transitions, which wake the relevant controller.
- **H.sched.5** (soon, realised) A cap limits units across implementing and verifying.
  Dispatch goes downstream first: verify, then implement, then debate, then
  propose.
- **H.sched.6** (soon, realised) A proposal rate throttles the painter. The sweeper is
  the only controller on a timer.
- **H.sched.7** (soon, realised) A global daily budget pauses dispatch when spent.
  Status shows streaks of degraded operation.
- **H.sched.8** (distant) When the gap is empty the painter proposes nothing
  until the horizon moves. The sweeper keeps patrolling.
- **H.sched.9** (soon, realised) The operator can cap the units in flight from sealed
  through queued. Shed seals no unit while the cap is reached.
- **H.sched.10** (near, refines H.sched.8) `shed status` says, after the units
  and notices, whether the painter may propose now and, when it may not, what
  holds it back, in the same words `shed serve -once` uses for the painter
  under S.serve.1. An empty gap is reported as the painter being idle until
  the horizon moves.
- **H.sched.11** (soon, refines H.sched.8) After a painter session reports
  `nothing`, shed starts no further painter session until the gap the painter
  may work on differs from the gap in that session's bundle: a clause has
  entered or left it, or a clause in it has changed text. Shed records that
  gap in the event log, so the wait holds across restarts and tracker
  rebuilds. `shed serve -once` and `shed status` say the painter is waiting
  for the gap to change.
- **H.sched.12** (soon, refines H.sched.8) When a landing changes the horizon
  on main so that the gap the painter may work on gains a clause it did not
  hold before the landing, the painter's streak of proposals that went
  nowhere ends, and the painter may propose at once without waiting out
  `painter.interval`.

## Horizon governance

- **H.hz.1** (soon, realised) Horizon amendments go through the shed. Near-tier
  amendments accept on consensus. Soon-tier amendments accept on zero dissent
  and wait for the owner on a split. Distant and eventual amendments always
  wait for the owner.
- **H.hz.2** (soon, realised) The operator can require owner approval at every tier.
- **H.hz.3** (soon, realised) A near or soon clause names the distant or eventual
  clause it refines. An amendment that would change that parent is judged at
  the parent's tier.
- **H.hz.4** (distant) Shed samples a configured fraction of auto-accepted
  amendments to the owner and records whether the owner agreed.
- **H.hz.5** (distant) The owner may veto any horizon amendment after the fact.
  A veto reverts it and reconciles units that cited it.
- **H.hz.6** (distant) An optional settling period stops a horizon change from
  justifying a proposal until it is a configured number of days old.
- **H.hz.7** (soon, realised) The frame builder breaks horizon clauses into
  footprint-sized increments and moves clauses between tiers by amendment.
- **H.hz.8** (distant) A painter may propose moving a deferred archive entry
  onto the eventual tier. Painters reread the deferred shelf whenever the
  horizon moves.
- **H.hz.9** (distant) Shed reports progress per horizon clause and per
  milestone. A milestone is done when every clause in it is realised on main.
- **H.hz.10** (soon, realised) A unit may mark the horizon clauses it fully realises as
  realised, in the same diff as its spec change. Verification checks each
  claim against the unit's behaviour, and refuses one the spec does not
  fully satisfy.
- **H.hz.11** (near, refines H.hz.4, realised) Sampling under `owner.sample_every`
  covers only auto-accepted horizon amendments. A landing whose seal came
  from the owner's `approve`, or from `shed frame -accept`, is never sampled
  and takes no place in the sampling count, so one in every N amendments
  accepted without the owner is sampled.
- **H.hz.12** (near, refines H.hz.4, realised) `shed answer <unit> agree <reason>` and
  `shed answer <unit> disagree <reason>` record in the event log, with the
  owner as actor, whether the owner agreed with a sampled amendment, so
  `shed tracker rebuild` keeps the answer. Shed refuses either for a unit
  that was not sampled or already has one, or with an empty reason. An
  answer moves no unit and changes nothing on main.
- **H.hz.13** (soon, refines H.hz.4) `shed inbox` lists each sampled
  amendment until the owner agrees or disagrees with it, not only those
  sampled since the previous recorded `shed inbox`, and marks as new those
  sampled since then. An answered amendment leaves the inbox.
- **H.hz.14** (soon, refines H.hz.4) `shed status` reports how many horizon
  amendments were auto-accepted and how many were sampled, and of the
  sampled ones how many the owner agreed with, disagreed with and has not
  answered.

## The owner

- **H.owner.1** (soon, realised) One inbox holds contested units, charter questions,
  sampled amendments and horizon diffs since the owner last looked. The owner
  answers from the CLI. Nothing in the inbox blocks the factory.
- **H.owner.2** (distant) A painter or the owner drafts a charter amendment.
  The committee debates its wording only, and the owner gets a finished
  proposal with its debate record. A ratified amendment is committed alone and
  tagged `charter/v<n>`. A declined one goes to the archive.
- **H.owner.3** (distant) When painters keep hitting the same charter clause,
  shed raises a charter question. Until the owner answers, painters propose
  nothing in that direction.
- **H.owner.4** (soon, realised) Declined horizon amendments go to the archive with
  their debate record.
- **H.owner.5** (near, refines H.owner.2, realised) Shed refuses to seal, pass
  verification for or land any unit whose change alters `charter.md` against
  the main commit it descends from, naming the unit and the file. Only a
  charter amendment unit changes the charter.
- **H.owner.6** (near, refines H.owner.2) `shed charter draft <title>` opens a
  charter amendment unit on main with the owner as actor, whose workspace the
  owner edits. Shed refuses to debate it while its change alters any file
  other than `charter.md`, alters nothing, or holds a charter that fails the
  document checks, such as a reused or retired clause ID.
- **H.owner.7** (soon, refines H.owner.2) The committee debates a charter
  amendment unit for its wording only: ambiguity, contradiction with other
  charter clauses, and operator settings that do not belong in the charter.
  Every objection cites clause IDs, no objection vetoes it, and the author
  answers once per round. The debate ends at consensus or at its round cap,
  and either way the unit waits for the owner with the objections still
  standing. It is never sealed, implemented or archived by a committee.
- **H.owner.8** (near, refines H.owner.2) `shed inbox`, including `-peek`,
  lists every charter amendment waiting for the owner, oldest first, with its
  short change ID, title, the charter clauses it adds, changes or removes,
  each with its text before and after, any objections still standing, and the
  path of its full debate record.
- **H.owner.9** (soon, refines H.owner.2) `shed answer <unit> ratify <reason>`
  lands a waiting charter amendment as one commit that changes only
  `charter.md`, fast-forward and under the landing identity, and tags that
  commit `charter/v<n>`, where n is one more than the highest existing
  charter tag, or 2 when none exists. It refuses when `charter.md` on main
  has changed since the debate ended, and sends the unit back to debate. After
  the landing shed rebases and reconciles the other units as after any
  landing.
- **H.owner.10** (near, refines H.owner.2) `shed answer <unit> decline <reason>`
  archives a waiting charter amendment with the owner as actor. Its archive
  entry lists the charter clauses it would have added, changed or removed,
  with their text before and after, its debate record and the owner's reason,
  and painters can read it.
- **H.owner.11** (soon, refines H.owner.2) A painter may draft a charter
  amendment unit for a charter question listed in the inbox (S.owner.9),
  under the proposal rate. Its bundle holds the charter, the question's
  rejected entries and the declined charter amendments, and shed refuses a
  draft that changes the same clauses to the same text as a declined one.

## Init

- **H.init.1** (distant) `shed init` runs on an empty or nearly empty git
  repository and a design document the owner names.
- **H.init.2** (distant) It opens a charter template with only four headings:
  purpose, non-goals, boundaries and amendment rule. It refuses to continue
  while a section is empty or the charter is over the configured line budget.
- **H.init.3** (distant) It commits the charter alone, first, and tags it
  `charter/v1`.
- **H.init.4** (distant) The frame builder drafts a horizon from the design
  document and the charter, as clauses with no sequencing. It flags anything
  that contradicts the charter instead of dropping it. The owner edits and
  commits the horizon.
- **H.init.5** (distant) It writes a spec with only a preamble saying main has
  no behaviour.
- **H.init.6** (distant) It lays down the tracker, role prompts, merge queue
  configuration and conventions in one commit after the three documents.
- **H.init.7** (distant) It opens the first change unit, with a horizon
  footprint the frame builder chooses.
- **H.init.8** (distant) Running `shed init` on an initialised repository is an
  error.
- **H.init.9** (near, refines H.init.1, realised) `shed init <design-document>` runs at
  the root of a git repository and checks that the repository and the
  document qualify. It refuses a missing argument, a path that does not exist
  or is not a regular file, and a document that is empty or not UTF-8 text,
  naming the path. It prints every failed check, exits non-zero when any
  fails, and changes nothing in the repository, its git state or the state
  directory when it refuses.
- **H.init.10** (near, refines H.init.1, realised) `shed init` treats a repository as
  nearly empty when every file in its working tree that git does not ignore,
  tracked or not, is a `README`, `LICENSE`, `.gitignore` or `.gitattributes`
  file at the root, or the design document itself. A repository with no
  commits qualifies. Otherwise it refuses, listing each other path in path
  name order and saying that an existing codebase is adopted rather than
  initialised.
- **H.init.11** (soon, refines H.init.1) On a qualifying git repository with no
  jj repository, `shed init` makes a jj repository colocated with git at the
  root, using a jj inside the pinned range (S.vcs.1), and leaves git's
  commits, branches, index and working files as they were. A repository
  already colocated at the root is used as it is. It refuses a jj repository
  that is not colocated with git, or one whose root is not the git root.
- **H.init.12** (soon, refines H.init.1) Before its first step that writes,
  `shed init` records the design document's path and SHA-256 digest in the
  state directory. Later init steps read the document only through that
  record and refuse to continue, naming the path and both digests, if the
  document has changed since.

## Sweeper

- **H.sweep.1** (distant) The sweeper patrols main on a timer, checks
  behaviour against the spec and proofs, and files each mismatch as a bug.
- **H.sweep.2** (distant) When the code is wrong, the bug enters implementing
  directly against the existing spec, with the violated clauses as its
  footprint. When the spec is wrong, the bug becomes an ordinary proposed unit.
- **H.sweep.3** (near, refines H.sweep.1, realised) `shed sweep` checks main's current
  commit out into a fresh directory outside every unit's change, runs the
  proofs of every spec clause there as `shed prove` does, and records the
  sweep in the tracker and the event log: the main commit, when it ran, and
  pass or fail per clause. It calls no model and changes no unit. It prints
  the failing clauses and exits non-zero when any fails.
- **H.sweep.4** (near, refines H.sweep.1) A clause that fails a sweep is filed
  as a bug in the tracker, naming the clause, the main commit and the output
  of its failing proofs. While its bug is open a clause that keeps failing is
  not filed again, and the first sweep in which it passes closes the bug and
  records that commit. `shed status` lists open bugs with their clauses, the
  commit that first failed and how long each has been open. Bugs come back
  after `shed tracker rebuild`.
- **H.sweep.5** (soon, refines H.sweep.1) `shed serve` runs the sweep of
  H.sweep.3 on a timer, every `sweeper.interval` after the previous sweep
  started, and never two sweeps at once. An interval of zero turns patrolling
  off. A sweep that a crash interrupts records nothing and runs again on the
  next pass.
- **H.sweep.6** (soon, refines H.sweep.1) A sweeper session checks main's
  behaviour against spec clauses beyond what their proofs test. It gets a
  read-only copy of main at the swept commit, the charter, the spec and the
  proofs of the clauses it patrols, and can run proofs and tests, but holds no
  version control. Each session patrols the `sweeper.clauses_per_session`
  clauses that sessions have patrolled least recently, and reports `clean` or
  a list of mismatches, each citing the clauses it violates and its evidence.
  Shed refuses a mismatch whose citations do not resolve. `shed serve` starts
  these sessions one at a time, after each sweep, under the daily budget.
- **H.sweep.7** (soon, refines H.sweep.1) A mismatch a sweeper session reports
  is filed as a bug under H.sweep.4 only when `sweeper.confirmations` further
  sweeper sessions, each given the mismatch and main at the same commit,
  confirm it. One that disputes it records the mismatch as disputed with its
  reason, and nothing is filed. A mismatch on a clause with an open bug joins
  that bug. Such a bug closes when a later patrol of its clauses, confirmed
  the same way, finds them clean.

## Context

- **H.ctx.1** (distant) Shed emits every event to Hearsay as an L0 record: unit
  opened, footprint declared, each debate contribution, seal, reopen, archive,
  landing, and every change to the three documents.
- **H.ctx.2** (distant) With Hearsay as the context provider, a role's bundle is
  its anchors, recent items and the stance history of the unit's topic. The
  unit's footprint filters it and the role's class limits it.
- **H.ctx.3** (distant) Provenance flows one way. Nothing in Hearsay changes
  shed's git history or tracker, and shed never writes Hearsay's distilled
  layers.
- **H.ctx.4** (near, refines H.ctx.1) A shed command prints the event log as
  L0 records, one JSON line each, oldest first, with no network access. It
  covers unit opened, footprint declared, each objection, answer and
  withdrawal, seal, reopen, archive and landing. Each record carries an ID
  derived from its event's sequence number, the time, the kind, the unit as
  its topic, the actor, the clause IDs it cites and its text. The same log
  always gives the same records, and other event kinds give none.
- **H.ctx.5** (near, refines H.ctx.1) The same command also prints one L0
  record for each clause added, removed or changed in `charter.md`, `spec/`
  or `horizon.md` by each commit on main, oldest commit first, computed as
  under H.doc.6. Each record names the document, the clause ID, its text
  before and after, the commit and, for a landing, the unit's change ID. Its
  ID derives from the commit and clause ID, so the same history always gives
  the same records.
- **H.ctx.6** (soon, refines H.ctx.1) When the operator configures a Hearsay
  ingest endpoint, `shed serve` sends it the records of H.ctx.4 and H.ctx.5
  in order and logs the last record Hearsay accepted, so a tracker rebuild
  keeps it. An unreachable or refusing endpoint never blocks, fails or
  delays a transition or session. Shed retries later from the last accepted
  record, and may resend a record but never skips one. With no endpoint
  configured shed sends nothing and needs nothing beyond its git remote.
- **H.ctx.7** (soon, refines H.ctx.1) `shed status` shows, when a Hearsay
  endpoint is configured, how many records are waiting to be sent, the time
  of the last accepted record and the last delivery error.

## Adoption

- **H.adopt.1** (distant) Shed can adopt an existing codebase. Before normal
  operation, sweepers and painters write spec clauses and proofs for what main
  already does.
- **H.adopt.2** (distant) They sort each behaviour three ways. A behaviour that
  is charter-compliant and on the horizon gets specced. One that is
  charter-compliant but off the horizon gets specced and flagged for a possible
  removal unit. One that violates the charter gets specced and a unit filed to
  remove it.
- **H.adopt.3** (distant) Normal operation starts only once main is
  spec-conformant.
- **H.adopt.4** (near, refines H.adopt.1) `shed uncovered` runs every proof
  on main once, through the configured runner, with statement coverage over
  the whole Go module, and lists each non-test Go source file with the
  number and share of its statements that no proof executes, most
  unexecuted first, then the module's totals. It calls no model and changes
  nothing. It exits non-zero only when it cannot run the proofs, and a
  failing proof still counts the statements it executed.
- **H.adopt.5** (soon, refines H.adopt.1) `shed adopt` puts a project into
  adoption, with the owner as actor, and refuses while main fails
  `shed check` or the project is already adopting. Shed records the start
  in the event log, so a tracker rebuild keeps it, with main's totals under
  H.adopt.4. While adopting, `shed status` says so and shows the totals at
  the start and at the latest main, and the painter drafts only describing
  proposals (H.adopt.6) instead of proposing from the gap. Adoption ends
  only as H.adopt.3 allows.
- **H.adopt.6** (soon, refines H.adopt.1) While adopting, the painter's
  bundle holds the report of H.adopt.4 for main in place of the gap, with
  the files each in-flight describing unit declares. A describing proposal
  adds spec clauses and proofs that state what main already does in the
  source files it declares, and proposes no new behaviour. `declare` takes
  those files and refuses one that has no unexecuted statement on main or
  that another in-flight describing unit declares. Its horizon clauses may
  be any unrealised clause of any tier, even one an in-flight unit already
  advances. Behaviour that advances no horizon clause is left to H.adopt.2.
- **H.adopt.7** (soon, refines H.adopt.1) A describing unit is implemented by
  the operator's `describe` formula, by default a single `proofs` step. Shed
  refuses a mechanic's step, and verification fails the unit, when its change
  touches any file outside `spec/` and Go test files, naming each such file.
  Verification also fails a describing unit when, for some file it declares,
  its proofs execute no statement that main's proofs left unexecuted, naming
  that file.

## Releases

- **H.release.1** (soon, realised) Every shed binary reports the release it
  was built from.

## Vision

- **H.vision.1** (eventual) Shed builds shed. Every change to this repository
  lands as a shed unit.
- **H.vision.2** (eventual) The owner's regular work is keeping the charter
  true and pointing the distant horizon where the project should go.
- **H.vision.3** (eventual) The tracker can move to a shared backend so one
  factory spans several machines.
- **H.vision.4** (eventual) The frame builder proposes formula changes through
  debate.
- **H.vision.5** (near, refines H.vision.1) A shed command lists the commits
  on main's first-parent history after a commit that `shed.toml` names that
  did not land as a shed unit: each commit whose message has no `Unit:`
  trailer naming a unit the tracker records as landed with that commit. Each
  line gives the commit, its author and its subject, oldest first. A commit
  that changes only `charter.md` is listed apart, as a charter change. The
  command changes nothing, prints nothing else when every commit landed as
  a unit, and fails when `shed.toml` names no such commit or one that is not
  on main.
- **H.vision.6** (soon, refines H.vision.1) `shed inbox` lists the commits
  on main after the main commit the previous recorded `shed inbox` stored
  that did not land as a shed unit, as H.vision.5 finds them, keeping
  charter changes apart. `-peek` lists the same commits. Listing them moves
  no unit and blocks nothing.
- **H.vision.7** (near, refines H.vision.1) The owner can land an edit of
  `horizon.md` as a unit instead of committing it to main. A shed command
  takes a file holding the edited horizon, opens a unit on main whose change
  holds it as `horizon.md` and nothing else, with the owner as actor, and
  lands it as `shed frame -accept` lands a framing, with a `Unit:` trailer,
  recorded as a horizon amendment that is never sampled. It refuses an edit
  that changes no clause, fails `shed check`, or adds a clause under an ID
  that main's history has retired, and then opens and lands nothing.
- **H.vision.8** (soon, refines H.vision.1) When the operator configures a
  build command, `shed serve` runs it after each landing whose commit changes
  a file outside `charter.md`, `spec/` and `horizon.md`. Once no stage is
  running it replaces itself with the binary the command built and resumes
  from the tracker as after a crash. A build that fails, or a built binary
  that fails `shed doctor`, leaves the running binary serving, and
  `shed status` shows the failure and the landed commit it was built from.

## Milestones

A milestone is a named bundle of horizon clauses. It is done when every clause
in it is realised on main. They come in the order listed. After M15, the
remaining milestones are not ordered among themselves.

- **M1** Clause foundations. Everything else reduces to set operations on
  clause IDs, so the document format comes first. H.doc.2 to H.doc.13.
- **M2** Tracker and units. The deterministic state machine and its storage.
  H.track.1 to H.track.4, H.track.6, H.track.7, H.unit.1 to H.unit.6.
- **M3** Hands off version control. Shed owns every jj operation and sessions
  never see one. H.vcs.1, H.vcs.3 to H.vcs.6.
- **M4** Sessions and bundles. Roles run through busybees/core with scoped
  grants, outcome tools and assembled bundles, and can run the project's
  tests without holding version control. H.sess.1 to H.sess.9, H.vcs.2.
- **M5** The shed. Debate, veto, deferral, consensus, sealing and the archive.
  H.doc.1, H.shed.1 to H.shed.10, H.fp.1, H.fp.2.
- **M6** Self-hosting. The shed can run itself: the owner drives a unit from
  proposal to landing with shed commands alone. After M6, changes to this
  repository go through shed. A mechanic who finds the spec wrong reopens the
  unit for a full debate. H.impl.1 to H.impl.3, H.verify.1 to H.verify.4,
  H.queue.1, H.queue.2, H.sched.1.
- **M15** Autopilot. Shed works through the soon tier on its own: the painter
  proposes from the gap, controllers carry each unit through the shed to main,
  and a budget stops the spending. The in-flight cap stays at one until M7, so
  nothing needs reconciling. The owner promotes clauses to soon by editing
  the horizon until M9. H.track.5, H.shed.12, H.shed.13, H.sched.2 to
  H.sched.7, H.sched.9, H.hz.10.
- **M7** Reconcile and entanglement. Many units in flight at once without merge
  hell. H.vcs.7 to H.vcs.9, H.fp.3, H.fp.4, H.queue.3 to H.queue.5.
- **M8** Autonomous scheduling at full width. The amendment fast lane, cost
  overruns, idling and contested timeouts. H.shed.11, H.impl.4, H.impl.5,
  H.sched.8, H.unit.7.
- **M9** Horizon governance and the owner. H.hz.1 to H.hz.9, H.owner.1 to
  H.owner.4.
- **M10** Shed init. H.init.1 to H.init.8.
- **M11** Parallel steps inside a unit. H.impl.6.
- **M12** Sweeper. H.sweep.1, H.sweep.2.
- **M13** Hearsay context. H.ctx.1 to H.ctx.3.
- **M14** Adoption of existing codebases. H.adopt.1 to H.adopt.3.
