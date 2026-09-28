package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, code := run(t, dir, args...)
	if code != OK {
		t.Fatalf("shed %s = %d\n%s", strings.Join(args, " "), code, stderr)
	}
	return stdout
}

// openUnit opens a unit through the command line and returns its change ID.
func openUnit(t *testing.T, dir, title string) string {
	t.Helper()
	out := mustRun(t, dir, "unit", "open", title)
	change, ok := strings.CutPrefix(strings.TrimSpace(out), "opened ")
	change, _, _ = strings.Cut(change, " ")
	if !ok || unit.ValidChangeID(change) != nil {
		t.Fatalf("unit open printed %q", out)
	}
	return change
}

// seal seals a unit directly through the tracker, as the shed will.
func seal(t *testing.T, state, change string) {
	t.Helper()
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Seal(change, "main1", tracker.Footprint{}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
}

//shed:proves S.track.1 S.track.9
func TestStatus(t *testing.T) {
	r := testrepo.Colocated(t)
	if out := mustRun(t, r.Dir, "status"); out != "no units\n" {
		t.Errorf("empty status = %q", out)
	}
	state := filepath.Join(r.Dir, DefaultStateDir)
	for _, name := range []string{tracker.DBFile, tracker.LogFile, tracker.SessionsDir} {
		if _, err := os.Stat(filepath.Join(state, name)); err != nil {
			t.Errorf("default state directory lacks %s: %v", name, err)
		}
	}
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\n")

	a := openUnit(t, r.Dir, "Say goodbye")
	b := openUnit(t, r.Dir, "Wave")
	seal(t, state, a)
	mustRun(t, r.Dir, "unit", "reopen", "-amendment", a[:6], "the spec is ambiguous")

	want := "UNIT          STATE      BOUNCES  AMENDMENTS  COST   TITLE\n" +
		unit.Short(a) + "  contested  1        1           $0.00  Say goodbye\n" +
		unit.Short(b) + "  proposed   0        0           $0.00  Wave\n" +
		"\nWaiting for the owner:\n" +
		"  " + unit.Short(a) + "  Unit " + unit.Short(a) + " is contested: bounced 1 time, over the threshold of 0.\n"
	if out := mustRun(t, r.Dir, "status"); out != want {
		t.Errorf("status =\n%s\nwant\n%s", out, want)
	}

	// Another state directory holds another tracker.
	other := filepath.Join(t.TempDir(), "elsewhere")
	if out := mustRun(t, r.Dir, "-state", other, "status"); out != "no units\n" {
		t.Errorf("status in -state = %q", out)
	}
	t.Setenv("SHED_STATE", other)
	if out := mustRun(t, r.Dir, "status"); out != "no units\n" {
		t.Errorf("status in $SHED_STATE = %q", out)
	}
}

//shed:proves S.track.10
func TestUnitLog(t *testing.T) {
	r := testrepo.Colocated(t)
	change := openUnit(t, r.Dir, "Say goodbye")
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	mustRun(t, r.Dir, "unit", "move", change, "implementing", "picked", "up")
	mustRun(t, r.Dir, "unit", "reopen", change, "needs", "debate")

	out := mustRun(t, r.Dir, "unit", "log", change[:8])
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := []string{
		`^1  \S+Z  owner      opened "Say goodbye"$`,
		`^2  \S+Z  committee  proposed -> sealed at main main1: consensus$`,
		`^3  \S+Z  owner      sealed -> implementing: picked up$`,
		`^4  \S+Z  owner      implementing -> proposed: needs debate$`,
	}
	if len(lines) != len(want) {
		t.Fatalf("log =\n%s", out)
	}
	for i, pattern := range want {
		if !regexp.MustCompile(pattern).MatchString(lines[i]) {
			t.Errorf("line %d = %q, want %s", i+1, lines[i], pattern)
		}
	}
}

//shed:proves S.unit.8
func TestUnitCommands(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	changeA := openUnit(t, r.Dir, "Say goodbye")
	short := unit.Short(changeA)
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"unit", "open"}, Misused, "unit open needs a title"},
		{[]string{"unit", "move", changeA, "sealed", "by hand"}, Misused, "units reach sealed through shed, not by hand"},
		{[]string{"unit", "move", changeA, "landed", "by hand"}, Misused, "units reach landed through shed, not by hand"},
		{[]string{"unit", "move", changeA, "archived", "by hand"}, Misused, "units reach archived through shed, not by hand"},
		{[]string{"unit", "move", changeA, "implementing", "skip"}, Failed, "cannot move from proposed to implementing"},
		{[]string{"unit", "move", changeA, "verifying"}, Misused, "unit move needs a unit, a state and a reason"},
		{[]string{"unit", "reopen", changeA, "again"}, Failed, "cannot move from proposed to proposed"},
		{[]string{"unit", "move", "kkkk", "queued", "x"}, Failed, "unit kkkk: not found"},
	} {
		_, stderr, code := run(t, r.Dir, tc.args...)
		if code != tc.code || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v = %d, %q; want %d, %q", tc.args, code, stderr, tc.code, tc.want)
		}
	}

	seal(t, state, changeA)
	for _, step := range [][]string{
		{"implementing", "moved " + short + " from sealed to implementing"},
		{"verifying", "moved " + short + " from implementing to verifying"},
		{"implementing", "moved " + short + " from verifying to implementing"},
		{"verifying", "moved " + short + " from implementing to verifying"},
		{"queued", "moved " + short + " from verifying to queued"},
	} {
		if out := mustRun(t, r.Dir, "unit", "move", changeA, step[0], "by", "hand"); out != step[1]+"\n" {
			t.Errorf("move to %s = %q", step[0], out)
		}
	}
	if _, stderr, code := run(t, r.Dir, "unit", "move", changeA, "proposed", "x"); code != Misused || !strings.Contains(stderr, "use unit reopen") {
		t.Errorf("move queued to proposed = %d, %q", code, stderr)
	}
	if out := mustRun(t, r.Dir, "unit", "reopen", changeA, "wrong", "spec"); out != "reopened "+short+"; bounces 1, now proposed\n" {
		t.Errorf("reopen = %q", out)
	}
}

