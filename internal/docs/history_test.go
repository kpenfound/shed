package docs

import (
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/testrepo"
)

func history(t *testing.T, r *testrepo.Repo) []clause.Problem {
	t.Helper()
	s, _ := Load(revision.Worktree(r.Dir))
	problems, err := History(r.Dir, s)
	if err != nil {
		t.Fatal(err)
	}
	return problems
}

//shed:proves S.doc.6
func TestRetiredIDsAreNeverReused(t *testing.T) {
	r := testrepo.Minimal(t)
	if got := history(t, r); len(got) != 0 {
		t.Fatalf("outside git: %s", messages(got))
	}
	r.Init()
	r.Commit("documents")
	r.Write("charter.md", "# Charter\n\n- **C1** The tool greets people.\n")
	retiredAt := r.Commit("retire C2")[:12]
	if got := history(t, r); len(got) != 0 {
		t.Fatalf("after retiring: %s", messages(got))
	}

	r.Write("charter.md", "# Charter\n\n- **C1** The tool greets people.\n- **C2** A different rule.\n")
	wantProblems(t, history(t, r), "charter.md:4: C2 reuses an ID retired in "+retiredAt)

	r.Commit("reuse C2")
	wantProblems(t, history(t, r), "charter.md:4: C2 reuses an ID retired in "+retiredAt)
}

//shed:proves S.doc.7
func TestClausesAreNeverRenumbered(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Init()
	r.Commit("documents")

	r.Write("charter.md", "# Charter\n\n- **C2** The tool greets people.\n- **C3** It never shouts.\n")
	got := history(t, r)
	wantProblems(t, got,
		"charter.md:3: C2 takes the text of C1; IDs are never renumbered",
		"charter.md:4: C3 takes the text of C2; IDs are never renumbered",
	)

	// Copying a clause's text while the original keeps it is not a move.
	r.Write("charter.md", testrepo.Charter+"- **C3** It never shouts.\n")
	if got := history(t, r); len(got) != 0 {
		t.Errorf("copy: %s", messages(got))
	}
}

//shed:proves S.diff.2
func TestSpecDiff(t *testing.T) {
	old := testrepo.Minimal(t)
	old.Write("spec/core.md", `- **S.core.1** (H.greet.1) Running the tool prints hello.
- **S.core.2** (H.greet.1) Removed later.
- **S.core.3** (H.greet.1) Text changes.
- **S.core.4** (H.greet.1) Advances change.
- **S.core.5** (H.greet.1) Rewrapped only.
`)
	cur := testrepo.Minimal(t)
	cur.Write("spec/core.md", `- **S.core.1** (H.greet.1) Running the tool prints hello.
- **S.core.3** (H.greet.1) Text changed.
- **S.core.4** (H.greet.1, H.greet.2) Advances change.
- **S.core.5** (H.greet.1)   Rewrapped
  only.
- **S.core.6** (H.greet.2) Added.
`)
	from, _ := Load(revision.Worktree(old.Dir))
	to, _ := Load(revision.Worktree(cur.Dir))
	d := DiffSpec(from, to)
	ids := func(ids []clause.ID) string { return JoinIDs(ids) }
	if got := ids(d.Added); got != "S.core.6" {
		t.Errorf("added = %s", got)
	}
	if got := ids(d.Removed); got != "S.core.2" {
		t.Errorf("removed = %s", got)
	}
	if got := ids(d.Changed); got != "S.core.3, S.core.4" {
		t.Errorf("changed = %s", got)
	}
	if !DiffSpec(to, to).Empty() {
		t.Error("a set differs from itself")
	}
}

//shed:proves S.horizon.11
func TestNewNearAndSoonClausesNameTheirParent(t *testing.T) {
	r := testrepo.Minimal(t)
	head := `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone.
- **H.greet.5** (soon, refines H.greet.3) The tool greets in French.
- **H.greet.6** (distant) The tool greets in song.
`
	r.Write("horizon.md", head)
	work := `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near) The tool says goodbye politely.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone.
- **H.greet.5** (soon) The tool greets in French.
- **H.greet.6** (soon) The tool greets in song.
- **H.greet.7** (near) The tool greets by name.
- **H.greet.8** (soon, refines H.greet.4) The tool greets crowds.
- **H.greet.9** (distant) The tool greets in writing.
`
	// Outside git there is no HEAD to compare against.
	r.Write("horizon.md", work)
	if got := history(t, r); len(got) != 0 {
		t.Fatalf("outside git: %s", messages(got))
	}

	r.Write("horizon.md", head)
	r.Init()
	r.Commit("documents")
	if got := history(t, r); len(got) != 0 {
		t.Fatalf("at HEAD: %s", messages(got))
	}

	// H.greet.2 was soon without a refines tag at HEAD, so it may keep
	// lacking one even as its tier and text change. H.greet.5 dropped its
	// tag, H.greet.6 left the distant tier and H.greet.7 is new.
	r.Write("horizon.md", work)
	wantProblems(t, history(t, r),
		"horizon.md:7: H.greet.5 is soon and names no distant or eventual clause it refines",
		"horizon.md:8: H.greet.6 is soon and names no distant or eventual clause it refines",
		"horizon.md:9: H.greet.7 is near and names no distant or eventual clause it refines",
	)

	// Once HEAD holds them at near or soon without a tag, they may keep
	// lacking one.
	r.Commit("untagged clauses")
	if got := history(t, r); len(got) != 0 {
		t.Errorf("after commit: %s", messages(got))
	}
}
