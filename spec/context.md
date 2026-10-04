# Context records

- **S.ctx.1** (H.ctx.4) `shed records` prints the event log of the state
  directory (S.track.1) as L0 records, one JSON object per line, in the
  order of the events' sequence numbers. Each record has exactly these
  keys, in this order: `id`, the text `shed/event/` followed by the event's
  sequence number; `time`, the event's time in RFC 3339 UTC; `kind`, the
  record kind of S.ctx.2; `topic`, the full change ID of the event's unit;
  `actor`, the event's actor; `cites`, a list of clause IDs, empty when the
  record cites none; and `text`. It reads the log as S.track.6 does,
  ignoring a last line cut short, and nothing else: it makes no network
  access, opens no repository, records nothing in the tracker and changes
  no file. An empty or missing log prints nothing and exits zero; a log it
  cannot read fails with a message saying why and prints no record. The
  same log always prints the same bytes.
- **S.ctx.2** (H.ctx.4) `shed records` gives one record for each event of
  these kinds and none for any other event. A unit opening is kind
  `unit.opened`, its text the unit's title. A footprint declaration is
  `footprint.declared`, its text the reason the declaration records
  (S.track.3), citing the clauses it records as modified, then those
  depended on, then the horizon clauses advanced, each list in the order the
  event records it. An objection is `objection`, citing the clause IDs it
  cites (S.shed.2), its text the objection's text. An answer is `answer`,
  its text the answer's text. A withdrawal is `withdrawal`, its text the
  reason the withdrawal records. A move to sealed is `seal`, a reopen
  (S.unit.5) is `reopen`, a move to archived is `archive` and a move to
  landed is `landing`, each with the move's reason as its text. Only
  objection and footprint records cite clauses.
