package cli

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// DefaultStateDir is the state directory under the repository root. It holds
// the tracker, the event log, session directories and operator settings, and
// stays out of version control.
const DefaultStateDir = ".shed"

func (e env) operator() (config.Operator, error) {
	return config.LoadOperator(filepath.Join(e.state, config.OperatorFile))
}

func (e env) openTracker() (*tracker.Tracker, error) {
	op, err := e.operator()
	if err != nil {
		return nil, err
	}
	return tracker.Open(e.state, tracker.Options{BounceThreshold: op.Shed.BounceThreshold})
}

// withTracker opens the tracker, runs fn and closes the tracker.
func (e env) withTracker(fn func(*tracker.Tracker) int) int {
	t, err := e.openTracker()
	if err != nil {
		return e.fail(err)
	}
	defer t.Close()
	return fn(t)
}

func (e env) status(args []string) int {
	if len(args) > 0 {
		return e.misuse("status takes no arguments")
	}
	return e.withTracker(func(t *tracker.Tracker) int {
		units, err := t.Units()
		if err != nil {
			return e.fail(err)
		}
		if len(units) == 0 {
			fmt.Fprintln(e.stdout, "no units")
			return OK
		}
		w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "UNIT\tSTATE\tBOUNCES\tAMENDMENTS\tCOST\tTITLE")
		for _, u := range units {
			fmt.Fprintf(w, "%s\t%s\t%d\t%d\t$%.2f\t%s\n",
				unit.Short(u.Change), u.State, u.Bounces, u.Amendments, u.CostUSD, u.Title)
		}
		if err := w.Flush(); err != nil {
			return e.fail(err)
		}
		notices, err := t.Notices(unit.Owner, true)
		if err != nil {
			return e.fail(err)
		}
		if len(notices) > 0 {
			fmt.Fprintln(e.stdout, "\nWaiting for the owner:")
			for _, n := range notices {
				fmt.Fprintf(e.stdout, "  %s  %s\n", unit.Short(n.Unit), n.Body)
			}
		}
		return OK
	})
}

func (e env) unit(args []string) int {
	if len(args) == 0 {
		return e.misuse("unit needs a subcommand: open, move, reopen or log")
	}
	switch args[0] {
	case "open":
		return e.unitOpen(args[1:])
	case "move":
		return e.unitMove(args[1:])
	case "reopen":
		return e.unitReopen(args[1:])
	case "log":
		return e.unitLog(args[1:])
	}
	return e.misuse("unknown unit subcommand %q", args[0])
}

func (e env) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	return fs
}

func (e env) unitOpen(args []string) int {
	fs := e.flags("unit open")
	change := fs.String("change", "", "the unit's change ID")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	title := strings.Join(fs.Args(), " ")
	if *change == "" || title == "" {
		return e.misuse("unit open needs -change <id> and a title")
	}
	return e.withTracker(func(t *tracker.Tracker) int {
		if err := t.OpenUnit(*change, title, unit.Owner); err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "opened %s in proposed\n", unit.Short(*change))
		return OK
	})
}

// manualTargets are the states the owner may move a unit to by hand. Sealing,
// landing and archiving carry records of their own and happen through the
// shed, the merge queue and the frame builder.
var manualTargets = []unit.State{unit.Implementing, unit.Verifying, unit.Queued, unit.Proposed}

func (e env) unitMove(args []string) int {
	if len(args) < 3 {
		return e.misuse("unit move needs a unit, a state and a reason")
	}
	to, err := unit.ParseState(args[1])
	if err != nil {
		return e.misuse("%v", err)
	}
	manual := false
	for _, s := range manualTargets {
		manual = manual || s == to
	}
	if !manual {
		return e.misuse("units reach %s through shed, not by hand", to)
	}
	reason := strings.Join(args[2:], " ")
	return e.withTracker(func(t *tracker.Tracker) int {
		u, err := t.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		if unit.IsReopen(u.State, to) {
			return e.misuse("moving a %s unit to proposed is a reopen; use unit reopen", u.State)
		}
		if err := t.Move(u.Change, to, unit.Owner, reason); err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "moved %s from %s to %s\n", unit.Short(u.Change), u.State, to)
		return OK
	})
}

func (e env) unitReopen(args []string) int {
	fs := e.flags("unit reopen")
	amendment := fs.Bool("amendment", false, "the reopen requests an amendment to the sealed spec")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() < 2 {
		return e.misuse("unit reopen needs a unit and a reason")
	}
	reason := strings.Join(fs.Args()[1:], " ")
	return e.withTracker(func(t *tracker.Tracker) int {
		u, err := t.Unit(fs.Arg(0))
		if err != nil {
			return e.fail(err)
		}
		if err := t.Reopen(u.Change, unit.Owner, reason, *amendment); err != nil {
			return e.fail(err)
		}
		after, err := t.Unit(u.Change)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "reopened %s; bounces %d, now %s\n", unit.Short(u.Change), after.Bounces, after.State)
		return OK
	})
}

func (e env) unitLog(args []string) int {
	if len(args) != 1 {
		return e.misuse("unit log needs one unit")
	}
	return e.withTracker(func(t *tracker.Tracker) int {
		u, err := t.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		events, err := t.Events(u.Change)
		if err != nil {
			return e.fail(err)
		}
		w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
		for _, ev := range events {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", ev.Seq, ev.Time.Format(time.RFC3339), ev.Actor, tracker.Describe(ev))
		}
		if err := w.Flush(); err != nil {
			return e.fail(err)
		}
		return OK
	})
}

func (e env) config(args []string) int {
	if len(args) > 0 {
		return e.misuse("config takes no arguments")
	}
	op, err := e.operator()
	if err != nil {
		return e.fail(err)
	}
	fmt.Fprintf(e.stdout, "# %s\n", filepath.Join(e.state, config.OperatorFile))
	if err := op.Write(e.stdout); err != nil {
		return e.fail(err)
	}
	return OK
}

func (e env) tracker(args []string) int {
	if len(args) != 1 || args[0] != "rebuild" {
		return e.misuse("tracker takes one subcommand: rebuild")
	}
	return e.withTracker(func(t *tracker.Tracker) int {
		if err := t.Rebuild(); err != nil {
			return e.fail(err)
		}
		units, err := t.Units()
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "rebuilt the tracker from %s: %d units\n", tracker.LogFile, len(units))
		return OK
	})
}
