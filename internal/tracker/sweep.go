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
// fail for each spec clause it swept. In the same event, it also files a
// bug for each failing clause with no open bug, naming the clause, the
// commit and the clause's proof output, and closes the open bug of each
// clause the sweep found passing, recording the commit (S.sweep.2,
// S.sweep.3). It names no unit.
func (t *Tracker) RecordSweep(commit string, started time.Time, clauses []SweepClause) error {
	_, err := t.write(func(tx *sql.Tx) ([]Event, error) {
		open, err := openBugClauses(tx)
		if err != nil {
			return nil, err
		}
		var filed []FiledBugEv
		var closed []string
		for _, c := range clauses {
			if c.Pass {
				if open[c.Clause] {
					closed = append(closed, c.Clause)
				}
				continue
			}
			if !open[c.Clause] {
				filed = append(filed, FiledBugEv{Clause: c.Clause, Output: c.Output})
			}
		}
		return []Event{{Kind: SweepRan, Actor: unit.Shed, Commit: commit,
			Sweep: &SweepEv{Started: started, Clauses: clauses, Filed: filed, Closed: closed}}}, nil
	})
	return err
}

// openBugClauses returns the clauses that currently have an open bug.
func openBugClauses(tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.Query(`SELECT clause FROM bugs WHERE closed_commit = ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var clause string
		if err := rows.Scan(&clause); err != nil {
			return nil, err
		}
		out[clause] = true
	}
	return out, rows.Err()
}

// Bug is a bug the tracker holds, filed when a swept clause fails and has no
// open bug, and closed when its clause's bug is later found passing
// (S.sweep.3).
type Bug struct {
	Clause string
	// Commit is the main commit the sweep checked out when the bug was
	// filed.
	Commit string
	// Output is the output of the clause's proofs in the sweep that filed
	// the bug.
	Output string
	// Filed is when the sweep that filed the bug started.
	Filed time.Time
	// ClosedCommit is the main commit the sweep checked out when the bug
	// was closed, or "" while it is open.
	ClosedCommit string
}

// Open reports whether the bug is still open.
func (b Bug) Open() bool { return b.ClosedCommit == "" }

// Bugs returns every bug the tracker holds, oldest filed first.
func (t *Tracker) Bugs() ([]Bug, error) {
	rows, err := t.db.Query(`SELECT clause, commit_id, output, filed_at, closed_commit FROM bugs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bug
	for rows.Next() {
		var b Bug
		var filed string
		if err := rows.Scan(&b.Clause, &b.Commit, &b.Output, &filed, &b.ClosedCommit); err != nil {
			return nil, err
		}
		b.Filed, _ = time.Parse(time.RFC3339Nano, filed)
		out = append(out, b)
	}
	return out, rows.Err()
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
