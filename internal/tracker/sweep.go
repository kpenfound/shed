package tracker

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// Sweep is a sweep of main as the tracker holds it.
type Sweep struct {
	Commit  string
	Started time.Time
	Clauses []SweepClause
}

// RecordSweep records a sweep of main's commit: when it started and pass or
// fail for each spec clause it swept. It names no unit (S.sweep.2).
func (t *Tracker) RecordSweep(commit string, started time.Time, clauses []SweepClause) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		return []Event{{Kind: SweepRan, Actor: unit.Shed, Commit: commit, Sweep: &SweepEv{Started: started, Clauses: clauses}}}, nil
	})
	return err
}

// Sweeps returns every recorded sweep, oldest first.
func (t *Tracker) Sweeps() ([]Sweep, error) {
	rows, err := t.db.Query(`SELECT commit_id, started_at, clauses FROM sweeps ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Sweep
	for rows.Next() {
		var s Sweep
		var started, clauses string
		if err := rows.Scan(&s.Commit, &started, &clauses); err != nil {
			return nil, err
		}
		s.Started, _ = time.Parse(time.RFC3339Nano, started)
		if err := json.Unmarshal([]byte(clauses), &s.Clauses); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
