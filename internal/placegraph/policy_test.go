package placegraph

import (
	"encoding/json"
	"errors"
	"testing"
)

// The figures the design states are kept exactly; a change to one of them is a
// design change, not a tuning.
func TestTheDesignsOwnFiguresAreTheDefaults(t *testing.T) {
	p := DefaultRecommendPolicy()
	if p.MinClusterChats != 5 || p.DeclineSnoozeDays != 30 || p.MaxAIDepth != 3 {
		t.Fatalf("%+v", p)
	}
}

// Nothing reorganises by itself until somebody turns it on.
func TestNothingIsAutomaticByDefault(t *testing.T) {
	if DefaultRecommendPolicy().AutoFile {
		t.Fatal("filing without asking is on by default")
	}
}

func TestEveryDefaultIsInsideItsOwnBounds(t *testing.T) {
	d := DefaultRecommendPolicy()
	if d != d.Normalized() {
		t.Fatalf("defaults move when normalized: %+v", d.Normalized())
	}
}

func TestSetRefusesWhatItCannotHold(t *testing.T) {
	p := DefaultRecommendPolicy()
	for _, bad := range []struct{ key, value string }{
		{"minClusterChats", "2"}, {"minClusterChats", "4.5"}, {"minClusterChats", `"5"`},
		{"autoFile", "1"}, {"noSuchSetting", "true"}, {"maxAiDepth", "99"},
	} {
		if err := p.Set(bad.key, json.RawMessage(bad.value)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s=%s: %v", bad.key, bad.value, err)
		}
	}
	if p != DefaultRecommendPolicy() {
		t.Fatal("a refused write changed the policy")
	}
	if err := p.Set("minClusterChats", json.RawMessage("8")); err != nil || p.MinClusterChats != 8 {
		t.Fatalf("%v %d", err, p.MinClusterChats)
	}
	if v, _ := p.Value("minClusterChats"); v != 8 {
		t.Fatalf("read back %v", v)
	}
}

// A hand-edited file asking for a negative budget or an absurd depth is held
// to the bounds rather than trusted.
func TestAHandEditedPolicyIsHeldToItsBounds(t *testing.T) {
	p := DefaultRecommendPolicy()
	p.FilingCallsPerDay, p.MaxAIDepth, p.MinConfidence = -4, 400, 3
	n := p.Normalized()
	if n.FilingCallsPerDay != 0 || n.MaxAIDepth != 6 || n.MinConfidence != 50 {
		t.Fatalf("%+v", n)
	}
}
