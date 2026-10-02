package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// contestToAt opens the tracker directly with a clock fixed at at, seals
// change and reopens it, so the bounce that makes it contested (with
// bounce_threshold 0) lands at exactly at.
func contestToAt(t *testing.T, state, change string, at time.Time) {
	t.Helper()
	tr, err := tracker.Open(state, tracker.Options{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Seal(change, "main1", "unitcommit", tracker.Footprint{}, unit.Committee, "consensus", nil); err != nil {
		t.Fatal(err)
	}
	if err := tr.Reopen(change, unit.Owner, "the spec is wrong", false); err != nil {
		t.Fatal(err)
	}
}

// inboxEntries returns the inbox's contested units and horizon changes, one
// per line with runs of whitespace collapsed. A contested unit new since the
// last recorded inbox reads "<change> new bounces ...".
func inboxEntries(out string) (contested, horizon []string) {
	contestedLine := regexp.MustCompile(`^[a-z]+ (new )?bounces \d+ `)
	horizonLine := regexp.MustCompile(`^(added|changed|removed) H\.`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		switch {
		case contestedLine.MatchString(line):
			contested = append(contested, line)
		case horizonLine.MatchString(line):
			horizon = append(horizon, line)
		}
	}
	return contested, horizon
}

func wantEntries(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s =\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// mainClone is a clone of the remote that moves main on the remote, and so
// on the repository once it fetches.
type mainClone struct {
	t     *testing.T
	repo  *testrepo.Repo
	clone *testrepo.Repo
}

func newMainClone(t *testing.T, r *testrepo.Repo) *mainClone {
	t.Helper()
	c := testrepo.New(t)
	c.Git("clone", "-q", r.Remote, ".")
	// Git refuses to fetch into a checked-out branch, so the repository's
	// HEAD leaves main, as jj leaves it.
	r.Git("checkout", "-q", "--detach")
	return &mainClone{t: t, repo: r, clone: c}
}

// commit commits a horizon to main and brings main up to it in the
// repository.
func (m *mainClone) commit(horizon string) string {
	m.t.Helper()
	m.clone.Write("horizon.md", horizon)
	commit := m.clone.Commit("horizon")
	m.clone.Git("push", "-q", "origin", "main")
	m.repo.Git("fetch", "-q", "origin", "main:main")
	return commit
}

// rewrite replaces main's last commit with another holding the horizon, so
// the commit main was at is no longer its ancestor.
func (m *mainClone) rewrite(horizon string) string {
	m.t.Helper()
	m.clone.Write("horizon.md", horizon)
	m.clone.Git("add", "-A")
	m.clone.Git("commit", "-q", "--amend", "-m", "horizon, rewritten")
	commit := m.clone.Git("rev-parse", "HEAD")
	m.clone.Git("push", "-q", "-f", "origin", "main")
	m.repo.Git("fetch", "-q", "-f", "origin", "+main:main")
	return commit
}

const (
	// horizonSings, from testrepo.Horizon, adds H.greet.4 above the others,
	// rewraps H.greet.1 and moves H.greet.2 to near.
	horizonSings = `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool
  says   hello.
- **H.greet.2** (near) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`
	// horizonLoud, from horizonSings, rewords H.greet.4 and removes
	// H.greet.3.
	horizonLoud = `# Horizon

- **H.greet.4** (eventual) The tool sings loudly.
- **H.greet.1** (soon, realised) The tool
  says   hello.
- **H.greet.2** (near) The tool says goodbye.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`
)

//shed:proves S.owner.1
func TestInboxListsContestedUnits(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	a := openUnit(t, r.Dir, "Say goodbye")
	b := openUnit(t, r.Dir, "Wave")
	c := openUnit(t, r.Dir, "Shout")
	openUnit(t, r.Dir, "Whisper")

	contested, _ := inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "contested units with none", contested)

	// Wave is contested first, on its first bounce.
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\n")
	seal(t, state, b)
	mustRun(t, r.Dir, "unit", "reopen", b, "the", "spec", "is", "wrong")

	// Say goodbye is contested next, on its second bounce, and Shout is
	// contested and then archived.
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 1\n")
	for _, change := range []string{a, a, c, c} {
		seal(t, state, change)
		mustRun(t, r.Dir, "unit", "reopen", change, "still", "wrong")
	}
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = tr.Archive(c, unit.Deferred, unit.Owner, "later")
	tr.Close()
	if err != nil {
		t.Fatal(err)
	}

	contested, _ = inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "contested units", contested,
		unit.Short(b)+" new bounces 1 wait 0h0m Wave: bounced 1 time, over the threshold of 0",
		unit.Short(a)+" new bounces 2 wait 0h0m Say goodbye: bounced 2 times, over the threshold of 1")
}

//shed:proves S.owner.2
func TestInboxListsHorizonChanges(t *testing.T) {
	r := testrepo.Colocated(t)
	m := newMainClone(t, r)

	out := mustRun(t, r.Dir, "inbox")
	if _, horizon := inboxEntries(out); len(horizon) != 0 {
		t.Errorf("the first inbox lists horizon changes:\n%s", out)
	}
	m.commit(horizonSings)
	_, horizon := inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "horizon changes", horizon,
		"added H.greet.4 eventual",
		"changed H.greet.2 near")

	m.commit(horizonLoud)
	_, horizon = inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "horizon changes after a rewording and a removal", horizon,
		"changed H.greet.4 eventual",
		"removed H.greet.3 distant")

	_, horizon = inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "horizon changes with main unmoved", horizon)

	// Main no longer holds the recorded commit: the inbox says so, lists
	// nothing and starts afresh from the main it read.
	m.rewrite(testrepo.Horizon)
	out = mustRun(t, r.Dir, "inbox")
	if _, horizon := inboxEntries(out); len(horizon) != 0 || !strings.Contains(out, "not an ancestor of main") {
		t.Errorf("inbox after main was rewritten =\n%s", out)
	}
	m.commit(horizonSings)
	_, horizon = inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "horizon changes after starting afresh", horizon,
		"added H.greet.4 eventual",
		"changed H.greet.2 near")
}

