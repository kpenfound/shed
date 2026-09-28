package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/kpenfound/shed/internal/unit"
)

// OperatorFile is the operator settings file, relative to the state
// directory. It is outside version control and never part of the charter or
// spec.
const OperatorFile = "config.toml"

// Operator holds the settings that tune the factory on one machine.
type Operator struct {
	Budget      Budget             `toml:"budget"`
	Concurrency Concurrency        `toml:"concurrency"`
	Shed        Debate             `toml:"shed"`
	Profiles    map[string]Profile `toml:"profiles"`
	Roles       map[string]Role    `toml:"roles"`
	Formulas    map[string]Formula `toml:"formulas"`
	VCS         VCS                `toml:"vcs"`
}

// VCS configures version control.
type VCS struct {
	// JJ is the jj executable; empty runs jj from PATH.
	JJ string `toml:"jj,omitempty"`
	// Main is the bookmark units land on.
	Main string `toml:"main"`
	// Remote, when set, is fetched before each landing and receives main
	// after it.
	Remote string `toml:"remote,omitempty"`
	// LandingName and LandingEmail are the identity landed commits are made
	// under.
	LandingName  string `toml:"landing_name"`
	LandingEmail string `toml:"landing_email"`
}

// Budget limits spending. Zero means no limit.
type Budget struct {
	PerSessionUSD float64 `toml:"per_session_usd"`
	PerDayUSD     float64 `toml:"per_day_usd"`
	// OverrunMultiple is how far past its estimate a unit's cost may go
	// before it reopens for debate.
	OverrunMultiple float64 `toml:"overrun_multiple"`
}

// Concurrency caps how much runs at once.
type Concurrency struct {
	// Units caps units across implementing and verifying.
	Units            int `toml:"units"`
	MechanicsPerUnit int `toml:"mechanics_per_unit"`
	// Committee is the number of committee members per debate.
	Committee int `toml:"committee"`
}

// Debate bounds the shed.
type Debate struct {
	MaxRounds       int `toml:"max_rounds"`
	AmendmentRounds int `toml:"amendment_rounds"`
	// BounceThreshold is how many bounces a unit may take; one more makes
	// it contested.
	BounceThreshold  int      `toml:"bounce_threshold"`
	ContestedTimeout Duration `toml:"contested_timeout"`
}

// Profile is how a role's sessions run.
type Profile struct {
	Agent    string   `toml:"agent"`
	Model    string   `toml:"model,omitempty"`
	Effort   string   `toml:"effort,omitempty"`
	Fallback string   `toml:"fallback,omitempty"`
	MaxTurns int      `toml:"max_turns,omitempty"`
	Timeout  Duration `toml:"timeout,omitempty"`
}

// Role selects the profile a role's sessions use.
type Role struct {
	Profile string `toml:"profile"`
}

// Formula is the DAG of steps inside implementing for one unit type.
type Formula struct {
	Steps []Step `toml:"steps"`
}

// Step is one formula step. Needs names the steps that finish before it.
type Step struct {
	Name  string   `toml:"name"`
	Needs []string `toml:"needs,omitempty"`
}

// Duration is a time.Duration written as a string such as "72h".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	d.Duration = v
	return err
}

func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Agents lists the agent backends a profile may name.
var Agents = []string{"claude", "codex", "opencode", "pi"}

// DefaultFormula is the formula units use unless their type names another.
const DefaultFormula = "default"

// Defaults returns the settings used where the operator file is silent.
func Defaults() Operator {
	roles := map[string]Role{}
	for _, r := range unit.Roles {
		roles[string(r)] = Role{Profile: "default"}
	}
	return Operator{
		Budget:      Budget{PerSessionUSD: 5, PerDayUSD: 50, OverrunMultiple: 3},
		Concurrency: Concurrency{Units: 4, MechanicsPerUnit: 1, Committee: 3},
		Shed: Debate{MaxRounds: 3, AmendmentRounds: 1, BounceThreshold: 3,
			ContestedTimeout: Duration{72 * time.Hour}},
		Profiles: map[string]Profile{"default": {Agent: "claude"}},
		Roles:    roles,
		Formulas: map[string]Formula{DefaultFormula: {Steps: []Step{
			{Name: "proofs"},
			{Name: "implement", Needs: []string{"proofs"}},
			{Name: "docs", Needs: []string{"implement"}},
		}}},
		VCS: VCS{Main: "main", LandingName: "shed wheelbuilder", LandingEmail: "wheelbuilder@shed.localhost"},
	}
}

