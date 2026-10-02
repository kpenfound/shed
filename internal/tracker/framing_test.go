package tracker

import "testing"

//shed:proves S.frame.5
func TestFramingClaimSurvivesRestartAndRebuild(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{})
	claim := func(tr *Tracker, clause, commit string, want bool) {
		t.Helper()
		got, err := tr.ClaimFraming(clause, commit)
		must(t, err)
		if got != want {
			t.Fatalf("claim %s at %s = %v, want %v", clause, commit, got, want)
		}
	}
	// A crash between the claim and session dispatch must not cause a retry.
	claim(tr, "H.greet.3", "main-a", true)
	must(t, tr.Close())
	tr = open(t, dir, Options{})
	claim(tr, "H.greet.3", "main-a", false)
	claim(tr, "H.greet.3", "main-b", true)
	claim(tr, "H.greet.4", "main-b", true)
	must(t, tr.Rebuild())
	claim(tr, "H.greet.3", "main-a", false)
	claim(tr, "H.greet.3", "main-b", false)
	claim(tr, "H.greet.4", "main-b", false)
	events, err := tr.Events("")
	must(t, err)
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	for _, ev := range events {
		if ev.Kind != FrameAttempted || ev.Actor != "frame-builder" || ev.Commit == "" || ev.Clause == "" {
			t.Errorf("event = %+v", ev)
		}
	}
}
