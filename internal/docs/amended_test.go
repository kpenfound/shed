package docs

import (
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/testrepo"
)

// horizonSet loads the Minimal documents with the given horizon.
func horizonSet(t *testing.T, horizon string) *Set {
	t.Helper()
	r := testrepo.Minimal(t)
	r.Write("horizon.md", horizon)
	s, problems := Load(revision.Worktree(r.Dir))
	if len(problems) > 0 {
		t.Fatalf("problems: %s", messages(problems))
	}
	return s
}

func describeChanges(changes []HorizonChange) string {
	var out []string
	for _, c := range changes {
		out = append(out, c.Change+" "+c.ID.String())
	}
	return strings.Join(out, ", ")
}

//shed:proves S.owner.11 S.owner.12
func TestAmendedHorizon(t *testing.T) {
	const base = `# Horizon

- **H.greet.1** (soon) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
`
	for _, c := range []struct {
		name, horizon, want string
	}{
		{"unchanged", base, ""},
		{"rewrapped only", `# Horizon

- **H.greet.1** (soon) The tool
  says   hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
`, ""},
		{"only marked realised", `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon, realised) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
`, ""},
		{"marked realised and reworded", `# Horizon

- **H.greet.1** (soon, realised) The tool says hello warmly.
- **H.greet.2** (soon, realised) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
`, "changed H.greet.1"},
		{"marked realised and moved tier", `# Horizon

- **H.greet.1** (soon) The tool says hello.
- **H.greet.2** (near, realised) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
`, "changed H.greet.2"},
		{"added, changed and removed, with a realised marking", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near) The tool says goodbye.
`, "added H.greet.4, changed H.greet.2, removed H.greet.3"},
	} {
		got := describeChanges(AmendedHorizon(horizonSet(t, base), horizonSet(t, c.horizon)))
		if got != c.want {
			t.Errorf("%s: amended = %q, want %q", c.name, got, c.want)
		}
	}

	// Removing realised from a tag list amends the clause.
	realised := horizonSet(t, strings.Replace(base, "(soon) The tool says hello", "(soon, realised) The tool says hello", 1))
	if got := describeChanges(AmendedHorizon(realised, horizonSet(t, base))); got != "changed H.greet.1" {
		t.Errorf("unmarking realised: amended = %q, want %q", got, "changed H.greet.1")
	}
}
