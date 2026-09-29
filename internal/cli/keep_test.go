package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// logLines returns the lines of the tracker's event log, each decoded as a
// JSON object.
func logLines(t *testing.T, state string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(state, tracker.LogFile))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

// holds reports whether v, or any value nested in it, is want.
func holds(v, want any) bool {
	switch v := v.(type) {
	case map[string]any:
		for _, x := range v {
			if holds(x, want) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if holds(x, want) {
				return true
			}
		}
	default:
		return v == want
	}
	return false
}

// without returns a copy of ev without the named top-level fields.
func without(ev map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for k, v := range ev {
		out[k] = v
	}
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

//shed:proves S.owner.10 S.owner.9 S.owner.6
func TestAnswerKeepsACharterClause(t *testing.T) {
	// The charter on main holds C1, C2, C4 and C5; C3 is retired.
	r := retiredCharterRepo(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[shed]\nbounce_threshold = 0\n")
	repo, err := vcs.Open(context.Background(), r.Dir, state, vcs.Options{Remote: "origin",
		Landing: vcs.Identity{Name: "lander", Email: "lander@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	q := func(change, title string) string { return unit.Short(change) + " " + title }
	reject := func(title string, citations ...string) string {
		t.Helper()
		change := openUnit(t, r.Dir, title)
		shelveRejected(t, repo, state, change, title, citations...)
		return change
	}
	inbox := func(what string, want ...string) {
		t.Helper()
		wantEntries(t, what, inboxQuestions(mustRun(t, r.Dir, "inbox")), want...)
	}

	// Two rejected units cite C2 and two cite C4; one cites C1.
	wave := reject("Wave", "C2", "C4")
	shout := reject("Shout", "C2@HEAD~1", "C1")
	bow := reject("Bow", "C4")
	inbox("questions before a keep",
		"C2 new: "+q(wave, "Wave")+"; "+q(shout, "Shout"),
		"C4 new: "+q(wave, "Wave")+"; "+q(bow, "Bow"))
	whisper := contestedUnit(t, r, "Whisper")

	// Refused answers to a clause record nothing.
	refused := func(what string) {
		t.Helper()
		before, err := os.ReadFile(filepath.Join(state, tracker.LogFile))
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{
			{"answer", "C2", "retry", "try", "again"},
			{"answer", "C2", "defer", "later"},
			{"answer", "C2", "reject", "breaks", "C2"},
			{"answer", "C2", "kept", "still", "right"},
			{"answer", "C2@HEAD", "keep", "still", "right"},
			{"answer", "C4@HEAD~1", "keep", "still", "right"},
			{"answer", "C3", "keep", "it", "is", "retired"},
			{"answer", "C9", "keep", "no", "such", "clause"},
			{"answer", "C1", "keep", "no", "question"},
			{"answer", "C5", "keep", "no", "question"},
			{"answer", "C4", "keep"},
			{"answer", "C4", "keep", " "},
			{"answer", "C4", "keep", "", " "},
			{"answer", "C4"},
			{"answer", whisper, "keep", "still", "right"},
		} {
			if _, stderr, code := run(t, r.Dir, args...); code == OK || stderr == "" {
				t.Errorf("%s: %q = %d, %q; want it refused", what, args, code, stderr)
			}
		}
		after, err := os.ReadFile(filepath.Join(state, tracker.LogFile))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Errorf("%s: refused answers recorded events:\n%s", what, after[len(before):])
		}
	}
	refused("before a keep")

	// units describes each unit's state and its moves.
	units := func(changes ...string) string {
		t.Helper()
		var sb strings.Builder
		for _, change := range changes {
			u := unitNow(t, r, change)
			sb.WriteString(unit.Short(change) + " " + string(u.State) + " " + string(u.Shelf) + "\n")
			sb.WriteString(lastMove(t, r, change).Reason + "\n")
		}
		return sb.String()
	}
	all := []string{wave, shout, bow, whisper}
	unitsBefore := units(all...)
	archiveBefore := r.GitRemote("rev-parse", vcs.ArchiveBranch)

	// The owner keeps C2. The keep is one event in the log, by the owner,
	// with the reason, naming C2 and the latest event before it.
	before := logLines(t, state)
	latest := before[len(before)-1]["seq"]
	if _, stderr, code := run(t, r.Dir, "answer", "C2", "keep", "shouting", "is", "still", "wrong"); code != OK {
		t.Fatalf("answer C2 keep = %d, %q", code, stderr)
	}
	after := logLines(t, state)
	if len(after) != len(before)+1 {
		t.Fatalf("a keep recorded %d events, want 1", len(after)-len(before))
	}
	keep := after[len(after)-1]
	if keep["actor"] != string(unit.Owner) || keep["reason"] != "shouting is still wrong" {
		t.Errorf("the keep's actor and reason = %v, %v", keep["actor"], keep["reason"])
	}
	if rest := without(keep, "seq", "time", "kind", "actor", "reason"); !holds(rest, "C2") || !holds(rest, latest) {
		t.Errorf("the keep %v does not record C2 and the latest event before it, %v", keep, latest)
	}

	// The keep moved no unit and changed no archive entry.
	if got := units(all...); got != unitsBefore {
		t.Errorf("a keep moved units:\n%s\nwant\n%s", got, unitsBefore)
	}
	if got := r.GitRemote("rev-parse", vcs.ArchiveBranch); got != archiveBefore {
		t.Errorf("a keep moved the archive branch from %s to %s", archiveBefore, got)
	}

	// C2 leaves the inbox; C4 is unaffected.
	inbox("questions after a keep", "C4: "+q(wave, "Wave")+"; "+q(bow, "Bow"))

	// A kept clause has no question, so keeping it again is refused.
	refused("after a keep")
	if _, _, code := run(t, r.Dir, "answer", "C2", "keep", "again"); code == OK {
		t.Error("keeping a clause with no question listed was accepted")
	}

	// One unit archived after the keep citing C2 raises no question; a
	// second does, counting only the two, and the question is new.
	bellow := reject("Bellow", "C2")
	inbox("questions after one rejection since the keep", "C4: "+q(wave, "Wave")+"; "+q(bow, "Bow"))
	mustRun(t, r.Dir, "answer", whisper, "reject", "shouts", "(C2)")
	c2 := "C2 new: " + q(bellow, "Bellow") + "; " + q(whisper, "Whisper")
	wantEntries(t, "peeked questions after two rejections since the keep",
		inboxQuestions(mustRun(t, r.Dir, "inbox", "-peek")), c2, "C4: "+q(wave, "Wave")+"; "+q(bow, "Bow"))

	// The keep survives a rebuild of the tracker.
	mustRun(t, r.Dir, "tracker", "rebuild")
	inbox("questions after a rebuild", c2, "C4: "+q(wave, "Wave")+"; "+q(bow, "Bow"))

	// Keeping C4 leaves the re-raised C2 alone.
	mustRun(t, r.Dir, "answer", "C4", "keep", "politeness", "matters")
	inbox("questions after keeping C4", "C2: "+q(bellow, "Bellow")+"; "+q(whisper, "Whisper"))
}
