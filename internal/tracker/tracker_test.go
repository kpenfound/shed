package tracker

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/unit"
)

const (
	unitA = "qpvuntsmwlqtqpvuntsmwlqt"
	unitB = "kkkkllllmmmmnnnnoooopppp"
	unitC = "zzzzyyyyxxxxwwwwvvvvuuuu"
)

func clock() func() time.Time {
	var mu sync.Mutex
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
}

func open(t *testing.T, dir string, opts Options) *Tracker {
	t.Helper()
	if opts.Now == nil {
		opts.Now = clock()
	}
	if opts.Alive == nil {
		opts.Alive = func(int) bool { return true }
	}
	tr, err := Open(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func onMain(ids ...string) func(clause.ID) bool {
	return func(id clause.ID) bool { return slices.Contains(ids, id.String()) }
}

func get(t *testing.T, tr *Tracker, ref string) Unit {
	t.Helper()
	u, err := tr.Unit(ref)
	must(t, err)
	return u
}

// through moves a proposed unit along to the given state.
func through(t *testing.T, tr *Tracker, change string, states ...unit.State) {
	t.Helper()
	for _, s := range states {
		switch s {
		case unit.Sealed:
			must(t, tr.Seal(change, "main1", "unitcommit", Footprint{}, unit.Committee, "consensus", nil))
			continue
		case unit.Landed:
			must(t, tr.Land(change, "landed1", Footprint{}, unit.Wheelbuilder, "landed"))
			continue
		}
		must(t, tr.Move(change, s, unit.Mechanic, "next"))
	}
}

func readLog(t *testing.T, dir string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, LogFile))
	must(t, err)
	defer f.Close()
	var out []map[string]any
	s := bufio.NewScanner(f)
	for s.Scan() {
		var m map[string]any
		must(t, json.Unmarshal(s.Bytes(), &m))
		out = append(out, m)
	}
	return out
}

//shed:proves S.track.1
func TestStateDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	tr := open(t, dir, Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Owner))
	for _, name := range []string{DBFile, LogFile, SessionsDir} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

//shed:proves S.track.2 S.unit.4
func TestTrackerHoldsUnits(t *testing.T) {
	tr := open(t, t.TempDir(), Options{BounceThreshold: 5})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	fp := Footprint{Modifies: []string{"S.greet.2"}, Depends: []string{"S.greet.1"}, Advances: []string{"H.greet.2"}}
	must(t, tr.Seal(unitA, "abc123", "def456", fp, unit.Committee, "consensus after two rounds", onMain("S.greet.1")))
	must(t, tr.Move(unitA, unit.Implementing, unit.Mechanic, "dispatched"))
	s, err := tr.StartSession(unitA, unit.Mechanic, "proofs", 42)
	must(t, err)
	must(t, tr.FinishSession(s.ID, Succeeded, "proofs written", 1.25, true))
	_, err = tr.AddNotice(unitA, unit.Mechanic, "seal-moved", "The seal moved.", unit.Wheelbuilder)
	must(t, err)
	must(t, tr.Reopen(unitA, unit.Mechanic, "the spec is ambiguous", true))

	u := get(t, tr, unitA)
	want := Unit{
		Change: unitA, Title: "Say goodbye", OpenedBy: unit.Painter, State: unit.Proposed, Bounces: 1, Amendments: 1,
		Reason: "the spec is ambiguous", Seal: &Seal{Main: "abc123", Change: unitA, Commit: "def456"},
		Footprint: fp, CostUSD: 1.25,
	}
	u.Opened, u.Updated = time.Time{}, time.Time{}
	if !reflect.DeepEqual(u, want) {
		t.Errorf("unit =\n%+v\nwant\n%+v", u, want)
	}
	notices, err := tr.Notices(unit.Mechanic, true)
	must(t, err)
	if len(notices) != 1 || notices[0].Body != "The seal moved." {
		t.Errorf("notices = %+v", notices)
	}
	must(t, tr.DeliverNotice(notices[0].ID))
	if pending, _ := tr.Notices(unit.Mechanic, true); len(pending) != 0 {
		t.Errorf("pending after delivery = %+v", pending)
	}

	must(t, tr.OpenUnit(unitB, "Wave", unit.Painter))
	must(t, tr.Archive(unitB, unit.Deferred, unit.Committee, "off the horizon"))
	if b := get(t, tr, unitB); b.State != unit.Archived || b.Shelf != unit.Deferred {
		t.Errorf("archived unit = %+v", b)
	}
	if err := tr.Archive(unitA, "lost", unit.Committee, "x"); err == nil {
		t.Error("archived onto an unknown shelf")
	}
}

