package docs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/clause"
)

// Tiers are a horizon clause's distance from the spec, nearest first.
var Tiers = []string{"near", "soon", "distant", "eventual"}

// Realised marks a horizon clause the spec fully satisfies.
const Realised = "realised"

// Validate checks tags and citations across the set.
func Validate(s *Set, r *Resolver) []clause.Problem {
	var problems []clause.Problem
	add := func(c clause.Clause, format string, args ...any) {
		problems = append(problems, clause.Problem{File: c.File, Line: c.Line, Msg: fmt.Sprintf(format, args...)})
	}

	for _, c := range s.Clauses(clause.Horizon) {
		if !c.Tagged {
			add(c, "%s has no tier; start it with (near), (soon), (distant) or (eventual)", c.ID)
			continue
		}
		var tiers []string
		for _, t := range c.Tags {
			switch {
			case slices.Contains(Tiers, t):
				tiers = append(tiers, t)
			case t == Realised:
			default:
				add(c, "%s has unknown tag %q", c.ID, t)
			}
		}
		if len(tiers) != 1 {
			add(c, "%s must have exactly one tier, has %d", c.ID, len(tiers))
		}
	}

	for _, c := range s.Clauses(clause.Spec) {
		if !c.Tagged || len(c.Tags) == 0 {
			add(c, "%s names no horizon clause; start it with the horizon clauses it advances, such as (H.area.1)", c.ID)
			continue
		}
		for _, t := range c.Tags {
			id, err := clause.ParseID(t)
			switch {
			case err != nil:
				add(c, "%s: %v", c.ID, err)
			case id.Kind != clause.Horizon:
				add(c, "%s advances %s, which is not a horizon clause", c.ID, id)
			default:
				if _, ok := s.Lookup(id); !ok {
					add(c, "%s advances %s, which is not in the horizon", c.ID, id)
				}
			}
		}
	}

	for _, e := range Trace(s) {
		if e.Realised && len(e.AdvancedBy) == 0 {
			add(e.Clause, "%s is marked realised but no spec clause advances it", e.Clause.ID)
		}
	}

	for _, doc := range s.Documents() {
		for _, m := range doc.Mentions {
			if err := resolveMention(r, m); err != nil {
				problems = append(problems, clause.Problem{File: m.File, Line: m.Line, Msg: err.Error()})
			}
		}
	}
	return problems
}

func resolveMention(r *Resolver, m clause.Mention) error {
	from, err := clause.ParseCitation(m.Token)
	if err != nil {
		return err
	}
	if m.To == "" {
		_, err = r.Resolve(from)
		return err
	}
	to, err := clause.ParseCitation(m.To)
	if err != nil {
		return err
	}
	_, err = r.ResolveRange(from, to)
	return err
}

// TraceEntry is a horizon clause and the spec clauses that advance it.
type TraceEntry struct {
	Clause     clause.Clause
	Tier       string
	Realised   bool
	AdvancedBy []clause.ID
}

// Trace returns every horizon clause, in ID order, with the spec clauses that
// advance it.
func Trace(s *Set) []TraceEntry {
	advancedBy := map[clause.ID][]clause.ID{}
	for _, c := range s.Clauses(clause.Spec) {
		for _, t := range c.Tags {
			if id, err := clause.ParseID(t); err == nil {
				advancedBy[id] = append(advancedBy[id], c.ID)
			}
		}
	}
	var out []TraceEntry
	for _, c := range s.Clauses(clause.Horizon) {
		e := TraceEntry{Clause: c, AdvancedBy: advancedBy[c.ID]}
		for _, t := range c.Tags {
			if t == Realised {
				e.Realised = true
			} else if slices.Contains(Tiers, t) {
				e.Tier = t
			}
		}
		out = append(out, e)
	}
	return out
}

// Gap returns the horizon clauses that are not realised.
func Gap(s *Set) []TraceEntry {
	var out []TraceEntry
	for _, e := range Trace(s) {
		if !e.Realised {
			out = append(out, e)
		}
	}
	return out
}

// JoinIDs formats IDs as a comma-separated list.
func JoinIDs(ids []clause.ID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.String()
	}
	return strings.Join(parts, ", ")
}
