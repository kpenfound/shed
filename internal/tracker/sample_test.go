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
// horizon amendment. Its seal is an ordinary consensus seal, so a horizon
// amendment it lands is auto-accepted.
func landing(t *testing.T, tr *Tracker, change string, amendment bool) {
	t.Helper()
	must(t, tr.OpenUnit(change, "Unit", unit.Painter))
	through(t, tr, change, unit.Sealed, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(change, "commit-"+change[:4], Footprint{}, amendment, unit.Wheelbuilder, "landed on main"))
	if u := get(t, tr, change); u.State != unit.Landed {
		t.Errorf("unit %s is %s after landing, want landed", unit.Short(change), u.State)
	}
}

// landingApproved opens, seals under the owner's approve, queues and lands a
// unit as a horizon amendment, so its landing is owner-accepted (S.owner.11,
// S.shed.17).
func landingApproved(t *testing.T, tr *Tracker, change string) {
	t.Helper()
	must(t, tr.OpenUnit(change, "Unit", unit.Painter))
	must(t, tr.SealApproved(change, "main1", "unitcommit", Footprint{}, unit.Committee, "the owner approved its horizon amendment", nil))
	through(t, tr, change, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(change, "commit-"+change[:4], Footprint{}, true, unit.Wheelbuilder, "landed on main"))
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
	// horizon amendment also records that it is auto-accepted, since none
	// of these landed by the owner's approve or shed frame -accept.
	for change, want := range map[string]bool{k: true, l: false, m: true} {
		ev := landingEvent(t, dir, change)
		if got, ok := ev["horizon_amendment"]; !ok || got != want {
			t.Errorf("landing of %s records horizon_amendment %v (present %v), want %v", unit.Short(change), got, ok, want)
		}
		if sampled := ev["sampled"] == true; sampled != (change == m) {
			t.Errorf("landing of %s records sampled %v", unit.Short(change), sampled)
		}
		if want {
			if got, ok := ev["owner_accepted"]; !ok || got != false {
				t.Errorf("landing of %s records owner_accepted %v (present %v), want false", unit.Short(change), got, ok)
			}
		} else if _, ok := ev["owner_accepted"]; ok {
			t.Errorf("landing of %s records owner_accepted %v, want it unset since it is not a horizon amendment", unit.Short(change), ev["owner_accepted"])
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
	k, l, m, n, o := changeID('k'), changeID('l'), changeID('m'), changeID('n'), changeID('o')
	landing(t, tr, k, true)
	tr.Close()

	// strip removes keys from a unit's landing event, simulating a landing
	// recorded before shed tracked them.
	path := filepath.Join(dir, LogFile)
	strip := func(change string, keys ...string) {
		t.Helper()
		data, err := os.ReadFile(path)
		must(t, err)
		var out bytes.Buffer
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			var ev map[string]any
			must(t, json.Unmarshal(line, &ev))
			if ev["unit"] == change && ev["to"] == string(unit.Landed) {
				for _, key := range keys {
					delete(ev, key)
				}
			}
			b, err := json.Marshal(ev)
			must(t, err)
			out.Write(append(b, '\n'))
		}
		must(t, os.WriteFile(path, out.Bytes(), 0o644))
	}

	// K landed before shed recorded horizon amendments at all: its landing
	// event records neither, so it does not count.
	strip(k, "horizon_amendment", "sampled", "owner_accepted")

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
	tr.Close()

	// N landed after shed recorded horizon amendments but before it
	// recorded owner acceptance: its event records a horizon amendment but
	// not whether it is owner-accepted, so it counts as auto-accepted and
	// takes the third place in the sampling count.
	tr = open(t, dir, Options{SampleEvery: 2})
	landing(t, tr, n, true)
	tr.Close()
	strip(n, "owner_accepted", "sampled")

	tr = open(t, dir, Options{SampleEvery: 2})
	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after a landing recorded with no owner acceptance = %q, want %q", got, want)
	}
	landing(t, tr, o, true)
	if got, want := sampledNow(t, tr), unit.Short(m)+", "+unit.Short(o); got != want {
		t.Errorf("sampled after the fourth counted amendment = %q, want %q", got, want)
	}
}

