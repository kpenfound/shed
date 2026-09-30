package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeOperator(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), OperatorFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

//shed:proves S.config.1
func TestOperatorDefaultsAndOverrides(t *testing.T) {
	c, err := LoadOperator(filepath.Join(t.TempDir(), OperatorFile))
	if err != nil {
		t.Fatal(err)
	}
	if c.Shed.BounceThreshold != 3 || c.Concurrency.Units != 4 || c.Shed.ContestedTimeout.Duration != 72*time.Hour {
		t.Errorf("defaults = %+v", c)
	}
	if got := c.Roles["mechanic"].Profile; got != "default" {
		t.Errorf("mechanic profile = %q", got)
	}
	if steps := c.Formulas[DefaultFormula].Steps; len(steps) != 3 || steps[0].Name != "proofs" {
		t.Errorf("default formula = %+v", steps)
	}
	if c.VCS != (VCS{Main: "main", LandingName: "shed wheelbuilder", LandingEmail: "wheelbuilder@shed.localhost"}) {
		t.Errorf("vcs = %+v", c.VCS)
	}
	if c.Owner.SampleEvery != 0 {
		t.Errorf("owner = %+v", c.Owner)
	}

	c, err = LoadOperator(writeOperator(t, `
[budget]
per_day_usd = 20

[owner]
sample_every = 3

[shed]
bounce_threshold = 5
contested_timeout = "24h"

[profiles.fast]
agent = "codex"
model = "small"
fallback = "default"

[roles.committee]
profile = "fast"

[formulas.bug]
steps = [{ name = "fix" }, { name = "proofs", needs = ["fix"] }]

[vcs]
remote = "origin"
landing_email = "lander@example.com"
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Budget.PerDayUSD != 20 || c.Budget.PerSessionUSD != 5 {
		t.Errorf("budget = %+v", c.Budget)
	}
	if c.Shed.BounceThreshold != 5 || c.Shed.MaxRounds != 3 || c.Shed.ContestedTimeout.Duration != 24*time.Hour {
		t.Errorf("shed = %+v", c.Shed)
	}
	if c.Profiles["default"].Agent != "claude" || c.Profiles["fast"].Fallback != "default" {
		t.Errorf("profiles = %+v", c.Profiles)
	}
	if c.Roles["committee"].Profile != "fast" || c.Roles["painter"].Profile != "default" {
		t.Errorf("roles = %+v", c.Roles)
	}
	if len(c.Formulas) != 2 {
		t.Errorf("formulas = %+v", c.Formulas)
	}
	if c.VCS.Remote != "origin" || c.VCS.Main != "main" || c.VCS.LandingEmail != "lander@example.com" || c.VCS.LandingName != "shed wheelbuilder" {
		t.Errorf("vcs = %+v", c.VCS)
	}
	if c.Owner.SampleEvery != 3 {
		t.Errorf("owner = %+v", c.Owner)
	}
}

//shed:proves S.owner.11
func TestOperatorOwnerSampling(t *testing.T) {
	c, err := LoadOperator(filepath.Join(t.TempDir(), OperatorFile))
	if err != nil {
		t.Fatal(err)
	}
	if c.Owner.SampleEvery != 0 {
		t.Errorf("default owner.sample_every = %d, want 0", c.Owner.SampleEvery)
	}
	for toml, want := range map[string]int{"[owner]\nsample_every = 0\n": 0, "[owner]\nsample_every = 5\n": 5} {
		c, err := LoadOperator(writeOperator(t, toml))
		if err != nil {
			t.Errorf("%q: %v", toml, err)
		} else if c.Owner.SampleEvery != want {
			t.Errorf("%q: owner.sample_every = %d, want %d", toml, c.Owner.SampleEvery, want)
		}
	}
	for toml, want := range map[string]string{
		"[owner]\nsample_every = -1\n": "owner.sample_every must not be negative",
		"[owner]\nsample_rate = 2\n":   "unknown setting owner.sample_rate",
	} {
		if _, err := LoadOperator(writeOperator(t, toml)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", toml, err, want)
		}
	}
}

//shed:proves S.config.2
func TestOperatorRefusesBadSettings(t *testing.T) {
	for _, tc := range []struct{ toml, want string }{
		{"[budget]\nper_hour = 1\n", "unknown setting budget.per_hour"},
		{"[budget]\nper_day_usd = -1\n", "budget.per_day_usd must not be negative"},
		{"[budget]\noverrun_multiple = 0.5\n", "budget.overrun_multiple must be at least 1"},
		{"[concurrency]\nunits = 0\n", "concurrency.units must be at least 1"},
		{"[shed]\nmax_rounds = 0\n", "shed.max_rounds must be at least 1"},
		{"[shed]\nbounce_threshold = -1\n", "shed.bounce_threshold must not be negative"},
		{"[shed]\nmax_rounds = 2\namendment_rounds = 3\n", "shed.amendment_rounds must not be greater than shed.max_rounds"},
		{"[profiles.x]\nagent = \"gpt\"\n", `profiles.x.agent "gpt" is not one of`},
		{"[profiles.x]\nagent = \"pi\"\nfallback = \"y\"\n", `profiles.x.fallback names unknown profile "y"`},
		{"[profiles.a]\nagent = \"pi\"\nfallback = \"b\"\n[profiles.b]\nagent = \"pi\"\nfallback = \"a\"\n", "profiles.a.fallback loops back to itself"},
		{"[roles.painter]\nprofile = \"nope\"\n", `roles.painter.profile names unknown profile "nope"`},
		{"[roles.owner]\nprofile = \"default\"\n", "roles.owner is not a role"},
		{"[formulas.x]\nsteps = [{ name = \"a\", needs = [\"b\"] }]\n", `formulas.x: step "a" needs unknown step "b"`},
		{"[formulas.x]\nsteps = [{ name = \"a\", needs = [\"b\"] }, { name = \"b\", needs = [\"a\"] }]\n", "needs itself through a cycle"},
		{"[formulas.x]\nsteps = [{ name = \"a\" }, { name = \"a\" }]\n", `formulas.x: step "a" appears twice`},
		{"[formulas.x]\nsteps = []\n", "formulas.x: has no steps"},
		{"[vcs]\nmain = \"\"\n", "vcs.main must not be empty"},
		{"[painter]\ninterval = \"2h\"\nmax_interval = \"1h\"\n", "painter.max_interval must not be less than painter.interval"},
		{"[profiles.x]\nagent = \"pi\"\nmounts = [\"/tmp\"]\n", `profiles.x.mounts: "/tmp" is not path:ro or path:rw`},
		{"[vcs]\nlanding_name = \" \"\n", "vcs.landing_name must not be empty"},
	} {
		_, err := LoadOperator(writeOperator(t, tc.toml))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want %q", tc.toml, err, tc.want)
		}
	}

	for _, toml := range []string{"[shed]\nmax_rounds = 2\namendment_rounds = 2\n", "[shed]\nmax_rounds = 1\n"} {
		if _, err := LoadOperator(writeOperator(t, toml)); err != nil {
			t.Errorf("%q: %v", toml, err)
		}
	}

	c := Defaults()
	delete(c.Formulas, DefaultFormula)
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "formulas.default is missing") {
		t.Errorf("missing default formula: %v", err)
	}
}

//shed:proves S.config.1 S.config.2 S.config.3 S.shed.20
func TestCommitteeAssignments(t *testing.T) {
	for _, input := range []string{
		"[committee]\nperspectives = []",
		"[committee]\nperspectives = [\"unknown\"]",
		"[committee]\nprofiles = [\"unknown\"]",
	} {
		if _, err := LoadOperator(writeOperator(t, input)); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
	c, err := LoadOperator(writeOperator(t, "[committee]\nperspectives = [\"scope\", \"correctness\"]\nprofiles = [\"default\"]"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Committee.Perspectives, ",") != "scope,correctness" || strings.Join(c.Committee.Profiles, ",") != "default" {
		t.Fatalf("assignments: %+v", c.Committee)
	}
}
