package agentfield

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// EVERY CALL THIS FACTORY MAKES ABOUT A RUN SAYS WHOSE IT IS: the run it
// starts, and every read, ack, steer and cancel of it after, carry
// X-AgentField-Run-Owner, so the control plane serves only its own runs. The
// ack names how far it has kept in its BODY, {"through": N}.
func TestEveryRunCallCarriesTheOwnerAndTheAckCarriesThrough(t *testing.T) {
	var mu sync.Mutex
	owners := map[string]string{}
	var ack map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		owners[r.Method+" "+r.URL.Path] = r.Header.Get("X-AgentField-Run-Owner")
		if r.URL.Path == "/api/v1/executions/e1/relay/ack" {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &ack)
		}
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/execute/async/support.triage":
			_, _ = w.Write([]byte(`{"execution_id":"e1","run_id":"r1","status":"running"}`))
		case "/api/v1/executions/e1/relay":
			_, _ = w.Write([]byte(`{"entries":[],"next":0}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()
	c := &Client{URL: server.URL, Owner: "codeaf-factory"}
	ctx := context.Background()
	if _, err := c.Dispatch(ctx, "support.triage", map[string]any{"title": "t"}, "codeaf-factory"); err != nil {
		t.Fatal(err)
	}
	_, _, _ = c.Tree(ctx, "r1")
	if _, err := c.Relay(ctx, "e1", 0); err != nil {
		t.Fatal(err)
	}
	if err := c.RelayAck(ctx, "e1", 7); err != nil {
		t.Fatal(err)
	}
	_ = c.Steer(ctx, "e1", "check negative amounts too")
	_ = c.CancelTree(ctx, "r1")
	mu.Lock()
	defer mu.Unlock()
	for call, owner := range owners {
		if owner != "codeaf-factory" {
			t.Errorf("%s carried owner %q", call, owner)
		}
	}
	if len(owners) != 6 {
		t.Fatalf("calls made: %v", owners)
	}
	if ack["through"] != float64(7) || len(ack) != 1 {
		t.Fatalf("the ack's body is %v, want {through: 7}", ack)
	}
}
