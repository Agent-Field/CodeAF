package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"path/filepath"
	"strings"
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
	work := agent.knowledgeJournal.compile.Load()
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

func TestCloseCapsKnowledgeWritersAndNamesMisses(t *testing.T) {
	agent := consentChat(t, t.TempDir())
	agent.stopKnowledge()
	const cap = 80 * time.Millisecond
	agent.knowledgeCloseWait = cap
	scannerCtx, cancelScanner := context.WithCancel(context.Background())
	compilerCtx, cancelCompiler := context.WithCancel(context.Background())
	retiredCtx, cancelRetired := context.WithCancel(context.Background())
	scannerDone, compilerDone, retiredDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	unblock := func() { release.Do(func() { close(scannerDone); close(compilerDone); close(retiredDone) }) }
	defer unblock()
	agent.knowledgeJournal = &knowledgeJournal{path: agent.file.journalName(), ctx: scannerCtx, cancel: cancelScanner, done: scannerDone, vault: agent.Knowledge()}
	retired := &knowledgeJournal{path: "retired.jsonl", ctx: retiredCtx, cancel: cancelRetired, done: retiredDone, vault: agent.Knowledge()}
	retired.compile.Store(&jobKnowledgeCompile{cancel: cancelCompiler, done: compilerDone})
	agent.knowledgeRetired = []*knowledgeJournal{retired}
	// A scanner stalled in SQLite can hold its cursor lock beyond the deadline.
	retired.mu.Lock()
	unlock := sync.OnceFunc(retired.mu.Unlock)
	defer unlock()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	closed := make(chan error, 1)
	started := time.Now()
	go func() { closed <- agent.Close() }()
	select {
	case <-scannerCtx.Done():
		if retiredCtx.Err() == nil || compilerCtx.Err() == nil {
			t.Error("Close waited before cancelling the retired scanner and compiler")
		}
	case <-time.After(5 * cap):
		t.Error("Close did not cancel scanners promptly")
	}

	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * cap):
		unlock()
		unblock()
		<-closed
		t.Fatal("Close exceeded its shared knowledge deadline")
	}
	elapsed := time.Since(started)
	if elapsed < cap || elapsed > 3*cap {
		t.Fatalf("Close waited %s; want one shared %s cap", elapsed, cap)
	}
	if scannerCtx.Err() == nil || retiredCtx.Err() == nil || compilerCtx.Err() == nil {
		t.Fatal("Close did not cancel every scanner and retired compiler")
	}
	output := logs.String()
	profile := sha256.Sum256([]byte(agent.config.ProfileDir))
	for _, writer := range []struct{ kind, path string }{
		{"scanner", agent.knowledgeJournal.path},
		{"scanner", retired.path},
		{"compiler", retired.path},
	} {
		journal := sha256.Sum256([]byte(writer.path))
		prefix := fmt.Sprintf("knowledge: close timeout: writer=%s journal=%x profile=%x waited=", writer.kind, journal[:8], profile[:8])
		if got := strings.Count(output, prefix); got != 1 {
			t.Fatalf("named miss count = %d, want 1 for %s: %s", got, writer.kind, output)
		}
		duration := strings.Fields(strings.SplitN(output, prefix, 2)[1])[0]
		waited, err := time.ParseDuration(duration)
		if err != nil || waited < cap || waited > elapsed {
			t.Fatalf("miss wait %q does not describe close's %s wait", duration, elapsed)
		}
	}
	if strings.Contains(output, agent.config.ProfileDir) || strings.Contains(output, agent.knowledgeJournal.path) {
		t.Fatal("timeout log exposed a private path")
	}

	if strings.Count(output, "journal=") != 3 || strings.Count(output, "profile=") != 3 || strings.Count(output, "waited=") != 3 {
		t.Fatalf("misses lack identity or wait duration: %s", output)
	}
}
