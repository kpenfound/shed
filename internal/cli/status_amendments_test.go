package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
)

// amendmentsLine returns the last line of shed status's output, trimmed:
// the horizon-amendments line S.owner.21 adds after the painter line of
// S.serve.9.
func amendmentsLine(t *testing.T, dir string) string {
	t.Helper()
	out := strings.TrimRight(mustRun(t, dir, "status"), "\n")
	lines := strings.Split(out, "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// TestStatusCountsHorizonAmendments checks that shed status prints a line
// counting horizon amendments after the painter line: the sampling count of
// S.owner.11 as auto-accepted, the number of landings recorded as sampled,
// and of those how many the owner agreed with, disagreed with and has not
// answered (S.owner.20). It also checks that the line prints with every
// count zero before any horizon amendment lands, and that shed tracker
// rebuild gives back the same counts.
//
//shed:proves S.owner.21
func TestStatusCountsHorizonAmendments(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[owner]\nsample_every = 2\n")

	want := "amendments: 0 auto-accepted, 0 sampled (0 agreed, 0 disagreed, 0 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line with nothing landed = %q, want %q", got, want)
	}

	out := mustRun(t, r.Dir, "status")
	painterAt := strings.Index(out, "painter:")
	amendAt := strings.Index(out, "amendments:")
	if painterAt < 0 || amendAt < 0 || amendAt < painterAt {
		t.Fatalf("status =\n%s\nwant the amendments line after the painter line", out)
	}

	// The first auto-accepted amendment is not sampled, since
	// owner.sample_every = 2 samples every second one.
	landHorizon(t, r, state, "Sing", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	want = "amendments: 1 auto-accepted, 0 sampled (0 agreed, 0 disagreed, 0 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after one amendment = %q, want %q", got, want)
	}

	// The second is sampled, and counts unanswered until the owner answers
	// it.
	b := landHorizon(t, r, state, "Hum", `# Horizon

- **H.greet.4** (eventual) The tool hums.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	want = "amendments: 2 auto-accepted, 1 sampled (0 agreed, 0 disagreed, 1 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after the sampled amendment = %q, want %q", got, want)
	}

	mustRun(t, r.Dir, "answer", b, "agree", "humming", "fits", "the", "charter")
	want = "amendments: 2 auto-accepted, 1 sampled (1 agreed, 0 disagreed, 0 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after an agree = %q, want %q", got, want)
	}

	// The third amendment is not sampled again.
	landHorizon(t, r, state, "Whistle", `# Horizon

- **H.greet.4** (eventual) The tool whistles.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	want = "amendments: 3 auto-accepted, 1 sampled (1 agreed, 0 disagreed, 0 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after a third, unsampled amendment = %q, want %q", got, want)
	}

	// The fourth is sampled again, and the owner disagrees this time.
	d := landHorizon(t, r, state, "Yodel", `# Horizon

- **H.greet.4** (eventual) The tool yodels.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	want = "amendments: 4 auto-accepted, 2 sampled (1 agreed, 0 disagreed, 1 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after the second sampled amendment = %q, want %q", got, want)
	}

	mustRun(t, r.Dir, "answer", d, "disagree", "yodeling", "was", "not", "asked", "for")
	want = "amendments: 4 auto-accepted, 2 sampled (1 agreed, 1 disagreed, 0 unanswered)"
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after a disagree = %q, want %q", got, want)
	}

	// shed tracker rebuild gives back the same counts.
	mustRun(t, r.Dir, "tracker", "rebuild")
	if got := amendmentsLine(t, r.Dir); got != want {
		t.Errorf("amendments line after a rebuild = %q, want %q", got, want)
	}
}
