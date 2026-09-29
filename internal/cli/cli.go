// Package cli implements the shed command line.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/revision"
	"github.com/kpenfound/shed/internal/session"
)

// Version is the release the binary was built from; a release build sets
// it. Without it, shed reports the commit it was built from, if known.
var Version = ""

// buildVersion names what this binary was built from: the release, else
// the commit Go recorded, else "dev".
func buildVersion() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		var revision, modified string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if revision != "" && modified == "true" {
			return "dev (" + revision + ", modified)"
		}
		if revision != "" {
			return "dev (" + revision + ")"
		}
	}
	return "dev"
}

// Exit codes.
const (
	OK      = 0
	Failed  = 1
	Misused = 2
)

const usage = `usage: shed [-C dir] [-state dir] <command> [arguments]

Documents:
  check              validate the documents, proofs and citations
  show <citation>... print the clauses the citations name
  diff <from> [<to>] list spec and horizon clauses added, removed or changed between revisions
  trace              list every horizon clause with the spec clauses advancing it
  gap                list the horizon clauses the spec has not realised
  prove [<id>...]    run the proofs of spec clauses and report per clause

Units:
  status                                  list units and what waits for the owner
  inbox [-peek]                           list contested units and horizon changes since the last inbox
  answer <unit> retry|defer|reject|approve <reason>
                                          answer a contested unit: retry, defer, reject or approve it
  answer <clause> keep <reason>           answer a charter question by keeping the clause
  unit open <title>                       make a change for a unit and open it in proposed
  unit move <unit> <state> <reason>       move a unit to another state
  unit reopen [-amendment] <unit> <reason> send a unit back to the shed
  unit log <unit>                         print a unit's events
  unit path <unit>                        print the directory of a unit's workspace
  unit declare [-title t] [-depends ids] [-advances ids] <unit>
                                          record what a proposal depends on and advances
  debate <unit>                           debate a proposed unit in the shed
  land <unit>                             land a queued unit on main
  run <unit>                              take a unit through the shed to main
  serve [-once]                           run the factory: propose, debate, implement, verify, land

State:
  config             print the operator settings in effect
  doctor             check that everything running the factory needs is in place
  tracker rebuild    rebuild the tracker database from the event log
  version            print the release shed was built from

The state directory defaults to .shed under the repository root, or
$SHED_STATE when set.
`

type env struct {
	ctx    context.Context
	root   string
	state  string
	runner session.Runner
	stdout io.Writer
	stderr io.Writer
}

// Run runs the command line and returns its exit code. Sessions run in
// Docker Sandboxes.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return RunWith(ctx, args, stdout, stderr, nil)
}

