package scheduler

import (
	"context"
	"errors"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/loopguard"
)

const maxConsecutiveEmptyToolTurns = 3

type leafTurnMonitor struct {
	mu            sync.Mutex
	guard         loopguard.LoopGuard
	observed      bool
	emptyToolRuns int
	stopReason    string
}

func newLeafTurnMonitor(guard loopguard.LoopGuard) *leafTurnMonitor {
	return &leafTurnMonitor{guard: guard}
}

func (monitor *leafTurnMonitor) Observe(
	_ context.Context, turn LeafTurnObservation,
) error {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	monitor.observed = true
	if monitor.stopReason != "" {
		return errors.New(monitor.stopReason)
	}
	if monitor.guard != nil {
		for _, part := range turn.Parts {
			if part.Type != "tool" {
				continue
			}
			if monitor.stop(monitor.guard.Observe(loopguard.LoopAction{
				Tool: part.Tool, ArgsKey: part.ArgsKey,
			})) {
				return errors.New(monitor.stopReason)
			}
		}
		if monitor.stop(monitor.guard.ObserveCost(&turn.CostUSD)) {
			return errors.New(monitor.stopReason)
		}
	}
	if turn.Finish == "tool-calls" && len(turn.Parts) == 0 {
		monitor.emptyToolRuns++
	} else {
		monitor.emptyToolRuns = 0
	}
	if monitor.emptyToolRuns >= maxConsecutiveEmptyToolTurns {
		monitor.stopReason = "provider protocol made no progress after 3 consecutive tool-call turns with zero tool calls"
		return errors.New(monitor.stopReason)
	}
	return nil
}

func (monitor *leafTurnMonitor) stop(verdict loopguard.LoopVerdict) bool {
	if verdict.Status != loopguard.LoopStatusStop {
		return false
	}
	monitor.stopReason = "loop guard stopped leaf"
	if verdict.Reason != nil {
		monitor.stopReason = *verdict.Reason
	}
	return true
}

func (monitor *leafTurnMonitor) Observed() bool {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	return monitor.observed
}

func (monitor *leafTurnMonitor) StopReason() string {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	return monitor.stopReason
}
