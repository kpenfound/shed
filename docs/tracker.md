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
  painter, moves it on to `contested`. Shed leaves a notice for the owner
  and keeps working on other units. A unit leaves `contested` only on the
  owner's answer: back to `proposed`, or to the deferred shelf.
- `landed` and `archived` are terminal. An archived unit rests on the
  `rejected` or `deferred` shelf.

Every move records an actor and a reason.

### Footprints and seals

Sealing records the seal, the main commit, the unit's change ID and the
commit that change points to, together with the unit's footprint. The footprint lists the spec clauses the unit
modifies, the spec clauses it depends on and the horizon clauses it
advances.

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

Last it lists charter questions: the charter clauses that keep sinking
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

```
Contested units:
  qpvuntsm  new  bounces 4  Say goodbye: bounced 4 times, over the threshold of 3

Horizon changes since main at 3f2a9c1d7e4b:
  changed  H.greet.2  near
  added    H.greet.4  eventual

Charter questions:
  C6          new
    zkxolmrw  Host a web dashboard
    ynqtprsv  Sync units to GitHub issues
  C12
    wlsmvkto  Edit the spec on main from the sweeper
    rpoznkqx  Land spec fixes without a unit
```

Every `shed inbox` records the main commit it read, and the event sequence
number it marks against, in the event log, so `shed tracker rebuild` keeps
them and the next inbox starts from there. The
first inbox has no earlier commit and lists no horizon changes. If the
recorded commit is not an ancestor of main, for example after the remote was
re-cloned, the inbox says so and lists no horizon changes, and the commit it
records becomes the new starting point. `shed inbox -peek` lists the same
entries, marks new units and questions against the previous recorded inbox
just as a recording read does, and records nothing (S.owner.3, S.owner.7,
S.owner.9).

Reading the inbox never moves a unit and never starts or stops a stage. The
factory does not wait for it, and listing a charter question holds back no
session or proposal.

## Answering contested units

The owner takes a unit out of `contested` with `shed answer`, and only with
it:

```
shed answer qpvuntsm retry the painter has the missing clause now
shed answer qpvuntsm defer revisit once the sweeper exists
shed answer qpvuntsm reject edits main directly, against C12 (C2 to C4)
```

- `retry` moves the unit to `proposed`, with the owner as actor and the
  reason as the move's reason. The unit keeps its bounce count, so it is
  still past the threshold and its next bounce, of any kind, sends it back
  to `contested` (S.owner.4).
- `defer` archives the unit on the deferred shelf as a deferred proposal is
  archived, with the owner as actor and the reason as what would change the
  decision (S.owner.5).
- `reject` archives the unit on the rejected shelf as a rejected proposal is
  archived, with the owner as actor and the reason as the move's reason. The
  entry cites as violated every charter clause the reason names, in the
  order they first appear and without repeats (S.owner.8).

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
refuses an answer to a unit that is not contested, an answer to a unit that
is not `retry`, `defer` or `reject`, and an answer with an empty reason. A
refused answer records nothing and moves nothing (S.owner.6).

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

## Commands

| Command | Does |
| --- | --- |
| `shed status` | Lists units in the order they opened, with state, bounces, amendments, cost and title, then the notices waiting for the owner. |
| `shed inbox [-peek]` | Lists contested units, the horizon changes on main and the charter questions from repeated rejections, marking what is new since the last inbox. `-peek` records nothing. |
| `shed unit open <title>` | Makes a jj change for the unit on top of main and opens the unit in `proposed`. |
| `shed answer <unit> retry\|defer\|reject <reason>` | Answers a contested unit: moves it back to `proposed`, or defers or rejects it to the archive. |
| `shed answer <clause> keep <reason>` | Answers a charter question by keeping the clause, clearing the question until two more rejections cite it. |
| `shed unit move <unit> <state> <reason>` | Moves a unit by hand to `implementing`, `verifying` or `queued`. |
| `shed unit reopen [-amendment] <unit> <reason>` | Sends a unit back to the shed. |
| `shed unit log <unit>` | Prints a unit's events. |
| `shed unit path <unit>` | Prints the directory of the unit's workspace. |
| `shed land <unit>` | Lands a queued unit on main and reports its footprint drift. See [version control](vcs.md). |
| `shed tracker rebuild` | Rebuilds the database from the event log. |
| `shed config` | Prints the operator settings in effect. |

A unit argument is any prefix of its change ID that names one unit.

Sealing, landing and archiving each produce a record of their own, so they
cannot be done by hand with `unit move`. `shed land` is the only way a unit
becomes landed. `shed unit open`, `move` and `reopen` never move a unit out
of `contested`; `shed answer` does that.

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
contested_timeout = "72h"

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
```

A profile's fallback must name another profile, and fallbacks may not loop.
A formula is a DAG: every step it needs must exist, and no step may need
itself through a cycle.
