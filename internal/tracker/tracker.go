// Package tracker keeps shed's workflow state: units, seals, footprints,
// counters, sessions and notices. Every change is appended to a JSONL event
// log first and then applied to a SQLite database, so the database can always
// be rebuilt from the log.
package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kpenfound/shed/internal/unit"
)

// Files in the state directory.
const (
	DBFile      = "tracker.db"
	LogFile     = "events.jsonl"
	LockFile    = "lock"
	SessionsDir = "sessions"
)

// Options tune a tracker.
type Options struct {
	// BounceThreshold is how many bounces a unit may take before it becomes
	// contested.
	BounceThreshold int
	// Now returns the time recorded on events. It defaults to time.Now.
	Now func() time.Time
	// Alive reports whether a session's process is still running. It
	// defaults to asking the operating system.
	Alive func(pid int) bool
	// SampleEvery samples every Nth horizon amendment landed to the owner;
	// zero samples none.
	SampleEvery int
}

// Tracker is an open state directory.
type Tracker struct {
	dir  string
	db   *sql.DB
	opts Options
}

// Open opens the tracker in a state directory, creating it if needed. It
// applies any logged events the database lacks and marks sessions whose
// process has exited as interrupted.
func Open(dir string, opts Options) (*Tracker, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Alive == nil {
		opts.Alive = processAlive
	}
	if err := os.MkdirAll(filepath.Join(dir, SessionsDir), 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, LogFile), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	logFile.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, DBFile)+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	t := &Tracker{dir: dir, db: db, opts: opts}
	if err := t.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating tracker schema: %w", err)
	}
	if _, err := t.write(t.interrupted); err != nil {
		db.Close()
		return nil, err
	}
	return t, nil
}

// Close closes the database.
func (t *Tracker) Close() error { return t.db.Close() }

// Dir is the state directory.
func (t *Tracker) Dir() string { return t.dir }

