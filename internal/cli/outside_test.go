package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
)

// rawCommit writes file (unless it is empty), stages everything and commits
// it directly with git, as an owner commit shed never saw. An empty author
// commits as the fixed test identity testrepo.Repo.Git uses.
func rawCommit(t *testing.T, r *testrepo.Repo, file, content, author, message string) string {
	t.Helper()
	if file != "" {
		r.Write(file, content)
	}
	r.Git("add", "-A")
	args := []string{"commit", "-q", "--allow-empty", "-m", message}
	if author != "" {
		args = append(args, "--author="+author)
	}
	r.Git(args...)
	return strings.TrimSpace(r.Git("rev-parse", "HEAD"))
}

// checkoutMain attaches the owner's git checkout to main, as it was before a
// landing detached it, so a further rawCommit lands on top of main.
func checkoutMain(t *testing.T, r *testrepo.Repo) {
	t.Helper()
	r.Git("checkout", "-q", "main")
}

// landUnit opens a unit, gives it one file, seals it directly through the
// tracker and lands it, returning its change ID and the commit it landed as.
func landUnit(t *testing.T, r *testrepo.Repo, state, title, file, content string) (change, commit string) {
	t.Helper()
	change = openUnit(t, r.Dir, title)
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	seal(t, state, change)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
	}
	mustRun(t, r.Dir, "land", change)
	return change, strings.TrimSpace(r.Git("rev-parse", "main"))
}

// writeSince sets outside.since in shed.toml.
func writeSince(r *testrepo.Repo, since string) {
	r.Write("shed.toml", fmt.Sprintf("[outside]\nsince = %q\n", since))
}

