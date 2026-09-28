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
- **H.vcs.7** (distant) After every landing shed rebases every in-flight unit
  onto the new main. Conflicts are stored in the change and block nothing.
- **H.vcs.8** (distant) Shed lists the in-flight units that carry stored
  conflicts, and their files, from jj alone.
- **H.vcs.9** (distant) The bundle of a unit with stored conflicts names the
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
- **H.fp.3** (distant) Shed records the footprint predicted at sealing and the
  actual footprint at landing, and reports the difference.
- **H.fp.4** (distant) Two in-flight units whose spec footprints intersect are
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
- **H.queue.5** (distant) The same three outcomes apply when the horizon
  changes, using horizon footprints.

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

## Horizon governance

- **H.hz.1** (distant) Horizon amendments go through the shed. Near-tier
  amendments accept on consensus. Soon-tier amendments accept on zero dissent
  and wait for the owner on a split. Distant and eventual amendments always
  wait for the owner.
- **H.hz.2** (distant) The operator can require owner approval at every tier.
- **H.hz.3** (distant) A near or soon clause names the distant or eventual
  clause it refines. An amendment that would change that parent is judged at
  the parent's tier.
- **H.hz.4** (distant) Shed samples a configured fraction of auto-accepted
  amendments to the owner and records whether the owner agreed.
- **H.hz.5** (distant) The owner may veto any horizon amendment after the fact.
  A veto reverts it and reconciles units that cited it.
- **H.hz.6** (distant) An optional settling period stops a horizon change from
  justifying a proposal until it is a configured number of days old.
- **H.hz.7** (distant) The frame builder breaks horizon clauses into
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

## The owner

- **H.owner.1** (distant) One inbox holds contested units, charter questions,
  sampled amendments and horizon diffs since the owner last looked. The owner
  answers from the CLI. Nothing in the inbox blocks the factory.
- **H.owner.2** (distant) A painter or the owner drafts a charter amendment.
  The committee debates its wording only, and the owner gets a finished
  proposal with its debate record. A ratified amendment is committed alone and
  tagged `charter/v<n>`. A declined one goes to the archive.
- **H.owner.3** (distant) When painters keep hitting the same charter clause,
  shed raises a charter question. Until the owner answers, painters propose
  nothing in that direction.
- **H.owner.4** (distant) Declined horizon amendments go to the archive with
  their debate record.

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

## Sweeper

- **H.sweep.1** (distant) The sweeper patrols main on a timer, checks
  behaviour against the spec and proofs, and files each mismatch as a bug.
- **H.sweep.2** (distant) When the code is wrong, the bug enters implementing
  directly against the existing spec, with the violated clauses as its
  footprint. When the spec is wrong, the bug becomes an ordinary proposed unit.

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
