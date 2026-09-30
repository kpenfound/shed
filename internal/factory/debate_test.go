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
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.2"}, unit.Painter))
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
		if !strings.Contains(turn.Bundle, "Depends on: S.core.1") {
			t.Errorf("next round lacks declared dependency: %s", turn.Bundle)
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
		_, err = call(t, turn, "declare", map[string]any{"depends": []string{"S.core.1"}})
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
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	if u.State != unit.Sealed || u.Seal == nil || u.Seal.Main != main || u.Seal.Change != change || u.Seal.Commit != commit {
		t.Errorf("unit = %+v, want a seal at main %s and unit commit %s", u, main, commit)
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

//shed:proves S.shed.15
func TestArchiveEntryListsHorizonChanges(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[shed]\nmax_rounds = 1\n")
	change := propose(t, f)
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "horizon.md", `# Horizon

- **H.greet.1** (soon, realised) The tool
  says hello.
- **H.greet.2** (near) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool waves.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	fake.on(unit.Committee, "debate round 1", func(turn session.Turn) session.Result {
		if member(turn) == 1 {
			_, err := call(t, turn, "object", map[string]any{"kind": "horizon", "citations": []string{"H.greet.3"},
				"text": "Waving is not on the horizon."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Deferred {
		t.Fatalf("debate = %s", out)
	}
	entries, err := archive.Read(r.Dir)
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("archive = %+v", entries)
	}
	want := "## Horizon changes\n\n" +
		"- Changed H.greet.2\n  Was: (soon) The tool says goodbye.\n  Now: (near) The tool says goodbye.\n" +
		"- Added H.greet.4 (eventual) The tool waves.\n"
	if !strings.Contains(entries[0].Text, want) {
		t.Errorf("entry = %s, want it to hold\n%s", entries[0].Text, want)
	}
	if strings.Contains(entries[0].Text, "H.greet.1 ") {
		t.Errorf("a rewrapped clause counts as changed:\n%s", entries[0].Text)
	}
	if strings.Index(entries[0].Text, "## Proposal") > strings.Index(entries[0].Text, "## Horizon changes") ||
		strings.Index(entries[0].Text, "## Horizon changes") > strings.Index(entries[0].Text, "## Debate") {
		t.Errorf("horizon changes are not beside the spec changes and debate:\n%s", entries[0].Text)
	}

	other := propose(t, f)
	out, err = f.Debate(ctx, other)
	must(t, err)
	if out != Deferred {
		t.Fatalf("debate = %s", out)
	}
	entries, err = archive.Read(r.Dir)
	must(t, err)
	for _, e := range entries {
		if strings.Contains(e.Path, other) && strings.Contains(e.Text, "Horizon changes") {
			t.Errorf("an entry with no horizon change lists some:\n%s", e.Text)
		}
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
	// An amendment that adds a clause outside its scope bounces at the cap.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	sealedSpec, err := os.ReadFile(filepath.Join(dir, "spec/core.md"))
	must(t, err)
	write(t, dir, "spec/core.md", string(sealedSpec)+"- **S.core.4** (H.greet.3) Running the tool with --hola prints hola.\n")
	replies := len(fake.ran(unit.Painter))
	out, turns = debate(false)
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
	if u.State != unit.Proposed || !strings.Contains(u.Reason, "S.core.4") {
		t.Errorf("after the amendment debate = %+v", u)
	}

	// A bounce at the cap keeps the unit in the amendment lane: its next
	// debate is held to the same cap, and objections standing there reject
	// the amendment.
	write(t, dir, "spec/core.md", string(sealedSpec))
	out, turns = debate(true)
	if out != Sealed {
		t.Fatalf("amendment debate after a bounce = %s", out)
	}
	if len(turns) != 3 {
		t.Errorf("%d committee sessions after a bounce in the amendment lane, want 3 members for 1 round", len(turns))
	}
	told(turns, 1)
	if u, _ := f.Tracker.Unit(change); !strings.Contains(u.Reason, "after 1 rounds") {
		t.Errorf("after the amendment debate following a bounce = %+v", u)
	}

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
		if strings.Contains(revision, "S.core.4") {
			_, err := call(t, turn, "declare", map[string]any{"depends": []string{"S.core.1", "S.core.4"}})
			must(t, err)
		} else {
			_, err := call(t, turn, "declare", map[string]any{"depends": []string{"S.core.1"}})
			must(t, err)
		}
		return done("replied")
	})

	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Sealed {
		t.Fatalf("first debate = %s", out)
	}

	// Another unit lands after the seal and adds a clause this unit never
	// touched. Shed stopped before rebasing, so the next process rebases
	// the proposal onto the new main: its spec now holds the clause, which
	// the main recorded in the seal lacks.
	other, err := f.Repo.NewUnit(ctx, "Wave")
	must(t, err)
	dir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	_, err = f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Wave", nil })
	must(t, err)
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, f.Close())
	f, err = Open(ctx, r.Dir, f.State, fake)
	must(t, err)
	t.Cleanup(func() { f.Close() })
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	if parent := r.Git("rev-parse", commit+"^"); parent != main {
		t.Fatalf("the proposal sits on %s, want the new main %s", parent, main)
	}

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
	for _, id := range []string{"S.core.1", "S.core.2", "S.wave.1"} {
		if strings.Contains(u.Reason, id) {
			t.Errorf("the bounce names %s, which is not outside the scope: %q", id, u.Reason)
		}
	}

	// An amendment that changes the clauses it modified and depended on
	// seals, though main added S.wave.1 after the last seal.
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

// proposeHorizon proposes the goodbye clause with the given horizon on the
// unit's change.
func proposeHorizon(t *testing.T, f *Factory, horizon string) string {
	t.Helper()
	change := propose(t, f)
	if horizon != "" {
		dir, err := f.Repo.Workspace(ctx, change)
		must(t, err)
		write(t, dir, "horizon.md", horizon)
	}
	return change
}

// lastMoveOf is a unit's latest move.
func lastMoveOf(t *testing.T, f *Factory, change string) tracker.Event {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == tracker.UnitMoved {
			return events[i]
		}
	}
	t.Fatal("the unit has never moved")
	return tracker.Event{}
}

// Horizons a proposal may carry, one per tier of amendment.
var (
	nearHorizon    = strings.Replace(testHorizon, "within C2.\n", "within C2.\n- **H.greet.4** (near) The tool bows.\n", 1)
	soonHorizon    = strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon) The tool says goodbye politely.", 1)
	distantHorizon = strings.Replace(testHorizon, "greets in any language", "greets in every language", 1)
	// eventualHorizon also changes a soon clause, and lists H.greet.5
	// before H.greet.4.
	eventualHorizon = strings.Replace(soonHorizon, "within C2.\n",
		"within C2.\n- **H.greet.5** (eventual) The tool sings.\n- **H.greet.4** (eventual) The tool waves.\n", 1)
)

//shed:proves S.shed.16
func TestDistantAmendmentsWaitForTheOwner(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	// A soon-tier amendment seals on consensus and fills the cap on units
	// in flight.
	soon := proposeHorizon(t, f, soonHorizon)
	if out, err := f.Debate(ctx, soon); err != nil || out != Sealed {
		t.Fatalf("a soon-tier amendment = %s, %v", out, err)
	}
	// A proposal proposed now, with no horizon change, is debated after
	// main changes the horizon.
	behind := propose(t, f)

	// contested checks that a unit moved to contested with shed as actor,
	// counting no bounce, naming the tier and the clauses in order.
	contested := func(change, tier string, named []string, unnamed ...string) {
		t.Helper()
		u, err := f.Tracker.Unit(change)
		must(t, err)
		if u.State != unit.Contested || u.Bounces != 0 || u.Seal != nil {
			t.Errorf("unit = %+v, want contested with no bounce and no seal", u)
		}
		ev := lastMoveOf(t, f, change)
		if ev.From != unit.Proposed || ev.To != unit.Contested || ev.Actor != unit.Shed || ev.Bounce {
			t.Errorf("the move = %+v, want shed moving it from proposed to contested", ev)
		}
		if !strings.Contains(ev.Reason, tier) {
			t.Errorf("the reason does not name the tier %s: %q", tier, ev.Reason)
		}
		last := -1
		for _, id := range named {
			i := strings.Index(ev.Reason, id)
			if i < 0 {
				t.Errorf("the reason does not name %s: %q", id, ev.Reason)
				continue
			}
			if i < last {
				t.Errorf("the reason names %s out of document order: %q", id, ev.Reason)
			}
			last = i
		}
		for _, id := range unnamed {
			if strings.Contains(ev.Reason, id) {
				t.Errorf("the reason names %s, which is not counted at %s: %q", id, tier, ev.Reason)
			}
		}
	}

	// An eventual-tier amendment is neither sealed nor held back by the
	// full cap: it goes to the owner.
	committee := len(fake.ran(unit.Committee))
	eventual := proposeHorizon(t, f, eventualHorizon)
	out, err := f.Debate(ctx, eventual)
	must(t, err)
	if out != Contested {
		t.Fatalf("an eventual-tier amendment = %s", out)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 3 {
		t.Errorf("%d committee sessions, want 3 members for 1 round", n)
	}
	contested(eventual, "eventual", []string{"H.greet.5", "H.greet.4"}, "H.greet.2", "H.greet.3")

	// So is a distant-tier amendment.
	distant := proposeHorizon(t, f, distantHorizon)
	if out, err := f.Debate(ctx, distant); err != nil || out != Contested {
		t.Fatalf("a distant-tier amendment = %s, %v", out, err)
	}
	contested(distant, "distant", []string{"H.greet.3"}, "H.greet.2")

	// A near-tier amendment is held back as before.
	near := proposeHorizon(t, f, nearHorizon)
	if out, err := f.Debate(ctx, near); err != nil || out != Waiting {
		t.Fatalf("a near-tier amendment under a full cap = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(near); u.State != unit.Proposed || u.Bounces != 0 {
		t.Errorf("the held unit = %+v", u)
	}

	// Main moves a clause to the eventual tier. The tier is taken against
	// the main commit a proposal descends from, so the proposal behind
	// main, which changes no horizon clause, seals.
	landOther(t, f, "Hum", map[string]string{
		"horizon.md": strings.Replace(testHorizon, "(soon, realised) The tool says hello.", "(eventual, realised) The tool hums hello.", 1)})
	must(t, f.Tracker.Reopen(soon, unit.Wheelbuilder, "the proof is weak", false))
	if out, err := f.Debate(ctx, behind); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(behind)
		t.Fatalf("a proposal with no horizon change behind main = %s, %v: %s", out, err, u.Reason)
	}

	// The held seal is released with no further round.
	must(t, f.Tracker.Reopen(behind, unit.Wheelbuilder, "the proof is weak", false))
	committee = len(fake.ran(unit.Committee))
	if out, err := f.Debate(ctx, near); err != nil || out != Sealed {
		t.Fatalf("the released seal = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the released seal ran %d committee sessions", n)
	}

	// In the amendment lane, a unit bounced for a clause outside its scope
	// takes no tier.
	must(t, f.Tracker.Reopen(near, unit.Wheelbuilder, "the proof is weak", false))
	sealedUnit := proposeHorizon(t, f, "")
	if out, err := f.Debate(ctx, sealedUnit); err != nil || out != Sealed {
		t.Fatalf("debate = %s, %v", out, err)
	}
	must(t, f.Tracker.Reopen(sealedUnit, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	dir, err := f.Repo.Workspace(ctx, sealedUnit)
	must(t, err)
	amended := strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1)
	write(t, dir, "spec/core.md", amended+"- **S.core.4** (H.greet.3) Running the tool with --hola prints hola.\n")
	write(t, dir, "horizon.md", eventualHorizon)
	out, err = f.Debate(ctx, sealedUnit)
	must(t, err)
	if u, _ := f.Tracker.Unit(sealedUnit); out != Bounced || !strings.Contains(u.Reason, "S.core.4") {
		t.Fatalf("an amendment outside its scope = %s: %+v", out, u)
	}

	// Within its scope, an eventual-tier amendment goes to the owner.
	write(t, dir, "spec/core.md", amended)
	if out, err := f.Debate(ctx, sealedUnit); err != nil || out != Contested {
		t.Fatalf("an eventual-tier amendment in the amendment lane = %s, %v", out, err)
	}
	if ev := lastMoveOf(t, f, sealedUnit); ev.Actor != unit.Shed || ev.To != unit.Contested || !strings.Contains(ev.Reason, "eventual") {
		t.Errorf("the move = %+v", ev)
	}
}

//shed:proves S.shed.17
func TestApprovedAmendmentSeals(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 1\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })

	change := proposeHorizon(t, f, eventualHorizon)
	if out, err := f.Debate(ctx, change); err != nil || out != Contested {
		t.Fatalf("an eventual-tier amendment = %s, %v", out, err)
	}

	// The owner approves: the unit moves to proposed with the owner as
	// actor and the reason as the move's reason.
	const why = "Singing and waving are where the tool is going."
	must(t, f.Tracker.Approve(change, why))
	u, err := f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 0 {
		t.Errorf("after the approval = %+v", u)
	}
	if ev := lastMoveOf(t, f, change); ev.From != unit.Contested || ev.To != unit.Proposed || ev.Actor != unit.Owner || ev.Reason != why {
		t.Errorf("the approval's move = %+v", ev)
	}
	if err := f.Tracker.Approve(change, why); err == nil {
		t.Error("approved a unit that is not contested")
	}

	// While the cap holds sealing back, the approved unit waits in proposed
	// and its debate runs no round.
	other := propose(t, f)
	if out, err := f.Debate(ctx, other); err != nil || out != Sealed {
		t.Fatalf("the other unit's debate = %s, %v", out, err)
	}
	committee, painter := len(fake.ran(unit.Committee)), len(fake.ran(unit.Painter))
	if out, err := f.Debate(ctx, change); err != nil || out != Waiting {
		t.Fatalf("an approved unit under a full cap = %s, %v", out, err)
	}
	if u, _ := f.Tracker.Unit(change); u.State != unit.Proposed {
		t.Errorf("the waiting unit is %s", u.State)
	}

	// Once the cap frees, its debate runs no round and seals it, without
	// taking its tier.
	must(t, f.Tracker.Reopen(other, unit.Wheelbuilder, "the proof is weak", false))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("an approved unit = %s, %v: %+v", out, err, u)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the approved unit's debates ran %d committee sessions", n)
	}
	if n := len(fake.ran(unit.Painter)) - painter; n != 0 {
		t.Errorf("the approved unit's debates ran %d painter sessions", n)
	}
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	commit, err := f.Repo.Commit(ctx, change)
	must(t, err)
	u, err = f.Tracker.Unit(change)
	must(t, err)
	if u.State != unit.Sealed || u.Seal == nil || u.Seal.Main != main || u.Seal.Commit != commit {
		t.Errorf("unit = %+v, want a seal at main %s and unit commit %s", u, main, commit)
	}
	if got := r.Git("show", commit+":horizon.md"); !strings.Contains(got, "H.greet.4") || !strings.Contains(got, "H.greet.5") {
		t.Errorf("the sealed horizon lacks the approved clauses:\n%s", got)
	}

	// A bounce before the seal ends the approval: the next debate runs its
	// rounds and takes the tier again.
	again := proposeHorizon(t, f, distantHorizon)
	if out, err := f.Debate(ctx, again); err != nil || out != Contested {
		t.Fatalf("a distant-tier amendment = %s, %v", out, err)
	}
	must(t, f.Tracker.Approve(again, why))
	must(t, f.Tracker.Bounce(again, unit.Committee, "sealing could not rebase the change"))
	committee = len(fake.ran(unit.Committee))
	if out, err := f.Debate(ctx, again); err != nil || out != Contested {
		t.Fatalf("an approved unit after a bounce = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 3 {
		t.Errorf("%d committee sessions after the bounce, want 3 members for 1 round", n)
	}
	if ev := lastMoveOf(t, f, again); ev.Actor != unit.Shed || ev.To != unit.Contested {
		t.Errorf("the move after the bounce = %+v", ev)
	}

	// A unit in the amendment lane goes to the owner for its tier, stays in
	// the lane when approved, and leaves it at the approved seal, which the
	// mechanic is told is a seal after an amendment.
	must(t, f.Tracker.Reopen(change, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1))
	if out, err := f.Debate(ctx, change); err != nil || out != Contested {
		t.Fatalf("an eventual-tier amendment in the amendment lane = %s, %v", out, err)
	}
	must(t, f.Tracker.Approve(change, why))
	if lane, _ := f.lane(change); lane == nil {
		t.Error("the approved unit left the amendment lane before its seal")
	}
	committee = len(fake.ran(unit.Committee))
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		t.Fatalf("an approved unit in the amendment lane = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the approved unit's debate ran %d committee sessions", n)
	}
	if lane, _ := f.lane(change); lane != nil {
		t.Error("the approved seal kept the unit in the amendment lane")
	}
	var bundles []string
	for _, s := range []string{"proofs", "implement", "docs"} {
		fake.on(unit.Mechanic, s, func(turn session.Turn) session.Result {
			bundles = append(bundles, turn.Bundle)
			return done("done")
		})
	}
	if out, err := f.Implement(ctx, change); err != nil || out != Implemented {
		t.Fatalf("implement = %s, %v", out, err)
	}
	for _, b := range bundles {
		if !strings.Contains(amendmentSection(b), "S.core.2") {
			t.Errorf("the bundle after the approved seal does not give the amendment:\n%s", b)
		}
	}

	// Shed refuses to approve a unit contested other than for its tier.
	f2 := open(t, project(t), newFake(t), "[shed]\nbounce_threshold = 0\n")
	contestedByBounce := propose(t, f2)
	must(t, f2.Tracker.Bounce(contestedByBounce, unit.Committee, "objections stand"))
	if u, _ := f2.Tracker.Unit(contestedByBounce); u.State != unit.Contested {
		t.Fatalf("unit = %+v, want contested", u)
	}
	if err := f2.Tracker.Approve(contestedByBounce, why); err == nil {
		t.Error("approved a unit contested by its bounces")
	}
	if u, _ := f2.Tracker.Unit(contestedByBounce); u.State != unit.Contested {
		t.Errorf("a refused approval moved the unit to %s", u.State)
	}
}

//shed:proves S.shed.18 S.shed.17
func TestSplitSoonAmendmentWaitsForTheOwner(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 10\n[shed]\nmax_rounds = 2\namendment_rounds = 1\nbounce_threshold = 10\n")

	// plan gives the kind of objection a member raises in a round, or ""
	// for none. raised holds the IDs of the objections raised.
	var mu sync.Mutex
	var plan func(round, member int) string
	var raised []string
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		var round, m int
		_, _ = fmtSscanf(turn.Step, &round, &m)
		mu.Lock()
		kind := plan(round, m)
		mu.Unlock()
		if kind == "" {
			return done("clean")
		}
		citation := "S.core.2"
		if kind == tracker.HorizonObjection {
			citation = "H.greet.2"
		}
		out, err := call(t, turn, "object", map[string]any{"kind": kind, "citations": []string{citation}, "text": "Goodbye need not be polite."})
		must(t, err)
		mu.Lock()
		raised = append(raised, strings.Fields(out)[1])
		mu.Unlock()
		return done("objecting")
	})
	fake.on(unit.Painter, "reply", func(session.Turn) session.Result { return done("replied") })
	debate := func(change string, p func(round, member int) string) Outcome {
		t.Helper()
		mu.Lock()
		plan, raised = p, nil
		mu.Unlock()
		out, err := f.Debate(ctx, change)
		must(t, err)
		return out
	}
	clean := func(int, int) string { return "" }

	// Members 1 and 2 hold horizon objections at the cap and member 3 holds
	// none: the soon-tier amendment is split and goes to the owner.
	split := func(round, member int) string {
		if round == member && member < 3 {
			return tracker.HorizonObjection
		}
		return ""
	}
	soon := proposeHorizon(t, f, soonHorizon)
	if out := debate(soon, split); out != Contested {
		t.Fatalf("a split soon-tier amendment = %s", out)
	}
	objections := append([]string(nil), raised...)
	if len(objections) != 2 {
		t.Fatalf("raised %v, want two objections", objections)
	}
	u, err := f.Tracker.Unit(soon)
	must(t, err)
	if u.State != unit.Contested || u.Bounces != 0 || u.Seal != nil {
		t.Errorf("unit = %+v, want contested with no bounce and no seal", u)
	}
	ev := lastMoveOf(t, f, soon)
	if ev.From != unit.Proposed || ev.To != unit.Contested || ev.Actor != unit.Shed || ev.Bounce {
		t.Errorf("the move = %+v, want shed moving it from proposed to contested", ev)
	}
	for _, want := range append([]string{"soon"}, objections...) {
		if !strings.Contains(ev.Reason, want) {
			t.Errorf("the reason does not name %s: %q", want, ev.Reason)
		}
	}
	if entries, _ := archive.Read(r.Dir); len(entries) != 0 {
		t.Errorf("the split amendment was archived: %+v", entries)
	}

	// Every member holding an objection at the cap is no split: the
	// soon-tier amendment is deferred.
	unanimous := proposeHorizon(t, f, soonHorizon)
	if out := debate(unanimous, func(round, member int) string {
		if (round == 1 && member < 3) || (round == 2 && member == 3) {
			return tracker.HorizonObjection
		}
		return ""
	}); out != Deferred {
		t.Errorf("a soon-tier amendment every member objects to = %s, want deferred", out)
	}

	// A standing objection of another kind is no split: it bounces.
	mixed := proposeHorizon(t, f, soonHorizon)
	if out := debate(mixed, func(round, member int) string {
		switch {
		case round == 1 && member == 1:
			return tracker.HorizonObjection
		case round == 1 && member == 2:
			return tracker.SpecObjection
		}
		return ""
	}); out != Bounced {
		t.Errorf("a split with a spec objection standing = %s, want bounced", out)
	}
	if u, _ := f.Tracker.Unit(mixed); u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("the bounced unit = %+v", u)
	}

	// A split over a near, distant or eventual amendment, or over a
	// proposal with no horizon amendment, is deferred.
	for name, horizon := range map[string]string{"near": nearHorizon, "distant": distantHorizon, "eventual": eventualHorizon} {
		change := proposeHorizon(t, f, horizon)
		if out := debate(change, split); out != Deferred {
			t.Errorf("a split %s-tier amendment = %s, want deferred", name, out)
		}
	}
	if out := debate(propose(t, f), split); out != Deferred {
		t.Errorf("a split proposal with no horizon amendment = %s, want deferred", out)
	}

	// The owner retries the split unit: it goes back to its painter with
	// the objections that stood at the cap as the reason, counts no bounce,
	// and its next debate starts afresh from round one.
	must(t, f.Tracker.Retry(soon, "Look at politeness again."))
	u, err = f.Tracker.Unit(soon)
	must(t, err)
	if u.State != unit.Proposed || u.Bounces != 0 || u.Round != 0 {
		t.Errorf("after the retry = %+v, want proposed at round 0 with no bounce", u)
	}
	for _, id := range objections {
		if !strings.Contains(u.Reason, id) {
			t.Errorf("the retried unit's reason does not name %s: %q", id, u.Reason)
		}
	}
	if standing, _ := f.Tracker.Standing(soon); len(standing) != 0 {
		t.Errorf("objections of the old debate still stand: %+v", standing)
	}
	committee := len(fake.ran(unit.Committee))
	if out := debate(soon, clean); out != Sealed {
		t.Fatalf("the retried unit's debate = %s", out)
	}
	turns := fake.ran(unit.Committee)[committee:]
	if len(turns) != 3 {
		t.Errorf("%d committee sessions after the retry, want 3 members for 1 round", len(turns))
	}
	for _, turn := range turns {
		if !strings.HasPrefix(turn.Step, "debate round 1") {
			t.Errorf("the retried unit's debate ran %s, want round 1", turn.Step)
		}
	}

	// The owner approves a split unit: its next debate runs no round and
	// seals it.
	approved := proposeHorizon(t, f, soonHorizon)
	if out := debate(approved, split); out != Contested {
		t.Fatalf("a split soon-tier amendment = %s", out)
	}
	const why = "Politeness is where the tool is going."
	must(t, f.Tracker.Approve(approved, why))
	if ev := lastMoveOf(t, f, approved); ev.From != unit.Contested || ev.To != unit.Proposed || ev.Actor != unit.Owner || ev.Reason != why {
		t.Errorf("the approval's move = %+v", ev)
	}
	committee, painter := len(fake.ran(unit.Committee)), len(fake.ran(unit.Painter))
	if out := debate(approved, split); out != Sealed {
		u, _ := f.Tracker.Unit(approved)
		t.Fatalf("an approved split unit = %s: %+v", out, u)
	}
	if n := len(fake.ran(unit.Committee)) - committee; n != 0 {
		t.Errorf("the approved unit's debate ran %d committee sessions", n)
	}
	if n := len(fake.ran(unit.Painter)) - painter; n != 0 {
		t.Errorf("the approved unit's debate ran %d painter sessions", n)
	}
	commit, err := f.Repo.Commit(ctx, approved)
	must(t, err)
	if got := r.Git("show", commit+":horizon.md"); !strings.Contains(got, "goodbye politely") {
		t.Errorf("the sealed horizon lacks the approved amendment:\n%s", got)
	}

	// In the amendment lane a split at the cap rejects the amendment,
	// restoring the horizon of the last seal.
	lane := propose(t, f)
	if out := debate(lane, clean); out != Sealed {
		t.Fatalf("debate = %s", out)
	}
	must(t, f.Tracker.Reopen(lane, unit.Mechanic, "the mechanic requested an amendment:\nS.core.2 should say more.", true))
	dir, err := f.Repo.Workspace(ctx, lane)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(goodbyeSpec, "prints goodbye.", "prints the word goodbye.", 1))
	write(t, dir, "horizon.md", soonHorizon)
	if out := debate(lane, split); out != Sealed {
		u, _ := f.Tracker.Unit(lane)
		t.Fatalf("a split soon-tier amendment in the amendment lane = %s, want the amendment rejected: %+v", out, u)
	}
	commit, err = f.Repo.Commit(ctx, lane)
	must(t, err)
	if got := r.Git("show", commit+":horizon.md"); strings.Contains(got, "goodbye politely") {
		t.Errorf("the rejected amendment's horizon was kept:\n%s", got)
	}
}