//shed:proves S.outside.1
func TestOutsideListsCommitsNotLandedAsUnits(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	since := strings.TrimSpace(r.Git("rev-parse", "main"))

	// An owner commit, not touching shed at all.
	b := rawCommit(t, r, "greeting.txt", "hello\n", "Jane Doe <jane@example.com>", "Add greeting\n\nExplain the greeting.")

	// A unit shed actually lands: excluded from the list.
	l1, _ := landUnit(t, r, state, "Say hi", "hello.txt", "hi\n")
	checkoutMain(t, r)

	// A commit whose trailer names l1, which is landed, but with its real
	// landing commit, not this one: does not count as landed (S.outside.1).
	d := rawCommit(t, r, "fake.txt", "fake\n", "", "Claim a landing\n\nUnit: "+l1+"\n")

	// A commit whose trailer names no unit the tracker knows.
	const unknown = "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	e := rawCommit(t, r, "unknown.txt", "unknown\n", "", "Name an unknown unit\n\nUnit: "+unknown+"\n")

	// A commit whose trailer names a real unit that has not landed.
	u2 := openUnit(t, r.Dir, "Never lands")
	f := rawCommit(t, r, "notyet.txt", "soon\n", "", "Name an unlanded unit\n\nUnit: "+u2+"\n")

	// Another unit shed lands: also excluded.
	landUnit(t, r, state, "Say bye", "bye.txt", "bye\n")

	writeSince(r, since[:8])

	line := func(commit, author, subject string) string { return commit + " " + author + " " + subject }
	want := []string{
		line(b, "Jane Doe", "Add greeting"),
		line(d, "shed", "Claim a landing"),
		line(e, "shed", "Name an unknown unit"),
		line(f, "shed", "Name an unlanded unit"),
	}
	got := collapsed(mustRun(t, r.Dir, "outside"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outside =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

//shed:proves S.outside.1
func TestOutsidePrintsNothingWhenNothingIsOutside(t *testing.T) {
	t.Run("since names main's own tip", func(t *testing.T) {
		r := testrepo.Colocated(t)
		since := strings.TrimSpace(r.Git("rev-parse", "main"))
		writeSince(r, since)
		if out := mustRun(t, r.Dir, "outside"); out != "" {
			t.Errorf("outside with since at main's tip = %q, want nothing", out)
		}
	})
	t.Run("every commit after since landed as a unit", func(t *testing.T) {
		r := testrepo.Colocated(t)
		state := filepath.Join(r.Dir, DefaultStateDir)
		since := strings.TrimSpace(r.Git("rev-parse", "main"))
		landUnit(t, r, state, "Say hi", "hi.txt", "hi\n")
		writeSince(r, since)
		if out := mustRun(t, r.Dir, "outside"); out != "" {
			t.Errorf("outside once everything landed = %q, want nothing", out)
		}
	})
}

//shed:proves S.outside.2
func TestOutsideListsCharterChangesSeparately(t *testing.T) {
	r := testrepo.Colocated(t)
	since := strings.TrimSpace(r.Git("rev-parse", "main"))

	b := rawCommit(t, r, "note.txt", "note\n", "", "A normal change")

	withC3 := testrepo.Charter + "- **C3** The tool is also honest.\n"
	r.Write("charter.md", withC3)
	c1 := rawCommit(t, r, "", "", "", "Amend the charter")

	// Changes charter.md and another file: not a charter change, so it
	// stays in the regular list (S.outside.2).
	withC4 := withC3 + "- **C4** The tool is also kind.\n"
	r.Write("charter.md", withC4)
	mixed := rawCommit(t, r, "also.txt", "also\n", "", "Amend the charter and add a file")

	c2 := rawCommit(t, r, "charter.md", withC4+"- **C5** The tool is also brave.\n", "", "Amend the charter again")

	writeSince(r, since)
	line := func(commit, subject string) string { return commit + " shed " + subject }
	want := []string{
		line(b, "A normal change"),
		line(mixed, "Amend the charter and add a file"),
		"charter changes:",
		line(c1, "Amend the charter"),
		line(c2, "Amend the charter again"),
	}
	got := collapsed(mustRun(t, r.Dir, "outside"))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("outside =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

//shed:proves S.outside.2
func TestOutsidePrintsNoCharterHeaderWithoutACharterChange(t *testing.T) {
	r := testrepo.Colocated(t)
	since := strings.TrimSpace(r.Git("rev-parse", "main"))
	rawCommit(t, r, "note.txt", "note\n", "", "A normal change")
	writeSince(r, since)
	if out := mustRun(t, r.Dir, "outside"); strings.Contains(out, "charter changes:") {
		t.Errorf("outside with no charter change printed a header:\n%s", out)
	}
}

//shed:proves S.outside.3
func TestOutsideFails(t *testing.T) {
	t.Run("shed.toml is missing", func(t *testing.T) {
		r := testrepo.Colocated(t)
		stdout, stderr, code := run(t, r.Dir, "outside")
		if code != Failed || stderr == "" || stdout != "" {
			t.Fatalf("outside without shed.toml = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("shed.toml sets no outside.since", func(t *testing.T) {
		r := testrepo.Colocated(t)
		r.Write("shed.toml", "[proofs]\nrunner = [\"true\"]\n")
		stdout, stderr, code := run(t, r.Dir, "outside")
		if code != Failed || stderr == "" || stdout != "" {
			t.Fatalf("outside without outside.since = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("the value names no commit", func(t *testing.T) {
		r := testrepo.Colocated(t)
		r.Write("shed.toml", "[outside]\nsince = \"not-a-commit\"\n")
		stdout, stderr, code := run(t, r.Dir, "outside")
		if code != Failed || stderr == "" || stdout != "" {
			t.Fatalf("outside with a since naming no commit = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("the commit is not on main's first-parent history", func(t *testing.T) {
		r := testrepo.Colocated(t)
		r.Git("branch", "side", "main")
		r.Git("checkout", "-q", "side")
		r.Write("side.txt", "side\n")
		r.Git("add", "-A")
		r.Git("commit", "-q", "-m", "a side commit")
		off := strings.TrimSpace(r.Git("rev-parse", "HEAD"))
		r.Git("checkout", "-q", "main")
		writeSince(r, off)
		stdout, stderr, code := run(t, r.Dir, "outside")
		if code != Failed || stderr == "" || stdout != "" {
			t.Fatalf("outside with a since off main = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})

	t.Run("main cannot be brought in", func(t *testing.T) {
		r := testrepo.Minimal(t)
		stdout, stderr, code := run(t, r.Dir, "outside")
		if code != Failed || stderr == "" || stdout != "" {
			t.Fatalf("outside with no repository = %d, stdout %q, stderr %q", code, stdout, stderr)
		}
	})
}

//shed:proves S.outside.3
func TestOutsideLeavesMainTrackerAndRemoteUnchanged(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	since := strings.TrimSpace(r.Git("rev-parse", "main"))
	landUnit(t, r, state, "Say hi", "hi.txt", "hi\n")
	checkoutMain(t, r)
	rawCommit(t, r, "note.txt", "note\n", "", "A plain commit")
	writeSince(r, since)

	beforeMain := r.Git("rev-parse", "main")
	beforeRemote := r.GitRemote("rev-parse", "main")
	logPath := filepath.Join(state, tracker.LogFile)
	beforeLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	mustRun(t, r.Dir, "outside")

	if got := r.Git("rev-parse", "main"); got != beforeMain {
		t.Errorf("outside moved main from %s to %s", beforeMain, got)
	}
	if got := r.GitRemote("rev-parse", "main"); got != beforeRemote {
		t.Errorf("outside moved the remote's main from %s to %s", beforeRemote, got)
	}
	afterLog, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeLog, afterLog) {
		t.Errorf("outside recorded something in the tracker:\nbefore %s\nafter %s", beforeLog, afterLog)
	}
}
