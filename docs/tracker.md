# Units and the tracker

A change unit is one spec diff with its proofs and implementation, identified
by its jj change ID. The tracker records where every unit is and everything
that happened to it.

## The state directory

Shed keeps workflow state in `.shed` under the repository root. Pass
`-state <dir>` or set `$SHED_STATE` to keep it somewhere else. The directory
stays out of version control; the repository's `.gitignore` lists `.shed/`.

| Path | Holds |
| --- | --- |
| `events.jsonl` | The event log: one JSON line per change, oldest first. It is the source of truth. |
| `tracker.db` | A SQLite database built from the event log, for fast reads. |
| `sessions/<id>/` | One directory per agent session: `bundle.md`, `transcript.jsonl`, `outcome.json` and `result.json`. |
| `config.toml` | Operator settings. |
| `workspaces/` | A jj workspace per unit in flight. See [version control](vcs.md). |
| `conflicts/<change>.json` | Per unit, each file shed wrote conflict markers into and the marker lines it wrote. See [version control](vcs.md#unresolved-conflicts). |
| `lock` | Taken while a shed process changes the tracker. |

Every change is appended to the event log and synced before it reaches the
database. If shed stops between the two, the next shed process to open the
tracker applies the missing events. A last line cut short by a crash is
ignored and overwritten by the next change. `shed tracker rebuild` empties
the database and replays the whole log.

When the tracker opens, sessions whose process has exited are marked
interrupted. A unit keeps the list of steps its sessions finished, so work
picks up after the last finished step.

## Unit states

```
proposed -> sealed -> implementing -> verifying -> queued -> landed
    ^_____________________________________________|
                  reopen, from sealed through queued
```

- A unit opens in `proposed`, the shed's resting state.
- `verifying` may send a unit back to `implementing` when the spec is not in
  question.
- A reopen sends a unit from `sealed`, `implementing`, `verifying` or `queued`
  back to `proposed` with a reason. It counts a bounce. A reopen that
  requests an amendment also counts an amendment.
- Any bounce that leaves a unit's bounce count above the operator's
  `bounce_threshold`, whether a reopen or a proposal bounced back to its
  painter, moves it on to `contested`. So does a debate that reaches
  consensus on a distant or eventual horizon amendment, or splits at its
  round cap over a soon one, without counting a bounce. Shed leaves a notice for the owner and keeps working on other
  units. A unit leaves `contested` only by the owner's answer, back to
  `proposed` or to the deferred or rejected shelf, or by the frame builder
  archiving it after a timeout. Shed refuses any other move out of
  `contested`, naming the unit and the actor, and leaves the unit as it was
  (S.unit.9).
- A unit's cost can force a reopen on its own. Once a sealed, implementing,
  verifying or queued unit's cost since the seal that set its estimate (see
  `ESTIMATE` under [footprints and seals](#footprints-and-seals), below)
  passes that estimate times `budget.overrun_multiple`, the unit has
  overrun, and shed starts no further session on it in any role. A stage
  already running when a session's cost crosses that line still records
  the session's result and any move it makes as usual, but starts nothing
  more and lands nothing. Once no session on the unit is
  running, shed reopens it with shed as the actor and a reason naming the
  cost, the estimate and the multiple, the amounts in USD to the cent, and
  clears the recorded estimate, so the unit sits as a draft until a painter
  declares a fresh one. The reopen counts a bounce like any other and never
  requests an amendment. `shed serve` checks every pass, so a unit that
  overran while no session was running on it — after a restart, or because
  the operator lowered `overrun_multiple` — is reopened on the next pass
  just the same. `overrun_multiple` defaults to 3; zero turns overruns off,
  and a unit whose most recent seal recorded no estimate never overruns.
- A sealing bounce for a `spec/` conflict a landing brought in after the
  painter's latest session saw `spec/` clean counts no bounce toward
  `bounce_threshold`, since the painter never had the conflict to leave.
  Such an uncounted bounce still runs its own count, since the unit opened
  or was last sealed, and crossing `bounce_threshold` on that count alone
  still moves the unit to `contested`, with a notice that landings kept
  bringing conflicts under `spec/`. See [debate](autopilot.md#debate).
- `landed` and `archived` are terminal. An archived unit rests on the
  `rejected` or `deferred` shelf.

Every move records an actor and a reason.

### Footprints and seals

Sealing records the seal, the main commit, the unit's change ID and the
commit that change points to, together with the unit's footprint. The footprint lists the spec clauses the unit
modifies, the spec clauses it depends on and the horizon clauses it
advances, and its estimate: a positive amount in USD of what taking the
unit from sealed to landed will cost. A unit with no recorded estimate is a
draft and is never debated, so every seal records one. `shed unit log` shows
it on the seal's line:

```
proposed -> sealed at main 69636c9e2da4, estimate $150.00 as f2a9c1d7e4b5
```

A seal out of the amendment lane keeps the estimate recorded at the unit's
previous seal, whatever was declared meanwhile; see
[the amendment lane](autopilot.md#debate). `shed tracker rebuild` gives the
estimate back too, giving back with no estimate a seal recorded before shed
tracked them.

`shed status` shows a unit's estimate beside its cost since the seal that
set it: the unit's most recent seal that did not carry its estimate forward
from the seal before it. A seal out of the amendment lane that carries the
estimate forward therefore leaves that window where it was, so a
mechanic-requested amendment does not reset it; any other seal starts it
afresh. The window counts the cost of every session that finished after that
seal, in any role and whatever state the unit has since moved to, so a unit
reopened after that seal keeps adding to the window until a seal sets the
estimate again. A session still running adds nothing until it finishes. Both
amounts render in USD to the cent in their own `ESTIMATE` column, cost since
the seal first, as `$1.20 of $5.00`. This is separate from the unconditional
`COST` column, which keeps showing the unit's whole cost so far
(S.track.9). A unit that was never sealed, or whose most recent seal
recorded no estimate, leaves `ESTIMATE` empty. `shed tracker rebuild` gives
back the same amounts in both columns.

Landing records the unit's actual footprint beside the sealed one: the spec
clauses the landed commit adds, changes or removes against its parent, with
the dependencies and horizon clauses recorded at the last seal. The drift is
the difference between the clauses the two footprints modify: the clauses
the unit modified but was not sealed to, and the clauses it was sealed to
modify but did not. `shed land` prints the drift after the landed commit,
and the landing's line in `shed unit log` ends with it:

```
landed qpvuntsm on main as 3f2a9c1d7e4b5a6f8c9d0e1f2a3b4c5d6e7f8091
footprint drifted: not sealed S.greet.3; not modified none
```

When both lists are empty the report reads `footprint held`.
`shed tracker rebuild` gives back both footprints.

### Entanglement

Two in-flight units are entangled when their spec footprints, the clauses
they modify and depend on, share a clause. Horizon clauses do not count.
When a unit seals, shed compares its spec footprint with the footprint
recorded at the last seal of every other unit that is sealed,
implementing, verifying or queued. For each unit it shares a clause with,
shed records an advisory in the newly sealed unit's `shed unit log`:

```
entangled with unit qpvuntsm on S.greet.2, S.greet.3
```

The advisory names the other unit by its short change ID. The shared
clauses follow the order they appear in main's `spec/` at the seal, with
files taken in name order. Shared clauses main does not have yet come
last, in the order they appear in the newly sealed unit's `spec/`. When a
unit is entangled with several others, their advisories follow the order
the units opened, as `shed status` lists them.

An advisory blocks nothing. The seal and both units' states are as they
would be without it. It is a prompt to consider merging the two units while
both are still cheap to change.

A unit may depend only on spec clauses that are on main or that it modifies
itself. Depending on a clause another in-flight unit is adding is refused.
Wait for that unit to land, or merge the two units.

## The owner inbox

`shed inbox` gathers what is waiting for the owner. It lists every contested
unit in the order the units became contested, each with its short change ID,
bounce count, title and the reason it was contested. A unit that has left
`contested` drops off the list (S.owner.1).

Beside each contested unit, both `shed inbox` and `shed status` show how long
it has waited: the time from its latest move to `contested` to the moment the
inbox or status is read. The tracker keeps that time with the unit when it
records the move to `contested`, so it needs no scan of the event log and
`shed tracker rebuild` restores it along with the rest of the unit's state.
The wait is rendered rounded down to the whole minute, such as `26h5m`; the
rounding is only cosmetic. A unit is marked `overdue` when its unrounded wait
is strictly longer than `shed.contested_timeout` (see
[Operator settings](#operator-settings)), so a wait exactly equal to the
timeout is not overdue. A `shed.contested_timeout` of zero turns expiry off,
so no unit is ever marked overdue. `shed inbox -peek` shows the same waits
and marks without recording anything. Units that are not contested show
neither a wait nor an overdue mark in `shed status` (S.owner.15, S.track.11).

A contested unit is marked `new` when it became contested after the previous
recorded `shed inbox` read the list, so the owner can see at a glance what
they have not seen yet. "After" means event order, not wall-clock time: each
recorded inbox stores the sequence number of the latest tracker event at the
moment it read the contested units, and a unit is new when its latest move to
`contested` has a higher sequence number. A unit contested while an inbox is
running is therefore new at the next one. When no inbox has been recorded,
every contested unit is new. A unit that is retried and becomes contested
again after the previous recorded inbox is new again (S.owner.7).

Then it lists the horizon clauses added, changed or removed on main since
the main commit the previous `shed inbox` read. They come in document order,
each with its ID and tier. A removed clause shows its tier at that earlier
commit. A clause counts as changed when its tag list differs, or when its
text differs once runs of whitespace are collapsed to one space, so
rewrapping a clause is not a change (S.owner.2).

A clause whose `refines` tag differs between that commit and main, including
an added clause that carries one and a removed clause that carried one, also
names on its line each parent `shed diff` between the two commits would
count it at, with the parent's tier on the commit whose tag names it: first
the parent on the recorded commit, then the one on main. A clause whose tag
is the same on both commits, or that carries none, names no parent
(S.owner.13).

Then it lists charter questions: the charter clauses that keep sinking
proposals. A clause of the charter on main is a question when the entries of
at least two units on the rejected shelf cite it as violated. Shed counts,
for each clause, only the units archived after the owner last kept that
clause with `shed answer` (see below), by event sequence number, or the
whole shelf when the clause has never been kept. Questions come in charter
order, each with its clause ID and then, in the order they were archived,
the short change ID and title of every counted unit whose entry cites it.
A citation counts for its clause with or without a revision, so `C3@HEAD~1`
counts toward C3, and a unit whose entry cites a clause more than once counts
once. Spec and horizon citations raise no question, and neither do charter
IDs that are not clauses of the charter on main, such as retired ones.

A question is marked `new` when at least one of its counted units was
archived after the previous recorded `shed inbox`, by the same event sequence
number that marks contested units. When no inbox has been recorded, every
question is new. Reading the inbox does not reset the count: a question stays
listed until the owner keeps its clause, and it loses its mark once the owner
has seen all of its units (S.owner.9).

Last come sampled amendments, so the owner can check horizon amendments the
factory accepted without them. A landed unit's commit amends a horizon
clause when, against its parent on main, it adds or removes the clause or
changes it as a horizon change above, except when the only change is adding
`realised` to its tag list. A clause whose text or other tags change as well
is amended. A horizon amendment is a landed unit whose commit amends at
least one horizon clause, so a unit that only marks clauses realised is not
one.

Every landing records in the event log whether the unit is a horizon
amendment, so `shed tracker rebuild` keeps the count of horizon amendments.
Landings from before shed recorded this do not count.

A horizon amendment's landing also records whether it is owner-accepted:
true when it lands by [`shed frame -accept`](autopilot.md#framing-the-horizon),
or when the unit's latest seal before landing follows the owner's `approve`
(see below); a unit sealed again after that, as when it is resealed out of
the amendment lane, is judged by that later seal alone. Every other horizon
amendment is auto-accepted. A horizon amendment landed before shed recorded
this is owner-accepted when it landed by `shed frame -accept` and
auto-accepted otherwise.

Sampling covers only auto-accepted horizon amendments: `shed tracker rebuild`
also keeps the sampling count, the number of auto-accepted horizon amendments
landed. With `owner.sample_every` set to N above 0, the landing of every Nth
auto-accepted horizon amendment in that count, counting from one in landing
order, also records the unit as sampled. With the default of 0 nothing is
sampled, but auto-accepted horizon amendments still count, and changing the
setting does not restart the count. An owner-accepted horizon amendment is
never recorded as sampled and takes no place in the sampling count, so with N
above 0 one in every N auto-accepted horizon amendments is sampled
(S.owner.11).

`shed status` ends with a line giving these counts, `amendments: <a>
auto-accepted, <s> sampled (<y> agreed, <n> disagreed, <u> unanswered)`,
where `<y>`, `<n>` and `<u>` split the sampled amendments by their `agree` or
`disagree` answer (below) or lack of one. The line always prints, zero counts
included, and `shed tracker rebuild` gives back the same counts (S.owner.21).

The inbox lists each unit sampled after the event sequence number the
previous recorded `shed inbox` stored, or every sampled unit when no inbox
has been recorded, in landing order. Each shows its short change ID, landed
commit and title, then the horizon clauses its commit amended, in document
order, each marked added, changed or removed. A clause the commit only
marked realised is left out. `-peek` lists the same amendments (S.owner.12).
Sampling moves no unit and never holds back or fails a landing.

Last come units the frame builder archived on timeout (S.frame.7) since the
previous recorded `shed inbox`, or every such unit when no inbox has been
recorded, in the order they were archived. Each shows its short change ID,
shelf (`rejected` or `deferred`), the frame builder's reason and how long it
had waited from its latest move to `contested` to the archive, written
rounded down to the whole minute just as a contested unit's wait is. A unit
archived by the owner or any other actor is not listed here, only one the
frame builder archived on timeout, whether dispatched by hand with
`shed frame -expire` or automatically by `shed serve` (see
[expiring a contested unit by hand](autopilot.md#expiring-a-contested-unit-by-hand)
and [automatic expiry](autopilot.md#automatic-expiry)).
`-peek` lists the same expired units. Listing one moves no unit and changes
no archive entry (S.owner.16).

```
Contested units:
  qpvuntsm  new  bounces 4  wait 75h3m  overdue  Say goodbye: bounced 4 times, over the threshold of 3

Horizon changes since main at 3f2a9c1d7e4b:
  changed  H.greet.2  near
  added    H.greet.4  eventual
  added    H.greet.5  soon      parent H.greet.4 (eventual)

Charter questions:
  C6          new
    zkxolmrw  Host a web dashboard
    ynqtprsv  Sync units to GitHub issues
  C12
    wlsmvkto  Edit the spec on main from the sweeper
    rpoznkqx  Land spec fixes without a unit

Sampled amendments:
  xtnwkqpl  9c41d07a2be5  Sing along
    added    H.greet.4
    changed  H.greet.2

Expired units:
  zvxnqwto  rejected  wait 80h5m  Say goodbye: Goodbye is rude, breaks C2
```

Every `shed inbox` records the main commit it read, and the event sequence
number it marks against, in the event log, so `shed tracker rebuild` keeps
them and the next inbox starts from there. The
first inbox has no earlier commit and lists no horizon changes. If the
recorded commit is not an ancestor of main, for example after the remote was
re-cloned, the inbox says so and lists no horizon changes, and the commit it
records becomes the new starting point. `shed inbox -peek` lists the same
entries, with the same parents, marks new units and questions against the previous recorded inbox
just as a recording read does, and records nothing (S.owner.3, S.owner.7,
S.owner.9, S.owner.12, S.owner.13, S.owner.16).

Reading the inbox never moves a unit and never starts or stops a stage. The
factory does not wait for it, and listing a charter question holds back no
session or proposal.

## Answering contested units

The owner takes a unit out of `contested` with `shed answer`. The only other
way out is a frame builder session archiving an overdue one, whether
dispatched by hand with
[`shed frame -expire`](autopilot.md#expiring-a-contested-unit-by-hand) or
automatically by [`shed serve`](autopilot.md#automatic-expiry). Shed
refuses any other move out of `contested`, whatever its target state,
including one by the frame builder to `proposed`, one by the painter,
committee, mechanic, wheelbuilder or sweeper as the outcome of a session, and
one with shed as actor. The refusal names the unit and the actor and changes
nothing: no event is recorded, and the unit stays `contested` with its bounce
count, amendment count and latest move to `contested` unchanged (S.unit.9).

```
shed answer qpvuntsm retry the painter has the missing clause now
shed answer qpvuntsm defer revisit once the sweeper exists
shed answer qpvuntsm reject edits main directly, against C12 (C2 to C4)
shed answer qpvuntsm approve the new distant clause matches where shed is going
```

- `retry` moves the unit to `proposed`, with the owner as actor and the
  reason as the move's reason. The unit keeps its bounce count, so it is
  still past the threshold and its next bounce, of any kind, sends it back
  to `contested` (S.owner.4). A unit contested over a split soon horizon
  amendment goes back to its painter with the objections that stood at the
  cap, counts no bounce, and debates afresh from round one (S.shed.18).
- `defer` archives the unit on the deferred shelf as a deferred proposal is
  archived, with the owner as actor and the reason as what would change the
  decision (S.owner.5).
- `reject` archives the unit on the rejected shelf as a rejected proposal is
  archived, with the owner as actor and the reason as the move's reason. The
  entry cites as violated every charter clause the reason names, in the
  order they first appear and without repeats (S.owner.8).
- `approve` moves a unit that shed contested because its horizon amendment
  is distant or eventual tier, or a near or soon tier `shed.horizon_owner_approval`
  widened S.shed.16 to cover (S.shed.16), or because its debate split over
  a soon tier amendment (S.shed.18), back to `proposed`, with the owner
  as actor and the reason as the move's reason. Its next debate runs no
  round and seals it; a bounce before that seal ends the approval. Shed
  refuses `approve` for a unit contested for any other reason (S.shed.17).
  See [autopilot](autopilot.md#debate). If the unit's seal from that debate
  is the one it lands under, its landing is owner-accepted: it takes no
  place in the sampling count described [above](#the-owner-inbox) and is
  never sampled (S.owner.11).

A reject's reason names charter clauses through its ID-shaped tokens, read
whole as citations anywhere in the text. `C12` names C12 and not C1, `XC1`
names nothing, and punctuation around a token, as in `(C3)` or `C3,`, does
not change it. Two charter citations joined by "to" name every clause of the
charter on main from the first to the second, so `C2 to C4` names C2, C3 and
C4. Spec and horizon IDs name no charter clause. Shed refuses a reject whose
reason names no charter clause, has a charter citation with a revision such
as `C3@HEAD~1`, has a charter range whose ends do not increase, or names an
ID that is not a clause of the charter on main, such as a retired one.

Every later bundle of the unit has an "Owner's answers" section listing the
owner's answers to it, oldest first, each with its time, kind and reason. A
deferred or rejected unit's archive entry holds them too.

`shed answer` reads its first argument as a charter clause when it is a
charter citation such as `C3` or `C3@HEAD~1`, and as a unit otherwise. Shed
refuses an answer to a unit with a kind other than `retry`, `defer`,
`reject`, `approve`, `agree` or `disagree`, an answer of the first four
kinds to a unit that is not contested, and an answer with an empty reason. A
refused answer records nothing and moves nothing (S.owner.6). `agree` and
`disagree` answer a sampled amendment instead, and are not held to the
contested check; see [below](#answering-sampled-amendments).

## Answering sampled amendments

The owner answers a sampled amendment (see
[the owner inbox](#the-owner-inbox)) with `shed answer`, naming the landed
unit instead of a contested one:

```
shed answer xtnwkqpl agree the change matches where shed is going
shed answer xtnwkqpl disagree this drifted further than I'd like
```

`agree` and `disagree` record the owner's answer in the event log, with the
owner as actor, the reason as the event's reason, the unit, and whether the
owner agreed, so `shed tracker rebuild` keeps it. Shed refuses either kind
for a unit whose landing was not recorded as sampled, for a unit that has
not landed, and for a unit that already has an `agree` or `disagree`
answer, including one recorded before a `shed tracker rebuild`; a refused
answer records nothing. An `agree` or `disagree` answer moves no unit,
changes no archive entry and makes no commit, so main is unchanged
(S.owner.20).

## Answering charter questions

The owner answers a charter question by keeping the clause as it stands:

```
shed answer C6 keep hosting stays out; these proposals misread the charter
```

The keep is recorded in the event log with the owner as actor, the reason as
the event's reason, the clause ID and the sequence number of the latest
tracker event before the answer, so `shed tracker rebuild` keeps it. The
question leaves the inbox at once. From then on the inbox counts, for that
clause, only units archived after the keep, so the question comes back only
when at least two newly rejected units cite the clause. Questions for other
clauses are unaffected. A keep moves no unit and changes no archive entry
(S.owner.9, S.owner.10).

Shed refuses an answer to a clause that is not `keep`, a clause with a
revision such as `C6@HEAD~1`, an ID that is not a clause of the charter on
main, a clause with no charter question listed for it at that moment, and an
answer with an empty reason. A refused answer records nothing (S.owner.10).

## Sweeps

`shed sweep` (see [clauses and proofs](clauses.md#sweeping-main)) proves
main's current commit in a fresh directory outside every unit's workspace
and reports pass or fail per clause. A sweep whose proofs ran records
itself in the event log as one event naming no unit: the main commit it
checked out, when it started and pass or fail for each clause it swept.
`shed tracker rebuild` gives back the same sweeps. A sweep that cannot
bring in or check out main records nothing, prints why and exits non-zero.

## Commands

| Command | Does |
| --- | --- |
| `shed status` | Lists units in the order they opened, with state, bounces, amendments, cost, estimate (with the cost since the seal that set it) and wait; a contested unit's line also carries an `overdue` mark once its wait passes `shed.contested_timeout`, and a queued unit's line shows its place in the landing order as `land #<n>` (see [autopilot](autopilot.md#autopilot)). Then comes the title. After the units it lists the notices waiting for the owner, then one line on whether the painter may propose now, and ends with one line counting auto-accepted and sampled horizon amendments, agreed, disagreed and unanswered (see [autopilot](autopilot.md#autopilot)). |
| `shed inbox [-peek]` | Lists contested units with their wait and, once overdue, an `overdue` mark, the horizon changes on main, the charter questions from repeated rejections, the sampled horizon amendments and the units the frame builder archived on timeout, marking what is new since the last inbox. `-peek` records nothing. |
| `shed unit open <title>` | Makes a jj change for the unit on top of main and opens the unit in `proposed`. |
| `shed answer <unit> retry\|defer\|reject\|approve <reason>` | Answers a contested unit: moves it back to `proposed`, defers or rejects it to the archive, or approves its distant, eventual or split soon horizon amendment. |
| `shed answer <unit> agree\|disagree <reason>` | Answers a sampled amendment, recording whether the owner agreed with it; moves no unit. |
| `shed answer <clause> keep <reason>` | Answers a charter question by keeping the clause, clearing the question until two more rejections cite it. |
| `shed unit move <unit> <state> <reason>` | Moves a unit by hand to `implementing`, `verifying` or `queued`. |
| `shed unit reopen [-amendment] <unit> <reason>` | Sends a unit back to the shed. |
| `shed unit log <unit>` | Prints a unit's events. |
| `shed unit path <unit>` | Prints the directory of the unit's workspace. |
| `shed land <unit>` | Lands a queued unit that is not marked for horizon review on main, reports its footprint drift and how each unit in flight was rebased and reconciles the other units in flight against the horizon changes it made. See [version control](vcs.md) and [autopilot](autopilot.md#landing). |
| `shed sweep` | Proves main's current commit in a fresh directory and records the sweep. See [clauses and proofs](clauses.md#sweeping-main). |
| `shed tracker rebuild` | Rebuilds the database from the event log. |
| `shed config` | Prints the operator settings in effect. |

A unit argument is any prefix of its change ID that names one unit.

Sealing, landing and archiving each produce a record of their own, so they
cannot be done by hand with `unit move`. `shed land` is the only way a unit
becomes landed. `shed unit open`, `move` and `reopen` never move a unit out
of `contested`; only `shed answer` or a frame builder archive does that
(S.unit.8, S.unit.9).

## Operator settings

`config.toml` in the state directory tunes the factory on one machine. Every
setting is optional; `shed config` prints the values in effect. Shed refuses
an unknown key or a value it cannot use.

```toml
[budget]
per_session_usd = 5     # 0 for no cap
per_day_usd = 50        # 0 for no cap
overrun_multiple = 3    # how far past its estimate a unit's cost may go

[concurrency]
units = 4               # units across implementing and verifying
mechanics_per_unit = 1
committee = 3           # committee members per debate
in_flight = 1           # units from sealed through queued; 0 for no cap

[shed]
max_rounds = 3
amendment_rounds = 1    # the cap in the amendment lane; at most max_rounds
bounce_threshold = 3
contested_timeout = "72h"  # how long a unit may sit contested before shed inbox/status mark it overdue; 0 turns expiry off
horizon_owner_approval = false  # wait for the owner on every horizon amendment, not just distant and eventual

[profiles.default]
agent = "claude"        # claude, codex, opencode or pi
# model, effort, fallback (another profile), max_turns, timeout,
# template (sbx template), mounts ("path:ro" or "path:rw"), env

[roles.mechanic]        # also frame-builder, painter, committee, wheelbuilder, sweeper
profile = "default"

[formulas.default]
steps = [
  { name = "proofs" },
  { name = "implement", needs = ["proofs"] },
  { name = "docs", needs = ["implement"] },
]

[vcs]
jj = "jj"                # the jj executable
main = "main"            # the bookmark units land on
remote = ""              # fetched before and pushed after each landing; empty keeps main local
landing_name = "shed wheelbuilder"
landing_email = "wheelbuilder@shed.localhost"

[painter]
interval = "15m"         # the wait after a proposal that went nowhere
max_interval = "24h"     # the wait doubles for each further one, up to this
max_proposed = 1         # proposals that may wait before the painter proposes again

[serve]
tick = "1m"              # wakes the controllers when nothing else has

[owner]
sample_every = 0         # sample every Nth auto-accepted horizon amendment to the inbox; 0 for none
```

A profile's fallback must name another profile, and fallbacks may not loop.
A formula is a DAG: every step it needs must exist, and no step may need
itself through a cycle.
