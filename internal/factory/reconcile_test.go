package factory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// advancing opens a unit and seals it advancing the given horizon clauses,
// then moves it on to state.
func advancing(t *testing.T, f *Factory, title string, state unit.State, advances ...string) string {
	t.Helper()
	change, err := f.Repo.NewUnit(ctx, title)
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, title, unit.Painter))
	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	must(t, f.Tracker.Seal(change, main, "unitcommit", tracker.Footprint{Advances: advances}, unit.Committee, "consensus", nil))
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		if state == unit.Sealed {
			break
		}
		must(t, f.Tracker.Move(change, s, unit.Mechanic, "next"))
		if s == state {
			break
		}
	}
	return change
}

// landHorizon lands a unit whose change writes the horizon and any other
// files given.
func landHorizon(t *testing.T, f *Factory, horizon string, files ...string) string {
	t.Helper()
	change := advancing(t, f, "Rework the horizon", unit.Queued)
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "horizon.md", horizon)
	for i := 0; i+1 < len(files); i += 2 {
		write(t, dir, files[i], files[i+1])
	}
	if out, err := f.Land(ctx, change); err != nil || out != Landed {
		t.Fatalf("land = %s, %v", out, err)
	}
	return change
}

// eventsSince returns a unit's events after the first n.
func eventsSince(t *testing.T, f *Factory, change string, n int) []tracker.Event {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	return events[n:]
}

func eventCount(t *testing.T, f *Factory, change string) int {
	t.Helper()
	events, err := f.Tracker.Events(change)
	must(t, err)
	return len(events)
}

// noticesIn returns the notices among events.
func noticesIn(events []tracker.Event) []tracker.Event {
	var out []tracker.Event
	for _, e := range events {
		if e.Kind == tracker.NoticeAdded {
			out = append(out, e)
		}
	}
	return out
}

