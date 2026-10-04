package factory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/roles"
	"github.com/kpenfound/shed/internal/session"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Outcome is where a stage left a unit.
type Outcome string

const (
	Sealed    Outcome = "sealed"
	Rejected  Outcome = "rejected"
	Deferred  Outcome = "deferred"
	Bounced   Outcome = "bounced"
	Contested Outcome = "contested"
	// Waiting is a unit that reached consensus but may not be sealed yet,
	// because the cap on units in flight is reached.
	Waiting     Outcome = "waiting"
	Implemented Outcome = "implemented"
	Stepped     Outcome = "stepped"
	Reopened    Outcome = "reopened"
	Verified    Outcome = "verified"
	Failed      Outcome = "failed"
	Landed      Outcome = "landed"
	Consistent  Outcome = "consistent"
	Discarded   Outcome = "discarded"
)

// Committee outcomes in the shed.
const (
	outcomeClean     = "clean"
	outcomeObjecting = "objecting"
	outcomeReplied   = "replied"
)

// Declare records the dependencies and horizon clauses a proposal declares,
// with the clauses its spec diff modifies, and optionally retitles it. An
// estimate given replaces the unit's recorded estimate and must be a
// positive number of USD; omitted, the unit's recorded estimate is
// preserved. The painter's declare refuses a declaration that leaves the
// unit with no estimate, naming the missing field (S.impl.6).
func (f *Factory) Declare(ctx context.Context, change, title string, depends, advances []string, actor unit.Actor, estimate ...float64) error {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return err
	}
	if u.OpenedBy == unit.FrameBuilder {
		return fmt.Errorf("unit %s records a framing: its change modifies no spec clause, so it cannot be declared", unit.Short(u.Change))
	}
	est := u.Footprint.Estimate
	if len(estimate) > 0 {
		if estimate[0] <= 0 {
			return fmt.Errorf("unit %s: the estimate must be a positive number of USD, not %v", unit.Short(u.Change), estimate[0])
		}
		est = estimate[0]
	}
	if actor == unit.Painter && est <= 0 {
		return fmt.Errorf("unit %s: the painter's declare leaves the unit with no estimate", unit.Short(u.Change))
	}
	if title != "" && title != u.Title {
		if err := f.Tracker.Retitle(u.Change, title, actor); err != nil {
			return err
		}
	}
	main, err := f.mainSet(ctx)
	if err != nil {
		return err
	}
	head, err := f.headSet(ctx, u.Change)
	if err != nil {
		return err
	}
	fp := tracker.Footprint{Modifies: modified(main, head), Depends: depends, Advances: advances, Estimate: est}
	return f.Tracker.SetFootprint(u.Change, fp, actor, "declared", onMain(main))
}

// modified lists the spec clauses a unit's spec diff adds, changes or
// removes.
func modified(main, head *docs.Set) []string {
	return docs.DiffSpec(main, head).Modified()
}

// refreshFootprint recomputes the clauses a proposal modifies from its spec
// diff, keeping its declared dependencies and horizon clauses.
func (f *Factory) refreshFootprint(ctx context.Context, u tracker.Unit) (tracker.Footprint, *docs.Set, error) {
	main, err := f.mainSet(ctx)
	if err != nil {
		return tracker.Footprint{}, nil, err
	}
	head, err := f.headSet(ctx, u.Change)
	if err != nil {
		return tracker.Footprint{}, nil, err
	}
	fp := u.Footprint
	fp.Modifies = modified(main, head)
	if len(fp.Modifies) == 0 {
		return fp, main, errNoSpecChange
	}
	if err := f.Tracker.SetFootprint(u.Change, fp, unit.Shed, "the spec diff changed", onMain(main)); err != nil {
		return fp, main, err
	}
	return fp, main, nil
}

var errNoSpecChange = errors.New("the proposal changes no spec clause")

