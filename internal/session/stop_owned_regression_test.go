package session

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestReviewStopCancelsOwnedManagerBackgroundWork(t *testing.T) {
	var stopped atomic.Bool
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.JobBudget = &liveBudgetStub{cap: 10}
		c.FactoryWorkRefusal = func() string {
			if stopped.Load() {
				return factory.PersonStopReason
			}
			return ""
		}
	})
	id := startJob(t, a, "sleep 30")
	stopped.Store(true)
	if a.automaticFactoryRefusal() == "" {
		t.Fatal("the durable Stop was not visible to the manager")
	}
	// The existing budget watcher polls every 100 ms. Give it three polls.
	time.Sleep(350 * time.Millisecond)
	if a.jobs.find(id).running() {
		t.Fatal("person Stop is visible, but the manager's owned background command still runs")
	}
}
