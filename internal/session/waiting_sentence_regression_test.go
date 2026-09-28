package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/subharness"
)

func waitForPersonLane(t *testing.T, agent *Agent, lane func(*Agent) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if lane(agent) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the question lane never became active")
}

func connectLaneActive(agent *Agent) bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return len(agent.connectAsks) > 0
}

func harnessLaneActive(agent *Agent) bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return len(agent.harnessAsks) > 0
}

func TestWaitingSentenceConnectSignIn(t *testing.T) {
	agent, _ := questionSession(t, "sign1111sign1111", nil)
	events := watched(agent)
	go func() {
		for range events {
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// THE FIRST OBSERVATION IS THE ONE THAT USED TO BE EMPTY. The lane and the
	// sentence are published together, so the first read that sees a person is
	// needed already carries the sign-in line. Waiting on the map and then
	// sleeping let that read land in the gap.
	bad := make(chan personAsk, 1)
	saw := make(chan struct{}, 1)
	go func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			waiting := agent.waitingOnPerson()
			if !waiting.waiting {
				continue
			}
			if !strings.Contains(waiting.reason, "connect your Notion account?") {
				bad <- waiting
				return
			}
			saw <- struct{}{}
			return
		}
	}()
	go func() { _, _ = agent.askConnect(ctx, connectStatus{ID: "notion", Name: "Notion"}) }()
	select {
	case waiting := <-bad:
		t.Fatalf("sign-in says a person is needed without the sign-in sentence: waiting=%v reason=%q", waiting.waiting, waiting.reason)
	case <-saw:
	case <-time.After(2 * time.Second):
		t.Fatal("the question lane never became active")
	}
}

func TestWaitingSentenceHarnessAsk(t *testing.T) {
	agent, _ := questionSession(t, "harn1111harn1111", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	route := harnessRoute{Match: subharness.Match{Entry: subharness.Entry{Name: "research", Description: "finds an answer across sources"}}}
	go func() { _, _ = agent.askHarness(ctx, newEventHub(), route) }()
	waitForPersonLane(t, agent, harnessLaneActive)

	waiting := agent.waitingOnPerson()
	if !waiting.waiting || !strings.Contains(waiting.reason, `run harness "research"?`) {
		t.Fatalf("harness ask says a person is needed without the harness sentence: waiting=%v reason=%q", waiting.waiting, waiting.reason)
	}
}

func TestWaitingSentenceWrongOverlap(t *testing.T) {
	agent, _ := questionSession(t, "over1111over1111", nil)
	events := watched(agent)
	go func() {
		for range events {
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = agent.askConnect(ctx, connectStatus{ID: "github", Name: "GitHub"}) }()
	waitForPersonLane(t, agent, connectLaneActive)
	forgetOther := agent.presenceAsking(QuestionConsent, 41, "approve deleting the archive?")
	defer forgetOther()

	waiting := agent.waitingOnPerson()
	if !strings.Contains(waiting.reason, "connect your GitHub account?") {
		t.Fatalf("sign-in borrowed another lane's sentence: reason=%q", waiting.reason)
	}
}
