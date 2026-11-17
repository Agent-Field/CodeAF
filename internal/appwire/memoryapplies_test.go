package appwire

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

type appliesEngine struct {
	sendEngine
	calls int
}

func (e *appliesEngine) ApplicableMemory(context.Context, string) (session.MemoryScopeSnapshot, error) {
	e.calls++
	return session.MemoryScopeSnapshot{Basis: "configured_live"}, nil
}
func TestApplicableMemoryRegisteredReadRejectsBadParamsAndOldEngines(t *testing.T) {
	if _, err := methods[FeatureMemoryApplies].call(&sendEngine{}, json.RawMessage(`{"agent":"a"}`)); codeOf(err) != CodeUnknownMethod {
		t.Fatal(err)
	}
	e := &appliesEngine{}
	for _, ask := range []string{`{}`, `{"agent":" a"}`, `{"agent":"a","crew":"x"}`} {
		if _, err := methods[FeatureMemoryApplies].call(e, json.RawMessage(ask)); codeOf(err) != CodeBadParams {
			t.Fatalf("%s: %v", ask, err)
		}
	}
	if e.calls != 0 {
		t.Fatal("invalid ask reached producer")
	}
	if _, err := methods[FeatureMemoryApplies].call(e, json.RawMessage(`{"agent":"a"}`)); err != nil || e.calls != 1 {
		t.Fatalf("actual read: %v calls%d", err, e.calls)
	}
	if len(withMemoryAppliesFeature(Hello{}, &sendEngine{}).Features) != 0 || len(withMemoryAppliesFeature(Hello{}, e).Features) != 1 {
		t.Fatal("feature fabricated or absent")
	}
}
func TestApplicableMemoryRegisteredReadKeepsItsAdmissionUntilProducerReturns(t *testing.T) {
	for n := 0; n < cap(memoryAppliesReads); n++ {
		memoryAppliesReads <- struct{}{}
	}
	defer func() {
		for n := 0; n < cap(memoryAppliesReads); n++ {
			<-memoryAppliesReads
		}
	}()
	e := &appliesEngine{}
	if _, err := methods[FeatureMemoryApplies].call(e, json.RawMessage(`{"agent":"a"}`)); codeOf(err) != CodeBusy || e.calls != 0 {
		t.Fatalf("busy: %v calls%d", err, e.calls)
	}
}
