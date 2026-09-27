package proof

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/testrepo"
)

const greetTests = `package greet

import "testing"

//shed:proves S.core.1
func TestHello(t *testing.T) {}

// TestFails proves two clauses and fails.
//
//shed:proves S.core.2 S.core.3
func TestFails(t *testing.T) { t.Fatal("no") }

//shed:proves S.core.4
func TestSkips(t *testing.T) { t.Skip("later") }

//shed:proves S.core.1
func helper(t *testing.T) {}

//shed:proves H.greet.1 S.core.01
func TestHorizon(t *testing.T) {}

//shed:proves
func TestEmpty(t *testing.T) {}

func TestPlain(t *testing.T) {
	//shed:proves S.core.1
}

func TestUnrelated(t *testing.T) {}
`

func module(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.Minimal(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet_test.go", greetTests)
	r.Write("broken/broken_test.go", "package broken\n\nimport \"testing\"\n\n//shed:proves S.core.5\nfunc TestBroken(t *testing.T) { undefined() }\n")
	for _, skipped := range []string{"testdata", ".hidden", "_build", "vendor/x", "nested"} {
		r.Write(skipped+"/skip_test.go", "package skip\n\nimport \"testing\"\n\n//shed:proves S.core.9\nfunc TestSkipped(t *testing.T) {}\n")
	}
	r.Write("nested/go.mod", "module example.com/nested\n")
	return r
}

func names(proofs []Proof) []string {
	var out []string
	for _, p := range proofs {
		ids := make([]string, len(p.Clauses))
		for i, id := range p.Clauses {
			ids[i] = id.String()
		}
		out = append(out, p.Name()+" "+strings.Join(ids, ","))
	}
	return out
}

func messages(problems []clause.Problem) []string {
	var out []string
	for _, p := range problems {
		out = append(out, p.String())
	}
	return out
}

//shed:proves S.proof.1 S.proof.3
func TestDiscoverFindsAnnotatedTests(t *testing.T) {
	r := module(t)
	proofs, _, err := Discover(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"broken.TestBroken S.core.5",
		"TestHello S.core.1",
		"TestFails S.core.2,S.core.3",
		"TestSkips S.core.4",
		"TestHorizon ",
		"TestEmpty ",
	}
	if got := names(proofs); !slices.Equal(got, want) {
		t.Errorf("proofs =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if proofs[1].File != "greet_test.go" || proofs[1].Line != 6 {
		t.Errorf("TestHello at %s:%d", proofs[1].File, proofs[1].Line)
	}
}

//shed:proves S.proof.2
func TestMisplacedDirectivesAreProblems(t *testing.T) {
	r := module(t)
	_, problems, err := Discover(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"greet_test.go:17: helper is not a test function; only tests can be proofs",
		"greet_test.go:19: H.greet.1 is not a spec clause; proofs prove spec clauses",
		`greet_test.go:19: malformed clause ID "S.core.01"`,
		"greet_test.go:22: shed:proves names no clause",
		"greet_test.go:26: shed:proves must be in the doc comment of a test function",
	}
	if got := messages(problems); !slices.Equal(got, want) {
		t.Errorf("problems =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

//shed:proves S.proof.2
func TestEveryClauseNeedsAProof(t *testing.T) {
	r := testrepo.Minimal(t)
	r.Write("spec/core.md", "- **S.core.1** (H.greet.1) Proven.\n- **S.core.2** (H.greet.1) Not proven.\n")
	set, _ := docs.Load(revision.Worktree(r.Dir))
	proofs := []Proof{{Dir: ".", Test: "TestA", File: "a_test.go", Line: 4, Clauses: []clause.ID{
		clause.MustParseID("S.core.1"), clause.MustParseID("S.core.7"),
	}}}
	want := []string{
		"a_test.go:4: TestA proves S.core.7, which is not in the spec",
		"spec/core.md:2: S.core.2 has no proof",
	}
	if got := messages(Check(proofs, set)); !slices.Equal(got, want) {
		t.Errorf("problems = %q, want %q", got, want)
	}
}

//shed:proves S.proof.4
func TestRunReportsEachProof(t *testing.T) {
	r := module(t)
	proofs, _, err := Discover(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err := Runner{Root: r.Dir}.Run(context.Background(), proofs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Status{
		"TestHello":         Pass,
		"TestFails":         Fail,
		"TestSkips":         Skip,
		"TestHorizon":       Pass,
		"TestEmpty":         Pass,
		"broken.TestBroken": NoResult,
	}
	for name, status := range want {
		if results[name] != status {
			t.Errorf("%s = %q, want %q", name, results[name], status)
		}
	}

	ids := []clause.ID{
		clause.MustParseID("S.core.1"), clause.MustParseID("S.core.2"),
		clause.MustParseID("S.core.4"), clause.MustParseID("S.core.5"),
		clause.MustParseID("S.core.8"),
	}
	var got []string
	for _, cr := range ByClause(ids, proofs, results) {
		got = append(got, cr.ID.String()+" "+map[bool]string{true: "pass", false: "fail"}[cr.Pass])
	}
	wantClauses := []string{"S.core.1 pass", "S.core.2 fail", "S.core.4 fail", "S.core.5 fail", "S.core.8 fail"}
	if !slices.Equal(got, wantClauses) {
		t.Errorf("clauses = %q, want %q", got, wantClauses)
	}
}

//shed:proves S.proof.5
func TestRunnerPrefixWrapsGoTest(t *testing.T) {
	r := module(t)
	r.Write("wrap.sh", "#!/bin/sh\nprintf '%s\\n' \"$@\" > wrapped.txt\nexec \"$@\"\n")
	if err := os.Chmod(r.Dir+"/wrap.sh", 0o755); err != nil {
		t.Fatal(err)
	}
	proofs, _, err := Discover(r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	hello := slices.IndexFunc(proofs, func(p Proof) bool { return p.Test == "TestHello" })
	results, err := Runner{Root: r.Dir, Prefix: []string{"./wrap.sh"}}.Run(context.Background(), proofs[hello:hello+1])
	if err != nil {
		t.Fatal(err)
	}
	if results["TestHello"] != Pass {
		t.Errorf("TestHello = %q through the runner", results["TestHello"])
	}
	wrapped, err := os.ReadFile(r.Dir + "/wrapped.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := "go\ntest\n-json\n-count=1\n-run\n^(TestHello)$\n.\n"
	if string(wrapped) != want {
		t.Errorf("runner got %q, want %q", wrapped, want)
	}
}
