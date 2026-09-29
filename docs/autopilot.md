# The life of a unit, and autopilot

A unit goes from proposal to main in four stages: debate, implementation,
verification and landing. The owner can drive one unit through them with a
few commands, or `shed serve` can run the whole factory on its own.

## Driving a unit by hand

```sh
shed unit open "Say goodbye"             # makes a jj change and prints its ID
cd "$(shed unit path <unit>)"            # edit spec/ there: the spec diff is the proposal
shed unit declare -depends S.core.1 -advances H.greet.2 <unit>
shed run <unit>                          # debate, implement, verify, land
```

`shed debate` and `shed land` run a single stage. `shed run` stops when the
unit lands, or when a stage bounces, reopens, archives or contests it.

## Debate

A proposal is its spec diff. The clauses it modifies are computed from the
diff; the clauses it depends on and the horizon clauses it advances are
declared, and dependencies must be on main.

In each round, `concurrency.committee` committee members review the same
revision at once; what they write in their copies is thrown away. They
object with `object`, citing clause IDs
that must resolve, in one of four kinds:

| Kind | Means | At the end |
| --- | --- | --- |
| `charter` | The proposal violates a charter clause. | A veto: rejected at the end of the round. |
| `horizon` | It does not move the spec toward the horizon. | Deferred if only horizon objections stand at the cap, unless a split soon amendment waits for the owner. |
| `size` | The footprint is too large; split it by clause. | Stands until withdrawn. |
| `spec` | A clause is ambiguous, untestable or wrong. | Stands until withdrawn. |

Between rounds the painter answers each standing objection once with
`answer`, and may revise the files. Only the member who raised an objection
can withdraw it. With no objection standing, the unit is sealed against
main's current commit and the commit its change points to. Sealing first
rebases the change onto that main commit, so a sealed unit's change is
always based on its seal's main. A conflict the rebase leaves only in files
outside `spec/` is stored in them, and the unit is sealed; its mechanics
resolve it against the sealed spec. If the rebase fails, or leaves an
unresolved conflict in any file under `spec/`, including markers a painter
left in place, nothing is sealed: the proposal bounces to its painter, with
a reason naming the failure, or each file under `spec/` holding an
unresolved conflict and each clause ID inside a conflicted region. A
conflicted change keeps its rebased files, so the painter's next session
sees the markers and resolves them before the committee debates the text
again. At `shed.max_rounds` with objections still standing, the proposal
bounces back to its painter and counts a bounce. Unless that bounce leaves
it past `shed.bounce_threshold` bounces, when it is contested and waits for
the owner's `shed answer`, it stays proposed and debates afresh next time.

A proposal's horizon amendment is tiered before it is sealed. When a round
ends with no objection standing, and no clause outside the scope of an
amendment (below), shed takes the tier `shed diff` gives between the latest
main commit the unit's change descends from and the change. It does so
before the in-flight cap can hold the seal back. A distant or eventual tier
always waits for the owner: the unit is neither sealed nor held but moves to
contested, with shed as actor and a reason naming the tier and each horizon
clause counted at it, in document order. A clause counted at the tier only
because of its `refines` tag is followed by each clause at whose tier it
counts, with that clause's ID and tier, as `shed diff` names them (S.shed.19):

```
the horizon amendment is eventual tier, so it waits for the owner: H.greet.2 parent H.greet.5 (eventual); H.greet.7
```

A clause counted at the tier by its own tier names none, even when its tag
also names a clause of that tier. This counts no bounce. A proposal
with no tier, or a near or soon tier, is sealed or held as before. A held
seal released with no painter session run on the unit since its round ended
is not tiered again, since its change holds only what that round saw,
rebased. A painter session on a held unit, such as one resolving conflicts
a landing stored in its files, ends the hold: the unit stays proposed,
counts no bounce, and its next debate starts afresh from round one, so the
tier is taken again before it is sealed. A unit bounced for changing a
clause outside an amendment's scope is not tiered (S.shed.16).

