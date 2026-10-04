package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// record is one L0 record S.ctx.1 prints: an event log entry rendered in
// the exact key order the clause requires.
type record struct {
	ID    string   `json:"id"`
	Time  string   `json:"time"`
	Kind  string   `json:"kind"`
	Topic string   `json:"topic"`
	Actor string   `json:"actor"`
	Cites []string `json:"cites"`
	Text  string   `json:"text"`
}

// clauseRecord is one L0 record S.ctx.4 prints for a clause a commit on
// main's first-parent history adds, removes or changes.
type clauseRecord struct {
	ID       string   `json:"id"`
	Time     string   `json:"time"`
	Kind     string   `json:"kind"`
	Topic    string   `json:"topic"`
	Actor    string   `json:"actor"`
	Cites    []string `json:"cites"`
	Document string   `json:"document"`
	Commit   string   `json:"commit"`
	Before   string   `json:"before"`
	After    string   `json:"after"`
}

// records implements `shed records` (S.ctx.1 to S.ctx.5): it reads the event
// log, brings in main and reads its first-parent history and every compared
// document version, and only once all of that succeeds does it print the
// event records followed by the clause records.
func (e env) records(args []string) int {
	if len(args) > 0 {
		return e.misuse("records takes no arguments")
	}
	events, err := tracker.ReadLog(filepath.Join(e.state, tracker.LogFile))
	if err != nil {
		return e.fail(err)
	}
	var eventRecs []record
	for _, ev := range events {
		if r, ok := recordFor(ev); ok {
			eventRecs = append(eventRecs, r)
		}
	}

	var clauseRecs []clauseRecord
	if code := e.withFactory(func(f *factory.Factory) int {
		head, err := f.Repo.MainCommit(e.ctx)
		if err != nil {
			return e.fail(err)
		}
		commits, err := revision.FirstParent(f.Root, head)
		if err != nil {
			return e.fail(err)
		}
		recs, err := clauseRecordsOf(f.Root, commits, f.Tracker)
		if err != nil {
			return e.fail(err)
		}
		clauseRecs = recs
		return OK
	}); code != OK {
		return code
	}

	for _, r := range eventRecs {
		if code := e.printRecord(r); code != OK {
			return code
		}
	}
	for _, r := range clauseRecs {
		if code := e.printRecord(r); code != OK {
			return code
		}
	}
	return OK
}

// printRecord marshals and prints one record as a single JSON line.
func (e env) printRecord(v any) int {
	line, err := json.Marshal(v)
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintln(e.stdout, string(line))
	return OK
}

// recordFor maps one event to the L0 record S.ctx.2 gives it, reporting
// false for an event kind that gives none.
func recordFor(ev tracker.Event) (record, bool) {
	r := record{
		ID:    fmt.Sprintf("shed/event/%d", ev.Seq),
		Time:  ev.Time.UTC().Format(time.RFC3339),
		Topic: ev.Unit,
		Actor: string(ev.Actor),
		Cites: []string{},
	}
	switch ev.Kind {
	case tracker.UnitOpened:
		r.Kind = "unit.opened"
		r.Text = ev.Title
	case tracker.UnitFootprint:
		r.Kind = "footprint.declared"
		r.Text = ev.Reason
		if ev.Footprint != nil {
			r.Cites = append(r.Cites, ev.Footprint.Modifies...)
			r.Cites = append(r.Cites, ev.Footprint.Depends...)
			r.Cites = append(r.Cites, ev.Footprint.Advances...)
		}
	case tracker.ObjectionRaised:
		r.Kind = "objection"
		if ev.Objection != nil {
			r.Cites = append(r.Cites, ev.Objection.Citations...)
			r.Text = ev.Objection.Text
		}
	case tracker.ObjectionAnswer:
		r.Kind = "answer"
		if ev.Objection != nil {
			r.Text = ev.Objection.Text
		}
	case tracker.ObjectionClosed:
		r.Kind = "withdrawal"
		r.Text = ev.Reason
	case tracker.UnitMoved:
		switch {
		case ev.Bounce && ev.To == unit.Proposed:
			r.Kind = "reopen"
		case ev.To == unit.Sealed:
			r.Kind = "seal"
		case ev.To == unit.Archived:
			r.Kind = "archive"
		case ev.To == unit.Landed:
			r.Kind = "landing"
		default:
			return record{}, false
		}
		r.Text = ev.Reason
	default:
		return record{}, false
	}
	return r, true
}

// clauseDoc names one of the three documents S.ctx.3 compares commit by
// commit, the clause kinds it holds and how to tell a problem S.doc.5 names
// belongs to it.
type clauseDoc struct {
	name  string
	kinds []clause.Kind
	owns  func(file string) bool
}