// LoadOperator reads the operator file over the defaults. A missing file
// gives the defaults.
func LoadOperator(path string) (Operator, error) {
	c := Defaults()
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Operator{}, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Operator{}, fmt.Errorf("%s: unknown setting %s", path, undecoded[0])
	}
	if err := c.Validate(); err != nil {
		return Operator{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Validate checks the settings for values the factory cannot use.
func (c Operator) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	for name, v := range map[string]float64{
		"budget.per_session_usd": c.Budget.PerSessionUSD,
		"budget.per_day_usd":     c.Budget.PerDayUSD,
	} {
		if v < 0 {
			fail("%s must not be negative", name)
		}
	}
	if c.Budget.OverrunMultiple != 0 && c.Budget.OverrunMultiple < 1 {
		fail("budget.overrun_multiple must be at least 1, or 0 for none")
	}
	for name, v := range map[string]int{
		"concurrency.units":              c.Concurrency.Units,
		"concurrency.mechanics_per_unit": c.Concurrency.MechanicsPerUnit,
		"concurrency.committee":          c.Concurrency.Committee,
		"shed.max_rounds":                c.Shed.MaxRounds,
		"shed.amendment_rounds":          c.Shed.AmendmentRounds,
	} {
		if v < 1 {
			fail("%s must be at least 1", name)
		}
	}
	if c.Shed.BounceThreshold < 0 {
		fail("shed.bounce_threshold must not be negative")
	}
	if c.Shed.ContestedTimeout.Duration < 0 {
		fail("shed.contested_timeout must not be negative")
	}

	for _, name := range sortedKeys(c.Profiles) {
		p := c.Profiles[name]
		if !slices.Contains(Agents, p.Agent) {
			fail("profiles.%s.agent %q is not one of %v", name, p.Agent, Agents)
		}
		if p.Fallback != "" {
			if _, ok := c.Profiles[p.Fallback]; !ok {
				fail("profiles.%s.fallback names unknown profile %q", name, p.Fallback)
			} else if fallbackLoops(c.Profiles, name) {
				fail("profiles.%s.fallback loops back to itself", name)
			}
		}
	}
	for _, name := range sortedKeys(c.Roles) {
		if !slices.Contains(unit.Roles, unit.Actor(name)) {
			fail("roles.%s is not a role", name)
		} else if _, ok := c.Profiles[c.Roles[name].Profile]; !ok {
			fail("roles.%s.profile names unknown profile %q", name, c.Roles[name].Profile)
		}
	}
	for _, r := range unit.Roles {
		if _, ok := c.Roles[string(r)]; !ok {
			fail("roles.%s is missing", r)
		}
	}
	if _, ok := c.Formulas[DefaultFormula]; !ok {
		fail("formulas.%s is missing", DefaultFormula)
	}
	for _, name := range sortedKeys(c.Formulas) {
		if err := c.Formulas[name].validate(); err != nil {
			fail("formulas.%s: %v", name, err)
		}
	}
	for name, v := range map[string]string{
		"vcs.main": c.VCS.Main, "vcs.landing_name": c.VCS.LandingName, "vcs.landing_email": c.VCS.LandingEmail,
	} {
		if strings.TrimSpace(v) == "" {
			fail("%s must not be empty", name)
		}
	}
	return errors.Join(errs...)
}

func fallbackLoops(profiles map[string]Profile, start string) bool {
	seen := map[string]bool{start: true}
	for name := profiles[start].Fallback; name != ""; name = profiles[name].Fallback {
		if seen[name] {
			return true
		}
		seen[name] = true
	}
	return false
}

func (f Formula) validate() error {
	if len(f.Steps) == 0 {
		return errors.New("has no steps")
	}
	index := map[string]int{}
	for i, s := range f.Steps {
		if s.Name == "" {
			return fmt.Errorf("step %d has no name", i+1)
		}
		if _, dup := index[s.Name]; dup {
			return fmt.Errorf("step %q appears twice", s.Name)
		}
		index[s.Name] = i
	}
	for _, s := range f.Steps {
		for _, n := range s.Needs {
			if _, ok := index[n]; !ok {
				return fmt.Errorf("step %q needs unknown step %q", s.Name, n)
			}
		}
	}
	// Depth-first search for a cycle through the needs edges.
	const (
		unvisited = iota
		visiting
		done
	)
	mark := make([]int, len(f.Steps))
	var visit func(i int) error
	visit = func(i int) error {
		switch mark[i] {
		case visiting:
			return fmt.Errorf("step %q needs itself through a cycle", f.Steps[i].Name)
		case done:
			return nil
		}
		mark[i] = visiting
		for _, n := range f.Steps[i].Needs {
			if err := visit(index[n]); err != nil {
				return err
			}
		}
		mark[i] = done
		return nil
	}
	for i := range f.Steps {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

// Write prints the settings as TOML.
func (c Operator) Write(w io.Writer) error {
	return toml.NewEncoder(w).Encode(c)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