// Debate runs a proposed unit through the shed. Committee members debate it
// in parallel, round by round, and the painter answers between rounds. A
// charter objection rejects the proposal at once. With no standing
// objection the unit is sealed. At the round cap, a proposal whose only
// standing objections say it is off the horizon is deferred, unless the
// committee split over a soon-tier horizon amendment, which waits for the
// owner; any other goes back to its painter with a bounce. In the amendment lane, objections
// standing at the cap reject the amendment instead.
func (f *Factory) Debate(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if u.State != unit.Proposed {
		return "", fmt.Errorf("unit %s is %s; only proposed units are debated", unit.Short(u.Change), u.State)
	}
	lane, err := f.amendmentOf(u.Change)
	if err != nil {
		return "", err
	}
	if why, err := f.resolve(ctx, u, lane); err != nil || why != "" {
		if err != nil {
			return "", err
		}
		return f.bounce(u, why)
	}
	if _, _, err := f.refreshFootprint(ctx, u); err != nil {
		return f.bounce(u, err.Error())
	}
	if u.Footprint.Estimate <= 0 {
		return "", fmt.Errorf("unit %s is a draft: it has no recorded estimate", unit.Short(u.Change))
	}
	events, err := f.Tracker.Events(u.Change)
	if err != nil {
		return "", err
	}
	start := u.Round
	switch pendingSeal(events) {
	case approvedSeal:
		return f.seal(ctx, u, "the owner approved its horizon amendment", lane)
	case heldSeal:
		return f.seal(ctx, u, fmt.Sprintf("no objection stands after round %d", u.Round), lane)
	case endedSeal:
		start = 0
	}
	max := lane.cap
	for round := start; ; {
		if round < max {
			if why, err := f.resolve(ctx, u, lane); err != nil || why != "" {
				if err != nil {
					return "", err
				}
				return f.bounce(u, why)
			}
			round++
			if err := f.Tracker.StartRound(u.Change, round); err != nil {
				return "", err
			}
			if u, err = f.Tracker.Unit(u.Change); err != nil {
				return "", err
			}
			if err := f.committeeRound(ctx, u, round, max, lane); err != nil {
				return "", err
			}
		}
		standing, err := f.Tracker.Standing(u.Change)
		if err != nil {
			return "", err
		}
		if veto := byKind(standing, tracker.CharterObjection); len(veto) > 0 {
			return f.archive(ctx, u, unit.Rejected, veto)
		}
		if len(standing) == 0 {
			if lane.in() {
				out, err := f.outside(ctx, lane, u.Change)
				if err != nil {
					return "", err
				}
				if len(out) > 0 {
					return f.bounce(u, "the amendment changes clauses outside its sealed scope: "+strings.Join(out, ", "))
				}
			}
			if tier, why, err := f.farTier(ctx, u.Change); err != nil || tier != "" {
				if err != nil {
					return "", err
				}
				if err := f.Tracker.ContestTier(u.Change, tier, why); err != nil {
					return "", err
				}
				return Contested, nil
			}
			out, err := f.seal(ctx, u, fmt.Sprintf("no objection stands after round %d", round), lane)
			if err == nil && out == Waiting {
				err = f.Tracker.HoldSeal(u.Change, round)
			}
			return out, err
		}
		if round >= max {
			if lane.in() {
				return f.rejectAmendment(ctx, u, lane, standing, round)
			}
			if len(byKind(standing, tracker.HorizonObjection)) == len(standing) {
				if f.split(standing) {
					d, err := f.amendmentTier(ctx, u.Change)
					if err != nil {
						return "", err
					}
					if d.Tier == "soon" {
						if err := f.Tracker.ContestSplit(u.Change, d.Tier, splitReason(d.Tier, standing, round)); err != nil {
							return "", err
						}
						return Contested, nil
					}
				}
				return f.archive(ctx, u, unit.Deferred, standing)
			}
			return f.bounce(u, fmt.Sprintf("%d objections still stand after %d rounds: %s", len(standing), round, ids(standing)))
		}
		if err := f.reply(ctx, u, round, lane); err != nil {
			return "", err
		}
		if u, err = f.Tracker.Unit(u.Change); err != nil {
			return "", err
		}
		if _, _, err := f.refreshFootprint(ctx, u); err != nil {
			return f.bounce(u, err.Error())
		}
		if u, err = f.Tracker.Unit(u.Change); err != nil {
			return "", err
		}
	}
}

// lane reports whether a unit is in the amendment lane: its latest reopen
// requested an amendment and it has not been sealed since (S.shed.11). A
// unit in the lane comes with its last seal event, which holds the sealed
// main commit and footprint.
func (f *Factory) lane(change string) (*tracker.Event, error) {
	events, err := f.Tracker.Events(change)
	if err != nil {
		return nil, err
	}
	return laneOf(events), nil
}

// laneOf is lane over a unit's events, oldest first.
func laneOf(events []tracker.Event) *tracker.Event {
	amended := false
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != tracker.UnitMoved {
			continue
		}
		if e.To == unit.Sealed {
			if amended && e.Seal != nil {
				return &e
			}
			return nil
		}
		if e.To == unit.Proposed && e.Bounce && !amended {
			if !e.Amendment {
				return nil
			}
			amended = true
		}
	}
	return nil
}

// amendment is what the amendment lane holds a debate to: its round cap and
// the scope, footprint, main commit and unit commit of the unit's last
// seal. A unit outside the lane has no scope, and its cap is
// shed.max_rounds.
type amendment struct {
	request   string
	cap       int
	scope     []string
	footprint tracker.Footprint
	main      string
	commit    string
}

func (a amendment) in() bool { return a.main != "" }

