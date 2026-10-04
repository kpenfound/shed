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
	"slices"
	"strings"
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
	UnitRebased     = "unit.rebased"
	// UnitRestarted sends a proposed unit back to its proposer to start a
	// new debate, as a bounce does, without counting a bounce (S.shed.18).
	UnitRestarted = "unit.restarted"
	// UnitBounceUncounted sends a proposed unit back to its painter, as a
	// bounce does, without counting toward S.unit.6's threshold: a sealing
	// rebase left an unresolved conflict under spec/ that came in after the
	// painter's latest capture (S.vcs.17).
	UnitBounceUncounted = "unit.bounce_uncounted"
	// UnitPainterCaptured records, each time shed captures the directory of
	// one of a unit's painter sessions (S.vcs.4), whether spec/ then held an
	// unresolved conflict (S.vcs.12, S.vcs.17).
	UnitPainterCaptured = "unit.painter_captured"
	UnitRetitled        = "unit.retitled"
	UnitEntangled       = "unit.entangled"
	UnitReviewed        = "unit.reviewed"
	UnitHeld            = "debate.held"
	RoundStarted        = "debate.round"
	// Consensus records that a debate round ended with no objection
	// standing while the cap on units in flight held the seal back.
	Consensus       = "debate.consensus"
	ObjectionRaised = "debate.objection"
	ObjectionClosed = "debate.withdrawn"
	ObjectionAnswer = "debate.answer"
	InboxRead       = "inbox.read"
	ClauseKept      = "clause.kept"
	FrameAttempted  = "frame.attempted"
	// ExpiryAttempted records an automatic expiry session before it starts,
	// keyed by the unit and the sequence number of its latest move to
	// contested (S.frame.8).
	ExpiryAttempted = "expiry.attempted"
	// UnitSampledAnswered records the owner's agree or disagree answer to a
	// sampled amendment (S.owner.20). It moves no unit.
	UnitSampledAnswered = "unit.sampled_answered"
	// SweepRan records a sweep of main: the commit it checked out, when it
	// started, and pass or fail for each spec clause it swept (S.sweep.2).
	// It names no unit.
	SweepRan = "sweep.ran"
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
	// Rejected is set on a seal that rejected a requested amendment.
	Rejected bool `json:"rejected,omitempty"`
	// Tier is set on a move to contested for the tier of a horizon
	// amendment (S.shed.16, S.shed.18): the amendment's tier.
	Tier string `json:"tier,omitempty"`
	// Split is set on a move to contested for a soon-tier horizon
	// amendment whose debate split at the round cap (S.shed.18).
	Split bool `json:"split,omitempty"`
	// Approved is set on the owner's move out of contested that approves
	// a horizon amendment waiting for the owner (S.shed.17).
	Approved bool `json:"approved,omitempty"`
	// Review is set on a notice that marks its unit for horizon review.
	Review bool `json:"review,omitempty"`
	// Shelf is set when a unit is archived.
	Shelf unit.Shelf `json:"shelf,omitempty"`
	// Expired marks an archive the frame builder made by hand on an overdue
	// contested unit (S.frame.6, S.frame.7). Timeout and Wait are the
	// operator's shed.contested_timeout and how long the unit had waited
	// from its latest move to contested to this archive.
	Expired bool          `json:"expired,omitempty"`
	Timeout time.Duration `json:"timeout,omitempty"`
	Wait    time.Duration `json:"wait,omitempty"`
	// Seal is set when a unit is sealed.
	Seal *Seal `json:"seal,omitempty"`
	// Commit is the commit on main a unit landed as, the main commit an
	// inbox read, or the main commit a sweep checked out.
	Commit    string     `json:"commit,omitempty"`
	Footprint *Footprint `json:"footprint,omitempty"`
	// ReadSeq is the sequence number of the latest event an inbox read, or
	// the latest event before the owner kept a charter clause.
	ReadSeq int64 `json:"read_seq,omitempty"`
	// ContestSeq is set on an expiry attempt (ExpiryAttempted) for the
	// sequence number of the unit's latest move to contested being claimed
	// (S.frame.8).
	ContestSeq int64 `json:"contest_seq,omitempty"`
	// Clause is the charter clause kept or the horizon clause being framed.
	Clause string `json:"clause,omitempty"`
	// Actual and Drift are set when a unit lands: its actual footprint and
	// how that differs from the footprint recorded at its seal.
	Actual *Footprint `json:"actual,omitempty"`
	Drift  *Drift     `json:"drift,omitempty"`
	// HorizonAmendment is set on every landing: whether the landed commit
	// amends a horizon clause. A landing that predates it records neither.
	HorizonAmendment *bool `json:"horizon_amendment,omitempty"`
	// OwnerAccepted is set on the landing of a horizon amendment: whether
	// it is owner-accepted (S.owner.11). It is unset on a landing that is
	// not a horizon amendment, and on one that predates this recording.
	OwnerAccepted *bool `json:"owner_accepted,omitempty"`
	// Sampled is set on the landing of a horizon amendment sampled to the
	// owner.
	Sampled bool `json:"sampled,omitempty"`
	// Agreed is set on a UnitSampledAnswered event: whether the owner
	// agreed with the sampled amendment (S.owner.20).
	Agreed bool `json:"agreed,omitempty"`
	// SpecConflict is set on a painter capture (S.vcs.17) that found spec/
	// holding an unresolved conflict.
	SpecConflict bool         `json:"spec_conflict,omitempty"`
	Session      *SessionEv   `json:"session,omitempty"`
	Notice       *NoticeEv    `json:"notice,omitempty"`
	Objection    *ObjectionEv `json:"objection,omitempty"`
	// Entangled is set on an entanglement advisory.
	Entangled *EntangledEv `json:"entangled,omitempty"`
	// Rebased is set when a landing's rebase of a unit is recorded.
	Rebased *RebasedEv `json:"rebased,omitempty"`
	// Sweep is set on a SweepRan event: when the sweep started and pass or
	// fail for each spec clause it swept.
	Sweep   *SweepEv `json:"sweep,omitempty"`
	Round   int      `json:"round,omitempty"`
	CostUSD float64  `json:"cost_usd,omitempty"`
}

