package main

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestLateReceiptKeepsChatHomeAfterQuit(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{}), costRelease: make(chan struct{}), costRequested: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Save before late accounting.")
	h.wait("shared answer", h.saved)
	select {
	case <-g.costRequested:
	case <-time.After(10 * time.Second):
		t.Fatal("receipt was not requested")
	}
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
	original := session.UsageLedgerPath()
	t.Setenv(home.EnvVar, t.TempDir())
	other := session.UsageLedgerPath()
	// Both destinations remain alive until the pending receipt has settled.
	// Quit may release the chat before accounting, but cannot retarget its home.
	defer func() { session.StopUsageWriter(original); session.StopUsageWriter(other) }()
	close(g.costRelease)
	receiptTotal := func(path string) float64 {
		rows, err := session.ReadUsage(path, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var sum float64
		for _, row := range rows {
			if row.GenerationID == "late-title-cost" {
				sum += row.USD
			}
		}
		return sum
	}
	h.wait("receipt accounting settled", func() bool {
		return session.FlushUsage() && receiptTotal(original)+receiptTotal(other) > 0
	})
	oldTotal, newTotal := receiptTotal(original), receiptTotal(other)
	t.Logf("original expected=$0.000345232 recorded=$%.9f; other expected=$0 recorded=$%.9f", oldTotal, newTotal)
	if math.Abs(oldTotal-.000345232) > 1e-12 || newTotal != 0 {
		t.Fatalf("receipt changed homes: original expected=$0.000345232 recorded=$%.9f; other expected=$0 recorded=$%.9f", oldTotal, newTotal)
	}
}

func TestHostedWaitReceiptsBeforeHomeCleanup(t *testing.T) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{}), costRelease: make(chan struct{}), costRequested: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Keep the pending receipt before removing this home.")
	h.wait("shared answer", h.saved)
	select {
	case <-g.costRequested:
	case <-time.After(10 * time.Second):
		t.Fatal("receipt was not requested")
	}
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	agent := h.agents[len(h.agents)-1]
	h.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	if err := agent.WaitReceipts(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("pending receipt wait = %v, want deadline exceeded", err)
	}
	close(g.costRelease)
	// Even the red run settles the admitted receipt before the fixture's home
	// disappears, so its expected failure cannot contaminate another test.
	path := session.UsageLedgerPath()
	h.wait("receipt in original home", func() bool {
		if !session.FlushUsage() {
			return false
		}
		rows, err := session.ReadUsage(path, time.Time{})
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.GenerationID == "late-title-cost" {
				return true
			}
		}
		return false
	})
	joined, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	if err := agent.WaitReceipts(joined); err != nil {
		t.Fatal(err)
	}
}
