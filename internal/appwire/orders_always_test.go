package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// verdictEngine keeps standing orders and records the verdicts it is given.
type verdictEngine struct {
	sendEngine
	verdicts []string
}

func (*verdictEngine) Orders(string) ([]Order, error)            { return nil, nil }
func (*verdictEngine) OrderRuns(string, int) ([]OrderRun, error) { return nil, nil }
func (*verdictEngine) RatifyOrder(string, *OrderEdits) (Order, error) {
	return Order{}, nil
}
func (*verdictEngine) ChangeOrder(string, OrderEdits) (Order, error) { return Order{}, nil }
func (*verdictEngine) NotNowOrder(string) error                      { return nil }
func (*verdictEngine) SetOrderState(string, string) (Order, error)   { return Order{}, nil }
func (*verdictEngine) RunOrderNow(string) (Order, error)             { return Order{}, nil }
func (e *verdictEngine) OrderVerdict(_ string, _ int64, verdict string) (Order, error) {
	e.verdicts = append(e.verdicts, verdict)
	return Order{}, nil
}

// alwaysEngine is one that takes "Go, and stop asking".
type alwaysEngine struct{ verdictEngine }

func (*alwaysEngine) TakesOrderAlways() {}

// Contract C6: hello names orders.always only for an engine that takes the
// verdict, so a desktop draws "Go, and stop asking" only against one that does.
func TestOrdersAlwaysIsAFeatureOfTheEnginesThatTakeIt(t *testing.T) {
	if h := withOrdersAlwaysFeature(Hello{}, &verdictEngine{}); len(h.Features) != 0 {
		t.Fatalf("an engine that does not take it names %v", h.Features)
	}
	if h := withOrdersAlwaysFeature(Hello{}, &alwaysEngine{}); len(h.Features) != 1 || h.Features[0] != FeatureOrdersAlways {
		t.Fatalf("an engine that takes it names %v", h.Features)
	}
	if FeatureOrdersAlways != "orders.always" {
		t.Fatalf("the feature is spelled %q", FeatureOrdersAlways)
	}
}

// Contract C3 and C6: orders.run takes the verdict always from an engine that
// says it does, and refuses it, naming the verdicts it does take, from one
// that does not; every other verdict is taken as before.
func TestOrdersRunTakesAlwaysOnlyFromAnEngineThatSaysSo(t *testing.T) {
	always := &alwaysEngine{}
	for _, verdict := range []string{"go", "always", "skip", "not_right"} {
		if _, err := methods["orders.run"].call(always, json.RawMessage(`{"id":"o","run":4,"verdict":"`+verdict+`"}`)); err != nil {
			t.Fatalf("%s: %v", verdict, err)
		}
	}
	if got := strings.Join(always.verdicts, ","); got != "go,always,skip,not_right" {
		t.Fatalf("the engine was given %q", got)
	}

	_, err := methods["orders.run"].call(always, json.RawMessage(`{"id":"o","run":4,"verdict":"whenever"}`))
	if codeOf(err) != CodeBadParams || !strings.Contains(err.Error(), "verdict is go, always, skip or not_right") {
		t.Fatalf("a verdict that is no answer: %v", err)
	}

	older := &verdictEngine{}
	if _, err := methods["orders.run"].call(older, json.RawMessage(`{"id":"o","run":4,"verdict":"go"}`)); err != nil {
		t.Fatal(err)
	}
	_, err = methods["orders.run"].call(older, json.RawMessage(`{"id":"o","run":4,"verdict":"always"}`))
	if codeOf(err) != CodeBadParams || !strings.Contains(err.Error(), "verdict is go, skip or not_right") {
		t.Fatalf("always against an engine that does not take it: %v", err)
	}
	if len(older.verdicts) != 1 {
		t.Fatalf("a refused verdict reached the engine: %v", older.verdicts)
	}
}
