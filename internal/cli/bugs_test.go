package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
)

// recordSweepAt opens the tracker directly with a clock fixed at at and
// records a sweep of commit with clauses, so a bug it files is filed at
// exactly at (S.sweep.3).
func recordSweepAt(t *testing.T, state, commit string, at time.Time, clauses ...tracker.SweepClause) {
	t.Helper()
	tr, err := tracker.Open(state, tracker.Options{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.RecordSweep(commit, at, clauses); err != nil {
		t.Fatal(err)
	}
}

// bugsSection returns the lines of shed status's bugs section, S.sweep.4
// adds after the amendments line: either one line "bugs: none" or a "bugs:"
// header followed by one line per open bug, with runs of whitespace
// collapsed. It is always the last part of status's output.
func bugsSection(t *testing.T, out string) []string {
	t.Helper()
	lines := collapsed(out)
	for i, line := range lines {
		if line == "bugs: none" || line == "bugs:" {
			return lines[i:]
		}
	}
	t.Fatalf("status has no bugs section:\n%s", out)
	return nil
}

// TestStatusListsOpenBugs checks that shed status prints "bugs: none" when
// no bug is open; that once bugs are filed it lists each open bug, oldest
// filed first, with its clause, the commit it was filed at and how long it
// has been open, written as under S.owner.15; that a closed bug is not
// listed; and that shed tracker rebuild gives back the same list.
//
//shed:proves S.sweep.4
func TestStatusListsOpenBugs(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	if got, want := bugsSection(t, mustRun(t, r.Dir, "status")), []string{"bugs: none"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bugs section with none open = %v, want %v", got, want)
	}

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	recordSweepAt(t, state, "commit1", base, tracker.SweepClause{Clause: "S.core.2", Pass: false, Output: "boom"})
	recordSweepAt(t, state, "commit2", base.Add(time.Hour), tracker.SweepClause{Clause: "S.core.3", Pass: false, Output: "bang"})

	status := func(now time.Time) []string {
		t.Helper()
		return bugsSection(t, runAt(t, r.Dir, state, now, env.status))
	}

	now := base.Add(26*time.Hour + 5*time.Minute + 10*time.Second)
	want := []string{"bugs:", "S.core.2 commit1 wait 26h5m", "S.core.3 commit2 wait 25h5m"}
	if got := status(now); !reflect.DeepEqual(got, want) {
		t.Errorf("bugs section with two open =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Closing S.core.2's bug leaves only S.core.3's listed.
	recordSweepAt(t, state, "commit3", base.Add(2*time.Hour),
		tracker.SweepClause{Clause: "S.core.2", Pass: true},
		tracker.SweepClause{Clause: "S.core.3", Pass: false, Output: "bang"})
	want = []string{"bugs:", "S.core.3 commit2 wait 25h5m"}
	if got := status(now); !reflect.DeepEqual(got, want) {
		t.Errorf("bugs section after closing one =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	mustRun(t, r.Dir, "tracker", "rebuild")
	if got := status(now); !reflect.DeepEqual(got, want) {
		t.Errorf("bugs section after a rebuild =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