// TestInboxNamesRefinedParent checks that a listed horizon clause whose
// refines tag differs between the recorded commit and main names each parent
// it is judged at, with the parent's tier on the commit whose tag names it,
// recorded commit first, that a clause whose tag is unchanged or absent names
// none, and that a peek names the same parents.
//
//shed:proves S.owner.13
func TestInboxNamesRefinedParent(t *testing.T) {
	r := testrepo.Colocated(t)
	m := newMainClone(t, r)
	horizon := func(clauses string) string {
		return "# Horizon\n\n" + clauses + "\n## Milestones\n\n- **M1** Greetings. H.greet.1 to H.greet.2.\n"
	}
	steps := []struct {
		name    string
		horizon string
		want    []string
	}{
		{"added clauses that refine name their parent", horizon(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near, refines H.greet.3) The tool greets in French.
- **H.greet.6** (soon, refines H.greet.3) The tool greets in German.
`), []string{
			"added H.greet.4 eventual",
			"added H.greet.5 near parent H.greet.3 (distant)",
			"added H.greet.6 soon parent H.greet.3 (distant)",
		}},
		{"a retargeted tag names the old parent, then the new, and an unchanged tag names none", horizon(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near, refines H.greet.4) The tool greets in French.
- **H.greet.6** (soon, refines H.greet.3) The tool greets in German, formally.
`), []string{
			"changed H.greet.5 near parent H.greet.3 (distant), H.greet.4 (eventual)",
			"changed H.greet.6 soon",
		}},
		{"a new tag names the parent's tier on main and a dropped tag the old parent", horizon(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon, refines H.greet.3) The tool says goodbye.
- **H.greet.3** (eventual) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near) The tool greets in French.
- **H.greet.6** (soon, refines H.greet.3) The tool greets in German, formally.
`), []string{
			"changed H.greet.2 soon parent H.greet.3 (eventual)",
			"changed H.greet.3 eventual",
			"changed H.greet.5 near parent H.greet.4 (eventual)",
		}},
		{"a removed clause names the parent's tier on the recorded commit", horizon(`- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon, refines H.greet.3) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool greets everyone at once.