//shed:proves S.track.3
func TestEventLog(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.Seal(unitA, "abc123", "unitcommit", Footprint{}, unit.Committee, "consensus", nil))
	s, err := tr.StartSession(unitA, unit.Mechanic, "proofs", 7)
	must(t, err)
	must(t, tr.FinishSession(s.ID, Failed, "gave up", 0.5, false))

	lines := readLog(t, dir)
	if len(lines) != 4 {
		t.Fatalf("got %d log lines, want 4", len(lines))
	}
	for i, l := range lines {
		if l["seq"] != float64(i+1) || l["time"] == nil || l["unit"] != unitA {
			t.Errorf("line %d = %v", i+1, l)
		}
	}
	move := lines[1]
	if move["kind"] != UnitMoved || move["from"] != "proposed" || move["to"] != "sealed" ||
		move["actor"] != "committee" || move["reason"] != "consensus" {
		t.Errorf("move line = %v", move)
	}
	if lines[3]["kind"] != SessionFinished || lines[3]["cost_usd"] != 0.5 {
		t.Errorf("session line = %v", lines[3])
	}
	if err := tr.Move(unitA, unit.Implementing, unit.Mechanic, " "); err == nil {
		t.Error("moved without a reason")
	}
	if got := len(readLog(t, dir)); got != 4 {
		t.Errorf("a refused move was logged: %d lines", got)
	}
}

//shed:proves S.track.4
func TestWritersTakeTurns(t *testing.T) {
	dir := t.TempDir()
	first := open(t, dir, Options{})
	second := open(t, dir, Options{})
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 40 {
		tr := first
		if i%2 == 1 {
			tr = second
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- tr.OpenUnit(fmt.Sprintf("kkkkkkkkkkk%c", 'k'+rune(i%16))+strings.Repeat("z", i+1), "unit", unit.Painter)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	lines := readLog(t, dir)
	seen := map[float64]bool{}
	for _, l := range lines {
		seen[l["seq"].(float64)] = true
	}
	if len(lines) != 40 || len(seen) != 40 {
		t.Errorf("%d lines with %d distinct sequence numbers", len(lines), len(seen))
	}
	for i, tr := range []*Tracker{first, second} {
		// Each tracker catches up with the other's changes on its next write.
		must(t, tr.OpenUnit(strings.Repeat("m", 12)+strings.Repeat(string(rune('n'+i)), 12), "late", unit.Painter))
		units, err := tr.Units()
		must(t, err)
		if len(units) < 41 {
			t.Errorf("tracker sees %d units", len(units))
		}
	}
}

func snapshot(t *testing.T, tr *Tracker) any {
	t.Helper()
	units, err := tr.Units()
	must(t, err)
	var sessions []Session
	for _, u := range units {
		s, err := tr.Sessions(u.Change)
		must(t, err)
		sessions = append(sessions, s...)
	}
	owner, err := tr.Notices(unit.Owner, false)
	must(t, err)
	return []any{units, sessions, owner}
}

//shed:proves S.track.5 S.track.6
func TestRebuildReplaysTheLog(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{BounceThreshold: 1})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.SetFootprint(unitA, Footprint{Modifies: []string{"S.greet.2"}}, unit.Painter, "declared", nil))
	through(t, tr, unitA, unit.Sealed, unit.Implementing)
	s, err := tr.StartSession(unitA, unit.Mechanic, "proofs", 1)
	must(t, err)
	must(t, tr.FinishSession(s.ID, Succeeded, "done", 2, true))
	must(t, tr.Reopen(unitA, unit.Mechanic, "one", false))
	through(t, tr, unitA, unit.Sealed)
	must(t, tr.Reopen(unitA, unit.Mechanic, "two", true))
	must(t, tr.OpenUnit(unitB, "Wave", unit.Painter))
	before := snapshot(t, tr)

	must(t, tr.Rebuild())
	if after := snapshot(t, tr); !reflect.DeepEqual(before, after) {
		t.Errorf("after rebuild\n%+v\nwant\n%+v", after, before)
	}

	// A lost database is rebuilt when the tracker next opens.
	tr.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(filepath.Join(dir, DBFile+suffix))
	}
	reopened := open(t, dir, Options{BounceThreshold: 1})
	if after := snapshot(t, reopened); !reflect.DeepEqual(before, after) {
		t.Errorf("after reopening\n%+v\nwant\n%+v", after, before)
	}
}

