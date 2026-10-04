package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	case unit.Landed:
		return errors.New("use Land to land a unit")
	}
	return t.move(change, Event{To: to, Actor: actor, Reason: reason})
}

// Reopen sends a unit back to the shed with a written reason and counts a
// bounce. An amendment request also counts an amendment.
func (t *Tracker) Reopen(change string, actor unit.Actor, reason string, amendment bool) error {
	return t.move(change, Event{To: unit.Proposed, Actor: actor, Reason: reason, Amendment: amendment, Bounce: true})
}

// ReopenFootprint reopens a unit as Reopen does and, in the same move,
// records a new footprint, such as an overrun's cleared estimate
// (S.impl.9). onMain reports whether a spec clause is on main.
func (t *Tracker) ReopenFootprint(change string, actor unit.Actor, reason string, amendment bool, fp Footprint, onMain func(clause.ID) bool) error {
	return t.move(change, Event{To: unit.Proposed, Actor: actor, Reason: reason, Amendment: amendment, Bounce: true, Footprint: &fp}, onMain)
}

// Seal records a proposed unit's seal, the main commit and the commit its
// change points to, and its footprint, and moves it to sealed. onMain
// reports whether a spec clause is on main.
func (t *Tracker) Seal(change, main, commit string, fp Footprint, actor unit.Actor, reason string, onMain func(clause.ID) bool) error {
	return t.seal(change, main, commit, fp, actor, reason, false, false, onMain)
}

// SealRejected seals a unit whose requested amendment was rejected, as
// Seal does, and marks the seal as that rejection.
func (t *Tracker) SealRejected(change, main, commit string, fp Footprint, actor unit.Actor, reason string, onMain func(clause.ID) bool) error {
	return t.seal(change, main, commit, fp, actor, reason, true, false, onMain)
}

// SealApproved seals a unit as Seal does, and marks the seal as the one
// that follows the owner's approve (S.shed.17), so a horizon amendment
// landed under it is owner-accepted (S.owner.11).
func (t *Tracker) SealApproved(change, main, commit string, fp Footprint, actor unit.Actor, reason string, onMain func(clause.ID) bool) error {
	return t.seal(change, main, commit, fp, actor, reason, false, true, onMain)
}

func (t *Tracker) seal(change, main, commit string, fp Footprint, actor unit.Actor, reason string, rejected, approved bool, onMain func(clause.ID) bool) error {
	if strings.TrimSpace(main) == "" {
		return errors.New("a seal needs the main commit")
	}
	if strings.TrimSpace(commit) == "" {
		return errors.New("a seal needs the unit's commit")
	}
	return t.move(change, Event{To: unit.Sealed, Actor: actor, Reason: reason, Rejected: rejected,
		Seal: &Seal{Main: main, Change: change, Commit: commit, FollowsApprove: approved}, Footprint: &fp}, onMain)
}

// Entangle records an entanglement advisory on a sealed unit for every
// other sealed, implementing, verifying or queued unit whose spec footprint,
// as recorded at its last seal, shares a clause with the unit's own. The
// advisories come in the order those units opened, and order puts each
// advisory's shared clauses in document order. Horizon clauses never count.
// The advisories change no unit's state.
func (t *Tracker) Entangle(change string, order func([]string) []string) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		state, err := stateOf(tx, change)
		if err != nil {
			return nil, err
		}
		if state != unit.Sealed {
			return nil, fmt.Errorf("unit %s is %s; entanglement is reported when a unit seals", unit.Short(change), state)
		}
		own, err := loadFootprint(tx, "footprints", change)
		if err != nil {
			return nil, err
		}
		rows, err := tx.Query(`SELECT change FROM units WHERE change != ? AND state IN (?, ?, ?, ?) ORDER BY opened_seq`,
			change, unit.Sealed, unit.Implementing, unit.Verifying, unit.Queued)
		if err != nil {
			return nil, err
		}
		var others []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				rows.Close()
				return nil, err
			}
			others = append(others, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		mine := own.SpecClauses()
		var events []Event
		for _, other := range others {
			fp, err := loadFootprint(tx, "footprints", other)
			if err != nil {
				return nil, err
			}
			theirs := fp.SpecClauses()
			var shared []string
			for _, id := range mine {
				if slices.Contains(theirs, id) {
					shared = append(shared, id)
				}
			}
			if len(shared) == 0 {
				continue
			}
			events = append(events, Event{Kind: UnitEntangled, Unit: change, Actor: unit.Shed,
				Entangled: &EntangledEv{Unit: other, Clauses: order(shared)}})
		}
		return events, nil
	})
	return err
}

