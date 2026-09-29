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

// Refines opens the tag naming the distant or eventual clause a near or soon
// clause refines, such as "refines H.vision.1".
const Refines = "refines"

// refinesTarget returns the ID text of a refines tag.
func refinesTarget(tag string) (string, bool) {
	rest, ok := strings.CutPrefix(tag, Refines)
	if !ok || rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// RefinesOf returns the IDs the refines tags of a clause name, in tag order.
func RefinesOf(c clause.Clause) []clause.ID {
	var out []clause.ID
	for _, t := range c.Tags {
		if target, ok := refinesTarget(t); ok {
			if id, err := clause.ParseID(target); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// parentTier reports whether a tier is one a refines tag may name.
func parentTier(tier string) bool {
	return tier == "distant" || tier == "eventual"
}

// Validate checks tags and citations across the set.
func Validate(s *Set, r *Resolver) []clause.Problem {
	var problems []clause.Problem
	add := func(c clause.Clause, format string, args ...any) {
		problems = append(problems, clause.Problem{File: c.File, Line: c.Line, Msg: fmt.Sprintf(format, args...)})
	}

	tierOf := map[clause.ID]string{}
	for _, e := range Trace(s) {
		tierOf[e.Clause.ID] = e.Tier
	}
	for _, c := range s.Clauses(clause.Horizon) {
		if !c.Tagged {
			add(c, "%s has no tier; start it with (near), (soon), (distant) or (eventual)", c.ID)
			continue
		}
		var tiers, refines []string
		for _, t := range c.Tags {
			if target, ok := refinesTarget(t); ok {
				refines = append(refines, target)
				continue
			}
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
		switch {
		case len(refines) == 0:
		case len(tiers) == 1 && parentTier(tiers[0]):
			add(c, "%s is %s and cannot refine another clause", c.ID, tiers[0])
		case len(refines) > 1:
			add(c, "%s has more than one refines tag", c.ID)
		default:
			id, err := clause.ParseID(refines[0])
			switch {
			case err != nil:
				add(c, "%s: %v", c.ID, err)
			case id.Kind != clause.Horizon:
				add(c, "%s refines %s, which is not a horizon clause", c.ID, id)
			default:
				if _, ok := s.Lookup(id); !ok {
					add(c, "%s refines %s, which is not in the horizon", c.ID, id)
				} else if tier := tierOf[id]; !parentTier(tier) {
					add(c, "%s refines %s, which is %s, not distant or eventual", c.ID, id, tier)
				}
			}
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

// TraceEntry is a horizon clause, the spec clauses that advance it and its
// refinement links.
type TraceEntry struct {
	Clause     clause.Clause
	Tier       string
	Realised   bool
	AdvancedBy []clause.ID
	// Refines is the clause this one refines, or the zero ID.
	Refines clause.ID
	// RefinesTier is the tier of the clause this one refines, or "".
	RefinesTier string
	// RefinedBy holds the clauses that refine this one, in document order.
	RefinedBy []clause.ID
}

// Trace returns every horizon clause, in document order, with the spec
// clauses that advance it and its refinement links (S.horizon.7).
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
	refinedBy := map[clause.ID][]clause.ID{}
	for _, c := range s.Clauses(clause.Horizon) {
		e := TraceEntry{Clause: c, AdvancedBy: advancedBy[c.ID]}
		for _, t := range c.Tags {
			if target, ok := refinesTarget(t); ok {
				if id, err := clause.ParseID(target); err == nil && e.Refines == (clause.ID{}) {
					e.Refines = id
					refinedBy[id] = append(refinedBy[id], c.ID)
				}
			} else if t == Realised {
				e.Realised = true
			} else if slices.Contains(Tiers, t) {
				e.Tier = t
			}
		}
		out = append(out, e)
	}
	tiers := map[clause.ID]string{}
	for _, e := range out {
		tiers[e.Clause.ID] = e.Tier
	}
	for i := range out {
		out[i].RefinedBy = refinedBy[out[i].Clause.ID]
		if out[i].Refines != (clause.ID{}) {
			out[i].RefinesTier = tiers[out[i].Refines]
		}
	}
	return out
}

// tier returns a horizon clause's tier, or "" if it has none.
func tier(c clause.Clause) string {
	for _, t := range c.Tags {
		if slices.Contains(Tiers, t) {
			return t
		}
	}
	return ""
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
