package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// listener is a committee that finds nothing wrong and keeps every bundle
// it is given.
type listener struct{ bundles *[]string }

func (l listener) Run(_ context.Context, turn session.Turn) (session.Result, error) {
	*l.bundles = append(*l.bundles, turn.Bundle)
	return session.Result{Status: "clean"}, nil
}

// contestedUnit opens a unit that adds S.core.2, declares it and makes it
// contested: the repository's bounce threshold is 0, so its first reopen
// contests it.
func contestedUnit(t *testing.T, r *testrepo.Repo, title string) string {
	t.Helper()
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[shed]\nbounce_threshold = 0\n")
	change := openUnit(t, r.Dir, title)
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	spec := testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n"
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", change)
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	mustRun(t, r.Dir, "unit", "reopen", change, "the", "spec", "is", "wrong")
	if u := unitNow(t, r, change); u.State != unit.Contested {
		t.Fatalf("unit is %s, want contested", u.State)
	}
	return change
}

func unitNow(t *testing.T, r *testrepo.Repo, change string) tracker.Unit {
	t.Helper()
	tr, err := tracker.Open(filepath.Join(r.Dir, DefaultStateDir), tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	u, err := tr.Unit(change)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func lastMove(t *testing.T, r *testrepo.Repo, change string) tracker.Event {
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
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == tracker.UnitMoved {
			return events[i]
		}
	}
	t.Fatal("the unit has never moved")
	return tracker.Event{}
}

var answerTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}`)

// answerLine returns the index of the first line of text that holds an
// answer: its kind, its reason and its time. It returns -1 if none does.
func answerLine(text, kind, reason string) int {
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, kind) && strings.Contains(line, reason) && answerTime.MatchString(line) {
			return i
		}
	}
	return -1
}

//shed:proves S.owner.4
func TestAnswerRetry(t *testing.T) {
	r := testrepo.Colocated(t)
	change := contestedUnit(t, r, "Say goodbye")

	if _, stderr, code := run(t, r.Dir, "answer", change, "retry", "narrow", "the", "scope"); code != OK {
		t.Fatalf("answer retry = %d, %q", code, stderr)
	}
	u := unitNow(t, r, change)
	if u.State != unit.Proposed || u.Bounces != 1 {
		t.Errorf("after a retry: %s with %d bounces, want proposed with 1", u.State, u.Bounces)
	}
	if ev := lastMove(t, r, change); ev.From != unit.Contested || ev.To != unit.Proposed || ev.Actor != unit.Owner || ev.Reason != "narrow the scope" {
		t.Errorf("the retry's move = %+v", ev)
	}

	// The retried unit is debated with the answer in its bundle, sealed,
	// and its next bounce contests it again.
	var bundles []string
	debate := func() {
		t.Helper()
		var out, errOut bytes.Buffer
		code := RunWith(context.Background(), []string{"-C", r.Dir, "debate", change}, &out, &errOut, listener{&bundles})
		if code != OK || out.String() != unit.Short(change)+" sealed\n" {
			t.Fatalf("debate = %d, %q, %q", code, out.String(), errOut.String())
		}
	}
	debate()
	if len(bundles) == 0 || answerLine(bundles[len(bundles)-1], "retry", "narrow the scope") < 0 {
		t.Errorf("the bundle after a retry lacks the answer:\n%s", bundles)
	}
	mustRun(t, r.Dir, "unit", "reopen", change, "still", "wrong")
	if u := unitNow(t, r, change); u.State != unit.Contested || u.Bounces != 2 {
		t.Errorf("the next bounce after a retry: %s with %d bounces, want contested with 2", u.State, u.Bounces)
	}

	mustRun(t, r.Dir, "answer", change, "retry", "split", "it", "in", "two")
	bundles = nil
	debate()
	last := bundles[len(bundles)-1]
	first, second := answerLine(last, "retry", "narrow the scope"), answerLine(last, "retry", "split it in two")
	if first < 0 || second < 0 || first >= second {
		t.Errorf("the bundle does not hold both answers oldest first (lines %d and %d):\n%s", first, second, last)
	}
}

//shed:proves S.owner.5
func TestAnswerDefer(t *testing.T) {
	r := testrepo.Colocated(t)
	change := contestedUnit(t, r, "Say goodbye")
	mustRun(t, r.Dir, "answer", change, "retry", "narrow", "the", "scope")
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	mustRun(t, r.Dir, "unit", "reopen", change, "still", "wrong")

	if _, stderr, code := run(t, r.Dir, "answer", change, "defer", "when", "farewells", "are", "on", "the", "horizon"); code != OK {
		t.Fatalf("answer defer = %d, %q", code, stderr)
	}
	u := unitNow(t, r, change)
	if u.State != unit.Archived || u.Shelf != unit.Deferred {
		t.Errorf("after a defer: %s on shelf %q, want archived on the deferred shelf", u.State, u.Shelf)
	}
	if ev := lastMove(t, r, change); ev.From != unit.Contested || ev.To != unit.Archived || ev.Actor != unit.Owner {
		t.Errorf("the defer's move = %+v", ev)
	}

	// The entry is on the archive branch the remote holds.
	entry := r.GitRemote("show", vcs.ArchiveBranch+":"+archive.Path(unit.Deferred, change))
	_, decision, _ := strings.Cut(entry, "## What would change the decision\n")
	decision, _, _ = strings.Cut(decision, "\n## ")
	if !strings.Contains(entry, "- Shelf: deferred") || !strings.Contains(decision, "when farewells are on the horizon") {
		t.Errorf("the entry does not say what would change the decision:\n%s", entry)
	}
	if !strings.Contains(entry, "S.core.2") {
		t.Errorf("the entry lacks the proposal's spec changes:\n%s", entry)
	}
	if answerLine(entry, "retry", "narrow the scope") < 0 {
		t.Errorf("the entry lacks the owner's answers:\n%s", entry)
	}
}

//shed:proves S.owner.6
func TestAnswerRefusals(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	contested := contestedUnit(t, r, "Say goodbye")
	proposed := openUnit(t, r.Dir, "Wave")
	sealed := openUnit(t, r.Dir, "Bow")
	seal(t, state, sealed)

	log := filepath.Join(state, tracker.LogFile)
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"answer", proposed, "retry", "try", "again"},
		{"answer", proposed, "defer", "later"},
		{"answer", sealed, "retry", "try", "again"},
		{"answer", sealed, "defer", "later"},
		{"answer", contested, "reject", "off", "the", "charter"},
		{"answer", contested, "proposed", "try", "again"},
		{"answer", contested, "retry"},
		{"answer", contested, "defer"},
		{"answer", contested, "retry", "  "},
		{"answer", contested, "defer", "", " "},
		{"answer", contested},
	} {
		if _, stderr, code := run(t, r.Dir, args...); code == OK || stderr == "" {
			t.Errorf("%q = %d, %q; want it refused", args, code, stderr)
		}
	}
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("refused answers recorded events:\n%s", after[len(before):])
	}
	for change, want := range map[string]unit.State{contested: unit.Contested, proposed: unit.Proposed, sealed: unit.Sealed} {
		if u := unitNow(t, r, change); u.State != want {
			t.Errorf("unit %s is %s after refused answers, want %s", unit.Short(change), u.State, want)
		}
	}
}
