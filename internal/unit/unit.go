// Package unit defines change units: their states, the transitions between
// them, the roles that act on them and the change IDs that identify them.
package unit

import (
	"fmt"
	"regexp"
	"slices"
)

// State is where a unit is in its life.
type State string

const (
	Proposed     State = "proposed"
	Sealed       State = "sealed"
	Implementing State = "implementing"
	Verifying    State = "verifying"
	Queued       State = "queued"
	Landed       State = "landed"
	Contested    State = "contested"
	Archived     State = "archived"
)

// States lists every state in the order a unit usually passes through them.
var States = []State{Proposed, Sealed, Implementing, Verifying, Queued, Landed, Contested, Archived}

// ParseState parses a state name.
func ParseState(s string) (State, error) {
	if st := State(s); slices.Contains(States, st) {
		return st, nil
	}
	return "", fmt.Errorf("unknown state %q", s)
}

// Terminal reports whether a unit in this state never moves again.
func (s State) Terminal() bool { return s == Landed || s == Archived }

// InFlight reports whether a unit in this state has not landed and has not
// been archived.
func (s State) InFlight() bool { return !s.Terminal() }

var transitions = map[State][]State{
	Proposed:     {Sealed, Archived, Contested},
	Sealed:       {Implementing, Proposed},
	Implementing: {Verifying, Proposed},
	Verifying:    {Queued, Implementing, Proposed},
	Queued:       {Landed, Proposed},
	Contested:    {Proposed, Archived},
}

// CanMove reports whether a unit may move from one state to another.
func CanMove(from, to State) bool {
	return slices.Contains(transitions[from], to)
}

// IsReopen reports whether a move sends a unit back to the shed. A reopen
// counts a bounce.
func IsReopen(from, to State) bool {
	return to == Proposed && (from == Sealed || from == Implementing || from == Verifying || from == Queued)
}

// Actor is who acts on a unit: the owner, a role, or shed itself.
type Actor string

const (
	Owner        Actor = "owner"
	FrameBuilder Actor = "frame-builder"
	Painter      Actor = "painter"
	Committee    Actor = "committee"
	Mechanic     Actor = "mechanic"
	Wheelbuilder Actor = "wheelbuilder"
	Sweeper      Actor = "sweeper"
	Shed         Actor = "shed"
)

// Actors lists every actor.
var Actors = []Actor{Owner, FrameBuilder, Painter, Committee, Mechanic, Wheelbuilder, Sweeper, Shed}

// Roles lists the actors that run as agent sessions.
var Roles = []Actor{FrameBuilder, Painter, Committee, Mechanic, Wheelbuilder, Sweeper}

// ParseActor parses an actor name.
func ParseActor(s string) (Actor, error) {
	if a := Actor(s); slices.Contains(Actors, a) {
		return a, nil
	}
	return "", fmt.Errorf("unknown actor %q", s)
}

// Shelf is where an archived unit rests.
type Shelf string

const (
	// Rejected units violated the charter.
	Rejected Shelf = "rejected"
	// Deferred units were clean but off the horizon.
	Deferred Shelf = "deferred"
)

// ParseShelf parses a shelf name.
func ParseShelf(s string) (Shelf, error) {
	switch Shelf(s) {
	case Rejected, Deferred:
		return Shelf(s), nil
	}
	return "", fmt.Errorf("unknown shelf %q", s)
}

// changeIDPattern matches jj change IDs, written in the letters k to z.
var changeIDPattern = regexp.MustCompile(`^[k-z]{12,64}$`)

// ValidChangeID checks that s is a jj change ID.
func ValidChangeID(s string) error {
	if !changeIDPattern.MatchString(s) {
		return fmt.Errorf("%q is not a change ID: change IDs are at least 12 of the letters k to z", s)
	}
	return nil
}

// Short abbreviates a change ID for display.
func Short(change string) string {
	if len(change) > 12 {
		return change[:12]
	}
	return change
}
