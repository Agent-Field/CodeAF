package appwire

import (
	"slices"
	"testing"
)

type drawnAttender struct{ fakeEngine }

func (*drawnAttender) DeferAsk(string, string) (*Ask, error) { return nil, nil }
func (*drawnAttender) ShowAsk(string, string) (*Ask, error)  { return nil, nil }
func (*drawnAttender) Since(string, string) (Since, error)   { return Since{}, nil }

// A surface sends asks.shown when it has drawn an ask's card; an engine whose
// choice clock waits for that says so, and an older one does not.
func TestAnEngineWhoseChoiceClockWaitsForTheCardSaysSo(t *testing.T) {
	if !slices.Contains(withAsksDrawnFeature(Hello{}, &drawnAttender{}).Features, FeatureAsksClockDrawn) {
		t.Fatal("an engine whose clock waits for asks.shown does not say so")
	}
	if slices.Contains(withAsksDrawnFeature(Hello{}, &fakeEngine{}).Features, FeatureAsksClockDrawn) {
		t.Fatal("an engine with no asks.shown says its clock waits for it")
	}
}
