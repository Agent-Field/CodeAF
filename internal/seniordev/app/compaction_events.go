//go:build !windows

package app

import (
	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/compaction"
)

var seniorDevCompactionDecisionEvent = bus.Define(
	"session.compaction.decision", compaction.CompactionDecision{},
)

type seniorDevCompactionDecisionSink struct{ bus *bus.Bus }

func newSeniorDevCompactionDecisionSink(instance *bus.Bus) compaction.DecisionSink {
	if instance == nil {
		return nil
	}
	return seniorDevCompactionDecisionSink{bus: instance}
}

func (sink seniorDevCompactionDecisionSink) CompactionDecision(
	decision compaction.CompactionDecision,
) {
	sink.bus.Publish(seniorDevCompactionDecisionEvent, decision)
}

var _ compaction.DecisionSink = seniorDevCompactionDecisionSink{}
