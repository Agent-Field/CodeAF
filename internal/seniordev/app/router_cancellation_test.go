//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/router/adaptive"
)

func TestRouterCancellationDecisionsAreTraced(t *testing.T) {
	var output bytes.Buffer
	router := initRunRouter(cliArgs{High: "openrouter/moonshotai/kimi-k3"}, newEventWriter(&output))
	choice, err := router.PickContext(context.Background(), "coder", adaptive.ModelTierHigh)
	if err != nil {
		t.Fatal(err)
	}
	router.RegisterCanceled(choice)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = router.PickContext(ctx, "coder", adaptive.ModelTierHigh); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("trace=%s", output.Bytes())
	}
	for i, reason := range []string{"caller-canceled-request", "caller-canceled-pick"} {
		var event event
		if err = json.Unmarshal(lines[i], &event); err != nil {
			t.Fatal(err)
		}
		if event.Stage != "router-cancellation" || event.Status != reason || event.Data["provider_health_changed"] != false {
			t.Fatalf("event=%+v", event)
		}
	}
}

// THE CODER MOVING TO ANOTHER MODEL IS A STAGE, with where it came from, where
// it went and the router's reason; a pick that stays on its model, and the
// run's first pick, move nothing and say nothing.
func TestTheCodersMoveToAnotherModelIsAStage(t *testing.T) {
	var output bytes.Buffer
	router := initRunRouter(cliArgs{High: "openrouter/vendor/one,openrouter/vendor/two"}, newEventWriter(&output))
	first, err := router.PickContext(context.Background(), "coder", adaptive.ModelTierHigh)
	if err != nil {
		t.Fatal(err)
	}
	router.Register(first, 1, 10, errors.New("429 rate limit exceeded"))
	second, err := router.PickContext(context.Background(), "coder", adaptive.ModelTierHigh)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Switched {
		t.Fatalf("the router stayed on %s after a rate limit; the test cannot see a switch", second.Candidate.ID)
	}
	router.Register(second, 1, 10, nil)
	var switches []event
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatal(err)
		}
		if value.Stage == "model-switch" {
			switches = append(switches, value)
		}
	}
	if len(switches) != 1 {
		t.Fatalf("model-switch stages = %d, want the one real change: %s", len(switches), output.Bytes())
	}
	got := switches[0]
	if got.Status != "switched" || got.Data["from"] != first.Candidate.ID || got.Data["to"] != second.Candidate.ID || got.Data["reason"] == "" {
		t.Fatalf("the switch = %+v, want from %s to %s with a reason", got, first.Candidate.ID, second.Candidate.ID)
	}
}
