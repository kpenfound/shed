package factory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

func member(turn session.Turn) int {
	var round, m int
	_, _ = fmtSscanf(turn.Step, &round, &m)
	return m
}

//shed:proves S.shed.1 S.shed.5 S.shed.8 S.shed.9
func TestDebateSealsOnConsensus(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := propose(t, f)
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)

	var mu sync.Mutex
	objection := ""
	views := map[string]bool{}
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		spec, _ := os.ReadFile(filepath.Join(turn.Dir, "spec", "core.md"))
		mu.Lock()
		views[turn.Step[:len("debate round 1")]+string(spec)] = true
		mu.Unlock()
		if turn.Writable {
			t.Error("a committee member got a writable directory")
		}
		if member(turn) != 1 {
			return done("clean")
		}
		if strings.HasPrefix(turn.Step, "debate round 1") {
			out, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Say what goodbye prints."})
			must(t, err)
			mu.Lock()
			objection = strings.Fields(out)[1]
			mu.Unlock()
			return done("objecting")
		}
		if !strings.Contains(turn.Bundle, "Answer: It prints goodbye.") || !strings.Contains(turn.Bundle, "Your standing objections") {
			t.Errorf("round 2 bundle lacks the debate record:\n%s", turn.Bundle)
		}
		_, err := call(t, turn, "withdraw", map[string]any{"objection": objection, "reason": "answered"})
		must(t, err)
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
		if !turn.Writable || !strings.Contains(turn.Bundle, objection) {
			t.Errorf("reply turn: writable %v, bundle:\n%s", turn.Writable, turn.Bundle)
		}
		_, err := call(t, turn, "answer", map[string]any{"objection": objection, "text": "It prints goodbye."})
		must(t, err)
		write(t, turn.Dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1))
		return done("replied")
	})

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("debate = %s", out)
	}
	if n := len(fake.ran(unit.Committee)); n != 6 {
		t.Errorf("%d committee sessions, want 3 members for 2 rounds", n)
	}
	if len(views) != 2 {
		t.Errorf("members saw %d versions of the proposal across 2 rounds: %v", len(views), views)
	}
	u, err := f.Tracker.Unit(change)
	must(t, err)
	want := tracker.Footprint{Modifies: []string{"S.core.2"}, Depends: []string{"S.core.1"}, Advances: []string{"H.greet.2"}}
	if u.State != unit.Sealed || u.Seal == nil || u.Seal.Main != main || u.Seal.Change != change {
		t.Errorf("unit = %+v", u)
	}
	if !slices.Equal(u.Footprint.Modifies, want.Modifies) || !slices.Equal(u.Footprint.Depends, want.Depends) || !slices.Equal(u.Footprint.Advances, want.Advances) {
		t.Errorf("footprint = %+v, want %+v", u.Footprint, want)
	}
	objs, err := f.Tracker.Objections(change, -1)
	must(t, err)
	if len(objs) != 1 || objs[0].Standing() || objs[0].Answer != "It prints goodbye." {
		t.Errorf("objections = %+v", objs)
	}
	head, err := f.headSet(ctx, change)
	must(t, err)
	if c, _ := head.Lookup(mustID("S.core.2")); !strings.Contains(c.Text, "the word goodbye") {
		t.Errorf("the painter's revision was not captured: %q", c.Text)
	}
}

