package docs

import (
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/testrepo"
)

func messages(problems []clause.Problem) string {
	var out []string
	for _, p := range problems {
		out = append(out, p.String())
	}
	return strings.Join(out, "\n")
}

func load(t *testing.T, r *testrepo.Repo) (*Set, []clause.Problem) {
	t.Helper()
	s, problems := Load(revision.Worktree(r.Dir))
	return s, append(problems, Validate(s, NewResolver(r.Dir, s))...)
}

func wantProblems(t *testing.T, problems []clause.Problem, want ...string) {
	t.Helper()
	got := messages(problems)
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("problems lack %q; got:\n%s", w, got)
		}
	}
	if len(problems) != len(want) {
		t.Errorf("got %d problems, want %d:\n%s", len(problems), len(want), got)
	}
}

//shed:proves S.doc.1
func TestLoadReadsTheThreeDocuments(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/more/extra.md", "- **S.extra.1** (H.greet.2) Nested spec file.\n")
	r.Write("spec/notes.txt", "- **S.ignored.1** (H.greet.2) Not Markdown.\n")
	s, problems := load(t, r)
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	if got := len(s.Clauses(clause.Charter)); got != 2 {
		t.Errorf("charter clauses = %d, want 2", got)
	}
	var spec []string
	for _, c := range s.Clauses(clause.Spec) {
		spec = append(spec, c.ID.String()+"@"+c.File)
	}
	if want := []string{"S.core.1@spec/core.md", "S.extra.1@spec/more/extra.md"}; !slices.Equal(spec, want) {
		t.Errorf("spec clauses = %v, want %v", spec, want)
	}
	if got := len(s.Clauses(clause.Horizon)); got != 3 {
		t.Errorf("horizon clauses = %d, want 3", got)
	}

	empty := testrepo.New(t)
	_, problems = Load(revision.Worktree(empty.Dir))
	wantProblems(t, problems, "charter.md: missing", "spec/: missing", "horizon.md: missing")
}

//shed:proves S.doc.5
func TestDuplicateIDsAcrossSpecFiles(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/other.md", "# Other\n\n- **S.core.1** (H.greet.2) Same ID again.\n")
	_, problems := load(t, r)
	wantProblems(t, problems, "spec/other.md:3: duplicate ID S.core.1, first used at spec/core.md:3")
}

//shed:proves S.horizon.1
func TestHorizonTiers(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Fine.
- **H.greet.2** No tier.
- **H.greet.3** (soon, distant) Two tiers.
- **H.greet.4** (later) Unknown tag.
- **H.greet.5** (realised) Realised without a tier.
- **H.greet.6** (near, refines H.greet.7) A refines tag is allowed.
- **H.greet.7** (eventual) Refined.
`)
	_, problems := load(t, r)
	wantProblems(t, problems,
		"horizon.md:2: H.greet.2 has no tier",
		"horizon.md:3: H.greet.3 must have exactly one tier, has 2",
		`horizon.md:4: H.greet.4 has unknown tag "later"`,
		"horizon.md:4: H.greet.4 must have exactly one tier, has 0",
		"horizon.md:5: H.greet.5 must have exactly one tier, has 0",
		"horizon.md:5: H.greet.5 is marked realised but no spec clause advances it",
	)
}

//shed:proves S.horizon.6
func TestRefinesTag(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Fine.
- **H.greet.2** (soon, refines H.greet.3) Refines a distant clause.
- **H.greet.3** (distant) Parent.
- **H.greet.4** (eventual) Parent.
- **H.greet.5** (near, refines H.greet.4) Refines an eventual clause.
- **H.greet.6** (distant, refines H.greet.4) Distant refines.
- **H.greet.7** (eventual, refines H.greet.3) Eventual refines.
- **H.greet.8** (soon, refines H.greet.3, refines H.greet.4) Two refines tags.
- **H.greet.9** (soon, refines H.greet.1) Refines a soon clause.
- **H.greet.10** (soon, refines H.greet.99) Refines a missing clause.
- **H.greet.11** (soon, refines S.core.1) Refines a spec clause.
- **H.greet.12** (soon, refines H.greet.01) Malformed.
`)
	_, problems := load(t, r)
	wantProblems(t, problems,
		"horizon.md:6: H.greet.6 is distant and cannot refine another clause",
		"horizon.md:7: H.greet.7 is eventual and cannot refine another clause",
		"horizon.md:8: H.greet.8 has more than one refines tag",
		"horizon.md:9: H.greet.9 refines H.greet.1, which is soon, not distant or eventual",
		"horizon.md:10: H.greet.10 refines H.greet.99, which is not in the horizon",
		"horizon.md:11: H.greet.11 refines S.core.1, which is not a horizon clause",
		`horizon.md:12: H.greet.12: malformed clause ID "H.greet.01"`,
	)
}

