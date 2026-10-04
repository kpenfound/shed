package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// colocatedRepo returns a colocated repository after write populates its
// first commit's content, however it likes: some clauses, none, or a
// problem S.doc.5 names.
func colocatedRepo(t *testing.T, write func(*testrepo.Repo)) *testrepo.Repo {
	t.Helper()
	testrepo.RequireJJ(t)
	r := testrepo.New(t)
	write(r)
	r.Write(".gitignore", ".shed/\n")
	r.Init()
	r.Commit("documents")
	r.Remote = t.TempDir()
	r.GitRemoteInit()
	r.Git("remote", "add", "origin", r.Remote)
	r.Git("push", "-q", "origin", "main")
	r.JJ("git", "init", "--colocate")
	return r
}

// emptyColocated returns a colocated repository whose first commit holds no
// clauses at all, so it gives no clause record (S.ctx.3) and a records test
// can check event records free of clause noise.
func emptyColocated(t *testing.T) *testrepo.Repo {
	return colocatedRepo(t, func(r *testrepo.Repo) {
		r.Write("charter.md", "# Charter\n")
		r.Write("spec/core.md", "# Core\n")
		r.Write("horizon.md", "# Horizon\n")
	})
}

// commitDocs writes the charter, spec/core.md and horizon documents and
// commits them under the given author (the repository's default identity
// when empty), returning the commit's full hash.
func commitDocs(t *testing.T, r *testrepo.Repo, author, message, charter, spec, horizon string) string {
	t.Helper()
	r.Write("charter.md", charter)
	r.Write("spec/core.md", spec)
	r.Write("horizon.md", horizon)
	r.Git("add", "-A")
	args := []string{"commit", "-q", "--allow-empty", "-m", message}
	if author != "" {
		args = append(args, "--author="+author)
	}
	r.Git(args...)
	return strings.TrimSpace(r.Git("rev-parse", "HEAD"))
}

// landDocsUnit opens a unit, overwrites spec/core.md and horizon.md in its
// workspace, seals it, moves it through to queued and lands it, returning
// its change ID and the commit it landed as. It leaves the charter alone:
// landing a unit that alters it is rejected.
func landDocsUnit(t *testing.T, r *testrepo.Repo, state, title, spec, horizon string) (change, commit string) {
	t.Helper()
	change = openUnit(t, r.Dir, title)
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	for path, content := range map[string]string{"spec/core.md": spec, "horizon.md": horizon} {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seal(t, state, change)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
	}
	mustRun(t, r.Dir, "land", change)
	return change, strings.TrimSpace(r.Git("rev-parse", "main"))
}

