package factory

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// The spec on main lists S.zz.1 in basics.md, before core.md, and core.md
// lists S.core.3 before S.core.2, so document order differs from the
// order of the IDs.
const (
	entangledBasics = "# Basics\n\n- **S.zz.1** (H.greet.1) The tool runs.\n"
	entangledCore   = "# Core\n\n" +
		"- **S.core.1** (H.greet.1) Running the tool prints hello.\n" +
		"- **S.core.3** (H.greet.1) The tool exits zero.\n" +
		"- **S.core.2** (H.greet.1) The tool prints to standard output.\n"
)

var specID = regexp.MustCompile(`\bS\.[a-z][a-z0-9-]*\.[0-9]+\b`)

//shed:proves S.fp.5
func TestSealingReportsEntanglement(t *testing.T) {
	r := projectWith(t, map[string]string{"spec/basics.md": entangledBasics, "spec/core.md": entangledCore})
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	opened := func(title string) string {
		t.Helper()
		change, err := f.Repo.NewUnit(ctx, title)
		must(t, err)
		must(t, f.Tracker.OpenUnit(change, title, unit.Painter))
		return change
	}
	main, err := f.mainSet(ctx)
	must(t, err)
	seal := func(change string, fp tracker.Footprint) {
		t.Helper()
		must(t, f.Tracker.Seal(change, "main1", "unitcommit", fp, unit.Committee, "consensus", onMain(main)))
	}
	move := func(change string, to ...unit.State) {
		t.Helper()
		for _, s := range to {
			must(t, f.Tracker.Move(change, s, unit.Shed, "moving on"))
		}
	}

	// Units open in this order, and seal in another.
	a := opened("Implementing, sealed twice")
	b := opened("Still proposed")
	reopened := opened("Reopened")
	change := opened("The newly sealed unit")
	c := opened("Sharing only a horizon clause at its last seal")
	d := opened("Queued")
	v := opened("Verifying")
	archived := opened("Archived")

	must(t, f.Tracker.SetFootprint(b, tracker.Footprint{Modifies: []string{"S.core.2"}}, unit.Painter, "declared", onMain(main)))
	seal(reopened, tracker.Footprint{Modifies: []string{"S.core.2"}, Depends: []string{"S.core.1"}})
	must(t, f.Tracker.Reopen(reopened, unit.Mechanic, "the spec is wrong", false))
	must(t, f.Tracker.SetFootprint(archived, tracker.Footprint{Modifies: []string{"S.core.3"}}, unit.Painter, "declared", onMain(main)))
	must(t, f.Tracker.Archive(archived, unit.Rejected, unit.Committee, "rejected"))

	seal(d, tracker.Footprint{Modifies: []string{"S.core.4", "S.zz.1", "S.core.5", "S.core.3"}})
	move(d, unit.Implementing, unit.Verifying, unit.Queued)
	seal(v, tracker.Footprint{Modifies: []string{"S.core.9"}, Depends: []string{"S.core.3"}})
	move(v, unit.Implementing, unit.Verifying)
	seal(c, tracker.Footprint{Modifies: []string{"S.core.2"}})
	must(t, f.Tracker.Reopen(c, unit.Mechanic, "rethink", false))
	seal(c, tracker.Footprint{Modifies: []string{"S.core.9"}, Advances: []string{"H.greet.2"}})
	seal(a, tracker.Footprint{Modifies: []string{"S.core.9"}})
	must(t, f.Tracker.Reopen(a, unit.Mechanic, "rethink", false))
	seal(a, tracker.Footprint{Modifies: []string{"S.core.4", "S.core.2"}, Depends: []string{"S.core.1"}})
	move(a, unit.Implementing)

	// The newly sealed unit changes S.zz.1, S.core.3 and S.core.2 and adds
	// S.core.5 before S.core.4.
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/basics.md", strings.Replace(entangledBasics, "runs.", "runs anywhere.", 1))
	write(t, dir, "spec/core.md", strings.NewReplacer("exits zero.", "exits zero on success.", "standard output.", "standard output only.").Replace(entangledCore)+
		"- **S.core.5** (H.greet.2) Running the tool with --bye prints goodbye.\n"+
		"- **S.core.4** (H.greet.2) Running the tool with --wave waves.\n")
	must(t, f.Declare(ctx, change, "", []string{"S.core.1"}, []string{"H.greet.2"}, unit.Painter))

	before := map[string]tracker.Unit{}
	for _, other := range []string{a, b, reopened, c, d, v, archived} {
		u, err := f.Tracker.Unit(other)
		must(t, err)
		before[other] = u
	}

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s", out)
	}

	u, err := f.Tracker.Unit(change)
	must(t, err)
	wantModifies := []string{"S.zz.1", "S.core.3", "S.core.2", "S.core.5", "S.core.4"}
	if u.State != unit.Sealed || u.Seal == nil || u.Seal.Change != change {
		t.Errorf("the newly sealed unit = %+v, want it sealed", u)
	}
	if got := slices.Sorted(slices.Values(u.Footprint.Modifies)); !slices.Equal(got, slices.Sorted(slices.Values(wantModifies))) ||
		!slices.Equal(u.Footprint.Depends, []string{"S.core.1"}) || !slices.Equal(u.Footprint.Advances, []string{"H.greet.2"}) {
		t.Errorf("sealed footprint = %+v", u.Footprint)
	}
	for other, was := range before {
		now, err := f.Tracker.Unit(other)
		must(t, err)
		if now.State != was.State || now.Bounces != was.Bounces || !slices.Equal(now.Footprint.Modifies, was.Footprint.Modifies) ||
			!slices.Equal(now.Footprint.Depends, was.Footprint.Depends) {
			t.Errorf("unit %s changed from %+v to %+v", unit.Short(other), was, now)
		}
	}

	events, err := f.Tracker.Events(change)
	must(t, err)
	var advisories []string
	for _, e := range events {
		if line := tracker.Describe(e); strings.Contains(line, "entangle") {
			advisories = append(advisories, line)
		}
	}
	want := []struct {
		unit    string
		clauses []string
	}{
		{a, []string{"S.core.1", "S.core.2", "S.core.4"}},
		{d, []string{"S.zz.1", "S.core.3", "S.core.5", "S.core.4"}},
		{v, []string{"S.core.3"}},
	}
	if len(advisories) != len(want) {
		t.Fatalf("unit log advisories:\n%s\nwant one for each of %s, %s and %s", strings.Join(advisories, "\n"), unit.Short(a), unit.Short(d), unit.Short(v))
	}
	for i, w := range want {
		line := advisories[i]
		if !strings.Contains(line, unit.Short(w.unit)) {
			t.Errorf("advisory %d = %q, want it to name %s", i+1, line, unit.Short(w.unit))
		}
		if got := specID.FindAllString(line, -1); !slices.Equal(got, w.clauses) {
			t.Errorf("advisory %d = %q lists %v, want %v", i+1, line, got, w.clauses)
		}
	}
}