// amendmentOf is a unit's amendment lane. Its cap is shed.amendment_rounds
// (S.shed.11), and its scope is every spec clause the footprint recorded at
// the last seal modified or depended on (S.shed.12).
func (f *Factory) amendmentOf(change string) (amendment, error) {
	sealed, err := f.lane(change)
	if err != nil || sealed == nil {
		return amendment{cap: f.Operator.Shed.MaxRounds}, err
	}
	a := amendment{cap: f.Operator.Shed.AmendmentRounds, main: sealed.Seal.Main, commit: sealed.Seal.Commit}
	events, err := f.Tracker.Events(change)
	if err != nil {
		return amendment{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Amendment && events[i].Bounce {
			a.request = events[i].Reason
			break
		}
	}

	if fp := sealed.Footprint; fp != nil {
		a.footprint = *fp
		for _, id := range slices.Concat(fp.Modifies, fp.Depends) {
			if !slices.Contains(a.scope, id) {
				a.scope = append(a.scope, id)
			}
		}
	}
	slices.Sort(a.scope)
	return a, nil
}

// told is what a session in the amendment lane is told of its scope.
func (a amendment) told() string {
	if !a.in() {
		return ""
	}
	return fmt.Sprintf(" This is an amendment: its scope is the sealed spec clauses %s, and a change to any other clause bounces it.", orNone(a.scope))
}

// outside lists the spec clauses a proposal changes outside its amendment
// scope: those whose text differs from the main commit the change is based
// on, which is the sealed main commit until a landing rebases the change
// (S.shed.12).
func (f *Factory) outside(ctx context.Context, a amendment, change string) ([]string, error) {
	base, err := f.Repo.Base(ctx, change)
	if err != nil {
		return nil, err
	}
	sealed, _ := docs.Load(revision.Git{Root: f.Root, Rev: base})
	head, err := f.headSet(ctx, change)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, id := range modified(sealed, head) {
		if !slices.Contains(a.scope, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// committeeRound runs every committee member's session of a round at once.
func (f *Factory) committeeRound(ctx context.Context, u tracker.Unit, round, max int, lane amendment) error {
	overrun, hasOverrun, err := f.overrunSection(u.Change)
	if err != nil {
		return err
	}
	members := f.Operator.Concurrency.Committee
	errs := make([]error, members)
	var wg sync.WaitGroup
	for m := 1; m <= members; m++ {
		wg.Add(1)
		go func(member int) {
			defer wg.Done()
			mine, err := f.memberObjections(u.Change, member)
			if err != nil {
				errs[member-1] = err
				return
			}
			perspectives := f.Operator.Committee.Perspectives
			perspective := perspectives[(member-1)%len(perspectives)]
			profile := ""
			if profiles := f.Operator.Committee.Profiles; len(profiles) > 0 {
				profile = profiles[(member-1)%len(profiles)]
			}
			extra := []bundle.Section{{Title: "Your standing objections", Body: mine}, {Title: "Amendment request", Body: lane.request}}
			if hasOverrun {
				extra = append(extra, overrun)
			}
			res, err := f.session(ctx, work{
				Member: member, Perspective: perspective, Profile: profile,
				Unit: u, Role: unit.Committee, Prompt: roles.Committee,
				Step:     fmt.Sprintf("debate round %d, member %d", round, member),
				Task:     fmt.Sprintf("Debate the proposal %q. This is round %d of at most %d, and you are member %d.", u.Title, round, max, member) + lane.told(),
				Tools:    func(_ string, head *docs.Set) []session.Tool { return f.committeeTools(u.Change, member, head) },
				Outcomes: []string{outcomeClean, outcomeObjecting},
				Extra:    extra,
			})
			if err == nil && res.Failure != session.NoFailure {
				err = fmt.Errorf("committee member %d: %s: %s", member, res.Failure, res.Reason)
			}
			errs[member-1] = err
		}(m)
	}
	wg.Wait()
	return errors.Join(errs...)
}

func (f *Factory) memberObjections(change string, member int) (string, error) {
	standing, err := f.Tracker.Standing(change)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, o := range standing {
		if o.Member == member {
			fmt.Fprintf(&b, "- %s (%s, citing %s): %s\n", o.ID, o.Kind, strings.Join(o.Citations, ", "), o.Text)
			if o.Answer != "" {
				fmt.Fprintf(&b, "  - Answer: %s\n", o.Answer)
			}
		}
	}
	return b.String(), nil
}

// committeeTools are the tools a committee member debates with. Every
// citation must resolve against the proposal's documents.
func (f *Factory) committeeTools(change string, member int, head *docs.Set) []session.Tool {
	resolver := docs.NewResolver(f.Root, head)
	type objectIn struct {
		Kind      string   `json:"kind" jsonschema:"charter, horizon, size or spec"`
		Citations []string `json:"citations" jsonschema:"the clause IDs the objection concerns"`
		Text      string   `json:"text" jsonschema:"what is wrong and what would fix it"`
	}
	type withdrawIn struct {
		Objection string `json:"objection" jsonschema:"the ID of your objection"`
		Reason    string `json:"reason" jsonschema:"why it no longer applies"`
	}
	return []session.Tool{
		session.NewTool("object", "Raise an objection to the proposal, citing clause IDs.",
			func(_ context.Context, in objectIn) (string, error) {
				for _, c := range in.Citations {
					cit, err := clause.ParseCitation(c)
					if err != nil {
						return "", err
					}
					if _, err := resolver.Resolve(cit); err != nil {
						return "", err
					}
				}
				id, err := f.Tracker.Object(change, member, in.Kind, in.Citations, in.Text)
				if err != nil {
					return "", err
				}
				return "objection " + id + " recorded", nil
			}),
		session.NewTool("withdraw", "Withdraw one of your standing objections.",
			func(_ context.Context, in withdrawIn) (string, error) {
				if err := f.Tracker.Withdraw(in.Objection, member, in.Reason); err != nil {
					return "", err
				}
				return "objection " + in.Objection + " withdrawn", nil
			}),
	}
}

// reply runs the painter's answer to the standing objections. A declaration
// made during the reply is recorded only once the reply is captured, with
// the clauses the captured files modify (S.shed.5); an invalid one stops the
// debate and leaves the footprint as it was.
func (f *Factory) reply(ctx context.Context, u tracker.Unit, round int, lane amendment) error {
	type answerIn struct {
		Objection string `json:"objection" jsonschema:"the objection's ID"`
		Text      string `json:"text" jsonschema:"your answer, citing clause IDs"`
	}
	type declareIn struct {
		Depends  *[]string `json:"depends,omitempty" jsonschema:"Replace the spec dependencies; omit to preserve, empty array to clear."`
		Advances *[]string `json:"advances,omitempty" jsonschema:"Replace the horizon clauses advanced; omit to preserve, empty array to clear."`
	}
	overrun, hasOverrun, err := f.overrunSection(u.Change)
	if err != nil {
		return err
	}
	var extra []bundle.Section
	if hasOverrun {
		extra = append(extra, overrun)
	}
	var mu sync.Mutex
	fp := u.Footprint
	declared := false
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Painter, Prompt: roles.PainterReply, Writable: true,
		Step:  fmt.Sprintf("reply to round %d", round),
		Task:  fmt.Sprintf("Answer the committee's standing objections to %q after round %d.", u.Title, round) + lane.told(),
		Extra: extra,
		Tools: func(string, *docs.Set) []session.Tool {
			return []session.Tool{session.NewTool("answer", "Answer one standing objection.",
				func(_ context.Context, in answerIn) (string, error) {
					if err := f.Tracker.Answer(in.Objection, in.Text, unit.Painter); err != nil {
						return "", err
					}
					return "answer recorded", nil
				}), session.NewTool("declare", "Update this proposal's dependencies and horizon advances for the next round. Omitted fields are preserved.",
				func(_ context.Context, in declareIn) (string, error) {
					for _, field := range []struct {
						ids  *[]string
						kind clause.Kind
					}{{in.Depends, clause.Spec}, {in.Advances, clause.Horizon}} {
						if field.ids == nil {
							continue
						}
						for _, raw := range *field.ids {
							id, err := clause.ParseID(raw)
							if err != nil || id.Kind != field.kind {
								return "", fmt.Errorf("invalid declaration clause %q", raw)
							}
						}
					}
					mu.Lock()
					defer mu.Unlock()
					if in.Depends != nil {
						fp.Depends = slices.Clone(*in.Depends)
					}
					if in.Advances != nil {
						fp.Advances = slices.Clone(*in.Advances)
					}
					declared = true
					return "declaration staged; validated and recorded after the reply is captured", nil
				})}
		},
		Outcomes: []string{outcomeReplied},
	})
	if err != nil {
		return err
	}
	if res.Failure != session.NoFailure {
		return fmt.Errorf("the painter's reply: %s: %s", res.Failure, res.Reason)
	}
	mu.Lock()
	defer mu.Unlock()
	if declared {
		return f.Declare(ctx, u.Change, "", fp.Depends, fp.Advances, unit.Painter)
	}
	return nil
}

// resolve hands a proposal whose files under spec/ hold a conflict with
// main to its painter, whose session resolves it before any member debates
// the text (S.vcs.10). It returns why the unit bounces when a conflict under
// spec/ remains, or "" when none does.
func (f *Factory) resolve(ctx context.Context, u tracker.Unit, lane amendment) (string, error) {
	conflicts, err := f.specConflicted(ctx, u.Change)
	if err != nil || conflicts == "" {
		return "", err
	}
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Painter, Prompt: roles.PainterReply, Writable: true,
		Step:     "resolve conflicts",
		Task:     fmt.Sprintf("Files under spec/ in the proposal %q conflict with main. Resolve every conflict before the committee debates the text: %s.", u.Title, conflicts) + lane.told(),
		Outcomes: []string{outcomeReplied},
	})
	if err != nil {
		return "", err
	}
	if res.Failure != session.NoFailure {
		return "", fmt.Errorf("the painter's resolution: %s: %s", res.Failure, res.Reason)
	}
	if conflicts, err = f.specConflicted(ctx, u.Change); err != nil || conflicts == "" {
		return "", err
	}
	return "conflicts under spec/ remain after the painter's session: " + conflicts, nil
}

