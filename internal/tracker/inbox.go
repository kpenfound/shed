package tracker

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/kpenfound/shed/internal/unit"
)

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
	since := int64(-1)
	if err := tx.QueryRow(`SELECT value FROM meta WHERE key = 'inbox_seq'`).Scan(&since); err != nil && !errors.Is(err, sql.ErrNoRows) {
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
