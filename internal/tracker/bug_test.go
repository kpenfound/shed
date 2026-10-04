package tracker

import (
	"reflect"
	"testing"
	"time"
)

// TestSweepFilesAndClosesBugs checks that recording a sweep also files and
// closes bugs in the tracker, in the same event: a failing clause with no
// open bug gets a new one naming the clause, the commit the sweep checked
// out and the output of its proofs in that sweep; a clause that keeps
// failing gets no other bug and keeps the commit and output it was filed
// with; a clause absent from a sweep leaves its bug as it is; a clause whose
// open bug is found passing has it closed, recording the commit; a clause
// that fails again after its bug closed gets a new one; the sweep's event
// line names each bug it files, with its clause and proof output, and each
// bug it closes; and `shed tracker rebuild` gives back the same bugs, open
// and closed.
//
//shed:proves S.sweep.3 S.track.2 S.track.3 S.track.5
func TestSweepFilesAndClosesBugs(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{})
	first := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	must(t, tr.RecordSweep("commit1", first, []SweepClause{
		{Clause: "S.core.1", Pass: true},
		{Clause: "S.core.2", Pass: false, Output: "TestBye: fail\nno goodbye\n"},
	}))

	bugs, err := tr.Bugs()
	must(t, err)
	if len(bugs) != 1 {
		t.Fatalf("bugs after the first sweep = %+v, want one", bugs)
	}
	if b := bugs[0]; b.Clause != "S.core.2" || b.Commit != "commit1" ||
		b.Output != "TestBye: fail\nno goodbye\n" || !b.Filed.Equal(first) || !b.Open() {
		t.Errorf("filed bug = %+v", b)
	}

	// S.core.2 keeps failing, with different output: no other bug is filed,
	// and the existing one keeps its original commit and output.
	must(t, tr.RecordSweep("commit2", first.Add(time.Hour), []SweepClause{
		{Clause: "S.core.1", Pass: true},
		{Clause: "S.core.2", Pass: false, Output: "a different failure"},
	}))
	bugs, err = tr.Bugs()
	must(t, err)
	if len(bugs) != 1 || bugs[0].Commit != "commit1" || bugs[0].Output != "TestBye: fail\nno goodbye\n" {
		t.Errorf("bugs after a second failing sweep = %+v, want the original bug unchanged", bugs)
	}

	// S.core.2 is absent from a sweep, as a clause removed from the spec is:
	// its bug is left as it is.
	must(t, tr.RecordSweep("commit3", first.Add(2*time.Hour), []SweepClause{
		{Clause: "S.core.1", Pass: true},
	}))
	bugs, err = tr.Bugs()
	must(t, err)
	if len(bugs) != 1 || !bugs[0].Open() || bugs[0].Commit != "commit1" {
		t.Errorf("bugs after a sweep missing the clause = %+v, want the bug untouched", bugs)
	}

	// S.core.2 passes: its bug is closed, recording the commit the sweep
	// checked out.
	must(t, tr.RecordSweep("commit4", first.Add(3*time.Hour), []SweepClause{
		{Clause: "S.core.1", Pass: true},
		{Clause: "S.core.2", Pass: true},
	}))
	bugs, err = tr.Bugs()
	must(t, err)
	if len(bugs) != 1 || bugs[0].Open() || bugs[0].ClosedCommit != "commit4" {
		t.Errorf("bugs after S.core.2 passes = %+v, want it closed at commit4", bugs)
	}

	// S.core.2 fails again: a new bug is filed, oldest first alongside the
	// closed one.
	must(t, tr.RecordSweep("commit5", first.Add(4*time.Hour), []SweepClause{
		{Clause: "S.core.1", Pass: true},
		{Clause: "S.core.2", Pass: false, Output: "fails again"},
	}))
	bugs, err = tr.Bugs()
	must(t, err)
	if len(bugs) != 2 || bugs[0].Commit != "commit1" || bugs[1].Commit != "commit5" || !bugs[1].Open() {
		t.Fatalf("bugs after S.core.2 fails again = %+v, want the closed bug then a new open one", bugs)
	}

	// Each sweep's event line names no unit, and the ones that filed or
	// closed a bug name it with its clause, output and (for a filed bug)
	// the output of its proofs in that sweep.
	var sweepLines []map[string]any
	for _, line := range readLog(t, dir) {
		if line["kind"] != SweepRan {
			continue
		}
		if _, named := line["unit"]; named {
			t.Errorf("sweep event names a unit: %v", line)
		}
		sweepLines = append(sweepLines, line)
	}
	if len(sweepLines) != 5 {
		t.Fatalf("logged %d sweep events, want 5", len(sweepLines))
	}
	filedIn := func(line map[string]any) []any {
		sweep, _ := line["sweep"].(map[string]any)
		filed, _ := sweep["filed"].([]any)
		return filed
	}
	closedIn := func(line map[string]any) []any {
		sweep, _ := line["sweep"].(map[string]any)
		closed, _ := sweep["closed"].([]any)
		return closed
	}
	filed := filedIn(sweepLines[0])
	if len(filed) != 1 {
		t.Fatalf("first sweep line filed = %v, want one bug", filed)
	}
	bugEv, ok := filed[0].(map[string]any)
	if !ok || bugEv["clause"] != "S.core.2" || bugEv["output"] != "TestBye: fail\nno goodbye\n" {
		t.Errorf("filed bug event = %v", filed[0])
	}
	for _, i := range []int{1, 2} {
		if f, c := filedIn(sweepLines[i]), closedIn(sweepLines[i]); len(f) != 0 || len(c) != 0 {
			t.Errorf("sweep line %d filed %v closed %v, want neither", i, f, c)
		}
	}
	closed := closedIn(sweepLines[3])
	if len(closed) != 1 || closed[0] != "S.core.2" {
		t.Errorf("fourth sweep line closed = %v, want [S.core.2]", closed)
	}
	if f := filedIn(sweepLines[4]); len(f) != 1 || f[0].(map[string]any)["clause"] != "S.core.2" {
		t.Errorf("fifth sweep line filed = %v, want a new S.core.2 bug", f)
	}

	// shed tracker rebuild gives back the same bugs, open and closed.
	before := bugs
	must(t, tr.Rebuild())
	after, err := tr.Bugs()
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("bugs after rebuild =\n%+v\nwant\n%+v", after, before)
	}
}