const schema = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS units (
	change TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	opened_by TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL,
	bounces INTEGER NOT NULL DEFAULT 0,
	uncounted_bounces INTEGER NOT NULL DEFAULT 0,
	estimate REAL NOT NULL DEFAULT 0,
	estimate_cost REAL NOT NULL DEFAULT 0,
	in_lane INTEGER NOT NULL DEFAULT 0,
	painter_captured INTEGER NOT NULL DEFAULT 0,
	painter_spec_conflict INTEGER NOT NULL DEFAULT 0,
	amendments INTEGER NOT NULL DEFAULT 0,
	shelf TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	landed TEXT NOT NULL DEFAULT '',
	actual INTEGER NOT NULL DEFAULT 0,
	review INTEGER NOT NULL DEFAULT 0,
	round INTEGER NOT NULL DEFAULT 0,
	cycle INTEGER NOT NULL DEFAULT 0,
	contested_seq INTEGER NOT NULL DEFAULT 0,
	contested_reason TEXT NOT NULL DEFAULT '',
	contested_at TEXT NOT NULL DEFAULT '',
	archived_seq INTEGER NOT NULL DEFAULT 0,
	expired INTEGER NOT NULL DEFAULT 0,
	wait INTEGER NOT NULL DEFAULT 0,
	sampled_seq INTEGER NOT NULL DEFAULT 0,
	opened_seq INTEGER NOT NULL,
	opened_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS seals (
	change TEXT PRIMARY KEY,
	main TEXT NOT NULL,
	commit_id TEXT NOT NULL DEFAULT '',
	sealed_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS footprints (
	change TEXT NOT NULL,
	relation TEXT NOT NULL,
	clause TEXT NOT NULL,
	PRIMARY KEY (change, relation, clause)
);
CREATE TABLE IF NOT EXISTS actual_footprints (
	change TEXT NOT NULL,
	relation TEXT NOT NULL,
	clause TEXT NOT NULL,
	PRIMARY KEY (change, relation, clause)
);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	change TEXT NOT NULL,
	role TEXT NOT NULL,
	step TEXT NOT NULL,
	pid INTEGER NOT NULL,
	status TEXT NOT NULL,
	outcome TEXT NOT NULL DEFAULT '',
	cost_usd REAL NOT NULL DEFAULT 0,
	started_at TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS steps (
	change TEXT NOT NULL,
	step TEXT NOT NULL,
	seq INTEGER NOT NULL,
	PRIMARY KEY (change, step)
);
CREATE TABLE IF NOT EXISTS objections (
	id TEXT PRIMARY KEY,
	change TEXT NOT NULL,
	cycle INTEGER NOT NULL,
	round INTEGER NOT NULL,
	member INTEGER NOT NULL,
	kind TEXT NOT NULL,
	citations TEXT NOT NULL,
	text TEXT NOT NULL,
	answer TEXT NOT NULL DEFAULT '',
	withdrawn TEXT NOT NULL DEFAULT '',
	seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notices (
	id TEXT PRIMARY KEY,
	change TEXT NOT NULL,
	audience TEXT NOT NULL,
	kind TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL,
	delivered_at TEXT NOT NULL DEFAULT ''
);
`

var tables = []string{"units", "seals", "footprints", "actual_footprints", "sessions", "steps", "notices", "objections", "meta"}

// schemaVersion changes whenever the schema does. The database is derived
// from the event log, so a database with another version is dropped and
// rebuilt rather than migrated.
const schemaVersion = 16

func (t *Tracker) migrate() error {
	var v string
	err := t.db.QueryRow(`SELECT value FROM meta WHERE key = 'schema'`).Scan(&v)
	if err == nil && v == strconv.Itoa(schemaVersion) {
		return nil
	}
	for _, table := range tables {
		if _, err := t.db.Exec("DROP TABLE IF EXISTS " + table); err != nil {
			return err
		}
	}
	if _, err := t.db.Exec(schema); err != nil {
		return err
	}
	_, err = t.db.Exec(`INSERT INTO meta (key, value) VALUES ('schema', ?)`, strconv.Itoa(schemaVersion))
	return err
}

// write runs one change to the tracker under the state directory lock. build
// reads the caught-up database and returns the events to record, or an
// error to record nothing. The events are logged, then applied.
func (t *Tracker) write(build func(tx *sql.Tx) ([]Event, error)) ([]Event, error) {
	unlock, err := t.lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	offset, err := t.catchUp()
	if err != nil {
		return nil, err
	}

	tx, err := t.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	events, err := build(tx)
	if err != nil || len(events) == 0 {
		return nil, err
	}
	seq, err := metaInt(tx, "seq")
	if err != nil {
		return nil, err
	}
	now := t.opts.Now().UTC()
	for i := range events {
		seq++
		events[i].Seq = seq
		events[i].Time = now
		// Sessions, notices and objections take their IDs from the event
		// that adds them.
		if e := &events[i]; e.Kind == SessionStarted && e.Session.ID == "" {
			e.Session.ID = "s" + strconv.FormatInt(seq, 10)
		} else if e.Kind == NoticeAdded && e.Notice.ID == "" {
			e.Notice.ID = "n" + strconv.FormatInt(seq, 10)
		} else if e.Kind == ObjectionRaised && e.Objection.ID == "" {
			e.Objection.ID = "o" + strconv.FormatInt(seq, 10)
		}
	}

	// Anything past the applied offset is a line cut short by a crash.
	logPath := filepath.Join(t.dir, LogFile)
	if info, err := os.Stat(logPath); err == nil && info.Size() > offset {
		if err := os.Truncate(logPath, offset); err != nil {
			return nil, err
		}
	}
	if err := appendEvents(logPath, events); err != nil {
		return nil, err
	}
	if err := t.applyAll(tx, events); err != nil {
		return nil, err
	}
	info, err := os.Stat(logPath)
	if err != nil {
		return nil, err
	}
	if err := setMeta(tx, "offset", info.Size()); err != nil {
		return nil, err
	}
	return events, tx.Commit()
}

// catchUp applies logged events the database has not seen, such as those
// logged just before a crash, and returns the log offset it reached.
func (t *Tracker) catchUp() (int64, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	offset, err := metaInt(tx, "offset")
	if err != nil {
		return 0, err
	}
	events, next, err := readEvents(filepath.Join(t.dir, LogFile), offset)
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return offset, nil
	}
	if err := t.applyAll(tx, events); err != nil {
		return 0, err
	}
	if err := setMeta(tx, "offset", next); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

// Rebuild discards the database's contents and replays the event log.
func (t *Tracker) Rebuild() error {
	unlock, err := t.lock()
	if err != nil {
		return err
	}
	defer unlock()
	tx, err := t.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range tables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO meta (key, value) VALUES ('schema', ?)`, strconv.Itoa(schemaVersion)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, err = t.catchUp()
	return err
}

func (t *Tracker) applyAll(tx *sql.Tx, events []Event) error {
	for _, e := range events {
		if err := apply(tx, e); err != nil {
			return fmt.Errorf("applying event %d (%s): %w", e.Seq, e.Kind, err)
		}
		if err := setMeta(tx, "seq", e.Seq); err != nil {
			return err
		}
	}
	return nil
}

// apply projects one event onto the database. It never refuses an event:
// events are checked before they are logged.
func apply(tx *sql.Tx, e Event) error {
	at := e.Time.UTC().Format(time.RFC3339Nano)
	exec := func(query string, args ...any) error {
		_, err := tx.Exec(query, args...)
		return err
	}
	switch e.Kind {
	case UnitOpened:
		return exec(`INSERT INTO units (change, title, opened_by, state, opened_seq, opened_at, updated_at, reason)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, e.Unit, e.Title, e.Actor, e.To, e.Seq, at, at, e.Reason)
	case UnitMoved:
		// A bounce starts a new debate, from round zero, and so does a
		// move to contested for a horizon amendment's tier.
		if err := exec(`UPDATE units SET state = ?, bounces = bounces + ?, cycle = cycle + ?, amendments = amendments + ?,
			shelf = ?, reason = ?, landed = ?, updated_at = ?, round = CASE WHEN ? THEN 0 ELSE round END WHERE change = ?`,
			e.To, boolInt(e.Bounce), boolInt(e.Bounce), boolInt(e.Amendment), e.Shelf, e.Reason, e.Commit, at, e.Bounce || e.Tier != "", e.Unit); err != nil {
			return err
		}
		// in_lane tracks whether the unit's latest reopen requested an
		// amendment (S.shed.11), so the next seal can tell whether it
		// carries the estimate forward (S.impl.7) and so the cost-since-seal
		// window should keep adding rather than start afresh (S.impl.8).
		if e.To == unit.Proposed && e.Bounce {
			if err := exec(`UPDATE units SET in_lane = ? WHERE change = ?`, boolInt(e.Amendment), e.Unit); err != nil {
				return err
			}
		}
		// A unit that leaves sealed, implementing, verifying and queued
		// leaves horizon review with them.
		if !unit.PastSeal(e.To) {
			if err := exec(`UPDATE units SET review = 0 WHERE change = ?`, e.Unit); err != nil {
				return err
			}
		}
		// Work starts over when a unit reopens or verification sends it
		// back.
		if e.Bounce || (e.From == unit.Verifying && e.To == unit.Implementing) {
			if err := exec(`DELETE FROM steps WHERE change = ?`, e.Unit); err != nil {
				return err
			}
		}
		if e.To == unit.Contested {
			if err := exec(`UPDATE units SET contested_seq = ?, contested_reason = ?, contested_at = ? WHERE change = ?`, e.Seq, e.Reason, at, e.Unit); err != nil {
				return err
			}
		}
		if e.To == unit.Archived {
			if err := exec(`UPDATE units SET archived_seq = ?, expired = ?, wait = ? WHERE change = ?`,
				e.Seq, boolInt(e.Expired), int64(e.Wait), e.Unit); err != nil {
				return err
			}
		}
		if e.HorizonAmendment != nil && *e.HorizonAmendment {
			n, err := metaInt(tx, horizonAmendmentsKey)
			if err != nil {
				return err
			}
			if err := setMeta(tx, horizonAmendmentsKey, n+1); err != nil {
				return err
			}
		}
		if e.Sampled {
			if err := exec(`UPDATE units SET sampled_seq = ? WHERE change = ?`, e.Seq, e.Unit); err != nil {
				return err
			}
		}
		if e.Seal != nil {
			if err := exec(`INSERT OR REPLACE INTO seals (change, main, commit_id, sealed_at) VALUES (?, ?, ?, ?)`,
				e.Unit, e.Seal.Main, e.Seal.Commit, at); err != nil {
				return err
			}
			// A seal starts a fresh run of uncounted bounces (S.vcs.17).
			if err := exec(`UPDATE units SET uncounted_bounces = 0 WHERE change = ?`, e.Unit); err != nil {
				return err
			}
		}
		if e.Actual != nil {
			if err := exec(`UPDATE units SET actual = 1 WHERE change = ?`, e.Unit); err != nil {
				return err
			}
			if err := writeFootprint(tx, "actual_footprints", e.Unit, *e.Actual); err != nil {
				return err
			}
		}
		if e.Footprint != nil {
			// Only a seal sets a footprint on a UnitMoved event. It starts
			// the cost-since-seal window afresh unless it carries the
			// estimate forward from a unit in the amendment lane with a
			// positive estimate already recorded (S.impl.7, S.impl.8).
			var inLane int
			var oldEstimate float64
			if err := tx.QueryRow(`SELECT in_lane, estimate FROM units WHERE change = ?`, e.Unit).Scan(&inLane, &oldEstimate); err != nil {
				return err
			}
			if carriedForward := inLane != 0 && oldEstimate > 0; !carriedForward {
				if err := exec(`UPDATE units SET estimate_cost = 0 WHERE change = ?`, e.Unit); err != nil {
					return err
				}
			}
			if err := writeFootprint(tx, "footprints", e.Unit, *e.Footprint); err != nil {
				return err
			}
			return exec(`UPDATE units SET estimate = ? WHERE change = ?`, e.Footprint.Estimate, e.Unit)
		}
		return nil
	case UnitFootprint:
		if e.Footprint == nil {
			return errors.New("footprint event without a footprint")
		}
		if err := writeFootprint(tx, "footprints", e.Unit, *e.Footprint); err != nil {
			return err
		}
		return exec(`UPDATE units SET estimate = ? WHERE change = ?`, e.Footprint.Estimate, e.Unit)
	case SessionStarted:
		s := e.Session
		return exec(`INSERT INTO sessions (id, change, role, step, pid, status, started_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, s.ID, e.Unit, s.Role, s.Step, s.PID, Running, at)
	case SessionFinished:
		s := e.Session
		if err := exec(`UPDATE sessions SET status = ?, outcome = ?, cost_usd = ?, finished_at = ? WHERE id = ?`,
			s.Status, s.Outcome, e.CostUSD, at, s.ID); err != nil {
			return err
		}
		// A finished session adds to the cost since the seal that set the
		// unit's estimate (S.impl.8); a session still running adds nothing.
		if err := exec(`UPDATE units SET estimate_cost = estimate_cost + ? WHERE change = ?`, e.CostUSD, e.Unit); err != nil {
			return err
		}
		if s.StepDone {
			return exec(`INSERT OR REPLACE INTO steps (change, step, seq) VALUES (?, ?, ?)`, e.Unit, s.Step, e.Seq)
		}
		return nil
	case NoticeAdded:
		n := e.Notice
		if e.Review {
			if err := exec(`UPDATE units SET review = 1 WHERE change = ?`, e.Unit); err != nil {
				return err
			}
		}
		return exec(`INSERT INTO notices (id, change, audience, kind, body, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			n.ID, e.Unit, n.Audience, n.Kind, n.Body, at)
	case NoticeDelivered:
		return exec(`UPDATE notices SET delivered_at = ? WHERE id = ?`, at, e.Notice.ID)
	case UnitBounced:
		return exec(`UPDATE units SET bounces = bounces + 1, cycle = cycle + 1, round = 0, reason = ?, updated_at = ? WHERE change = ?`, e.Reason, at, e.Unit)
	case UnitRestarted:
		// A new debate, as after a bounce, with no bounce counted.
		return exec(`UPDATE units SET cycle = cycle + 1, round = 0, reason = ?, updated_at = ? WHERE change = ?`, e.Reason, at, e.Unit)
	case UnitBounceUncounted:
		// A new debate, as after a bounce, with no bounce counted toward
		// S.unit.6's threshold, but counted toward S.vcs.17's own (reset at
		// each seal).
		return exec(`UPDATE units SET uncounted_bounces = uncounted_bounces + 1, cycle = cycle + 1, round = 0, reason = ?, updated_at = ? WHERE change = ?`, e.Reason, at, e.Unit)
	case UnitPainterCaptured:
		return exec(`UPDATE units SET painter_captured = 1, painter_spec_conflict = ? WHERE change = ?`, boolInt(e.SpecConflict), e.Unit)
	case UnitEntangled, UnitRebased, Consensus:
		return nil
	case UnitReviewed:
		return exec(`UPDATE units SET review = 0 WHERE change = ?`, e.Unit)
	case UnitHeld:
		// A held debate is over: the unit waits to be sealed, and a
		// debate that ends the hold starts afresh from round one.
		return exec(`UPDATE units SET round = 0, updated_at = ? WHERE change = ?`, at, e.Unit)
	case UnitRetitled:
		return exec(`UPDATE units SET title = ?, updated_at = ? WHERE change = ?`, e.Title, at, e.Unit)
	case RoundStarted:
		return exec(`UPDATE units SET round = ?, updated_at = ? WHERE change = ?`, e.Round, at, e.Unit)
	case ObjectionRaised:
		o := e.Objection
		return exec(`INSERT INTO objections (id, change, cycle, round, member, kind, citations, text, seq)
			VALUES (?, ?, (SELECT cycle FROM units WHERE change = ?), ?, ?, ?, ?, ?, ?)`,
			o.ID, e.Unit, e.Unit, e.Round, o.Member, o.Kind, strings.Join(o.Citations, ","), o.Text, e.Seq)
	case ObjectionClosed:
		return exec(`UPDATE objections SET withdrawn = ? WHERE id = ?`, e.Reason, e.Objection.ID)
	case ObjectionAnswer:
		return exec(`UPDATE objections SET answer = ? WHERE id = ?`, e.Objection.Text, e.Objection.ID)
	case InboxRead:
		if err := exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('inbox', ?)`, e.Commit); err != nil {
			return err
		}
		return setMeta(tx, "inbox_seq", e.ReadSeq)
	case ClauseKept:
		return setMeta(tx, keptKey+e.Clause, e.ReadSeq)
	case FrameAttempted:
		return setMeta(tx, frameAttemptKey+e.Clause+":"+e.Commit, e.Seq)
	}
	return fmt.Errorf("unknown event kind %q", e.Kind)
}

// writeFootprint replaces a unit's footprint in a footprint table: the
// sealed footprints or the actual ones.
func writeFootprint(tx *sql.Tx, table, change string, fp Footprint) error {
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE change = ?`, change); err != nil {
		return err
	}
	for relation, ids := range fp.relations() {
		for _, id := range ids {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO `+table+` (change, relation, clause) VALUES (?, ?, ?)`,
				change, relation, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fp Footprint) relations() map[string][]string {
	return map[string][]string{"modifies": fp.Modifies, "depends": fp.Depends, "advances": fp.Advances}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func metaInt(tx *sql.Tx, key string) (int64, error) {
	var v string
	err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func setMeta(tx *sql.Tx, key string, v int64) error {
	_, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)`, key, strconv.FormatInt(v, 10))
	return err
}

// lock takes an exclusive lock on the state directory, so that one shed
// process changes the tracker at a time.
func (t *Tracker) lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(t.dir, LockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