//shed:proves S.track.6
func TestOpenAppliesLoggedEvents(t *testing.T) {
	dir := t.TempDir()
	tr := open(t, dir, Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	tr.Close()

	// A crash after logging the second event and in the middle of the third.
	logged, err := json.Marshal(Event{Seq: 2, Time: time.Now().UTC(), Kind: UnitOpened, Unit: unitB,
		Actor: unit.Painter, To: unit.Proposed, Title: "Wave"})
	must(t, err)
	f, err := os.OpenFile(filepath.Join(dir, LogFile), os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	_, err = f.WriteString(string(logged) + "\n" + `{"seq":3,"kind":"unit.op`)
	must(t, err)
	f.Close()

	tr = open(t, dir, Options{})
	if u := get(t, tr, unitB); u.Title != "Wave" || u.State != unit.Proposed {
		t.Errorf("logged unit = %+v", u)
	}
	must(t, tr.OpenUnit(unitC, "Bow", unit.Painter))
	lines := readLog(t, dir)
	if len(lines) != 3 || lines[2]["seq"] != float64(3) || lines[2]["unit"] != unitC {
		t.Errorf("log after recovery = %v", lines)
	}
}

//shed:proves S.track.7 S.track.8
func TestSessions(t *testing.T) {
	dir := t.TempDir()
	alive := map[int]bool{100: true, 200: true}
	var mu sync.Mutex
	isAlive := func(pid int) bool { mu.Lock(); defer mu.Unlock(); return alive[pid] }
	tr := open(t, dir, Options{Alive: isAlive})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	through(t, tr, unitA, unit.Sealed, unit.Implementing)

	proofs, err := tr.StartSession(unitA, unit.Mechanic, "proofs", 100)
	must(t, err)
	if info, err := os.Stat(proofs.Dir); err != nil || !info.IsDir() {
		t.Fatalf("session directory: %v", err)
	}
	if proofs.Dir != filepath.Join(dir, SessionsDir, proofs.ID) {
		t.Errorf("session dir = %s", proofs.Dir)
	}
	for got, want := range map[string]string{
		proofs.BundlePath(): "bundle.md", proofs.TranscriptPath(): "transcript.jsonl",
		proofs.OutcomePath(): "outcome.json", proofs.ResultPath(): "result.json",
	} {
		if got != filepath.Join(proofs.Dir, want) {
			t.Errorf("session file %s, want %s", got, want)
		}
	}
	must(t, tr.FinishSession(proofs.ID, Succeeded, "done", 1, true))
	impl, err := tr.StartSession(unitA, unit.Mechanic, "implement", 100)
	must(t, err)
	docs, err := tr.StartSession(unitA, unit.Mechanic, "docs", 200)
	must(t, err)
	if s, _ := tr.Session(impl.ID); s.Status != Running || s.PID != 100 || s.Role != unit.Mechanic || s.Step != "implement" {
		t.Errorf("running session = %+v", s)
	}
	tr.Close()

	// The process running the implement session died.
	mu.Lock()
	delete(alive, 100)
	mu.Unlock()
	tr = open(t, dir, Options{Alive: isAlive})
	if s, _ := tr.Session(impl.ID); s.Status != Interrupted {
		t.Errorf("dead session = %s", s.Status)
	}
	if s, _ := tr.Session(docs.ID); s.Status != Running {
		t.Errorf("live session = %s", s.Status)
	}
	if u := get(t, tr, unitA); !slices.Equal(u.Steps, []string{"proofs"}) || u.CostUSD != 1 {
		t.Errorf("unit after recovery = %+v", u)
	}
	must(t, tr.FinishSession(docs.ID, Succeeded, "done", 0, true))
	through(t, tr, unitA, unit.Verifying, unit.Implementing)
	if u := get(t, tr, unitA); len(u.Steps) != 0 {
		t.Errorf("steps after verification sent the unit back = %v", u.Steps)
	}
	if err := tr.FinishSession(impl.ID, Succeeded, "late", 0, true); err == nil {
		t.Error("finished an interrupted session")
	}
	if _, err := tr.StartSession(unitA, unit.Owner, "x", 1); err == nil {
		t.Error("the owner started a session")
	}
}

//shed:proves S.unit.2 S.unit.3
func TestMovesFollowTheStateMachine(t *testing.T) {
	tr := open(t, t.TempDir(), Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	if u := get(t, tr, unitA); u.State != unit.Proposed {
		t.Errorf("opened in %s", u.State)
	}
	if err := tr.OpenUnit(unitA, "Again", unit.Painter); err == nil {
		t.Error("opened a unit twice")
	}
	if err := tr.OpenUnit("bad", "Bad", unit.Painter); err == nil {
		t.Error("opened a unit with a bad change ID")
	}
	if err := tr.Move(unitA, unit.Implementing, unit.Mechanic, "skip the shed"); err == nil ||
		!strings.Contains(err.Error(), "cannot move from proposed to implementing") {
		t.Errorf("proposed to implementing: %v", err)
	}
	if err := tr.Move(unitA, unit.Sealed, unit.Committee, "x"); err == nil {
		t.Error("sealed without a seal")
	}
	if err := tr.Move(unitA, unit.Implementing, "nobody", "x"); err == nil {
		t.Error("moved by an unknown actor")
	}
	through(t, tr, unitA, unit.Sealed, unit.Implementing, unit.Verifying, unit.Implementing,
		unit.Verifying, unit.Queued, unit.Landed)
	if err := tr.Reopen(unitA, unit.Sweeper, "found a bug", false); err == nil {
		t.Error("reopened a landed unit")
	}
	if err := tr.Move(unitA, unit.Queued, unit.Owner, "undo"); err == nil {
		t.Error("moved a landed unit")
	}
	if _, err := tr.StartSession(unitA, unit.Mechanic, "x", 1); err == nil {
		t.Error("started a session on a landed unit")
	}
}

//shed:proves S.unit.5
func TestReopenCountsBounces(t *testing.T) {
	tr := open(t, t.TempDir(), Options{BounceThreshold: 10})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	if err := tr.Reopen(unitA, unit.Painter, "already proposed", false); err == nil {
		t.Error("reopened a proposed unit")
	}
	through(t, tr, unitA, unit.Sealed)
	must(t, tr.Reopen(unitA, unit.Wheelbuilder, "a neighbour changed a dependency", false))
	through(t, tr, unitA, unit.Sealed, unit.Implementing)
	must(t, tr.Reopen(unitA, unit.Mechanic, "the spec is wrong", true))
	through(t, tr, unitA, unit.Sealed, unit.Implementing, unit.Verifying)
	if err := tr.Move(unitA, unit.Proposed, unit.Committee, "no bounce"); err == nil {
		t.Error("moved back to proposed without reopening")
	}
	must(t, tr.Reopen(unitA, unit.Committee, "behaviour does not match", false))
	if u := get(t, tr, unitA); u.Bounces != 3 || u.Amendments != 1 || u.State != unit.Proposed {
		t.Errorf("unit = %+v", u)
	}
}

//shed:proves S.unit.6
func TestContestedPastTheThreshold(t *testing.T) {
	tr := open(t, t.TempDir(), Options{BounceThreshold: 1})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.OpenUnit(unitB, "Wave", unit.Painter))
	through(t, tr, unitA, unit.Sealed)
	must(t, tr.Reopen(unitA, unit.Committee, "first", false))
	if u := get(t, tr, unitA); u.State != unit.Proposed {
		t.Fatalf("after one bounce: %s", u.State)
	}
	through(t, tr, unitA, unit.Sealed)
	must(t, tr.Reopen(unitA, unit.Committee, "second", false))
	u := get(t, tr, unitA)
	if u.State != unit.Contested || u.Bounces != 2 || u.Reason != "bounced 2 times, over the threshold of 1" {
		t.Errorf("after two bounces: %+v", u)
	}
	notices, err := tr.Notices(unit.Owner, true)
	must(t, err)
	if len(notices) != 1 || notices[0].Unit != unitA || notices[0].Kind != "contested" ||
		notices[0].Body != "Unit qpvuntsmwlqt is contested: bounced 2 times, over the threshold of 1." {
		t.Errorf("owner notices = %+v", notices)
	}
	if b := get(t, tr, unitB); b.State != unit.Proposed {
		t.Errorf("other unit = %s", b.State)
	}
	through(t, tr, unitB, unit.Sealed, unit.Implementing)
	must(t, tr.Move(unitA, unit.Proposed, unit.Owner, "answered"))
	if u := get(t, tr, unitA); u.State != unit.Proposed || u.Bounces != 2 {
		t.Errorf("after the owner answers: %+v", u)
	}

	// A proposal bounced back to its painter counts too, and so does any
	// bounce of a unit already past the threshold.
	must(t, tr.OpenUnit(unitC, "Bow", unit.Painter))
	must(t, tr.Bounce(unitC, unit.Committee, "objections stand"))
	if u := get(t, tr, unitC); u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("after one painter bounce: %+v", u)
	}
	must(t, tr.Bounce(unitC, unit.Committee, "objections still stand"))
	if u := get(t, tr, unitC); u.State != unit.Contested || u.Bounces != 2 {
		t.Errorf("after two painter bounces: %+v", u)
	}
	must(t, tr.Bounce(unitA, unit.Committee, "no spec clause changes"))
	if u := get(t, tr, unitA); u.State != unit.Contested || u.Bounces != 3 {
		t.Errorf("a bounce past the threshold: %+v", u)
	}
}

//shed:proves S.unit.7
func TestDependenciesMustBeOnMain(t *testing.T) {
	tr := open(t, t.TempDir(), Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.OpenUnit(unitB, "Wave goodbye", unit.Painter))
	main := onMain("S.greet.1")
	must(t, tr.SetFootprint(unitA, Footprint{Modifies: []string{"S.greet.1", "S.greet.2"}}, unit.Painter, "declared", main))

	err := tr.SetFootprint(unitB, Footprint{Modifies: []string{"S.greet.3"}, Depends: []string{"S.greet.2"}}, unit.Painter, "declared", main)
	if err == nil || !strings.Contains(err.Error(), "depends on S.greet.2, which unit qpvuntsmwlqt changes and has not landed") {
		t.Errorf("dependency on an unlanded clause: %v", err)
	}
	err = tr.Seal(unitB, "main1", "unitcommit", Footprint{Depends: []string{"S.greet.9"}}, unit.Committee, "consensus", main)
	if err == nil || !strings.Contains(err.Error(), "depends on S.greet.9, which is not in the spec on main") {
		t.Errorf("dependency on a missing clause: %v", err)
	}
	// A clause on main is a fine dependency even while another unit changes
	// it, and so is a clause the unit adds itself.
	must(t, tr.SetFootprint(unitB, Footprint{Modifies: []string{"S.greet.4"}, Depends: []string{"S.greet.1", "S.greet.4"}}, unit.Painter, "declared", main))
	for _, bad := range []Footprint{{Modifies: []string{"H.greet.1"}}, {Advances: []string{"S.greet.1"}}, {Depends: []string{"S.greet.01"}}} {
		if err := tr.SetFootprint(unitB, bad, unit.Painter, "declared", main); err == nil {
			t.Errorf("accepted footprint %+v", bad)
		}
	}

	// Once the other unit lands, the dependency is on main.
	through(t, tr, unitA, unit.Sealed, unit.Implementing, unit.Verifying, unit.Queued, unit.Landed)
	must(t, tr.Seal(unitB, "main2", "unitcommit", Footprint{Depends: []string{"S.greet.2"}}, unit.Committee, "consensus", onMain("S.greet.1", "S.greet.2")))
}

//shed:proves S.unit.1
func TestUnitsByPrefix(t *testing.T) {
	tr := open(t, t.TempDir(), Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.OpenUnit("qpvuzzzzzzzzzzzz", "Other", unit.Painter))
	if u := get(t, tr, "qpvun"); u.Change != unitA {
		t.Errorf("prefix found %s", u.Change)
	}
	if _, err := tr.Unit("qpvu"); err == nil || !strings.Contains(err.Error(), "names 2 units") {
		t.Errorf("ambiguous prefix: %v", err)
	}
	if _, err := tr.Unit("kkkk"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown prefix: %v", err)
	}
}

//shed:proves S.fp.3
func TestLandingRecordsTheActualFootprint(t *testing.T) {
	tr := open(t, t.TempDir(), Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	sealed := Footprint{Modifies: []string{"S.greet.2", "S.greet.3"}, Depends: []string{"S.greet.1"}, Advances: []string{"H.greet.2"}}
	must(t, tr.Seal(unitA, "main1", "unitcommit", sealed, unit.Committee, "consensus", onMain("S.greet.1")))
	through(t, tr, unitA, unit.Implementing, unit.Verifying, unit.Queued)
	if u := get(t, tr, unitA); u.Actual != nil {
		t.Errorf("actual footprint before landing = %+v", *u.Actual)
	}
	actual := Footprint{Modifies: []string{"S.greet.2", "S.greet.4"}, Depends: []string{"S.greet.1"}, Advances: []string{"H.greet.2"}}
	must(t, tr.Land(unitA, "landed1", actual, unit.Wheelbuilder, "landed on main"))

	check := func(when string) {
		t.Helper()
		u := get(t, tr, unitA)
		if !reflect.DeepEqual(u.Footprint, sealed) {
			t.Errorf("%s: sealed footprint = %+v, want %+v", when, u.Footprint, sealed)
		}
		if u.Actual == nil || !reflect.DeepEqual(*u.Actual, actual) {
			t.Errorf("%s: actual footprint = %+v, want %+v", when, u.Actual, actual)
		}
	}
	check("after landing")
	must(t, tr.Rebuild())
	check("after rebuild")
}

//shed:proves S.fp.4
func TestFootprintDrift(t *testing.T) {
	sealed := Footprint{Modifies: []string{"S.greet.2", "S.greet.3"}, Depends: []string{"S.greet.1"}}
	for _, c := range []struct {
		actual     []string
		unsealed   []string
		unmodified []string
		text       string
	}{
		{[]string{"S.greet.2", "S.greet.3"}, nil, nil, "footprint held"},
		{[]string{"S.greet.3", "S.greet.2"}, nil, nil, "footprint held"},
		{[]string{"S.greet.2", "S.greet.4", "S.greet.5"}, []string{"S.greet.4", "S.greet.5"}, []string{"S.greet.3"},
			"footprint drifted: not sealed S.greet.4, S.greet.5; not modified S.greet.3"},
		{[]string{"S.greet.2", "S.greet.3", "S.greet.4"}, []string{"S.greet.4"}, nil,
			"footprint drifted: not sealed S.greet.4; not modified none"},
		{nil, nil, []string{"S.greet.2", "S.greet.3"},
			"footprint drifted: not sealed none; not modified S.greet.2, S.greet.3"},
	} {
		d := FootprintDrift(sealed, Footprint{Modifies: c.actual, Depends: sealed.Depends})
		if !reflect.DeepEqual(d.Unsealed, c.unsealed) || !reflect.DeepEqual(d.Unmodified, c.unmodified) {
			t.Errorf("drift of %v = %+v, want unsealed %v, unmodified %v", c.actual, d, c.unsealed, c.unmodified)
		}
		if d.Held() != (c.text == "footprint held") {
			t.Errorf("drift of %v held = %v", c.actual, d.Held())
		}
		if got := d.String(); got != c.text {
			t.Errorf("drift of %v = %q, want %q", c.actual, got, c.text)
		}
	}
}

//shed:proves S.fp.4
func TestLandingEventReportsDrift(t *testing.T) {
	tr := open(t, t.TempDir(), Options{})
	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter))
	must(t, tr.Seal(unitA, "main1", "unitcommit", Footprint{Modifies: []string{"S.greet.2"}}, unit.Committee, "consensus", nil))
	through(t, tr, unitA, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(unitA, "landed1", Footprint{Modifies: []string{"S.greet.3"}}, unit.Wheelbuilder, "landed on main"))
	must(t, tr.OpenUnit(unitB, "Wave", unit.Painter))
	must(t, tr.Seal(unitB, "main1", "unitcommit", Footprint{Modifies: []string{"S.greet.4"}}, unit.Committee, "consensus", nil))
	through(t, tr, unitB, unit.Implementing, unit.Verifying, unit.Queued)
	must(t, tr.Land(unitB, "landed2", Footprint{Modifies: []string{"S.greet.4"}}, unit.Wheelbuilder, "landed on main"))

	for change, want := range map[string]string{
		unitA: "footprint drifted: not sealed S.greet.3; not modified S.greet.2",
		unitB: "footprint held",
	} {
		events, err := tr.Events(change)
		must(t, err)
		last := events[len(events)-1]
		if last.To != unit.Landed {
			t.Fatalf("last event of %s = %+v", change, last)
		}
		if got := Describe(last); !strings.Contains(got, want) {
			t.Errorf("landing of %s = %q, want it to report %q", change, got, want)
		}
	}
}
