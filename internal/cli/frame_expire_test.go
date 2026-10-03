package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// expireConfig is the operator config for shed frame -expire tests: a
// remote to push the archive branch to, a bounce threshold of 0 so a
// single reopen contests a unit, and the given contested_timeout.
func expireConfig(timeout string) string {
	return "[vcs]\nremote = \"origin\"\n\n[shed]\nbounce_threshold = 0\ncontested_timeout = \"" + timeout + "\"\n"
}

// frameExpirer plays the frame builder for shed frame -expire sessions. It
// fails the test if any other role or step runs.
type frameExpirer struct {
	t     *testing.T
	mu    sync.Mutex
	turns []session.Turn
	run   func(turn session.Turn) session.Result
}

func (f *frameExpirer) Run(_ context.Context, turn session.Turn) (session.Result, error) {
	f.mu.Lock()
	f.turns = append(f.turns, turn)
	f.mu.Unlock()
	if turn.Role != unit.FrameBuilder {
		f.t.Errorf("%s ran step %q", turn.Role, turn.Step)
		return session.Result{}, nil
	}
	return f.run(turn), nil
}

// runFrameExpireAt runs shed frame with a fixed clock and a session
// runner, bypassing RunWith's flag parsing so the test controls what "now"
// means to the overdue check (S.owner.15) and to the archive's recorded
// wait (S.frame.7).
func runFrameExpireAt(t *testing.T, dir, state string, now time.Time, runner session.Runner, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	e := env{ctx: context.Background(), root: dir, state: state, runner: runner, stdout: &out, stderr: &errOut, now: func() time.Time { return now }}
	code = e.frame(args[1:])
	return out.String(), errOut.String(), code
}

