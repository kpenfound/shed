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

const (
	changeA = "qpvuntsmwlqtqpvuntsmwlqt"
	changeB = "kkkkllllmmmmnnnnoooopppp"
)

func mustRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, code := run(t, dir, args...)
	if code != OK {
		t.Fatalf("shed %s = %d\n%s", strings.Join(args, " "), code, stderr)
	}
	return stdout
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
	r := testrepo.New(t)
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

	mustRun(t, r.Dir, "unit", "open", "-change", changeA, "Say", "goodbye")
	mustRun(t, r.Dir, "unit", "open", "-change", changeB, "Wave")
	seal(t, state, changeA)
	mustRun(t, r.Dir, "unit", "reopen", "-amendment", "qpvu", "the spec is ambiguous")

	want := "UNIT          STATE      BOUNCES  AMENDMENTS  COST   TITLE\n" +
		"qpvuntsmwlqt  contested  1        1           $0.00  Say goodbye\n" +
		"kkkkllllmmmm  proposed   0        0           $0.00  Wave\n" +
		"\nWaiting for the owner:\n" +
		"  qpvuntsmwlqt  Unit qpvuntsmwlqt is contested: bounced 1 time, over the threshold of 0.\n"
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
	r := testrepo.New(t)
	mustRun(t, r.Dir, "unit", "open", "-change", changeA, "Say goodbye")
	seal(t, filepath.Join(r.Dir, DefaultStateDir), changeA)
	mustRun(t, r.Dir, "unit", "move", changeA, "implementing", "picked", "up")
	mustRun(t, r.Dir, "unit", "reopen", changeA, "needs", "debate")

	out := mustRun(t, r.Dir, "unit", "log", "qpvunt")
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
	r := testrepo.New(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	if out := mustRun(t, r.Dir, "unit", "open", "-change", changeA, "Say", "goodbye"); out != "opened qpvuntsmwlqt in proposed\n" {
		t.Errorf("open = %q", out)
	}
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"unit", "open", "Untitled"}, Misused, "unit open needs -change <id> and a title"},
		{[]string{"unit", "open", "-change", "nope", "Bad"}, Failed, `"nope" is not a change ID`},
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
		{"implementing", "moved qpvuntsmwlqt from sealed to implementing"},
		{"verifying", "moved qpvuntsmwlqt from implementing to verifying"},
		{"implementing", "moved qpvuntsmwlqt from verifying to implementing"},
		{"verifying", "moved qpvuntsmwlqt from implementing to verifying"},
		{"queued", "moved qpvuntsmwlqt from verifying to queued"},
	} {
		if out := mustRun(t, r.Dir, "unit", "move", changeA, step[0], "by", "hand"); out != step[1]+"\n" {
			t.Errorf("move to %s = %q", step[0], out)
		}
	}
	if _, stderr, code := run(t, r.Dir, "unit", "move", changeA, "proposed", "x"); code != Misused || !strings.Contains(stderr, "use unit reopen") {
		t.Errorf("move queued to proposed = %d, %q", code, stderr)
	}
	if out := mustRun(t, r.Dir, "unit", "reopen", changeA, "wrong", "spec"); out != "reopened qpvuntsmwlqt; bounces 1, now proposed\n" {
		t.Errorf("reopen = %q", out)
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
	r := testrepo.New(t)
	mustRun(t, r.Dir, "unit", "open", "-change", changeA, "Say goodbye")
	mustRun(t, r.Dir, "unit", "open", "-change", changeB, "Wave")
	before := mustRun(t, r.Dir, "status")
	if out := mustRun(t, r.Dir, "tracker", "rebuild"); out != "rebuilt the tracker from events.jsonl: 2 units\n" {
		t.Errorf("rebuild = %q", out)
	}
	if after := mustRun(t, r.Dir, "status"); after != before {
		t.Errorf("status after rebuild =\n%s\nwant\n%s", after, before)
	}
}
