package tracker

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// FormatWait renders a duration rounded down to the whole minute as
// "<hours>h<minutes>m", such as "26h5m" (S.owner.15).
func FormatWait(d time.Duration) string {
	d = d.Truncate(time.Minute)
	return fmt.Sprintf("%dh%dm", int64(d/time.Hour), int64(d%time.Hour/time.Minute))
}

// ContestedUnit is a contested unit and the reason it was contested.
type ContestedUnit struct {
	Unit
	ContestedReason string
	// New is set when the unit became contested after the latest event the
	// last recorded inbox read, or when no inbox has been recorded.
	New bool
}

// Contested returns every contested unit in the order they became
// contested, each marked new or not against the last recorded inbox. It
// also returns the sequence number of the latest event it read, for the
// inbox to record.
func (t *Tracker) Contested() ([]ContestedUnit, int64, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	seq, err := metaInt(tx, "seq")
	if err != nil {
		return nil, 0, err
	}
	// With no inbox recorded, every contested unit is new.
	since, err := inboxSeq(tx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(`SELECT change, contested_seq, contested_reason FROM units WHERE state = ? ORDER BY contested_seq`, unit.Contested)
	if err != nil {
		return nil, 0, err
	}
	var out []ContestedUnit
	for rows.Next() {
		var c ContestedUnit
		var contestedSeq int64
		if err := rows.Scan(&c.Change, &contestedSeq, &c.ContestedReason); err != nil {
			rows.Close()
			return nil, 0, err
		}
		c.New = contestedSeq > since
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	for i := range out {
		u, err := loadUnit(tx, out[i].Change)
		if err != nil {
			return nil, 0, err
		}
		out[i].Unit = u
	}
	return out, seq, nil
}

// RejectedUnit is a unit archived on the rejected shelf.
type RejectedUnit struct {
	Unit
	// ArchivedSeq is the sequence number of the unit's move to archived.
	ArchivedSeq int64
	// New is set when the unit was archived after the latest event the
	// last recorded inbox read, or when no inbox has been recorded.
	New bool
}

// Rejected returns every unit on the rejected shelf in the order they were
// archived, each marked new or not against the last recorded inbox.
func (t *Tracker) Rejected() ([]RejectedUnit, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	since, err := inboxSeq(tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT change, archived_seq FROM units WHERE state = ? AND shelf = ? ORDER BY archived_seq`, unit.Archived, unit.Rejected)
	if err != nil {
		return nil, err
	}
	var out []RejectedUnit
	for rows.Next() {
		var r RejectedUnit
		if err := rows.Scan(&r.Change, &r.ArchivedSeq); err != nil {
			rows.Close()
			return nil, err
		}
		r.New = r.ArchivedSeq > since
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		u, err := loadUnit(tx, out[i].Change)
		if err != nil {
			return nil, err
		}
		out[i].Unit = u
	}
	return out, nil
}

// ExpiredUnit is a unit the frame builder archived on timeout (S.frame.7).
type ExpiredUnit struct {
	Unit
	// ArchivedSeq is the sequence number of the unit's move to archived.
	ArchivedSeq int64
	// Wait is how long the unit had waited from its latest move to
	// contested to the archive.
	Wait time.Duration
}

// Expired returns every unit the frame builder archived on timeout
// (S.frame.7) whose move to archived came after the latest event the last
// recorded inbox read, or every such unit when no inbox has been recorded,
// in the order they were archived (S.owner.16). A unit archived by any
// other actor is not returned.
func (t *Tracker) Expired() ([]ExpiredUnit, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	since, err := inboxSeq(tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT change, archived_seq, wait FROM units WHERE state = ? AND expired = 1 AND archived_seq > ? ORDER BY archived_seq`,
		unit.Archived, since)
	if err != nil {
		return nil, err
	}
	var out []ExpiredUnit
	for rows.Next() {
		var e ExpiredUnit
		var wait int64
		if err := rows.Scan(&e.Change, &e.ArchivedSeq, &wait); err != nil {
			rows.Close()
			return nil, err
		}
		e.Wait = time.Duration(wait)
		out = append(out, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		u, err := loadUnit(tx, out[i].Change)
		if err != nil {
			return nil, err
		}
		out[i].Unit = u
	}
	return out, nil
}

// horizonAmendmentsKey is the meta key counting the landings recorded as
// horizon amendments.
const horizonAmendmentsKey = "horizon_amendments"

// samplingCountKey is the meta key counting the auto-accepted horizon
// amendments landed: the sampling count (S.owner.11).
const samplingCountKey = "auto_accepted_amendments"

// Sampled returns the units sampled to the owner after the latest event
// the last recorded inbox read, or every sampled unit when no inbox has
// been recorded, in landing order (S.owner.11, S.owner.12).
func (t *Tracker) Sampled() ([]Unit, error) {
	tx, err := t.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	since, err := inboxSeq(tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT change FROM units WHERE sampled_seq > 0 AND sampled_seq > ? ORDER BY sampled_seq`, since)
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
	out := make([]Unit, 0, len(changes))
	for _, c := range changes {
		u, err := loadUnit(tx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// inboxSeq returns the sequence number of the latest event the last
// recorded inbox read, or -1 when no inbox has been recorded, so every
// event is after it.
func inboxSeq(tx *sql.Tx) (int64, error) {
	since := int64(-1)
	if err := tx.QueryRow(`SELECT value FROM meta WHERE key = 'inbox_seq'`).Scan(&since); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return since, nil
}

// InboxCommit returns the main commit the last recorded inbox read, or ""
// before the first.
func (t *Tracker) InboxCommit() (string, error) {
	var v string
	err := t.db.QueryRow(`SELECT value FROM meta WHERE key = 'inbox'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// RecordInbox records the main commit an inbox read and the sequence number
// of the latest event it read, so the next inbox lists what changed since.
// It moves no unit.
func (t *Tracker) RecordInbox(commit string, seq int64) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("an inbox read needs the main commit")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		return []Event{{Kind: InboxRead, Actor: unit.Owner, Commit: commit, ReadSeq: seq}}, nil
	})
	return err
}

// keptKey prefixes the meta key holding, for a kept charter clause, the
// sequence number its latest keep recorded.
const keptKey = "kept:"

// Kept returns, for each charter clause the owner has kept, the sequence
// number of the latest event before its latest keep (S.owner.10).
func (t *Tracker) Kept() (map[string]int64, error) {
	rows, err := t.db.Query(`SELECT key, value FROM meta WHERE key LIKE ?`, keptKey+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var seq int64
		if err := rows.Scan(&key, &seq); err != nil {
			return nil, err
		}
		out[strings.TrimPrefix(key, keptKey)] = seq
	}
	return out, rows.Err()
}

// Keep records the owner keeping a charter clause as it stands, with a
// reason and the sequence number of the latest event before the keep, so
// the clause's charter question counts only units archived after it
// (S.owner.9, S.owner.10). It moves no unit. The caller checks that the
// clause has a question listed.
func (t *Tracker) Keep(clause, reason string) error {
	if strings.TrimSpace(clause) == "" {
		return errors.New("a keep needs a charter clause")
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("an answer needs a reason")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		seq, err := metaInt(tx, "seq")
		if err != nil {
			return nil, err
		}
		return []Event{{Kind: ClauseKept, Actor: unit.Owner, Reason: reason, Clause: clause, ReadSeq: seq}}, nil
	})
	return err
}
