package run

import "github.com/Agent-Field/codeaf/internal/factory"

// asksBeforeStep is Manual's hold before a step's first round, as the runner
// of this age applied it. The factory itself dropped the rule (the owner's
// decision of 2026-11-17, internal/factory/run's runPhase); this package is
// the old runner the service's tests still meet, so it keeps its own copy.
func asksBeforeStep(it factory.Item, i int) bool {
	if it.Autonomy != factory.AutonomyManual || it.Stream == nil || i < 0 || i >= len(it.Stream.Phases) {
		return false
	}
	ph := it.Stream.Phases
	p := ph[i]
	if p.Kind == factory.StageGate || p.State != factory.PhasePending || p.Round > 0 || it.FollowUp != nil || p.Name == factory.FollowPRStep {
		return false
	}
	if factory.PreviousWork(ph, i) < 0 {
		return false
	}
	// The step before is an approve step that asked: its yes was the word.
	for k := i - 1; k >= 0; k-- {
		q := ph[k]
		if q.Kind != factory.StageGate {
			return true
		}
		if j := factory.StageIndex(it.Stages, q.Name); j < 0 || it.Stages[j].On {
			return q.State != factory.PhaseDone
		}
	}
	return true
}
