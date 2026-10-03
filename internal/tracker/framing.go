package tracker

import (
	"database/sql"
	"errors"
	"strconv"

	"github.com/kpenfound/shed/internal/unit"
)

const frameAttemptKey = "frame-attempt:"

// ClaimFraming records an automatic framing before its session starts.
// A clause is attempted at most once against each main revision, including
// across restarts and tracker rebuilds (S.frame.5).
func (t *Tracker) ClaimFraming(clause, commit string) (bool, error) {
	if clause == "" || commit == "" {
		return false, errors.New("a framing attempt needs a clause and main commit")
	}
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		var previous string
		err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, frameAttemptKey+clause+":"+commit).Scan(&previous)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return []Event{{Kind: FrameAttempted, Actor: unit.FrameBuilder, Clause: clause, Commit: commit}}, nil
	})
	return len(events) > 0, err
}

const expiryAttemptKey = "expiry-attempt:"

// ClaimExpiry records an automatic expiry session before it starts. A
// contested episode -- the pair of a unit and the sequence number of its
// latest move to contested -- is attempted at most once, including across
// restarts and tracker rebuilds (S.frame.8).
func (t *Tracker) ClaimExpiry(change string, seq int64) (bool, error) {
	if change == "" || seq == 0 {
		return false, errors.New("an expiry attempt needs a unit and the sequence number of its latest move to contested")
	}
	key := expiryAttemptKey + change + ":" + strconv.FormatInt(seq, 10)
	events, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		var previous string
		err := tx.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&previous)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return []Event{{Kind: ExpiryAttempted, Actor: unit.FrameBuilder, Unit: change, ContestSeq: seq}}, nil
	})
	return len(events) > 0, err
}
