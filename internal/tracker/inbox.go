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
}

// Contested returns every contested unit in the order they became
// contested.
func (t *Tracker) Contested() ([]ContestedUnit, error) {
	rows, err := t.db.Query(`SELECT change, contested_reason FROM units WHERE state = ? ORDER BY contested_seq`, unit.Contested)
	if err != nil {
		return nil, err
	}
	var out []ContestedUnit
	for rows.Next() {
		var c ContestedUnit
		if err := rows.Scan(&c.Change, &c.ContestedReason); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		u, err := loadUnit(t.db, out[i].Change)
		if err != nil {
			return nil, err
		}
		out[i].Unit = u
	}
	return out, nil
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

// RecordInbox records the main commit an inbox read, so the next inbox
// lists what changed since. It moves no unit.
func (t *Tracker) RecordInbox(commit string) error {
	if strings.TrimSpace(commit) == "" {
		return errors.New("an inbox read needs the main commit")
	}
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		return []Event{{Kind: InboxRead, Actor: unit.Owner, Commit: commit}}, nil
	})
	return err
}