// A proposal's next debate seals it with no round when the owner approved
// its horizon amendment (S.shed.17) or its last round ended with no
// objection standing while the cap on units in flight held its seal back
// (S.serve.7). A painter session since then ends the approval or the hold,
// and the next debate starts afresh from round one with no bounce
// (S.shed.16).
const (
	noPendingSeal = iota
	approvedSeal
	heldSeal
	endedSeal
)

// pendingSeal reads from a unit's events, oldest first, whether its next
// debate seals it with no round. A round, a bounce or any other move since
// the approval or the held seal ends it with no pending seal; a painter
// session since then ends it afresh.
func pendingSeal(events []tracker.Event) int {
	painted := false
	pending := func(kind int) int {
		if painted {
			return endedSeal
		}
		return kind
	}
	for i := len(events) - 1; i >= 0; i-- {
		switch e := events[i]; e.Kind {
		case tracker.SessionStarted:
			if e.Session != nil && e.Session.Role == unit.Painter {
				painted = true
			}
		case tracker.Consensus:
			return pending(heldSeal)
		case tracker.RoundStarted, tracker.UnitBounced:
			return noPendingSeal
		case tracker.UnitMoved:
			if e.Approved {
				return pending(approvedSeal)
			}
			return noPendingSeal
		}
	}
	return noPendingSeal
}

