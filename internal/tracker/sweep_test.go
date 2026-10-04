package tracker

import (
	"reflect"
	"testing"
	"time"
)

//shed:proves S.sweep.2
func TestSweepsSurviveRebuild(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{})
	first := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	must(t, tr.RecordSweep("commit1", first, []SweepClause{
		{Clause: "S.core.1", Pass: true}, {Clause: "S.core.2", Pass: false},
	}))
	must(t, tr.RecordSweep("commit2", first.Add(time.Hour), []SweepClause{
		{Clause: "S.core.1", Pass: true}, {Clause: "S.core.2", Pass: true},
	}))

	before, err := tr.Sweeps()
	must(t, err)
	if len(before) != 2 || before[0].Commit != "commit1" || before[1].Commit != "commit2" {
		t.Fatalf("sweeps = %+v, want commit1 then commit2", before)
	}
	if !before[0].Started.Equal(first) || !before[1].Started.Equal(first.Add(time.Hour)) {
		t.Errorf("sweep start times = %v, %v", before[0].Started, before[1].Started)
	}
	if !reflect.DeepEqual(before[0].Clauses, []SweepClause{{Clause: "S.core.1", Pass: true}, {Clause: "S.core.2", Pass: false}}) {
		t.Errorf("first sweep's clauses = %+v", before[0].Clauses)
	}

	// Each sweep is logged as one event naming no unit.
	var logged int
	for _, line := range readLog(t, dir) {
		if line["kind"] == SweepRan {
			logged++
			if _, named := line["unit"]; named {
				t.Errorf("sweep event names a unit: %v", line)
			}
		}
	}
	if logged != 2 {
		t.Errorf("logged %d sweep events, want 2", logged)
	}

	must(t, tr.Rebuild())
	after, err := tr.Sweeps()
	must(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("sweeps after rebuild =\n%+v\nwant\n%+v", after, before)
	}
}
