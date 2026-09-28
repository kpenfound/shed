package tracker

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/kpenfound/shed/internal/unit"
)

// Event kinds.
const (
	UnitOpened      = "unit.opened"
	UnitMoved       = "unit.moved"
	UnitFootprint   = "unit.footprint"
	SessionStarted  = "session.started"
	SessionFinished = "session.finished"
	NoticeAdded     = "notice.added"
	NoticeDelivered = "notice.delivered"
	UnitBounced     = "unit.bounced"
	UnitRetitled    = "unit.retitled"
	RoundStarted    = "debate.round"
	ObjectionRaised = "debate.objection"
	ObjectionClosed = "debate.withdrawn"
	ObjectionAnswer = "debate.answer"
)

// Event is one line of the event log. The log is the tracker's source of
// truth: replaying it rebuilds the database.
type Event struct {
	Seq    int64      `json:"seq"`
	Time   time.Time  `json:"time"`
	Kind   string     `json:"kind"`
	Unit   string     `json:"unit,omitempty"`
	Actor  unit.Actor `json:"actor"`
	Reason string     `json:"reason,omitempty"`
	From   unit.State `json:"from,omitempty"`
	To     unit.State `json:"to,omitempty"`

	// Title is set when a unit opens.
	Title string `json:"title,omitempty"`
	// Bounce and Amendment are set on a reopen.
	Bounce    bool `json:"bounce,omitempty"`
	Amendment bool `json:"amendment,omitempty"`
	// Shelf is set when a unit is archived.
	Shelf unit.Shelf `json:"shelf,omitempty"`
	// Seal is set when a unit is sealed.
	Seal *Seal `json:"seal,omitempty"`
	// Commit is the commit on main a unit landed as.
	Commit    string       `json:"commit,omitempty"`
	Footprint *Footprint   `json:"footprint,omitempty"`
	Session   *SessionEv   `json:"session,omitempty"`
	Notice    *NoticeEv    `json:"notice,omitempty"`
	Objection *ObjectionEv `json:"objection,omitempty"`
	Round     int          `json:"round,omitempty"`
	CostUSD   float64      `json:"cost_usd,omitempty"`
}

// Seal pins a sealed unit to the main commit it was sealed against and to
// the commit its change pointed to when it was sealed.
type Seal struct {
	Main   string `json:"main"`
	Change string `json:"change"`
	Commit string `json:"commit,omitempty"`
}

// Footprint is the clauses a unit modifies and depends on in the spec, and
// the horizon clauses it advances.
type Footprint struct {
	Modifies []string `json:"modifies,omitempty"`
	Depends  []string `json:"depends,omitempty"`
	Advances []string `json:"advances,omitempty"`
}

// SessionEv describes a session in the event log.
type SessionEv struct {
	ID       string        `json:"id"`
	Role     unit.Actor    `json:"role,omitempty"`
	Step     string        `json:"step,omitempty"`
	PID      int           `json:"pid,omitempty"`
	Status   SessionStatus `json:"status,omitempty"`
	Outcome  string        `json:"outcome,omitempty"`
	StepDone bool          `json:"step_done,omitempty"`
}

// NoticeEv describes a notice in the event log.
type NoticeEv struct {
	ID       string `json:"id"`
	Audience string `json:"audience,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Body     string `json:"body,omitempty"`
}

// ObjectionEv describes an objection, its withdrawal or its answer in the
// event log.
type ObjectionEv struct {
	ID        string   `json:"id"`
	Member    int      `json:"member,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	Citations []string `json:"citations,omitempty"`
	Text      string   `json:"text,omitempty"`
}

// appendEvents writes events to the log and syncs it.
func appendEvents(path string, events []Event) error {
	var buf bytes.Buffer
	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// readEvents reads the events after a byte offset and returns them with the
// offset just past the last complete line. A final line without a newline
// was cut short by a crash and is left unread.
func readEvents(path string, offset int64) ([]Event, int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	r := bufio.NewReaderSize(f, 64*1024)
	var events []Event
	for {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return events, offset, nil
		}
		if err != nil {
			return nil, offset, err
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, offset, fmt.Errorf("%s at byte %d: %w", path, offset, err)
		}
		events = append(events, e)
		offset += int64(len(line))
	}
}