A soon tier accepts on zero dissent and waits for the owner on a split. A
debate outside the amendment lane is split when it reaches its round cap
with objections standing, every one of them a horizon objection, and at
least one committee member of its last round has no objection standing.
Shed then tiers the horizon amendment as above. When the tier is soon, the
unit is not deferred: it moves to contested, with shed as actor and a
reason naming the tier and each standing objection. This counts no bounce.
A debate that is not split, such as one where every member objects or a
spec or size objection stands, and a split one whose amendment has no tier
or a near, distant or eventual tier, is deferred, bounced or rejected as
before. When the owner answers `retry` to a unit contested this way, it
goes back to its painter as a bounce at the cap would, with the objections
that stood at the cap as the reason, but counts no bounce, and its next
debate starts afresh from round one. The amendment lane is left out, since
standing objections at its cap reject the amendment and restore the
horizon of the last seal (S.shed.18).

The owner lets a distant, eventual or split soon amendment through with
`shed answer <unit> approve <reason>`, which moves the unit back to proposed.
Its next debate runs no round and seals it, without tiering it again; while
the in-flight cap holds sealing back it waits in proposed like any proposal
that reached consensus. A unit in the amendment lane stays there until that
seal, which counts as a seal out of the lane. A bounce before the seal ends
the approval, and so does a painter session on the unit, such as one
resolving conflicts a landing stored in its files; that session counts no
bounce. Once the approval ends, the unit's later debates run as for any
proposal, starting afresh from round one, so the horizon amendment is
tiered again as the painter left it. Shed refuses `approve` for a unit whose
latest move to contested was for any other reason (S.shed.17).

A unit whose latest reopen requested an amendment is debated in the
amendment lane. Every rule above holds, but the round cap is
`shed.amendment_rounds` instead of `shed.max_rounds`, and each member's
session is told that cap. The unit stays in the lane, even after a bounce at
the cap, until it is next sealed.

An amendment is scoped to the sealed spec: the clauses the footprint
recorded at the unit's last seal modified or depended on. Every member's and
the painter's session is told those clauses. A clause outside that scope
must keep the text it has on the main commit the unit's change is based on.
That is the main commit recorded in the seal, onto which sealing rebased the
change, until a landing rebases it onto a newer main, so clauses that main
changed after the seal never count against the amendment. When a round ends
with no objection standing but the proposal changes a clause outside its
scope, the unit is not sealed. It bounces to its painter, with a reason
naming each clause outside the scope.

At the amendment lane's cap, standing objections reject the amendment rather
than bounce or defer the unit, as long as none of them is a charter
objection. A charter objection still rejects the whole proposal, which is
archived. A rejected amendment archives nothing and keeps the unit's change.
Shed makes the files under `spec/` and the file `horizon.md` exactly those
of the unit's commit recorded at its last seal, rebased onto the main commit
the change is based on. Any spec file absent there is removed and every
other file is left alone. Clauses that main added, changed or removed after
the seal stay as main has them, and a conflict between main and the sealed
spec or horizon is stored in the files. It then seals the unit again, with
the standing objections as the reason. This seal is not tiered, since the
horizon it restores was tiered or approved at the last seal, and a painter
session does not end its wait, since the restore overwrites whatever the
painter wrote to the horizon meanwhile. If a restored file holds a conflict,
or sealing's rebase fails or conflicts under `spec/`, the unit is not
sealed: it keeps the restored files, bounces to its painter as any seal
does, and stays in the amendment lane. The restore and the seal happen together. While the
in-flight cap holds sealing back, nothing is restored and the unit waits in
proposed in the amendment lane. Its debate does not start afresh, so the
next one runs no further round and rejects the amendment again. Once sealed,
the unit leaves the lane and counts no further bounce. Every mechanic
session of its next implementation is told in its bundle that the requested
amendment was rejected and the sealed spec stands as written, with the
objections that stood at the cap.

When the amendment lane seals the unit after accepting an amendment, every
mechanic session of its next implementation is told in its bundle that the
unit was resealed after an amendment, with the amendment's diff. The diff
compares the clauses of `spec/` on the unit's commit recorded at its
previous seal with those on the unit's commit recorded at this seal, and
lists each clause whose text differs with both texts, marking a clause that
was added or removed. A clause whose text on each of the two unit commits
matches the main commit sealed with it is left out, since main changed it
and the amendment did not. When no clause is listed, the bundle says the
amendment changed no clause. Bundles after any other seal say nothing of an
amendment.

