# Shed

Shed is a software factory: AI agents that plan, debate, build, verify and
ship changes to a repository with little human involvement. It runs on a plain
git repository, with no issue tracker and no hosted platform, and it builds
itself.

You write down what the project is for and where it should go. Shed's agents
decide what to build next, argue about whether it is a good idea, write it,
prove it, review it and land it on main.

The name comes from Parkinson's law of triviality: a committee approves a
nuclear reactor in minutes and argues for an hour about the bike shed. Debate
used to be expensive and awkward to repeat. Between agents it is cheap, it can
happen at every stage, and reopening a settled decision costs nobody their
pride. So shed puts debate at its centre, and surrounds it with deterministic
code that makes sure it ends.

## How it works

### Three documents

Everything shed does starts from three Markdown documents committed next to
the code. Every clause in them has a stable ID, so agents cite clauses instead
of arguing from impressions.

| Document | Says | Changed by |
| --- | --- | --- |
| [`charter.md`](charter.md) | What the project is for and what it must never do. | The owner, and only the owner. |
| [`spec/`](spec/) | What the software on main does, clause by clause. Every clause has a proof: a test that shows it holds. | Landing a change, and nothing else. |
| [`horizon.md`](horizon.md) | Where the software is going, each clause tiered by its distance from the spec: near, soon, distant or eventual. | The owner, and units marking clauses realised. |

The backlog is the gap between the horizon and the spec. There is no issue
tracker: when the gap is empty, the factory has nothing to do.

```markdown
- **C2** The tool never shouts.                                          <- charter
- **H.greet.2** (soon) The tool says goodbye.                            <- horizon
- **S.greet.4** (H.greet.2) Running the tool with --bye prints goodbye.  <- spec
```

### Change units

The unit of work is a change unit: a spec diff, the proofs that demonstrate
it, and the code that satisfies it. A unit lives on its own jj change and
lands on main as one commit, never in pieces. Main therefore always does what
its spec says, and `git log main` reads as the spec's changelog.

```
proposed -> sealed -> implementing -> verifying -> queued -> landed
    ^________________________________________________|
               reopened, from any stage, with a reason
```

`proposed` is the shed, where debate happens, and every later stage can send
a unit back there. Acceptance is not a promise: implementation finds out
things the proposal could not, and the unit goes back to be argued again.
Every reopen counts a bounce. Too many bounces and the unit is contested and
waits for the owner, so debate always ends.

### Roles

Each role is an agent session, run by a small, deterministic Go scheduler.

| Role | Does |
| --- | --- |
| Painter | Reads the gap and proposes the next small increment as a spec diff. |
| Committee | Debates each proposal in parallel rounds, citing clause IDs. A charter objection is a veto: the proposal is rejected. A proposal that does not move toward the horizon is deferred. Later, a committee member who did not build the unit reviews its code. |
| Mechanic | Implements a sealed unit step by step: proofs first, then code, then docs. |
| Wheelbuilder | Lands units on main one at a time, resolving conflicts against the sealed spec. |
| Owner | You. You write the charter, steer the horizon and answer contested units. You never block the factory. |

Rejected and deferred proposals go to an archive, so the same idea is not
proposed twice without something having changed.

### The machine around the agents

- **Decisions are code.** States, transitions, footprints, seals, queue
  order, round caps and budgets are Go. No model decides where a unit goes
  next.
- **Agents never touch version control.** Shed performs every git and jj
  operation. Each session gets a plain copy of the unit's files in a Docker
  Sandbox microVM, with no `.git`, no `.jj` and no VCS credentials.
- **Tests run in Dagger.** Sessions run tests through tools shed serves, and
  shed runs them with the project's configured runner, not in the sandbox.
- **Everything is recorded.** An append-only event log is the source of
  truth, and a SQLite tracker is rebuilt from it. After a crash, shed carries
  on where the log left off.
- **Spending is bounded.** Each session has a cost cap, and a daily budget
  pauses the factory.

## Quickstart

### Requirements

- **git** 2.41 or later, and **jj** 0.45.x.
- **Docker Sandboxes**, the `sbx` CLI, with a credential for your agent stored
  with `sbx secret set`.
- **An agent**: Claude Code, Codex, OpenCode or pi. Claude is the default.
- **Dagger**, if your project runs its tests through it, as shed does.
- **A Go project.** Proofs are Go tests today.

### Install

Download a release for your platform:

```sh
version=v0.1.0
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -fsSL "https://github.com/kpenfound/shed/releases/download/${version}/shed_${version}_${os}_${arch}.tar.gz" | tar -xz shed
sudo mv shed /usr/local/bin/
shed version
```

Or build it with Go 1.25 or later:

```sh
go install github.com/kpenfound/shed/cmd/shed@latest
```

### Set up a repository

