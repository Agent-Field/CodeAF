package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/store"
)

type recoveringMemoryClient struct {
	mu         sync.Mutex
	recoverAt  time.Time
	calls      int
	shown      []string
	alwaysFail bool
	failure    error
}

func (c *recoveringMemoryClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.failure != nil {
		return nil, c.failure
	}
	if len(messages) > 1 {
		c.shown = append(c.shown, messageText(messages[1]))
	}
	if c.alwaysFail || time.Now().Before(c.recoverAt) {
		return nil, context.DeadlineExceeded
	}
	return textResponse(`{"keep":"I prefer concise answers","scope":"user","durability":"standing_rule"}`), nil
}

func TestMemoryRecoveryKeepsOriginalExchangeOnce(t *testing.T) {
	usageWriterFor(UsageLedgerPath())
	synctest.Test(t, func(t *testing.T) {
		client := &recoveringMemoryClient{recoverAt: time.Now().Add(2 * time.Second)}
		agent, brain := brainAgent(t, client, nil)
		agent.stopLaneBeat() // The process-wide lane queue is outside the fake-clock bubble.
		agent.learnFromTurn(userMessage{message: textMessage("user", "I prefer concise answers")}, "understood")
		settleMemory(agent)
		agent.memory.mu.Lock()
		failureSaid := agent.memory.failureSaid
		agent.memory.mu.Unlock()
		if !failureSaid.IsZero() {
			t.Fatal("recoverable outage was shown as a failure")
		}
		got := activeTexts(t, brain, store.OwnerUser)
		if len(got) != 1 || got[0] != "I prefer concise answers" {
			t.Fatalf("saved=%v", got)
		}
		client.mu.Lock()
		defer client.mu.Unlock()
		if client.calls < 2 {
			t.Fatalf("calls=%d; recovery never ran", client.calls)
		}
		for _, shown := range client.shown {
			if !strings.Contains(shown, "I prefer concise answers") {
				t.Fatalf("original exchange lost: %q", shown)
			}
		}
	})
}

func TestMemoryRecoveryCancellationStopsBackoff(t *testing.T) {
	usageWriterFor(UsageLedgerPath())
	synctest.Test(t, func(t *testing.T) {
		client := &recoveringMemoryClient{alwaysFail: true}
		agent, brain := brainAgent(t, client, nil)
		agent.stopLaneBeat() // The process-wide lane queue is outside the fake-clock bubble.
		agent.learnFromTurn(userMessage{message: textMessage("user", "I prefer concise answers")}, "understood")
		synctest.Wait()
		client.mu.Lock()
		before := client.calls
		client.mu.Unlock()
		if before == 0 {
			t.Fatal("first request never ran")
		}
		agent.memoryStop()
		settleMemory(agent)
		time.Sleep(3 * time.Second)
		client.mu.Lock()
		after := client.calls
		client.mu.Unlock()
		agent.memory.mu.Lock()
		failureSaid := agent.memory.failureSaid
		agent.memory.mu.Unlock()
		if !failureSaid.IsZero() {
			t.Fatal("shutdown cancellation was shown as a failure")
		}
		if after != before {
			t.Fatalf("retried after cancellation: %d -> %d", before, after)
		}
		if got := activeTexts(t, brain, store.OwnerUser); len(got) != 0 {
			t.Fatalf("cancelled pass saved=%v", got)
		}
	})
}

func TestMemoryRecoveryExhaustionOffersManualSave(t *testing.T) {
	usageWriterFor(UsageLedgerPath())
	synctest.Test(t, func(t *testing.T) {
		client := &recoveringMemoryClient{alwaysFail: true}
		agent, brain := brainAgent(t, client, nil)
		agent.stopLaneBeat()
		agent.learnFromTurn(userMessage{message: textMessage("user", "I prefer concise answers")}, "understood")
		settleMemory(agent)
		client.mu.Lock()
		calls := client.calls
		client.mu.Unlock()
		if calls < 2 || calls > 4 {
			t.Fatalf("unbounded or missing recovery: %d requests", calls)
		}
		agent.memory.mu.Lock()
		said := strings.Join(agent.memory.said, "\n")
		agent.memory.mu.Unlock()
		if !strings.Contains(said, "use /remember") || strings.Contains(said, "context deadline exceeded") {
			t.Fatalf("no actionable final notice: %q", said)
		}
		if got := activeTexts(t, brain, store.OwnerUser); len(got) != 0 {
			t.Fatalf("failed pass saved=%v", got)
		}
		assertMemoryFailureJournal(t, brain, "context deadline exceeded")
		if _, err := agent.Remember("I prefer concise answers"); err != nil {
			t.Fatalf("manual save failed: %v", err)
		}
		if got := activeTexts(t, brain, store.OwnerUser); len(got) != 1 {
			t.Fatalf("manual save not retained: %v", got)
		}
	})
}

func assertMemoryFailureJournal(t *testing.T, brain *store.Store, reason string) {
	t.Helper()
	events, err := brain.Events(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, event := range events {
		if event.Kind == store.EventMemoryWriteFailed {
			failures++
			if !strings.Contains(string(event.Payload), reason) {
				t.Fatalf("technical reason lost: %s", event.Payload)
			}
		}
	}
	if failures != 1 {
		t.Fatalf("failure journal rows=%d", failures)
	}
}

func TestMemoryRecoveryPermanentFailureDoesNotRetry(t *testing.T) {
	usageWriterFor(UsageLedgerPath())
	synctest.Test(t, func(t *testing.T) {
		client := &recoveringMemoryClient{failure: errors.New("invalid credentials")}
		agent, brain := brainAgent(t, client, nil)
		agent.stopLaneBeat()
		agent.learnFromTurn(userMessage{message: textMessage("user", "I prefer concise answers")}, "understood")
		settleMemory(agent)
		client.mu.Lock()
		calls := client.calls
		client.mu.Unlock()
		if calls != 1 {
			t.Fatalf("permanent failure retried %d times", calls)
		}
		if got := activeTexts(t, brain, store.OwnerUser); len(got) != 0 {
			t.Fatalf("failed pass saved=%v", got)
		}
		agent.memory.mu.Lock()
		said := strings.Join(agent.memory.said, "\n")
		agent.memory.mu.Unlock()
		if !strings.Contains(said, "use /remember") {
			t.Fatalf("no actionable failure notice: %q", said)
		}
		assertMemoryFailureJournal(t, brain, "invalid credentials")
	})
}
