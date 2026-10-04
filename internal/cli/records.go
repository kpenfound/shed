package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

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

// records implements `shed records` (S.ctx.1, S.ctx.2): it reads the event
// log directly, without opening the tracker, and prints the L0 records it
// selects.
func (e env) records(args []string) int {
	if len(args) > 0 {
		return e.misuse("records takes no arguments")
	}
	events, err := tracker.ReadLog(filepath.Join(e.state, tracker.LogFile))
	if err != nil {
		return e.fail(err)
	}
	for _, ev := range events {
		r, ok := recordFor(ev)
		if !ok {
			continue
		}
		line, err := json.Marshal(r)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stdout, string(line))
	}
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
