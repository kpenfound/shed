# Context records

- **S.ctx.1** (H.ctx.4) `shed records` prints the event log of the state
  directory (S.track.1) as L0 records, one JSON object per line, in the
  order of the events' sequence numbers. Each record has exactly these
  keys, in this order: `id`, the text `shed/event/` followed by the event's
  sequence number; `time`, the event's time in RFC 3339 UTC; `kind`, the
  record kind of S.ctx.2; `topic`, the full change ID of the event's unit;
  `actor`, the event's actor; `cites`, a list of clause IDs, empty when the
  record cites none; and `text`. It reads the log as S.track.6 does,
  ignoring a last line cut short. An empty or missing log gives no event
  record; a log it cannot read fails with a message saying why and prints
  no record. The same log always gives the same event records.
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
- **S.ctx.3** (H.ctx.5) After the event records, `shed records` prints one
  record for each clause that each commit on main's first-parent history
  adds, removes or changes in the charter, the spec or the horizon. Commits
  come oldest first. Each commit is compared, document by document, with
  that document at the latest earlier commit on that history at which it
  had no problem S.doc.5 names, which is normally the commit's first
  parent, or with none of its clauses when no earlier commit qualifies, as
  for the first commit. A document that has such a problem at a commit
  gives no record at that commit. A commit that lacks a document holds none
  of its clauses. Within a commit, the charter's clauses come first, then
  the spec's, then the horizon's, each in ascending order of clause ID: by
  area as text and then by number as a number, with IDs that have no area,
  such as `M<n>`, after those that do. Spec and horizon clauses count as
  added, removed or changed as `shed diff` counts them (S.diff.1 to
  S.diff.3), and a charter clause changes when its text (S.doc.2) changes.
- **S.ctx.4** (H.ctx.5) A clause record has exactly these keys, in this
  order: `id`, the text `shed/clause/` followed by the commit's full hash, a
  slash and the clause ID; `time`, the commit's committer time in RFC 3339
  UTC; `kind`, one of `clause.added`, `clause.removed` and
  `clause.changed`; `topic`, the full change ID of the unit the commit
  landed, when it landed as a shed unit as S.outside.1 defines, and
  otherwise the empty text; `actor`, the commit's author name; `cites`, a
  list holding only the clause ID; `document`, one of `charter`, `spec` and
  `horizon`; `commit`, the commit's full hash; `before`, the clause's text
  (S.doc.2) at the commit S.ctx.3 compares the clause's document with,
  empty for an added clause; and `after`, its text at the commit, empty for
  a removed clause.
- **S.ctx.5** (H.ctx.4, H.ctx.5) `shed records` makes no network access.
  Before it prints any record, it reads the event log, opens the repository
  and the tracker, brings in main as S.vcs.9 says, and reads main's
  first-parent history and every document version S.ctx.3 compares. When
  any of these fails it fails with a message saying why and prints no
  record. Beyond bringing in main and the recovery every shed process does
  on opening the repository and the tracker (S.vcs.5, S.vcs.11, S.track.8),
  it moves no unit, records nothing in the tracker, and leaves main and the
  remote as they were. The same event log, tracker and main history always
  print the same bytes.