// SpecClauses returns the spec clauses a footprint modifies or depends on,
// each once.
func (fp Footprint) SpecClauses() []string {
	var out []string
	for _, id := range slices.Concat(fp.Modifies, fp.Depends) {
		if parsed, err := clause.ParseID(id); err == nil && parsed.Kind == clause.Spec && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Land records that a queued unit landed on main as a commit, with its
// actual footprint: the clauses the commit modifies, with the dependencies
// and horizon clauses recorded at its seal. The landing records how the
// actual footprint drifted from the sealed one.
//
// amendment says whether the landed commit amends a horizon clause
// (S.owner.11). The landing records it, and, for a horizon amendment,
// whether it is owner-accepted: whether the unit's latest seal follows the
// owner's approve. An auto-accepted amendment whose place in the sampling
// count is a multiple of SampleEvery is recorded as sampled.
func (t *Tracker) Land(change, commit string, actual Footprint, amendment bool, actor unit.Actor, reason string) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("a landing needs the commit on main")
	}
	return t.move(change, Event{To: unit.Landed, Commit: commit, Actual: &actual, HorizonAmendment: &amendment, Actor: actor, Reason: reason})
}

// LandFraming lands a proposed unit that shed frame opened, moving it
// straight from proposed to landed: the only route from proposed to landed
// (S.unit.3), taken only by `shed frame -accept` (S.frame.4). The landing
// always records a horizon amendment, owner-accepted, which takes its place
// in the count of horizon amendments but never in the sampling count and
// never as sampled, since the owner accepted it (S.owner.11).
func (t *Tracker) LandFraming(change, commit string, actual Footprint, actor unit.Actor, reason string) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("a landing needs the commit on main")
	}
	if err := validActor(actor); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("a move needs a reason")
	}
	amendment, ownerAccepted := true, true
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		state, openedBy, err := stateAndOpener(tx, change)
		if err != nil {
			return nil, err
		}
		if openedBy != unit.FrameBuilder {
			return nil, fmt.Errorf("unit %s was not opened by shed frame", unit.Short(change))
		}
		if state != unit.Proposed {
			return nil, fmt.Errorf("unit %s is %s; only a proposed framing lands this way", unit.Short(change), state)
		}
		return []Event{{Kind: UnitMoved, Unit: change, From: state, To: unit.Landed, Commit: commit,
			Actual: &actual, HorizonAmendment: &amendment, OwnerAccepted: &ownerAccepted, Actor: actor, Reason: reason}}, nil
	})
	return err
}

// Archive moves a unit to the archive on a shelf.
func (t *Tracker) Archive(change string, shelf unit.Shelf, actor unit.Actor, reason string) error {
	if _, err := unit.ParseShelf(string(shelf)); err != nil {
		return err
	}
	return t.move(change, Event{To: unit.Archived, Shelf: shelf, Actor: actor, Reason: reason})
}

// ExpireArchive archives a contested unit the frame builder expired by hand
// (S.frame.6, S.frame.7), marking the move as an expiry and recording the
// operator's shed.contested_timeout and how long the unit had waited from
// its latest move to contested to this archive, so shed tracker rebuild
// keeps them (S.track.5).
func (t *Tracker) ExpireArchive(change string, shelf unit.Shelf, actor unit.Actor, reason string, timeout, wait time.Duration) error {
	if _, err := unit.ParseShelf(string(shelf)); err != nil {
		return err
	}
	return t.move(change, Event{To: unit.Archived, Shelf: shelf, Actor: actor, Reason: reason, Expired: true, Timeout: timeout, Wait: wait})
}

