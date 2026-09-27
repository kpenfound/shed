package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/unit"
)

// ErrNotFound reports a unit, session or notice the tracker does not hold.
var ErrNotFound = errors.New("not found")

// OpenUnit records a new unit in proposed, identified by its change ID.
func (t *Tracker) OpenUnit(change, title string, actor unit.Actor) error {
	if err := unit.ValidChangeID(change); err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("a unit needs a title")
	}
	if err := validActor(actor); err != nil {
		return err
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if _, err := stateOf(tx, change); err == nil {
			return nil, fmt.Errorf("unit %s already exists", unit.Short(change))
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		return []Event{{Kind: UnitOpened, Unit: change, Actor: actor, To: unit.Proposed, Title: title}}, nil
	})
	return err
}

// Move moves a unit to another state. Moving a unit back to proposed from
// sealed, implementing, verifying or queued is a reopen and counts a bounce.
// Sealing and archiving carry data of their own, so they have their own
// methods.
func (t *Tracker) Move(change string, to unit.State, actor unit.Actor, reason string) error {
	switch to {
	case unit.Sealed:
		return errors.New("use Seal to seal a unit")
	case unit.Archived:
		return errors.New("use Archive to archive a unit")
	}
	return t.move(change, Event{To: to, Actor: actor, Reason: reason})
}

// Reopen sends a unit back to the shed with a written reason and counts a
// bounce. An amendment request also counts an amendment.
func (t *Tracker) Reopen(change string, actor unit.Actor, reason string, amendment bool) error {
	return t.move(change, Event{To: unit.Proposed, Actor: actor, Reason: reason, Amendment: amendment, Bounce: true})
}

// Seal records a proposed unit's seal and footprint and moves it to sealed.
// onMain reports whether a spec clause is on main.
func (t *Tracker) Seal(change, main string, fp Footprint, actor unit.Actor, reason string, onMain func(clause.ID) bool) error {
	if strings.TrimSpace(main) == "" {
		return errors.New("a seal needs the main commit")
	}
	return t.move(change, Event{To: unit.Sealed, Actor: actor, Reason: reason,
		Seal: &Seal{Main: main, Change: change}, Footprint: &fp}, onMain)
}

// Archive moves a unit to the archive on a shelf.
func (t *Tracker) Archive(change string, shelf unit.Shelf, actor unit.Actor, reason string) error {
	if _, err := unit.ParseShelf(string(shelf)); err != nil {
		return err
	}
	return t.move(change, Event{To: unit.Archived, Shelf: shelf, Actor: actor, Reason: reason})
}

// SetFootprint records the footprint a unit declares while it is proposed.
func (t *Tracker) SetFootprint(change string, fp Footprint, actor unit.Actor, reason string, onMain func(clause.ID) bool) error {
	if err := validActor(actor); err != nil {
		return err
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		state, err := stateOf(tx, change)
		if err != nil {
			return nil, err
		}
		if state != unit.Proposed {
			return nil, fmt.Errorf("unit %s is %s; footprints are declared while proposed", unit.Short(change), state)
		}
		if err := checkFootprint(tx, change, fp, onMain); err != nil {
			return nil, err
		}
		return []Event{{Kind: UnitFootprint, Unit: change, Actor: actor, Reason: reason, Footprint: &fp}}, nil
	})
	return err
}

func (t *Tracker) move(change string, e Event, onMain ...func(clause.ID) bool) error {
	if err := validActor(e.Actor); err != nil {
		return err
	}
	if strings.TrimSpace(e.Reason) == "" {
		return errors.New("a move needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		from, err := stateOf(tx, change)
		if err != nil {
			return nil, err
		}
		if !unit.CanMove(from, e.To) {
			return nil, fmt.Errorf("unit %s cannot move from %s to %s", unit.Short(change), from, e.To)
		}
		if e.Bounce != unit.IsReopen(from, e.To) {
			if e.Bounce {
				return nil, fmt.Errorf("unit %s is %s; only sealed, implementing, verifying and queued units reopen", unit.Short(change), from)
			}
			return nil, fmt.Errorf("moving unit %s from %s back to proposed is a reopen", unit.Short(change), from)
		}
		if e.Footprint != nil {
			var f func(clause.ID) bool
			if len(onMain) > 0 {
				f = onMain[0]
			}
			if err := checkFootprint(tx, change, *e.Footprint, f); err != nil {
				return nil, err
			}
		}
		e.Kind, e.Unit, e.From = UnitMoved, change, from
		events := []Event{e}
		if e.Bounce {
			contested, err := t.overThreshold(tx, change)
			if err != nil {
				return nil, err
			}
			events = append(events, contested...)
		}
		return events, nil
	})
	return err
}

