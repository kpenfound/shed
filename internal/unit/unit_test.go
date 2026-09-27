package unit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

//shed:proves S.unit.2 S.unit.3
func TestTransitions(t *testing.T) {
	allowed := map[string]bool{
		"proposed>sealed": true, "proposed>archived": true, "proposed>contested": true,
		"sealed>implementing": true, "implementing>verifying": true,
		"verifying>queued": true, "verifying>implementing": true, "queued>landed": true,
		"sealed>proposed": true, "implementing>proposed": true, "verifying>proposed": true, "queued>proposed": true,
		"contested>proposed": true, "contested>archived": true,
	}
	for _, from := range States {
		for _, to := range States {
			key := fmt.Sprintf("%s>%s", from, to)
			if got := CanMove(from, to); got != allowed[key] {
				t.Errorf("CanMove(%s, %s) = %v", from, to, got)
			}
		}
	}
	for _, s := range States {
		if s.Terminal() != (s == Landed || s == Archived) {
			t.Errorf("%s terminal = %v", s, s.Terminal())
		}
		if s.Terminal() && slices.ContainsFunc(States, func(to State) bool { return CanMove(s, to) }) {
			t.Errorf("terminal state %s can move", s)
		}
	}
	if _, err := ParseState("done"); err == nil {
		t.Error(`ParseState("done") succeeded`)
	}
}

//shed:proves S.unit.5
func TestReopenIsAMoveBackToProposed(t *testing.T) {
	for _, from := range States {
		want := from == Sealed || from == Implementing || from == Verifying || from == Queued
		if got := IsReopen(from, Proposed); got != want {
			t.Errorf("IsReopen(%s, proposed) = %v", from, got)
		}
	}
	if IsReopen(Verifying, Implementing) {
		t.Error("verifying to implementing counted as a reopen")
	}
}

//shed:proves S.unit.1
func TestChangeIDs(t *testing.T) {
	for _, ok := range []string{"qpvuntsmwlqt", strings.Repeat("k", 32), "zzzzyyyyxxxxwwww"} {
		if err := ValidChangeID(ok); err != nil {
			t.Errorf("ValidChangeID(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "qpvuntsm", "qpvuntsmwlqA", "abcdefabcdef", "qpvuntsm-wlqt", "0123456789ab"} {
		if err := ValidChangeID(bad); err == nil {
			t.Errorf("ValidChangeID(%q) succeeded", bad)
		}
	}
	if Short(strings.Repeat("k", 32)) != strings.Repeat("k", 12) {
		t.Error("Short does not cut to 12")
	}
}
