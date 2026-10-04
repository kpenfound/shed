package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// recordsClock returns a deterministic, strictly increasing UTC clock, one
// second per call, so records derived from event times are reproducible.
func recordsClock() func() time.Time {
	var mu sync.Mutex
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
}

// onMainIDs reports a spec clause among ids as being on main, for tests that
// declare a footprint depending on a clause without landing a unit that
// modifies it first.
func onMainIDs(ids ...string) func(clause.ID) bool {
	return func(id clause.ID) bool {
		for _, s := range ids {
			if id.String() == s {
				return true
			}
		}
		return false
	}
}

// recordsKeyOrder matches the exact key order S.ctx.1 requires, whatever
// whitespace separates them.
var recordsKeyOrder = regexp.MustCompile(
	`^\{\s*"id"\s*:.*"time"\s*:.*"kind"\s*:.*"topic"\s*:.*"actor"\s*:.*"cites"\s*:.*"text"\s*:.*\}\s*$`)

// recordLine is a decoded L0 record, used to check field values once key
// order and the exact key set are checked separately.
type recordLine struct {
	ID    string
	Time  string
	Kind  string
	Topic string
	Actor string
	Cites []string
	Text  string
}

func decodeRecord(t *testing.T, line string) recordLine {
	t.Helper()
	if !recordsKeyOrder.MatchString(line) {
		t.Fatalf("record keys out of order: %s", line)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("decoding %q: %v", line, err)
	}
	if len(fields) != 7 {
		t.Fatalf("record has %d keys, want exactly 7: %s", len(fields), line)
	}
	var r recordLine
	for key, dst := range map[string]any{
		"id": &r.ID, "time": &r.Time, "kind": &r.Kind, "topic": &r.Topic, "actor": &r.Actor, "text": &r.Text,
	} {
		raw, ok := fields[key]
		if !ok {
			t.Fatalf("record missing %q: %s", key, line)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("decoding %q of %q: %v", key, line, err)
		}
	}
	citesRaw, ok := fields["cites"]
	if !ok {
		t.Fatalf("record missing %q: %s", "cites", line)
	}
	if err := json.Unmarshal(citesRaw, &r.Cites); err != nil {
		t.Fatalf("decoding cites of %q: %v", line, err)
	}
	if r.Cites == nil {
		t.Errorf("cites decoded as null, want []: %s", line)
	}
	return r
}

func recordLines(t *testing.T, out string) []recordLine {
	t.Helper()
	var lines []recordLine
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l == "" {
			continue
		}
		lines = append(lines, decodeRecord(t, l))
	}
	return lines
}

