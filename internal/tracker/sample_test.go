package tracker

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/unit"
)

// landing opens, seals, queues and lands a unit, recording whether it is a
// horizon amendment.
func landing(t *testing.T, tr *Tracker, change string, amendment bool) {
	t.Helper()
	must(t, tr.OpenUnit(change, "Unit", unit.Painter))
	through(t, tr, change, unit.Sealed, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(change, "commit-"+change[:4], Footprint{}, amendment, unit.Wheelbuilder, "landed on main"))
	if u := get(t, tr, change); u.State != unit.Landed {
		t.Errorf("unit %s is %s after landing, want landed", unit.Short(change), u.State)
	}
}

// sampledNow lists the units sampled since the last recorded inbox.
func sampledNow(t *testing.T, tr *Tracker) string {
	t.Helper()
	units, err := tr.Sampled()
	must(t, err)
	var out []string
	for _, u := range units {
		out = append(out, unit.Short(u.Change))
	}
	return strings.Join(out, ", ")
}

// changeID is a valid change ID of one repeated letter.
func changeID(letter byte) string {
	return strings.Repeat(string(letter), 24)
}

// landingEvent returns the logged landing event of a unit.
func landingEvent(t *testing.T, dir, change string) map[string]any {
	t.Helper()
	for _, ev := range readLog(t, dir) {
		if ev["unit"] == change && ev["to"] == string(unit.Landed) {
			return ev
		}
	}
	t.Fatalf("no landing event for %s", unit.Short(change))
	return nil
}

//shed:proves S.owner.11 S.owner.12
func TestLandingSamplesHorizonAmendments(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{SampleEvery: 2})
	k, l, m, n, o := changeID('k'), changeID('l'), changeID('m'), changeID('n'), changeID('o')

	// The first horizon amendment is not sampled, a landing that is not a
	// horizon amendment does not count, and the second amendment is.
	landing(t, tr, k, true)
	landing(t, tr, l, false)
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after one amendment = %q, want none", got)
	}
	landing(t, tr, m, true)
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after two amendments = %q, want %q", got, want)
	}

	// Every landing records whether it is a horizon amendment, and a
	// sampled one records that too.
	for change, want := range map[string]bool{k: true, l: false, m: true} {
		ev := landingEvent(t, dir, change)
		if got, ok := ev["horizon_amendment"]; !ok || got != want {
			t.Errorf("landing of %s records horizon_amendment %v (present %v), want %v", unit.Short(change), got, ok, want)
		}
		if sampled := ev["sampled"] == true; sampled != (change == m) {
			t.Errorf("landing of %s records sampled %v", unit.Short(change), sampled)
		}
	}

	// The count survives a rebuild. With sampling off, a horizon
	// amendment is not sampled but still counts, and changing the setting
	// does not restart the count: the fourth amendment is sampled.
	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after a rebuild = %q, want %q", got, want)
	}
	tr.Close()
	tr = open(t, dir, Options{SampleEvery: 0})
	landing(t, tr, n, true)
	tr.Close()
	tr = open(t, dir, Options{SampleEvery: 2})
	landing(t, tr, o, true)
	want := unit.Short(m) + ", " + unit.Short(o)
	if got := sampledNow(t, tr); got != want {
		t.Errorf("sampled after the setting changed = %q, want %q", got, want)
	}

	// The inbox lists only units sampled since the last recorded inbox,
	// and that survives a rebuild.
	_, seq, err := tr.Contested()
	must(t, err)
	must(t, tr.RecordInbox("main1", seq))
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled since the last inbox = %q, want none", got)
	}
	p, q := changeID('p'), changeID('q')
	landing(t, tr, p, true)
	landing(t, tr, q, true)
	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(q); got != want {
		t.Errorf("sampled since the last inbox, after a rebuild = %q, want %q", got, want)
	}
}

//shed:proves S.owner.11
func TestLandingsBeforeRecordingDoNotCount(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{SampleEvery: 2})
	k, l, m := changeID('k'), changeID('l'), changeID('m')
	landing(t, tr, k, true)
	tr.Close()

	// K landed before shed recorded horizon amendments: its landing event
	// records neither.
	path := filepath.Join(dir, LogFile)
	data, err := os.ReadFile(path)
	must(t, err)
	var out bytes.Buffer
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var ev map[string]any
		must(t, json.Unmarshal(line, &ev))
		if ev["unit"] == k && ev["to"] == string(unit.Landed) {
			delete(ev, "horizon_amendment")
			delete(ev, "sampled")
		}
		b, err := json.Marshal(ev)
		must(t, err)
		out.Write(append(b, '\n'))
	}
	must(t, os.WriteFile(path, out.Bytes(), 0o644))

	tr = open(t, dir, Options{SampleEvery: 2})
	must(t, tr.Rebuild())
	landing(t, tr, l, true)
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after the first recorded amendment = %q, want none", got)
	}
	landing(t, tr, m, true)
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after the second recorded amendment = %q, want %q", got, want)
	}
}

//shed:proves S.owner.11
func TestAcceptedFramingsAreNeverSampled(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{SampleEvery: 2})
	k, l, m, n := changeID('k'), changeID('l'), changeID('m'), changeID('n')

	// The accepted framing takes the second place in the count, which
	// samples nothing; the fourth amendment is sampled.
	landing(t, tr, k, true)
	must(t, tr.OpenUnit(l, "Frame H.greet.3 into near and soon clauses", unit.FrameBuilder))
	must(t, tr.LandFraming(l, "commit-l", Footprint{Advances: []string{"H.greet.3"}}, unit.Owner, "the owner accepted the framing"))
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after an accepted framing = %q, want none", got)
	}
	landing(t, tr, m, true)
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after three amendments = %q, want none", got)
	}
	landing(t, tr, n, true)
	if got, want := sampledNow(t, tr), unit.Short(n); got != want {
		t.Errorf("sampled after four amendments = %q, want %q", got, want)
	}

	// The accepted framing's landing records a horizon amendment, not
	// sampled, and that survives a rebuild.
	ev := landingEvent(t, dir, l)
	if ev["horizon_amendment"] != true || ev["sampled"] == true {
		t.Errorf("the accepted framing's landing = %v", ev)
	}
	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(n); got != want {
		t.Errorf("sampled after a rebuild = %q, want %q", got, want)
	}
}
