package factory

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kpenfound/shed/internal/bundle"
	"github.com/kpenfound/shed/internal/config"
	"github.com/kpenfound/shed/internal/tracker"
	"github.com/kpenfound/shed/internal/unit"
)

// overrunHelp points a debate bundle's Overrun section to the painter's
// right to declare a revised estimate (S.impl.6), recorded at the unit's
// next seal (S.impl.7), and a member's right to object that the footprint
// is too large and ask for a split instead (S.shed.7).
const overrunHelp = "The painter may declare a revised estimate under S.impl.6, which this unit's next seal records under S.impl.7. A member may instead object that the footprint is too large and ask for a split under S.shed.7.\n"

// overrunSection builds the Overrun section of a committee or painter debate
// bundle (S.impl.10). It applies only to a unit whose latest reopen is the
// overrun reopen of S.impl.9: the latest move to proposed, found from the
// event log alone, that carries a footprint, since only that reopen records
// one, to clear the estimate it reopened with. The section is built from the
// seal that actually set the estimate still recorded (S.impl.7, S.impl.8) —
// found the same way the tracker finds it, walking the log forward so a seal
// out of the amendment lane that carries forward an already-positive
// estimate does not move the boundary — and the sessions that finished
// between that seal and the reopen alone; it never reads the reopen's
// reason. It reports false for a unit with no such reopen.
func (f *Factory) overrunSection(change string) (bundle.Section, bool, error) {
	events, err := f.Tracker.Events(change)
	if err != nil {
		return bundle.Section{}, false, err
	}
	reopen := -1
	for i := len(events) - 1; i >= 0; i-- {
		if e := events[i]; e.Kind == tracker.UnitMoved && e.To == unit.Proposed {
			reopen = i
			break
		}
	}
	if reopen < 0 || events[reopen].Footprint == nil {
		return bundle.Section{}, false, nil
	}
	seal := -1
	inLane := false
	estimate := 0.0
	for i := 0; i < reopen; i++ {
		e := events[i]
		if e.Kind == tracker.UnitMoved && e.To == unit.Proposed && e.Bounce {
			inLane = e.Amendment
		}
		if e.Kind == tracker.UnitMoved && e.To == unit.Sealed && e.Seal != nil {
			if carriedForward := inLane && estimate > 0; !carriedForward {
				seal = i
			}
			if e.Footprint != nil {
				estimate = e.Footprint.Estimate
			}
		}
	}
	if seal < 0 {
		return bundle.Section{}, false, nil
	}

	var sessions []tracker.Event
	var cost float64
	for _, e := range events[seal+1 : reopen] {
		if e.Kind == tracker.SessionFinished {
			sessions = append(sessions, e)
			cost += e.CostUSD
		}
	}

	steps := f.Operator.Formulas[config.DefaultFormula].Steps
	isFormulaStep := func(name string) bool {
		return slices.ContainsFunc(steps, func(s config.Step) bool { return s.Name == name })
	}

	var body strings.Builder
	fmt.Fprintf(&body, "$%.2f of $%.2f\n\n", cost, estimate)
	for _, e := range sessions {
		s := e.Session
		label := string(e.Actor)
		if s.Step != "" {
			label += ", " + s.Step
		}
		fmt.Fprintf(&body, "- %s (%s): %s, $%.2f\n", s.ID, label, s.Outcome, e.CostUSD)
	}

	var order []string
	subtotal := map[string]float64{}
	for _, e := range sessions {
		s := e.Session
		key := s.Step
		if !isFormulaStep(key) {
			key = string(e.Actor)
		}
		if _, ok := subtotal[key]; !ok {
			order = append(order, key)
		}
		subtotal[key] += e.CostUSD
	}
	body.WriteString("\n")
	for _, key := range order {
		fmt.Fprintf(&body, "%s: $%.2f\n", key, subtotal[key])
	}
	body.WriteString("\n" + overrunHelp)

	return bundle.Section{Title: bundle.OverrunSection, Body: body.String()}, true, nil
}
