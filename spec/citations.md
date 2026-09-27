# Citations

- **S.cite.1** (H.doc.12) A citation is a clause ID, optionally followed by
  `@` and a git revision, such as `C3` or `S.doc.2@HEAD~1`. Without a
  revision it refers to the working tree.
- **S.cite.2** (H.doc.12) `shed show` prints the clause each citation names
  with its file and line, and exits non-zero if any citation does not
  resolve.
- **S.cite.3** (H.doc.12) Every ID-shaped token in the prose of the charter,
  spec and horizon is a citation, and shed refuses one that does not resolve.
  Code spans, code blocks and HTML are not prose.
- **S.cite.4** (H.doc.12) Two citations joined by "to" cite a range. Both
  ends must resolve, share a kind and area, and increase.