// overThreshold returns the events that make a unit contested when the
// bounce about to be recorded takes it past the threshold.
func (t *Tracker) overThreshold(tx *sql.Tx, change string) ([]Event, error) {
	var bounces int
	if err := tx.QueryRow(`SELECT bounces FROM units WHERE change = ?`, change).Scan(&bounces); err != nil {
		return nil, err
	}
	bounces++
	if bounces <= t.opts.BounceThreshold {
		return nil, nil
	}
	times := "times"
	if bounces == 1 {
		times = "time"
	}
	reason := fmt.Sprintf("bounced %d %s, over the threshold of %d", bounces, times, t.opts.BounceThreshold)
	return []Event{
		{Kind: UnitMoved, Unit: change, Actor: unit.Shed, From: unit.Proposed, To: unit.Contested, Reason: reason},
		{Kind: NoticeAdded, Unit: change, Actor: unit.Shed, Notice: &NoticeEv{
			Audience: string(unit.Owner), Kind: "contested",
			Body: fmt.Sprintf("Unit %s is contested: %s.", unit.Short(change), reason),
		}},
	}, nil
}

// checkFootprint refuses a footprint with malformed IDs, and one that
// depends on a clause missing from main: one another unit is adding and has
// not landed, or one no unit has. A clause the unit modifies itself is not a
// dependency.
func checkFootprint(tx *sql.Tx, change string, fp Footprint, onMain func(clause.ID) bool) error {
	for relation, ids := range fp.relations() {
		want := clause.Spec
		if relation == "advances" {
			want = clause.Horizon
		}
		for _, s := range ids {
			id, err := clause.ParseID(s)
			if err != nil {
				return fmt.Errorf("footprint %s: %w", relation, err)
			}
			if id.Kind != want {
				return fmt.Errorf("footprint %s: %s is not a %s clause", relation, id, want)
			}
		}
	}
	for _, s := range fp.Depends {
		id := clause.MustParseID(s)
		if slices.Contains(fp.Modifies, s) || (onMain != nil && onMain(id)) {
			continue
		}
		var other string
		err := tx.QueryRow(`SELECT f.change FROM footprints f JOIN units u ON u.change = f.change
			WHERE f.relation = 'modifies' AND f.clause = ? AND f.change != ? AND u.state NOT IN (?, ?)
			ORDER BY u.opened_seq LIMIT 1`, s, change, unit.Landed, unit.Archived).Scan(&other)
		switch {
		case err == nil:
			return fmt.Errorf("unit %s depends on %s, which unit %s changes and has not landed; wait for it to land or merge the two units",
				unit.Short(change), s, unit.Short(other))
		case !errors.Is(err, sql.ErrNoRows):
			return err
		case onMain != nil:
			return fmt.Errorf("unit %s depends on %s, which is not in the spec on main", unit.Short(change), s)
		}
	}
	return nil
}

// StartSession records a session on a unit and creates its directory.
func (t *Tracker) StartSession(change string, role unit.Actor, step string, pid int) (Session, error) {
	if !isRole(role) {
		return Session{}, fmt.Errorf("%q is not a role that runs sessions", role)
	}
	if strings.TrimSpace(step) == "" {
		return Session{}, errors.New("a session needs a step")
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		state, err := stateOf(tx, change)
		if err != nil {
			return nil, err
		}
		if state.Terminal() {
			return nil, fmt.Errorf("unit %s is %s", unit.Short(change), state)
		}
		return []Event{{Kind: SessionStarted, Unit: change, Actor: role,
			Session: &SessionEv{Role: role, Step: step, PID: pid}}}, nil
	})
	if err != nil {
		return Session{}, err
	}
	s, err := t.Session(events[0].Session.ID)
	if err != nil {
		return Session{}, err
	}
	return s, os.MkdirAll(s.Dir, 0o755)
}

