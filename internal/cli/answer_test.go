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
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", "-estimate", "100", change)
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

	// The retried unit is debated independently, sealed,
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
	mustRun(t, r.Dir, "unit", "reopen", change, "still", "wrong")
	if u := unitNow(t, r, change); u.State != unit.Contested || u.Bounces != 2 {
		t.Errorf("the next bounce after a retry: %s with %d bounces, want contested with 2", u.State, u.Bounces)
	}

	mustRun(t, r.Dir, "answer", change, "retry", "split", "it", "in", "two")
	bundles = nil
	debate()
	tr, err := tracker.Open(filepath.Join(r.Dir, DefaultStateDir), tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	answers, err := tr.Answers(change)
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 2 || answers[0].Reason != "narrow the scope" || answers[1].Reason != "split it in two" {
		t.Fatalf("answer history: %+v", answers)
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
		{"answer", proposed, "reject", "breaks", "C2"},
		{"answer", sealed, "reject", "breaks", "C2"},
		{"answer", contested, "proposed", "try", "again"},
		{"answer", contested, "archive", "breaks", "C2"},
		{"answer", contested, "keep", "still", "right"},
		{"answer", contested, "retry"},
		{"answer", contested, "defer"},
		{"answer", contested, "reject"},
		{"answer", contested, "retry", "  "},
		{"answer", contested, "defer", "", " "},
		{"answer", contested, "reject", " "},
		{"answer", proposed, "approve", "go", "ahead"},
		{"answer", sealed, "approve", "go", "ahead"},
		{"answer", contested, "approve", "go", "ahead"},
		{"answer", contested, "approve", " "},
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

//shed:proves S.shed.17 S.owner.6
func TestAnswerApprove(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	bounced := contestedUnit(t, r, "Say goodbye")

	// A unit whose horizon amendment is eventual goes to the owner.
	change := openUnit(t, r.Dir, "Wave goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	spec := testrepo.Spec + "- **S.core.2** (H.greet.2) Running the tool with --bye waves goodbye.\n"
	horizon := strings.Replace(testrepo.Horizon, "within C2.\n", "within C2.\n- **H.greet.4** (eventual) The tool waves.\n", 1)
	for name, content := range map[string]string{"spec/core.md": spec, "horizon.md": horizon} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", "-estimate", "100", change)
	var bundles []string
	debate := func() string {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := RunWith(context.Background(), []string{"-C", r.Dir, "debate", change}, &out, &errOut, listener{&bundles}); code != OK {
			t.Fatalf("debate = %d, %q, %q", code, out.String(), errOut.String())
		}
		return out.String()
	}
	debate()
	if u := unitNow(t, r, change); u.State != unit.Contested || u.Bounces != 0 {
		t.Fatalf("after the debate: %s with %d bounces, want contested with none", u.State, u.Bounces)
	}

	// Approving a unit contested by its bounces is refused and records
	// nothing.
	log := filepath.Join(state, tracker.LogFile)
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, r.Dir, "answer", bounced, "approve", "go", "ahead"); code == OK || stderr == "" {
		t.Errorf("approving a unit contested by its bounces = %d, %q; want it refused", code, stderr)
	}
	if after, _ := os.ReadFile(log); !bytes.Equal(before, after) {
		t.Errorf("a refused approval recorded events:\n%s", after[len(before):])
	}

	// The owner approves the amendment, and its next debate seals it with
	// no round.
	if _, stderr, code := run(t, r.Dir, "answer", change, "approve", "the", "tool", "will", "wave"); code != OK {
		t.Fatalf("answer approve = %d, %q", code, stderr)
	}
	if u := unitNow(t, r, change); u.State != unit.Proposed || u.Bounces != 0 {
		t.Errorf("after the approval: %s with %d bounces, want proposed with none", u.State, u.Bounces)
	}
	if ev := lastMove(t, r, change); ev.From != unit.Contested || ev.To != unit.Proposed || ev.Actor != unit.Owner || ev.Reason != "the tool will wave" {
		t.Errorf("the approval's move = %+v", ev)
	}
	sessions := len(bundles)
	if out := debate(); out != unit.Short(change)+" sealed\n" {
		t.Errorf("the approved unit's debate printed %q", out)
	}
	if n := len(bundles) - sessions; n != 0 {
		t.Errorf("the approved unit's debate ran %d sessions", n)
	}
	if u := unitNow(t, r, change); u.State != unit.Sealed {
		t.Errorf("the approved unit is %s, want sealed", u.State)
	}
}

// retiredCharterRepo is a colocated repository whose charter on main holds
// C1, C2, C4 and C5: C3 was removed in an earlier commit, so its ID is
// retired.
func retiredCharterRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	testrepo.RequireJJ(t)
	r := testrepo.Minimal(t)
	r.Write(".gitignore", ".shed/\n")
	r.Write("charter.md", "# Charter\n\n"+
		"- **C1** The tool greets people.\n"+
		"- **C2** It never shouts.\n"+
		"- **C3** It greets in English.\n"+
		"- **C4** It is polite.\n"+
		"- **C5** It is brief.\n")
	r.Init()
	r.Commit("documents")
	r.Write("charter.md", "# Charter\n\n"+
		"- **C1** The tool greets people.\n"+
		"- **C2** It never shouts.\n"+
		"- **C4** It is polite.\n"+
		"- **C5** It is brief.\n")
	r.Commit("retire C3")
	r.Remote = t.TempDir()
	r.GitRemoteInit()
	r.Git("remote", "add", "origin", r.Remote)
	r.Git("push", "-q", "origin", "main")
	r.JJ("git", "init", "--colocate")
	return r
}

//shed:proves S.owner.8
func TestAnswerReject(t *testing.T) {
	r := retiredCharterRepo(t)
	change := contestedUnit(t, r, "Say goodbye")
	mustRun(t, r.Dir, "answer", change, "retry", "narrow", "the", "scope")
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	mustRun(t, r.Dir, "unit", "reopen", change, "still", "wrong")

	reason := "Goodbye is rude (C4), against C2, C4 and C1 to C5; see S.core.1 and H.greet.2, not XC1."
	if _, stderr, code := run(t, r.Dir, "answer", change, "reject", reason); code != OK {
		t.Fatalf("answer reject = %d, %q", code, stderr)
	}
	u := unitNow(t, r, change)
	if u.State != unit.Archived || u.Shelf != unit.Rejected {
		t.Errorf("after a reject: %s on shelf %q, want archived on the rejected shelf", u.State, u.Shelf)
	}
	if ev := lastMove(t, r, change); ev.From != unit.Contested || ev.To != unit.Archived || ev.Actor != unit.Owner || ev.Reason != reason {
		t.Errorf("the reject's move = %+v", ev)
	}

	// The entry is on the archive branch the remote holds. It cites the
	// named clauses in order of first appearance, without repeats; the
	// range C1 to C5 skips the retired C3, and XC1 and the spec and horizon
	// IDs name no charter clause.
	entry := r.GitRemote("show", vcs.ArchiveBranch+":"+archive.Path(unit.Rejected, change))
	if !strings.Contains(entry, "- Shelf: rejected") || !strings.Contains(entry, "- Citations: C4, C2, C1, C5\n") {
		t.Errorf("the entry does not cite C4, C2, C1, C5 as violated:\n%s", entry)
	}
	if !strings.Contains(entry, "S.core.2") {
		t.Errorf("the entry lacks the proposal's spec changes:\n%s", entry)
	}
	if answerLine(entry, "retry", "narrow the scope") < 0 {
		t.Errorf("the entry lacks the owner's answers:\n%s", entry)
	}

	// A single whole token with punctuation around it names its clause.
	other := contestedUnit(t, r, "Wave")
	mustRun(t, r.Dir, "answer", other, "reject", "XC1", "shouts", "(C2),", "see", "S.core.1")
	entry = r.GitRemote("show", vcs.ArchiveBranch+":"+archive.Path(unit.Rejected, other))
	if !strings.Contains(entry, "- Citations: C2\n") {
		t.Errorf("the entry does not cite only C2:\n%s", entry)
	}
}

//shed:proves S.owner.8
func TestAnswerRejectRefusals(t *testing.T) {
	r := retiredCharterRepo(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	change := contestedUnit(t, r, "Say goodbye")

	log := filepath.Join(state, tracker.LogFile)
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{
		"off the charter",
		"see S.core.1 and H.greet.2",
		"XC1 is wrong",
		"breaks C2@HEAD",
		"breaks C1 and C2@HEAD~1",
		"C5 to C1",
		"C2 to C2",
		"C1 to C6",
		"breaks C3",
		"breaks C2 and C3",
		"breaks C12",
		"breaks C6",
	} {
		if _, stderr, code := run(t, r.Dir, "answer", change, "reject", reason); code == OK || stderr == "" {
			t.Errorf("reject %q = %d, %q; want it refused", reason, code, stderr)
		}
	}
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("refused rejects recorded events:\n%s", after[len(before):])
	}
	if u := unitNow(t, r, change); u.State != unit.Contested {
		t.Errorf("unit is %s after refused rejects, want contested", u.State)
	}
}
