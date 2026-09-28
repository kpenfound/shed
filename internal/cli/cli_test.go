package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

func run(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), append([]string{"-C", dir}, args...), &out, &errOut)
	return out.String(), errOut.String(), code
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

//shed:proves S.horizon.4 S.horizon.5
func TestTraceAndGap(t *testing.T) {
	r := project(t)
	stdout, _, code := run(t, r.Dir, "trace")
	want := "H.greet.1  soon     realised  S.core.1\n" +
		"H.greet.2  soon               -\n" +
		"H.greet.3  distant            -\n"
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
