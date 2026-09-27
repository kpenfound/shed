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
