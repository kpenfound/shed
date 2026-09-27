# Documents

- **S.doc.1** (H.doc.1) Shed treats the directory it runs in, or the one named
  with `-C`, as the repository root. It reads the charter from `charter.md`,
  the spec from every Markdown file under `spec/` and the horizon from
  `horizon.md`. A missing charter, spec directory or horizon is a problem.
- **S.doc.2** (H.doc.2) A clause is a Markdown list item whose first paragraph
  opens with its ID in bold. The clause's text is the rest of that list item,
  nested content included, with whitespace collapsed.
- **S.doc.3** (H.doc.2) A list item that opens with a bold token shaped like
  an ID, a capital letter followed by a digit or a dot, must hold a valid ID.
  Any other list item is prose.
- **S.doc.4** (H.doc.3) Charter IDs are `C<n>` and spec IDs are
  `S.<area>.<n>`. Horizon IDs are `H.<area>.<n>`, and the horizon also holds
  milestone IDs `M<n>`. An area is lowercase letters, digits and hyphens,
  starting with a letter. Numbers start at 1 and have no leading zeros.
- **S.doc.5** (H.doc.5) Shed refuses a malformed ID, an ID in the wrong
  document, a duplicate ID and a clause nested inside another clause, naming
  the file and line. Spec IDs are unique across all spec files.
- **S.doc.6** (H.doc.4, H.doc.5) In a git repository, shed refuses an ID that
  an earlier commit removed and a later commit or the working tree brings
  back.
- **S.doc.7** (H.doc.4) Shed refuses a working tree change that moves a
  clause's text from one ID to another.
