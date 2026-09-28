package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/factory"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
	"github.com/kpenfound/shed/internal/vcs"
)

// DefaultStateDir is the state directory under the repository root. It holds
// the tracker, the event log, session directories and operator settings, and
// stays out of version control.
const DefaultStateDir = ".shed"

func (e env) operator() (config.Operator, error) {
	return config.LoadOperator(filepath.Join(e.state, config.OperatorFile))
}

func (e env) openRepo(op config.Operator) (*vcs.Repo, error) {
	return vcs.Open(e.ctx, e.root, e.state, vcs.Options{
		JJ: op.VCS.JJ, Main: op.VCS.Main, Remote: op.VCS.Remote,
		Landing: vcs.Identity{Name: op.VCS.LandingName, Email: op.VCS.LandingEmail},
	})
}

// withRepo opens the tracker and the repository, runs fn and closes the
// tracker.
func (e env) withRepo(fn func(*tracker.Tracker, *vcs.Repo) int) int {
	op, err := e.operator()
	if err != nil {
		return e.fail(err)
	}
	repo, err := e.openRepo(op)
	if err != nil {
		return e.fail(err)
	}
	return e.withTracker(func(t *tracker.Tracker) int { return fn(t, repo) })
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
		if err := e.health(t); err != nil {
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
		return e.misuse("unit needs a subcommand: open, move, reopen, log or path")
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
	case "path":
		return e.unitPath(args[1:])
	case "declare":
		return e.unitDeclare(args[1:])
	}
	return e.misuse("unknown unit subcommand %q", args[0])
}

func (e env) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	return fs
}

func (e env) unitOpen(args []string) int {
	title := strings.Join(args, " ")
	if strings.TrimSpace(title) == "" {
		return e.misuse("unit open needs a title")
	}
	return e.withRepo(func(t *tracker.Tracker, repo *vcs.Repo) int {
		change, err := repo.NewUnit(e.ctx, title)
		if err != nil {
			return e.fail(err)
		}
		if err := t.OpenUnit(change, title, unit.Owner); err != nil {
			return e.fail(errors.Join(err, repo.Discard(e.ctx, change)))
		}
		fmt.Fprintf(e.stdout, "opened %s in proposed\n", change)
		return OK
	})
}

func (e env) unitPath(args []string) int {
	if len(args) != 1 {
		return e.misuse("unit path needs one unit")
	}
	return e.withRepo(func(t *tracker.Tracker, repo *vcs.Repo) int {
		u, err := t.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		dir, err := repo.Workspace(e.ctx, u.Change)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.stdout, dir)
		return OK
	})
}

// health prints whether dispatch is paused by the daily budget, and how
// many sessions in a row have failed for infrastructure reasons.
func (e env) health(t *tracker.Tracker) error {
	op, err := e.operator()
	if err != nil {
		return err
	}
	if budget := op.Budget.PerDayUSD; budget > 0 {
		spent, err := t.Spend(time.Now().Add(-24 * time.Hour))
		if err != nil {
			return err
		}
		if spent >= budget {
			fmt.Fprintf(e.stdout, "\nPaused: the daily budget is spent: $%.2f of $%.2f in the last 24 hours.\n", spent, budget)
		}
	}
	streak, err := t.InfraStreak()
	if err != nil {
		return err
	}
	if streak > 0 {
		fmt.Fprintf(e.stdout, "\nDegraded: the last %d sessions failed for infrastructure reasons.\n", streak)
	}
	return nil
}

func (e env) serve(args []string) int {
	fs := e.flags("serve")
	once := fs.Bool("once", false, "stop when there is nothing left to start")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() != 0 {
		return e.misuse("serve takes no arguments")
	}
	return e.withFactory(func(f *factory.Factory) int {
		err := f.Serve(e.ctx, factory.ServeOptions{Once: *once, Log: e.stdout})
		if err != nil && !errors.Is(err, context.Canceled) {
			return e.fail(err)
		}
		return OK
	})
}

// withFactory opens the factory, runs fn and closes it.
func (e env) withFactory(fn func(*factory.Factory) int) int {
	f, err := factory.Open(e.ctx, e.root, e.state, e.runner)
	if err != nil {
		return e.fail(err)
	}
	defer f.Close()
	return fn(f)
}

func splitIDs(s string) []string {
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (e env) unitDeclare(args []string) int {
	fs := e.flags("unit declare")
	title := fs.String("title", "", "a new title for the unit")
	depends := fs.String("depends", "", "comma-separated spec clauses the proposal depends on")
	advances := fs.String("advances", "", "comma-separated horizon clauses the proposal advances")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() != 1 {
		return e.misuse("unit declare needs one unit")
	}
	return e.withFactory(func(f *factory.Factory) int {
		u, err := f.Tracker.Unit(fs.Arg(0))
		if err != nil {
			return e.fail(err)
		}
		if err := f.Declare(e.ctx, u.Change, *title, splitIDs(*depends), splitIDs(*advances), unit.Owner); err != nil {
			return e.fail(err)
		}
		after, err := f.Tracker.Unit(u.Change)
		if err != nil {
			return e.fail(err)
		}
		fp := after.Footprint
		fmt.Fprintf(e.stdout, "%s modifies %s; depends on %s; advances %s\n", unit.Short(u.Change),
			orNone(fp.Modifies), orNone(fp.Depends), orNone(fp.Advances))
		return OK
	})
}

func orNone(ids []string) string {
	if len(ids) == 0 {
		return "nothing"
	}
	return strings.Join(ids, ", ")
}

func (e env) debate(args []string) int {
	if len(args) != 1 {
		return e.misuse("debate needs one unit")
	}
	return e.withFactory(func(f *factory.Factory) int {
		u, err := f.Tracker.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		out, err := f.Debate(e.ctx, u.Change)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "%s %s\n", unit.Short(u.Change), out)
		return OK
	})
}

func (e env) land(args []string) int {
	if len(args) != 1 {
		return e.misuse("land needs one unit")
	}
	return e.withFactory(func(f *factory.Factory) int {
		u, err := f.Tracker.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		out, err := f.Land(e.ctx, u.Change)
		if err != nil {
			return e.fail(err)
		}
		if out != factory.Landed {
			fmt.Fprintf(e.stdout, "%s %s\n", unit.Short(u.Change), out)
			return Failed
		}
		after, err := f.Tracker.Unit(u.Change)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "landed %s on main as %s\n", unit.Short(u.Change), after.Landed)
		if after.Actual != nil {
			fmt.Fprintln(e.stdout, tracker.FootprintDrift(after.Footprint, *after.Actual))
		}
		return OK
	})
}

func (e env) runUnit(args []string) int {
	if len(args) != 1 {
		return e.misuse("run needs one unit")
	}
	return e.withFactory(func(f *factory.Factory) int {
		u, err := f.Tracker.Unit(args[0])
		if err != nil {
			return e.fail(err)
		}
		out, err := f.Run(e.ctx, u.Change)
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.stdout, "%s %s\n", unit.Short(u.Change), out)
		if out != factory.Landed {
			return Failed
		}
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
