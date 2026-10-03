package factory

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

// resolveFirst matches the instruction to resolve stored conflicts against
// the sealed spec before any other work.
var resolveFirst = regexp.MustCompile(`(?i)resolve[^\n]*against the sealed spec[^\n]*before any other work`)

//shed:proves S.vcs.14
func TestBundlesFlagStoredConflicts(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	mechanic(t, fake)
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result {
		write(t, turn.Dir, "notes.md", "Notes from unit "+turn.Unit+".\n")
		write(t, turn.Dir, "guide.md", "A guide from unit "+turn.Unit+".\n")
		return done("done")
	})
	var reviews []session.Turn
	fake.on(unit.Committee, "review", func(turn session.Turn) session.Result {
		reviews = append(reviews, turn)
		return done("pass")
	})

	a := sealed(t, f, fake)
	b := sealed(t, f, fake)
	must2(t, f.Implement)(a)
	must2(t, f.Verify)(a)
	must2(t, f.Implement)(b)
	// b reaches queued clean, before any conflict exists: S.vcs.12 fails
	// verification outright on a stored conflict, before any reviewer runs,
	// so a reviewer's bundle is never built for a conflicted change.
	must2(t, f.Verify)(b)
	if out, err := f.Land(ctx, a); err != nil || out != Landed {
		t.Fatalf("land a = %s, %v", out, err)
	}
	for _, turn := range append(fake.ran(unit.Mechanic), reviews...) {
		if strings.Contains(strings.ToLower(turn.Bundle), "conflict") {
			t.Errorf("a bundle for a unit without stored conflicts mentions them:\n%s", turn.Bundle)
		}
	}

	// Rebasing b, now queued, onto main stores conflicts in both files.
	conflicted, err := f.Repo.Rebase(ctx, b)
	must(t, err)
	if !conflicted {
		t.Fatal("b does not conflict with main")
	}
	listed := unit.Short(b) + " guide.md notes.md"

	// The wheelbuilder's work is kept: it gets the list and the instruction.
	var wheel string
	fake.on(unit.Wheelbuilder, "resolve", func(turn session.Turn) session.Result {
		wheel = turn.Bundle
		write(t, turn.Dir, "notes.md", "Notes from both units.\n")
		write(t, turn.Dir, "guide.md", "A guide from both units.\n")
		return done("resolved")
	})
	if out, err := f.Land(ctx, b); err != nil || out != Landed {
		t.Fatalf("land b = %s, %v", out, err)
	}
	if !strings.Contains(wheel, listed) || !resolveFirst.MatchString(wheel) {
		t.Errorf("wheelbuilder's bundle should list %q and say to resolve them first:\n%s", listed, wheel)
	}
}

