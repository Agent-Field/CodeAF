package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"testing"
)

func TestApplicableMemoryReadNeverOpensAClosedConversation(t *testing.T) {
	h := newActionHarness(t)
	id := h.agent()
	a, err := h.b.find(id)
	if err != nil {
		t.Fatal(err)
	}
	letGo(t, h.b, id, a.transcript)
	if _, err := h.b.ApplicableMemory(context.Background(), id); err == nil {
		t.Fatal("closed live basis invented")
	}
	if h.b.liveOf(a) != nil {
		t.Fatal("pure memory read opened the conversation")
	}
}
func TestApplicableMemoryReadDoesNotInventAnUnsupportedHeldCapability(t *testing.T) {
	h := newActionHarness(t)
	id := h.agent()
	// This fixture has ordinary session actions but no scoped-memory read.
	if _, err := h.b.ApplicableMemory(context.Background(), id); err == nil {
		t.Fatal("missing scoped-memory door became an empty snapshot")
	} else if w, ok := err.(*appwire.Error); !ok || w.Code != appwire.CodeUnknownMethod {
		t.Fatalf("unsupported: %v", err)
	}
}
