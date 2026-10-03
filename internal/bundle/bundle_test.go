package bundle

import (
	"context"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

//shed:proves S.sess.5 S.sess.7
func TestFilesBundle(t *testing.T) {
	mainRepo := testrepo.Minimal(t)
	head := testrepo.Minimal(t)
	head.Write("spec/core.md", "# Core\n\n- **S.core.1** (H.greet.1) Running the tool prints hello, loudly.\n- **S.core.2** (H.greet.2) Running the tool with --bye prints goodbye.\n")
	mainSet, _ := docs.Load(revision.Worktree(mainRepo.Dir))
	headSet, _ := docs.Load(revision.Worktree(head.Dir))
	u := tracker.Unit{Change: "qpvuntsmwlqtqpvuntsmwlqt", Title: "Say goodbye", State: unit.Implementing, Bounces: 1,
		Seal:      &tracker.Seal{Main: "abc123"},
		Footprint: tracker.Footprint{Modifies: []string{"S.core.1", "S.core.2"}, Advances: []string{"H.greet.2"}}}
	req := Request{
		Role: unit.Mechanic, Unit: u, Main: mainSet, Head: headSet,
		Proofs:  []proof.Proof{{Dir: "greet", Test: "TestBye", File: "greet/bye_test.go", Clauses: []clause.ID{clause.MustParseID("S.core.2")}}},
		Debate:  "Round 1: no objections.",
		Notices: []tracker.Notice{{Kind: "seal-moved", Body: "The seal moved to def456."}},
		Extra:   []Section{{Title: "Step", Body: "implement"}},
	}
	b, err := Files{}.Bundle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, s := range b.Sections {
		titles = append(titles, s.Title)
	}
	want := []string{UnitSection, CharterSection, FootprintSection, ChangesSection, SealedSection, HorizonSection, ProofsSection, DebateSection, NoticesSection, "Step"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("sections = %v, want %v", titles, want)
	}
	out := b.Render()
	for _, s := range []string{
		"- Change: qpvuntsmwlqtqpvuntsmwlqt",
		"- Sealed against main commit abc123",
		"- C2 It never shouts.",
		"- Modifies: S.core.1, S.core.2",
		"- Added S.core.2 (H.greet.2) Running the tool with --bye prints goodbye.",
		"- Changed S.core.1 (H.greet.1) Running the tool prints hello, loudly.\n  Was: (H.greet.1) Running the tool prints hello.",
		"- H.greet.2 (soon) The tool says goodbye.",
		"- S.core.1: no proof yet",
		"- S.core.2: greet.TestBye (greet/bye_test.go)",
		"Round 1: no objections.",
		"- seal-moved: The seal moved to def456.",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("bundle lacks %q:\n%s", s, out)
		}
	}

	req.Role = unit.Sweeper
	b, _ = Files{}.Bundle(context.Background(), req)
	if b.Has(DebateSection) {
		t.Error("the sweeper's bundle carries the debate record")
	}
}

//shed:proves S.impl.7
func TestBundleShowsTheEstimate(t *testing.T) {
	mainRepo := testrepo.Minimal(t)
	mainSet, _ := docs.Load(revision.Worktree(mainRepo.Dir))
	u := tracker.Unit{Change: "qpvuntsmwlqtqpvuntsmwlqt", Title: "Say goodbye", State: unit.Proposed,
		Footprint: tracker.Footprint{Advances: []string{"H.greet.2"}, Estimate: 1500}}
	for _, role := range []unit.Actor{unit.Committee, unit.Painter} {
		req := Request{Role: role, Unit: u, Main: mainSet, Head: mainSet}
		b, err := Files{}.Bundle(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.Render(), "- Estimate: $1500.00") {
			t.Errorf("%s's bundle lacks the estimate:\n%s", role, b.Render())
		}
	}

	u.Footprint.Estimate = 0
	b, err := Files{}.Bundle(context.Background(), Request{Role: unit.Committee, Unit: u, Main: mainSet, Head: mainSet})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.Render(), "Estimate") {
		t.Errorf("a unit with no recorded estimate shows one:\n%s", b.Render())
	}
}
