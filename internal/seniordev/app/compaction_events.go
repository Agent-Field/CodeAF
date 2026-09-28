//go:build !windows

package app

import (
	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
)

var seniorDevCompactionDecisionEvent = bus.Define(
	"session.compaction.decision", compaction.CompactionDecision{},
)

// seniorDevCompactionDecisionSink hears every compaction the run makes: it
// publishes the decision on the instance bus, as it always has, and reports it
// as the `compaction` stage, so codeaf's page can say the run compacted its
// memory and whether its model summarized it or the deterministic record stood
// in.
//
// THE STAGE IS A REPORT OF A DECISION ALREADY MADE. The sink is told after the
// history has been rewritten; nothing the model sees and nothing about when or
// how the run compacts depends on it.
type seniorDevCompactionDecisionSink struct {
	bus    *bus.Bus
	events *eventWriter
}

func newSeniorDevCompactionDecisionSink(instance *bus.Bus, events *eventWriter) compaction.DecisionSink {
	if instance == nil && events == nil {
		return nil
	}
	return seniorDevCompactionDecisionSink{bus: instance, events: events}
}

func (sink seniorDevCompactionDecisionSink) CompactionDecision(
	decision compaction.CompactionDecision,
) {
	if sink.bus != nil {
		sink.bus.Publish(seniorDevCompactionDecisionEvent, decision)
	}
	if sink.events != nil {
		sink.events.stage("compaction", compactionStatus(decision.SummaryStatus), map[string]any{
			"summary_status": decision.SummaryStatus,
			"before_tokens":  decision.Before, "after_tokens": decision.After,
		})
	}
}

// compactionStatus is the compaction stage's status: `summarized` when the
// model's summary stands in the history (valid, or normalized into shape), and
// `fallback` for every other ending, each of which installs the deterministic
// record in its place (compaction's CompactionDecision.SummaryStatus).
func compactionStatus(summary string) string {
	switch summary {
	case "valid", "normalized":
		return "summarized"
	}
	return "fallback"
}

var _ compaction.DecisionSink = seniorDevCompactionDecisionSink{}