//shed:proves S.vcs.14
func TestPainterBundlesFlagProposedConflicts(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n[shed]\nbounce_threshold = 10\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })

	hola, err := f.Repo.NewUnit(ctx, "Hola")
	must(t, err)
	must(t, f.Tracker.OpenUnit(hola, "Hola", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, hola)
	must(t, err)
	write(t, dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola.", 1))
	write(t, dir, "greet.go", "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n")
	must(t, f.Declare(ctx, hola, "", nil, []string{"H.greet.1"}, unit.Painter, 100))
	landOther(t, f, "Newline", map[string]string{
		"spec/core.md": strings.Replace(testrepo.Spec, "prints hello.", "prints hello and a newline.", 1),
		"greet.go":     "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n",
	})

	// The seal attempt rebases the proposal onto the new main and bounces on
	// the conflict under spec/, storing both it and the one in greet.go.
	if out, err := f.Debate(ctx, hola); err != nil || out != Bounced {
		t.Fatalf("debate of a proposal that conflicts under spec/ and elsewhere = %s, %v", out, err)
	}

	// The painter's next session, whose work is kept, sees both conflicted
	// files. The unit has no sealed spec, so it is told to resolve spec/core.md
	// by keeping main's text and re-applying the proposal's own change, and
	// greet.go against the proposal's own spec, not a sealed one.
	var bundle string
	fake.on(unit.Painter, "", func(turn session.Turn) session.Result {
		bundle = turn.Bundle
		write(t, turn.Dir, "spec/core.md", strings.Replace(testrepo.Spec, "prints hello.", "prints hola and a newline.", 1))
		write(t, turn.Dir, "greet.go", "package greet\n\n// Hello greets kindly and warmly.\nfunc Hello() string { return \"hello\" }\n")
		return done("replied")
	})
	if out, err := f.Debate(ctx, hola); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(hola)
		t.Fatalf("debate once the painter resolved both conflicts = %s, %v: %q", out, err, u.Reason)
	}

	for _, want := range []string{"spec/core.md", "greet.go"} {
		if !strings.Contains(bundle, want) {
			t.Errorf("the painter's bundle does not name %q:\n%s", want, bundle)
		}
	}
	if resolveFirst.MatchString(bundle) {
		t.Errorf("a proposed unit has no sealed spec, so its painter's bundle should not point to one:\n%s", bundle)
	}
	low := strings.ToLower(bundle)
	if !strings.Contains(low, "before any other work") {
		t.Errorf("the painter's bundle does not say to resolve conflicts before any other work:\n%s", bundle)
	}
	if !strings.Contains(low, "main's text") || !strings.Contains(low, "re-appl") {
		t.Errorf("the painter's bundle does not say to keep main's text under spec/ and re-apply the proposal's own spec changes:\n%s", bundle)
	}
	if !strings.Contains(low, "proposed spec") {
		t.Errorf("the painter's bundle does not say to resolve greet.go against the unit's proposed spec:\n%s", bundle)
	}
}

//shed:proves S.vcs.14
func TestCommitteeBundlesListConflictsWithoutInstruction(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\ncommittee = 1\n[shed]\nbounce_threshold = 10\n")

	// A conflict outside spec/ does not stop a proposal's debate, so a
	// committee member's bundle is built while the change still holds it.
	change, err := f.Repo.NewUnit(ctx, "Kindly")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Kindly", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	write(t, dir, "greet.go", "package greet\n\n// Hello greets kindly.\nfunc Hello() string { return \"hello\" }\n")
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.3"}, unit.Painter, 100))
	landOther(t, f, "Warmly", map[string]string{
		"greet.go": "package greet\n\n// Hello greets warmly.\nfunc Hello() string { return \"hello\" }\n"})

	// A proposed unit's change keeps any conflict with main stored in its
	// files (S.vcs.10), whatever landing's sweep later does: rebase it
	// directly so the committee's round starts with the conflict already
	// stored, as a sweep between landings would leave it.
	conflicted, err := f.Repo.Rebase(ctx, change)
	must(t, err)
	if !conflicted {
		t.Fatal("the proposal does not conflict with main")
	}

	var bundle string
	var writable bool
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		bundle, writable = turn.Bundle, turn.Writable
		return done("clean")
	})
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("debate of a proposal that conflicts outside spec/ = %s, %v: %q", out, err, u.Reason)
	}

	if writable {
		t.Error("a committee member's session should not be writable: its work on the unit's files is thrown away")
	}
	if !strings.Contains(bundle, "greet.go") {
		t.Errorf("the committee's bundle does not name greet.go:\n%s", bundle)
	}
	if low := strings.ToLower(bundle); resolveFirst.MatchString(bundle) || strings.Contains(low, "before any other work") {
		t.Errorf("a thrown-away session should get the list without the resolve-first instruction:\n%s", bundle)
	}
}