// snapshotState captures every file under a directory with its size and
// modification time, so a later comparison can show the directory held
// still.
func snapshotState(t *testing.T, dir string) map[string][2]int64 {
	t.Helper()
	out := map[string][2]int64{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[rel] = [2]int64{info.Size(), info.ModTime().UnixNano()}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

//shed:proves S.ctx.1 S.ctx.2
func TestRecordsPrintsL0Records(t *testing.T) {
	const (
		unitA = "qpvuntsmwlqtqpvuntsmwlqt"
		unitB = "kkkkllllmmmmnnnnoooopppp"
		unitC = "zzzzyyyyxxxxwwwwvvvvuuuu"
	)
	r := testrepo.New(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	tr, err := tracker.Open(state, tracker.Options{Now: recordsClock(), Alive: func(int) bool { return true }, BounceThreshold: 10})
	if err != nil {
		t.Fatal(err)
	}

	must(t, tr.OpenUnit(unitA, "Say goodbye", unit.Painter)) // seq 1: unit.opened
	fp := tracker.Footprint{
		Modifies: []string{"S.greet.3", "S.greet.2"},
		Depends:  []string{"S.greet.1"},
		Advances: []string{"H.greet.2", "H.greet.1"},
	}
	must(t, tr.SetFootprint(unitA, fp, unit.Painter, "declared because the proposal changes greet",
		onMainIDs("S.greet.1"))) // seq 2: footprint.declared

	objID, err := tr.Object(unitA, 1, tracker.SpecObjection, []string{"S.ctx.1", "S.ctx.2"}, "the clause is ambiguous")
	must(t, err)                                                              // seq 3: objection
	must(t, tr.Answer(objID, "clarified the wording", unit.Painter))          // seq 4: answer
	must(t, tr.Withdraw(objID, 1, "the clarification resolves it"))           // seq 5: withdrawal
	must(t, tr.Seal(unitA, "main123456", "unitcommit123", fp, unit.Committee, // seq 6: seal
		"consensus reached", onMainIDs("S.greet.1")))
	must(t, tr.Move(unitA, unit.Implementing, unit.Mechanic, "dispatched")) // seq 7: no record
	must(t, tr.Reopen(unitA, unit.Mechanic, "the spec is ambiguous after all", true))
	// seq 8: reopen

	must(t, tr.OpenUnit(unitB, "Wave", unit.Painter))                                    // seq 9: unit.opened
	must(t, tr.Archive(unitB, unit.Deferred, unit.Committee, "off the horizon for now")) // seq 10: archive

	must(t, tr.OpenUnit(unitC, "Sing", unit.Painter)) // seq 11: unit.opened
	must(t, tr.Seal(unitC, "mainc2", "unitc2", tracker.Footprint{}, unit.Committee, "consensus for sing", nil))
	// seq 12: seal
	must(t, tr.Move(unitC, unit.Implementing, unit.Mechanic, "dispatched"))  // seq 13: no record
	must(t, tr.Move(unitC, unit.Verifying, unit.Mechanic, "proofs pass"))    // seq 14: no record
	must(t, tr.Move(unitC, unit.Queued, unit.Wheelbuilder, "ready to land")) // seq 15: no record
	s, err := tr.StartSession(unitC, unit.Mechanic, "proofs", 123)
	must(t, err)                                                          // seq 16: no record
	must(t, tr.FinishSession(s.ID, tracker.Succeeded, "done", 1.5, true)) // seq 17: no record
	must(t, tr.Land(unitC, "landedcommit123", tracker.Footprint{}, false, unit.Wheelbuilder, "landed successfully"))
	// seq 18: landing

	tr.Close()

	// shed records reads only the log; drop the tracker database the setup
	// above opened to build it, so the checks below over the state
	// directory, and for the database file, reflect records alone.
	if err := os.Remove(filepath.Join(state, tracker.DBFile)); err != nil {
		t.Fatal(err)
	}

	before := snapshotState(t, state)

	out1 := mustRun(t, r.Dir, "records")
	out2 := mustRun(t, r.Dir, "records")
	if out1 != out2 {
		t.Errorf("shed records is not deterministic:\n%q\nvs\n%q", out1, out2)
	}

	after := snapshotState(t, state)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("shed records changed files under the state directory:\nbefore %v\nafter  %v", before, after)
	}
	if _, err := os.Stat(filepath.Join(state, tracker.DBFile)); err == nil {
		t.Errorf("shed records left %s behind", tracker.DBFile)
	}

	lines := recordLines(t, out1)
	type want struct {
		seq   int
		kind  string
		topic string
		actor string
		cites []string
		text  string
	}
	wants := []want{
		{1, "unit.opened", unitA, "painter", nil, "Say goodbye"},
		{2, "footprint.declared", unitA, "painter", []string{"S.greet.3", "S.greet.2", "S.greet.1", "H.greet.2", "H.greet.1"}, "declared because the proposal changes greet"},
		{3, "objection", unitA, "committee", []string{"S.ctx.1", "S.ctx.2"}, "the clause is ambiguous"},
		{4, "answer", unitA, "painter", nil, "clarified the wording"},
		{5, "withdrawal", unitA, "committee", nil, "the clarification resolves it"},
		{6, "seal", unitA, "committee", nil, "consensus reached"},
		{8, "reopen", unitA, "mechanic", nil, "the spec is ambiguous after all"},
		{9, "unit.opened", unitB, "painter", nil, "Wave"},
		{10, "archive", unitB, "committee", nil, "off the horizon for now"},
		{11, "unit.opened", unitC, "painter", nil, "Sing"},
		{12, "seal", unitC, "committee", nil, "consensus for sing"},
		{18, "landing", unitC, "wheelbuilder", nil, "landed successfully"},
	}
	if len(lines) != len(wants) {
		t.Fatalf("got %d records, want %d:\n%s", len(lines), len(wants), out1)
	}
	for i, w := range wants {
		l := lines[i]
		if l.ID != "shed/event/"+strconv.Itoa(w.seq) {
			t.Errorf("record %d: id = %q, want shed/event/%d", i, l.ID, w.seq)
		}
		wantTime := time.Date(2026, 9, 27, 12, 0, w.seq, 0, time.UTC).Format(time.RFC3339)
		if l.Time != wantTime {
			t.Errorf("record %d: time = %q, want %q", i, l.Time, wantTime)
		}
		if l.Kind != w.kind || l.Topic != w.topic || l.Actor != w.actor || l.Text != w.text {
			t.Errorf("record %d = %+v, want kind=%s topic=%s actor=%s text=%s", i, l, w.kind, w.topic, w.actor, w.text)
		}
		wantCites := w.cites
		if wantCites == nil {
			wantCites = []string{}
		}
		if !reflect.DeepEqual(l.Cites, wantCites) {
			t.Errorf("record %d cites = %v, want %v", i, l.Cites, wantCites)
		}
	}
}

//shed:proves S.ctx.1
func TestRecordsEmptyMissingAndUnreadableLog(t *testing.T) {
	r := testrepo.New(t)

	// No state directory at all: the log is missing.
	if out, _, code := run(t, r.Dir, "records"); out != "" || code != OK {
		t.Errorf("missing log: out = %q, code = %d, want \"\", %d", out, code, OK)
	}

	// An empty log file.
	state := filepath.Join(r.Dir, DefaultStateDir)
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(state, tracker.LogFile)
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, code := run(t, r.Dir, "records"); out != "" || code != OK {
		t.Errorf("empty log: out = %q, code = %d, want \"\", %d", out, code, OK)
	}
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}

	// A log shed records cannot read: a directory in its place, which
	// fails to read regardless of the user running the command.
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := run(t, r.Dir, "records")
	if code == OK {
		t.Errorf("unreadable log: code = %d, want non-zero", code)
	}
	if out != "" {
		t.Errorf("unreadable log: printed a record: %q", out)
	}
	if strings.TrimSpace(errOut) == "" {
		t.Error("unreadable log: no message on stderr saying why")
	}
}
