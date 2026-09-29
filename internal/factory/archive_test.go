package factory

import (
	"context"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/unit"
)

// archiveHorizon is main's horizon for the archive tests. H.greet.7 sits
// apart from the clauses a proposal rewords, so that main can change it
// after the proposal branches without the two edits touching.
const archiveHorizon = `# Horizon

- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.
- **H.greet.5** (distant) The tool greets   politely.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.

## Later

- **H.greet.7** (distant) The tool greets at night.
`

// proposedHorizon rewords H.greet.1, retiers H.greet.2, removes H.greet.3,
// rewraps H.greet.5 and adds H.greet.4.
const proposedHorizon = `# Horizon

- **H.greet.1** (soon, realised) The tool says hello to everyone.
- **H.greet.2** (near) The tool says goodbye.
- **H.greet.5** (distant) The tool greets
  politely.
- **H.greet.4** (soon) The tool says farewell.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.

## Later

- **H.greet.7** (distant) The tool greets at night.
`

// vetoAll has member 2 veto every proposal on the charter.
func vetoAll(t *testing.T, fake *fakeRunner) {
	t.Helper()
	fake.on(unit.Committee, "debate", func(turn session.Turn) session.Result {
		if member(turn) == 2 {
			_, err := call(t, turn, "object", map[string]any{"kind": "charter", "citations": []string{"C2"}, "text": "Goodbye is shouted."})
			must(t, err)
			return done("objecting")
		}
		return done("clean")
	})
}

// horizonSection returns the entry's horizon changes section, or "" when it
// has none.
func horizonSection(text string) string {
	_, rest, ok := strings.Cut(text, "\n## Horizon changes\n")
	if !ok {
		return ""
	}
	if i := strings.Index(rest, "\n## "); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

//shed:proves S.shed.15
func TestArchiveListsHorizonChanges(t *testing.T) {
	r := projectWith(t, map[string]string{"horizon.md": archiveHorizon})
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := propose(t, f)
	dir, err := f.Repo.Workspace(ctx, change)
	must(t, err)
	write(t, dir, "horizon.md", proposedHorizon)

	// Main moves on after the proposal branches and retiers H.greet.7. That
	// is main's change, not the proposal's.
	other, err := f.Repo.NewUnit(ctx, "Night")
	must(t, err)
	odir, err := f.Repo.Workspace(ctx, other)
	must(t, err)
	write(t, odir, "horizon.md", strings.Replace(archiveHorizon, "(distant) The tool greets at night.", "(soon) The tool greets at night.", 1))
	_, err = f.Repo.Land(ctx, other, func(context.Context, string, string) (string, error) { return "Night", nil })
	must(t, err)

	vetoAll(t, fake)
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Rejected {
		t.Fatalf("debate = %s", out)
	}
	entries, err := archive.Read(r.Dir)
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("archive = %+v", entries)
	}
	text := entries[0].Text
	for _, want := range []string{"Added S.core.2", "Goodbye is shouted."} {
		if !strings.Contains(text, want) {
			t.Errorf("entry lacks its spec changes or debate, %q:\n%s", want, text)
		}
	}
	section := horizonSection(text)
	if section == "" {
		t.Fatalf("entry has no horizon changes:\n%s", text)
	}

	// Each listed clause is on its own item, in document order.
	items := map[string]string{}
	var order []string
	for _, item := range strings.Split(section, "\n- ") {
		for _, id := range []string{"H.greet.1", "H.greet.2", "H.greet.3", "H.greet.4", "H.greet.5", "H.greet.7"} {
			if strings.Contains(item, id) {
				items[id] = item
				order = append(order, id)
			}
		}
	}
	if strings.Join(order, " ") != "H.greet.1 H.greet.2 H.greet.3 H.greet.4" {
		t.Errorf("horizon changes list %v, want H.greet.1 to H.greet.4 in document order:\n%s", order, section)
	}
	cases := []struct {
		id, change string
		wants      []string
	}{
		{"H.greet.1", "changed", []string{"soon, realised", "The tool says hello.", "The tool says hello to everyone."}},
		{"H.greet.2", "changed", []string{"soon", "near", "The tool says goodbye."}},
		{"H.greet.3", "removed", []string{"distant", "The tool greets in any language, within C2."}},
		{"H.greet.4", "added", []string{"soon", "The tool says farewell."}},
	}
	for _, c := range cases {
		item := items[c.id]
		if !strings.Contains(strings.ToLower(item), c.change) {
			t.Errorf("%s is not listed as %s: %q", c.id, c.change, item)
		}
		for _, want := range c.wants {
			if !strings.Contains(item, want) {
				t.Errorf("%s's item lacks %q: %q", c.id, want, item)
			}
		}
	}
}

//shed:proves S.shed.15
func TestArchiveListsNoHorizonChanges(t *testing.T) {
	r := project(t)
	fake := newFake(t)
	f := open(t, r, fake, "")
	change := propose(t, f)
	vetoAll(t, fake)
	out, err := f.Debate(ctx, change)
	must(t, err)
	if out != Rejected {
		t.Fatalf("debate = %s", out)
	}
	entries, err := archive.Read(r.Dir)
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("archive = %+v", entries)
	}
	if strings.Contains(entries[0].Text, "## Horizon changes") {
		t.Errorf("an entry whose proposal changes no horizon clause lists horizon changes:\n%s", entries[0].Text)
	}
}
