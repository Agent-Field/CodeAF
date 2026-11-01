package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
)

// R10(a) repairs stock Ollama's explicit refusal only for the receiving task
// client, including its later calls; a new client must learn for itself.
func TestATaskChildResendsWithoutEffortWhenTheModelSaysItCannotThink(t *testing.T) {
	for _, rung := range []effort.Rung{effort.Low, effort.Max} {
		t.Run(string(rung), func(t *testing.T) {
			fixture := noThinkingEndpoint(t)
			client := fixture.client(t, true)
			ctx := WithConfiguredEffortRung(context.Background(), rung)
			for i := 0; i < 2; i++ {
				if _, err := client.CompleteWithMessages(ctx, userMessages("write KIWI")); err != nil {
					t.Fatalf("task's refused effort was not repaired: %v", err)
				}
			}
			bodies := fixture.requests()
			if len(bodies) != 3 || bodies[0]["reasoning"] == nil {
				t.Fatalf("want one refused request, its repair, then one memoized call: %+v", bodies)
			}
			for _, body := range bodies[1:] {
				if body["reasoning"] != nil || body["reasoning_effort"] != nil || body["thinking"] != nil {
					t.Errorf("repaired request retained effort or budget: %+v", body)
				}
			}
			if _, err := os.Stat(filepath.Join(fixture.profile, quirksFile)); !os.IsNotExist(err) {
				t.Fatalf("the refusal escaped the client into the quirks file: %v", err)
			}
			fresh := fixture.client(t, false)
			if _, err := fresh.CompleteWithMessages(ctx, userMessages("write KIWI")); err == nil {
				t.Fatal("another client inherited the task client's memo")
			}
			if len(fixture.requests()) != 4 {
				t.Fatal("the other client retried a refusal without opting in")
			}
			freshTask := fixture.client(t, true)
			if _, err := freshTask.CompleteWithMessages(ctx, userMessages("write KIWI")); err != nil || len(fixture.requests()) != 6 {
				t.Fatalf("another task client did not discover its own refusal: %v", err)
			}
			if _, err := client.CompleteWithMessages(ctx, userMessages("write KIWI"), ai.WithModel("another-model")); err != nil || len(fixture.requests()) != 8 {
				t.Fatalf("one model's memo leaked into another model: %v", err)
			}
		})
	}
}

// R10(a) bounds repair to the precise 400, a sent effort, and one resend.
func TestTaskEffortRepairMatchesOnlyTheExplicit400(t *testing.T) {
	for _, tc := range []struct {
		name, message        string
		status, requests     int
		sentEffort, succeeds bool
	}{
		{"reasoning spelling", "DOES NOT SUPPORT REASONING", 400, 2, true, true},
		{"unrelated refusal", "context length is too large", 400, 1, true, false},
		{"other status", "does not support thinking", 422, 1, true, false},
		{"no effort sent", "does not support thinking", 400, 1, false, false},
		{"second refusal", "does not support thinking", 400, 2, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if call > 1 && tc.succeeds {
					w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"KIWI"},"finish_reason":"stop"}]}`))
					return
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": tc.message}})
			}))
			defer server.Close()
			client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, Model: "fixture/no-thinking", Direct: true, DropRefusedEffort: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.sentEffort {
				ctx = WithConfiguredReasoningEffort(ctx, EffortLow)
			}
			_, err = client.CompleteWithMessages(ctx, userMessages("write KIWI"))
			if (err == nil) != tc.succeeds || int(calls.Load()) != tc.requests {
				t.Fatalf("requests=%d, want %d; error=%v", calls.Load(), tc.requests, err)
			}
		})
	}
}

// R10's stability rule keeps ordinary clients' explicit effort and 400 error.
func TestWithoutTheFlagARefusedEffortIsReturnedAsBefore(t *testing.T) {
	fixture := noThinkingEndpoint(t)
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortLow)
	_, err := fixture.client(t, false).CompleteWithMessages(ctx, userMessages("write KIWI"))
	if err == nil || !strings.Contains(err.Error(), `API error (400): "llama3.2:latest" does not support thinking`) {
		t.Fatalf("ordinary refusal changed: %v", err)
	}
	if bodies := fixture.requests(); len(bodies) != 1 || bodies[0]["reasoning"] == nil {
		t.Fatalf("ordinary call changed or retried: %+v", bodies)
	}
}

// R10(a) requires an effort on the actual wire. The endpoint ladder can
// already have removed it before this refusal loop gets the request.
func TestAnAlreadyRemovedEffortDoesNotTeachTheTaskClient(t *testing.T) {
	var recorded capture
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded.record(r)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"does not support thinking"}}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{APIKey: "test-key", BaseURL: server.URL, Model: "fixture/no-thinking", Direct: true, DropRefusedEffort: true})
	if err != nil {
		t.Fatal(err)
	}
	request := &ai.Request{Model: "fixture/no-thinking", Messages: userMessages("write KIWI")}
	response, err := client.sendRepaired(context.Background(), request, callKnobs{effort: effortRequest{effort: EffortLow, explicit: true}, relaxed: relaxReasoning}, false)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 400 || recorded.count() != 1 || recorded.body(0)["reasoning"] != nil {
		t.Errorf("an already effortless request earned a repair: status %d, requests %d", response.StatusCode, recorded.count())
	}
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortLow)
	_, err = client.CompleteWithMessages(ctx, userMessages("write KIWI"))
	if err == nil || recorded.count() != 3 || recorded.body(1)["reasoning"] == nil {
		t.Errorf("an effortless refusal changed the later ordinary attempt: requests %d, error %v", recorded.count(), err)
	}
}

type noThinkingService struct {
	server  *httptest.Server
	profile string
	mu      sync.Mutex
	bodies  []map[string]any
}

func noThinkingEndpoint(t *testing.T) *noThinkingService {
	t.Helper()
	forgetLanes(t)
	f := &noThinkingService{profile: quirksAt(t, "llama3.2:latest")}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if body["reasoning"] != nil || body["reasoning_effort"] != nil || body["thinking"] != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"message":"\"llama3.2:latest\" does not support thinking"}}`))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"KIWI"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *noThinkingService) client(t *testing.T, repair bool) *Client {
	t.Helper()
	c, err := NewClient(Config{APIKey: "test-key", BaseURL: f.server.URL, Model: "llama3.2:latest", Direct: true, DropRefusedEffort: repair})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *noThinkingService) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.bodies...)
}