// contestTierAt opens the tracker directly with a clock fixed at at and
// moves a proposed unit to contested for the tier of its horizon amendment
// (S.shed.16), or, with split, for a soon-tier amendment whose debate split
// at the round cap (S.shed.18).
func contestTierAt(t *testing.T, state, change string, at time.Time, tier string, split bool, reason string) {
	t.Helper()
	tr, err := tracker.Open(state, tracker.Options{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if split {
		err = tr.ContestSplit(change, tier, reason)
	} else {
		err = tr.ContestTier(change, tier, reason)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// mustEvents returns a unit's logged events, oldest first.
func mustEvents(t *testing.T, r *testrepo.Repo, change string) []tracker.Event {
	t.Helper()
	tr, err := tracker.Open(filepath.Join(r.Dir, DefaultStateDir), tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	events, err := tr.Events(change)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

//shed:proves S.frame.6
func TestFrameExpireRefuses(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	proposed := openUnit(t, r.Dir, "Wave")
	sealedU := openUnit(t, r.Dir, "Bow")
	seal(t, state, sealedU)
	notOverdue := openUnit(t, r.Dir, "Say goodbye")
	contestToAt(t, state, notOverdue, base)
	atTimeout := openUnit(t, r.Dir, "Nod")
	contestToAt(t, state, atTimeout, base)
	offUnit := openUnit(t, r.Dir, "Shrug")
	contestToAt(t, state, offUnit, base)

	f := &frameExpirer{t: t, run: func(session.Turn) session.Result {
		t.Fatal("a refused unit started a session")
		return session.Result{}
	}}

	// A unit that is not contested is refused, naming the unit and its
	// state.
	out, errOut, code := runFrameExpireAt(t, r.Dir, state, base, f, "frame", "-expire", unit.Short(proposed))
	if code == OK {
		t.Fatalf("frame -expire took a proposed unit: %q", out)
	}
	if said := out + errOut; !strings.Contains(said, unit.Short(proposed)) || !strings.Contains(said, "proposed") {
		t.Errorf("the refusal does not name the unit and its state: %q", said)
	}
	if _, _, code := runFrameExpireAt(t, r.Dir, state, base, f, "frame", "-expire", unit.Short(sealedU)); code == OK {
		t.Error("frame -expire took a sealed unit")
	}

	// A contested unit that is not yet overdue is refused, naming its wait
	// and the timeout.
	out, errOut, code = runFrameExpireAt(t, r.Dir, state, base.Add(26*time.Hour), f, "frame", "-expire", unit.Short(notOverdue))
	if code == OK {
		t.Fatalf("frame -expire took a unit under the timeout: %q", out)
	}
	if said := out + errOut; !strings.Contains(said, "72h") {
		t.Errorf("the refusal does not name the timeout: %q", said)
	}

	// A wait exactly equal to the timeout is not overdue (S.owner.15).
	if _, _, code := runFrameExpireAt(t, r.Dir, state, base.Add(72*time.Hour), f, "frame", "-expire", unit.Short(atTimeout)); code == OK {
		t.Error("frame -expire took a unit whose wait exactly equals the timeout")
	}

	// A unit that does not exist is refused.
	if _, _, code := runFrameExpireAt(t, r.Dir, state, base, f, "frame", "-expire", "deadbeef"); code == OK {
		t.Error("frame -expire took a unit that does not exist")
	}

	// A zero timeout turns expiry off, however long the wait.
	r.Write(".shed/config.toml", expireConfig("0s"))
	if _, _, code := runFrameExpireAt(t, r.Dir, state, base.Add(1000*time.Hour), f, "frame", "-expire", unit.Short(offUnit)); code == OK {
		t.Error("a zero timeout allowed an expiry")
	}

	if len(f.turns) != 0 {
		t.Errorf("refused units started %d sessions", len(f.turns))
	}
	want := map[string]unit.State{proposed: unit.Proposed, sealedU: unit.Sealed, notOverdue: unit.Contested, atTimeout: unit.Contested, offUnit: unit.Contested}
	for change, w := range want {
		if u := unitNow(t, r, change); u.State != w {
			t.Errorf("unit %s is %s after a refusal, want %s", unit.Short(change), u.State, w)
		}
	}
}

//shed:proves S.frame.6
func TestFrameExpireSessionBundle(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	change := openUnit(t, r.Dir, "Say goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	spec := testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", change)

	// Three bounces, oldest first, with two owner retries between them.
	seal(t, state, change)
	mustRun(t, r.Dir, "unit", "reopen", change, "wrong-reason-one")
	mustRun(t, r.Dir, "answer", change, "retry", "retry-reason-one")
	seal(t, state, change)
	mustRun(t, r.Dir, "unit", "reopen", change, "wrong-reason-two")
	mustRun(t, r.Dir, "answer", change, "retry", "retry-reason-two")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	contestToAt(t, state, change, base) // the third bounce, reason "the spec is wrong"

	var turn session.Turn
	f := &frameExpirer{t: t, run: func(tn session.Turn) session.Result {
		turn = tn
		return session.Result{Status: "keep", CostUSD: 0.1}
	}}
	out, errOut, code := runFrameExpireAt(t, r.Dir, state, base.Add(100*time.Hour), f, "frame", "-expire", unit.Short(change))
	if code != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
	}
	if len(f.turns) != 1 {
		t.Fatalf("frame -expire ran %d sessions, want 1", len(f.turns))
	}
	if turn.Writable {
		t.Error("the session's files are writable, but S.frame.6 throws its changes away")
	}
	if data, err := os.ReadFile(filepath.Join(turn.Dir, "spec", "core.md")); err != nil || string(data) != spec {
		t.Errorf("the session's copy does not hold the unit's own files: %q, %v", data, err)
	}

	bundle := turn.Bundle
	for _, want := range []string{
		"C1 The tool greets people.", // the charter
		"H.greet.3",                  // the whole horizon, not just the advanced clause
		"S.core.2",                   // the unit's spec change
	} {
		if !strings.Contains(bundle, want) {
			t.Errorf("the bundle lacks %q:\n%s", want, bundle)
		}
	}

	// The bounce reasons appear oldest first.
	i1 := strings.Index(bundle, "wrong-reason-one")
	i2 := strings.Index(bundle, "wrong-reason-two")
	i3 := strings.Index(bundle, "the spec is wrong")
	if i1 < 0 || i2 < 0 || i3 < 0 || !(i1 < i2 && i2 < i3) {
		t.Errorf("the bundle's bounce reasons are not oldest first:\n%s", bundle)
	}

	// The owner's answers appear oldest first.
	a1 := strings.Index(bundle, "retry-reason-one")
	a2 := strings.Index(bundle, "retry-reason-two")
	if a1 < 0 || a2 < 0 || a1 > a2 {
		t.Errorf("the bundle's owner answers are not oldest first:\n%s", bundle)
	}

	// The timeout and the wait.
	if !strings.Contains(bundle, "100h0m") {
		t.Errorf("the bundle lacks the wait 100h0m:\n%s", bundle)
	}
	if !strings.Contains(bundle, "72h") {
		t.Errorf("the bundle lacks the timeout:\n%s", bundle)
	}

	// The reason of the latest move to contested, and no S.shed.16/S.shed.18
	// basis for an ordinary bounce.
	if !strings.Contains(bundle, "the spec is wrong") {
		t.Errorf("the bundle lacks the latest contest's reason:\n%s", bundle)
	}
	if strings.Contains(bundle, "S.shed.16") || strings.Contains(bundle, "S.shed.18") {
		t.Errorf("the bundle names a tier basis for an ordinary bounce:\n%s", bundle)
	}
}

//shed:proves S.frame.6
func TestFrameExpireBundleNamesTierBasis(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tierChange := openUnit(t, r.Dir, "Wave")
	contestTierAt(t, state, tierChange, base, "distant", false, "the amendment adds a distant clause")
	splitChange := openUnit(t, r.Dir, "Nod")
	contestTierAt(t, state, splitChange, base, "soon", true, "2 objections stood at the round cap")

	var bundles []string
	f := &frameExpirer{t: t, run: func(turn session.Turn) session.Result {
		bundles = append(bundles, turn.Bundle)
		return session.Result{Status: "keep", CostUSD: 0.1}
	}}
	for _, change := range []string{tierChange, splitChange} {
		if out, errOut, code := runFrameExpireAt(t, r.Dir, state, base.Add(100*time.Hour), f, "frame", "-expire", unit.Short(change)); code != OK {
			t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
		}
	}
	if !strings.Contains(bundles[0], "S.shed.16") || !strings.Contains(bundles[0], "distant") {
		t.Errorf("the tier contest's bundle lacks its S.shed.16 basis:\n%s", bundles[0])
	}
	if !strings.Contains(bundles[1], "S.shed.18") || !strings.Contains(bundles[1], "soon") {
		t.Errorf("the split contest's bundle lacks its S.shed.18 basis:\n%s", bundles[1])
	}
}

//shed:proves S.frame.6
func TestFrameExpireDoneToolRefusals(t *testing.T) {
	r := retiredCharterRepo(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	plain := openUnit(t, r.Dir, "Say goodbye")
	contestToAt(t, state, plain, base)
	tiered := openUnit(t, r.Dir, "Wave")
	contestTierAt(t, state, tiered, base, "eventual", false, "the amendment is eventual")

	probe := func(change string, fn func(check func(status, note string) error)) {
		t.Helper()
		f := &frameExpirer{t: t, run: func(turn session.Turn) session.Result {
			if turn.Check == nil {
				t.Fatal("the session has no done-tool check")
			}
			fn(turn.Check)
			return session.Result{Status: "keep", CostUSD: 0.1}
		}}
		if out, errOut, code := runFrameExpireAt(t, r.Dir, state, base.Add(100*time.Hour), f, "frame", "-expire", unit.Short(change)); code != OK {
			t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
		}
	}

	probe(plain, func(check func(string, string) error) {
		for _, bad := range []struct{ status, note string }{
			{"rejected", ""},
			{"rejected", "   "},
			{"deferred", ""},
			{"rejected", "off the charter, no clause named"},
			{"rejected", "XC1 is wrong"},
			{"rejected", "breaks C2@HEAD"},
			{"rejected", "C2 to C2"},
			{"rejected", "breaks the retired C3"},
			{"rejected", "breaks C9"},
		} {
			if err := check(bad.status, bad.note); err == nil {
				t.Errorf("done accepted %s %q", bad.status, bad.note)
			}
		}
		for _, good := range []struct{ status, note string }{
			{"keep", ""},
			{"deferred", "the horizon moved on"},
			{"rejected", "breaks C2"},
		} {
			if err := check(good.status, good.note); err != nil {
				t.Errorf("done refused %s %q: %v", good.status, good.note, err)
			}
		}
	})

	// A unit contested only to wait for the owner's approve (S.shed.16,
	// S.shed.18) may be kept or deferred, never rejected.
	probe(tiered, func(check func(string, string) error) {
		if err := check("rejected", "breaks C2"); err == nil {
			t.Error("done accepted rejecting a unit that is contested only to wait for the owner's approve")
		}
		for _, good := range []struct{ status, note string }{
			{"keep", ""},
			{"deferred", "the amendment can wait"},
		} {
			if err := check(good.status, good.note); err != nil {
				t.Errorf("done refused %s %q: %v", good.status, good.note, err)
			}
		}
	})

	// A refused report records nothing.
	for _, change := range []string{plain, tiered} {
		if u := unitNow(t, r, change); u.State != unit.Contested {
			t.Errorf("unit %s is %s after refused reports, want contested", unit.Short(change), u.State)
		}
	}
}

//shed:proves S.frame.7
func TestFrameExpireArchivesRejected(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	change := openUnit(t, r.Dir, "Say goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	spec := testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", change)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	contestToAt(t, state, change, base)

	reason := "Goodbye is rude, breaks C2"
	f := &frameExpirer{t: t, run: func(session.Turn) session.Result {
		return session.Result{Status: "rejected", Note: reason, CostUSD: 0.2}
	}}
	now := base.Add(100 * time.Hour)
	out, errOut, code := runFrameExpireAt(t, r.Dir, state, now, f, "frame", "-expire", unit.Short(change))
	if code != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
	}

	u := unitNow(t, r, change)
	if u.State != unit.Archived || u.Shelf != unit.Rejected {
		t.Errorf("after expiry: %s on shelf %q, want archived rejected", u.State, u.Shelf)
	}
	ev := lastMove(t, r, change)
	if ev.From != unit.Contested || ev.To != unit.Archived || ev.Actor != unit.FrameBuilder || ev.Reason != reason || ev.Shelf != unit.Rejected {
		t.Errorf("the expiry's move = %+v", ev)
	}
	if !ev.Expired {
		t.Errorf("the move does not record that the unit expired: %+v", ev)
	}
	if ev.Timeout != 72*time.Hour {
		t.Errorf("the move's timeout = %s, want 72h", ev.Timeout)
	}
	if ev.Wait != 100*time.Hour {
		t.Errorf("the move's wait = %s, want 100h", ev.Wait)
	}

	entry := r.GitRemote("show", vcs.ArchiveBranch+":"+archive.Path(unit.Rejected, change))
	if !strings.Contains(entry, "- Shelf: rejected") || !strings.Contains(entry, "- Citations: C2\n") {
		t.Errorf("the entry does not cite C2 as violated:\n%s", entry)
	}
	if !strings.Contains(entry, "S.core.2") {
		t.Errorf("the entry lacks the proposal's spec changes:\n%s", entry)
	}
	if !strings.Contains(entry, "72h") || !strings.Contains(entry, "100h0m") {
		t.Errorf("the entry lacks the timeout and the wait:\n%s", entry)
	}

	// The expiry's recorded fields survive a tracker rebuild (S.track.5).
	mustRun(t, r.Dir, "tracker", "rebuild")
	u2 := unitNow(t, r, change)
	if u2.State != unit.Archived || u2.Shelf != unit.Rejected {
		t.Errorf("after rebuild: %s on shelf %q", u2.State, u2.Shelf)
	}
	ev2 := lastMove(t, r, change)
	if !ev2.Expired || ev2.Timeout != 72*time.Hour || ev2.Wait != 100*time.Hour {
		t.Errorf("rebuild lost the expiry's recorded fields: %+v", ev2)
	}
}

//shed:proves S.frame.7
func TestFrameExpireArchivesDeferred(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	change := openUnit(t, r.Dir, "Wave")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	contestTierAt(t, state, change, base, "distant", false, "the amendment is distant")

	reason := "the amendment can wait for the next horizon cycle"
	f := &frameExpirer{t: t, run: func(session.Turn) session.Result {
		return session.Result{Status: "deferred", Note: reason, CostUSD: 0.2}
	}}
	now := base.Add(200 * time.Hour)
	out, errOut, code := runFrameExpireAt(t, r.Dir, state, now, f, "frame", "-expire", unit.Short(change))
	if code != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
	}

	u := unitNow(t, r, change)
	if u.State != unit.Archived || u.Shelf != unit.Deferred {
		t.Errorf("after expiry: %s on shelf %q, want archived deferred", u.State, u.Shelf)
	}
	ev := lastMove(t, r, change)
	if ev.Actor != unit.FrameBuilder || ev.Reason != reason || !ev.Expired || ev.Timeout != 72*time.Hour || ev.Wait != 200*time.Hour {
		t.Errorf("the expiry's move = %+v", ev)
	}

	entry := r.GitRemote("show", vcs.ArchiveBranch+":"+archive.Path(unit.Deferred, change))
	_, decision, _ := strings.Cut(entry, "## What would change the decision\n")
	decision, _, _ = strings.Cut(decision, "\n## ")
	if !strings.Contains(entry, "- Shelf: deferred") || !strings.Contains(decision, reason) {
		t.Errorf("the entry does not say what would change the decision:\n%s", entry)
	}
	if !strings.Contains(entry, "72h") || !strings.Contains(entry, "200h0m") {
		t.Errorf("the entry lacks the timeout and the wait:\n%s", entry)
	}
}

//shed:proves S.frame.7
func TestFrameExpireKeepsOrEndsWithoutOutcome(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base.Add(100 * time.Hour)

	kept := openUnit(t, r.Dir, "Say goodbye")
	contestToAt(t, state, kept, base)
	noOutcome := openUnit(t, r.Dir, "Wave")
	contestToAt(t, state, noOutcome, base)
	keptEvents, noOutcomeEvents := len(mustEvents(t, r, kept)), len(mustEvents(t, r, noOutcome))

	keptF := &frameExpirer{t: t, run: func(session.Turn) session.Result { return session.Result{Status: "keep", CostUSD: 0.1} }}
	out, errOut, code := runFrameExpireAt(t, r.Dir, state, now, keptF, "frame", "-expire", unit.Short(kept))
	if code != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code, out, errOut)
	}
	if !strings.Contains(out, unit.Short(kept)) {
		t.Errorf("frame -expire does not say which unit it kept: %q", out)
	}
	if u := unitNow(t, r, kept); u.State != unit.Contested {
		t.Errorf("a kept unit is %s, want contested", u.State)
	}

	noOutcomeF := &frameExpirer{t: t, run: func(session.Turn) session.Result { return session.Result{CostUSD: 0.1} }}
	out, errOut, code = runFrameExpireAt(t, r.Dir, state, now, noOutcomeF, "frame", "-expire", unit.Short(noOutcome))
	if code == OK {
		t.Errorf("a session that ended without an outcome succeeded: %q", out)
	}
	if said := out + errOut; !strings.Contains(said, "outcome") {
		t.Errorf("frame -expire does not say the session ended without an outcome: %q", said)
	}
	if u := unitNow(t, r, noOutcome); u.State != unit.Contested {
		t.Errorf("a unit whose session ended without an outcome is %s, want contested", u.State)
	}

	for _, pair := range []struct {
		change string
		before int
	}{{kept, keptEvents}, {noOutcome, noOutcomeEvents}} {
		for _, e := range mustEvents(t, r, pair.change)[pair.before:] {
			if e.Kind == tracker.UnitMoved {
				t.Errorf("unit %s recorded a move after a %s outcome: %+v", unit.Short(pair.change), e.Kind, e)
			}
		}
	}
}

//shed:proves S.frame.7
func TestFrameExpireDoesNothingWhenTheUnitChangedMeanwhile(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", expireConfig("72h"))
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base.Add(100 * time.Hour)

	left := openUnit(t, r.Dir, "Say goodbye")
	contestToAt(t, state, left, base)
	again := openUnit(t, r.Dir, "Wave")
	contestToAt(t, state, again, base)

	// The unit left contested (the owner retried it) while the session ran.
	f1 := &frameExpirer{t: t, run: func(session.Turn) session.Result {
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Retry(left, "the owner got there first"); err != nil {
			t.Fatal(err)
		}
		tr.Close()
		return session.Result{Status: "rejected", Note: "breaks C2", CostUSD: 0.1}
	}}
	out1, errOut1, code1 := runFrameExpireAt(t, r.Dir, state, now, f1, "frame", "-expire", unit.Short(left))
	if code1 != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code1, out1, errOut1)
	}
	if said := out1 + errOut1; !strings.Contains(said, "left contested") {
		t.Errorf("frame -expire does not say the unit left contested meanwhile: %q", said)
	}
	if u := unitNow(t, r, left); u.State != unit.Proposed {
		t.Errorf("a unit the owner took out of contested meanwhile is %s, want proposed", u.State)
	}

	// The unit moved to contested again (a new bounce) while the session
	// ran.
	f2 := &frameExpirer{t: t, run: func(session.Turn) session.Result {
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Retry(again, "try again"); err != nil {
			t.Fatal(err)
		}
		if err := tr.Move(again, unit.Contested, unit.Committee, "bounced again, meanwhile"); err != nil {
			t.Fatal(err)
		}
		tr.Close()
		return session.Result{Status: "rejected", Note: "breaks C2", CostUSD: 0.1}
	}}
	out2, errOut2, code2 := runFrameExpireAt(t, r.Dir, state, now, f2, "frame", "-expire", unit.Short(again))
	if code2 != OK {
		t.Fatalf("frame -expire = %d, %q, %q", code2, out2, errOut2)
	}
	if said := out2 + errOut2; !strings.Contains(said, "contested again") {
		t.Errorf("frame -expire does not say the unit moved to contested again: %q", said)
	}
	u := unitNow(t, r, again)
	if u.State != unit.Contested || u.Reason != "bounced again, meanwhile" {
		t.Errorf("a unit contested again meanwhile is %+v, want contested with the newer reason", u)
	}
	if u.Shelf != "" {
		t.Errorf("the stale archive report still archived the unit: shelf %q", u.Shelf)
	}
}
