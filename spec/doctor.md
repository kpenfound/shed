# Doctor

- **S.doctor.1** (H.vcs.1, H.sess.1) `shed doctor` checks what running the
  factory needs, and changes nothing: the documents and proofs as
  `shed check` checks them, the operator and project settings, the test
  runner `shed.toml` names, the jj release, a jj repository colocated with
  git at the root with an ignored state directory and a main bookmark, the
  configured remote, and a working `sbx`. It prints each check as `ok` or
  `FAIL`, with what to do about a failure, and exits non-zero when any
  fails.
