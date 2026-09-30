package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// Objection kinds.
const (
	// CharterObjection says a proposal violates a charter clause. It vetoes
	// the proposal.
	CharterObjection = "charter"
	// HorizonObjection says a proposal does not move the spec toward the
	// horizon.
	HorizonObjection = "horizon"
	// SizeObjection says a proposal's footprint is too large.
	SizeObjection = "size"
	// SpecObjection says a clause is ambiguous, untestable, contradicts
	// another or misses behaviour.
	SpecObjection = "spec"
)

// ObjectionKinds lists every kind of objection.
var ObjectionKinds = []string{CharterObjection, HorizonObjection, SizeObjection, SpecObjection}

// Objection is a committee member's objection to a proposal.
type Objection struct {
	ID   string
	Unit string
	// Cycle is the debate the objection was raised in: the unit's cycle at
	// the time. Each bounce starts a new debate.
	Cycle     int
	Round     int
	Member    int
	Kind      string
	Citations []string
	Text      string
	// Answer is the proposer's answer, if any.
	Answer string
	// Withdrawn is the member's reason for withdrawing it; empty while it
	// stands.
	Withdrawn string
}

// Standing reports whether the objection has not been withdrawn.
func (o Objection) Standing() bool { return o.Withdrawn == "" }

// StartRound records that a debate round began on a proposed unit.
func (t *Tracker) StartRound(change string, round int) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		return []Event{{Kind: RoundStarted, Unit: change, Actor: unit.Shed, Round: round}}, nil
	})
	return err
}

// Object records a committee member's objection to a proposed unit in the
// current round, and returns its ID.
func (t *Tracker) Object(change string, member int, kind string, citations []string, text string) (string, error) {
	if !slices.Contains(ObjectionKinds, kind) {
		return "", fmt.Errorf("unknown objection kind %q; use one of %s", kind, strings.Join(ObjectionKinds, ", "))
	}
	if len(citations) == 0 {
		return "", errors.New("an objection cites at least one clause")
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("an objection needs text")
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		var round int
		if err := tx.QueryRow(`SELECT round FROM units WHERE change = ?`, change).Scan(&round); err != nil {
			return nil, err
		}
		return []Event{{Kind: ObjectionRaised, Unit: change, Actor: unit.Committee, Round: round,
			Objection: &ObjectionEv{Member: member, Kind: kind, Citations: citations, Text: text}}}, nil
	})
	if err != nil {
		return "", err
	}
	return events[0].Objection.ID, nil
}

// Withdraw withdraws a standing objection. Only the member who raised it
// may.
func (t *Tracker) Withdraw(id string, member int, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("a withdrawal needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		o, err := objectionIn(tx, id)
		if err != nil {
			return nil, err
		}
		if o.Member != member {
			return nil, fmt.Errorf("objection %s was raised by member %d; only they can withdraw it", id, o.Member)
		}
		if !o.Standing() {
			return nil, fmt.Errorf("objection %s was already withdrawn", id)
		}
		return []Event{{Kind: ObjectionClosed, Unit: o.Unit, Actor: unit.Committee, Reason: reason, Objection: &ObjectionEv{ID: id, Member: member}}}, nil
	})
	return err
}

// Answer records the proposer's answer to an objection.
func (t *Tracker) Answer(id, text string, actor unit.Actor) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("an answer needs text")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		o, err := objectionIn(tx, id)
		if err != nil {
			return nil, err
		}
		return []Event{{Kind: ObjectionAnswer, Unit: o.Unit, Actor: actor, Objection: &ObjectionEv{ID: id, Text: text}}}, nil
	})
	return err
}

// Objections returns a unit's objections in the order they were raised.
// With cycle at or above zero, only those of that debate.
func (t *Tracker) Objections(change string, cycle int) ([]Objection, error) {
	rows, err := t.db.Query(`SELECT id FROM objections WHERE change = ? AND (? < 0 OR cycle = ?) ORDER BY seq`, change, cycle, cycle)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []Objection
	for _, id := range ids {
		o, err := objectionIn(t.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

// Standing returns the objections standing in a unit's current debate.
func (t *Tracker) Standing(change string) ([]Objection, error) {
	u, err := t.Unit(change)
	if err != nil {
		return nil, err
	}
	all, err := t.Objections(u.Change, u.Cycle)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(o Objection) bool { return !o.Standing() }), nil
}

// Bounce sends a proposed unit back to its proposer without leaving the
// shed, and counts a bounce. The next debate starts afresh.
func (t *Tracker) Bounce(change string, actor unit.Actor, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("a bounce needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		contested, err := t.overThreshold(tx, change)
		if err != nil {
			return nil, err
		}
		return append([]Event{{Kind: UnitBounced, Unit: change, Actor: actor, Reason: reason}}, contested...), nil
	})
	return err
}

// UncountedBounce sends a proposed unit back to its painter as Bounce does,
// but counts no bounce toward S.unit.6's threshold: shed calls it for a
// sealing rebase that leaves an unresolved conflict under spec/ that a
// landing brought in after the painter's latest capture (S.vcs.17). It still
// moves the unit to contested, with a notice for the owner, once the run of
// uncounted bounces since the unit opened or was last sealed exceeds the
// operator's bounce threshold.
func (t *Tracker) UncountedBounce(change string, actor unit.Actor, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("a bounce needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		contested, err := t.overUncountedThreshold(tx, change)
		if err != nil {
			return nil, err
		}
		return append([]Event{{Kind: UnitBounceUncounted, Unit: change, Actor: actor, Reason: reason}}, contested...), nil
	})
	return err
}

