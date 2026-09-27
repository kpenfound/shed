# Shed

Shed is a spec-driven software factory that runs on a plain git repository.
Agents propose changes to the spec, debate them, implement them and land them
whole. The [design](docs/design.md) describes where it is going.

Shed is under construction. Today it checks its three documents and tracks
change units through their states. The documents are:

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
```

[Clauses, citations and proofs](docs/clauses.md) covers the document format.
[Units and the tracker](docs/tracker.md) covers unit states, the state
directory and operator settings. [AGENTS.md](AGENTS.md) covers how to work
on shed.
