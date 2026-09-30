package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

//shed:proves S.vcs.13
func TestConflictsListsConflictedUnits(t *testing.T) {
	r := testrepo.Colocated(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	state := filepath.Join(r.Dir, DefaultStateDir)
	writeIn := func(change string, files map[string]string) {
		t.Helper()
		dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
		for name, content := range files {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	first := openUnit(t, r.Dir, "First")
	writeIn(first, map[string]string{"notes.md": "first\n", "guide.md": "first\n", "sub/deep.md": "first\n"})
	b := openUnit(t, r.Dir, "Second")
	writeIn(b, map[string]string{"sub/deep.md": "b\n", "notes.md": "b\n", "guide.md": "b\n"})
	clean := openUnit(t, r.Dir, "Clean")
	writeIn(clean, map[string]string{"other.md": "clean\n"})
	shelved := openUnit(t, r.Dir, "Shelved")
	writeIn(shelved, map[string]string{"guide.md": "shelved\n"})
	c := openUnit(t, r.Dir, "Third")
	writeIn(c, map[string]string{"notes.md": "c\n"})

	if out := mustRun(t, r.Dir, "conflicts"); out != "" {
		t.Errorf("conflicts with none = %q", out)
	}

	seal(t, state, first)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", first, s, "by hand")
	}
	mustRun(t, r.Dir, "land", first)

	// Rebasing onto the new main stores conflicts in the units that touch
	// the same files.
	repo, err := vcs.Open(context.Background(), r.Dir, state, vcs.Options{Main: "main", Remote: "origin",
		Landing: vcs.Identity{Name: "shed wheelbuilder", Email: "wheelbuilder@shed.localhost"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{b, clean, shelved, c} {
		if _, err := repo.Rebase(context.Background(), change); err != nil {
			t.Fatalf("rebase %s: %v", unit.Short(change), err)
		}
	}
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Archive(shelved, unit.Rejected, unit.Committee, "not wanted"); err != nil {
		t.Fatal(err)
	}
	tr.Close()

	want := unit.Short(b) + " guide.md notes.md sub/deep.md\n" +
		unit.Short(c) + " notes.md\n"
	stdout, stderr, code := run(t, r.Dir, "conflicts")
	if code != OK || stdout != want {
		t.Errorf("conflicts = %d, %q, %q; want %q", code, stdout, stderr, want)
	}
}
