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
| `horizon` | It does not move the spec toward the horizon. | Deferred if only horizon objections stand at the cap. |
| `size` | The footprint is too large; split it by clause. | Stands until withdrawn. |
| `spec` | A clause is ambiguous, untestable or wrong. | Stands until withdrawn. |

Between rounds the painter answers each standing objection once with
`answer`, and may revise the files. Only the member who raised an objection
can withdraw it. With no objection standing, the unit is sealed against
main's current commit and the commit its change points to. At `shed.max_rounds` with objections still standing,
the proposal bounces back to its painter and counts a bounce. Unless that
bounce leaves it past `shed.bounce_threshold` bounces, when it is contested
and waits for the owner's `shed answer`, it stays proposed and debates
afresh next time.

A unit whose latest reopen requested an amendment is debated in the
amendment lane. Every rule above holds, but the round cap is
`shed.amendment_rounds` instead of `shed.max_rounds`, and each member's
session is told that cap. The unit stays in the lane, even after a bounce at
the cap, until it is next sealed.

An amendment is scoped to the sealed spec: the clauses the footprint
recorded at the unit's last seal modified or depended on. Every member's and
the painter's session is told those clauses. A clause outside that scope
must keep the text it had on the main commit recorded in the seal, so clauses
that main changed after the seal never count against the amendment. When a
round ends with no objection standing but the proposal changes a clause
outside its scope, the unit is not sealed. It bounces to its painter, with a
reason naming each clause outside the scope.

At the amendment lane's cap, standing objections reject the amendment rather
than bounce or defer the unit, as long as none of them is a charter
objection. A charter objection still rejects the whole proposal, which is
archived. A rejected amendment archives nothing and keeps the unit's change.
Shed makes the files under `spec/` exactly those on the unit's commit
recorded at its last seal, removing any spec file absent there and leaving
every other file alone. It then seals the unit again, with the standing
objections as the reason. The restore and the seal happen together. While the
in-flight cap holds sealing back, nothing is restored and the unit waits in
proposed in the amendment lane. Its debate does not start afresh, so the
next one runs no further round and rejects the amendment again. Once sealed,
the unit leaves the lane and counts no further bounce. Every mechanic
session of its next implementation is told in its bundle that the requested
amendment was rejected and the sealed spec stands as written, with the
objections that stood at the cap.

When the amendment lane seals the unit after accepting an amendment the unit, every mechanic session of its next
implementation is told in its bundle that the unit was resealed after an
amendment, with the amendment's diff. The diff compares the clauses of
`spec/` on the unit's commit recorded at its previous seal with those on the
unit's commit recorded at this seal, and lists each clause whose text
differs with both texts, marking a clause that was added or removed. A clause
whose text on each of the two unit commits matches the main commit sealed
with it is left out, since main changed it and the amendment did not. When
no clause is listed, the bundle says the amendment changed no clause.
Bundles after any other seal say nothing of an amendment.

Rejected and deferred proposals go to the archive: a Markdown entry under
`archive/rejected/` or `archive/deferred/` on the `shed/archive` branch,
which shares no history with main, so archiving never moves main. The entry
holds the citations, the reason or what would change the decision, the spec
changes and the debate. A contested unit the owner defers with `shed answer`
goes to the deferred shelf the same way, and its entry also holds the
owner's answers.

## Implementation

The mechanic walks the operator's formula, one session per step. The
default formula writes proofs, then the implementation, then the
documentation. Each session gets a writable copy of the unit's files and the
`run_tests` and `prove` tools, and shed captures its files onto the unit's
change when it ends. A mechanic that finds the sealed spec wrong reports
`reopen`, and the unit goes back to the shed. A step that fails three times in
a row reopens the unit too.

A mechanic that wants the sealed spec changed can report `amend` instead. Its
note names each sealed clause it wants changed, gives the wording it wants for
each, and says why; `done` refuses an `amend` whose note cites no clause of
the unit's sealed spec. The unit reopens as it does for `reopen`: no further
step starts and the files already captured stay on the unit's change. The
reopen counts an amendment as well as a bounce, and the unit's next bundle
says that the mechanic requested an amendment and gives its note in full.

A mechanic may mark the horizon clauses the unit advances as realised, by
adding `realised` to their tags. Nothing else in the horizon may change.

## Verification

Verification first checks the unit mechanically: its documents pass
`shed check`, its horizon changes only mark clauses it advances, and the
proofs of its footprint pass. With `verify.all_proofs = true` in `shed.toml`
every proof must pass. Then a committee member who did not work on the unit
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

## Autopilot

`shed serve` runs a controller for each role, all reading the tracker:

| Controller | Starts | When |
| --- | --- | --- |
| wheelbuilder | landing | a unit is queued; one landing at a time |
| verifier | verification | a unit is verifying |
| mechanic | implementation | a unit is implementing, or sealed while fewer than `concurrency.units` units implement or verify |
| shed | debate | a proposal declares a horizon clause and fewer than `concurrency.in_flight` units are sealed through queued; one debate at a time |
| painter | a proposal | the gap is not empty, fewer than `painter.max_proposed` units are proposed, and the painter is not backing off |

Each pass starts work downstream first, so work in flight finishes before new
work starts. Controllers start stages in the background and never wait on
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
realised and that no unit in flight advances. It sees the deferred and
rejected shelves and the units in flight. Distant clauses need promoting to
soon, by editing the horizon, before the painter proposes against them.

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
