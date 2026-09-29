// Package bundle assembles the context a session receives. A bundle comes
// from a context provider; the default provider builds it from the documents,
// the unit's record in the tracker and the debate record, and needs nothing
// outside the repository and the state directory.
package bundle

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/docs"
	"github.com/kpenfound/shed/internal/proof"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// Section is one titled part of a bundle.
type Section struct {
	Title string
	Body  string
}

// Bundle is the context of one session.
type Bundle struct {
	Sections []Section
}

// Render writes the bundle as Markdown.
func (b Bundle) Render() string {
	var s strings.Builder
	s.WriteString("# Bundle\n")
	for _, sec := range b.Sections {
		fmt.Fprintf(&s, "\n## %s\n\n%s\n", sec.Title, strings.TrimRight(sec.Body, "\n"))
	}
	return s.String()
}

// Has reports whether the bundle has a section with a title.
func (b Bundle) Has(title string) bool {
	return slices.ContainsFunc(b.Sections, func(s Section) bool { return s.Title == title })
}

// Request is what a bundle is built for.
type Request struct {
	Role unit.Actor
	Unit tracker.Unit
	// Main and Head are the documents on main and on the unit's change.
	Main *docs.Set
	Head *docs.Set
	// Proofs are the proofs on the unit's change.
	Proofs []proof.Proof
	// Debate is the unit's debate record, rendered.
	Debate string
	// Answers are the owner's answers to the unit, oldest first.
	Answers []tracker.Answer
	// Notices are the pending notices for the role about the unit.
	Notices []tracker.Notice
	// Extra are sections the caller adds, such as the gap for a painter.
	Extra []Section
}

// Provider supplies bundles.
type Provider interface {
	Bundle(ctx context.Context, req Request) (Bundle, error)
}

// Section titles.
const (
	UnitSection      = "Unit"
	CharterSection   = "Charter"
	FootprintSection = "Footprint"
	ChangesSection   = "Spec changes"
	SealedSection    = "Sealed spec"
	HorizonSection   = "Horizon clauses advanced"
	ProofsSection    = "Proofs"
	DebateSection    = "Debate record"
	AnswersSection   = "Owner's answers"
	NoticesSection   = "Notices"
)

// Files is the default provider. It needs only the documents and the
// tracker.
type Files struct{}

// Bundle builds the unit, charter, footprint, spec changes, sealed spec,
// horizon, proofs, debate record, owner's answers and notices sections. The sweeper's bundle
// carries no debate record.
func (Files) Bundle(_ context.Context, req Request) (Bundle, error) {
	var b Bundle
	add := func(title, body string) {
		if strings.TrimSpace(body) != "" {
			b.Sections = append(b.Sections, Section{Title: title, Body: body})
		}
	}
	u := req.Unit
	if u.Change != "" {
		var s strings.Builder
		fmt.Fprintf(&s, "- Title: %s\n- Change: %s\n- State: %s\n- Bounces: %d\n- Amendments: %d\n",
			u.Title, u.Change, u.State, u.Bounces, u.Amendments)
		if u.Seal != nil {
			fmt.Fprintf(&s, "- Sealed against main commit %s\n", u.Seal.Main)
		}
		if u.Reason != "" {
			fmt.Fprintf(&s, "- Latest reason: %s\n", u.Reason)
		}
		add(UnitSection, s.String())
	}
	if req.Main != nil {
		add(CharterSection, list(req.Main.Clauses(clause.Charter)))
	}
	fp := u.Footprint
	if len(fp.Modifies)+len(fp.Depends)+len(fp.Advances) > 0 {
		var s strings.Builder
		for _, r := range []struct {
			label string
			ids   []string
		}{{"Modifies", fp.Modifies}, {"Depends on", fp.Depends}, {"Advances", fp.Advances}} {
			if len(r.ids) > 0 {
				fmt.Fprintf(&s, "- %s: %s\n", r.label, strings.Join(r.ids, ", "))
			}
		}
		add(FootprintSection, s.String())
	}
	if req.Main != nil && req.Head != nil {
		add(ChangesSection, Changes(req.Main, req.Head))
	}
	if req.Head != nil {
		var sealed []clause.Clause
		for _, s := range append(slices.Clone(fp.Modifies), fp.Depends...) {
			if id, err := clause.ParseID(s); err == nil {
				if c, ok := req.Head.Lookup(id); ok && !slices.ContainsFunc(sealed, func(x clause.Clause) bool { return x.ID == id }) {
					sealed = append(sealed, c)
				}
			}
		}
		add(SealedSection, list(sealed))
		var advanced []clause.Clause
		for _, s := range fp.Advances {
			if id, err := clause.ParseID(s); err == nil {
				if c, ok := req.Head.Lookup(id); ok {
					advanced = append(advanced, c)
				}
			}
		}
		add(HorizonSection, list(advanced))
	}
	if len(req.Proofs) > 0 {
		var s strings.Builder
		for _, id := range append(slices.Clone(fp.Modifies), fp.Depends...) {
			parsed, err := clause.ParseID(id)
			if err != nil {
				continue
			}
			var names []string
			for _, p := range proof.For(req.Proofs, parsed) {
				names = append(names, p.Name()+" ("+p.File+")")
			}
			if len(names) == 0 {
				names = []string{"no proof yet"}
			}
			fmt.Fprintf(&s, "- %s: %s\n", id, strings.Join(names, ", "))
		}
		add(ProofsSection, s.String())
	}
	if req.Role != unit.Sweeper {
		add(DebateSection, req.Debate)
	}
	if len(req.Answers) > 0 {
		var s strings.Builder
		for _, a := range req.Answers {
			fmt.Fprintf(&s, "- %s\n", a)
		}
		add(AnswersSection, s.String())
	}
	if len(req.Notices) > 0 {
		var s strings.Builder
		for _, n := range req.Notices {
			fmt.Fprintf(&s, "- %s: %s\n", n.Kind, n.Body)
		}
		add(NoticesSection, s.String())
	}
	for _, sec := range req.Extra {
		add(sec.Title, sec.Body)
	}
	return b, nil
}

