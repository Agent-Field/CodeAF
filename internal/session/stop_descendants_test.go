package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestConversationStopCutsItsOwnedDelegatedRun(t *testing.T) {
	agent, engine, _, _ := stoppableBeltRun(t, 91)
	if err := agent.StopWork(); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(250 * time.Millisecond)
	select {
	case <-engine.ctx.Done():
	case <-deadline:
		t.Fatal("conversation Stop returned but its delegated run could still spend")
	}
}

func TestInteractiveConversationWallCutsItsOwnedDelegatedRun(t *testing.T) {
	agent, engine, _, _ := stoppableBeltRun(t, 92)
	lane, unwatch := agent.WatchWakes()
	defer unwatch()
	agent.mu.Lock()
	agent.config.Interactive = true
	agent.config.Budget = Budget{Wall: 20 * time.Millisecond}
	agent.startedAt = time.Now()
	agent.mu.Unlock()
	agent.armWallClock()
	select {
	case <-engine.ctx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("conversation time limit left its delegated run able to spend")
	}
	stream := awaitWallStream(t, lane, time.Second)
	var terminal bool
	for event := range stream {
		if event.Kind == EventError && event.Err != nil && event.Err.Error() == conversationTimeLimitWord {
			terminal = true
		}
	}
	if !terminal {
		t.Fatal("time limit did not publish its actionable terminal error")
	}
	agent.beltMu.Lock()
	live := agent.beltRun != nil
	agent.beltMu.Unlock()
	if live {
		t.Fatal("terminal time-limit error arrived before owned run joined")
	}
	agent.mu.Lock()
	err := agent.resumeWorkLocked()
	agent.mu.Unlock()
	if err == nil || err.Error() != conversationTimeLimitWord {
		t.Fatalf("fresh input revived an expired conversation: %v", err)
	}
}

type stoppingLandingEngine struct {
	*beltRunDouble
	landingEntered chan struct{}
	releaseLanding chan struct{}
}

func (e *stoppingLandingEngine) Land(context.Context, *plandb.Store, string, string, string) (RunLanding, error) {
	close(e.landingEntered)
	<-e.releaseLanding
	return e.landing, nil
}

func TestPageStopDuringLandingRemainsTerminal(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	workspace := beltRunCommittedRepo(t)
	engine := &stoppingLandingEngine{beltRunDouble: newBeltRunDouble("work done"), landingEntered: make(chan struct{}), releaseLanding: make(chan struct{})}
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) { c.Workspace = workspace; c.Place = Place{Dir: t.TempDir()} })
	if err := agent.startKnownTaskRun(context.Background(), 93, "fixture work", "brief", nil, taskStand{dir: workspace, mode: TaskModeWorktree}, ""); err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	close(engine.release)
	<-engine.landingEntered
	if err := agent.PlanCancel("93"); err != nil {
		t.Fatal(err)
	}
	close(engine.releaseLanding)
	beltRunWaitFor(t, "stopped landing to settle", func() bool { agent.beltMu.Lock(); defer agent.beltMu.Unlock(); return agent.beltRun == nil })
	for _, row := range agent.graph().runRows(93) {
		if row.ID == 93 && (!row.Stopped || !strings.Contains(row.Report, "stopped")) {
			t.Fatalf("page Stop was replaced by a normal landing: %+v", row)
		}
	}
}

func TestLegacyWallStopsAStandaloneJobWithoutDelegatedTasks(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Unattended = true
		c.Budget = Budget{Wall: time.Hour}
	})
	id := startJob(t, agent, "sleep 30")
	spendWall(t, agent)
	if !agent.endRunOnTheWall() {
		t.Fatal("expired Steward wall ignored a standalone background job")
	}
	if agent.jobs.find(id).running() {
		t.Fatal("wall ending arrived before the background job joined")
	}
}
