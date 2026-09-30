package factory

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

//shed:proves S.shed.1 S.shed.9 S.shed.20 S.sess.5 S.owner.4
func TestCommitteeIndependentContexts(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, `[shed]
bounce_threshold = 0
[concurrency]
committee = 4
[profiles.second]
agent = "codex"
[committee]
profiles = ["default", "second"]
`)
	change := propose(t, f)
	must(t, f.Tracker.StartRound(change, 1))
	_, err := f.Tracker.Object(change, 1, "spec", []string{"S.core.2"}, "PRIOR-DEBATE")
	must(t, err)
	must(t, f.Tracker.Bounce(change, unit.Shed, "SHARED-REASON"))
	must(t, f.Tracker.Retry(change, "OWNER-HISTORY"))
	must(t, f.Tracker.StartRound(change, 1))
	ids := make([]string, 4)
	for i := range ids {
		ids[i], err = f.Tracker.Object(change, i+1, "spec", []string{"S.core.2"}, fmt.Sprintf("MEMBER-%d-ISSUE", i+1))
		must(t, err)
		must(t, f.Tracker.Answer(ids[i], fmt.Sprintf("MEMBER-%d-ANSWER", i+1), unit.Painter))
	}
	_, err = f.Tracker.AddNotice(change, unit.Committee, "review", "SHARED-NOTICE", unit.Shed)
	must(t, err)
	fresh := false
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		m := member(turn)
		for _, hidden := range []string{"PRIOR-DEBATE", "OWNER-HISTORY", "SHARED-REASON", "SHARED-NOTICE"} {
			if strings.Contains(turn.Bundle, hidden) {
				t.Errorf("member %d received %s", m, hidden)
			}
		}
		for other := 1; other <= 4; other++ {
			for _, suffix := range []string{"ISSUE", "ANSWER"} {
				text := fmt.Sprintf("MEMBER-%d-%s", other, suffix)
				if strings.Contains(turn.Bundle, text) != (other == m && !fresh) {
					t.Errorf("member %d context for %s (fresh %v)", m, text, fresh)
				}
			}
		}
		focus := []string{"correctness and charter compliance", "integration and direct", "scope and whether"}[(m-1)%3]
		if !strings.Contains(turn.SystemPrompt, focus) || !strings.Contains(strings.Join(strings.Fields(turn.SystemPrompt), " "), "any valid objection") {
			t.Errorf("member %d prompt: %s", m, turn.SystemPrompt)
		}
		want := []string{"default", "second"}[(m-1)%2]
		if turn.Profile != want {
			t.Errorf("member %d profile %q, want %q", m, turn.Profile, want)
		}
		if !fresh {
			_, err := call(t, turn, "withdraw", map[string]any{"objection": ids[m-1], "reason": "resolved"})
			must(t, err)
		}
		return done("clean")
	})
	u, err := f.Tracker.Unit(change)
	must(t, err)
	must(t, f.committeeRound(ctx, u, 2, 3, amendment{}))
	pending, err := f.Tracker.Notices(unit.Committee, true)
	must(t, err)
	if len(pending) != 1 {
		t.Fatalf("committee consumed notices: %+v", pending)
	}
	fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
		for _, text := range []string{"PRIOR-DEBATE", "MEMBER-1-ISSUE", "MEMBER-4-ANSWER", "OWNER-HISTORY"} {
			if !strings.Contains(turn.Bundle, text) {
				t.Errorf("painter lacks %s", text)
			}
		}
		return done("replied")
	})
	must(t, f.reply(ctx, u, 2, amendment{}))
	must(t, f.Tracker.Bounce(change, unit.Shed, "SHARED-REASON"))
	must(t, f.Tracker.Retry(change, "OWNER-HISTORY"))
	fresh = true
	u, err = f.Tracker.Unit(change)
	must(t, err)
	must(t, f.committeeRound(ctx, u, 1, 3, amendment{}))
	record, err := f.record(change)
	must(t, err)
	if !strings.Contains(record, "PRIOR-DEBATE") || !strings.Contains(record, "MEMBER-4-ISSUE") {
		t.Fatal("audit record lost history")
	}
}
