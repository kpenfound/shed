package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kpenfound/shed/internal/testrepo"
	"github.com/kpenfound/shed/internal/unit"
)

// landHorizon lands a unit whose change writes horizon.md as given, and
// returns its change ID.
func landHorizon(t *testing.T, r *testrepo.Repo, state, title, horizon string) string {
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
	mustRun(t, r.Dir, "land", change)
	return change
}

// TestAnswerAgreeDisagree checks that `shed answer <unit> agree <reason>`
// and `shed answer <unit> disagree <reason>` answer a sampled amendment
// (S.owner.11): each is recorded in the event log with the owner as actor,
// the given reason, the unit, and whether the owner agreed, without moving
// the unit or making a commit. It also checks the refusals: a landed unit
// whose landing did not record it as sampled, a unit that has not landed,
// an empty reason, and a unit that already has an agree or disagree
// answer, including after a tracker rebuild. Because agree and disagree
// answer a landed (so never contested) unit, this also proves that
// S.owner.6's contested-only check does not apply to them.
//
//shed:proves S.owner.20 S.owner.6
func TestAnswerAgreeDisagree(t *testing.T) {
	r := testrepo.Colocated(t)
	state := filepath.Join(r.Dir, DefaultStateDir)
	r.Write(".shed/config.toml", "[vcs]\nremote = \"origin\"\n\n[owner]\nsample_every = 1\n")

	sing := landHorizon(t, r, state, "Sing", `# Horizon

- **H.greet.4** (eventual) The tool sings.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	hum := landHorizon(t, r, state, "Hum", `# Horizon

- **H.greet.4** (eventual) The tool hums.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	// Realise only marks H.greet.2 realised: under S.owner.2 that is not a
	// change to the clause, so the landing amends no horizon clause
	// (S.owner.11) and is never sampled.
	notSampled := landHorizon(t, r, state, "Realise", `# Horizon

- **H.greet.4** (eventual) The tool hums.
- **H.greet.1** (soon, realised) The tool says hello.
- **H.greet.2** (soon, realised) The tool says goodbye.
- **H.greet.3** (distant) The tool greets in any language, within C2.

## Milestones

- **M1** Greetings. H.greet.1 to H.greet.2.
`)
	notLanded := openUnit(t, r.Dir, "Whisper")

	mainBefore := r.GitRemote("rev-parse", "main")

	before := logLines(t, state)
	if _, stderr, code := run(t, r.Dir, "answer", sing, "agree", "singing", "fits", "the", "charter"); code != OK {
		t.Fatalf("answer agree = %d, %q", code, stderr)
	}
	if _, stderr, code := run(t, r.Dir, "answer", hum, "disagree", "humming", "was", "not", "asked", "for"); code != OK {
		t.Fatalf("answer disagree = %d, %q", code, stderr)
	}
	after := logLines(t, state)
	if len(after) != len(before)+2 {
		t.Fatalf("an agree and a disagree recorded %d events, want 2", len(after)-len(before))
	}
	agreeEv, disagreeEv := after[len(after)-2], after[len(after)-1]
	if agreeEv["actor"] != string(unit.Owner) || agreeEv["reason"] != "singing fits the charter" || !holds(agreeEv, sing) {
		t.Errorf("the agree event = %v", agreeEv)
	}
	if agreeEv["agreed"] != true {
		t.Errorf("the agree event does not record the owner's agreement: %v", agreeEv)
	}
	if disagreeEv["actor"] != string(unit.Owner) || disagreeEv["reason"] != "humming was not asked for" || !holds(disagreeEv, hum) {
		t.Errorf("the disagree event = %v", disagreeEv)
	}
	if disagreeEv["agreed"] == true {
		t.Errorf("the disagree event records the owner as agreeing: %v", disagreeEv)
	}

	// Neither answer moved its unit or made a commit.
	for _, change := range []string{sing, hum} {
		if u := unitNow(t, r, change); u.State != unit.Landed {
			t.Errorf("unit %s is %s after an answer, want landed", unit.Short(change), u.State)
		}
	}
	if got := r.GitRemote("rev-parse", "main"); got != mainBefore {
		t.Errorf("an agree or disagree moved main from %s to %s", mainBefore, got)
	}

	refused := func(what string, args ...string) {
		t.Helper()
		before := logLines(t, state)
		if _, stderr, code := run(t, r.Dir, args...); code == OK || stderr == "" {
			t.Errorf("%s: %q = %d, %q; want it refused", what, args, code, stderr)
		}
		if after := logLines(t, state); len(after) != len(before) {
			t.Errorf("%s: a refused answer recorded events:\n%v", what, after[len(before):])
		}
	}
	refused("a landed unit not sampled, agree", "answer", notSampled, "agree", "fine", "as", "it", "is")
	refused("a landed unit not sampled, disagree", "answer", notSampled, "disagree", "fine", "as", "it", "is")
	refused("a unit that has not landed, agree", "answer", notLanded, "agree", "go", "ahead")
	refused("a unit that has not landed, disagree", "answer", notLanded, "disagree", "go", "ahead")
	refused("an empty reason, agree", "answer", sing, "agree")
	refused("a blank reason, disagree", "answer", hum, "disagree", " ")
	refused("a unit that already has an answer, agree again", "answer", sing, "agree", "again")
	refused("a unit that already has an answer, disagree instead", "answer", sing, "disagree", "changed", "my", "mind")
	refused("a unit that already has an answer, disagree again", "answer", hum, "disagree", "again")
	refused("a unit that already has an answer, agree instead", "answer", hum, "agree", "changed", "my", "mind")

	mustRun(t, r.Dir, "tracker", "rebuild")
	refused("a unit answered before a rebuild, agree", "answer", sing, "agree", "still", "no")
	refused("a unit answered before a rebuild, disagree", "answer", hum, "disagree", "still", "no")
}