//shed:proves S.queue.3
func TestLandingNoticesHorizonChanges(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	// The goodbye unit is sealed advancing H.greet.2.
	bye := sealed(t, f, fake)
	// Its horizon clauses are the ones recorded at its last seal.
	resealed := advancing(t, f, "Resealed", unit.Sealed, "H.greet.1")
	must(t, f.Tracker.Reopen(resealed, unit.Committee, "rethink", false))
	main, _ := f.Repo.MainCommit(ctx)
	must(t, f.Tracker.Seal(resealed, main, "unitcommit", tracker.Footprint{Advances: []string{"H.greet.3"}}, unit.Committee, "consensus", nil))
	both := advancing(t, f, "Both", unit.Implementing, "H.greet.3", "H.greet.2")
	verifying := advancing(t, f, "Verifying", unit.Verifying, "H.greet.2")
	queued := advancing(t, f, "Queued", unit.Queued, "H.greet.3")
	spaced := advancing(t, f, "Spaced", unit.Implementing, "H.greet.1")
	proposed, err := f.Repo.NewUnit(ctx, "Proposed")
	must(t, err)
	must(t, f.Tracker.OpenUnit(proposed, "Proposed", unit.Painter))
	must(t, f.Tracker.SetFootprint(proposed, tracker.Footprint{Advances: []string{"H.greet.2"}}, unit.Painter, "declared", nil))

	watched := []string{bye, resealed, both, verifying, queued, spaced, proposed}
	before := map[string]tracker.Unit{}
	counts := map[string]int{}
	for _, c := range watched {
		before[c], _ = f.Tracker.Unit(c)
		counts[c] = eventCount(t, f, c)
	}

	// The landing rewords H.greet.2, moves H.greet.3 to soon, only respaces
	// H.greet.1 and adds H.greet.4.
	horizon := strings.NewReplacer(
		"The tool says hello.", "The tool  says   hello.",
		"(soon) The tool says goodbye.", "(soon) The tool waves farewell.",
		"(distant) The tool greets", "(soon) The tool greets",
		"\n## Milestones", "- **H.greet.4** (eventual) The tool sings.\n\n## Milestones",
	).Replace(testHorizon)
	lander := landHorizon(t, f, horizon)
	short := unit.Short(lander)

	for _, c := range watched {
		after, _ := f.Tracker.Unit(c)
		if after.State != before[c].State || after.Bounces != before[c].Bounces {
			t.Errorf("unit %s moved from %s to %s with %d bounces", unit.Short(c), before[c].State, after.State, after.Bounces)
		}
	}
	for _, c := range []string{spaced, proposed} {
		if got := eventsSince(t, f, c, counts[c]); len(got) != 0 {
			t.Errorf("unit %s untouched by the landing got events %+v", unit.Short(c), got)
		}
	}

	notice := func(c string) string {
		t.Helper()
		events := eventsSince(t, f, c, counts[c])
		notices := noticesIn(events)
		if len(notices) != 1 || len(events) != 1 {
			t.Fatalf("unit %s got events %+v, want one notice", unit.Short(c), events)
		}
		body := notices[0].Notice.Body
		if !strings.Contains(body, short) {
			t.Errorf("notice for %s does not name the landed unit %s:\n%s", unit.Short(c), short, body)
		}
		if !strings.Contains(tracker.Describe(notices[0]), body) {
			t.Errorf("the unit log does not show the notice: %s", tracker.Describe(notices[0]))
		}
		for _, other := range []string{"H.greet.1", "H.greet.4"} {
			if strings.Contains(body, other) {
				t.Errorf("notice for %s names %s:\n%s", unit.Short(c), other, body)
			}
		}
		return body
	}
	reworded := []string{"H.greet.2", "soon", "The tool says goodbye.", "The tool waves farewell."}
	retagged := []string{"H.greet.3", "distant", "soon", "The tool greets in any language, within C2."}
	for c, want := range map[string][]string{
		bye: reworded, verifying: reworded,
		resealed: retagged, queued: retagged,
		both: append(reworded, retagged...),
	} {
		body := notice(c)
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("notice for %s lacks %q:\n%s", unit.Short(c), w, body)
			}
		}
		if c == bye || c == verifying {
			if strings.Contains(body, "H.greet.3") {
				t.Errorf("notice for %s names H.greet.3:\n%s", unit.Short(c), body)
			}
		}
		if c == resealed || c == queued {
			if strings.Contains(body, "H.greet.2") {
				t.Errorf("notice for %s names H.greet.2:\n%s", unit.Short(c), body)
			}
		}
	}
	// Clauses follow the parent's horizon, not the footprint.
	if body := notice(both); strings.Index(body, "H.greet.2") > strings.Index(body, "H.greet.3") {
		t.Errorf("notice lists H.greet.3 before H.greet.2:\n%s", body)
	}

	// The goodbye unit is marked for horizon review, which shows it the
	// notice without delivering it. Its next mechanic session carries it.
	fake.on(unit.Wheelbuilder, "review", func(session.Turn) session.Result {
		return session.Result{Status: "consistent", Note: "Farewell is goodbye.", CostUSD: 0.1}
	})
	must2(t, f.ReviewHorizon)(bye)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		for _, w := range []string{short, "H.greet.2", "The tool waves farewell."} {
			if !strings.Contains(turn.Bundle, w) {
				t.Errorf("the mechanic's bundle lacks %q:\n%s", w, turn.Bundle)
			}
		}
		write(t, turn.Dir, "bye_test.go", byeProof)
		return done("done")
	})
	must2(t, f.Implement)(bye)
}

