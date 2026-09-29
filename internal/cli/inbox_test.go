package cli

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// inboxEntries returns the inbox's contested units and horizon changes, one
// per line with runs of whitespace collapsed.
func inboxEntries(out string) (contested, horizon []string) {
	contestedLine := regexp.MustCompile(`^[a-z]+ bounces \d+ `)
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
		unit.Short(b)+" bounces 1 Wave: bounced 1 time, over the threshold of 0",
		unit.Short(a)+" bounces 2 Say goodbye: bounced 2 times, over the threshold of 1")
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
		unit.Short(a)+" bounces 1 Say goodbye: bounced 1 time, over the threshold of 0")
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
