package tracker

import (
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/unit"
)

// contest bounces a proposed unit past a threshold of 1, so it moves to
// contested.
func contest(t *testing.T, tr *Tracker, change string) {
	t.Helper()
	for get(t, tr, change).State != unit.Contested {
		must(t, tr.Bounce(change, unit.Committee, "objections stand"))
	}
}

// contestedNow lists the contested units, each marked new or not, and
// returns the sequence number the read saw.
func contestedNow(t *testing.T, tr *Tracker) (string, int64) {
	t.Helper()
	units, seq, err := tr.Contested()
	must(t, err)
	var entries []string
	for _, u := range units {
		entry := unit.Short(u.Change)
		if u.New {
			entry += " new"
		}
		entries = append(entries, entry)
	}
	return strings.Join(entries, ", "), seq
}

//shed:proves S.owner.7
func TestContestedUnitsNewSinceTheLastInbox(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{BounceThreshold: 1})
	a, b, c := unit.Short(unitA), unit.Short(unitB), unit.Short(unitC)
	for _, change := range []string{unitA, unitB, unitC} {
		must(t, tr.OpenUnit(change, "Unit", unit.Painter))
	}

	// Before any inbox is recorded, every contested unit is new.
	contest(t, tr, unitA)
	got, seq := contestedNow(t, tr)
	if want := a + " new"; got != want {
		t.Errorf("contested before any inbox = %q, want %q", got, want)
	}
	if last := lastSeq(t, dir); seq != last {
		t.Errorf("the read saw sequence number %d, want the latest event's %d", seq, last)
	}

	// B is contested after the read but before it is recorded, so the
	// owner has not seen it: it is still new.
	contest(t, tr, unitB)
	must(t, tr.RecordInbox("main1", seq))
	got, seq = contestedNow(t, tr)
	if want := a + ", " + b + " new"; got != want {
		t.Errorf("contested after the first inbox = %q, want %q", got, want)
	}

	// Reading without recording changes nothing.
	if again, _ := contestedNow(t, tr); again != got {
		t.Errorf("contested after an unrecorded read = %q, want %q", again, got)
	}

	// The stored sequence number survives a rebuild.
	must(t, tr.RecordInbox("main1", seq))
	must(t, tr.Rebuild())
	if got, _ := contestedNow(t, tr); got != a+", "+b {
		t.Errorf("contested after a rebuild = %q, want %q", got, a+", "+b)
	}
	tr.Close()
	tr = open(t, dir, Options{BounceThreshold: 1})

	// A is retried and contested again, and C is contested for the first
	// time: both are new, and the list keeps the order they became
	// contested.
	must(t, tr.Move(unitA, unit.Proposed, unit.Owner, "try again"))
	contest(t, tr, unitA)
	contest(t, tr, unitC)
	got, seq = contestedNow(t, tr)
	if want := b + ", " + a + " new, " + c + " new"; got != want {
		t.Errorf("contested after a retry = %q, want %q", got, want)
	}
	must(t, tr.RecordInbox("main1", seq))
	if got, _ := contestedNow(t, tr); got != b+", "+a+", "+c {
		t.Errorf("contested after the last inbox = %q, want %q", got, b+", "+a+", "+c)
	}
}

// lastSeq returns the sequence number of the latest logged event.
func lastSeq(t *testing.T, dir string) int64 {
	t.Helper()
	events := readLog(t, dir)
	return int64(events[len(events)-1]["seq"].(float64))
}
