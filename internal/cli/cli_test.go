package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/testrepo"
)

func run(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), append([]string{"-C", dir}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
}

// runAt runs cmd (env.inbox or env.status) directly, with a fixed clock
// injected as "now", so a test can control the moment the command reads
// instead of the real one.
func runAt(t *testing.T, root, state string, now time.Time, cmd func(env, []string) int, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	e := env{ctx: context.Background(), root: root, state: state, stdout: &out, stderr: &errOut, now: func() time.Time { return now }}
	if code := cmd(e, args); code != OK {
		t.Fatalf("command %v at %s = %d: %s", args, now, code, errOut.String())
	}
	return out.String()
}

// collapsed splits out into its non-blank lines, each with runs of
// whitespace collapsed to one space, so a comparison does not depend on
// tabwriter's column widths.
func collapsed(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			lines = append(lines, strings.Join(f, " "))
		}
	}
	return lines
}

// project is a repository with documents, a Go module and a passing proof.
func project(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.Minimal(t)
	r.Write("go.mod", "module example.com/greet\n\ngo 1.21\n")
	r.Write("greet_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.1\nfunc TestHello(t *testing.T) {}\n")
	return r
}

//shed:proves S.check.1 S.doc.1
func TestCheck(t *testing.T) {
	r := project(t)
	stdout, stderr, code := run(t, r.Dir, "check")
	if code != OK || stderr != "" {
		t.Fatalf("check = %d, stderr %q", code, stderr)
	}
	if want := "ok: 2 charter, 1 spec and 3 horizon clauses, 1 proofs\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	r.Write("spec/core.md", testrepo.Spec+"- **S.core.2** (H.greet.2) Says bye, per C7.\n")
	r.Remove("horizon.md")
	stdout, stderr, code = run(t, r.Dir, "check")
	if code != Failed || stdout != "" {
		t.Fatalf("check = %d, stdout %q", code, stdout)
	}
	want := strings.Join([]string{
		"horizon.md: missing",
		"spec/core.md:3: S.core.1 advances H.greet.1, which is not in the horizon",
		"spec/core.md:4: S.core.2 advances H.greet.2, which is not in the horizon",
		"spec/core.md:4: C7 does not resolve: no clause C7 in the working tree",
		"spec/core.md:4: S.core.2 has no proof",
	}, "\n") + "\n"
	if stderr != want {
		t.Errorf("stderr =\n%s\nwant\n%s", stderr, want)
	}
}

//shed:proves S.cite.2
func TestShow(t *testing.T) {
	r := project(t)
	r.Init()
	r.Commit("documents")
	r.Write("charter.md", "# Charter\n\n- **C1** The tool greets everyone.\n- **C2** It never shouts.\n")

	stdout, stderr, code := run(t, r.Dir, "show", "C1", "C1@HEAD", "H.greet.2")
	if code != OK || stderr != "" {
		t.Fatalf("show = %d, stderr %q", code, stderr)
	}
	want := "C1  charter.md:3\n    The tool greets everyone.\n" +
		"C1@HEAD  charter.md:3\n    The tool greets people.\n" +
		"H.greet.2  horizon.md:4\n    (soon) The tool says goodbye.\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}

	stdout, stderr, code = run(t, r.Dir, "show", "C2", "C9", "S.x")
	if code != Failed {
		t.Errorf("show with bad citations = %d", code)
	}
	if !strings.HasPrefix(stdout, "C2  charter.md:4\n") {
		t.Errorf("stdout = %q", stdout)
	}
	wantErr := "shed: C9 does not resolve: no clause C9 in the working tree\nshed: malformed clause ID \"S.x\"\n"
	if stderr != wantErr {
		t.Errorf("stderr = %q, want %q", stderr, wantErr)
	}
}