// FinishSession records how a session ended and what it cost. When stepDone
// is set, the session's step counts as finished.
func (t *Tracker) FinishSession(id string, status SessionStatus, outcome string, costUSD float64, stepDone bool) error {
	if status == Running || !validStatus(status) {
		return fmt.Errorf("%q is not a finished session status", status)
	}
	if costUSD < 0 {
		return errors.New("cost must not be negative")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		s, err := sessionIn(tx, t.dir, id)
		if err != nil {
			return nil, err
		}
		if s.Status != Running {
			return nil, fmt.Errorf("session %s already finished as %s", id, s.Status)
		}
		return []Event{{Kind: SessionFinished, Unit: s.Unit, Actor: s.Role, CostUSD: costUSD,
			Session: &SessionEv{ID: id, Step: s.Step, Status: status, Outcome: outcome, StepDone: stepDone && status == Succeeded}}}, nil
	})
	return err
}

// interrupted returns events marking running sessions whose process has
// exited as interrupted.
func (t *Tracker) interrupted(tx *sql.Tx) ([]Event, error) {
	rows, err := tx.Query(`SELECT id, change, role, step, pid FROM sessions WHERE status = ? ORDER BY started_at, id`, Running)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var id, change, role, step string
		var pid int
		if err := rows.Scan(&id, &change, &role, &step, &pid); err != nil {
			return nil, err
		}
		if t.opts.Alive(pid) {
			continue
		}
		events = append(events, Event{Kind: SessionFinished, Unit: change, Actor: unit.Shed,
			Reason:  fmt.Sprintf("process %d exited before the session finished", pid),
			Session: &SessionEv{ID: id, Step: step, Status: Interrupted}})
	}
	return events, rows.Err()
}

// AddNotice records a notice for a role or the owner about a unit.
func (t *Tracker) AddNotice(change string, audience unit.Actor, kind, body string, actor unit.Actor) (string, error) {
	if err := validActor(audience); err != nil {
		return "", err
	}
	if err := validActor(actor); err != nil {
		return "", err
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if _, err := stateOf(tx, change); err != nil {
			return nil, err
		}
		return []Event{{Kind: NoticeAdded, Unit: change, Actor: actor,
			Notice: &NoticeEv{Audience: string(audience), Kind: kind, Body: body}}}, nil
	})
	if err != nil {
		return "", err
	}
	return events[0].Notice.ID, nil
}

// DeliverNotice marks a notice delivered.
func (t *Tracker) DeliverNotice(id string) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		var change, delivered string
		err := tx.QueryRow(`SELECT change, delivered_at FROM notices WHERE id = ?`, id).Scan(&change, &delivered)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("notice %s: %w", id, ErrNotFound)
		}
		if err != nil {
			return nil, err
		}
		if delivered != "" {
			return nil, fmt.Errorf("notice %s was already delivered", id)
		}
		return []Event{{Kind: NoticeDelivered, Unit: change, Actor: unit.Shed, Notice: &NoticeEv{ID: id}}}, nil
	})
	return err
}

func stateOf(tx *sql.Tx, change string) (unit.State, error) {
	var s string
	err := tx.QueryRow(`SELECT state FROM units WHERE change = ?`, change).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("unit %s: %w", unit.Short(change), ErrNotFound)
	}
	return unit.State(s), err
}

func validActor(a unit.Actor) error {
	_, err := unit.ParseActor(string(a))
	return err
}

func isRole(a unit.Actor) bool {
	for _, r := range unit.Roles {
		if r == a {
			return true
		}
	}
	return false
}

// sessionDir is where a session keeps its files.
func sessionDir(stateDir, id string) string {
	return filepath.Join(stateDir, SessionsDir, id)
}
