# Working on shed

## Documents

- [docs/design.md](docs/design.md) is the design. [charter.md](charter.md) is
  the law, and only the owner changes it. [spec/](spec/) describes what main
  does. [horizon.md](horizon.md) describes where shed is going.
- A change is a spec diff plus the proofs and code that satisfy it, landed
  together. Do not change the spec without the code, or the code without the
  spec. Every spec clause names the horizon clauses it advances and has a
  proof: a test carrying `//shed:proves <id>...`. See
  [docs/clauses.md](docs/clauses.md).

## Testing

- Test with Dagger. Never run `go test` on the host, for any reason, including
  a single test or a flaky one. `gofmt`, `go build` and `go vet` are fine on
  the host.
- Run the full gate before declaring a change complete. It uses the pinned
  experimental Dagger release:

  ```sh
  DAGGER_X_RELEASE=v1.0.0-beta.15 dagger check
  ```

- `go run ./cmd/shed check` validates the documents and the proof mapping.
  `go run ./cmd/shed prove [<id>...]` runs proofs through
  [scripts/in-dagger](scripts/in-dagger), as `shed.toml` configures.
- Run one package or one test inside a Dagger container:

  ```sh
  dagger core container from --address golang:1.26-trixie \
    with-exec --args=sh,-c,'curl -fsSL https://github.com/jj-vcs/jj/releases/download/v0.45.1/jj-v0.45.1-$(uname -m)-unknown-linux-musl.tar.gz | tar -xz -C /usr/local/bin ./jj' \
    with-env-variable --name=SHED_REQUIRE_JJ --value=1 \
    with-directory --path /src --source . --exclude .git,.jj,.shed \
    with-workdir --path /src \
    with-mounted-temp --path /tmp \
    with-exec --args=go,test,-count=1,-run,'TestA|TestB',-v,./internal/example \
    combined-output
  ```

  The first `with-exec` installs the pinned jj the version control tests
  need. Without `SHED_REQUIRE_JJ=1` those tests skip when jj is missing.

- Inside a shed session, run tests with the `run_tests` and `prove` tools;
  shed runs them in Dagger for you.
- Add integration tests as you build a feature, not afterwards. Use temporary
  directories and local repositories for filesystem and VCS tests, and fake
  agents for sessions.
- Tests cover the expected state of the app. When a feature changes or goes
  away, update or delete its tests. Do not add tests asserting that an old
  behaviour is absent.
- Report which checks you ran and what they returned.

## Documentation

- Write documentation as you build a feature, in the same change.
- Code, tests, comments and documentation refer to features. They never
  mention milestones, horizon clauses, tracking issues, pull requests or
  commits. No `M1`, "milestone 1", `H.doc.3` or "PR 100". Spec clause IDs
  such as `S.doc.2` are fine, because a spec clause is a feature.
- Comments and documentation describe the current state. Do not describe how
  something used to work.
