package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// inboxQuestions returns the charter questions the inbox lists, one per
// question, as "<clause>[ new]: <change> <title>; <change> <title>". The
// questions follow a "Charter questions:" line and end at a blank line; a
// question's line starts with its clause ID and optionally marks it new,
// and each of its units follows on a line of its own, its short change ID
// then its title.
func inboxQuestions(out string) []string {
	questionLine := regexp.MustCompile(`^(C\d+)( new)?$`)
	var questions []string
	var units []string
	head := ""
	flush := func() {
		if head != "" {
			questions = append(questions, head+": "+strings.Join(units, "; "))
		}
		head, units = "", nil
	}
	in := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		switch {
		case line == "Charter questions:":
			in = true
		case !in:
		case line == "":
			flush()
			in = false
		case questionLine.MatchString(line):
			flush()
			head = line
		default:
			units = append(units, line)
		}
	}
	flush()
	return questions
}

// shelveRejected archives a unit on the rejected shelf as the committee
// does, with an entry citing the given IDs verbatim.
func shelveRejected(t *testing.T, repo *vcs.Repo, state, change, title string, citations ...string) {
	t.Helper()
	entry := archive.Format(archive.Record{
		Title: title, Change: change, Shelf: unit.Rejected, Citations: citations,
		Reason: "- o1 (member 1): it breaks the charter",
	})
	if _, err := repo.WriteArchive(context.Background(), archive.Path(unit.Rejected, change), []byte(entry), "rejected: "+title); err != nil {
		t.Fatal(err)
	}
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	if err := tr.Archive(change, unit.Rejected, unit.Committee, "rejected citing "+strings.Join(citations, ", ")); err != nil {
		t.Fatal(err)
	}
}

//shed:proves S.owner.9
func TestInboxListsCharterQuestions(t *testing.T) {
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

	// Units are opened in one order and archived in another, so the order
	// of a question's units is the order they were archived.
	wave := openUnit(t, r.Dir, "Wave")
	shout := openUnit(t, r.Dir, "Shout")
	whisper := openUnit(t, r.Dir, "Whisper")
	later := openUnit(t, r.Dir, "Sing")

	if got := inboxQuestions(mustRun(t, r.Dir, "inbox", "-peek")); len(got) != 0 {
		t.Errorf("questions with an empty shelf = %q", got)
	}

	// The owner rejects Say goodbye citing C4 and C2. The committee then
	// rejects Wave citing C2 twice, once at a revision, beside spec and
	// horizon IDs and the retired C3 and the unknown C9; and Shout citing
	// C3, C9 and C5. Sing is deferred with an entry citing C4, which is not
	// on the rejected shelf, and Whisper stays contested.
	goodbye := contestedUnit(t, r, "Say goodbye")
	mustRun(t, r.Dir, "answer", goodbye, "reject", "breaks C4 and C2")
	shelveRejected(t, repo, state, wave, "Wave", "C2@HEAD~1", "S.core.1", "H.greet.2", "C3", "C9", "C2")
	shelveRejected(t, repo, state, shout, "Shout", "C3", "C9@HEAD", "C5")
	deferred := archive.Format(archive.Record{Title: "Sing", Change: later, Shelf: unit.Deferred, Citations: []string{"C4"}, Reason: "when singing is on the horizon"})
	if _, err := repo.WriteArchive(context.Background(), archive.Path(unit.Deferred, later), []byte(deferred), "deferred: Sing"); err != nil {
		t.Fatal(err)
	}
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = tr.Archive(later, unit.Deferred, unit.Committee, "deferred citing C4")
	tr.Close()
	if err != nil {
		t.Fatal(err)
	}
	seal(t, state, whisper)
	mustRun(t, r.Dir, "unit", "reopen", whisper, "the", "spec", "is", "wrong")

	// units describes each unit's state and the moves and sessions it has
	// had, to show that listing questions moves nothing.
	units := func(changes ...string) string {
		t.Helper()
		tr, err := tracker.Open(state, tracker.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer tr.Close()
		var sb strings.Builder
		for _, change := range changes {
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
			fmt.Fprintf(&sb, "%s %s %s sessions %d\n", unit.Short(change), u.State, u.Shelf, len(sessions))
			for _, ev := range events {
				switch ev.Kind {
				case tracker.UnitMoved, tracker.SessionStarted, tracker.SessionFinished:
					fmt.Fprintf(&sb, "  %d %s\n", ev.Seq, tracker.Describe(ev))
				}
			}
		}
		return sb.String()
	}
	archived := []string{goodbye, wave, shout, later}
	before := units(archived...)
	contestedBefore := units(whisper)

	// Only C2 is cited by two rejected units. With no inbox recorded it is
	// new, and a peek marks it so too.
	c2 := "C2 new: " + q(goodbye, "Say goodbye") + "; " + q(wave, "Wave")
	wantEntries(t, "peeked questions before any inbox", inboxQuestions(mustRun(t, r.Dir, "inbox", "-peek")), c2)
	wantEntries(t, "questions at the first inbox", inboxQuestions(mustRun(t, r.Dir, "inbox")), c2)

	// The question stays listed, however often the inbox is read, but is
	// no longer new.
	c2 = "C2: " + q(goodbye, "Say goodbye") + "; " + q(wave, "Wave")
	wantEntries(t, "peeked questions after an inbox", inboxQuestions(mustRun(t, r.Dir, "inbox", "-peek")), c2)
	wantEntries(t, "questions at the next inbox", inboxQuestions(mustRun(t, r.Dir, "inbox")), c2)

	if after := units(whisper); after != contestedBefore {
		t.Errorf("reading the inbox moved the contested unit:\n%s\nwant\n%s", after, contestedBefore)
	}

	// A second rejection citing C4, archived after the last inbox, raises
	// a new question, listed after C2 in charter order.
	mustRun(t, r.Dir, "answer", whisper, "reject", "violates", "(C4)")
	archived = append(archived, whisper)
	before = units(archived...)
	c4 := "C4 new: " + q(goodbye, "Say goodbye") + "; " + q(whisper, "Whisper")
	wantEntries(t, "peeked questions after a new rejection", inboxQuestions(mustRun(t, r.Dir, "inbox", "-peek")), c2, c4)
	wantEntries(t, "questions after a new rejection", inboxQuestions(mustRun(t, r.Dir, "inbox")), c2, c4)

	// A question gaining a unit since the last inbox is new again, and the
	// last recorded inbox survives a rebuild of the tracker.
	third := openUnit(t, r.Dir, "Bellow")
	shelveRejected(t, repo, state, third, "Bellow", "C2", "C2@HEAD")
	mustRun(t, r.Dir, "tracker", "rebuild")
	c2new := "C2 new: " + q(goodbye, "Say goodbye") + "; " + q(wave, "Wave") + "; " + q(third, "Bellow")
	c4 = "C4: " + q(goodbye, "Say goodbye") + "; " + q(whisper, "Whisper")
	wantEntries(t, "questions after a rebuild", inboxQuestions(mustRun(t, r.Dir, "inbox")), c2new, c4)

	// Listing questions moved no unit and started no session.
	if after := units(archived...); after != before {
		t.Errorf("reading the inbox moved units or sessions:\n%s\nwant\n%s", after, before)
	}
}