// Changes describes the spec diff from main to a unit's change.
func Changes(main, head *docs.Set) string {
	d := docs.DiffSpec(main, head)
	var s strings.Builder
	for _, id := range d.Added {
		c, _ := head.Lookup(id)
		fmt.Fprintf(&s, "- Added %s\n", describe(c))
	}
	for _, id := range d.Changed {
		was, _ := main.Lookup(id)
		now, _ := head.Lookup(id)
		fmt.Fprintf(&s, "- Changed %s\n  Was: %s\n", describe(now), body(was))
	}
	for _, id := range d.Removed {
		c, _ := main.Lookup(id)
		fmt.Fprintf(&s, "- Removed %s\n", describe(c))
	}
	return s.String()
}

// Amendment describes what an amendment changed (S.shed.13): the spec
// clauses whose text differs between the unit's commit at its earlier seal,
// was, and at its new seal, now. A clause is left out when, on each unit
// commit, it reads as on the main commit sealed with it, wasMain and
// nowMain, since main changed it and the amendment did not. It is empty when
// the amendment changed no clause.
func Amendment(was, now, wasMain, nowMain *docs.Set) string {
	var s strings.Builder
	for _, id := range specIDs(was, now) {
		a, inWas := was.Lookup(id)
		b, inNow := now.Lookup(id)
		if same(a, inWas, b, inNow) || (sameAs(was, wasMain, id) && sameAs(now, nowMain, id)) {
			continue
		}
		switch {
		case !inWas:
			fmt.Fprintf(&s, "- %s was added: %s\n", id, body(b))
		case !inNow:
			fmt.Fprintf(&s, "- %s was removed. It said: %s\n", id, body(a))
		default:
			fmt.Fprintf(&s, "- %s changed.\n  Was: %s\n  Now: %s\n", id, body(a), body(b))
		}
	}
	return s.String()
}

// specIDs lists the spec clauses of either set, in order.
func specIDs(a, b *docs.Set) []clause.ID {
	var ids []clause.ID
	for _, set := range []*docs.Set{a, b} {
		for _, c := range set.Clauses(clause.Spec) {
			if !slices.Contains(ids, c.ID) {
				ids = append(ids, c.ID)
			}
		}
	}
	slices.SortFunc(ids, clause.Compare)
	return ids
}

// sameAs reports whether a spec clause reads the same in two sets, a clause
// absent from both counting as the same.
func sameAs(a, b *docs.Set, id clause.ID) bool {
	x, inA := a.Lookup(id)
	y, inB := b.Lookup(id)
	return same(x, inA, y, inB)
}

func same(a clause.Clause, inA bool, b clause.Clause, inB bool) bool {
	if !inA || !inB {
		return inA == inB
	}
	return a.Text == b.Text && slices.Equal(a.Tags, b.Tags)
}

func list(clauses []clause.Clause) string {
	var s strings.Builder
	for _, c := range clauses {
		fmt.Fprintf(&s, "- %s\n", describe(c))
	}
	return s.String()
}

func describe(c clause.Clause) string { return c.ID.String() + " " + body(c) }

func body(c clause.Clause) string {
	if c.Tagged {
		return "(" + strings.Join(c.Tags, ", ") + ") " + c.Text
	}
	return c.Text
}
