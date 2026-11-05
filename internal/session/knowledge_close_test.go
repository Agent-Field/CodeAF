package session

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestCloseJoinsKnowledgeCapturePastTheGrace(t *testing.T) {
	profile := t.TempDir()
	agent := consentChat(t, profile)
	journal := agent.file.journalName()
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	hook := func(path string) {
		if path == journal && agent.knowledgeJournal.ctx.Err() != nil {
			once.Do(func() {
				close(entered)
				// Hold the final scan before its uncancellable consent database read.
				<-release
			})
		}
	}
	previous := knowledgeScanHook.Swap(&hook)
	t.Cleanup(func() { knowledgeScanHook.Store(previous) })
	t.Cleanup(unblock)
	closed := make(chan error, 1)
	go func() { closed <- agent.Close() }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("final knowledge scan never started")
	}
	var early bool
	select {
	case <-closed:
		early = true
	case <-time.After(closeGrace + 100*time.Millisecond):
	}
	unblock()
	// Even a failing probe must finish its blocked database writer before cleanup.
	<-agent.knowledgeJournal.done
	if !early {
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("close did not join capture after the database operation finished")
		}
	}
	if early {
		t.Fatal("Close returned while knowledge capture could still write knowledge.sqlite")
	}
}

func TestCloseJoinsStoppedJobKnowledgeCapture(t *testing.T) {
	checkJobKnowledgeClose(t, true)
}

func TestClosingStageKeepsJobKnowledgeLifetime(t *testing.T) {
	checkJobKnowledgeClose(t, false)
}

func checkJobKnowledgeClose(t *testing.T, stopped bool) {
	t.Helper()
	jobCtx, cancelJob := context.WithCancel(t.Context())
	t.Cleanup(cancelJob)
	agent, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("A durable queue keeps work."), nil
		},
	}}, func(c *Config) {
		c.ProfileDir = t.TempDir()
		c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
		c.usageLedger = filepath.Join(t.TempDir(), UsageLedgerName)
		c.JobBudget = &liveBudgetStub{cap: 5}
		c.JobContext = jobCtx
	})
	if err := agent.Knowledge().StartBuild(); err != nil {
		t.Fatal(err)
	}
	journal := agent.file.journalName()
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	hook := func(path string) {
		if path == journal {
			once.Do(func() { close(entered); <-release })
		}
	}
	previous := knowledgeScanHook.Swap(&hook)
	t.Cleanup(func() { knowledgeScanHook.Store(previous) })
	t.Cleanup(unblock)
	events := mustSubmit(t, agent, "Explain our durable queue")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("job knowledge capture never started")
	}
	collect(t, events)
	if stopped {
		cancelJob()
	}
	agent.knowledgeJournal.mu.Lock()
	work := agent.knowledgeJournal.compile
	agent.knowledgeJournal.mu.Unlock()
	closed := make(chan error, 1)
	go func() { closed <- agent.Close() }()
	var early bool
	select {
	case <-closed:
		early = true
	case <-time.After(closeGrace + 100*time.Millisecond):
	}
	// End the fixture's job before releasing capture, so no compiler call runs.
	if !stopped && jobCtx.Err() != nil {
		t.Error("stage close cancelled its active job's learning context")
	}
	cancelJob()
	unblock()
	<-work.done
	if !early {
		select {
		case err := <-closed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("close did not join the stopped job's knowledge capture")
		}
	}
	if early && stopped {
		t.Fatal("Close returned while stopped job knowledge capture could still write knowledge.sqlite")
	}
	if !early && !stopped {
		t.Fatal("closing a stage waited for learning owned by its still-running job")
	}
}
