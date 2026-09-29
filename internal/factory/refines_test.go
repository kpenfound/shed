package factory

import (
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

// refinesHorizon is a main horizon whose near and soon clauses refine
// distant and eventual ones.
const refinesHorizon = `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool waves.
- **H.greet.5** (eventual) The tool sings.
- **H.greet.6** (near, refines H.greet.4) The tool waves hello.
- **H.greet.7** (eventual) The tool dances.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`

// TestContestedReasonNamesRefinedParent checks that the reason of a move to
// contested follows each clause counted at the tier only because of its
// refines tag with the clauses at whose tier it counts, as shed diff names
// them, and that a clause counted at the tier by its own tier names none.
//
//shed:proves S.shed.19
func TestContestedReasonNamesRefinedParent(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	landOther(t, f, "Refine", map[string]string{"horizon.md": refinesHorizon})

	// reason debates a proposal carrying a horizon and returns the reason
	// of its move to contested.
	reason := func(horizon string) string {
		t.Helper()
		change := proposeHorizon(t, f, horizon)
		if out, err := f.Debate(ctx, change); err != nil || out != Contested {
			u, _ := f.Tracker.Unit(change)
			t.Fatalf("debate = %s, %v: %s", out, err, u.Reason)
		}
		ev := lastMoveOf(t, f, change)
		if ev.To != unit.Contested || ev.Actor != unit.Shed {
			t.Fatalf("the move = %+v", ev)
		}
		return ev.Reason
	}
	// after is the part of the reason that follows a named clause, up to
	// the next named clause.
	after := func(reason, id, next string) string {
		t.Helper()
		i := strings.Index(reason, id)
		if i < 0 {
			t.Fatalf("the reason does not name %s: %q", id, reason)
		}
		rest := reason[i+len(id):]
		if next != "" {
			j := strings.Index(rest, next)
			if j < 0 {
				t.Fatalf("the reason does not name %s after %s: %q", next, id, reason)
			}
			rest = rest[:j]
		}
		return rest
	}

	// H.greet.2 gains a tag naming an eventual clause, so it counts at
	// eventual only because of its tag. H.greet.6 moves its tag from one
	// eventual clause to another. H.greet.7 counts at eventual by its own
	// tier on main, though its new tag also names an eventual clause.
	// H.greet.8 counts only at distant, below the tier.
	got := reason(strings.NewReplacer(
		"(soon) The tool says goodbye.", "(soon, refines H.greet.5) The tool says goodbye.",
		"(near, refines H.greet.4) The tool waves hello.", "(near, refines H.greet.5) The tool waves hello.",
		"(eventual) The tool dances.", "(soon, refines H.greet.4) The tool dances.\n- **H.greet.8** (near, refines H.greet.3) The tool greets in French.",
	).Replace(refinesHorizon))
	if !strings.Contains(got, "eventual") {
		t.Errorf("the reason does not name the tier: %q", got)
	}
	if s := after(got, "H.greet.2", "H.greet.6"); !strings.Contains(s, "H.greet.5 (eventual)") {
		t.Errorf("H.greet.2 is not followed by H.greet.5 (eventual): %q", got)
	}
	s := after(got, "H.greet.6", "H.greet.7")
	old, cur := strings.Index(s, "H.greet.4 (eventual)"), strings.Index(s, "H.greet.5 (eventual)")
	if old < 0 || cur < 0 || old > cur {
		t.Errorf("H.greet.6 is not followed by H.greet.4 (eventual), then H.greet.5 (eventual): %q", got)
	}
	if s := after(got, "H.greet.7", ""); strings.Contains(s, "H.greet.") {
		t.Errorf("H.greet.7, counted at eventual by its own tier, names a clause: %q", got)
	}
	for _, id := range []string{"H.greet.3", "H.greet.8"} {
		if strings.Contains(got, id) {
			t.Errorf("the reason names %s, which is not counted at eventual: %q", id, got)
		}
	}

	// An added clause whose tag names a distant clause counts at distant
	// only because of its tag.
	got = reason(strings.Replace(refinesHorizon, "The tool dances.\n",
		"The tool dances.\n- **H.greet.8** (near, refines H.greet.3) The tool greets in French.\n", 1))
	if s := after(got, "H.greet.8", ""); !strings.Contains(s, "H.greet.3 (distant)") {
		t.Errorf("H.greet.8 is not followed by H.greet.3 (distant): %q", got)
	}
}