//shed:proves S.queue.4
func TestLandingReopensUnitsWhoseHorizonClauseWentAway(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	sealedUnit := advancing(t, f, "Sealed", unit.Sealed, "H.greet.2", "H.greet.3")
	queued := advancing(t, f, "Queued", unit.Queued, "H.greet.3")
	reworded := advancing(t, f, "Reworded", unit.Implementing, "H.greet.2")
	untouched := advancing(t, f, "Untouched", unit.Verifying, "H.greet.1")
	// A unit past its seal whose change clashes with the landing: reopened
	// first, it is rebased as a proposed unit and keeps the conflict.
	clashing := advancing(t, f, "Clashing", unit.Implementing, "H.greet.3")
	dir, err := f.Repo.Workspace(ctx, clashing)
	must(t, err)
	write(t, dir, "wave.txt", "wave kindly\n")
	_, err = f.Repo.Snapshot(ctx, clashing)
	must(t, err)
	watched := []string{sealedUnit, queued, reworded, untouched, clashing}
	counts := map[string]int{}
	for _, c := range watched {
		counts[c] = eventCount(t, f, c)
	}

	// The landing removes H.greet.3 and rewords H.greet.2.
	horizon := strings.NewReplacer(
		"- **H.greet.3** (distant) The tool greets in any language, within C2.\n", "",
		"(soon) The tool says goodbye.", "(soon) The tool waves farewell.",
	).Replace(testHorizon)
	lander := landHorizon(t, f, horizon, "wave.txt", "wave warmly\n")
	short := unit.Short(lander)

	for _, c := range []string{sealedUnit, queued, clashing} {
		u, _ := f.Tracker.Unit(c)
		if u.State != unit.Proposed || u.Bounces != 1 {
			t.Errorf("unit %s is %s with %d bounces, want proposed with 1", unit.Short(c), u.State, u.Bounces)
		}
		if !strings.Contains(u.Reason, short) || !strings.Contains(u.Reason, "H.greet.3") {
			t.Errorf("reopen reason %q does not name %s and H.greet.3", u.Reason, short)
		}
		events := eventsSince(t, f, c, counts[c])
		var reopen *tracker.Event
		for i, e := range events {
			if e.Kind == tracker.UnitMoved && e.To == unit.Proposed {
				reopen = &events[i]
			}
		}
		if reopen == nil || reopen.Actor != unit.Shed || !reopen.Bounce {
			t.Errorf("unit %s reopen event = %+v, want a bounce by shed", unit.Short(c), reopen)
		}
		if n := noticesIn(events); len(n) != 0 {
			t.Errorf("reopened unit %s got notices %+v", unit.Short(c), n)
		}
	}

	if u, _ := f.Tracker.Unit(reworded); u.State != unit.Implementing || u.Bounces != 0 {
		t.Errorf("reworded unit is %s with %d bounces", u.State, u.Bounces)
	}
	if n := noticesIn(eventsSince(t, f, reworded, counts[reworded])); len(n) != 1 || strings.Contains(n[0].Notice.Body, "H.greet.3") {
		t.Errorf("reworded unit got notices %+v, want one naming only H.greet.2", n)
	}
	if u, _ := f.Tracker.Unit(untouched); u.State != unit.Verifying || len(eventsSince(t, f, untouched, counts[untouched])) != 0 {
		t.Errorf("untouched unit is %s with new events", u.State)
	}

	main, err := f.Repo.MainCommit(ctx)
	must(t, err)
	if got := parent(t, f, r, clashing); got != main {
		t.Errorf("the reopened clashing unit sits on %s, want the new main %s", got, main)
	}
	dir, err = f.Repo.Workspace(ctx, clashing)
	must(t, err)
	if got, _ := os.ReadFile(filepath.Join(dir, "wave.txt")); !strings.Contains(string(got), "<<<<<<<") {
		t.Errorf("the reopened clashing unit's wave.txt keeps no conflict:\n%s", got)
	}
}