// sectionBody returns the body of rendered's section named title, or "" if
// rendered has no such section.
func sectionBody(rendered, title string) string {
	marker := "\n## " + title + "\n\n"
	i := strings.Index(rendered, marker)
	if i < 0 {
		return ""
	}
	rest := rendered[i+len(marker):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

//shed:proves S.vcs.14 S.vcs.12 S.vcs.13
func TestMechanicBundleNamesEachConflictOnce(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "[concurrency]\nin_flight = 0\n")
	fake.on(unit.Committee, "debate", func(session.Turn) session.Result { return done("clean") })
	landOther(t, f, "Docs", map[string]string{"docs/greet.md": "Hello greets.\n"})

	// Sealing rebases the unit onto a main whose docs clash with its own,
	// storing a genuine conflict jj itself holds, outside spec/.
	change, err := f.Repo.NewUnit(ctx, "Wave")
	must(t, err)
	must(t, f.Tracker.OpenUnit(change, "Wave", unit.Painter))
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "spec/wave.md", "# Wave\n\n- **S.wave.1** (H.greet.3) Running the tool with --wave waves.\n")
	write(t, dir, "docs/greet.md", "Hello greets kindly.\n")
	must(t, f.Declare(ctx, change, "", nil, []string{"H.greet.3"}, unit.Painter, 100))
	landOther(t, f, "Warmly", map[string]string{"docs/greet.md": "Hello greets warmly.\n"})
	if out, err := f.Debate(ctx, change); err != nil || out != Sealed {
		u, _ := f.Tracker.Unit(change)
		t.Fatalf("debate = %s, %v: %q", out, err, u.Reason)
	}

	// Before any capture, jj itself stores the conflict, so `shed conflicts`
	// (S.vcs.13), which reads the change in jj alone, lists it too.
	stored, err := f.Repo.Conflicts(ctx, change)
	must(t, err)
	if !slices.Contains(stored, "docs/greet.md") {
		t.Errorf("shed conflicts should list docs/greet.md while jj stores it as conflicted, got %v", stored)
	}

	// The first mechanic session sees the conflict jj itself stores. Its
	// bundle names docs/greet.md once, in the "Stored conflicts" section
	// S.vcs.14 defines, not a second time in some other section: for a
	// mechanic this is the bundle S.vcs.10 requires, given once.
	var first string
	fake.on(unit.Mechanic, "proofs", func(turn session.Turn) session.Result {
		first = turn.Bundle
		write(t, turn.Dir, "wave_test.go", waveProof)
		// unfence strips the file's closing marker, so jj resolves it on
		// capture, but the opening and separator marker lines survive as
		// plain content: the file still holds an unresolved conflict under
		// S.vcs.12's test, though jj no longer stores one.
		unfence(t, filepath.Join(turn.Dir, "docs", "greet.md"))
		return done("done")
	})
	var second string
	fake.on(unit.Mechanic, "implement", func(turn session.Turn) session.Result {
		second = turn.Bundle
		return done("done")
	})
	fake.on(unit.Mechanic, "docs", func(turn session.Turn) session.Result { return done("done") })
	fake.on(unit.Committee, "review", func(session.Turn) session.Result { return done("pass") })

	must2(t, f.Implement)(change)

	if n := strings.Count(first, "docs/greet.md"); n != 1 {
		t.Errorf("the first mechanic's bundle names docs/greet.md %d times, want once:\n%s", n, first)
	}
	if !strings.Contains(sectionBody(first, bundle.ConflictsSection), "docs/greet.md") {
		t.Errorf("the first mechanic's bundle has no %q section naming docs/greet.md:\n%s", bundle.ConflictsSection, first)
	}

	// The second mechanic session runs after capture turned the conflict
	// marker-only: jj no longer stores it, but S.vcs.12 still counts it, so
	// the bundle must still flag it, once, in the same section.
	if n := strings.Count(second, "docs/greet.md"); n != 1 {
		t.Errorf("the second mechanic's bundle names docs/greet.md %d times, want once:\n%s", n, second)
	}
	if !strings.Contains(sectionBody(second, bundle.ConflictsSection), "docs/greet.md") {
		t.Errorf("the second mechanic's bundle does not name the marker-only conflict left in docs/greet.md in its %q section, though S.vcs.12 still counts it:\n%s", bundle.ConflictsSection, second)
	}

	// Capture turned the conflict marker-only: jj no longer stores it, so
	// `shed conflicts` (S.vcs.13) excludes it, even though the bundle (S.vcs.14)
	// and verification (S.vcs.12) still count it.
	after, err := f.Repo.Conflicts(ctx, change)
	must(t, err)
	if slices.Contains(after, "docs/greet.md") {
		t.Errorf("shed conflicts should not list docs/greet.md once only its markers remain (S.vcs.12), got %v", after)
	}
}
