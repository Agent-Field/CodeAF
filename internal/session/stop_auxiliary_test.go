package session

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type stoppedMemoryRetryProbe struct {
	calls atomic.Int64
	first chan struct{}
}

func (p *stoppedMemoryRetryProbe) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	if p.calls.Add(1) == 1 {
		close(p.first)
		return nil, context.DeadlineExceeded
	}
	return textResponse(`{"keep":""}`), nil
}

func TestConversationStopPreventsBackgroundMemoryRetryAndRestoresOnlyForFreshInput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		memory, stopMemory := context.WithCancel(context.Background())
		title, stopTitle := context.WithCancel(context.Background())
		t.Cleanup(stopMemory)
		t.Cleanup(stopTitle)
		agent := &Agent{memoryCtx: memory, memoryStop: stopMemory, titleCtx: title, titleStop: stopTitle}
		oldMemory, oldTitle := agent.memoryCtx, agent.titleCtx
		probe := &stoppedMemoryRetryProbe{first: make(chan struct{})}
		if !agent.startMemoryJob() {
			t.Fatal("memory job not admitted")
		}
		go func() {
			defer agent.memoryJobs.Done()
			_, _ = agent.noticeWithRecovery(probe, "mock preference", "okay", nil, true)
		}()
		<-probe.first
		if err := agent.StopWork(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		if got := probe.calls.Load(); got != 1 {
			t.Fatalf("memory started %d model calls after Stop; want first call only", got)
		}
		if oldMemory.Err() == nil || oldTitle.Err() == nil {
			t.Fatal("stopped auxiliary lifetimes remain able to retry")
		}
		if agent.startMemoryJob() {
			agent.memoryJobs.Done()
			t.Fatal("new memory job admitted while stopped")
		}
		agent.mu.Lock()
		if err := agent.resumeWorkLocked(); err != nil {
			agent.mu.Unlock()
			t.Fatal(err)
		}
		resumedMemory, resumedTitle := agent.memoryCtx, agent.titleCtx
		agent.mu.Unlock()
		if resumedMemory == oldMemory || resumedTitle == oldTitle || resumedMemory.Err() != nil || resumedTitle.Err() != nil {
			t.Fatal("fresh input did not get fresh auxiliary lifetimes")
		}
	})
}

func TestConversationStopRejectsLateTeamHandleAttempt(t *testing.T) {
	agent := &Agent{workStopped: true, config: Config{ProfileDir: t.TempDir(), SessionFile: "mock-session.jsonl"}}
	agent.chooseTeamHandlesLater("Mock project", []teamRole{{derived: true}})
	agent.team.mu.Lock()
	attempted := agent.team.handleTried
	agent.team.mu.Unlock()
	if attempted {
		t.Fatal("stopped conversation admitted a new auxiliary handle attempt")
	}
}
