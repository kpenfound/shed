package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// SessionStatus is where a session is.
type SessionStatus string

const (
	Running     SessionStatus = "running"
	Succeeded   SessionStatus = "succeeded"
	Failed      SessionStatus = "failed"
	Interrupted SessionStatus = "interrupted"
)

func validStatus(s SessionStatus) bool {
	return slices.Contains([]SessionStatus{Running, Succeeded, Failed, Interrupted}, s)
}

// Unit is a unit as the tracker holds it.
type Unit struct {
	Change string
	Title  string
	// OpenedBy is who opened the unit: the painter, or the owner by hand.
	OpenedBy   unit.Actor
	State      unit.State
	Bounces    int
	Amendments int
	// Round is the round of the unit's current debate.
	Round int
	// Reason is the reason given for the unit's latest move.
	Reason string
	Shelf  unit.Shelf
	// Seal is set once the unit has been sealed.
	Seal *Seal
	// Landed is the commit on main the unit landed as.
	Landed string
	// Footprint is the footprint the unit declared, or recorded at its
	// last seal.
	Footprint Footprint
	// Actual is the footprint recorded when the unit landed.
	Actual *Footprint
	// CostUSD is the total cost of the unit's sessions so far.
	CostUSD float64
	// Steps lists the unit's finished steps in the order they finished.
	Steps   []string
	Opened  time.Time
	Updated time.Time
}

// Session is a session as the tracker holds it.
type Session struct {
	ID      string
	Unit    string
	Role    unit.Actor
	Step    string
	PID     int
	Status  SessionStatus
	Outcome string
	CostUSD float64
	// Dir holds the session's bundle, transcript, outcome and result.
	Dir string
}

// BundlePath, TranscriptPath, OutcomePath and ResultPath are the files a
// session keeps in its directory.
func (s Session) BundlePath() string     { return filepath.Join(s.Dir, "bundle.md") }
func (s Session) TranscriptPath() string { return filepath.Join(s.Dir, "transcript.jsonl") }
func (s Session) OutcomePath() string    { return filepath.Join(s.Dir, "outcome.json") }
func (s Session) ResultPath() string     { return filepath.Join(s.Dir, "result.json") }

// Notice is a message for a role or the owner about a unit.
type Notice struct {
	ID        string
	Unit      string
	Audience  unit.Actor
	Kind      string
	Body      string
	Delivered bool
}

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// Units returns every unit in the order they opened.
func (t *Tracker) Units() ([]Unit, error) {
	rows, err := t.db.Query(`SELECT change FROM units ORDER BY opened_seq`)
	if err != nil {
		return nil, err
	}
	var changes []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return nil, err
		}
		changes = append(changes, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	units := make([]Unit, 0, len(changes))
	for _, c := range changes {
		u, err := loadUnit(t.db, c)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	return units, nil
}

// Unit returns the unit whose change ID is ref or starts with it.
func (t *Tracker) Unit(ref string) (Unit, error) {
	if ref == "" {
		return Unit{}, errors.New("no unit named")
	}
	rows, err := t.db.Query(`SELECT change FROM units WHERE substr(change, 1, ?) = ? ORDER BY opened_seq`, len(ref), ref)
	if err != nil {
		return Unit{}, err
	}
	var matches []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			rows.Close()
			return Unit{}, err
		}
		matches = append(matches, c)
	}
	rows.Close()
	switch len(matches) {
	case 0:
		return Unit{}, fmt.Errorf("unit %s: %w", ref, ErrNotFound)
	case 1:
		return loadUnit(t.db, matches[0])
	}
	return Unit{}, fmt.Errorf("%s names %d units; give more of the change ID", ref, len(matches))
}