// commitTime returns a commit's committer time in RFC 3339 UTC, as git
// records it, so a test can check a clause record's time field against the
// repository's own ground truth.
func commitTime(t *testing.T, r *testrepo.Repo, hash string) string {
	t.Helper()
	out := r.Git("show", "-s", "--format=%cI", hash)
	ts, err := time.Parse(time.RFC3339, strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	return ts.UTC().Format(time.RFC3339)
}

// commitAuthor returns a commit's author name, as git records it.
func commitAuthor(t *testing.T, r *testrepo.Repo, hash string) string {
	t.Helper()
	return strings.TrimSpace(r.Git("show", "-s", "--format=%an", hash))
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
	for _, l := range rawLines(out) {
		lines = append(lines, decodeRecord(t, l))
	}
	return lines
}

// rawLines splits shed records' stdout into its non-blank lines, in order,
// without decoding them: a mix of event and clause records has to be sliced
// before each part can be decoded with its own key set.
func rawLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// clauseRecordKeyOrder matches the exact key order S.ctx.4 requires.
var clauseRecordKeyOrder = regexp.MustCompile(
	`^\{\s*"id"\s*:.*"time"\s*:.*"kind"\s*:.*"topic"\s*:.*"actor"\s*:.*"cites"\s*:.*"document"\s*:.*"commit"\s*:.*"before"\s*:.*"after"\s*:.*\}\s*$`)

// clauseRecordLine is a decoded clause record (S.ctx.4).
type clauseRecordLine struct {
	ID       string
	Time     string
	Kind     string
	Topic    string
	Actor    string
	Cites    []string
	Document string
	Commit   string
	Before   string
	After    string
}

func decodeClauseRecord(t *testing.T, line string) clauseRecordLine {
	t.Helper()
	if !clauseRecordKeyOrder.MatchString(line) {
		t.Fatalf("clause record keys out of order: %s", line)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("decoding %q: %v", line, err)
	}
	if len(fields) != 10 {
		t.Fatalf("clause record has %d keys, want exactly 10: %s", len(fields), line)
	}
	var r clauseRecordLine
	for key, dst := range map[string]any{
		"id": &r.ID, "time": &r.Time, "kind": &r.Kind, "topic": &r.Topic, "actor": &r.Actor,
		"document": &r.Document, "commit": &r.Commit, "before": &r.Before, "after": &r.After,
	} {
		raw, ok := fields[key]
		if !ok {
			t.Fatalf("clause record missing %q: %s", key, line)
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			t.Fatalf("decoding %q of %q: %v", key, line, err)
		}
	}
	citesRaw, ok := fields["cites"]
	if !ok {
		t.Fatalf("clause record missing %q: %s", "cites", line)
	}
	if err := json.Unmarshal(citesRaw, &r.Cites); err != nil {
		t.Fatalf("decoding cites of %q: %v", line, err)
	}
	return r
}

//shed:proves S.ctx.1 S.ctx.2
func TestRecordsPrintsL0Records(t *testing.T) {
	const (
		unitA = "qpvuntsmwlqtqpvuntsmwlqt"
		unitB = "kkkkllllmmmmnnnnoooopppp"
		unitC = "zzzzyyyyxxxxwwwwvvvvuuuu"
	)
	r := emptyColocated(t)
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

	out1 := mustRun(t, r.Dir, "records")
	out2 := mustRun(t, r.Dir, "records")
	if out1 != out2 {
		t.Errorf("shed records is not deterministic:\n%q\nvs\n%q", out1, out2)
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
	r := emptyColocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	// No state directory at all: the log is missing.
	if out, _, code := run(t, r.Dir, "records"); out != "" || code != OK {
		t.Errorf("missing log: out = %q, code = %d, want \"\", %d", out, code, OK)
	}

	// An empty log file.
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

// TestRecordsPrintsClauseRecords checks the shape, ordering and kind of
// clause records across a short history: a plain commit adding clauses, a
// landed unit changing and removing some, and a commit that only rewraps a
// charter clause's text, which gives no record at all.
//
//shed:proves S.ctx.3 S.ctx.4
func TestRecordsPrintsClauseRecords(t *testing.T) {
	r := emptyColocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)

	// An event with no bearing on any clause, so the event-records-first
	// ordering is exercised alongside clause records.
	openUnit(t, r.Dir, "Say hi")

	commit1 := commitDocs(t, r, "Jane Doe <jane@example.com>", "Add clauses",
		"# Charter\n\n- **C1** The tool greets people.\n",
		"# Core\n\n- **S.greet.2** (H.greet.1) Says hi.\n"+
			"- **S.greet.10** (H.greet.1) Says hi twice.\n"+
			"- **S.loud.1** (H.greet.1) Shouts.\n",
		"# Horizon\n\n- **H.greet.1** (near) Say hi.\n\n"+
			"## Milestones\n\n- **M2** Greetings twice.\n- **M1** Greet once.\n")

	// A landed unit amends spec and horizon clauses; it never touches the
	// charter, since landing a unit that does is rejected.
	change, commit2 := landDocsUnit(t, r, state, "Amend spec and horizon",
		"# Core\n\n- **S.greet.1** (H.greet.1) Greets once more.\n"+
			"- **S.greet.2** (H.greet.1) Says hi.\n"+
			"- **S.greet.10** (H.greet.1) Says hi twice.\n",
		"# Horizon\n\n- **H.greet.1** (soon) Say hi warmly.\n\n"+
			"## Milestones\n\n- **M2** Greetings twice.\n- **M1** Greet once.\n")
	checkoutMain(t, r)

	// Rewrapping C1's text changes nothing it says (S.doc.2), so this commit
	// gives no clause record at all, even though the file's bytes differ.
	// Spec and horizon are repeated unchanged.
	commitDocs(t, r, "Jane Doe <jane@example.com>", "Rewrap C1",
		"# Charter\n\n- **C1** The tool\n  greets people.\n",
		"# Core\n\n- **S.greet.1** (H.greet.1) Greets once more.\n"+
			"- **S.greet.2** (H.greet.1) Says hi.\n"+
			"- **S.greet.10** (H.greet.1) Says hi twice.\n",
		"# Horizon\n\n- **H.greet.1** (soon) Say hi warmly.\n\n"+
			"## Milestones\n\n- **M2** Greetings twice.\n- **M1** Greet once.\n")

	out := mustRun(t, r.Dir, "records")
	lines := rawLines(out)
	if len(lines) != 14 {
		t.Fatalf("got %d lines, want 4 event + 10 clause records:\n%s", len(lines), out)
	}
	for _, l := range lines[:4] {
		decodeRecord(t, l) // event records come first (S.ctx.3)
	}

	type want struct {
		id       string
		commit   string
		kind     string
		document string
		before   string
		after    string
	}
	wants := []want{
		{"C1", commit1, "clause.added", "charter", "", "The tool greets people."},
		{"S.greet.2", commit1, "clause.added", "spec", "", "Says hi."},
		{"S.greet.10", commit1, "clause.added", "spec", "", "Says hi twice."},
		{"S.loud.1", commit1, "clause.added", "spec", "", "Shouts."},
		{"H.greet.1", commit1, "clause.added", "horizon", "", "Say hi."},
		{"M1", commit1, "clause.added", "horizon", "", "Greet once."},
		{"M2", commit1, "clause.added", "horizon", "", "Greetings twice."},
		{"S.greet.1", commit2, "clause.added", "spec", "", "Greets once more."},
		{"S.loud.1", commit2, "clause.removed", "spec", "Shouts.", ""},
		{"H.greet.1", commit2, "clause.changed", "horizon", "Say hi.", "Say hi warmly."},
	}
	for i, w := range wants {
		l := decodeClauseRecord(t, lines[4+i])
		wantTopic := ""
		if w.commit == commit2 {
			wantTopic = change
		}
		wantActor := commitAuthor(t, r, w.commit)
		wantTime := commitTime(t, r, w.commit)
		wantID := fmt.Sprintf("shed/clause/%s/%s", w.commit, w.id)
		if l.ID != wantID || l.Time != wantTime || l.Kind != w.kind || l.Topic != wantTopic ||
			l.Actor != wantActor || l.Document != w.document || l.Commit != w.commit ||
			l.Before != w.before || l.After != w.after {
			t.Errorf("record %d = %+v, want id=%s time=%s kind=%s topic=%s actor=%s document=%s commit=%s before=%q after=%q",
				i, l, wantID, wantTime, w.kind, wantTopic, wantActor, w.document, w.commit, w.before, w.after)
		}
		if !reflect.DeepEqual(l.Cites, []string{w.id}) {
			t.Errorf("record %d cites = %v, want [%s]", i, l.Cites, w.id)
		}
	}
}

// TestRecordsMalformedDocumentFallsBackPerDocument checks that a document
// with a problem S.doc.5 names gives no record at the commit holding it,
// that the withholding is per document, and that a later clean commit's
// baseline skips back to the latest commit without the problem, including
// all the way to no clauses at all when every earlier commit had it.
//
//shed:proves S.ctx.3
func TestRecordsMalformedDocumentFallsBackPerDocument(t *testing.T) {
	r := colocatedRepo(t, func(r *testrepo.Repo) {
		// The charter is malformed from the very first commit: a duplicate
		// ID is a problem S.doc.5 names, so no earlier commit ever
		// qualifies as a clean baseline for it.
		r.Write("charter.md", "# Charter\n\n- **C1** First.\n- **C1** Duplicate.\n")
		r.Write("spec/core.md", "# Core\n")
		r.Write("horizon.md", "# Horizon\n")
	})

	// Fixes the charter and adds a spec clause; the charter's baseline is
	// empty, since no earlier commit had it problem-free.
	commit1 := commitDocs(t, r, "", "Fix the charter and add a clause",
		"# Charter\n\n- **C1** First.\n",
		"# Core\n\n- **S.a.1** (H.x.1) Placeholder text.\n",
		"# Horizon\n")

	// The spec becomes malformed with a duplicate ID; the charter stays
	// clean and unchanged, and gives no record either way.
	commitDocs(t, r, "", "Duplicate a spec ID",
		"# Charter\n\n- **C1** First.\n",
		"# Core\n\n- **S.a.1** (H.x.1) Placeholder text.\n- **S.a.1** (H.x.1) Duplicate.\n",
		"# Horizon\n")

	// The spec is fixed again, with new text. Its baseline skips back over
	// the malformed commit to commit1's clean spec, so this is a change,
	// not an addition.
	commit3 := commitDocs(t, r, "", "Fix the spec with new text",
		"# Charter\n\n- **C1** First.\n",
		"# Core\n\n- **S.a.1** (H.x.1) Updated text.\n",
		"# Horizon\n")

	out := mustRun(t, r.Dir, "records")
	lines := rawLines(out)

	type want struct {
		id       string
		commit   string
		kind     string
		document string
		before   string
		after    string
	}
	wants := []want{
		{"C1", commit1, "clause.added", "charter", "", "First."},
		{"S.a.1", commit1, "clause.added", "spec", "", "Placeholder text."},
		{"S.a.1", commit3, "clause.changed", "spec", "Placeholder text.", "Updated text."},
	}
	if len(lines) != len(wants) {
		t.Fatalf("got %d clause records, want %d:\n%s", len(lines), len(wants), out)
	}
	for i, w := range wants {
		l := decodeClauseRecord(t, lines[i])
		wantID := fmt.Sprintf("shed/clause/%s/%s", w.commit, w.id)
		if l.ID != wantID || l.Kind != w.kind || l.Document != w.document ||
			l.Commit != w.commit || l.Before != w.before || l.After != w.after {
			t.Errorf("record %d = %+v, want id=%s kind=%s document=%s commit=%s before=%q after=%q",
				i, l, wantID, w.kind, w.document, w.commit, w.before, w.after)
		}
	}
}

// TestRecordsMissingDocumentIsACleanBaseline checks that a commit lacking a
// document entirely holds none of its clauses: a document going missing
// gives ordinary removed records, not a withheld commit as a problem
// S.doc.5 names would, and the document's reappearance compares against
// that zero-clause commit rather than skipping back past it.
//
//shed:proves S.ctx.3
func TestRecordsMissingDocumentIsACleanBaseline(t *testing.T) {
	r := colocatedRepo(t, func(r *testrepo.Repo) {
		r.Write("charter.md", "# Charter\n")
		r.Write("horizon.md", "# Horizon\n")
		r.Write("spec/core.md", "# Core\n\n- **S.a.1** (H.x.1) Hi.\n- **S.b.1** (H.x.1) Bye.\n")
	})
	// The root commit has no earlier commit to compare against, so its own
	// spec clauses are added (S.ctx.3's first-commit baseline).
	root := strings.TrimSpace(r.Git("rev-parse", "HEAD"))

	// Removing the spec directory entirely: the document is now missing,
	// not malformed, so this commit gives ordinary removed records.
	r.Remove("spec")
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", "Remove the spec")
	commit1 := strings.TrimSpace(r.Git("rev-parse", "HEAD"))

	// Re-adding the spec compares against commit1, which lacks the document
	// and so holds none of its clauses: a clean, zero-clause baseline. If
	// an implementation instead skipped back past commit1 to the root
	// commit's S.a.1, this would wrongly read "changed", not "added".
	commit2 := commitDocs(t, r, "", "Re-add the spec",
		"# Charter\n", "# Core\n\n- **S.a.1** (H.x.1) Hi again.\n", "# Horizon\n")

	out := mustRun(t, r.Dir, "records")
	lines := rawLines(out)
	type want struct {
		id     string
		commit string
		kind   string
		before string
		after  string
	}
	wants := []want{
		{"S.a.1", root, "clause.added", "", "Hi."},
		{"S.b.1", root, "clause.added", "", "Bye."},
		{"S.a.1", commit1, "clause.removed", "Hi.", ""},
		{"S.b.1", commit1, "clause.removed", "Bye.", ""},
		{"S.a.1", commit2, "clause.added", "", "Hi again."},
	}
	if len(lines) != len(wants) {
		t.Fatalf("got %d clause records, want %d:\n%s", len(lines), len(wants), out)
	}
	for i, w := range wants {
		l := decodeClauseRecord(t, lines[i])
		wantID := fmt.Sprintf("shed/clause/%s/%s", w.commit, w.id)
		if l.ID != wantID || l.Kind != w.kind || l.Document != "spec" || l.Commit != w.commit ||
			l.Before != w.before || l.After != w.after {
			t.Errorf("record %d = %+v, want id=%s kind=%s commit=%s before=%q after=%q",
				i, l, wantID, w.kind, w.commit, w.before, w.after)
		}
	}
}

// TestRecordsFailsBeforePrintingAnyRecord checks that a failure reading the
// event log, opening the repository, bringing in main or reading its
// history leaves stdout empty, even when records that step alone would
// have given are otherwise available.
//
//shed:proves S.ctx.5
func TestRecordsFailsBeforePrintingAnyRecord(t *testing.T) {
	t.Run("main cannot be brought in, despite a nonempty event log", func(t *testing.T) {
		r := testrepo.New(t)
		state := filepath.Join(r.Dir, DefaultStateDir)
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		must(t, tr.OpenUnit("qpvuntsmwlqtqpvuntsmwlqt", "Say hi", unit.Painter))
		tr.Close()

		out, errOut, code := run(t, r.Dir, "records")
		if code == OK {
			t.Errorf("code = %d, want non-zero", code)
		}
		if out != "" {
			t.Errorf("printed %q despite a nonempty event log, want no output", out)
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("no message on stderr saying why")
		}
	})

	t.Run("the event log cannot be read, despite real clauses on main", func(t *testing.T) {
		r := testrepo.Colocated(t)
		state := filepath.Join(r.Dir, DefaultStateDir)
		if err := os.MkdirAll(filepath.Join(state, tracker.LogFile), 0o755); err != nil {
			t.Fatal(err)
		}

		out, errOut, code := run(t, r.Dir, "records")
		if code == OK {
			t.Errorf("code = %d, want non-zero", code)
		}
		if out != "" {
			t.Errorf("printed %q despite real clauses on main, want no output", out)
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("no message on stderr saying why")
		}
	})

	t.Run("a document version fails to read for a reason other than not existing", func(t *testing.T) {
		r := colocatedRepo(t, func(r *testrepo.Repo) {
			r.Write("charter.md", "# Charter\n\n- **C1** First.\n")
			r.Write("spec/core.md", "# Core\n")
			r.Write("horizon.md", "# Horizon\n")
		})

		// Turn the charter from a file into a directory: reading it at this
		// commit fails with a type mismatch, not with "missing", so S.ctx.5
		// must abort the whole command rather than treat it as a document
		// that simply isn't there (S.ctx.3's clean, zero-clause baseline).
		r.Remove("charter.md")
		r.Write("charter.md/stray.txt", "oops\n")
		r.Git("add", "-A")
		r.Git("commit", "-q", "-m", "Turn the charter into a directory")

		out, errOut, code := run(t, r.Dir, "records")
		if code == OK {
			t.Errorf("code = %d, want non-zero", code)
		}
		if out != "" {
			t.Errorf("printed %q despite the earlier commit's real clause, want no output", out)
		}
		if strings.TrimSpace(errOut) == "" {
			t.Error("no message on stderr saying why")
		}
	})
}

// TestRecordsLeavesMainTrackerAndRemoteUnchanged checks that shed records
// moves no unit, records nothing in the tracker, leaves main and the
// remote as they were, and that the same state always prints the same
// bytes.
//
//shed:proves S.ctx.5
func TestRecordsLeavesMainTrackerAndRemoteUnchanged(t *testing.T) {
	r := emptyColocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	landUnit(t, r, state, "Say hi", "hi.txt", "hi\n")
	checkoutMain(t, r)
	openUnit(t, r.Dir, "Wave")

	beforeMain := r.Git("rev-parse", "main")
	beforeRemote := r.GitRemote("rev-parse", "main")
	logPath := filepath.Join(state, tracker.LogFile)
	beforeLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	out1 := mustRun(t, r.Dir, "records")
	out2 := mustRun(t, r.Dir, "records")
	if out1 != out2 {
		t.Errorf("shed records is not deterministic:\n%q\nvs\n%q", out1, out2)
	}

	if got := r.Git("rev-parse", "main"); got != beforeMain {
		t.Errorf("records moved main from %s to %s", beforeMain, got)
	}
	if got := r.GitRemote("rev-parse", "main"); got != beforeRemote {
		t.Errorf("records moved the remote's main from %s to %s", beforeRemote, got)
	}
	afterLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeLog, afterLog) {
		t.Errorf("records recorded something in the tracker:\nbefore %s\nafter %s", beforeLog, afterLog)
	}
}
