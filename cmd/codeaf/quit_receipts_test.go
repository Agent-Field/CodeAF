package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestProcessCloseSettlesAdmittedReceipt(t *testing.T) {
	t.Run("tracked", func(t *testing.T) { processCloseReceipt(t, false, false) })
	t.Run("forgotten", func(t *testing.T) { processCloseReceipt(t, true, false) })
	t.Run("deadline", func(t *testing.T) { processCloseReceipt(t, false, true) })
}

func processCloseReceipt(t *testing.T, forgotten, timeout bool) {
	g := &tailPublishGate{committed: make(chan struct{}), reply: make(chan struct{}), costRelease: make(chan struct{}), costRequested: make(chan struct{})}
	h := newHostedTailChat(t, g)
	h.say("Review fixture close accounting.")
	h.wait("shared answer", h.saved)
	select {
	case <-g.costRequested:
	case <-time.After(10 * time.Second):
		t.Fatal("receipt admission missing")
	}
	defer releaseTailGate(g.costRelease)
	if err := h.client.Agent().DetachAndWait(); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	agent := h.agents[len(h.agents)-1]
	h.mu.Unlock()
	started := make(chan struct{})
	p := &v3Process{processStop: func() { close(started) }}
	p.track(agent)
	if forgotten {
		p.forget(agent)
	}
	if timeout {
		old := v3ReceiptWait
		v3ReceiptWait = 25 * time.Millisecond
		defer func() { v3ReceiptWait = old }()
	}
	closed := make(chan struct{})
	go func() { p.closeAll(); close(closed) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("process shutdown did not start")
	}
	early := false
	began := time.Now()
	if timeout {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("quit exceeded bounded receipt wait")
		}
	} else {
		select {
		case <-closed:
			early = true
		case <-time.After(50 * time.Millisecond):
		}
	}
	// Observe shutdown before allowing the real local HTTP receipt to settle.
	if !timeout {
		releaseTailGate(g.costRelease)
	}
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("quit did not finish after receipt")
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got float64
	for _, r := range rows {
		if r.GenerationID == "late-title-cost" {
			got += r.USD
		}
	}
	t.Logf("after real v3Process.closeAll: owed=$0.000345232 recorded=$%.9f returned_before_receipt=%v", got, early)
	want := .000345232
	if timeout {
		want = 0
		t.Logf("bounded quit elapsed=%s expected=$0 recorded=$%.9f; receipt deliberately unavailable", time.Since(began), got)
	}
	if early || got != want {
		t.Errorf("shutdown receipt total: expected=$%.9f recorded=$%.9f", want, got)
	}
}
