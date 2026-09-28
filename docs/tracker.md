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

Sealing records the seal, the main commit and the unit's change ID, together
with the unit's footprint. The footprint lists the spec clauses the unit
modifies, the spec clauses it depends on and the horizon clauses it
advances.

A unit may depend only on spec clauses that are on main or that it modifies
itself. Depending on a clause another in-flight unit is adding is refused.
Wait for that unit to land, or merge the two units.

## Commands

| Command | Does |
| --- | --- |
| `shed status` | Lists units in the order they opened, with state, bounces, amendments, cost and title, then the notices waiting for the owner. |
| `shed unit open <title>` | Makes a jj change for the unit on top of main and opens the unit in `proposed`. |
| `shed unit move <unit> <state> <reason>` | Moves a unit by hand to `implementing`, `verifying` or `queued`, or from `contested` back to `proposed`. |
| `shed unit reopen [-amendment] <unit> <reason>` | Sends a unit back to the shed. |
| `shed unit log <unit>` | Prints a unit's events. |
| `shed unit path <unit>` | Prints the directory of the unit's workspace. |
| `shed land <unit>` | Lands a queued unit on main. See [version control](vcs.md). |
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