Rejected and deferred proposals go to the archive: a Markdown entry under
`archive/rejected/` or `archive/deferred/` on the `shed/archive` branch,
which shares no history with main, so archiving never moves main. The entry
holds the citations, the reason or what would change the decision, the spec
changes and the debate. When the proposal adds, changes or removes horizon
clauses against the latest main commit its change descends from, the entry
also lists them under "Horizon changes", in document order. Each item gives
the clause ID, whether it was added, changed or removed, and its tag list and
text before the proposal, after it, or both. A clause counts as changed when
its tag list differs or its text differs once runs of whitespace are
collapsed. An entry whose proposal changes no horizon clause has no such
section (S.shed.15). A contested unit the owner defers or rejects with
`shed answer` goes to the deferred or rejected shelf the same way, and its
entry also holds the owner's answers. A rejected one cites the charter
clauses the owner's reason names.

## Implementation

The mechanic walks the operator's formula, one session per step. The
default formula writes proofs, then the implementation, then the
documentation. Each session gets a writable copy of the unit's files and the
`run_tests` and `prove` tools, and shed captures its files onto the unit's
change when it ends. A mechanic that finds the sealed spec wrong reports
`reopen`, and the unit goes back to the shed. A step that fails three times in
a row reopens the unit too. When the unit's change holds an unresolved
conflict, every mechanic's bundle names each file holding one and says to
resolve it against the sealed spec before any other work.

A mechanic that wants the sealed spec changed can report `amend` instead. Its
note names each sealed clause it wants changed, gives the wording it wants for
each, and says why; `done` refuses an `amend` whose note cites no clause of
the unit's sealed spec. The unit reopens as it does for `reopen`: no further
step starts and the files already captured stay on the unit's change. The
reopen counts an amendment as well as a bounce, and the unit's next bundle
says that the mechanic requested an amendment and gives its note in full.

A mechanic may mark the horizon clauses the unit advances as realised, by
adding `realised` to their tags. Nothing else in the horizon may change: the
horizon amendment the unit carries is the one it was sealed with.

## Verification

Verification first checks the unit mechanically: its change holds no
unresolved conflict, its documents pass `shed check`, it changes the
horizon only as sealed or by marking clauses it advances as realised, and
the proofs of its footprint pass. With `verify.all_proofs = true` in
`shed.toml` every proof must pass. A horizon clause whose text or tags on the
unit's change differ from main's, a clause missing from one counting as
different, passes only when it is as on the unit's commit recorded at its
latest seal and differed there from the main commit recorded in that seal,
or when it is as on main or on that sealed commit apart from gaining
`realised` on a clause the unit advances (S.verify.1). Then a committee member who did not work on the unit
reviews it against the sealed spec and the charter, without keeping any
change, recording
findings with `finding`. A unit that fails goes back to implementing with a
notice for the mechanic; one whose spec the reviewer finds wrong reopens; one
that passes is queued.

## Landing

Landing rebases the unit onto main, keeping any conflicts in its files. A
wheelbuilder session resolves them against the sealed spec. Then the unit
lands as one commit, as [version control](vcs.md) describes. A unit whose
conflicts cannot be resolved, or that changes nothing, reopens.

After a landing, shed compares the horizon on the landed commit with the
horizon on its parent. A clause counts as changed when its tags differ or its
text differs once runs of whitespace are collapsed, as `shed inbox` counts it.
Shed then checks every other unit that is sealed, implementing, verifying or
queued against the horizon clauses its footprint recorded at its last seal:

- A unit advancing a clause the landing removed reopens, with shed as actor
  and a reason naming the landed unit and each removed clause. The reopen
  counts a bounce, as any reopen does.
- A unit advancing a clause the landing changed, and none it removed, gets
  one notice. The notice names the landed unit by its short change ID and
  gives each changed clause, in the order of the old horizon, with its tags
  and text before and after. It arrives in the unit's next bundle, shows in
  `shed unit log`, and does not move the unit.