// Consistent records that a horizon review found a marked unit consistent
// with the changed horizon, which clears the mark, and adds a notice giving
// the reason for audience. The unit keeps its state and its seal.
func (t *Tracker) Consistent(change string, actor, audience unit.Actor, reason string) error {
	if err := validActor(actor); err != nil {
		return err
	}
	if err := validActor(audience); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("a horizon review needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		_, marked, err := reviewOf(tx, change)
		if err != nil {
			return nil, err
		}
		if !marked {
			return nil, fmt.Errorf("unit %s is not marked for horizon review", unit.Short(change))
		}
		body := fmt.Sprintf("The %s reviewed the horizon changes and found this unit consistent with them: %s", actor, reason)
		return []Event{
			{Kind: UnitReviewed, Unit: change, Actor: actor, Reason: reason},
			{Kind: NoticeAdded, Unit: change, Actor: actor, Notice: &NoticeEv{Audience: string(audience), Kind: "horizon", Body: body}},
		}, nil
	})
	return err
}

// reviewOf returns a unit's state and whether it is marked for horizon
// review.
func reviewOf(tx *sql.Tx, change string) (unit.State, bool, error) {
	var s string
	var marked bool
	err := tx.QueryRow(`SELECT state, review FROM units WHERE change = ?`, change).Scan(&s, &marked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("unit %s: %w", unit.Short(change), ErrNotFound)
	}
	return unit.State(s), marked, err
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
	return t.moveThen(change, e, nil, onMain...)
}

