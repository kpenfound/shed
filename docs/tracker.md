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
- When a unit bounces more times than the operator's `bounce_threshold`, it
  moves on to `contested`. Shed leaves a notice for the owner and keeps
  working on other units. From `contested`, a unit goes back to `proposed`
  when the owner answers, or to the archive.
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

Then it lists the horizon clauses added, changed or removed on main since
the main commit the previous `shed inbox` read. They come in document order,
each with its ID and tier. A removed clause shows its tier at that earlier
commit. A clause counts as changed when its tag list differs, or when its
text differs once runs of whitespace are collapsed to one space, so
rewrapping a clause is not a change (S.owner.2).

```
Contested units:
  qpvuntsm  bounces 4  Say goodbye: bounced 4 times, over the threshold of 3

Horizon changes since main at 3f2a9c1d7e4b:
  changed  H.greet.2  near
  added    H.greet.4  eventual
```

Every `shed inbox` records the main commit it read in the event log, so
`shed tracker rebuild` keeps it and the next inbox starts from there. The
first inbox has no earlier commit and lists no horizon changes. If the
recorded commit is not an ancestor of main, for example after the remote was
re-cloned, the inbox says so and lists no horizon changes, and the commit it
records becomes the new starting point. `shed inbox -peek` lists the same
entries and records nothing (S.owner.3).

Reading the inbox never moves a unit and never starts or stops a stage. The
factory does not wait for it.

## Commands

| Command | Does |
| --- | --- |
| `shed status` | Lists units in the order they opened, with state, bounces, amendments, cost and title, then the notices waiting for the owner. |
| `shed inbox [-peek]` | Lists contested units and the horizon changes on main since the last inbox. `-peek` records nothing. |
| `shed unit open <title>` | Makes a jj change for the unit on top of main and opens the unit in `proposed`. |
| `shed unit move <unit> <state> <reason>` | Moves a unit by hand to `implementing`, `verifying` or `queued`, or from `contested` back to `proposed`. |
| `shed unit reopen [-amendment] <unit> <reason>` | Sends a unit back to the shed. |
| `shed unit log <unit>` | Prints a unit's events. |
| `shed unit path <unit>` | Prints the directory of the unit's workspace. |
| `shed land <unit>` | Lands a queued unit on main and reports its footprint drift. See [version control](vcs.md). |
| `shed tracker rebuild` | Rebuilds the database from the event log. |
| `shed config` | Prints the operator settings in effect. |

A unit argument is any prefix of its change ID that names one unit.

Sealing, landing and archiving each produce a record of their own, so they
cannot be done by hand with `unit move`. `shed land` is the only way a unit
becomes landed.

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