//shed:proves S.queue.3
func TestHorizonNoticesReachTheUnitsNextSession(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	// One goodbye unit is verifying with code its proof rejects, so its
	// next session is the mechanic's, not the reviewer's.
	failing := sealed(t, f, fake)
	mechanic(t, fake)
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "bye.go", strings.Replace(byeCode, `"goodbye"`, `"bye"`, 1))
		return done("done")
	})
	must2(t, f.Implement)(failing)
	// Another is queued and then reopened before it lands, so its next
	// session is a debate, not a landing.
	reopened := sealed(t, f, fake)
	for _, s := range []unit.State{unit.Implementing, unit.Verifying, unit.Queued} {
		must(t, f.Tracker.Move(reopened, s, unit.Mechanic, "next"))
	}

	lander := landHorizon(t, f, strings.Replace(testHorizon, "(soon) The tool says goodbye.", "(soon) The tool waves farewell.", 1))
	short := unit.Short(lander)
	carries := func(bundle string) bool {
		return strings.Contains(bundle, short) && strings.Contains(bundle, "The tool waves farewell.")
	}

	// The verifying unit, once its horizon review finds it consistent,
	// fails on its proofs without a review session, and the mechanic's
	// next bundle carries the notice.
	fake.on(unit.Wheelbuilder, "review", func(session.Turn) session.Result {
		return session.Result{Status: "consistent", Note: "Farewell is goodbye.", CostUSD: 0.1}
	})
	must2(t, f.ReviewHorizon)(failing)
	reviews := len(fake.ran(unit.Committee))
	if out, err := f.Verify(ctx, failing); err != nil || out != Failed {
		t.Fatalf("verify = %s, %v", out, err)
	}
	if n := len(fake.ran(unit.Committee)); n != reviews {
		t.Fatalf("the committee ran %d sessions although the proofs fail", n-reviews)
	}
	mechanic(t, fake)
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		if !carries(turn.Bundle) {
			t.Errorf("the mechanic's bundle lacks the horizon notice:\n%s", turn.Bundle)
		}
		return done("done")
	})
	must2(t, f.Implement)(failing)

	// The reopened unit's next session is a committee debate, and it
	// carries the notice.
	must(t, f.Tracker.Reopen(reopened, unit.Owner, "rethink", false))
	debates := len(fake.ran(unit.Committee))
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	if _, err := f.Debate(ctx, reopened); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, turn := range fake.ran(unit.Committee)[debates:] {
		if turn.Unit == reopened && carries(turn.Bundle) {
			found = true
		}
	}
	if !found {
		t.Error("no committee debate bundle for the reopened unit carries the horizon notice")
	}
	for _, turn := range fake.ran(unit.Wheelbuilder) {
		if turn.Unit == reopened {
			t.Errorf("the reopened unit ran a wheelbuilder session: %s", turn.Step)
		}
	}
}

