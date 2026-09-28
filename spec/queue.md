# Merge queue

- **S.queue.1** (H.queue.1) Shed lands queued units one at a time through
  `shed land`.
- **S.queue.2** (H.queue.2) Landing rebases a queued unit onto main first,
  keeping any conflicts in its files. A wheelbuilder session resolves them
  against the sealed spec in a writable copy. If the wheelbuilder reports
  `unresolvable`, or conflicts remain, the unit reopens. A unit that changes
  nothing once rebased reopens.
