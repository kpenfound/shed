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

A near or soon clause may name the distant or eventual clause it refines with
a `refines` tag:

```markdown
- **H.greet.3** (distant) The tool speaks every language.
- **H.greet.4** (soon, refines H.greet.3) The tool greets in French.
```

`shed check` refuses a `refines` tag on a distant or eventual clause, a second
`refines` tag, and one naming anything other than a distant or eventual clause
in the horizon. `shed frame <clause>` has a frame builder draft refining
clauses for a distant or eventual clause.

In a git repository, a new near or soon clause must carry a `refines` tag.
`shed check` refuses a working tree that leaves a near or soon clause without
one when, on HEAD, that clause was absent, was distant or eventual, or carried
a `refines` tag. A near or soon clause that HEAD already holds at near or soon
without a `refines` tag may keep lacking one, however else it changes.

`shed trace` ends with a line naming the near and soon clauses, realised or
not, that still carry no `refines` tag, in document order and with their
count:

```text
no parent (2): H.greet.1, H.greet.2
```

It prints no such line once every near and soon clause carries one.

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

## Sweeping main

`shed sweep` checks whether main still does what its own spec says. It
brings in main, checks its current commit out into a fresh directory outside
the repository's working copy and every unit's workspace, and runs the
proofs of every spec clause in that commit's documents there, exactly as
`shed prove` with no clauses named does, using the runner that commit's
`shed.toml` sets. It prints pass or fail per clause, then the failing
clauses again, one per line, and exits non-zero when any clause fails:

```
pass  S.greet.1
fail  S.greet.2  TestBye: fail
S.greet.2
```

A clause with no proof fails and prints `no proof` instead of a test name.
The directory is removed before `shed sweep` exits, and it leaves main,
every unit and the working copy exactly as it found them, apart from the
recovery any shed process runs when it opens the repository and the
tracker. It calls no model. A sweep whose proofs ran records the commit it
checked out, when it started and pass or fail per clause in the tracker and
the event log, naming no unit; the same recording files a bug for each
clause newly failing and closes the bug of each clause found passing, and
`shed tracker rebuild` gives the same sweeps and bugs back (see
[sweeps](tracker.md#sweeps) and [bugs](tracker.md#bugs)). A sweep that
cannot bring in or check out main records nothing and exits non-zero with a
reason on stderr.

## Coverage

`shed uncovered` runs every proof the repository holds once, instrumented
for statement coverage over the whole Go module at the repository root,
using the runner `shed.toml` sets (see [proofs](#proofs), above). A
statement counts as executed when any proof executes it, whichever package
holds that proof; a package no proof reaches counts as entirely unexecuted.

It prints one line for each non-test Go source file that the go tool, run
behind that same runner, builds into a package of the module, skipping the
directories the go tool skips, nested modules, and any file a build
constraint excludes from that build. Each line holds the file's path from
the repository root, its unexecuted statements, its total statements, and
the unexecuted share as a whole percentage, 0% for a file with no
statements. Lines are ordered by unexecuted statements, most first, then by
path, and a last line gives the same three figures for the whole module:

```
lib/lib.go      1  2  50%
types/types.go  0  0  0%
total           1  2  50%
```

It exits zero once every proof has run, whether it passes, fails or skips; a
failing proof still counts the statements it executed. It exits non-zero,
saying why, when it cannot run the proofs, such as when the runner cannot
start or the run yields no coverage profile, and when any proof never
reports, such as because its package does not build, naming each such
proof, even if other proofs ran and yielded coverage. It calls no model and
leaves the repository's files, the state directory and the tracker exactly
as it found them.

## Commands

| Command | Does |
| --- | --- |
| `shed check` | Validates documents, IDs, history, citations and proofs. Exits non-zero on any problem. |
| `shed show <citation>...` | Prints the clauses the citations name. |
| `shed diff <from> [<to>]` | Lists spec and horizon clauses added, removed or changed between revisions, or between a revision and the working tree, the parent each refining clause is judged at, and the tier of the horizon amendment. |
| `shed trace` | Lists every horizon clause with its tier, whether it is realised, the spec clauses advancing it, and the clause it refines or the clauses refining it, then the near and soon clauses that refine nothing. |
| `shed gap` | Lists the horizon clauses not yet realised, with their tier, the spec clauses advancing them, and the ID and tier of the clause each refines. |
| `shed prove [<id>...]` | Runs proofs and reports per clause. |
| `shed sweep` | Checks main's current commit out into a fresh directory, runs its proofs and reports per clause, and records the sweep, filing or closing bugs as it does. |
| `shed uncovered` | Runs every proof once, measuring statement coverage over the whole module, and reports each source file's share of unexecuted statements, most first, then the module's totals. |

## Diffs

`shed diff` lists the spec clauses first, then the horizon clauses, each
horizon clause with its tier. A removed clause takes its tier on the first
revision, an added clause its tier on the second, and a changed clause the
higher of the two. A clause changes when its text or its tag list changes.
Rewrapping or reindenting does not count.

When it lists any horizon clause, the diff ends with the tier of the horizon
amendment: the highest tier among the clauses it counts.

```
changed S.core.1
added   H.greet.9 eventual
removed H.greet.4 near
changed H.greet.7 soon
tier    eventual
```

Two rules adjust the count:

- A changed clause whose only change is gaining `realised` is listed but
  not counted. A diff that only marks clauses realised gives no tier.
  Losing `realised` counts like any other change.
- A clause whose `refines` tag is added, removed or retargeted, including an
  added clause that carries one and a removed clause that carried one, also
  counts at the tier of the clause its tag names on each revision where it
  carries the tag. Adding a soon clause that refines a distant clause makes
  a distant amendment.

A clause that counts at another clause's tier because of its `refines` tag
names that clause on its line, with the tier it has on the revision whose tag
names it. A retargeted tag names the old parent first, then the new one:

```
added   H.greet.6 near parent H.greet.3 (distant)
changed H.greet.5 near parent H.greet.3 (distant), H.greet.4 (eventual)
tier    eventual
```

A clause that counts only at its own tier, or not at all, names no parent.
