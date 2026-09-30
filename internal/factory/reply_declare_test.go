package factory

import (
	"slices"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

//shed:proves S.shed.5
func TestReplyDeclarationCapturedAndValidated(t *testing.T) {
	for _, mode := range []string{"captured", "clear", "invalid", "failed"} {
		t.Run(mode, func(t *testing.T) {
			r := project(t)
			fake := newFake(t)
			f := open(t, r, fake, "")
			change := propose(t, f)
			u, err := f.Tracker.Unit(change)
			must(t, err)
			fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
				if _, err := call(t, turn, "declare", map[string]any{"depends": []string{"H.greet.2"}}); err == nil {
					t.Error("accepted horizon ID as a dependency")
				}
				deps := []string{"S.core.3"}
				if mode == "clear" {
					deps = []string{}
				}
				_, err := call(t, turn, "declare", map[string]any{"depends": deps})
				must(t, err)
				if mode == "captured" {
					write(t, turn.Dir, "spec/core.md", goodbyeSpec+"- **S.core.3** (H.greet.2) The tool waves goodbye.\n")
					_, err = call(t, turn, "declare", map[string]any{"advances": []string{"H.greet.1", "H.greet.2"}})
					must(t, err)
				}
				during, err := f.Tracker.Unit(change)
				must(t, err)
				if !slices.Equal(during.Footprint.Depends, u.Footprint.Depends) {
					t.Error("declaration applied before capture")
				}
				if mode == "failed" {
					return session.Result{Failure: session.Failure("test failure"), Reason: "reply failed"}
				}
				return done("replied")
			})
			err = f.reply(ctx, u, 1, amendment{})
			if (err != nil) != (mode == "invalid" || mode == "failed") {
				t.Fatalf("reply error = %v", err)
			}
			after, err := f.Tracker.Unit(change)
			must(t, err)
			switch mode {
			case "captured":
				if !slices.Contains(after.Footprint.Modifies, "S.core.3") || !slices.Equal(after.Footprint.Depends, []string{"S.core.3"}) || !slices.Equal(after.Footprint.Advances, []string{"H.greet.1", "H.greet.2"}) {
					t.Fatalf("footprint = %+v", after.Footprint)
				}
			case "clear":
				if len(after.Footprint.Depends) != 0 || !slices.Equal(after.Footprint.Advances, u.Footprint.Advances) {
					t.Fatalf("footprint = %+v", after.Footprint)
				}
			default:
				if !slices.Equal(after.Footprint.Depends, u.Footprint.Depends) || !slices.Equal(after.Footprint.Advances, u.Footprint.Advances) {
					t.Fatalf("invalid or failed reply changed footprint: %+v", after.Footprint)
				}
			}
		})
	}
}