var clauseDocs = []clauseDoc{
	{"charter", []clause.Kind{clause.Charter}, func(f string) bool { return f == docs.CharterPath }},
	{"spec", []clause.Kind{clause.Spec}, func(f string) bool { return strings.HasPrefix(f, docs.SpecDir+"/") }},
	{"horizon", []clause.Kind{clause.Horizon, clause.Milestone}, func(f string) bool { return f == docs.HorizonPath }},
}

// clauseRecordsOf computes the clause records S.ctx.3 and S.ctx.4 give for
// commits, oldest first. Each document is compared against the latest
// earlier commit at which it held no problem S.doc.5 names, or against none
// of its clauses when no earlier commit qualifies. A document version that
// fails to read for a reason other than not existing aborts the whole
// command (S.ctx.5), rather than being withheld like a malformed document or
// treated as a clean, missing baseline.
func clauseRecordsOf(root string, commits []revision.Commit, tr *tracker.Tracker) ([]clauseRecord, error) {
	baseline := map[string]map[clause.ID]clause.Clause{}
	for _, d := range clauseDocs {
		baseline[d.name] = map[clause.ID]clause.Clause{}
	}
	var out []clauseRecord
	for _, c := range commits {
		set, problems := docs.Load(revision.Git{Root: root, Rev: c.Hash})
		topic := ""
		if landedAsUnit(tr, c.Hash, c.Message) {
			topic = unitTrailer(c.Message)
		}
		when := c.Time.UTC().Format(time.RFC3339)
		for _, d := range clauseDocs {
			malformed, err := docProblem(problems, d)
			if err != nil {
				return nil, fmt.Errorf("reading %s at %s: %w", d.name, c.Hash, err)
			}
			if malformed {
				continue
			}
			current := clausesOf(set, d.kinds)
			for _, id := range unionClauseIDs(baseline[d.name], current) {
				before, hasBefore := baseline[d.name][id]
				after, hasAfter := current[id]
				kind := clauseChangeKind(hasBefore, before, hasAfter, after)
				if kind == "" {
					continue
				}
				out = append(out, clauseRecord{
					ID:       fmt.Sprintf("shed/clause/%s/%s", c.Hash, id),
					Time:     when,
					Kind:     kind,
					Topic:    topic,
					Actor:    c.Author,
					Cites:    []string{id.String()},
					Document: d.name,
					Commit:   c.Hash,
					Before:   before.Text,
					After:    after.Text,
				})
			}
			baseline[d.name] = current
		}
	}
	return out, nil
}

// docProblem classifies a document's problems among those Load returns for
// one commit. malformed reports a problem S.doc.5 names: a malformed ID, an
// ID in the wrong document, a duplicate ID or a nested clause; such a
// document is withheld for this commit alone (S.ctx.3). err reports a read
// failure for another reason, which S.ctx.5 requires to abort the whole
// command. A missing file or directory is neither: it is a clean,
// zero-clause baseline (S.ctx.3), and Load reports it with no line.
func docProblem(problems []clause.Problem, d clauseDoc) (malformed bool, err error) {
	for _, p := range problems {
		if !d.owns(p.File) {
			continue
		}
		switch {
		case p.Line > 0:
			malformed = true
		case docs.Missing(p):
		default:
			return false, errors.New(p.String())
		}
	}
	return malformed, nil
}

// clausesOf collects a set's clauses of the given kinds by ID.
func clausesOf(set *docs.Set, kinds []clause.Kind) map[clause.ID]clause.Clause {
	out := map[clause.ID]clause.Clause{}
	for _, k := range kinds {
		for _, c := range set.Clauses(k) {
			out[c.ID] = c
		}
	}
	return out
}

// unionClauseIDs lists every ID in either map, ascending by clause ID: by
// area as text and then by number as a number, with IDs that have no area
// after those that do (S.ctx.3).
func unionClauseIDs(a, b map[clause.ID]clause.Clause) []clause.ID {
	seen := map[clause.ID]bool{}
	var ids []clause.ID
	for id := range a {
		seen[id] = true
		ids = append(ids, id)
	}
	for id := range b {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, clause.Compare)
	return ids
}

// clauseChangeKind reports how a clause compares between a baseline and the
// current commit, as `shed diff` counts it (S.diff.1 to S.diff.3): added,
// removed, changed, or "" when it holds unchanged. A charter clause has no
// tags, so this compares its text alone, as S.doc.2 and S.ctx.3 require.
func clauseChangeKind(hasBefore bool, before clause.Clause, hasAfter bool, after clause.Clause) string {
	switch {
	case !hasBefore && hasAfter:
		return "clause.added"
	case hasBefore && !hasAfter:
		return "clause.removed"
	case hasBefore && hasAfter && (before.Text != after.Text || !slices.Equal(before.Tags, after.Tags)):
		return "clause.changed"
	}
	return ""
}