// split reports whether a debate is split at its round cap: at least one
// committee member of its last round has no objection standing (S.shed.18).
func (f *Factory) split(standing []tracker.Objection) bool {
	for m := 1; m <= f.Operator.Concurrency.Committee; m++ {
		if !slices.ContainsFunc(standing, func(o tracker.Objection) bool { return o.Member == m }) {
			return true
		}
	}
	return false
}

// splitReason names the tier of a split debate's horizon amendment and each
// objection standing at its cap (S.shed.18).
func splitReason(tier string, standing []tracker.Objection, round int) string {
	lines := []string{fmt.Sprintf("the committee split over the %s-tier horizon amendment, so it waits for the owner; %d horizon objections still stand after %d rounds:", tier, len(standing), round)}
	for _, o := range standing {
		lines = append(lines, fmt.Sprintf("- %s (member %d, citing %s): %s", o.ID, o.Member, strings.Join(o.Citations, ", "), o.Text))
	}
	return strings.Join(lines, "\n")
}

// amendmentTier diffs a proposal's horizon against the latest main commit
// its change descends from, as shed diff does (S.diff.4).
func (f *Factory) amendmentTier(ctx context.Context, change string) (docs.HorizonDiff, error) {
	base, err := f.Repo.Base(ctx, change)
	if err != nil {
		return docs.HorizonDiff{}, err
	}
	from, _ := docs.Load(revision.Git{Root: f.Root, Rev: base})
	head, err := f.headSet(ctx, change)
	if err != nil {
		return docs.HorizonDiff{}, err
	}
	return docs.DiffHorizonAmendment(from, head), nil
}

// farTier takes the tier of a proposal's horizon amendment against the
// latest main commit its change descends from, as shed diff gives it
// (S.diff.4). A distant or eventual tier always waits for the owner
// (S.shed.16); while shed.horizon_owner_approval is true, a near or soon
// tier waits for the owner too (S.horizon.8, S.horizon.9). farTier returns
// the waiting tier with the reason naming it and each horizon clause
// counted at it, in document order. A clause counted at the tier only
// because of its refines tag is followed by the parents shed diff names for
// it (S.shed.19). It returns "" for any other tier.
func (f *Factory) farTier(ctx context.Context, change string) (string, string, error) {
	d, err := f.amendmentTier(ctx, change)
	if err != nil {
		return "", "", err
	}
	far := d.Tier == "distant" || d.Tier == "eventual"
	near := f.Operator.Shed.HorizonOwnerApproval && (d.Tier == "near" || d.Tier == "soon")
	if !far && !near {
		return "", "", nil
	}
	listed := map[clause.ID]docs.TieredHorizonChange{}
	for _, group := range [][]docs.TieredHorizonChange{d.Added, d.Removed, d.Changed} {
		for _, c := range group {
			listed[c.ID] = c
		}
	}
	var named []string
	for _, id := range d.CountedAt(d.Tier) {
		name := id.String()
		if c := listed[id]; c.Tier != d.Tier && len(c.Parents) > 0 {
			var parents []string
			for _, p := range c.Parents {
				parents = append(parents, fmt.Sprintf("%s (%s)", p.ID, p.Tier))
			}
			name += " parent " + strings.Join(parents, ", ")
		}
		named = append(named, name)
	}
	return d.Tier, fmt.Sprintf("the horizon amendment is %s tier, so it waits for the owner: %s", d.Tier, strings.Join(named, "; ")), nil
}

