package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

const (
	// frameHorizon is main's horizon in a framing repository: a distant and
	// an eventual clause to frame, a realised distant clause, and a clause
	// that already refines H.greet.3.
	frameHorizon = `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.4** (eventual) The tool sings.
- **H.greet.6** (distant, realised) The tool waves.
- **H.greet.7** (soon, refines H.greet.3) The tool greets in French.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`
	frameSpec = `# Core

- **S.core.1** (H.greet.1) Running the tool prints hello.
- **S.core.2** (H.greet.3) Running the tool with --lang es prints hola.
- **S.core.3** (H.greet.6) Running the tool with --wave prints a wave.
`
)

// framedHorizon is frameHorizon with clauses added after its last one.
func framedHorizon(clauses ...string) string {
	return strings.Replace(frameHorizon, "\n\n## Milestones", "\n"+strings.Join(clauses, "\n")+"\n\n## Milestones", 1)
}

// frameRepo is a colocated repository whose main holds frameHorizon and
// frameSpec. An earlier commit on main held H.greet.5, so its ID is
// retired.
func frameRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	testrepo.RequireJJ(t)
	r := testrepo.Minimal(t)
	r.Write(".gitignore", ".shed/\n")
	r.Write("spec/core.md", frameSpec)
	r.Write("horizon.md", framedHorizon("- **H.greet.5** (near) The tool whispers."))
	r.Init()
	r.Commit("whisper")
	r.Write("horizon.md", frameHorizon)
	r.Commit("no whispering")
	r.Remote = t.TempDir()
	r.GitRemoteInit()
	r.Git("remote", "add", "origin", r.Remote)
	r.Git("push", "-q", "origin", "main")
	r.JJ("git", "init", "--colocate")
	return r
}

// framer plays the frame builder with its frame function and a painter
// that finds nothing to propose. It keeps every turn it runs.
type framer struct {
	t     *testing.T
	mu    sync.Mutex
	turns []session.Turn
	frame func(turn session.Turn) session.Result
}

func (f *framer) Run(_ context.Context, turn session.Turn) (session.Result, error) {
	f.mu.Lock()
	f.turns = append(f.turns, turn)
	f.mu.Unlock()
	switch turn.Role {
	case unit.FrameBuilder:
		return f.frame(turn), nil
	case unit.Painter:
		return session.Result{Status: "nothing", CostUSD: 0.1}, nil
	}
	f.t.Errorf("%s ran step %q", turn.Role, turn.Step)
	return session.Result{}, nil
}

// ran returns the turns of a role.
func (f *framer) ran(role unit.Actor) []session.Turn {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []session.Turn
	for _, t := range f.turns {
		if t.Role == role {
			out = append(out, t)
		}
	}
	return out
}