//shed:proves S.vcs.3
func TestUnitOpenMakesAChange(t *testing.T) {
	r := testrepo.Colocated(t)
	change := openUnit(t, r.Dir, "Say goodbye")
	desc := r.JJ("--ignore-working-copy", "log", "--no-graph", "-r", "change_id("+change+")", "-T", "description.first_line()")
	if desc != "unit: Say goodbye" {
		t.Errorf("change description = %q", desc)
	}
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change[:10]))
	if !strings.HasPrefix(dir, filepath.Join(evalDir(t, r.Dir), DefaultStateDir, "workspaces")+string(filepath.Separator)) {
		t.Errorf("workspace %s is not under the state directory", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "charter.md")); err != nil {
		t.Errorf("workspace: %v", err)
	}

	plain := testrepo.Minimal(t)
	if _, stderr, code := run(t, plain.Dir, "unit", "open", "Nowhere"); code != Failed || !strings.Contains(stderr, "not a colocated jj repository") {
		t.Errorf("unit open outside a jj repository = %d, %q", code, stderr)
	}
	if out := mustRun(t, plain.Dir, "status"); out != "no units\n" {
		t.Errorf("a failed open left a unit: %q", out)
	}
}

func evalDir(t *testing.T, dir string) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

//shed:proves S.vcs.7
func TestLandCommand(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	change := openUnit(t, r.Dir, "Say goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	if err := os.WriteFile(filepath.Join(dir, "bye.txt"), []byte("bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, r.Dir, "land", change); code != Failed || !strings.Contains(stderr, "only queued units land") {
		t.Errorf("land a proposed unit = %d, %q", code, stderr)
	}
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
	}
	out := mustRun(t, r.Dir, "land", change)
	commit := r.GitRemote("rev-parse", "main")
	if out != "landed "+unit.Short(change)+" on main as "+commit+"\n" {
		t.Errorf("land = %q, remote main %s", out, commit)
	}
	if !strings.Contains(r.Git("log", "-1", "--format=%B", commit), "Unit: "+change) {
		t.Error("landed commit does not name the unit")
	}
	if status := mustRun(t, r.Dir, "status"); !strings.Contains(status, "landed") {
		t.Errorf("status after landing:\n%s", status)
	}
}

//shed:proves S.config.3
func TestConfigCommand(t *testing.T) {
	r := testrepo.New(t)
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 7\n")
	out := mustRun(t, r.Dir, "config")
	for _, want := range []string{
		"# " + filepath.Join(r.Dir, ".shed", "config.toml") + "\n",
		"bounce_threshold = 7\n",
		"max_rounds = 3\n",
		`contested_timeout = "72h0m0s"`,
		"[profiles.default]\n",
		"[roles.mechanic]\n",
		`name = "proofs"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("config lacks %q:\n%s", want, out)
		}
	}
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = -2\n")
	if _, stderr, code := run(t, r.Dir, "config"); code != Failed || !strings.Contains(stderr, "shed.bounce_threshold must not be negative") {
		t.Errorf("bad config = %d, %q", code, stderr)
	}
	if _, stderr, code := run(t, r.Dir, "status"); code != Failed || !strings.Contains(stderr, "shed.bounce_threshold") {
		t.Errorf("status with bad config = %d, %q", code, stderr)
	}
}

//shed:proves S.track.5
func TestTrackerRebuildCommand(t *testing.T) {
	r := testrepo.Colocated(t)
	openUnit(t, r.Dir, "Say goodbye")
	openUnit(t, r.Dir, "Wave")
	before := mustRun(t, r.Dir, "status")
	if out := mustRun(t, r.Dir, "tracker", "rebuild"); out != "rebuilt the tracker from events.jsonl: 2 units\n" {
		t.Errorf("rebuild = %q", out)
	}
	if after := mustRun(t, r.Dir, "status"); after != before {
		t.Errorf("status after rebuild =\n%s\nwant\n%s", after, before)
	}
}