func loadUnit(q querier, change string) (Unit, error) {
	u := Unit{Change: change}
	var opened, updated, shelf, state, openedBy string
	var actual bool
	err := q.QueryRow(`SELECT title, opened_by, state, bounces, amendments, round, reason, shelf, landed, actual, opened_at, updated_at,
		(SELECT COALESCE(SUM(cost_usd), 0) FROM sessions WHERE change = units.change)
		FROM units WHERE change = ?`, change).
		Scan(&u.Title, &openedBy, &state, &u.Bounces, &u.Amendments, &u.Round, &u.Reason, &shelf, &u.Landed, &actual, &opened, &updated, &u.CostUSD)
	if errors.Is(err, sql.ErrNoRows) {
		return Unit{}, fmt.Errorf("unit %s: %w", unit.Short(change), ErrNotFound)
	}
	if err != nil {
		return Unit{}, err
	}
	u.State, u.Shelf, u.OpenedBy = unit.State(state), unit.Shelf(shelf), unit.Actor(openedBy)
	u.Opened, _ = time.Parse(time.RFC3339Nano, opened)
	u.Updated, _ = time.Parse(time.RFC3339Nano, updated)

	var main, commit string
	err = q.QueryRow(`SELECT main, commit_id FROM seals WHERE change = ?`, change).Scan(&main, &commit)
	switch {
	case err == nil:
		u.Seal = &Seal{Main: main, Change: change, Commit: commit}
	case !errors.Is(err, sql.ErrNoRows):
		return Unit{}, err
	}

	if u.Footprint, err = loadFootprint(q, "footprints", change); err != nil {
		return Unit{}, err
	}
	if actual {
		fp, err := loadFootprint(q, "actual_footprints", change)
		if err != nil {
			return Unit{}, err
		}
		u.Actual = &fp
	}

	rows, err := q.Query(`SELECT step FROM steps WHERE change = ? ORDER BY seq`, change)
	if err != nil {
		return Unit{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var step string
		if err := rows.Scan(&step); err != nil {
			return Unit{}, err
		}
		u.Steps = append(u.Steps, step)
	}
	return u, rows.Err()
}

// loadFootprint reads a unit's footprint from a footprint table.
func loadFootprint(q querier, table, change string) (Footprint, error) {
	var fp Footprint
	rows, err := q.Query(`SELECT relation, clause FROM `+table+` WHERE change = ? ORDER BY relation, clause`, change)
	if err != nil {
		return fp, err
	}
	defer rows.Close()
	for rows.Next() {
		var relation, id string
		if err := rows.Scan(&relation, &id); err != nil {
			return fp, err
		}
		switch relation {
		case "modifies":
			fp.Modifies = append(fp.Modifies, id)
		case "depends":
			fp.Depends = append(fp.Depends, id)
		case "advances":
			fp.Advances = append(fp.Advances, id)
		}
	}
	return fp, rows.Err()
}

// Session returns a session by ID.
func (t *Tracker) Session(id string) (Session, error) {
	return sessionIn(t.db, t.dir, id)
}

func sessionIn(q querier, stateDir, id string) (Session, error) {
	s := Session{ID: id, Dir: sessionDir(stateDir, id)}
	var role, status string
	err := q.QueryRow(`SELECT change, role, step, pid, status, outcome, cost_usd FROM sessions WHERE id = ?`, id).
		Scan(&s.Unit, &role, &s.Step, &s.PID, &status, &s.Outcome, &s.CostUSD)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	s.Role, s.Status = unit.Actor(role), SessionStatus(status)
	return s, err
}

// Sessions returns a unit's sessions in the order they started.
func (t *Tracker) Sessions(change string) ([]Session, error) {
	rows, err := t.db.Query(`SELECT id FROM sessions WHERE change = ? ORDER BY started_at, CAST(substr(id, 2) AS INTEGER)`, change)
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
	var out []Session
	for _, id := range ids {
		s, err := t.Session(id)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Notices returns notices for an audience in the order they were added.
// With pending set, it returns only those not yet delivered.
func (t *Tracker) Notices(audience unit.Actor, pending bool) ([]Notice, error) {
	query := `SELECT id, change, audience, kind, body, delivered_at FROM notices WHERE audience = ?`
	if pending {
		query += ` AND delivered_at = ''`
	}
	rows, err := t.db.Query(query+` ORDER BY CAST(substr(id, 2) AS INTEGER)`, audience)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		var aud, delivered string
		if err := rows.Scan(&n.ID, &n.Unit, &aud, &n.Kind, &n.Body, &delivered); err != nil {
			return nil, err
		}
		n.Audience, n.Delivered = unit.Actor(aud), delivered != ""
		out = append(out, n)
	}
	return out, rows.Err()
}

// Events returns the logged events about a unit, oldest first.
func (t *Tracker) Events(change string) ([]Event, error) {
	events, _, err := readEvents(filepath.Join(t.dir, LogFile), 0)
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, e := range events {
		if e.Unit == change {
			out = append(out, e)
		}
	}
	return out, nil
}

// Describe summarises an event in one line.
func Describe(e Event) string {
	var b strings.Builder
	switch e.Kind {
	case UnitOpened:
		fmt.Fprintf(&b, "opened %q", e.Title)
	case UnitMoved:
		fmt.Fprintf(&b, "%s -> %s", e.From, e.To)
		if e.Amendment {
			b.WriteString(" (amendment)")
		}
		if e.Shelf != "" {
			fmt.Fprintf(&b, " on the %s shelf", e.Shelf)
		}
		if e.Seal != nil {
			fmt.Fprintf(&b, " at main %s", shortHash(e.Seal.Main))
		}
		if e.Commit != "" {
			fmt.Fprintf(&b, " as %s", shortHash(e.Commit))
		}
		if e.Drift != nil {
			fmt.Fprintf(&b, " (%s)", e.Drift)
		}
	case UnitFootprint:
		b.WriteString("footprint declared")
	case SessionStarted:
		fmt.Fprintf(&b, "session %s started step %s", e.Session.ID, e.Session.Step)
	case SessionFinished:
		fmt.Fprintf(&b, "session %s %s", e.Session.ID, e.Session.Status)
		if e.CostUSD > 0 {
			fmt.Fprintf(&b, " ($%.2f)", e.CostUSD)
		}
	case NoticeAdded:
		fmt.Fprintf(&b, "notice %s for %s: %s", e.Notice.ID, e.Notice.Audience, e.Notice.Body)
	case NoticeDelivered:
		fmt.Fprintf(&b, "notice %s delivered", e.Notice.ID)
	case UnitBounced:
		b.WriteString("bounced back to the proposer")
	case UnitRetitled:
		fmt.Fprintf(&b, "retitled %q", e.Title)
	case UnitEntangled:
		fmt.Fprintf(&b, "entangled with unit %s on %s", unit.Short(e.Entangled.Unit), strings.Join(e.Entangled.Clauses, ", "))
	case RoundStarted:
		fmt.Fprintf(&b, "debate round %d", e.Round)
	case ObjectionRaised:
		fmt.Fprintf(&b, "member %d objected (%s, citing %s): %s", e.Objection.Member, e.Objection.Kind, strings.Join(e.Objection.Citations, ", "), e.Objection.Text)
	case ObjectionClosed:
		fmt.Fprintf(&b, "objection %s withdrawn", e.Objection.ID)
	case ObjectionAnswer:
		fmt.Fprintf(&b, "objection %s answered: %s", e.Objection.ID, e.Objection.Text)
	case InboxRead:
		fmt.Fprintf(&b, "inbox read at main %s", shortHash(e.Commit))
	default:
		b.WriteString(e.Kind)
	}
	if e.Reason != "" {
		fmt.Fprintf(&b, ": %s", e.Reason)
	}
	return b.String()
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
