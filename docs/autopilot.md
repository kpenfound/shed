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
revision at once, read-only. They object with `object`, citing clause IDs
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
main's current commit. At `shed.max_rounds` with objections still standing,
the proposal bounces back to its painter: it stays proposed, counts a bounce
and debates afresh next time. Past `shed.bounce_threshold` bounces it is
contested and waits for the owner.

Rejected and deferred proposals go to the archive: a Markdown entry under
`archive/rejected/` or `archive/deferred/` on the `shed/archive` branch,
which shares no history with main, so archiving never moves main. The entry
holds the citations, the reason or what would change the decision, the spec
changes and the debate.

## Implementation

The mechanic walks the operator's formula, one session per step. The
default formula writes proofs, then the implementation, then the
documentation. Each session gets a writable copy of the unit's files and the
`run_tests` and `prove` tools, and shed captures its files onto the unit's
change when it ends. A mechanic that finds the sealed spec wrong reports
`reopen`, and the unit goes back to the shed. A step that fails three times in
a row reopens the unit too.

A mechanic may mark the horizon clauses the unit advances as realised, by
adding `realised` to their tags. Nothing else in the horizon may change.

## Verification

Verification first checks the unit mechanically: its documents pass
`shed check`, its horizon changes only mark clauses it advances, and the
proofs of its footprint pass. With `verify.all_proofs = true` in `shed.toml`
every proof must pass. Then a committee member who did not work on the unit
reviews it read-only against the sealed spec and the charter, recording
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
| painter | a proposal | the gap is not empty, fewer than `painter.max_proposed` units are proposed, and `painter.interval` has passed |

Each pass starts work downstream first, so work in flight finishes before new
work starts. Controllers start stages in the background and never wait on
a session; a stage that ends wakes them all, and `serve.tick` wakes them
anyway. `shed serve -once` stops when nothing is running and nothing can
start.

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