//shed:proves S.queue.6 S.queue.3
func TestLandingNoticesRefiningClauses(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")

	threes := advancing(t, f, "Threes", unit.Implementing, "H.greet.3")
	twos := advancing(t, f, "Twos", unit.Sealed, "H.greet.2")
	both := advancing(t, f, "Both", unit.Queued, "H.greet.1", "H.greet.3")
	proposed, err := f.Repo.NewUnit(ctx, "Proposed")
	must(t, err)
	must(t, f.Tracker.OpenUnit(proposed, "Proposed", unit.Painter))
	must(t, f.Tracker.SetFootprint(proposed, tracker.Footprint{Advances: []string{"H.greet.3"}}, unit.Painter, "declared", nil))

	watched := []string{threes, twos, both, proposed}
	before := map[string]tracker.Unit{}
	counts := map[string]int{}
	mark := func() {
		for _, c := range watched {
			before[c], _ = f.Tracker.Unit(c)
			counts[c] = eventCount(t, f, c)
		}
	}
	// notice returns the one notice a unit got from the landing, checking
	// that it names the landed unit, shows in the unit log, and that the
	// unit kept its state and seal and is not marked for horizon review.
	notice := func(c, short string) string {
		t.Helper()
		events := eventsSince(t, f, c, counts[c])
		notices := noticesIn(events)
		if len(notices) != 1 || len(events) != 1 {
			t.Fatalf("unit %s got events %+v, want one notice", unit.Short(c), events)
		}
		body := notices[0].Notice.Body
		if !strings.Contains(body, short) {
			t.Errorf("notice for %s does not name the landed unit %s:\n%s", unit.Short(c), short, body)
		}
		if !strings.Contains(tracker.Describe(notices[0]), body) {
			t.Errorf("the unit log does not show the notice: %s", tracker.Describe(notices[0]))
		}
		after, _ := f.Tracker.Unit(c)
		if after.State != before[c].State || after.Bounces != before[c].Bounces || !reflect.DeepEqual(after.Seal, before[c].Seal) {
			t.Errorf("unit %s moved from %s to %s with %d bounces, or lost its seal", unit.Short(c), before[c].State, after.State, after.Bounces)
		}
		if after.Review {
			t.Errorf("unit %s is marked for horizon review by a tag change or gained clause", unit.Short(c))
		}
		return body
	}
	lacks := func(c, body string, unwanted ...string) {
		t.Helper()
		for _, w := range unwanted {
			if strings.Contains(body, w) {
				t.Errorf("notice for %s has %q:\n%s", unit.Short(c), w, body)
			}
		}
	}
	has := func(c, body string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("notice for %s lacks %q:\n%s", unit.Short(c), w, body)
			}
		}
	}
	mark()

	// The first landing retags H.greet.1, gives H.greet.2 a refines tag
	// naming H.greet.3, and adds H.greet.4 and H.greet.5, which also
	// refines H.greet.3.
	first := strings.NewReplacer(
		"(soon, realised) The tool says hello.", "(near, realised) The tool says hello.",
		"(soon) The tool says goodbye.", "(soon, refines H.greet.3) The tool says goodbye.",
		"\n## Milestones", "- **H.greet.4** (eventual) The tool sings.\n- **H.greet.5** (soon, refines H.greet.3) The tool says hola.\n\n## Milestones",
	).Replace(testHorizon)
	short := unit.Short(landHorizon(t, f, first))

	// A unit whose only entries are gained clauses gets one notice giving
	// each, in the order of the landed horizon, with its tags and text.
	body := notice(threes, short)
	has(threes, body, "gained", "H.greet.2", "The tool says goodbye.", "H.greet.5", "The tool says hola.", "soon, refines H.greet.3")
	lacks(threes, body, "H.greet.1", "H.greet.4")
	if strings.Index(body, "H.greet.2") > strings.Index(body, "H.greet.5") {
		t.Errorf("notice lists H.greet.5 before H.greet.2:\n%s", body)
	}

	// Gained clauses follow any changed ones.
	body = notice(both, short)
	has(both, body, "gained", "H.greet.1", "soon, realised", "near, realised", "H.greet.2", "H.greet.5", "The tool says hola.")
	lacks(both, body, "H.greet.4")
	if strings.Index(body, "H.greet.1") > strings.Index(body, "gained") {
		t.Errorf("notice gives a gained clause before the changed H.greet.1:\n%s", body)
	}

	// A unit advancing the clause that gained a refines tag sees it only as
	// changed: nothing refines H.greet.2.
	body = notice(twos, short)
	has(twos, body, "H.greet.2", "(soon)", "soon, refines H.greet.3")
	lacks(twos, body, "gained", "H.greet.5")

	if got := eventsSince(t, f, proposed, counts[proposed]); len(got) != 0 {
		t.Errorf("the proposed unit got events %+v", got)
	}

	// A unit advancing H.greet.3 and H.greet.4 is sealed on the new main.
	gone := advancing(t, f, "Gone", unit.Implementing, "H.greet.3", "H.greet.4")
	watched = append(watched, gone)
	mark()

	// The second landing removes H.greet.4, rewords H.greet.5, which keeps
	// its refines tag, and adds H.greet.6 refining H.greet.3.
	second := strings.NewReplacer(
		"- **H.greet.4** (eventual) The tool sings.\n", "",
		"The tool says hola.", "The tool says hola, amigo.\n- **H.greet.6** (soon, refines H.greet.3) The tool says ciao.",
	).Replace(first)
	short = unit.Short(landHorizon(t, f, second))

	// Only the clause that newly refines H.greet.3 counts as gained.
	body = notice(threes, short)
	has(threes, body, "gained", "H.greet.6", "soon, refines H.greet.3", "The tool says ciao.")
	lacks(threes, body, "H.greet.2", "H.greet.5", "H.greet.4")
	body = notice(both, short)
	has(both, body, "gained", "H.greet.6", "The tool says ciao.")
	lacks(both, body, "H.greet.1", "H.greet.2", "H.greet.5")
	if got := eventsSince(t, f, twos, counts[twos]); len(got) != 0 {
		t.Errorf("the unit advancing H.greet.2 got events %+v", got)
	}

	// A unit that reopens for the landing gets no notice for it.
	if u, _ := f.Tracker.Unit(gone); u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("unit advancing the removed H.greet.4 is %s with %d bounces, want proposed with 1", u.State, u.Bounces)
	}
	if n := noticesIn(eventsSince(t, f, gone, counts[gone])); len(n) != 0 {
		t.Errorf("the reopened unit got notices %+v", n)
	}
}