// moveThen is move followed, in the same write, by further events.
func (t *Tracker) moveThen(change string, e Event, then []Event, onMain ...func(clause.ID) bool) error {
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
		if from == unit.Contested && !contestedExit(e.Actor, e.To) {
			return nil, fmt.Errorf("unit %s is contested; only the owner, or the frame builder archiving it, takes it out, not %s to %s",
				unit.Short(change), e.Actor, e.To)
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
		if e.Actual != nil {
			sealed, err := loadFootprint(tx, "footprints", change)
			if err != nil {
				return nil, err
			}
			drift := FootprintDrift(sealed, *e.Actual)
			e.Drift = &drift
		}
		if e.HorizonAmendment != nil && *e.HorizonAmendment {
			if e.OwnerAccepted == nil {
				var follows int
				if err := tx.QueryRow(`SELECT follows_approval FROM units WHERE change = ?`, change).Scan(&follows); err != nil {
					return nil, err
				}
				accepted := follows != 0
				e.OwnerAccepted = &accepted
			}
			if !*e.OwnerAccepted && t.opts.SampleEvery > 0 {
				n, err := metaInt(tx, samplingCountKey)
				if err != nil {
					return nil, err
				}
				e.Sampled = (n+1)%int64(t.opts.SampleEvery) == 0
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
		return append(events, then...), nil
	})
	return err
}

// contestedExit reports whether an actor may take a unit out of contested
// to a state: the owner to proposed or archived, as `shed answer` makes
// (S.owner.4, S.owner.5, S.owner.8, S.shed.17), or the frame builder to
// archived, as S.frame.7 makes. Every other actor, and the frame builder
// moving to any state but archived, is refused (S.unit.9).
func contestedExit(actor unit.Actor, to unit.State) bool {
	switch actor {
	case unit.Owner:
		return to == unit.Proposed || to == unit.Archived
	case unit.FrameBuilder:
		return to == unit.Archived
	default:
		return false
	}
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

// overUncountedThreshold returns the events that make a unit contested when
// the uncounted bounce about to be recorded leaves it with more uncounted
// bounces since it opened or was last sealed than the operator's bounce
// threshold (S.vcs.17). It counts no bounce itself.
func (t *Tracker) overUncountedThreshold(tx *sql.Tx, change string) ([]Event, error) {
	var n int
	if err := tx.QueryRow(`SELECT uncounted_bounces FROM units WHERE change = ?`, change).Scan(&n); err != nil {
		return nil, err
	}
	n++
	if n <= t.opts.BounceThreshold {
		return nil, nil
	}
	times := "times"
	if n == 1 {
		times = "time"
	}
	reason := fmt.Sprintf("landings kept bringing conflicts under spec/ %d %s, over the threshold of %d, so a unit that landings keep conflicting with still reaches the owner", n, times, t.opts.BounceThreshold)
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

// StartSession records a session on a unit and creates its directory. A
// session on no unit, such as the frame builder's, has an empty change.
func (t *Tracker) StartSession(change string, role unit.Actor, step string, pid int) (Session, error) {
	if !isRole(role) {
		return Session{}, fmt.Errorf("%q is not a role that runs sessions", role)
	}
	if strings.TrimSpace(step) == "" {
		return Session{}, errors.New("a session needs a step")
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if change != "" {
			state, err := stateOf(tx, change)
			if err != nil {
				return nil, err
			}
			if state.Terminal() {
				return nil, fmt.Errorf("unit %s is %s", unit.Short(change), state)
			}
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
	return t.addNotice(change, audience, kind, body, actor, false)
}

// AddReviewNotice records a notice as AddNotice does and, with review,
// marks its sealed, implementing, verifying or queued unit for horizon
// review (S.queue.5).
func (t *Tracker) AddReviewNotice(change string, audience unit.Actor, kind, body string, actor unit.Actor, review bool) (string, error) {
	return t.addNotice(change, audience, kind, body, actor, review)
}

func (t *Tracker) addNotice(change string, audience unit.Actor, kind, body string, actor unit.Actor, review bool) (string, error) {
	if err := validActor(audience); err != nil {
		return "", err
	}
	if err := validActor(actor); err != nil {
		return "", err
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		state, err := stateOf(tx, change)
		if err != nil {
			return nil, err
		}
		if review && !unit.PastSeal(state) {
			return nil, fmt.Errorf("unit %s is %s; only sealed, implementing, verifying and queued units are reviewed", unit.Short(change), state)
		}
		return []Event{{Kind: NoticeAdded, Unit: change, Actor: actor, Review: review,
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

// stateAndOpener returns a unit's state and who opened it.
func stateAndOpener(tx *sql.Tx, change string) (unit.State, unit.Actor, error) {
	var s, o string
	err := tx.QueryRow(`SELECT state, opened_by FROM units WHERE change = ?`, change).Scan(&s, &o)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("unit %s: %w", unit.Short(change), ErrNotFound)
	}
	return unit.State(s), unit.Actor(o), err
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

// RecordRebase records the outcome of rebasing a unit's change onto the
// main a landing made, naming the landed unit (S.vcs.15). It changes no
// unit's state.
func (t *Tracker) RecordRebase(change, lander, outcome string) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if _, err := stateOf(tx, change); err != nil {
			return nil, err
		}
		return []Event{{Kind: UnitRebased, Unit: change, Actor: unit.Shed,
			Rebased: &RebasedEv{Lander: lander, Outcome: outcome}}}, nil
	})
	return err
}

// RecordPainterCapture records, each time shed captures the directory of
// one of a unit's painter sessions (S.vcs.4), whether spec/ then holds an
// unresolved conflict (S.vcs.12). It changes no unit's state.
func (t *Tracker) RecordPainterCapture(change string, specConflict bool) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		if _, err := stateOf(tx, change); err != nil {
			return nil, err
		}
		return []Event{{Kind: UnitPainterCaptured, Unit: change, Actor: unit.Shed, SpecConflict: specConflict}}, nil
	})
	return err
}

// PainterSawSpecConflict reports whether the latest capture of one of a
// unit's painter session (S.vcs.4) found spec/ holding an unresolved
// conflict. A unit with no such capture reports true, since nothing
// establishes that a conflict a sealing rebase later finds came in after a
// painter saw it (S.vcs.17).
func (t *Tracker) PainterSawSpecConflict(change string) (bool, error) {
	var captured, conflict int
	err := t.db.QueryRow(`SELECT painter_captured, painter_spec_conflict FROM units WHERE change = ?`, change).Scan(&captured, &conflict)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("unit %s: %w", unit.Short(change), ErrNotFound)
	}
	if err != nil {
		return false, err
	}
	return captured == 0 || conflict != 0, nil
}
