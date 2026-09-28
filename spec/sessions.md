# Sessions

- **S.sess.1** (H.sess.1) Shed runs each session as one headless agent
  process through busybees/core, with the agent, model and fallback the
  operator settings give the role's profile: claude, codex, opencode or pi.
  The session ends when the agent does.
- **S.sess.2** (H.sess.2) Each role has its own system prompt: a part every
  role shares, then the role's own. Shed ships them, and a file of the same
  name in the state directory's `prompts` replaces one.
- **S.sess.3** (H.sess.2, H.vcs.2) Every session runs in a Docker Sandbox
  microVM that sees only what it is granted: its working directory,
  read-write for roles that change files and read-only for reviewers, its
  session directory, and the profile's extra mounts. It inherits only the
  listed environment variables and is never granted version control: git and
  jj are denied and no VCS credentials or metadata reach it.
- **S.sess.4** (H.sess.3) A session reaches shed's tools over MCP, on a
  loopback endpoint with a token of its own. Shed grants that endpoint's port
  to the session's sandbox alone, so the sbx network policy lets no other
  sandbox reach it. The session reports through a `done` tool that accepts
  only the outcomes valid for its role and step.
- **S.sess.5** (H.sess.4) Before every session shed writes its bundle to
  `bundle.md` in the session directory. The bundle holds the unit, the
  charter, the footprint, the spec changes, the sealed spec, the horizon
  clauses advanced, the proofs of the footprint's clauses, the debate record
  and the pending notices.
- **S.sess.6** (H.sess.5) The notices a bundle carries are delivered when the
  session starts. Notices added later wait for the next session.
- **S.sess.7** (H.sess.6) Bundles come from a context provider. The default
  provider uses only the documents, the tracker and the debate record, and
  gives the sweeper no debate record.
- **S.sess.8** (H.sess.7) Each session runs under the operator's per-session
  cost cap. A session that reaches it ends and counts as an infrastructure
  failure.
- **S.sess.9** (H.sess.8) A session that fails for infrastructure reasons is
  retried as a new session on the next profile along the role's fallback
  chain. A session that ends without reporting an outcome is a behavioural
  failure and is not retried. Every attempt is recorded in the tracker with
  its outcome and cost.
- **S.sess.10** (H.sess.9) Sessions that implement or verify get `run_tests`
  and `prove` tools. Shed runs them itself, in the session's directory,
  through the runner `shed.toml` configures in the repository, never one from
  the session's directory. They report each test or clause, with the output
  of failures.