// Seal pins a sealed unit to the main commit it was sealed against and to
// the commit its change pointed to when it was sealed.
type Seal struct {
	Main   string `json:"main"`
	Change string `json:"change"`
	Commit string `json:"commit,omitempty"`
	// FollowsApprove is whether this seal follows the owner's approve
	// (S.shed.17, S.owner.11).
	FollowsApprove bool `json:"follows_approve,omitempty"`
}

// Footprint is the clauses a unit modifies and depends on in the spec, the
// horizon clauses it advances, and its estimate in USD of what taking it
// from sealed to landed will cost. A footprint recorded before seals
// recorded estimates has none.
type Footprint struct {
	Modifies []string `json:"modifies,omitempty"`
	Depends  []string `json:"depends,omitempty"`
	Advances []string `json:"advances,omitempty"`
	Estimate float64  `json:"estimate,omitempty"`
}

// Drift is the difference between the clauses a unit was sealed to modify
// and the clauses it modified when it landed.
type Drift struct {
	// Unsealed are the clauses the unit modified but was not sealed to.
	Unsealed []string `json:"unsealed,omitempty"`
	// Unmodified are the clauses the unit was sealed to modify but did not.
	Unmodified []string `json:"unmodified,omitempty"`
}

// FootprintDrift compares the clauses a sealed footprint and an actual
// footprint modify.
func FootprintDrift(sealed, actual Footprint) Drift {
	var d Drift
	for _, id := range actual.Modifies {
		if !slices.Contains(sealed.Modifies, id) {
			d.Unsealed = append(d.Unsealed, id)
		}
	}
	for _, id := range sealed.Modifies {
		if !slices.Contains(actual.Modifies, id) {
			d.Unmodified = append(d.Unmodified, id)
		}
	}
	return d
}

// Held reports whether the unit modified exactly the clauses it was sealed
// to modify.
func (d Drift) Held() bool {
	return len(d.Unsealed)+len(d.Unmodified) == 0
}

func (d Drift) String() string {
	if d.Held() {
		return "footprint held"
	}
	list := func(ids []string) string {
		if len(ids) == 0 {
			return "none"
		}
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("footprint drifted: not sealed %s; not modified %s", list(d.Unsealed), list(d.Unmodified))
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

// EntangledEv names the in-flight unit a newly sealed unit is entangled
// with and the spec clauses their footprints share, in document order.
type EntangledEv struct {
	Unit    string   `json:"unit"`
	Clauses []string `json:"clauses"`
}

// RebasedEv names the unit whose landing made the commit a unit's change
// was rebased onto, and the rebase's outcome.
type RebasedEv struct {
	Lander  string `json:"lander"`
	Outcome string `json:"outcome"`
}

// SweepEv describes a sweep of main in the event log: when it started and
// pass or fail for each spec clause it swept.
type SweepEv struct {
	Started time.Time     `json:"started"`
	Clauses []SweepClause `json:"clauses"`
}

// SweepClause is one spec clause a sweep checked, and whether its proofs
// passed.
type SweepClause struct {
	Clause string `json:"clause"`
	Pass   bool   `json:"pass"`
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
