package factory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kpenfound/shed/internal/archive"
	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
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
	Discarded   Outcome = "discarded"
)

// Committee outcomes in the shed.
const (
	outcomeClean     = "clean"
	outcomeObjecting = "objecting"
	outcomeReplied   = "replied"
)

// Declare records the dependencies and horizon clauses a proposal declares,
// with the clauses its spec diff modifies, and optionally retitles it.
func (f *Factory) Declare(ctx context.Context, change, title string, depends, advances []string, actor unit.Actor) error {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return err
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
	fp := tracker.Footprint{Modifies: modified(main, head), Depends: depends, Advances: advances}
	return f.Tracker.SetFootprint(u.Change, fp, actor, "declared", onMain(main))
}

// modified lists the spec clauses a unit's spec diff adds, changes or
// removes.
func modified(main, head *docs.Set) []string {
	d := docs.DiffSpec(main, head)
	var out []string
	for _, id := range slices.Concat(d.Added, d.Changed, d.Removed) {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
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
// standing objections say it is off the horizon is deferred, and any other
// goes back to its painter with a bounce.
func (f *Factory) Debate(ctx context.Context, change string) (Outcome, error) {
	u, err := f.Tracker.Unit(change)
	if err != nil {
		return "", err
	}
	if u.State != unit.Proposed {
		return "", fmt.Errorf("unit %s is %s; only proposed units are debated", unit.Short(u.Change), u.State)
	}
	if _, _, err := f.refreshFootprint(ctx, u); err != nil {
		return f.bounce(u, err.Error())
	}
	max, err := f.roundCap(u.Change)
	if err != nil {
		return "", err
	}
	for round := u.Round; ; {
		if round < max {
			round++
			if err := f.Tracker.StartRound(u.Change, round); err != nil {
				return "", err
			}
			if u, err = f.Tracker.Unit(u.Change); err != nil {
				return "", err
			}
			if err := f.committeeRound(ctx, u, round, max); err != nil {
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
			return f.seal(ctx, u, round)
		}
		if round >= max {
			if len(byKind(standing, tracker.HorizonObjection)) == len(standing) {
				return f.archive(ctx, u, unit.Deferred, standing)
			}
			return f.bounce(u, fmt.Sprintf("%d objections still stand after %d rounds: %s", len(standing), round, ids(standing)))
		}
		if err := f.reply(ctx, u, round); err != nil {
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

// roundCap is the round cap of a unit's debate. A unit whose latest reopen
// requested an amendment is in the amendment lane until it is next sealed,
// and its cap is shed.amendment_rounds (S.shed.11). Any other unit's cap is
// shed.max_rounds.
func (f *Factory) roundCap(change string) (int, error) {
	events, err := f.Tracker.Events(change)
	if err != nil {
		return 0, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Kind != tracker.UnitMoved {
			continue
		}
		if e.To == unit.Sealed {
			break
		}
		if e.To == unit.Proposed && e.Bounce {
			if e.Amendment {
				return f.Operator.Shed.AmendmentRounds, nil
			}
			break
		}
	}
	return f.Operator.Shed.MaxRounds, nil
}

// committeeRound runs every committee member's session of a round at once.
func (f *Factory) committeeRound(ctx context.Context, u tracker.Unit, round, max int) error {
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
			res, err := f.session(ctx, work{
				Unit: u, Role: unit.Committee, Prompt: roles.Committee,
				Step:     fmt.Sprintf("debate round %d, member %d", round, member),
				Task:     fmt.Sprintf("Debate the proposal %q. This is round %d of at most %d, and you are member %d.", u.Title, round, max, member),
				Tools:    func(_ string, head *docs.Set) []session.Tool { return f.committeeTools(u.Change, member, head) },
				Outcomes: []string{outcomeClean, outcomeObjecting},
				Extra:    []bundle.Section{{Title: "Your standing objections", Body: mine}},
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

// reply runs the painter's answer to the standing objections.
func (f *Factory) reply(ctx context.Context, u tracker.Unit, round int) error {
	type answerIn struct {
		Objection string `json:"objection" jsonschema:"the objection's ID"`
		Text      string `json:"text" jsonschema:"your answer, citing clause IDs"`
	}
	res, err := f.session(ctx, work{
		Unit: u, Role: unit.Painter, Prompt: roles.PainterReply, Writable: true,
		Step: fmt.Sprintf("reply to round %d", round),
		Task: fmt.Sprintf("Answer the committee's standing objections to %q after round %d.", u.Title, round),
		Tools: func(string, *docs.Set) []session.Tool {
			return []session.Tool{session.NewTool("answer", "Answer one standing objection.",
				func(_ context.Context, in answerIn) (string, error) {
					if err := f.Tracker.Answer(in.Objection, in.Text, unit.Painter); err != nil {
						return "", err
					}
					return "answer recorded", nil
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
	return nil
}

// seal seals a unit that reached consensus, against main as it is now.
func (f *Factory) seal(ctx context.Context, u tracker.Unit, round int) (Outcome, error) {
	if full, err := f.inFlightFull(u.Change); err != nil || full {
		return Waiting, err
	}
	fp, main, err := f.refreshFootprint(ctx, u)
	if err != nil {
		return f.bounce(u, err.Error())
	}
	commit, err := f.Repo.MainCommit(ctx)
	if err != nil {
		return "", err
	}
	if err := f.Tracker.Seal(u.Change, commit, fp, unit.Committee,
		fmt.Sprintf("no objection stands after round %d", round), onMain(main)); err != nil {
		return f.bounce(u, err.Error())
	}
	return Sealed, nil
}

// bounce sends a proposal back to its painter, and reports whether that
// made it contested.
func (f *Factory) bounce(u tracker.Unit, reason string) (Outcome, error) {
	if err := f.Tracker.Bounce(u.Change, unit.Committee, reason); err != nil {
		return "", err
	}
	after, err := f.Tracker.Unit(u.Change)
	if err != nil {
		return "", err
	}
	if after.State == unit.Contested {
		return Contested, nil
	}
	return Bounced, nil
}

// archive puts a proposal on a shelf: it writes the archive entry, records
// the unit as archived and discards its change.
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
	main, err := f.mainSet(ctx)
	if err != nil {
		return "", err
	}
	head, err := f.headSet(ctx, u.Change)
	if err != nil {
		return "", err
	}
	record, err := f.record(u.Change)
	if err != nil {
		return "", err
	}
	entry := archive.Format(archive.Record{
		Title: u.Title, Change: u.Change, Shelf: shelf, Citations: citations,
		Reason: strings.Join(reasons, "\n"), Proposal: bundle.Changes(main, head), Debate: record,
	})
	if _, err := f.Repo.WriteArchive(ctx, archive.Path(shelf, u.Change), []byte(entry),
		fmt.Sprintf("%s: %s", shelf, u.Title)); err != nil {
		return "", err
	}
	reason := fmt.Sprintf("%s citing %s", shelf, strings.Join(citations, ", "))
	if err := f.Tracker.Archive(u.Change, shelf, unit.Committee, reason); err != nil {
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
