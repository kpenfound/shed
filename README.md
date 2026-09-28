# Shed

Shed is a spec-driven software factory that runs on a plain git repository.
Agents propose changes to the spec, debate them, implement them and land them
whole. The [design](docs/design.md) describes where it is going.

Shed can build itself. A painter proposes spec changes from the gap between
the horizon and the spec, a committee debates them, a mechanic implements
them, a reviewer verifies them and a wheelbuilder lands them on main, each
role an agent session in a Docker Sandbox. The documents are:

- [charter.md](charter.md), the purpose and boundaries, which only the owner
  changes
- [spec/](spec/), what shed on main does, clause by clause, each with proofs
- [horizon.md](horizon.md), where shed is going, tiered by distance from the
  spec

```sh
go build -o shed ./cmd/shed
./shed check   # validate documents, citations and proofs
./shed gap     # what the spec has not realised yet
./shed status  # units in flight and what waits for the owner
./shed serve   # run the factory
```

[Clauses, citations and proofs](docs/clauses.md) covers the document format.
[Units and the tracker](docs/tracker.md) covers unit states, the state
directory and operator settings. [Version control](docs/vcs.md) covers the
jj repository, unit workspaces and landing. [Sessions](docs/sessions.md)
covers the sandbox, tools and bundles, and [the life of a unit and
autopilot](docs/autopilot.md) covers debate, implementation, verification,
landing and `shed serve`. [Releasing](docs/releasing.md)
covers tagging a release. [AGENTS.md](AGENTS.md) covers how to work on
shed.