- **H.greet.5** (near) The tool greets in French.
`), []string{
			"changed H.greet.3 distant",
			"removed H.greet.6 soon parent H.greet.3 (eventual)",
		}},
	}

	mustRun(t, r.Dir, "inbox")
	for _, step := range steps {
		m.commit(step.horizon)
		_, peeked := inboxEntries(mustRun(t, r.Dir, "inbox", "-peek"))
		wantEntries(t, step.name+", peeked", peeked, step.want...)
		_, listed := inboxEntries(mustRun(t, r.Dir, "inbox"))
		wantEntries(t, step.name, listed, step.want...)
	}
}

//shed:proves S.owner.3
func TestInboxRecordsWhatItRead(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	m := newMainClone(t, r)
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\n")
	a := openUnit(t, r.Dir, "Say goodbye")
	b := openUnit(t, r.Dir, "Wave")
	seal(t, state, a)
	mustRun(t, r.Dir, "unit", "reopen", a, "the", "spec", "is", "wrong")
	seal(t, state, b)
	mustRun(t, r.Dir, "unit", "move", b, "implementing", "picked", "up")

	// units describes each unit's state and the moves and sessions it has
	// had.
	units := func() string {
		t.Helper()
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer tr.Close()
		var sb strings.Builder
		for _, change := range []string{a, b} {
			u, err := tr.Unit(change)
			if err != nil {
				t.Fatal(err)
			}
			events, err := tr.Events(change)
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := tr.Sessions(change)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&sb, "%s %s bounces %d sessions %d\n", unit.Short(change), u.State, u.Bounces, len(sessions))
			for _, ev := range events {
				switch ev.Kind {
				case tracker.UnitMoved, tracker.SessionStarted, tracker.SessionFinished:
					fmt.Fprintf(&sb, "  %d %s\n", ev.Seq, tracker.Describe(ev))
				}
			}
		}
		return sb.String()
	}
	before := units()

	// A peek records nothing, so the next inbox is still the first.
	mustRun(t, r.Dir, "inbox", "-peek")
	m.commit(horizonSings)
	if _, horizon := inboxEntries(mustRun(t, r.Dir, "inbox")); len(horizon) != 0 {
		t.Errorf("the first inbox after a peek lists horizon changes: %v", horizon)
	}

	m.commit(horizonLoud)
	peeked := mustRun(t, r.Dir, "inbox", "-peek")
	contested, horizon := inboxEntries(peeked)
	wantEntries(t, "peeked contested units", contested,
		unit.Short(a)+" bounces 1 wait 0h0m Say goodbye: bounced 1 time, over the threshold of 0")
	wantEntries(t, "peeked horizon changes", horizon,
		"changed H.greet.4 eventual",
		"removed H.greet.3 distant")
	if again := mustRun(t, r.Dir, "inbox", "-peek"); again != peeked {
		t.Errorf("a second peek =\n%s\nwant\n%s", again, peeked)
	}
	if out := mustRun(t, r.Dir, "inbox"); out != peeked {
		t.Errorf("inbox after peeks =\n%s\nwant\n%s", out, peeked)
	}

	// The recorded commit survives a rebuild of the tracker.
	mustRun(t, r.Dir, "tracker", "rebuild")
	if _, horizon := inboxEntries(mustRun(t, r.Dir, "inbox")); len(horizon) != 0 {
		t.Errorf("inbox after a rebuild lists horizon changes it already listed: %v", horizon)
	}
	m.commit(horizonSings)
	mustRun(t, r.Dir, "tracker", "rebuild")
	_, horizon = inboxEntries(mustRun(t, r.Dir, "inbox"))
	wantEntries(t, "horizon changes after a rebuild", horizon,
		"changed H.greet.4 eventual",
		"added H.greet.3 distant")

	if after := units(); after != before {
		t.Errorf("reading the inbox moved units or sessions:\n%s\nwant\n%s", after, before)
	}
}

//shed:proves S.owner.7
func TestInboxMarksNewContestedUnits(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\n")
	a := openUnit(t, r.Dir, "Say goodbye")
	b := openUnit(t, r.Dir, "Wave")
	contest := func(change string) {
		t.Helper()
		seal(t, state, change)
		mustRun(t, r.Dir, "unit", "reopen", change, "the", "spec", "is", "wrong")
	}
	line := func(change, title string, bounces int, isNew bool) string {
		mark := ""
		if isNew {
			mark = "new "
		}
		times := "times"
		if bounces == 1 {
			times = "time"
		}
		return fmt.Sprintf("%s %sbounces %d wait 0h0m %s: bounced %d %s, over the threshold of 0", unit.Short(change), mark, bounces, title, bounces, times)
	}
	inbox := func(what string, args []string, want ...string) {
		t.Helper()
		contested, _ := inboxEntries(mustRun(t, r.Dir, append([]string{"inbox"}, args...)...))
		wantEntries(t, what, contested, want...)
	}

	// With no inbox recorded, every contested unit is new, and a peek
	// leaves it so.
	contest(a)
	inbox("peeked before any inbox", []string{"-peek"}, line(a, "Say goodbye", 1, true))
	inbox("the first inbox", nil, line(a, "Say goodbye", 1, true))

	// Only a unit contested since the last recorded inbox is new. A peek
	// marks against that inbox, so it marks the same units.
	contest(b)
	inbox("peeked after a unit is contested", []string{"-peek"},
		line(a, "Say goodbye", 1, false), line(b, "Wave", 1, true))
	inbox("inbox after a unit is contested", nil,
		line(a, "Say goodbye", 1, false), line(b, "Wave", 1, true))
	inbox("inbox with nothing contested since", nil,
		line(a, "Say goodbye", 1, false), line(b, "Wave", 1, false))

	// A unit retried and contested again is new again, and the last
	// recorded inbox survives a rebuild of the tracker.
	mustRun(t, r.Dir, "answer", a, "retry", "narrow", "the", "scope")
	contest(a)
	mustRun(t, r.Dir, "tracker", "rebuild")
	inbox("inbox after a retry and a rebuild", nil,
		line(b, "Wave", 1, false), line(a, "Say goodbye", 2, true))
	mustRun(t, r.Dir, "tracker", "rebuild")
	inbox("inbox after another rebuild", nil,
		line(b, "Wave", 1, false), line(a, "Say goodbye", 2, false))
}

// TestInboxShowsWaitAndOverdue checks the wait shed inbox renders beside a
// contested unit and the overdue mark, against an injected clock for both
// the moment the unit became contested and the moment the inbox is read: a
// wait well under shed.contested_timeout renders and is not overdue; a wait
// exactly at the timeout renders the same whole minute as one 30 seconds
// beyond it, is not overdue, while the one 30 seconds beyond is; -peek and a
// full read agree; and a zero timeout marks no unit overdue however long the
// wait.
//
//shed:proves S.owner.15
func TestInboxShowsWaitAndOverdue(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\ncontested_timeout = \"72h\"\n")
	a := openUnit(t, r.Dir, "Say goodbye")

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	contestToAt(t, state, a, base)

	line := func(wait string, overdue bool) string {
		mark := ""
		if overdue {
			mark = " overdue"
		}
		return unit.Short(a) + " new bounces 1 wait " + wait + mark + " Say goodbye: bounced 1 time, over the threshold of 0"
	}
	peek := func(now time.Time) []string {
		t.Helper()
		contested, _ := inboxEntries(runAt(t, r.Dir, state, now, env.inbox, "-peek"))
		return contested
	}

	wantEntries(t, "well under the timeout", peek(base.Add(26*time.Hour+5*time.Minute+10*time.Second)), line("26h5m", false))
	wantEntries(t, "exactly at the timeout", peek(base.Add(72*time.Hour)), line("72h0m", false))
	wantEntries(t, "30s beyond the timeout, same rendered minute", peek(base.Add(72*time.Hour+30*time.Second)), line("72h0m", true))

	// A zero timeout turns expiry off, however long the wait.
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\ncontested_timeout = \"0s\"\n")
	wantEntries(t, "a zero timeout", peek(base.Add(1000*time.Hour)), line("1000h0m", false))
	r.Write(".shed/config.toml", "[shed]\nbounce_threshold = 0\ncontested_timeout = \"72h\"\n")

	// A full read shows the same wait and mark as a peek, with nothing yet
	// recorded against this unit becoming contested.
	contested, _ := inboxEntries(runAt(t, r.Dir, state, base.Add(72*time.Hour+30*time.Second), env.inbox))
	wantEntries(t, "a full read", contested, line("72h0m", true))
}

// sampledEntries returns the lines of the inbox's sampled amendments, with
// runs of whitespace collapsed: for each unit a line "<change> <commit>
// <title>", then a line "<added|changed|removed> <clause>" for each horizon
// clause it amended.
func sampledEntries(out string) []string {
	var entries []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		switch {
		case line == "Sampled amendments:":
			in = true
		case line == "":
			in = false
		case in:
			entries = append(entries, line)
		}
	}
	return entries
}

//shed:proves S.owner.11 S.owner.12
func TestInboxListsSampledAmendments(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[owner]\nsample_every = 1\n")

	// land lands a unit whose change writes the horizon, and returns the
	// line the inbox lists it under.
	land := func(title, horizon string) string {
		t.Helper()
		change := openUnit(t, r.Dir, title)
		dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
		if err := os.WriteFile(filepath.Join(dir, "horizon.md"), []byte(horizon), 0o644); err != nil {
			t.Fatal(err)
		}
		seal(t, state, change)
		for _, s := range []string{"implementing", "verifying", "queued"} {
			mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
		}
		out := mustRun(t, r.Dir, "land", change)
		commit := r.GitRemote("rev-parse", "main")
		if !strings.HasPrefix(out, "landed "+unit.Short(change)+" on main as "+commit) {
			t.Fatalf("land %s = %q", title, out)
		}
		return unit.Short(change) + " " + shortCommit(commit) + " " + title
	}
	sampled := func(what string, args []string, want ...string) {
		t.Helper()
		wantEntries(t, what, sampledEntries(mustRun(t, r.Dir, append([]string{"inbox"}, args...)...)), want...)
	}

	// Sing adds H.greet.4, moves H.greet.2 to near and only marks
	// H.greet.3 realised. Realise only marks H.greet.2 realised, so it is
	// no horizon amendment. Narrow removes H.greet.3.
	sing := land("Sing", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near) The tool says goodbye.
- **H.greet.3** (distant, realised) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	land("Realise", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near, realised) The tool says goodbye.
- **H.greet.3** (distant, realised) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	narrow := land("Narrow", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near, realised) The tool says goodbye.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)

	// With no inbox recorded, every sampled unit is listed, in landing
	// order, and a peek lists the same.
	all := []string{sing, "added H.greet.4", "changed H.greet.2", narrow, "removed H.greet.3"}
	sampled("peeked sampled amendments", []string{"-peek"}, all...)
	sampled("sampled amendments", nil, all...)
	sampled("sampled amendments with none since", nil)
	sampled("peeked sampled amendments with none since", []string{"-peek"})

	// Every second horizon amendment is sampled, counting on from the
	// amendments already landed, and the count survives a rebuild.
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[owner]\nsample_every = 2\n")
	land("Hum", `# Horizon

- **H.greet.4** (eventual) The tool hums.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near, realised) The tool says goodbye.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	mustRun(t, r.Dir, "tracker", "rebuild")
	whistle := land("Whistle", `# Horizon

- **H.greet.4** (eventual) The tool whistles.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (near, realised) The tool says goodbye.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	mustRun(t, r.Dir, "tracker", "rebuild")
	sampled("peeked sampled amendments after a rebuild", []string{"-peek"}, whistle, "changed H.greet.4")
	sampled("sampled amendments after a rebuild", nil, whistle, "changed H.greet.4")
	sampled("sampled amendments with none since, again", nil)
}