//shed:proves S.diff.1
func TestDiff(t *testing.T) {
	r := project(t)
	r.Init()
	first := r.Commit("first")
	r.Write("spec/core.md", "- **S.core.1** (H.greet.1) Running the tool prints hello, twice.\n- **S.core.2** (H.greet.2) Bye.\n")
	second := r.Commit("second")
	r.Write("spec/core.md", "- **S.core.2** (H.greet.2) Bye.\n")

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"diff", first, second}, "added   S.core.2\nchanged S.core.1\n"},
		{[]string{"diff", "HEAD"}, "removed S.core.1\n"},
		{[]string{"diff", first}, "added   S.core.2\nremoved S.core.1\n"},
		{[]string{"diff", "HEAD", "HEAD"}, ""},
	} {
		stdout, stderr, code := run(t, r.Dir, tc.args...)
		if code != OK || stderr != "" || stdout != tc.want {
			t.Errorf("%v = %d, %q, stderr %q; want %q", tc.args, code, stdout, stderr, tc.want)
		}
	}
	if _, stderr, code := run(t, r.Dir, "diff", "nope"); code != Failed || !strings.Contains(stderr, `unknown revision "nope"`) {
		t.Errorf("unknown revision = %d, %q", code, stderr)
	}
}

// horizonDoc wraps horizon clauses in the heading and milestones the test
// repository's horizon carries.
func horizonDoc(clauses string) string {
	return "# Horizon\n\n" + clauses + "\n## Milestones\n\n- **M1** Greetings. H.greet.1 to H.greet.2.\n"
}