// seal seals a unit, against main as it is now, with a reason. Its change
// is first rebased onto that main commit, and a rebase that fails or
// conflicts under spec/ bounces the unit instead (S.vcs.10). A spec/
// conflict counts no bounce when the painter's latest captured session
// (S.vcs.4) found spec/ clean, since the conflict then came in by a landing
// after that capture (S.vcs.17). A seal out of the amendment lane records
// the estimate recorded at the unit's previous seal, whatever was declared
// since; when that seal recorded none, it records the unit's estimate at
// sealing (S.impl.7).
func (f *Factory) seal(ctx context.Context, u tracker.Unit, reason string, lane amendment) (Outcome, error) {
	if full, err := f.inFlightFull(u.Change); err != nil || full {
		return Waiting, err
	}
	commit, err := f.Repo.MainCommit(ctx)
	if err != nil {
		return "", err
	}
	if why, specConflict, err := f.onSeal(ctx, u.Change, commit); err != nil || why != "" {
		if err != nil {
			return "", err
		}
		if specConflict {
			saw, err := f.Tracker.PainterSawSpecConflict(u.Change)
			if err != nil {
				return "", err
			}
			if !saw {
				return f.bounceUncounted(u, why)
			}
		}
		return f.bounce(u, why)
	}
	fp, main, err := f.refreshFootprint(ctx, u)
	if err != nil {
		return f.bounce(u, err.Error())
	}
	if lane.in() && lane.footprint.Estimate > 0 {
		fp.Estimate = lane.footprint.Estimate
	}
	head, err := f.Repo.Snapshot(ctx, u.Change)
	if err != nil {
		return "", err
	}
	if err := f.Tracker.Seal(u.Change, commit, head, fp, unit.Committee, reason, onMain(main)); err != nil {
		return f.bounce(u, err.Error())
	}
	if err := f.entangle(ctx, u.Change, main); err != nil {
		return "", err
	}
	return Sealed, nil
}

// entangle reports the in-flight units a newly sealed unit is entangled
// with (S.fp.5). Shared clauses follow the spec on main, files in name
// order, then the clauses absent from main in the order of the unit's own
// spec.
func (f *Factory) entangle(ctx context.Context, change string, main *docs.Set) error {
	head, err := f.headSet(ctx, change)
	if err != nil {
		return err
	}
	rank := map[string]int{}
	for _, set := range []*docs.Set{main, head} {
		for _, c := range set.Clauses(clause.Spec) {
			if _, ok := rank[c.ID.String()]; !ok {
				rank[c.ID.String()] = len(rank)
			}
		}
	}
	return f.Tracker.Entangle(change, func(ids []string) []string {
		out := slices.Clone(ids)
		slices.SortStableFunc(out, func(a, b string) int {
			ra, oka := rank[a]
			rb, okb := rank[b]
			switch {
			case oka && okb:
				return ra - rb
			case oka:
				return -1
			case okb:
				return 1
			}
			return strings.Compare(a, b)
		})
		return out
	})
}

// rejectAmendment rejects an amendment whose debate reached its cap with
// objections standing, none of them a charter objection (S.shed.14): the
// unit's change is rebased onto main as sealing does (S.vcs.10), the files
// under spec/ and horizon.md on it become those of its commit at the last
// seal rebased onto that main commit, and the unit is sealed again with the
// standing objections as the reason. A rebase that fails, or a restored file
// that holds a conflict, bounces the unit instead, and it stays in the
// amendment lane. While the cap on units in flight holds sealing back,
// nothing is restored and the unit waits in the amendment lane.
func (f *Factory) rejectAmendment(ctx context.Context, u tracker.Unit, lane amendment, standing []tracker.Objection, round int) (Outcome, error) {
	if full, err := f.inFlightFull(u.Change); err != nil || full {
		return Waiting, err
	}
	if lane.commit == "" {
		return "", fmt.Errorf("unit %s's last seal does not record its commit, so its sealed spec cannot be restored", unit.Short(u.Change))
	}
	commit, err := f.Repo.MainCommit(ctx)
	if err != nil {
		return "", err
	}
	if _, err := f.Repo.RebaseOnto(ctx, u.Change, commit); err != nil {
		return f.bounce(u, fmt.Sprintf("sealing could not rebase the change onto main %s: %v", commit, err))
	}
	head, err := f.Repo.Restore(ctx, u.Change, lane.commit, "spec", docs.HorizonPath)
	if err != nil {
		return "", err
	}
	if conflicts, err := f.conflictedIn(ctx, u.Change, "spec/", docs.HorizonPath); err != nil || conflicts != "" {
		if err != nil {
			return "", err
		}
		return f.bounce(u, "the amendment was rejected, but the restored sealed spec or horizon conflicts with main: "+conflicts)
	}
	main, err := f.mainSet(ctx)
	if err != nil {
		return "", err
	}
	spec, err := f.headSet(ctx, u.Change)
	if err != nil {
		return "", err
	}
	fp := lane.footprint
	fp.Modifies = modified(main, spec)
	if fp.Estimate <= 0 {
		fp.Estimate = u.Footprint.Estimate
	}
	reasons := []string{fmt.Sprintf("the amendment was rejected: %d objections still stand after %d rounds:", len(standing), round)}
	for _, o := range standing {
		reasons = append(reasons, fmt.Sprintf("- %s (member %d, %s, citing %s): %s", o.ID, o.Member, o.Kind, strings.Join(o.Citations, ", "), o.Text))
	}
	if err := f.Tracker.SealRejected(u.Change, commit, head, fp, unit.Committee, strings.Join(reasons, "\n"), onMain(main)); err != nil {
		return "", err
	}
	if err := f.entangle(ctx, u.Change, main); err != nil {
		return "", err
	}
	return Sealed, nil
}

