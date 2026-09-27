package docs

import (
	"fmt"

	"github.com/kpenfound/shed/internal/clause"
	"github.com/kpenfound/shed/internal/revision"
)

// Resolver resolves citations against the working tree or a git revision.
type Resolver struct {
	Root string
	Work *Set
	revs map[string]*Set
}

// NewResolver resolves citations without a revision against work, and the
// rest against the git repository at root.
func NewResolver(root string, work *Set) *Resolver {
	return &Resolver{Root: root, Work: work, revs: map[string]*Set{}}
}

func (r *Resolver) set(rev string) (*Set, error) {
	if rev == "" {
		return r.Work, nil
	}
	commit, err := revision.Resolve(r.Root, rev)
	if err != nil {
		return nil, err
	}
	if s, ok := r.revs[commit]; ok {
		return s, nil
	}
	s, _ := Load(revision.Git{Root: r.Root, Rev: commit})
	r.revs[commit] = s
	return s, nil
}

// Resolve returns the clause a citation names.
func (r *Resolver) Resolve(c clause.Citation) (clause.Clause, error) {
	s, err := r.set(c.Rev)
	if err != nil {
		return clause.Clause{}, fmt.Errorf("%s does not resolve: %w", c, err)
	}
	found, ok := s.Lookup(c.ID)
	if !ok {
		return clause.Clause{}, fmt.Errorf("%s does not resolve: no clause %s in the %s", c, c.ID, where(c.Rev))
	}
	return found, nil
}

// ResolveRange returns the clauses from one citation to another. Both ends
// must resolve, share a series and revision, and be in order.
func (r *Resolver) ResolveRange(from, to clause.Citation) ([]clause.Clause, error) {
	if !clause.SameSeries(from.ID, to.ID) || from.Rev != to.Rev {
		return nil, fmt.Errorf("range %s to %s spans two series", from, to)
	}
	if from.ID.N >= to.ID.N {
		return nil, fmt.Errorf("range %s to %s is out of order", from, to)
	}
	for _, c := range []clause.Citation{from, to} {
		if _, err := r.Resolve(c); err != nil {
			return nil, err
		}
	}
	s, _ := r.set(from.Rev)
	return s.Series(from.ID, to.ID), nil
}

func where(rev string) string {
	if rev == "" {
		return "working tree"
	}
	return "documents at " + rev
}
