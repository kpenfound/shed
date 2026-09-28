# Tracker

- **S.track.1** (H.track.1) Shed keeps its workflow state in a state
  directory: `.shed` under the repository root, unless `-state` or
  `$SHED_STATE` names another. The directory holds the SQLite database
  `tracker.db`, the event log `events.jsonl` and a `sessions` directory.
- **S.track.2** (H.track.1) The tracker holds each unit's title, state,
  bounce count, amendment count, latest reason, shelf, seal, footprint,
  finished steps, sessions and notices.
- **S.track.3** (H.track.2) Every change to the tracker is appended to the
  event log as one JSON line before it reaches the database. Each line names
  its sequence number, time, kind, unit, actor and reason, the states before
  and after a move, and the cost of a finished session.
- **S.track.4** (H.track.1) One shed process changes the tracker at a time.
  A change waits for the state directory's lock, and sequence numbers never
  repeat.
- **S.track.5** (H.track.3) `shed tracker rebuild` empties the database and
  replays the event log, giving back the same units, seals, footprints,
  counters, sessions and notices.
- **S.track.6** (H.track.3, H.track.5) Opening the tracker applies any logged
  events the database lacks. A last line cut short by a crash is ignored, and
  the next change overwrites it.
- **S.track.7** (H.track.4) Starting a session records its unit, role, step
  and process ID, and creates `sessions/<id>` to hold its `bundle.md`,
  `transcript.jsonl`, `outcome.json` and `result.json`.
- **S.track.8** (H.track.5) Opening the tracker marks each running session
  whose process has exited as interrupted, without the owner. A unit keeps
  the steps its sessions finished, in order, so its work can resume after the
  last one. Reopening a unit, or sending it back from verifying to
  implementing, clears them, and its work starts over.
- **S.track.9** (H.track.7) `shed status` lists every unit in the order they
  opened, with its short change ID, state, bounce count, amendment count, cost
  so far and title. After the units it lists the notices waiting for the
  owner.
- **S.track.10** (H.track.2) `shed unit log <unit>` prints a unit's events,
  oldest first, with sequence number, time, actor and what happened.