//shed:proves S.shed.3 S.shed.10 S.shed.9
func TestCharterVetoRejects(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[vcs]\nremote = \"origin\"\n")
	change := propose(t, f)
	main, _ := f.Repo.MainCommit(ctx)
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 2 {
			_, err := call(t, turn, "object", map[string]any{"kind": "charter", "citations": []string{"C2"}, "text": "Goodbye is shouted."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Rejected {
		t.Fatalf("debate = %s", out)
	}
	if len(fake.ran(unit.Painter)) != 0 {
		t.Error("the painter was asked to answer a veto")
	}
	u, _ := f.Tracker.Unit(change)
	if u.State != unit.Archived || u.Shelf != unit.Rejected {
		t.Errorf("unit = %+v", u)
	}
	if _, err := f.Repo.Workspace(ctx, change); err == nil {
		t.Error("the rejected unit's change was kept")
	}
	if now, _ := f.Repo.MainCommit(ctx); now != main {
		t.Error("archiving moved main")
	}
	entries, err := archive.Read(r.Dir)
	must(t, err)
	if len(entries) != 1 || entries[0].Shelf != unit.Rejected || entries[0].Path != "archive/rejected/"+change+".md" {
		t.Fatalf("archive = %+v", entries)
	}
	for _, want := range []string{"# Say goodbye", "- Citations: C2", "Goodbye is shouted.", "Added S.core.2", "charter, citing C2"} {
		if !strings.Contains(entries[0].Text, want) {
			t.Errorf("entry lacks %q:\n%s", want, entries[0].Text)
		}
	}
	if got := r.GitRemote("rev-parse", vcs.ArchiveRef); got == "" {
		t.Error("the archive was not pushed")
	}
	if r.Git("rev-list", "--max-parents=0", vcs.ArchiveBranch) == r.Git("rev-list", "--max-parents=0", "main") {
		t.Error("the archive shares history with main")
	}
}

//shed:proves S.shed.4 S.shed.10
func TestOffHorizonDefers(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 2\n")
	change := propose(t, f)
	fake.on(unit.Committee, "debate round 1", func(turn session.Turn) session.Result {
		if member(turn) == 3 {
			_, err := call(t, turn, "object", map[string]any{"kind": "horizon", "citations": []string{"H.greet.3"},
				"text": "Farewells are not on the horizon; a clause about greeting in other languages would be."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Committee, "debate round 2", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Deferred {
		t.Fatalf("debate = %s", out)
	}
	entries, err := archive.Read(r.Dir)
	must(t, err)
	if len(entries) != 1 || entries[0].Shelf != unit.Deferred || !strings.Contains(entries[0].Text, "## What would change the decision") ||
		!strings.Contains(entries[0].Text, "greeting in other languages") {
		t.Errorf("archive = %+v", entries)
	}
	if !strings.Contains(archive.Shelf(entries, unit.Deferred), change) || archive.Shelf(entries, unit.Rejected) != "" {
		t.Error("shelves do not hold the entry where it belongs")
	}
}

//shed:proves S.shed.6 S.shed.7 S.shed.5
func TestStandingObjectionsBounce(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 2\nbounce_threshold = 1\n")
	change := propose(t, f)
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 1 && strings.HasPrefix(turn.Step, "debate round 1") {
			_, err := call(t, turn, "object", map[string]any{"kind": "size", "citations": []string{"S.core.2", "S.core.1"},
				"text": "Split the change to S.core.1 from the new clause."})
			must(t, err)
			return done("objecting")
		}
		if member(turn) == 2 && strings.HasPrefix(turn.Step, "debate round 2") {
			objs, _ := f.Tracker.Standing(change)
			if _, err := call(t, turn, "withdraw", map[string]any{"objection": objs[0].ID, "reason": "not mine"}); err == nil {
				t.Error("a member withdrew another's objection")
			}
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Bounced {
		t.Fatalf("first debate = %s", out)
	}
	u, _ := f.Tracker.Unit(change)
	if u.State != unit.Proposed || u.Bounces != 1 || u.Round != 0 {
		t.Errorf("after a bounce = %+v", u)
	}
	if standing, _ := f.Tracker.Standing(change); len(standing) != 0 {
		t.Errorf("objections of the old debate still stand: %+v", standing)
	}
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Contested {
		t.Fatalf("second debate = %s", out)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Contested {
		t.Errorf("unit = %s", u.State)
	}
}

//shed:proves S.shed.2
func TestObjectionsMustCiteResolvingClauses(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\ncommittee = 1\n")
	change := propose(t, f)
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		for _, bad := range []map[string]any{
			{"kind": "spec", "citations": []string{"S.core.9"}, "text": "x"},
			{"kind": "spec", "citations": []string{"S.core"}, "text": "x"},
			{"kind": "spec", "citations": []string{}, "text": "x"},
			{"kind": "style", "citations": []string{"S.core.2"}, "text": "x"},
		} {
			if _, err := call(t, turn, "object", bad); err == nil {
				t.Errorf("objection %v was accepted", bad)
			}
		}
		// A clause the proposal adds can be cited.
		_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2", "H.greet.2"}, "text": "fine"})
		must(t, err)
		return done("objecting")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	if _, err := f.Debate(ctx, change); err != nil {
		t.Fatal(err)
	}
	objs, _ := f.Tracker.Objections(change, -1)
	if len(objs) != 3 {
		t.Errorf("recorded %d objections, want one per round", len(objs))
	}
}

//shed:proves S.fp.1 S.fp.2
func TestFootprintComesFromTheSpecDiff(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := propose(t, f)
	u, _ := f.Tracker.Unit(change)
	if !slices.Equal(u.Footprint.Modifies, []string{"S.core.2"}) || !slices.Equal(u.Footprint.Depends, []string{"S.core.1"}) ||
		!slices.Equal(u.Footprint.Advances, []string{"H.greet.2"}) {
		t.Errorf("declared footprint = %+v", u.Footprint)
	}
	if err := f.Declare(ctx, change, "", []string{"S.core.7"}, nil, unit.Owner); err == nil {
		t.Error("declared a dependency on a clause not on main")
	}
	if err := f.Declare(ctx, change, "Wave goodbye", nil, []string{"H.greet.2"}, unit.Owner); err != nil {
		t.Fatal(err)
	}
	if u, _ := f.Tracker.Unit(change); u.Title != "Wave goodbye" {
		t.Errorf("title = %q", u.Title)
	}

	empty, err := f.Repo.NewUnit(ctx, "Nothing")
	must(t, err)
	must(t, f.Tracker.OpenUnit(empty, "Nothing", unit.Owner))
	out, err := f.Debate(ctx, empty)
	must(t, err)
	if u, _ := f.Tracker.Unit(empty); out != Bounced || !strings.Contains(u.Reason, "changes no spec clause") {
		t.Errorf("a proposal without a spec change = %s, %+v", out, u)
	}
}

//shed:proves S.shed.11
func TestAmendmentLaneHasItsOwnCap(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 3\namendment_rounds = 1\nbounce_threshold = 10\n")
	change := propose(t, f)

	object := false
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if object && member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Say what goodbye prints."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// debate runs a debate and returns its outcome and the committee's turns.
	debate := func(objecting bool) (Outcome, []session.Turn) {
		t.Helper()
		object = objecting
		before := len(fake.ran(unit.Committee))
		out, err := f.Debate(ctx, change)
		must(t, err)
		return out, fake.ran(unit.Committee)[before:]
	}
	told := func(turns []session.Turn, cap int) {
		t.Helper()
		for _, turn := range turns {
			if want := fmt.Sprintf("of at most %d", cap); !strings.Contains(turn.Prompt, want) {
				t.Errorf("%s was not told the cap %d: %q", turn.Step, cap, turn.Prompt)
			}
		}
	}

	out, turns := debate(false)
	if out != Sealed {
		t.Fatalf("first debate = %s", out)
	}
	told(turns, 3)

	// A reopen that requests an amendment puts the unit in the amendment lane.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	replies := len(fake.ran(unit.Painter))
	out, turns = debate(true)
	if out != Bounced {
		t.Fatalf("amendment debate = %s", out)
	}
	if len(turns) != 3 {
		t.Errorf("%d committee sessions in the amendment lane, want 3 members for 1 round", len(turns))
	}
	if n := len(fake.ran(unit.Painter)) - replies; n != 0 {
		t.Errorf("the painter replied %d times in a one-round debate", n)
	}
	told(turns, 1)
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || !strings.Contains(u.Reason, "after 1 rounds") {
		t.Errorf("after the amendment debate = %+v", u)
	}

	// A bounce at the cap keeps the unit in the amendment lane.
	out, turns = debate(false)
	if out != Sealed {
		t.Fatalf("amendment debate after a bounce = %s", out)
	}
	told(turns, 1)

	// Sealing ends the lane: an ordinary reopen is debated under max_rounds.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the implement step failed", false))
	out, turns = debate(true)
	if out != Bounced {
		t.Fatalf("debate after an ordinary reopen = %s", out)
	}
	if len(turns) != 9 {
		t.Errorf("%d committee sessions after an ordinary reopen, want 3 members for 3 rounds", len(turns))
	}
	told(turns, 3)
}

//shed:proves S.shed.12
func TestAmendmentLaneKeepsToTheSealedScope(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 3\namendment_rounds = 2\nbounce_threshold = 10\n")
	change := propose(t, f)

	var mu sync.Mutex
	objection := ""
	object := false
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if !object || member(turn) != 1 {
			return done("clean")
		}
		if strings.HasPrefix(turn.Step, "debate round 1") {
			out, err := call(t, turn, "object", map[string]any{"kind": "spec", "citations": []string{"S.core.2"}, "text": "Say what goodbye prints."})
			must(t, err)
			mu.Lock()
			objection = strings.Fields(out)[1]
			mu.Unlock()
			return done("objecting")
		}
		mu.Lock()
		id := objection
		mu.Unlock()
		_, err := call(t, turn, "withdraw", map[string]any{"objection": id, "reason": "answered"})
		must(t, err)
		return done("clean")
	})
	revision := ""
	fake.on(unit.Painter, "reply", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "spec/core.md", revision)
		return done("replied")
	})

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("first debate = %s", out)
	}

	// Another unit lands after the seal and adds a clause this unit never
	// touched, so the proposal's spec no longer matches main's there.
	other, err := f.Repo.NewUnit(ctx, "Wave")
	must(t, err)
	dir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	write(t, dir, "spec/core.md", testrepo.Spec+"- **S.core.3** (H.greet.3) Running the tool with --wave waves.\n")
	_, err = f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Wave", nil })
	must(t, err)
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)

	// told checks that the sessions of the amendment lane were told the
	// sealed scope: the clause it modified and the clause it depended on.
	told := func(turns []session.Turn) {
		t.Helper()
		if len(turns) == 0 {
			t.Error("no sessions to check")
		}
		for _, turn := range turns {
			for _, id := range []string{"S.core.1", "S.core.2"} {
				if !strings.Contains(turn.Prompt, id) {
					t.Errorf("%s's %s was not told the scope clause %s: %q", turn.Role, turn.Step, id, turn.Prompt)
				}
			}
		}
	}

	// An amendment that adds a clause outside the scope bounces, naming it,
	// even with no objection standing.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	object = true
	revision = strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1) +
		"- **S.core.4** (H.greet.3) Running the tool with --hola prints hola.\n"
	committee, painter := len(fake.ran(unit.Committee)), len(fake.ran(unit.Painter))
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Bounced {
		t.Fatalf("an amendment outside the scope = %s", out)
	}
	if standing, _ := f.Tracker.Standing(change); len(standing) != 0 {
		t.Errorf("objections still stand: %+v", standing)
	}
	told(fake.ran(unit.Committee)[committee:])
	told(fake.ran(unit.Painter)[painter:])
	// The reopen counted one bounce and the scope counts another.
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 2 || !strings.Contains(u.Reason, "S.core.4") {
		t.Errorf("after an amendment outside the scope = %+v", u)
	}
	for _, id := range []string{"S.core.1", "S.core.2", "S.core.3"} {
		if strings.Contains(u.Reason, id) {
			t.Errorf("the bounce names %s, which is not outside the scope: %q", id, u.Reason)
		}
	}

	// An amendment that changes the clauses it modified and depended on
	// seals, though main changed S.core.3 after the last seal.
	revision = strings.Replace(strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1),
		"prints hello.", "prints the word hello.", 1)
	committee, painter = len(fake.ran(unit.Committee)), len(fake.ran(unit.Painter))
	out, err = f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("an amendment within the scope = %s: %s", out, u.Reason)
	}
	told(fake.ran(unit.Committee)[committee:])
	told(fake.ran(unit.Painter)[painter:])
	if u, _ := f.Tracker.Unit(change); u.Seal == nil || u.Seal.Main != main {
		t.Errorf("the amendment's seal = %+v, want main %s", u.Seal, main)
	}
}