//shed:proves S.horizon.7
func TestTraceShowsRefinement(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Fine.
- **H.greet.2** (soon, refines H.greet.3) Refines.
- **H.greet.3** (distant) Parent.
- **H.greet.4** (near, refines H.greet.3) Also refines.
`)
	s, problems := load(t, r)
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	var got []string
	for _, e := range Trace(s) {
		got = append(got, e.Clause.ID.String()+" "+e.Tier+" refines "+e.Refines.String()+" refined by "+JoinIDs(e.RefinedBy))
	}
	want := []string{
		"H.greet.1 soon refines  refined by ",
		"H.greet.2 soon refines H.greet.3 refined by ",
		"H.greet.3 distant refines  refined by H.greet.2, H.greet.4",
		"H.greet.4 near refines H.greet.3 refined by ",
	}
	if !slices.Equal(got, want) {
		t.Errorf("trace = %q, want %q", got, want)
	}
}

//shed:proves S.horizon.2
func TestSpecClausesNameHorizonClauses(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/core.md", `- **S.core.1** (H.greet.1) Fine.
- **S.core.2** No list.
- **S.core.3** () Empty list.
- **S.core.4** (C1) Charter clause.
- **S.core.5** (H.greet.9) Missing horizon clause.
- **S.core.6** (H.greet.01) Malformed.
`)
	_, problems := load(t, r)
	wantProblems(t, problems,
		"spec/core.md:2: S.core.2 names no horizon clause",
		"spec/core.md:3: S.core.3 names no horizon clause",
		"spec/core.md:4: S.core.4 advances C1, which is not a horizon clause",
		"spec/core.md:5: S.core.5 advances H.greet.9, which is not in the horizon",
		`spec/core.md:6: S.core.6: malformed clause ID "H.greet.01"`,
	)
}

//shed:proves S.horizon.3
func TestRealisedNeedsASpecClause(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/core.md", "- **S.core.1** (H.greet.2) Advances the other clause.\n")
	_, problems := load(t, r)
	wantProblems(t, problems, "horizon.md:3: H.greet.1 is marked realised but no spec clause advances it")
}

//shed:proves S.horizon.4 S.horizon.5
func TestTraceAndGap(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/more.md", "- **S.more.1** (H.greet.2, H.greet.1) Advances both.\n")
	s, problems := load(t, r)
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	summarise := func(entries []TraceEntry) []string {
		var out []string
		for _, e := range entries {
			out = append(out, e.Clause.ID.String()+" "+e.Tier+" "+JoinIDs(e.AdvancedBy))
		}
		return out
	}
	trace := Trace(s)
	want := []string{"H.greet.1 soon S.core.1, S.more.1", "H.greet.2 soon S.more.1", "H.greet.3 distant "}
	if got := summarise(trace); !slices.Equal(got, want) {
		t.Errorf("trace = %q, want %q", got, want)
	}
	if !trace[0].Realised || trace[1].Realised {
		t.Errorf("realised flags wrong: %+v", trace)
	}
	if got := summarise(Gap(s)); !slices.Equal(got, want[1:]) {
		t.Errorf("gap = %q, want %q", got, want[1:])
	}
}

//shed:proves S.cite.3 S.cite.4
func TestCitationsInProseMustResolve(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/core.md", `# Core

Background on C1, H.greet.1 to H.greet.3 and M1.

- **S.core.1** (H.greet.1) Cites C9, and ranges H.greet.3 to H.greet.1,
  C1 to H.greet.2, and C1 to C5. Code `+"`C42`"+` is not checked.
`)
	_, problems := load(t, r)
	wantProblems(t, problems,
		"spec/core.md:5: C9 does not resolve: no clause C9 in the working tree",
		"spec/core.md:5: range H.greet.3 to H.greet.1 is out of order",
		"spec/core.md:6: range C1 to H.greet.2 spans two series",
		"spec/core.md:6: C5 does not resolve: no clause C5 in the working tree",
	)
}

//shed:proves S.cite.1
func TestCitationsResolveAtRevisions(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Init()
	r.Commit("first")
	r.Git("tag", "v1")
	r.Write("charter.md", testrepo.Charter+"- **C3** It waves.\n")

	s, _ := load(t, r)
	res := NewResolver(r.Dir, s)
	for cite, want := range map[string]string{
		"C3":    "It waves.",
		"C2@v1": "It never shouts.",
	} {
		c, err := res.Resolve(mustCite(t, cite))
		if err != nil || c.Text != want {
			t.Errorf("Resolve(%s) = %q, %v; want %q", cite, c.Text, err, want)
		}
	}
	for cite, want := range map[string]string{
		"C3@v1":   "C3@v1 does not resolve: no clause C3 in the documents at v1",
		"C1@nope": `C1@nope does not resolve: unknown revision "nope"`,
	} {
		if _, err := res.Resolve(mustCite(t, cite)); err == nil || err.Error() != want {
			t.Errorf("Resolve(%s) error = %v, want %q", cite, err, want)
		}
	}
}

func mustCite(t *testing.T, s string) clause.Citation {
	t.Helper()
	c, err := clause.ParseCitation(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