- A unit advancing a clause that the landed horizon now refines, through a
  `refines` tag the old horizon did not give that clause, gets the same one
  notice. After any changed clauses, and in the order of the new horizon, the
  notice gives each such refining clause with its tags and text, marked as
  gained, and names the clause it refines. A unit whose notice gives only
  gained clauses keeps its state and its seal and is not marked for horizon
  review. A unit reopened for a removed clause gets no notice.
- Any other unit is left alone, and nothing is recorded for it.

A notice that gives a clause whose text changed, once runs of whitespace are
collapsed, also marks the unit for horizon review. A change to a clause's tags
alone does not. While a unit is marked, shed starts no implementation,
verification or landing for it, and `shed land` refuses it. Once the unit has
no stage running, the wheelbuilder reviews it in one session. The bundle for
that session shows the unit's pending notices without delivering them, so
they stay pending for the unit's next painter or mechanic session. The
wheelbuilder calls `done` with a written reason and one of two outcomes:

- `consistent`: the unit keeps its state and its seal, and a notice giving
  the reason joins its pending notices.
- `reopen`: the unit reopens with the wheelbuilder as actor and that reason,
  and the reopen counts a bounce.

`shed unit log` records either outcome, and either one clears the mark. The
mark also clears when the unit leaves sealed, implementing, verifying and
queued by any other route. A review session that reports neither outcome
leaves the mark, and a later review runs.

