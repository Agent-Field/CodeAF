//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
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