1. Write the three documents at the root of your repository. This
   repository's [`charter.md`](charter.md), [`horizon.md`](horizon.md) and
   [`spec/`](spec/) are working examples, and
   [the clause format](docs/clauses.md) explains the syntax. For a new
   project the spec is empty: a `spec/README.md` that says so.
2. Keep shed's state out of version control:

   ```sh
   echo ".shed/" >> .gitignore
   ```

3. Tell shed how to run your tests in `shed.toml`, unless plain `go test`
   will do. This repository runs them in Dagger:

   ```toml
   [proofs]
   runner = ["scripts/in-dagger"]
   ```

4. Colocate jj with git:

   ```sh
   jj git init --colocate
   ```

5. Set the operator settings in `.shed/config.toml`: at least the remote to
   push landings to, and a budget. `shed config` prints every setting in
   effect.

   ```toml
   [vcs]
   remote = "origin"

   [budget]
   per_session_usd = 5
   per_day_usd = 20
   ```

### Run it

Check that everything the factory needs is in place:

```sh
shed doctor
```

It checks the documents and proofs, the settings, the test runner, jj, the
repository, the remote and sbx, and says what to fix. Then run the whole
factory:

```sh
shed serve
```

The painter proposes against the near and soon clauses of the horizon, and
the rest follows: debate, implementation, verification, landing. Watch it
with `shed status`, and read each session's transcript under
`.shed/sessions/`. `shed serve -once` stops when nothing is left to start.

Or drive one unit yourself:

```sh
shed unit open "Say goodbye"
cd "$(shed unit path <unit>)"      # write the spec diff here
shed unit declare -depends S.greet.1 -advances H.greet.2 <unit>
shed run <unit>                    # debate, implement, verify, land
```

## Commands

| Command | Does |
| --- | --- |
| `shed check` | Validates the documents, IDs, citations and proofs. |
| `shed gap`, `shed trace` | List the horizon clauses not yet realised, or every horizon clause, with the spec clauses that advance each and its refinement links. |
| `shed prove [<id>...]` | Runs the proofs of spec clauses and reports per clause. |
| `shed show <citation>...` | Prints the clauses citations name, at any revision. |
| `shed diff <from> [<to>]` | Lists spec and horizon clauses added, changed and removed between revisions, and the horizon amendment's tier. |
| `shed status` | Lists units, what waits for the owner, and whether the factory is paused. |
| `shed inbox [-peek]` | Lists contested units, the horizon changes, the charter clauses that keep getting proposals rejected, and sampled horizon amendments, marking what is new since you last looked. |
| `shed answer <unit> retry\|defer\|reject\|approve <reason>` | Answers a contested unit: retry it in the shed, defer or reject it to the archive, or approve its distant, eventual or split soon horizon amendment. |
| `shed answer <clause> keep <reason>` | Answers a charter question by keeping the clause as it stands. |
| `shed unit open\|declare\|move\|reopen\|log\|path` | Drives units by hand. |
| `shed debate\|land\|run <unit>` | Runs one stage of a unit, or all of them. |
| `shed serve [-once]` | Runs the factory. |
| `shed config` | Prints the operator settings in effect. |
| `shed doctor` | Checks that everything running the factory needs is in place. |

## Status

Shed builds itself, one unit at a time. It checks and traces its documents,
tracks units through their states, keeps each unit on a jj change, runs
agents in Docker Sandboxes, debates, implements, verifies and lands units,
proposes from the gap, and serves all of it on its own under a budget.
`shed inbox` lists contested units, horizon changes, charter questions
from repeated rejections and sampled horizon amendments for the owner. A distant or eventual horizon amendment waits
for the owner, and so does a soon one the committee splits over. `shed answer` retries, defers, rejects or approves a contested
unit, and keeps a charter clause to answer its question.

Still to come, as the [horizon](horizon.md) describes: many units in flight at
once with reconciliation, a frame builder that sharpens the horizon, a sweeper that
patrols main for spec violations, `shed init`, and adopting existing
codebases. Until the frame builder exists, the owner moves
distant horizon clauses to soon by editing the horizon.

## Documentation

| Read | For |
| --- | --- |
| [Design](docs/design.md) | The full design and its reasoning. |
| [Clauses, citations and proofs](docs/clauses.md) | The document format. |
| [The life of a unit, and autopilot](docs/autopilot.md) | Debate, implementation, verification, landing and `shed serve`. |
| [Sessions](docs/sessions.md) | The sandbox, the tools and the bundles agents get. |
| [Units and the tracker](docs/tracker.md) | Unit states, the owner inbox, the state directory and operator settings. |
| [Version control](docs/vcs.md) | The jj repository, unit workspaces, landing, and rebasing units onto main. |
| [Releasing](docs/releasing.md) | Cutting a release. |
| [AGENTS.md](AGENTS.md) | Working on shed itself. |