//shed:proves S.diff.3
func TestDiffListsHorizonClauses(t *testing.T) {
	r := project(t)
	r.Write("horizon.md", horizonDoc(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (near) Removed later.
- **H.greet.5** (near) Rewrapped only.
- **H.greet.6** (near) Text changes.
- **H.greet.7** (soon) Tier changes.
- **H.greet.8** (near) Tier rises.
`))
	r.Init()
	first := r.Commit("first")
	r.Write("spec/core.md", "# Core\n\n- **S.core.1** (H.greet.1) Running the tool prints hello, twice.\n")
	r.Write("horizon.md", horizonDoc(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.5**   (near)   Rewrapped
  only.
- **H.greet.6** (near) Text changed.
- **H.greet.7** (near) Tier changes.
- **H.greet.8** (distant) Tier rises.
- **H.greet.9** (eventual) Added.
`))
	second := r.Commit("second")

	stdout, stderr, code := run(t, r.Dir, "diff", first, second)
	want := "changed S.core.1\n" +
		"added   H.greet.9 eventual\n" +
		"removed H.greet.4 near\n" +
		"changed H.greet.6 near\n" +
		"changed H.greet.7 soon\n" +
		"changed H.greet.8 distant\n" +
		"tier    eventual\n"
	if code != OK || stderr != "" || stdout != want {
		t.Errorf("diff = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, want)
	}

	// Against the working tree, and in the other direction, tiers come from
	// the revision each clause is on.
	r.Write("horizon.md", horizonDoc(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.5** (near) Rewrapped only.
- **H.greet.6** (near) Text changed.
- **H.greet.7** (near) Tier changes.
- **H.greet.8** (distant) Tier rises.
`))
	stdout, stderr, code = run(t, r.Dir, "diff", "HEAD")
	want = "removed H.greet.9 eventual\ntier    eventual\n"
	if code != OK || stderr != "" || stdout != want {
		t.Errorf("diff HEAD = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, want)
	}
	stdout, stderr, code = run(t, r.Dir, "diff", second, first)
	want = "changed S.core.1\n" +
		"added   H.greet.4 near\n" +
		"removed H.greet.9 eventual\n" +
		"changed H.greet.6 near\n" +
		"changed H.greet.7 soon\n" +
		"changed H.greet.8 distant\n" +
		"tier    eventual\n"
	if code != OK || stderr != "" || stdout != want {
		t.Errorf("diff second first = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, want)
	}
}

//shed:proves S.diff.4
func TestDiffGivesHorizonAmendmentTier(t *testing.T) {
	base := `- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near, refines H.greet.3) The tool greets in French.
`
	r := project(t)
	r.Write("horizon.md", horizonDoc(base))
	r.Init()
	r.Commit("base")

	for _, tc := range []struct {
		name    string
		horizon string
		want    string
	}{
		{"no horizon change", base, ""},
		{"gaining realised is listed, not counted",
			strings.Replace(base, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1),
			"changed H.greet.2 soon\n"},
		{"losing realised counts",
			strings.Replace(base, "(soon, realised) The tool says hello.", "(soon) The tool says hello.", 1),
			"changed H.greet.1 soon\ntier    soon\n"},
		{"realised on a refining clause is not counted",
			strings.Replace(base, "(near, refines H.greet.3)", "(near, refines H.greet.3, realised)", 1),
			"changed H.greet.5 near\n"},
		{"realised with a text change counts",
			strings.Replace(base, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye politely.", 1),
			"changed H.greet.2 soon\ntier    soon\n"},
		{"the tier is the highest counted, ignoring realised-only changes",
			strings.NewReplacer(
				"(soon) The tool says goodbye.", "(soon) The tool says goodbye politely.",
				"(distant) The tool", "(distant, realised) The tool",
			).Replace(base),
			"changed H.greet.2 soon\nchanged H.greet.3 distant\ntier    soon\n"},
		{"a refining clause with an unchanged tag counts at its own tier",
			strings.Replace(base, "greets in French.", "greets in French, formally.", 1),
			"changed H.greet.5 near\ntier    near\n"},
		{"an added clause that refines counts at its parent's tier",
			base + "- **H.greet.6** (near, refines H.greet.3) The tool greets in German.\n",
			"added   H.greet.6 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a removed clause that refined counts at its parent's tier",
			strings.Replace(base, "- **H.greet.5** (near, refines H.greet.3) The tool greets in French.\n", "", 1),
			"removed H.greet.5 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a dropped refines tag counts at the old parent's tier",
			strings.Replace(base, "(near, refines H.greet.3)", "(near)", 1),
			"changed H.greet.5 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a new refines tag counts at the new parent's tier",
			strings.Replace(base, "(soon) The tool says goodbye.", "(soon, refines H.greet.4) The tool says goodbye.", 1),
			"changed H.greet.2 soon parent H.greet.4 (eventual)\ntier    eventual\n"},
		{"a retargeted refines tag counts at both parents' tiers",
			strings.Replace(base, "(near, refines H.greet.3)", "(near, refines H.greet.4)", 1),
			"changed H.greet.5 near parent H.greet.3 (distant), H.greet.4 (eventual)\ntier    eventual\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.Write("horizon.md", horizonDoc(tc.horizon))
			stdout, stderr, code := run(t, r.Dir, "diff", "HEAD")
			if code != OK || stderr != "" || stdout != tc.want {
				t.Errorf("diff = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, tc.want)
			}
		})
	}
}

// TestDiffNamesRefinedParent checks that a listed horizon clause counted at
// a parent's tier because of its refines tag names that parent and the
// parent's tier on the revision whose tag names it, first revision first,
// and that other listed clauses name no parent.
//
//shed:proves S.diff.5
func TestDiffNamesRefinedParent(t *testing.T) {
	base := `- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near, refines H.greet.3) The tool greets in French.
`
	r := project(t)
	r.Write("horizon.md", horizonDoc(base))
	r.Init()
	first := r.Commit("base")

	for _, tc := range []struct {
		name    string
		horizon string
		want    string
	}{
		{"a refining clause with an unchanged tag names none",
			strings.Replace(base, "greets in French.", "greets in French, formally.", 1),
			"changed H.greet.5 near\ntier    near\n"},
		{"a refining clause gaining realised names none",
			strings.Replace(base, "(near, refines H.greet.3)", "(near, refines H.greet.3, realised)", 1),
			"changed H.greet.5 near\n"},
		{"an added clause that refines names its parent",
			base + "- **H.greet.6** (near, refines H.greet.3) The tool greets in German.\n",
			"added   H.greet.6 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a removed clause that refined names its parent",
			strings.Replace(base, "- **H.greet.5** (near, refines H.greet.3) The tool greets in French.\n", "", 1),
			"removed H.greet.5 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a dropped refines tag names the old parent",
			strings.Replace(base, "(near, refines H.greet.3)", "(near)", 1),
			"changed H.greet.5 near parent H.greet.3 (distant)\ntier    distant\n"},
		{"a new refines tag names the new parent",
			strings.Replace(base, "(soon) The tool says goodbye.", "(soon, refines H.greet.4) The tool says goodbye.", 1),
			"changed H.greet.2 soon parent H.greet.4 (eventual)\ntier    eventual\n"},
		{"a retargeted refines tag names the old parent, then the new",
			strings.Replace(base, "(near, refines H.greet.3)", "(near, refines H.greet.4)", 1),
			"changed H.greet.5 near parent H.greet.3 (distant), H.greet.4 (eventual)\ntier    eventual\n"},
		{"a parent's tier comes from the revision whose tag names it",
			strings.NewReplacer(
				"- **H.greet.5** (near, refines H.greet.3) The tool greets in French.\n", "",
				"(distant) The tool greets", "(eventual) The tool greets",
			).Replace(base),
			"removed H.greet.5 near parent H.greet.3 (distant)\nchanged H.greet.3 eventual\ntier    eventual\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.Write("horizon.md", horizonDoc(tc.horizon))
			stdout, stderr, code := run(t, r.Dir, "diff", "HEAD")
			if code != OK || stderr != "" || stdout != tc.want {
				t.Errorf("diff = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, tc.want)
			}
		})
	}

	// The same retarget in the other direction names the parents in the
	// other order.
	r.Write("horizon.md", horizonDoc(strings.Replace(base, "(near, refines H.greet.3)", "(near, refines H.greet.4)", 1)))
	second := r.Commit("retarget")
	stdout, stderr, code := run(t, r.Dir, "diff", second, first)
	want := "changed H.greet.5 near parent H.greet.4 (eventual), H.greet.3 (distant)\ntier    eventual\n"
	if code != OK || stderr != "" || stdout != want {
		t.Errorf("diff reversed = %d, stderr %q\n%s\nwant\n%s", code, stderr, stdout, want)
	}
}

//shed:proves S.horizon.4 S.horizon.5
func TestTraceAndGap(t *testing.T) {
	r := project(t)
	stdout, _, code := run(t, r.Dir, "trace")
	want := "H.greet.1  soon     realised  S.core.1\n" +
		"H.greet.2  soon               -\n" +
		"H.greet.3  distant            -\n" +
		"no parent (2): H.greet.1, H.greet.2\n"
	if code != OK || stdout != want {
		t.Errorf("trace = %d\n%s\nwant\n%s", code, stdout, want)
	}
	stdout, _, code = run(t, r.Dir, "gap")
	want = "H.greet.2  soon     -\nH.greet.3  distant  -\n"
	if code != OK || stdout != want {
		t.Errorf("gap = %d\n%s\nwant\n%s", code, stdout, want)
	}
}

//shed:proves S.horizon.7
func TestTraceShowsRefinement(t *testing.T) {
	r := project(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Fine.
- **H.greet.2** (soon, refines H.greet.3) Refines.
- **H.greet.3** (distant) Parent.
- **H.greet.4** (near, refines H.greet.3) Also refines.
`)
	stdout, stderr, code := run(t, r.Dir, "trace")
	if code != OK {
		t.Fatalf("trace = %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	want := []string{
		"H.greet.1 soon realised S.core.1",
		"H.greet.2 soon - refines H.greet.3",
		"H.greet.3 distant - refined by H.greet.2, H.greet.4",
		"H.greet.4 near - refines H.greet.3",
		"no parent (1): H.greet.1",
	}
	if len(lines) != len(want) {
		t.Fatalf("trace =\n%s\nwant %d lines", stdout, len(want))
	}
	for i, line := range lines {
		if got := strings.Join(strings.Fields(line), " "); got != want[i] {
			t.Errorf("trace line %d = %q, want %q", i+1, got, want[i])
		}
	}
}

// TestTraceListsClausesWithoutParent checks that trace ends with the near
// and soon clauses, realised or not, that refine nothing, and prints no such
// line when every near and soon clause refines a parent.
//
//shed:proves S.horizon.12
func TestTraceListsClausesWithoutParent(t *testing.T) {
	r := project(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Realised, no parent.
- **H.greet.2** (soon, refines H.greet.3) Refines.
- **H.greet.3** (distant) Parent.
- **H.greet.4** (near) No parent.
- **H.greet.5** (eventual) Far, never listed.
- **H.greet.6** (near, refines H.greet.5) Refines the far one.
- **H.greet.7** (soon) No parent either.
`)
	stdout, stderr, code := run(t, r.Dir, "trace")
	if code != OK {
		t.Fatalf("trace = %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("trace =\n%s\nwant 8 lines", stdout)
	}
	for i, line := range lines[:7] {
		if want := fmt.Sprintf("H.greet.%d", i+1); !strings.HasPrefix(line, want+" ") {
			t.Errorf("trace line %d = %q, want clause %s", i+1, line, want)
		}
	}
	if got, want := lines[7], "no parent (3): H.greet.1, H.greet.4, H.greet.7"; got != want {
		t.Errorf("last trace line = %q, want %q", got, want)
	}

	r.Write("horizon.md", `- **H.greet.1** (soon, realised, refines H.greet.3) Realised child.
- **H.greet.2** (near, refines H.greet.3) Child.
- **H.greet.3** (distant) Parent.
`)
	stdout, stderr, code = run(t, r.Dir, "trace")
	if code != OK {
		t.Fatalf("trace = %d, stderr %q", code, stderr)
	}
	if strings.Contains(stdout, "no parent") {
		t.Errorf("trace with every clause refined =\n%s\nwant no parent line", stdout)
	}
	if n := strings.Count(stdout, "\n"); n != 3 {
		t.Errorf("trace =\n%s\nwant 3 lines", stdout)
	}
}

//shed:proves S.horizon.10
func TestGapShowsRefinement(t *testing.T) {
	r := project(t)
	r.Write("horizon.md", `- **H.greet.1** (soon, realised) Fine.
- **H.greet.2** (soon, refines H.greet.3) Refines.
- **H.greet.3** (distant) Parent.
- **H.greet.4** (near, refines H.greet.5) Refines the far one.
- **H.greet.5** (eventual) Far parent.
- **H.greet.6** (soon) Stands alone.
`)
	stdout, stderr, code := run(t, r.Dir, "gap")
	if code != OK {
		t.Fatalf("gap = %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	want := []string{
		"H.greet.2 soon - refines H.greet.3 (distant)",
		"H.greet.3 distant -",
		"H.greet.4 near - refines H.greet.5 (eventual)",
		"H.greet.5 eventual -",
		"H.greet.6 soon -",
	}
	if len(lines) != len(want) {
		t.Fatalf("gap =\n%s\nwant %d lines", stdout, len(want))
	}
	for i, line := range lines {
		if got := strings.Join(strings.Fields(line), " "); got != want[i] {
			t.Errorf("gap line %d = %q, want %q", i+1, got, want[i])
		}
	}
}

//shed:proves S.proof.4 S.proof.5
func TestProve(t *testing.T) {
	r := project(t)
	r.Write("spec/core.md", testrepo.Spec+
		"- **S.core.2** (H.greet.2) Says goodbye.\n"+
		"- **S.core.3** (H.greet.2) Waves.\n")
	r.Write("bye_test.go", "package greet\n\nimport \"testing\"\n\n//shed:proves S.core.2\nfunc TestBye(t *testing.T) { t.Fatal(\"no goodbye\") }\n")
	r.Write("run-here", "#!/bin/sh\necho ran >> runner.log\nexec \"$@\"\n")
	if err := os.Chmod(filepath.Join(r.Dir, "run-here"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.Write("shed.toml", "[proofs]\nrunner = [\"./run-here\"]\n")

	stdout, _, code := run(t, r.Dir, "prove", "S.core.1")
	if code != OK || stdout != "pass  S.core.1\n" {
		t.Errorf("prove S.core.1 = %d, %q", code, stdout)
	}
	stdout, _, code = run(t, r.Dir, "prove")
	want := "pass  S.core.1\n" +
		"fail  S.core.2  TestBye: fail\n" +
		"fail  S.core.3  no proof\n"
	if code != Failed || stdout != want {
		t.Errorf("prove = %d\n%s\nwant\n%s", code, stdout, want)
	}
	log, err := os.ReadFile(filepath.Join(r.Dir, "runner.log"))
	if err != nil || string(log) != "ran\nran\n" {
		t.Errorf("runner log = %q, %v", log, err)
	}
	if _, stderr, code := run(t, r.Dir, "prove", "H.greet.1"); code != Misused || !strings.Contains(stderr, "H.greet.1 is not a spec clause") {
		t.Errorf("prove H.greet.1 = %d, %q", code, stderr)
	}
}

// The repository's own documents and proofs must always pass check.
func TestRepositoryPassesCheck(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := run(t, root, "check")
	if code != OK {
		t.Fatalf("check = %d\n%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "ok: ") {
		t.Errorf("stdout = %q", stdout)
	}
}

//shed:proves S.release.1
func TestVersion(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	Version = "v1.2.3"
	if out, _, code := run(t, t.TempDir(), "version"); code != OK || out != "shed v1.2.3\n" {
		t.Errorf("version = %d, %q", code, out)
	}
	Version = ""
	if out, _, code := run(t, t.TempDir(), "version"); code != OK || !strings.HasPrefix(out, "shed dev") {
		t.Errorf("version without a release = %d, %q", code, out)
	}
}