Then shed rebases every other unit that is neither landed nor archived onto
the new main, leaving its state alone. A proposed or contested unit,
including one the horizon check just reopened, keeps any conflict stored in
its files for its painter to resolve. A unit past its seal keeps the rebase
only if it is free of conflicts; otherwise shed undoes it, and the unit takes
main's changes later. A unit with a session running is rebased once its
sessions end and are captured. `shed land` prints each unit's outcome, and each unit's log records it. See
[version control](vcs.md#keeping-units-on-main).

## Autopilot

`shed serve` runs a controller for each role, all reading the tracker:

| Controller | Starts | When |
| --- | --- | --- |
| wheelbuilder | horizon review, then landing | a unit is marked for horizon review and has no stage running; a unit is queued and not marked, one landing at a time |
| verifier | verification | a unit is verifying |
| mechanic | implementation | a unit is implementing, or sealed while fewer than `concurrency.units` units implement or verify |
| shed | debate | a proposal declares a horizon clause and fewer than `concurrency.in_flight` units are sealed through queued; one debate at a time |
| painter | a proposal | the gap is not empty, fewer than `painter.max_proposed` units are proposed, and the painter is not backing off |

Units opened by `shed frame` never count toward `painter.max_proposed`, and
`shed serve -once` does not report them as drafts waiting to be declared.

Each pass starts work downstream first, so work in flight finishes before new
work starts: horizon reviews, landing, verification, implementation, debate,
then proposals. Controllers start stages in the background and never wait on
a session; a stage that ends wakes them all, and `serve.tick` wakes them
anyway. `shed serve -once` stops when nothing is running and nothing can
start.

The painter is the only controller that creates work, so it is the one
throttled, by how its proposals fare. While they are being sealed it
proposes again as soon as `painter.max_proposed` allows; with the default
of one, that is when its last proposal leaves the shed. After a proposal
that goes nowhere, archived or contested without being sealed, it waits
`painter.interval`, doubling for each further one in a row up to
`painter.max_interval`, and the next sealed proposal ends the streak. A
painter session that failed before doing anything does not count.

The painter works on the gap: near and soon horizon clauses that are not
realised and that no unit in flight advances. Each gap clause comes with the
spec clauses already advancing it and, if it refines another clause, that
clause's ID and tier. It sees the deferred and rejected shelves and the units
in flight. Distant and eventual clauses need
breaking into near or soon clauses, by editing the horizon, before the
painter proposes against them. `shed frame` drafts that breakdown.

## Framing the horizon

`shed frame <clause>` runs one frame builder session on a horizon clause of
main that is distant or eventual and not realised. It refuses any other
clause, and an ID that is not in the horizon, before starting a session. The
session works in a writable copy of main's files. Its bundle holds the
charter, the horizon, the clause to frame with the clauses that already
refine it and the spec clauses that advance it, and the units in flight with
their footprints. It ends with `framed` or `nothing`.

When the session reports `framed`, shed checks its copy against main:

- `horizon.md` is the only file changed, and the only change to it is added
  clauses;
- at least one clause is added;
- each added clause is tagged `near` or `soon`, is not realised, carries
  `refines` naming the framed clause, and has an ID that no clause on main
  holds or has held.

When the check passes, shed opens a unit on main whose change holds the
session's `horizon.md` and nothing else, titled after the framed clause, and
prints its short change ID and each added clause with its tier:

```
$ shed frame H.greet.3
opened qpvuntsm in proposed, framing H.greet.3
  H.greet.4	near
  H.greet.5	soon
```

When the check fails, shed lists every change that breaks it. When the
session reports `nothing` or ends without an outcome, shed says so. Either
way it keeps nothing and opens no unit.

The unit is a draft that is never debated, and since it modifies no spec
clause it cannot be declared. It stays proposed as a record of the framing
for the owner to read, until the owner lands it with `shed frame -accept` or
discards it with `shed frame -discard`; no clause seals it. Read it with
`shed unit path <unit>`. Crash recovery never archives it. Rebasing it onto a
new main, after a landing or by the recovery sweep, keeps the rebase only if
it leaves no conflict, as for a unit past its seal (see
[keeping units on main](vcs.md#keeping-units-on-main)).

`shed frame -discard <unit>` archives a proposed unit that `shed frame`
opened, as deferred with no archive entry, and discards its change; it
refuses any other unit.

`shed frame -accept <unit>` lands a proposed unit that `shed frame` opened;
it refuses any other unit. It fetches main, rebases the unit's change onto
it, and reruns the `shed frame` check against that main, for the clause the
unit was opened for, requiring the clause still be one `shed frame` accepts.
If the rebase conflicts or the check fails, it names every conflict or every
change that breaks the check, undoes the rebase, and leaves the unit
proposed with its change and workspace as they were. Otherwise it lands the
change like `shed land` (see [landing](vcs.md#landing)) and moves the unit
from proposed to landed with the owner as actor. The commit holds the unit's
title, the ID and tier of each added clause in document order, and a `Unit:`
trailer; it carries no `Sealed-Against:` trailer, since the unit was never
sealed:

```
$ shed frame -accept qpvuntsm
accepted qpvuntsm, landed on main as 3f2a9c1d7e4b5a6f8c9d0e1f2a3b4c5d6e7f8091
zsxkmwqp proposed: rebased cleanly
yostqsxw sealed: rebased cleanly
```

The landing counts as a horizon amendment, so `shed tracker rebuild` counts
it and its place in the count skips a sample even when `owner.sample_every`
would otherwise land on it (see [the owner inbox](tracker.md#the-owner-inbox)),
since the owner already saw and accepted it. Its footprint records no spec
clause and no dependency, only the framed clause as the horizon clause it
advances. From here it follows every step a `shed land` landing does, with
the frame unit as the landed unit: the notices, marks and reopens of the
other units in flight, then their rebase sweep, printed and logged the same
way, after the landed commit.

The in-flight cap defaults to one, so only one unit is ever between sealed
and landed and nothing needs reconciling.

No stage starts while the sessions of the last 24 hours cost
`budget.per_day_usd` or more; `shed status` says so, and says when the latest
sessions have failed for infrastructure reasons in a row. After a crash,
`shed serve` carries on from what the tracker holds.

## Running shed on this repository

1. Colocate jj with git: `jj git init --colocate`.
2. Store a credential for each agent with `sbx secret set`.
3. Optionally write `.shed/config.toml`: at least `vcs.remote = "origin"` to
   push landings, and the models, budgets and caps you want.
4. Build shed, check that everything is in place, and serve:

   ```sh
   go build -o shed ./cmd/shed
   ./shed doctor
   ./shed serve
   ```