// RunWith runs the command line with a session runner; nil runs sessions in
// Docker Sandboxes.
func RunWith(ctx context.Context, args []string, stdout, stderr io.Writer, runner session.Runner) int {
	fs := flag.NewFlagSet("shed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	root := fs.String("C", ".", "repository root")
	state := fs.String("state", os.Getenv("SHED_STATE"), "state directory")
	if err := fs.Parse(args); err != nil {
		return Misused
	}
	if fs.NArg() == 0 {
		fs.Usage()
		return Misused
	}
	e := env{ctx: ctx, root: *root, state: *state, runner: runner, stdout: stdout, stderr: stderr}
	if e.state == "" {
		e.state = filepath.Join(e.root, DefaultStateDir)
	}
	// Sessions are granted paths, which must be absolute.
	for _, p := range []*string{&e.root, &e.state} {
		abs, err := filepath.Abs(*p)
		if err != nil {
			fmt.Fprintf(stderr, "shed: %v\n", err)
			return Failed
		}
		*p = abs
	}
	cmd, rest := fs.Arg(0), fs.Args()[1:]
	switch cmd {
	case "check":
		return e.check(rest)
	case "show":
		return e.show(rest)
	case "diff":
		return e.diff(rest)
	case "trace":
		return e.trace(rest, false)
	case "gap":
		return e.trace(rest, true)
	case "prove":
		return e.prove(rest)
	case "status":
		return e.status(rest)
	case "inbox":
		return e.inbox(rest)
	case "answer":
		return e.answer(rest)
	case "unit":
		return e.unit(rest)
	case "land":
		return e.land(rest)
	case "debate":
		return e.debate(rest)
	case "run":
		return e.runUnit(rest)
	case "serve":
		return e.serve(rest)
	case "config":
		return e.config(rest)
	case "tracker":
		return e.tracker(rest)
	case "doctor":
		return e.doctor(rest)
	case "version":
		fmt.Fprintf(stdout, "shed %s\n", buildVersion())
		return OK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return OK
	}
	fmt.Fprintf(stderr, "shed: unknown command %q\n", cmd)
	fs.Usage()
	return Misused
}

func (e env) misuse(format string, args ...any) int {
	fmt.Fprintf(e.stderr, "shed: "+format+"\n", args...)
	return Misused
}

func (e env) fail(err error) int {
	fmt.Fprintf(e.stderr, "shed: %v\n", err)
	return Failed
}

func (e env) report(problems []clause.Problem) int {
	slices.SortStableFunc(problems, func(a, b clause.Problem) int {
		if c := strings.Compare(a.File, b.File); c != 0 {
			return c
		}
		return a.Line - b.Line
	})
	for _, p := range problems {
		fmt.Fprintln(e.stderr, p)
	}
	if len(problems) > 0 {
		return Failed
	}
	return OK
}

// load reads the working tree's documents, failing on any problem with them.
func (e env) load() (*docs.Set, int) {
	set, problems := docs.Load(revision.Worktree(e.root))
	if len(problems) > 0 {
		return nil, e.report(problems)
	}
	return set, OK
}

func (e env) check(args []string) int {
	if len(args) > 0 {
		return e.misuse("check takes no arguments")
	}
	set, problems := docs.Load(revision.Worktree(e.root))
	problems = append(problems, docs.Validate(set, docs.NewResolver(e.root, set))...)
	history, err := docs.History(e.root, set)
	if err != nil {
		return e.fail(err)
	}
	problems = append(problems, history...)
	proofs, pp, err := proof.Discover(e.root)
	if err != nil {
		return e.fail(err)
	}
	problems = append(problems, pp...)
	problems = append(problems, proof.Check(proofs, set)...)
	if code := e.report(problems); code != OK {
		return code
	}
	fmt.Fprintf(e.stdout, "ok: %d charter, %d spec and %d horizon clauses, %d proofs\n",
		len(set.Clauses(clause.Charter)), len(set.Clauses(clause.Spec)),
		len(set.Clauses(clause.Horizon)), len(proofs))
	return OK
}

func (e env) show(args []string) int {
	if len(args) == 0 {
		return e.misuse("show needs at least one citation")
	}
	set, problems := docs.Load(revision.Worktree(e.root))
	r := docs.NewResolver(e.root, set)
	code := OK
	for _, arg := range args {
		cit, err := clause.ParseCitation(arg)
		if err == nil {
			var c clause.Clause
			if c, err = r.Resolve(cit); err == nil {
				fmt.Fprintf(e.stdout, "%s  %s:%d\n%s\n", cit, c.File, c.Line, indent(clauseBody(c)))
				continue
			}
		}
		fmt.Fprintf(e.stderr, "shed: %v\n", err)
		code = Failed
	}
	if code != OK && len(problems) > 0 {
		fmt.Fprintln(e.stderr, "shed: the working tree's documents have problems; run shed check")
	}
	return code
}

func clauseBody(c clause.Clause) string {
	if c.Tagged {
		return "(" + strings.Join(c.Tags, ", ") + ") " + c.Text
	}
	return c.Text
}

func indent(s string) string {
	return "    " + s
}

func (e env) diff(args []string) int {
	if len(args) < 1 || len(args) > 2 {
		return e.misuse("diff takes one or two revisions")
	}
	from, err := e.setAt(args[0])
	if err != nil {
		return e.fail(err)
	}
	var to *docs.Set
	if len(args) == 2 {
		to, err = e.setAt(args[1])
		if err != nil {
			return e.fail(err)
		}
	} else {
		to, _ = docs.Load(revision.Worktree(e.root))
	}
	d := docs.DiffSpec(from, to)
	for _, group := range []struct {
		label string
		ids   []clause.ID
	}{{"added", d.Added}, {"removed", d.Removed}, {"changed", d.Changed}} {
		for _, id := range group.ids {
			fmt.Fprintf(e.stdout, "%-8s%s\n", group.label, id)
		}
	}
	h := docs.DiffHorizonAmendment(from, to)
	for _, group := range []struct {
		label   string
		changes []docs.TieredHorizonChange
	}{{"added", h.Added}, {"removed", h.Removed}, {"changed", h.Changed}} {
		for _, c := range group.changes {
			fmt.Fprintf(e.stdout, "%-8s%s %s\n", group.label, c.ID, c.Tier)
		}
	}
	if h.Tier != "" {
		fmt.Fprintf(e.stdout, "%-8s%s\n", "tier", h.Tier)
	}
	return OK
}

func (e env) setAt(rev string) (*docs.Set, error) {
	commit, err := revision.Resolve(e.root, rev)
	if err != nil {
		return nil, err
	}
	set, _ := docs.Load(revision.Git{Root: e.root, Rev: commit})
	return set, nil
}

func (e env) trace(args []string, gapOnly bool) int {
	if len(args) > 0 {
		return e.misuse("this command takes no arguments")
	}
	set, code := e.load()
	if code != OK {
		return code
	}
	entries := docs.Trace(set)
	if gapOnly {
		entries = docs.Gap(set)
	}
	w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	for _, en := range entries {
		state := ""
		if en.Realised {
			state = docs.Realised
		}
		advanced := "-"
		if len(en.AdvancedBy) > 0 {
			advanced = docs.JoinIDs(en.AdvancedBy)
		}
		if gapOnly {
			var refinement string
			if en.Refines != (clause.ID{}) {
				refinement = fmt.Sprintf("\trefines %s (%s)", en.Refines, en.RefinesTier)
			}
			fmt.Fprintf(w, "%s\t%s\t%s%s\n", en.Clause.ID, en.Tier, advanced, refinement)
		} else {
			var refinement string
			switch {
			case en.Refines != (clause.ID{}):
				refinement = "\trefines " + en.Refines.String()
			case len(en.RefinedBy) > 0:
				refinement = "\trefined by " + docs.JoinIDs(en.RefinedBy)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s%s\n", en.Clause.ID, en.Tier, state, advanced, refinement)
		}
	}
	if err := w.Flush(); err != nil {
		return e.fail(err)
	}
	if !gapOnly {
		// S.horizon.12: name the near and soon clauses that refine nothing.
		var orphans []clause.ID
		for _, en := range entries {
			if (en.Tier == "near" || en.Tier == "soon") && en.Refines == (clause.ID{}) {
				orphans = append(orphans, en.Clause.ID)
			}
		}
		if len(orphans) > 0 {
			fmt.Fprintf(e.stdout, "no parent (%d): %s\n", len(orphans), docs.JoinIDs(orphans))
		}
	}
	return OK
}

func (e env) prove(args []string) int {
	set, code := e.load()
	if code != OK {
		return code
	}
	var ids []clause.ID
	for _, arg := range args {
		id, err := clause.ParseID(arg)
		if err != nil {
			return e.misuse("%v", err)
		}
		if _, ok := set.Lookup(id); !ok || id.Kind != clause.Spec {
			return e.misuse("%s is not a spec clause", id)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		for _, c := range set.Clauses(clause.Spec) {
			ids = append(ids, c.ID)
		}
	}
	cfg, err := config.LoadProject(e.root)
	if err != nil {
		return e.fail(err)
	}
	proofs, problems, err := proof.Discover(e.root)
	if err != nil {
		return e.fail(err)
	}
	if len(problems) > 0 {
		return e.report(problems)
	}
	var selected []proof.Proof
	for _, id := range ids {
		for _, p := range proof.For(proofs, id) {
			if !slices.ContainsFunc(selected, func(q proof.Proof) bool { return q.Name() == p.Name() }) {
				selected = append(selected, p)
			}
		}
	}
	runner := proof.Runner{Root: e.root, Prefix: cfg.Proofs.Runner, Stderr: e.stderr}
	results, err := runner.Run(e.ctx, selected)
	if err != nil {
		return e.fail(err)
	}

	code = OK
	w := tabwriter.NewWriter(e.stdout, 0, 4, 2, ' ', 0)
	for _, cr := range proof.ByClause(ids, proofs, results) {
		if cr.Pass {
			fmt.Fprintf(w, "pass\t%s\n", cr.ID)
			continue
		}
		code = Failed
		if len(cr.Proofs) == 0 {
			fmt.Fprintf(w, "fail\t%s\tno proof\n", cr.ID)
			continue
		}
		var notes []string
		for _, name := range slices.Sorted(maps.Keys(cr.Proofs)) {
			if s := cr.Proofs[name]; s != proof.Pass {
				notes = append(notes, name+": "+string(s))
			}
		}
		fmt.Fprintf(w, "fail\t%s\t%s\n", cr.ID, strings.Join(notes, ", "))
	}
	if err := w.Flush(); err != nil {
		return e.fail(err)
	}
	return code
}