// bounce sends a proposal back to its painter, counting a bounce, and
// reports whether that made it contested.
func (f *Factory) bounce(u tracker.Unit, reason string) (Outcome, error) {
	if err := f.Tracker.Bounce(u.Change, unit.Committee, reason); err != nil {
		return "", err
	}
	return f.afterBounce(u.Change)
}

// bounceUncounted sends a proposal back to its painter for a sealing rebase
// conflict under spec/ that a landing brought in after the painter's latest
// capture, counting no bounce (S.vcs.17), and reports whether that still
// made it contested.
func (f *Factory) bounceUncounted(u tracker.Unit, reason string) (Outcome, error) {
	if err := f.Tracker.UncountedBounce(u.Change, unit.Committee, reason); err != nil {
		return "", err
	}
	return f.afterBounce(u.Change)
}

// afterBounce reports whether a bounce, counted or not, made a unit
// contested.
func (f *Factory) afterBounce(change string) (Outcome, error) {
	after, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if after.State == unit.Contested {
		return Contested, nil
	}
	return Bounced, nil
}

// archive puts a proposal on a shelf on the committee's decision.
func (f *Factory) archive(ctx context.Context, u tracker.Unit, shelf unit.Shelf, because []tracker.Objection) (Outcome, error) {
	var citations, reasons []string
	for _, o := range because {
		for _, c := range o.Citations {
			if !slices.Contains(citations, c) {
				citations = append(citations, c)
			}
		}
		reasons = append(reasons, fmt.Sprintf("- %s (member %d): %s", o.ID, o.Member, o.Text))
	}
	reason := fmt.Sprintf("%s citing %s", shelf, strings.Join(citations, ", "))
	return f.shelve(ctx, u, shelf, citations, strings.Join(reasons, "\n"), unit.Committee, reason, nil, nil)
}

// Defer archives a contested unit on the deferred shelf on the owner's
// answer, with the reason as what would change the decision (S.owner.5).
func (f *Factory) Defer(ctx context.Context, change, reason string) error {
	if err := f.Tracker.CheckAnswer(change, tracker.Defer, reason); err != nil {
		return err
	}
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return err
	}
	answer := tracker.Answer{Time: time.Now(), Kind: tracker.Defer, Reason: reason}
	_, err = f.shelve(ctx, u, unit.Deferred, nil, reason, unit.Owner, reason, &answer, nil)
	return err
}

// Reject archives a contested unit on the rejected shelf on the owner's
// answer. The entry cites the charter clauses the reason names; a reason
// that names none, or names one badly, is refused (S.owner.8).
func (f *Factory) Reject(ctx context.Context, change, reason string) error {
	if err := f.Tracker.CheckAnswer(change, tracker.Reject, reason); err != nil {
		return err
	}
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return err
	}
	main, err := f.mainSet(ctx)
	if err != nil {
		return err
	}
	citations, err := charterNamed(main, reason)
	if err != nil {
		return err
	}
	answer := tracker.Answer{Time: time.Now(), Kind: tracker.Reject, Reason: reason}
	_, err = f.shelve(ctx, u, unit.Rejected, citations, reason, unit.Owner, reason, &answer, nil)
	return err
}

