# Clauses, citations and proofs

Shed's documents are Markdown files made of clauses. Everything shed does
with them, from checking citations to diffing the spec, is a set operation
on clause IDs. No model is involved.

## Where the documents live

| Document | Path | IDs |
| --- | --- | --- |
| Charter | `charter.md` | `C<n>` |
| Spec | every `.md` file under `spec/` | `S.<area>.<n>` |
| Horizon | `horizon.md` | `H.<area>.<n>`, and milestones `M<n>` |

Paths are relative to the repository root, which is the current directory or
the directory given with `shed -C <dir>`.

## Writing a clause

A clause is a list item that opens with its ID in bold:

```markdown
- **C2** The tool never shouts.
```

The clause's text is the rest of the list item, nested content included.
Shed collapses whitespace, so rewrapping a clause does not change it. A list
item that opens with some other bold text, such as `**Note.**`, is ordinary
prose. A bold token that looks like an ID, a capital letter followed by a
digit or a dot, must be a valid one.

An area is lowercase letters, digits and hyphens, starting with a letter.
Numbers start at 1 and have no leading zeros. IDs are permanent. When you
remove a clause its ID is retired, and `shed check` refuses to let it come
back. It also refuses a change that moves a clause's text to a different ID.

### Horizon clauses

A horizon clause opens with a tag list holding its tier (`near`, `soon`,
`distant` or `eventual`). Once the spec fully satisfies the clause, add
`realised` and keep the clause as history:

```markdown
- **H.greet.2** (soon, realised) The tool says goodbye.
```

A realised clause needs at least one spec clause that advances it. Milestones
carry no tier.

### Spec clauses

A spec clause opens with the horizon clauses it advances:

```markdown
- **S.greet.4** (H.greet.2) Running the tool with `--bye` prints goodbye.
```

## Citations

Any clause ID in the prose of the three documents is a citation, and it must
resolve. Add `@<revision>` to cite a clause as it was at a git revision, as
in `C2@charter/v1`. Two IDs joined by "to", as in `H.greet.1 to H.greet.4`,
cite a range. Code spans and code blocks are not checked, which is why the
examples on this page are safe.

`shed show <citation>...` prints the clauses citations name.

## Proofs

Every spec clause needs at least one proof. A proof is a Go test with a
`//shed:proves` directive in its doc comment:

```go
//shed:proves S.greet.4 S.greet.5
func TestBye(t *testing.T) { ... }
```

Shed looks for proofs in the Go module at the repository root. It skips the
directories the go tool skips and any nested module.

`shed prove [<id>...]` runs the proofs with `go test -json` and prints pass
or fail per clause. A clause passes only when every proof of it passes. A
skipped proof, or one whose package does not build, fails the clause.

To run proofs somewhere other than the host, set a runner in `shed.toml`.
Shed puts it in front of the `go test` command line:

```toml
[proofs]
runner = ["scripts/in-dagger"]
```

## Commands

| Command | Does |
| --- | --- |
| `shed check` | Validates documents, IDs, history, citations and proofs. Exits non-zero on any problem. |
| `shed show <citation>...` | Prints the clauses the citations name. |
| `shed diff <from> [<to>]` | Lists spec clauses added, removed or changed between revisions, or between a revision and the working tree. |
| `shed trace` | Lists every horizon clause with its tier, whether it is realised, and the spec clauses advancing it. |
| `shed gap` | Lists the horizon clauses not yet realised. |
| `shed prove [<id>...]` | Runs proofs and reports per clause. |
