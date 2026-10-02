package factory

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

func exhaustedHorizon() string {
	return strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon, realised) The tool says goodbye.", 1)
}

//shed:proves S.frame.5 S.serve.1 S.serve.4
func TestServeFramesAfterLandingAndOffersAcceptedClauses(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[vcs]\nremote = \"origin\"\n")
	everyone(t, fake)
	fake.on(unit.FrameBuilder, "frame", func(turn session.Turn) session.Result {
		data, err := os.ReadFile(filepath.Join(turn.Dir, "horizon.md"))
		must(t, err)
		if !strings.Contains(string(data), "(soon, realised) The tool says goodbye.") {
			t.Error("framing started before main realised the work")
		}
		write(t, turn.Dir, "horizon.md", string(data)+"\n- **H.greet.4** (soon, refines H.greet.3) The tool greets in French.\n")
		return done("framed")
	})
	var log bytes.Buffer
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	units, err := f.Tracker.Units()
	must(t, err)
	if len(units) != 2 || units[0].State != unit.Landed || units[1].OpenedBy != unit.FrameBuilder || units[1].State != unit.Proposed {
		t.Fatalf("units = %+v; log: %s", units, &log)
	}
	framing := units[1]
	if !strings.Contains(log.String(), "shed frame -accept "+unit.Short(framing.Change)) {
		t.Fatalf("log: %s", &log)
	}
	// A fresh scheduler sees the persisted draft, and explains its next action.
	log.Reset()
	must(t, f.Tracker.Rebuild())
	must(t, f.Serve(ctx, ServeOptions{Once: true, Log: &log}))
	if len(fake.ran(unit.FrameBuilder)) != 1 || !strings.Contains(log.String(), "waits for owner acceptance") {
		t.Fatalf("frames=%d; log: %s", len(fake.ran(unit.FrameBuilder)), &log)
	}
	accepted, err := f.AcceptFraming(ctx, framing.Change)
	must(t, err)
	if !accepted.Landed() {
		t.Fatalf("accept = %+v", accepted)
	}
	fake.on(unit.Painter, "propose", func(turn session.Turn) session.Result {
		if !strings.Contains(turn.Bundle, "H.greet.4 (soon) The tool greets in French.") {
			t.Errorf("painter lacks accepted clause: %s", turn.Bundle)
		}
		return done("nothing")
	})
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if len(fake.ran(unit.Painter)) != 2 {
		t.Fatal("painter did not resume after acceptance")
	}
}

//shed:proves S.frame.5
func TestAutomaticFramingAttemptsPersistAndFollowTierOrder(t *testing.T) {
	h := strings.Replace(exhaustedHorizon(), "- **H.greet.3**", "- **H.greet.4** (eventual) The tool sings.\n- **H.greet.3**", 1)
	r := projectWith(t, map[string]string{"horizon.md": h, "spec/core.md": goodbyeSpec})
	fake := newFake(t)
	f := open(t, r, fake, "")
	var got []string
	fake.on(unit.FrameBuilder, "frame", func(turn session.Turn) session.Result {
		fields := strings.Fields(turn.Prompt)
		got = append(got, fields[1])
		if fields[1] == "H.greet.3" {
			write(t, turn.Dir, "scratch.txt", "invalid framing")
			return done("framed")
		}
		return done("nothing")
	})
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if !slices.Equal(got, []string{"H.greet.3", "H.greet.4"}) {
		t.Fatalf("order = %v", got)
	}
	must(t, f.Tracker.Rebuild())
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if len(got) != 2 {
		t.Fatalf("repeated attempts after rebuild: %v", got)
	}
	// A new main revision permits fresh attempts.
	landOther(t, f, "Readme", map[string]string{"README.md": "Greeting tool\n"})
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if !slices.Equal(got, []string{"H.greet.3", "H.greet.4", "H.greet.3", "H.greet.4"}) {
		t.Fatalf("attempts = %v", got)
	}
}

//shed:proves S.frame.5 S.serve.6
func TestAutomaticFramingHonoursBudgetAndPendingParents(t *testing.T) {
	h := exhaustedHorizon() + "\n- **H.greet.4** (eventual) The tool sings.\n"
	r := projectWith(t, map[string]string{"horizon.md": h, "spec/core.md": goodbyeSpec})
	fake := newFake(t)
	f := open(t, r, fake, "[budget]\nper_day_usd = 0.05\n")
	fake.on(unit.FrameBuilder, "frame", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "horizon.md", h+"- **H.greet.5** (soon, refines H.greet.3) The tool greets in French.\n")
		return done("framed")
	})
	fr, err := f.Frame(ctx, "H.greet.3")
	must(t, err)
	if !fr.Kept() {
		t.Fatalf("frame = %+v", fr)
	}
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if len(fake.ran(unit.FrameBuilder)) != 1 {
		t.Fatal("dispatched while daily budget was spent")
	}
	f.Operator.Budget.PerDayUSD = 0
	fake.on(unit.FrameBuilder, "frame", func(turn session.Turn) session.Result {
		if !strings.Contains(turn.Prompt, "H.greet.4") {
			t.Errorf("pending parent was chosen: %s", turn.Prompt)
		}
		return done("nothing")
	})
	must(t, f.Serve(ctx, ServeOptions{Once: true}))
	if len(fake.ran(unit.FrameBuilder)) != 2 {
		t.Fatal("waiting framing prevented another parent from being framed")
	}
}

//shed:proves S.frame.5
func TestFramingWaitsForAssignedWorkToBeRealised(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	propose(t, f)
	gap, err := f.Gap(ctx)
	must(t, err)
	if len(gap) != 0 {
		t.Fatal("the assigned clause remains available to painters")
	}
	units, err := f.Tracker.Units()
	must(t, err)
	s := &scheduler{f: f, busy: map[string]bool{}}
	must(t, f.startFraming(ctx, s, units))
	if s.started != 0 {
		t.Fatal("framing started while main still has unrealised soon work")
	}
	events, err := f.Tracker.Events("")
	must(t, err)
	if len(events) != 0 {
		t.Fatalf("claimed framing before work was realised: %+v", events)
	}
}
