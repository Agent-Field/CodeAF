package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestFactoryEffortRecoverySurvivesModelAndAccountSwitch(t *testing.T) {
	var mu sync.Mutex
	requests := make(map[string][]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var model string
		_ = json.Unmarshal(body["model"], &model)
		knobs := body["reasoning"] != nil || body["reasoning_effort"] != nil || body["thinking"] != nil
		key := model + "/" + r.Header.Get("Authorization")
		mu.Lock()
		requests[key] = append(requests[key], knobs)
		mu.Unlock()
		if knobs {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"message":"This model does not support reasoning","code":400}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	sources := func(key string) modelsource.Set {
		return modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(server.URL), Address: server.URL, Key: key})
	}
	agent, err := New(Config{Workspace: t.TempDir(), Model: "fixture/first", Sources: sources("first"), Stage: &fakeStageDoor{}, Effort: effort.High})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	submit := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		events, err := agent.Submit(ctx, "answer done")
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event.Kind == EventError {
				t.Fatalf("Factory turn: %v", event.Err)
			}
		}
	}
	submit()
	agent.SetModel("fixture/second")
	submit()
	agent.SetSources(sources("second"))
	submit()
	mu.Lock()
	defer mu.Unlock()
	for _, key := range []string{"fixture/first/Bearer first", "fixture/second/Bearer first", "fixture/second/Bearer second"} {
		got := requests[key]
		if len(got) != 2 || !got[0] || got[1] {
			t.Errorf("requests for %s = %v; want [true false]", key, got)
		}
	}
}