//shed:proves S.owner.11
func TestAcceptedFramingsAreNeverSampled(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{SampleEvery: 2})
	k, l, m := changeID('k'), changeID('l'), changeID('m')

	// The accepted framing is owner-accepted and takes no place in the
	// sampling count, so the second auto-accepted amendment is sampled
	// even though a framing landed between the two.
	landing(t, tr, k, true)
	must(t, tr.OpenUnit(l, "Frame H.greet.3 into near and soon clauses", unit.FrameBuilder))
	must(t, tr.LandFraming(l, "commit-l", Footprint{Advances: []string{"H.greet.3"}}, unit.Owner, "the owner accepted the framing"))
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after an accepted framing = %q, want none", got)
	}
	landing(t, tr, m, true)
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after the framing and two auto-accepted amendments = %q, want %q", got, want)
	}

	// The accepted framing's landing records a horizon amendment that is
	// owner-accepted, never sampled, and that survives a rebuild.
	ev := landingEvent(t, dir, l)
	if ev["horizon_amendment"] != true || ev["owner_accepted"] != true || ev["sampled"] == true {
		t.Errorf("the accepted framing's landing = %v", ev)
	}
	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after a rebuild = %q, want %q", got, want)
	}
}

//shed:proves S.owner.11
func TestApprovedAmendmentsAreNeverSampled(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{SampleEvery: 2, BounceThreshold: 1})
	k, l, m, n, o := changeID('k'), changeID('l'), changeID('m'), changeID('n'), changeID('o')

	// A seal that follows the owner's approve lands owner-accepted and
	// takes no place in the sampling count, so the second auto-accepted
	// amendment is sampled even though an approved one landed between them.
	landing(t, tr, k, true)
	landingApproved(t, tr, l)
	if got := sampledNow(t, tr); got != "" {
		t.Errorf("sampled after an approved amendment = %q, want none", got)
	}
	landing(t, tr, m, true)
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after the approved amendment and two auto-accepted amendments = %q, want %q", got, want)
	}

	// The approved amendment's landing records a horizon amendment that is
	// owner-accepted, and never sampled.
	ev := landingEvent(t, dir, l)
	if ev["horizon_amendment"] != true || ev["owner_accepted"] != true || ev["sampled"] == true {
		t.Errorf("the approved amendment's landing = %v", ev)
	}

	// A unit sealed again after the approval seal, as when it is resealed
	// out of the amendment lane, is judged by that later seal alone: an
	// ordinary consensus seal after the approval lands the unit
	// auto-accepted, taking the third place in the sampling count.
	must(t, tr.OpenUnit(n, "Unit", unit.Painter))
	must(t, tr.SealApproved(n, "main1", "unitcommit", Footprint{}, unit.Committee, "the owner approved its horizon amendment", nil))
	must(t, tr.Move(n, unit.Implementing, unit.Mechanic, "dispatched"))
	must(t, tr.Reopen(n, unit.Mechanic, "needs more work", false))
	must(t, tr.Seal(n, "main2", "unitcommit2", Footprint{}, unit.Committee, "consensus", nil))
	through(t, tr, n, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(n, "commit-"+n[:4], Footprint{}, true, unit.Wheelbuilder, "landed on main"))
	if got, want := sampledNow(t, tr), unit.Short(m); got != want {
		t.Errorf("sampled after a reseal lands auto-accepted = %q, want %q", got, want)
	}
	landing(t, tr, o, true)
	if got, want := sampledNow(t, tr), unit.Short(m)+", "+unit.Short(o); got != want {
		t.Errorf("sampled after the fourth auto-accepted amendment = %q, want %q", got, want)
	}

	must(t, tr.Rebuild())
	if got, want := sampledNow(t, tr), unit.Short(m)+", "+unit.Short(o); got != want {
		t.Errorf("sampled after a rebuild = %q, want %q", got, want)
	}
}
