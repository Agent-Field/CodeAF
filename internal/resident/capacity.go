package resident

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

const capacityPriorSamples = 8

// ShrinkOverrunRate applies the journal's shared n/(n+8) empirical-Bayes
// weight to a local overrun rate. The global population supplies the prior;
// without local observations it is the only honest estimate.
func ShrinkOverrunRate(local, global float64, samples int) float64 {
	if samples <= 0 {
		return global
	}
	weight := float64(samples) / float64(samples+capacityPriorSamples)
	return weight*local + (1-weight)*global
}

type capacityCount struct {
	runs     int
	overruns int
}

// CapacityOptions folds measured leaf capacity into planner options. Swarm is
// an argument, rather than a promise left to the caller, so the disabled path
// returns the input unchanged without even reading the journal. A missing or
// unreadable history does the same: capacity is evidence, never a prerequisite
// that can break planning.
//
// The journal cheaply identifies execution model on usage events. It records
// no workspace identity, so model is the narrowest honest segment; inventing a
// workspace join would turn process state into supposed historical evidence.
func CapacityOptions(journal *store.Store, swarm bool, model string, options plan.Options) plan.Options {
	if !swarm || journal == nil {
		return options
	}
	evidence, ok := measuredCapacity(journal, model)
	if !ok {
		return options
	}
	options.CapacitySamples = evidence.Runs
	options.CapacityOverrunRate = evidence.Rate
	options.Invoice = plan.AppendCapacityEvidence(options.Invoice, evidence)
	return options
}

func measuredCapacity(journal *store.Store, model string) (plan.CapacityEvidence, bool) {
	events, err := journal.Events(0, 0)
	if err != nil {
		return plan.CapacityEvidence{}, false
	}
	settled := make(map[string]bool)
	models := make(map[string]string)
	overran := make(map[string]bool)
	for _, event := range events {
		switch event.Kind {
		case store.EventNodeCompleted, store.EventNodeFailed:
			settled[event.NodeID] = true
		case store.EventUsageRecorded:
			var usage struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(event.Payload, &usage) == nil && strings.TrimSpace(usage.Model) != "" {
				models[event.NodeID] = strings.TrimSpace(usage.Model)
			}
		case store.EventJobGrowth:
			var growth store.JobGrowth
			if json.Unmarshal(event.Payload, &growth) == nil && growth.Reason == GrowOverrun {
				overran[strings.TrimSpace(growth.Lineage)] = true
			}
		case store.EventOverrunDeferred:
			overran[event.NodeID] = true
		case store.EventPlanGraph:
			// A plan revision sometimes journals a leaf after it has landed. Read
			// that direct settlement evidence too; growth remains necessary for
			// the ordinary final sink, whose in-memory graph is retired at landing.
			var recorded store.PlanGraph
			if json.Unmarshal(event.Payload, &recorded) != nil {
				continue
			}
			graph, loadErr := plan.Load(recorded.Graph)
			if loadErr != nil || len(graph.Nodes) == 0 {
				continue
			}
			lastID := graph.Nodes[len(graph.Nodes)-1].ID
			for _, node := range graph.Nodes {
				if node.Stop != "budget" && node.Stop != "turn-cap" &&
					string(node.Verdict) != "budget_stop" && string(node.Verdict) != "turn_cap" {
					continue
				}
				nodeID := event.NodeID + "-n" + fmt.Sprint(node.ID)
				if node.ID == lastID {
					nodeID = recorded.Root
				}
				overran[nodeID] = true
			}
		}
	}

	byModel := make(map[string]capacityCount)
	var global capacityCount
	for nodeID, servedBy := range models {
		if !settled[nodeID] {
			continue
		}
		count := byModel[servedBy]
		count.runs++
		global.runs++
		if overran[nodeID] {
			count.overruns++
			global.overruns++
		}
		byModel[servedBy] = count
	}
	model = strings.TrimSpace(model)
	local := byModel[model]
	if model == "" {
		local = global
	}
	if local.runs == 0 || global.runs == 0 {
		return plan.CapacityEvidence{}, false
	}
	localRate := float64(local.overruns) / float64(local.runs)
	globalRate := float64(global.overruns) / float64(global.runs)
	return plan.CapacityEvidence{
		Worker: model, Runs: local.runs, Overruns: local.overruns,
		Rate: ShrinkOverrunRate(localRate, globalRate, local.runs),
	}, true
}
