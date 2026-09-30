# Sessions

Every role runs as an ephemeral agent session: one headless agent process,
started through busybees/core, that works on one unit for one step and ends.

## The sandbox

Each session runs in a [Docker Sandbox](https://docs.docker.com/ai/sandboxes/)
microVM, created by the `sbx` CLI for the session and removed when it ends.
The sandbox sees only what the session is granted:

- its working directory: a plain copy of the unit's files made for the
  session. It is read-write, since sbx needs a writable primary workspace,
  but shed captures it onto the unit only for the painter, mechanic,
  wheelbuilder and frame builder; what a committee member writes there is thrown away;
- its session directory, where shed writes `bundle.md` and core writes the
  transcript and result;
- any extra mounts the operator adds to the role's profile.

Sessions are never granted version control. The working directory has no
`.git` or `.jj`, git and jj are denied, and no VCS credentials or
configuration reach the sandbox. The agent's own credential never enters the
sandbox either: sbx's proxy injects the one stored with `sbx secret set`.
Before running shed's sessions, store a credential with `sbx secret set` for
each agent the operator settings use.

## Tools

A session reaches shed's tools over MCP. Shed serves them on a loopback
port with a token of the session's own, and the sandbox reaches the port
through `host.docker.internal`. The sbx network policy denies that by
default: shed grants the port to the session's sandbox, and busybees/core
adds a rule for that sandbox alone once it exists and removes it before the
sandbox goes. No global `localhost` rule is needed, and no other sandbox can
reach the port. Every role has `done`, which ends the session
with an outcome valid for its role and step. Roles that implement or verify
also get:

| Tool | Does |
| --- | --- |
| `run_tests` | Runs Go tests in the session's directory, optionally limited to packages and a `-run` pattern, and reports each test with the output of those that failed. |
| `prove` | Runs the proofs of spec clauses in the session's directory and reports pass or fail per clause. |

Shed runs these itself, through the runner `shed.toml` configures in the
repository, so tests run in Dagger as they do everywhere else and the
sandbox needs no container engine. A runner in the session's own directory is
never used.

## Bundles and prompts

Committee debate bundles isolate history and omit shared summaries as
[Independent committee review](autopilot.md#independent-committee-review) describes.
For other sessions, shed writes its bundle: the unit, the charter, the
footprint, the spec changes, the sealed spec, the horizon clauses advanced,
the proofs of the footprint's clauses, the debate record, the owner's
answers to the unit, oldest first, and the notices pending for the role or
for whichever session on the unit comes next, such as a notice that a
landing changed a horizon clause the unit advances. A
mechanic's bundle after a reseal out of the amendment lane also gives the
amendment's diff, or, when the amendment was rejected, says the sealed spec
stands as written and gives the objections that stood at the cap. A
session's bundle names each file holding an unresolved conflict. When its
work is kept, it says to resolve those conflicts first: against the sealed
spec for a unit past its seal, or against the proposed spec otherwise,
keeping main's spec and horizon text and re-applying the unit's changes.

The frame builder works on no unit: its bundle holds the charter, the
horizon, the clause to frame with the clauses that refine it and the spec
clauses that advance it, and the units in flight with their footprints.
Notices count as delivered once a session starts, except in a wheelbuilder's
horizon review, whose bundle shows them without delivering them. Bundles
come from a context provider; the default one uses only the documents, the
tracker and the debate record.

Each role has a system prompt: a part every role shares, then its own. Write
a file of the same name under `prompts/` in the state directory to replace
one: `common.md`, `painter.md`, `painter-reply.md`, `committee.md`,
`committee-review.md`, `mechanic.md`, `wheelbuilder.md`,
`wheelbuilder-review.md` or `frame-builder.md`.

## Failures and cost

Every session runs under `budget.per_session_usd`. A session that reaches
the cap, cannot start, hits a rate limit or times out is an infrastructure
failure: shed retries it as a new session on the next profile along the
role's fallback chain. A session that ends without calling `done` is a
behavioural failure and is not retried. Every attempt is a session in the
tracker, with its outcome and cost.

## Profiles

```toml
[profiles.default]
agent = "claude"          # claude, codex, opencode or pi
model = "claude-opus-5-5"
fallback = "backup"       # another profile, tried after an infrastructure failure
template = ""             # sbx template; empty uses sbx's own for the agent
mounts = ["~/go/pkg/mod:ro"]
env = ["GOFLAGS"]
```