// writing is a frame builder that writes files into its copy and reports
// an outcome.
func writing(t *testing.T, status string, files map[string]string) func(session.Turn) session.Result {
	return func(turn session.Turn) session.Result {
		for name, content := range files {
			path := filepath.Join(turn.Dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return session.Result{Status: status, CostUSD: 0.1}
	}
}

func runFramer(t *testing.T, dir string, f *framer, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := RunWith(context.Background(), append([]string{"-C", dir}, args...), &out, &errOut, f)
	return out.String(), errOut.String(), code
}

func allUnits(t *testing.T, r *testrepo.Repo) []tracker.Unit {
	t.Helper()
	tr, err := tracker.Open(filepath.Join(r.Dir, DefaultStateDir), tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	units, err := tr.Units()
	if err != nil {
		t.Fatal(err)
	}
	return units
}

//shed:proves S.frame.1
func TestFrameRefusesClausesItMayNotFrame(t *testing.T) {
	r := frameRepo(t)
	f := &framer{t: t, frame: writing(t, "nothing", nil)}
	for _, id := range []string{"H.greet.2", "H.greet.1", "H.greet.7", "H.greet.6", "H.greet.5", "H.greet.9", "S.core.2", "C1"} {
		if out, errOut, code := runFramer(t, r.Dir, f, "frame", id); code == OK {
			t.Errorf("frame %s = %d, %q, %q", id, code, out, errOut)
		}
	}
	if _, _, code := runFramer(t, r.Dir, f, "frame"); code == OK {
		t.Error("frame with no clause succeeded")
	}
	if len(f.turns) != 0 {
		t.Errorf("a refused clause started %d sessions", len(f.turns))
	}
	if units := allUnits(t, r); len(units) != 0 {
		t.Errorf("a refused clause opened units: %+v", units)
	}
}

//shed:proves S.frame.1
func TestFrameSessionBundle(t *testing.T) {
	r := frameRepo(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	flight := openUnit(t, r.Dir, "Say goodbye")
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", flight))
	if err := os.WriteFile(filepath.Join(dir, "spec", "core.md"), []byte(frameSpec+"- **S.core.4** (H.greet.2) Running the tool with --bye prints goodbye.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, r.Dir, "unit", "declare", "-depends", "S.core.1", "-advances", "H.greet.2", flight)

	var copies []string
	f := &framer{t: t, frame: func(turn session.Turn) session.Result {
		data, err := os.ReadFile(filepath.Join(turn.Dir, "horizon.md"))
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, string(data))
		// The copy is writable.
		if err := os.WriteFile(filepath.Join(turn.Dir, "horizon.md"), []byte(framedHorizon("- **H.greet.8** (near, refines H.greet.3) The tool greets in German.")), 0o644); err != nil {
			t.Fatal(err)
		}
		return session.Result{Status: "nothing", CostUSD: 0.1}
	}}
	for _, id := range []string{"H.greet.3", "H.greet.4"} {
		if out, errOut, code := runFramer(t, r.Dir, f, "frame", id); code != OK {
			t.Fatalf("frame %s = %d, %q, %q", id, code, out, errOut)
		}
	}
	turns := f.ran(unit.FrameBuilder)
	if len(turns) != 2 {
		t.Fatalf("frame ran %d frame builder sessions, want one each", len(turns))
	}
	for i, turn := range turns {
		if !turn.Writable {
			t.Errorf("session %d works in a read-only copy", i)
		}
		if copies[i] != frameHorizon {
			t.Errorf("session %d's copy is not main's:\n%s", i, copies[i])
		}
		if strings.Join(turn.Outcomes, " ") != "framed nothing" {
			t.Errorf("session %d may report %v", i, turn.Outcomes)
		}
	}
	b := turns[0].Bundle
	for _, want := range []string{
		"C1 The tool greets people.", // the charter
		"H.greet.2",                  // the horizon
		"H.greet.3",                  // the named clause
		"The tool greets in French.", // a clause that refines it
		"S.core.2",                   // a spec clause that advances it
	} {
		if !strings.Contains(b, want) {
			t.Errorf("the bundle lacks %q:\n%s", want, b)
		}
	}
	// The unit in flight appears with its whole footprint: the clauses it
	// modifies, the clauses it depends on and the clauses it advances.
	var line string
	for _, l := range strings.Split(b, "\n") {
		if strings.Contains(l, unit.Short(flight)) {
			line = l
			break
		}
	}
	if line == "" {
		t.Errorf("the bundle lacks the unit in flight %s:\n%s", unit.Short(flight), b)
	}
	for _, want := range []string{"S.core.4", "S.core.1", "H.greet.2"} {
		if !strings.Contains(line, want) {
			t.Errorf("the unit in flight's footprint lacks %s: %q", want, line)
		}
	}
	if !strings.Contains(turns[1].Bundle, "The tool sings.") {
		t.Errorf("the bundle lacks the named eventual clause:\n%s", turns[1].Bundle)
	}
}

//shed:proves S.frame.2
func TestFrameChecksTheSessionsCopy(t *testing.T) {
	good := "- **H.greet.8** (near, refines H.greet.3) The tool greets in German."
	cases := []struct {
		name  string
		files map[string]string
		// named are the changes the refusal must name.
		named []string
	}{
		{"another file", map[string]string{"horizon.md": framedHorizon(good), "README.md": "greetings\n"},
			[]string{"README.md"}},
		{"another document", map[string]string{"horizon.md": framedHorizon(good),
			"spec/core.md": frameSpec + "- **S.core.4** (H.greet.3) Running the tool with --lang de prints hallo.\n"},
			[]string{"spec/core.md"}},
		{"a changed clause", map[string]string{"horizon.md": strings.Replace(framedHorizon(good), "(soon) The tool says goodbye.", "(near) The tool says goodbye.", 1)},
			[]string{"H.greet.2"}},
		{"a removed clause", map[string]string{"horizon.md": strings.Replace(framedHorizon(good), "- **H.greet.7** (soon, refines H.greet.3) The tool greets in French.\n", "", 1)},
			[]string{"H.greet.7"}},
		{"a changed milestone", map[string]string{"horizon.md": strings.Replace(framedHorizon(good), "H.greet.1 to H.greet.2.", "H.greet.1 to H.greet.8.", 1)},
			[]string{"M1"}},
		{"an added milestone", map[string]string{"horizon.md": framedHorizon(good) + "- **M2** Languages. H.greet.8.\n"},
			[]string{"M2"}},
		{"an ID added twice", map[string]string{"horizon.md": framedHorizon(good,
			"- **H.greet.8** (soon, refines H.greet.3) The tool greets in Dutch.")},
			[]string{"H.greet.8"}},
		{"no clause added", nil, nil},
		{"bad clauses", map[string]string{"horizon.md": framedHorizon(
			"- **H.greet.8** (distant) The tool greets in German.",
			"- **H.greet.9** (soon, realised, refines H.greet.3) The tool greets in Italian.",
			"- **H.greet.10** (near) The tool greets in Dutch.",
			"- **H.greet.11** (soon, refines H.greet.4) The tool greets in Welsh.",
			"- **H.greet.5** (near, refines H.greet.3) The tool greets in Greek.",
		)}, []string{"H.greet.8", "H.greet.9", "H.greet.10", "H.greet.11", "H.greet.5"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := frameRepo(t)
			f := &framer{t: t, frame: writing(t, "framed", c.files)}
			out, errOut, _ := runFramer(t, r.Dir, f, "frame", "H.greet.3")
			said := out + errOut
			for _, want := range c.named {
				if !strings.Contains(said, want) {
					t.Errorf("frame does not name %s:\n%s", want, said)
				}
			}
			if units := allUnits(t, r); len(units) != 0 {
				t.Errorf("a failed check opened units: %+v", units)
			}
			if h := r.Git("show", "main:horizon.md"); h+"\n" != frameHorizon {
				t.Errorf("main's horizon changed:\n%s", h)
			}
		})
	}
}

//shed:proves S.frame.2
func TestFrameWithNoOutcomeKeepsNothing(t *testing.T) {
	good := map[string]string{"horizon.md": framedHorizon("- **H.greet.8** (near, refines H.greet.3) The tool greets in German.")}
	for status, want := range map[string]string{"nothing": "nothing", "": "outcome"} {
		r := frameRepo(t)
		f := &framer{t: t, frame: writing(t, status, good)}
		out, errOut, _ := runFramer(t, r.Dir, f, "frame", "H.greet.3")
		if said := out + errOut; !strings.Contains(said, want) {
			t.Errorf("a session reporting %q: frame said %q", status, said)
		}
		if units := allUnits(t, r); len(units) != 0 {
			t.Errorf("a session reporting %q opened units: %+v", status, units)
		}
	}
}

//shed:proves S.frame.3
func TestFrameOpensADraftForTheOwner(t *testing.T) {
	r := frameRepo(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
	framed := framedHorizon(
		"- **H.greet.9** (soon, refines H.greet.3) The tool greets in Italian.",
		"- **H.greet.8** (near, refines H.greet.3) The tool greets in German.",
	)
	f := &framer{t: t, frame: writing(t, "framed", map[string]string{"horizon.md": framed})}
	out, errOut, code := runFramer(t, r.Dir, f, "frame", "H.greet.3")
	if code != OK {
		t.Fatalf("frame = %d, %q, %q", code, out, errOut)
	}
	units := allUnits(t, r)
	if len(units) != 1 {
		t.Fatalf("frame opened %d units", len(units))
	}
	u := units[0]
	if u.State != unit.Proposed || u.OpenedBy != unit.FrameBuilder || !strings.Contains(u.Title, "H.greet.3") ||
		len(u.Footprint.Advances) != 0 || len(u.Footprint.Modifies) != 0 {
		t.Errorf("unit = %+v", u)
	}

	// It prints the unit and each added clause with its tier, in document
	// order.
	if !strings.Contains(out, unit.Short(u.Change)) {
		t.Errorf("frame does not print the unit:\n%s", out)
	}
	line := func(id string) (int, string) {
		for i, l := range strings.Split(out, "\n") {
			if strings.Contains(l, id) {
				return i, l
			}
		}
		t.Errorf("frame does not print %s:\n%s", id, out)
		return -1, ""
	}
	i9, l9 := line("H.greet.9")
	i8, l8 := line("H.greet.8")
	if !strings.Contains(l9, "soon") || !strings.Contains(l8, "near") || i9 > i8 {
		t.Errorf("frame printed:\n%s", out)
	}

	// The change sits on main and holds the session's horizon alone.
	if names := r.JJ("diff", "--name-only", "-r", u.Change); names != "horizon.md" {
		t.Errorf("the change touches %q", names)
	}
	if parent := r.JJ("log", "--no-graph", "-r", u.Change+"-", "-T", "commit_id"); parent != r.Git("rev-parse", "main") {
		t.Errorf("the change sits on %s, not main", parent)
	}
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", u.Change))
	if data, err := os.ReadFile(filepath.Join(dir, "horizon.md")); err != nil || string(data) != framed {
		t.Errorf("the unit's horizon = %q, %v", data, err)
	}

	// It modifies no spec clause, so it cannot be declared.
	if _, _, code := run(t, r.Dir, "unit", "declare", "-advances", "H.greet.2", u.Change); code == OK {
		t.Error("the frame unit was declared")
	}

	// Serving neither debates it, archives it nor counts it as a proposal
	// the painter waits on, and never calls it a draft to declare.
	for range 2 {
		out, errOut, code := runFramer(t, r.Dir, f, "serve", "-once")
		if code != OK {
			t.Fatalf("serve = %d, %q, %q", code, out, errOut)
		}
		if strings.Contains(out, unit.Short(u.Change)+" is a draft") {
			t.Errorf("serve calls the frame unit a draft to declare:\n%s", out)
		}
	}
	if n := len(f.ran(unit.Painter)); n != 1 {
		t.Errorf("the painter ran %d times with the frame unit proposed, want once", n)
	}
	if now := unitNow(t, r, u.Change); now.State != unit.Proposed || now.Round != 0 {
		t.Errorf("after serving the frame unit is %+v", now)
	}

	// Discarding refuses a unit the owner opened, then archives the frame
	// unit as deferred with no archive entry, and refuses it once archived.
	owned := openUnit(t, r.Dir, "Wave")
	if _, _, code := runFramer(t, r.Dir, f, "frame", "-discard", owned); code == OK {
		t.Error("frame -discard took a unit the owner opened")
	}
	if now := unitNow(t, r, owned); now.State != unit.Proposed {
		t.Errorf("the owner's unit is %s", now.State)
	}
	if out, errOut, code := runFramer(t, r.Dir, f, "frame", "-discard", unit.Short(u.Change)); code != OK {
		t.Fatalf("frame -discard = %d, %q, %q", code, out, errOut)
	}
	if now := unitNow(t, r, u.Change); now.State != unit.Archived || now.Shelf != unit.Deferred {
		t.Errorf("after discarding the frame unit is %+v", now)
	}
	if entries, _ := archive.Read(r.Dir); len(entries) != 0 {
		t.Errorf("discarding wrote archive entries: %+v", entries)
	}
	if _, _, code := run(t, r.Dir, "unit", "path", u.Change); code == OK {
		t.Error("the discarded unit's change was kept")
	}
	if _, _, code := runFramer(t, r.Dir, f, "frame", "-discard", u.Change); code == OK {
		t.Error("frame -discard took an archived unit")
	}
}

// landByHand lands a unit the owner opens, writing files, and returns main's
// commit after it lands.
func landByHand(t *testing.T, r *testrepo.Repo, title string, files map[string]string) string {
	t.Helper()
	change := openUnit(t, r.Dir, title)
	dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seal(t, filepath.Join(r.Dir, DefaultStateDir), change)
	for _, s := range []string{"implementing", "verifying", "queued"} {
		mustRun(t, r.Dir, "unit", "move", change, s, "by hand")
	}
	mustRun(t, r.Dir, "land", change)
	return r.GitRemote("rev-parse", "main")
}

// commitOf returns the commit a unit's change is at.
func commitOf(r *testrepo.Repo, change string) string {
	return r.JJ("log", "--no-graph", "-r", change, "-T", "commit_id")
}

//shed:proves S.frame.4 S.unit.3 S.owner.11
func TestFrameAcceptLandsTheFraming(t *testing.T) {
	r := frameRepo(t)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[owner]\nsample_every = 1\n")
	state := filepath.Join(r.Dir, DefaultStateDir)
	framed := framedHorizon(
		"- **H.greet.9** (soon, refines H.greet.3) The tool greets in Italian.",
		"- **H.greet.8** (near, refines H.greet.3) The tool greets in German.",
	)
	f := &framer{t: t, frame: writing(t, "framed", map[string]string{"horizon.md": framed})}
	if out, errOut, code := runFramer(t, r.Dir, f, "frame", "H.greet.3"); code != OK {
		t.Fatalf("frame = %d, %q, %q", code, out, errOut)
	}
	units := allUnits(t, r)
	if len(units) != 1 {
		t.Fatalf("frame opened %d units", len(units))
	}
	u := units[0]

	// Other units in flight, in the order they open: a proposal the owner
	// opened and a sealed unit whose recorded horizon clause the framing
	// refines.
	owned := openUnit(t, r.Dir, "Nod")
	put := func(change, name, content string) {
		t.Helper()
		dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", change))
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(owned, "nod.txt", "nod\n")
	past := openUnit(t, r.Dir, "Greet in Spanish")
	put(past, "hola.txt", "hola\n")
	tr, err := tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = tr.Seal(past, "main1", "unitcommit", tracker.Footprint{Advances: []string{"H.greet.3"}}, unit.Committee, "consensus", nil)
	tr.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Accept refuses a unit shed frame did not open.
	if _, _, code := runFramer(t, r.Dir, f, "frame", "-accept", owned); code == OK {
		t.Error("frame -accept took a unit the owner opened")
	}
	if now := unitNow(t, r, owned); now.State != unit.Proposed {
		t.Errorf("the owner's unit is %s", now.State)
	}
	if _, _, code := runFramer(t, r.Dir, f, "frame", "-accept"); code == OK {
		t.Error("frame -accept with no unit succeeded")
	}

	out, errOut, code := runFramer(t, r.Dir, f, "frame", "-accept", unit.Short(u.Change))
	if code != OK {
		t.Fatalf("frame -accept = %d, %q, %q", code, out, errOut)
	}
	commit := r.GitRemote("rev-parse", "main")
	if h := r.GitRemote("show", "main:horizon.md"); h+"\n" != framed {
		t.Errorf("main's horizon after the accept:\n%s", h)
	}
	if parent := r.GitRemote("rev-parse", "main^"); parent == commit {
		t.Error("main did not move")
	}

	// The unit moved from proposed to landed, by the owner, with its commit.
	now := unitNow(t, r, u.Change)
	if now.State != unit.Landed || now.Landed != commit {
		t.Errorf("after the accept the frame unit is %+v, want landed as %s", now, commit)
	}
	move := lastMove(t, r, u.Change)
	if move.From != unit.Proposed || move.To != unit.Landed || move.Actor != unit.Owner ||
		!strings.Contains(move.Reason, "accept") || move.Commit != commit {
		t.Errorf("the landing move = %+v", move)
	}

	// It is a horizon amendment, never sampled even when every amendment
	// is.
	if move.HorizonAmendment == nil || !*move.HorizonAmendment || move.Sampled {
		t.Errorf("the landing records horizon amendment %v, sampled %v", move.HorizonAmendment, move.Sampled)
	}

	// Its actual footprint holds no spec clause and no dependency, and
	// advances the clause it framed.
	if a := now.Actual; a == nil || len(a.Modifies) != 0 || len(a.Depends) != 0 || strings.Join(a.Advances, " ") != "H.greet.3" {
		t.Errorf("the actual footprint = %+v", now.Actual)
	}

	// The commit message holds the title, each added clause with its tier
	// in document order, and the unit's change ID, and no seal.
	msg := r.GitRemote("log", "-1", "--format=%B", "main")
	i9, i8 := -1, -1
	for i, l := range strings.Split(msg, "\n") {
		if strings.Contains(l, "H.greet.9") && strings.Contains(l, "soon") {
			i9 = i
		}
		if strings.Contains(l, "H.greet.8") && strings.Contains(l, "near") {
			i8 = i
		}
	}
	if !strings.Contains(msg, u.Title) || i9 < 0 || i8 < 0 || i9 > i8 ||
		!strings.Contains(msg, "Unit: "+u.Change) || strings.Contains(msg, "Sealed-Against:") {
		t.Errorf("the commit message is:\n%s", msg)
	}

	// It prints the landed commit, then each other unit's sweep outcome in
	// the order they opened.
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	at := func(prefix string) int {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		t.Errorf("frame -accept does not print %q:\n%s", prefix, out)
		return -1
	}
	ic := -1
	for i, l := range lines {
		if strings.Contains(l, commit) {
			ic = i
			break
		}
	}
	io := at(unit.Short(owned) + " proposed: rebased cleanly")
	ip := at(unit.Short(past) + " sealed: rebased cleanly")
	if ic < 0 || ic > io || io > ip {
		t.Errorf("frame -accept printed:\n%s", out)
	}
	for change, outcome := range map[string]string{owned: "rebased cleanly", past: "rebased cleanly"} {
		logged := sweepLog(t, r.Dir, change, u.Change)
		if len(logged) != 1 || !strings.HasSuffix(logged[0], "landed: "+outcome) {
			t.Errorf("unit %s logs the sweep as %q, want %q", unit.Short(change), logged, outcome)
		}
	}

	// The sealed unit learns that its clause gained refining clauses, and
	// keeps its state.
	tr, err = tracker.Open(state, tracker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := tr.Events(past)
	if err != nil {
		tr.Close()
		t.Fatal(err)
	}
	var body string
	for _, e := range events {
		if e.Kind == tracker.NoticeAdded && e.Notice != nil {
			body += e.Notice.Body
		}
	}
	for _, want := range []string{unit.Short(u.Change), "gained", "H.greet.9", "H.greet.8"} {
		if !strings.Contains(body, want) {
			t.Errorf("the sealed unit's notices lack %q:\n%s", want, body)
		}
	}
	if p, err := tr.Unit(past); err != nil || p.State != unit.Sealed {
		t.Errorf("the sealed unit is %s, %v", p.State, err)
	}
	tr.Close()

	// A landed framing is not accepted again.
	if _, _, code := runFramer(t, r.Dir, f, "frame", "-accept", u.Change); code == OK {
		t.Error("frame -accept took a landed unit")
	}
	if got := r.GitRemote("rev-parse", "main"); got != commit {
		t.Errorf("a refused accept moved main to %s", got)
	}
}

//shed:proves S.frame.4
func TestFrameAcceptRefusesAFramingThatNoLongerFits(t *testing.T) {
	framed := framedHorizon(
		"- **H.greet.9** (soon, refines H.greet.3) The tool greets in Italian.",
		"- **H.greet.8** (near, refines H.greet.3) The tool greets in German.",
	)
	cases := []struct {
		name string
		// main is main's horizon when the owner accepts.
		main string
		// named are what the refusal must name.
		named []string
	}{
		{"a conflict", framedHorizon("- **H.greet.10** (near, refines H.greet.3) The tool greets in Welsh."),
			[]string{"horizon.md"}},
		{"an ID main now holds", strings.Replace(frameHorizon, "# Horizon\n\n", "# Horizon\n\n- **H.greet.8** (soon) The tool greets loudly.\n", 1),
			[]string{"H.greet.8"}},
		{"a clause shed frame no longer takes", strings.Replace(frameHorizon, "(distant) The tool greets in any language", "(distant, realised) The tool greets in any language", 1),
			[]string{"H.greet.3"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := frameRepo(t)
			r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n")
			f := &framer{t: t, frame: writing(t, "framed", map[string]string{"horizon.md": framed})}
			if out, errOut, code := runFramer(t, r.Dir, f, "frame", "H.greet.3"); code != OK {
				t.Fatalf("frame = %d, %q, %q", code, out, errOut)
			}
			u := allUnits(t, r)[0]
			main := landByHand(t, r, "Move main", map[string]string{"horizon.md": c.main})

			before := commitOf(r, u.Change)
			dir := strings.TrimSpace(mustRun(t, r.Dir, "unit", "path", u.Change))
			held, err := os.ReadFile(filepath.Join(dir, "horizon.md"))
			if err != nil {
				t.Fatal(err)
			}

			out, errOut, code := runFramer(t, r.Dir, f, "frame", "-accept", u.Change)
			if code == OK {
				t.Fatalf("frame -accept succeeded: %q", out)
			}
			said := out + errOut
			for _, want := range c.named {
				if !strings.Contains(said, want) {
					t.Errorf("frame -accept does not name %s:\n%s", want, said)
				}
			}

			// Nothing lands, and the unit stays proposed with its change and
			// workspace as they were.
			if got := r.GitRemote("rev-parse", "main"); got != main {
				t.Errorf("a refused accept moved main from %s to %s", main, got)
			}
			if now := unitNow(t, r, u.Change); now.State != unit.Proposed || now.Landed != "" {
				t.Errorf("after a refused accept the frame unit is %+v", now)
			}
			if after := commitOf(r, u.Change); after != before {
				t.Errorf("a refused accept moved the change from %s to %s", before, after)
			}
			if now, err := os.ReadFile(filepath.Join(dir, "horizon.md")); err != nil || string(now) != string(held) {
				t.Errorf("a refused accept changed the workspace's horizon to %q, %v", now, err)
			}
		})
	}
}

//shed:proves S.owner.14
func TestInboxListsPendingFramingsUntilResolved(t *testing.T) {
	r := frameRepo(t)
	f := &framer{t: t, frame: writing(t, "framed", map[string]string{"horizon.md": framedHorizon("- **H.greet.8** (soon, refines H.greet.3) The tool greets in French.")})}
	if out, errOut, code := runFramer(t, r.Dir, f, "frame", "H.greet.3"); code != OK {
		t.Fatalf("frame: %s %s", out, errOut)
	}
	units := allUnits(t, r)
	if len(units) != 1 {
		t.Fatalf("units = %+v", units)
	}
	id := unit.Short(units[0].Change)
	for _, args := range [][]string{{"inbox", "-peek"}, {"inbox"}, {"inbox"}} {
		out, stderr, code := run(t, r.Dir, args...)
		if code != OK {
			t.Fatalf("inbox: %s %s", out, stderr)
		}
		for _, want := range []string{"Framing " + id, units[0].Title, "shed unit path " + id, "shed frame -accept " + id, "shed frame -discard " + id} {
			if !strings.Contains(out, want) {
				t.Errorf("inbox lacks %q: %s", want, out)
			}
		}
	}
	if out, errOut, code := runFramer(t, r.Dir, f, "frame", "-discard", id); code != OK {
		t.Fatalf("discard: %s %s", out, errOut)
	}
	out, stderr, code := run(t, r.Dir, "inbox", "-peek")
	if code != OK || strings.Contains(out, "Framing "+id) {
		t.Fatalf("inbox after discard: %s %s", out, stderr)
	}
}