// ContestTier moves a proposed unit whose horizon amendment is distant or
// eventual to contested for the owner, with shed as actor, and counts no
// bounce (S.shed.16). The owner is notified.
func (t *Tracker) ContestTier(change, tier, reason string) error {
	return t.contestTier(change, tier, reason, false)
}

// ContestSplit moves a proposed unit whose debate split at the round cap
// over its soon-tier horizon amendment to contested for the owner, with
// shed as actor, and counts no bounce (S.shed.18). The owner is notified.
func (t *Tracker) ContestSplit(change, tier, reason string) error {
	return t.contestTier(change, tier, reason, true)
}

func (t *Tracker) contestTier(change, tier, reason string, split bool) error {
	if strings.TrimSpace(tier) == "" {
		return errors.New("contesting a horizon amendment needs its tier")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("a move needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		return []Event{
			{Kind: UnitMoved, Unit: change, Actor: unit.Shed, From: unit.Proposed, To: unit.Contested, Tier: tier, Split: split, Reason: reason},
			{Kind: NoticeAdded, Unit: change, Actor: unit.Shed, Notice: &NoticeEv{
				Audience: string(unit.Owner), Kind: "contested",
				Body: fmt.Sprintf("Unit %s is contested: %s.", unit.Short(change), reason),
			}},
		}, nil
	})
	return err
}

// HoldSeal records that a proposed unit's debate ended a round with no
// objection standing while its seal is held back, so its next debate runs
// no further round.
func (t *Tracker) HoldSeal(change string, round int) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if err := requireState(tx, change, unit.Proposed); err != nil {
			return nil, err
		}
		return []Event{{Kind: Consensus, Unit: change, Actor: unit.Shed, Round: round}}, nil
	})
	return err
}

// Retitle changes a unit's title.
func (t *Tracker) Retitle(change, title string, actor unit.Actor) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("a unit needs a title")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if _, err := stateOf(tx, change); err != nil {
			return nil, err
		}
		return []Event{{Kind: UnitRetitled, Unit: change, Actor: actor, Title: title}}, nil
	})
	return err
}

func requireState(tx *sql.Tx, change string, want unit.State) error {
	state, err := stateOf(tx, change)
	if err != nil {
		return err
	}
	if state != want {
		return fmt.Errorf("unit %s is %s, not %s", unit.Short(change), state, want)
	}
	return nil
}

func objectionIn(q querier, id string) (Objection, error) {
	o := Objection{ID: id}
	var citations string
	err := q.QueryRow(`SELECT change, cycle, round, member, kind, citations, text, answer, withdrawn FROM objections WHERE id = ?`, id).
		Scan(&o.Unit, &o.Cycle, &o.Round, &o.Member, &o.Kind, &citations, &o.Text, &o.Answer, &o.Withdrawn)
	if errors.Is(err, sql.ErrNoRows) {
		return Objection{}, fmt.Errorf("objection %s: %w", id, ErrNotFound)
	}
	if citations != "" {
		o.Citations = strings.Split(citations, ",")
	}
	return o, err
}

// Spend returns what the sessions started since a time cost.
func (t *Tracker) Spend(since time.Time) (float64, error) {
	var total float64
	err := t.db.QueryRow(`SELECT COALESCE(SUM(cost_usd), 0) FROM sessions WHERE started_at >= ?`,
		since.UTC().Format(time.RFC3339Nano)).Scan(&total)
	return total, err
}

// InfraStreak counts the most recent sessions that failed for
// infrastructure reasons, in a row.
func (t *Tracker) InfraStreak() (int, error) {
	rows, err := t.db.Query(`SELECT status, outcome FROM sessions WHERE status != ? ORDER BY started_at DESC, CAST(substr(id, 2) AS INTEGER) DESC`, Running)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var status, outcome string
		if err := rows.Scan(&status, &outcome); err != nil {
			return 0, err
		}
		if SessionStatus(status) != Failed || !strings.HasPrefix(outcome, "infrastructure") {
			break
		}
		n++
	}
	return n, rows.Err()
}
