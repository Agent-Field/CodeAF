package lease

import "github.com/Agent-Field/codeaf/internal/factory"

type policyStep struct {
	index int
	name  string
	round int
}

// policyAdmittedSteps copies the service's saved phase identities before a
// report can change them. Worker timestamps never grant admission.
func policyAdmittedSteps(it factory.Item) map[policyStep]bool {
	var steps map[policyStep]bool
	if it.Group != "" && it.Stream != nil {
		for i, ph := range it.Stream.Phases {
			if ph.State == factory.PhaseRunning {
				if steps == nil {
					steps = make(map[policyStep]bool)
				}
				steps[policyStep{index: i, name: ph.Name, round: ph.Round}] = true
			}
		}
	}
	return steps
}

// policyNewStep recognizes a phase or round the service has not admitted yet.
// Cost, progress and completion reports of an admitted step may finish it.
func policyNewStep(it factory.Item, admitted map[policyStep]bool) bool {
	if it.Stream != nil {
		for i, ph := range it.Stream.Phases {
			if ph.State == factory.PhaseRunning && !admitted[policyStep{index: i, name: ph.Name, round: ph.Round}] {
				return true
			}
		}
	}
	return false
}
