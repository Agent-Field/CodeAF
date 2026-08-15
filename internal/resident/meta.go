package resident

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

const (
	// MetaRetrospectiveEvery runs the bounded audit on every fifth reflection.
	MetaRetrospectiveEvery = 5
	// MetaReversalMinSamples keeps tuning inert until an action class has evidence.
	MetaReversalMinSamples = 6
	// MetaReversalEaseBelow makes a proven-safe action class one notch easier.
	MetaReversalEaseBelow = 0.10
	// MetaReversalTightenAbove makes a frequently reversed action class one notch stricter.
	MetaReversalTightenAbove = 0.35
)

type auditDial struct {
	class, parameter, label     string
	lowDirection, highDirection int
}

var auditRegistry = []auditDial{
	{"skill_promotions", store.ParameterSkillPromotionOccurrences, "skill promotion", -1, 1},
	{"consolidations", store.ParameterConsolidationThreshold, "belief consolidation", -1, 1},
	{"aging", store.ParameterBeliefRetentionThreshold, "belief aging", 1, -1},
	{"proposals", store.ParameterProposalCadenceRuns, "proposal cadence", -1, 1},
}

func (r *Reconciler) metaRetrospect() []store.ParameterChange {
	runs, err := r.store.RetrospectiveRuns()
	if err != nil || runs == 0 || runs%MetaRetrospectiveEvery != 0 {
		return nil
	}
	rates, err := r.store.MetaReversalRates()
	if err != nil {
		return nil
	}
	var changes []store.ParameterChange
	for _, dial := range auditRegistry {
		rate := rates[dial.class]
		if rate.Total < MetaReversalMinSamples {
			continue
		}
		direction, verb := 0, ""
		switch {
		case rate.Rate < MetaReversalEaseBelow:
			direction, verb = dial.lowDirection, "eased"
		case rate.Rate > MetaReversalTightenAbove:
			direction, verb = dial.highDirection, "tightened"
		default:
			continue
		}
		phrase := fmt.Sprintf("%s %s (%d/%d reversals)", verb, dial.label, rate.Reversals, rate.Total)
		change, changed, err := r.store.TuneParameter(dial.parameter, direction,
			store.ParameterEvidence{Reversals: rate.Reversals, Total: rate.Total, Rate: rate.Rate}, phrase)
		if err == nil && changed {
			changes = append(changes, change)
		}
	}
	return changes
}
