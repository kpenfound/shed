# Init

- **S.init.1** (H.init.9) `shed init <design-document>` checks that the
  repository root (S.doc.1) and the design document qualify for initialising.
  It uses S.doc.1 only to choose the root: it neither requires nor reads a
  charter, spec or horizon, and their absence is no problem for it. The root
  must be the top of a git working tree; unlike S.vcs.1, no jj repository is
  needed. A relative document path is resolved against the directory shed
  runs in, not the root named with `-C`. S.init.2 compares that path, with
  every symbolic link in it resolved, to each working tree path joined to the
  root, with every symbolic link in the root resolved, so a root reached
  through a symbolic link, by `-C` or otherwise, still matches the document
  inside it. It refuses
  a missing argument or more than one argument, and a document path that does
  not exist, is not a regular file, is empty or is not valid UTF-8 text,
  naming the path. It runs every check that its arguments allow, prints each
  one that fails, and exits non-zero when any fails. When every check passes
  it prints that the repository and document qualify and exits zero. Either
  way it changes nothing in the working tree, git's commits, branches, index
  or configuration, or the state directory, and creates no state directory.
- **S.init.2** (H.init.10) One check of S.init.1 is that the repository is
  nearly empty: every file in its working tree that git does not ignore,
  tracked or not, is at the root and named `README` or `LICENSE`, with or
  without an extension, `.gitignore` or `.gitattributes`, or is the design
  document itself. A repository with no commits passes this check when its
  files do. When the check fails, shed lists each other path, relative to
  the root, in path name order, and says that an existing codebase is adopted
  rather than initialised.