// charterNamed returns the charter clauses a reason names, in the order
// they first appear and without repeats. A range names every clause of the
// charter on main between its ends. It refuses a reason that names no
// charter clause, cites the charter at a revision, has a range whose ends
// do not increase, or names an ID that is not a clause of the charter on
// main (S.owner.8).
func charterNamed(main *docs.Set, reason string) ([]string, error) {
	charter := func(token string) (clause.ID, bool, error) {
		c, err := clause.ParseCitation(token)
		if err != nil || c.ID.Kind != clause.Charter {
			return clause.ID{}, false, nil
		}
		if c.Rev != "" {
			return clause.ID{}, false, fmt.Errorf("%s cites the charter at a revision; a reject names clauses of the charter on main", token)
		}
		if _, ok := main.Lookup(c.ID); !ok {
			return clause.ID{}, false, fmt.Errorf("%s is not a clause of the charter on main", c.ID)
		}
		return c.ID, true, nil
	}
	var named []string
	add := func(id clause.ID) {
		if s := id.String(); !slices.Contains(named, s) {
			named = append(named, s)
		}
	}
	for _, m := range clause.Cited(reason) {
		from, isCharter, err := charter(m.Token)
		if err != nil {
			return nil, err
		}
		if m.To == "" {
			if isCharter {
				add(from)
			}
			continue
		}
		to, toCharter, err := charter(m.To)
		if err != nil {
			return nil, err
		}
		if !isCharter || !toCharter {
			// Only two charter citations make a charter range; either
			// end on its own may still name a clause.
			if isCharter {
				add(from)
			}
			if toCharter {
				add(to)
			}
			continue
		}
		if to.N <= from.N {
			return nil, fmt.Errorf("the range %s to %s does not increase", from, to)
		}
		series := main.Series(from, to)
		slices.SortFunc(series, func(a, b clause.Clause) int { return clause.Compare(a.ID, b.ID) })
		for _, c := range series {
			add(c.ID)
		}
	}
	if len(named) == 0 {
		return nil, errors.New("a reject names the charter clauses the unit violates, and this reason names none")
	}
	return named, nil
}

// expireInfo carries the extra fields an archive shed frame -expire makes
// records (S.frame.7): the operator's shed.contested_timeout and how long
// the unit had waited from its latest move to contested to the archive. It
// is nil for an archive made any other way.
type expireInfo struct {
	Timeout, Wait time.Duration
}

// shelve puts a proposal on a shelf: it writes the archive entry, with the
// owner's answers to the unit and the answer being given, if any, and the
// horizon clauses the proposal changes against the main commit it descends
// from (S.shed.15), records the unit as archived and discards its change.
// With expire set, the archive is an expiry by shed frame -expire: the move
// and the entry also record that the unit expired, the timeout and the
// wait (S.frame.7).
func (f *Factory) shelve(ctx context.Context, u tracker.Unit, shelf unit.Shelf, citations []string, why string, actor unit.Actor, reason string, answer *tracker.Answer, expire *expireInfo) (Outcome, error) {
	main, err := f.mainSet(ctx)
	if err != nil {
		return "", err
	}
	head, err := f.headSet(ctx, u.Change)
	if err != nil {
		return "", err
	}
	base, err := f.Repo.Base(ctx, u.Change)
	if err != nil {
		return "", err
	}
	from, _ := docs.Load(revision.Git{Root: f.Root, Rev: base})
	record, err := f.record(u.Change)
	if err != nil {
		return "", err
	}
	answers, err := f.Tracker.Answers(u.Change)
	if err != nil {
		return "", err
	}
	if answer != nil {
		answers = append(answers, *answer)
	}
	rec := archive.Record{
		Title: u.Title, Change: u.Change, Shelf: shelf, Citations: citations,
		Reason: why, Proposal: bundle.Changes(main, head),
		Horizon: bundle.HorizonChanges(from, head), Answers: renderAnswers(answers), Debate: record,
	}
	if expire != nil {
		rec.Expired, rec.Timeout, rec.Wait = true, expire.Timeout, expire.Wait
	}
	entry := archive.Format(rec)
	if _, err := f.Repo.WriteArchive(ctx, archive.Path(shelf, u.Change), []byte(entry),
		fmt.Sprintf("%s: %s", shelf, u.Title)); err != nil {
		return "", err
	}
	if expire != nil {
		if err := f.Tracker.ExpireArchive(u.Change, shelf, actor, reason, expire.Timeout, expire.Wait); err != nil {
			return "", err
		}
	} else if err := f.Tracker.Archive(u.Change, shelf, actor, reason); err != nil {
		return "", err
	}
	if err := f.Repo.Discard(ctx, u.Change); err != nil {
		return "", err
	}
	if shelf == unit.Rejected {
		return Rejected, nil
	}
	return Deferred, nil
}

// renderAnswers lists the owner's answers, one per line.
func renderAnswers(answers []tracker.Answer) string {
	var b strings.Builder
	for _, a := range answers {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	return b.String()
}

func byKind(objections []tracker.Objection, kind string) []tracker.Objection {
	var out []tracker.Objection
	for _, o := range objections {
		if o.Kind == kind {
			out = append(out, o)
		}
	}
	return out
}

func ids(objections []tracker.Objection) string {
	var out []string
	for _, o := range objections {
		out = append(out, o.ID)
	}
	return strings.Join(out, ", ")
}

// inFlightFull reports whether sealing another unit would pass the cap on
// units in flight from sealed through queued.
func (f *Factory) inFlightFull(except string) (bool, error) {
	limit := f.Operator.Concurrency.InFlight
	if limit <= 0 {
		return false, nil
	}
	units, err := f.Tracker.Units()
	if err != nil {
		return false, err
	}
	n := 0
	for _, u := range units {
		switch u.State {
		case unit.Sealed, unit.Implementing, unit.Verifying, unit.Queued:
			if u.Change != except {
				n++
			}
		}
	}
	return n >= limit, nil
}
