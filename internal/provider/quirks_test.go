package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// strictHandler answers a disable the way MiniMax M2.7's endpoint does, and
// answers anything else normally. It is the whole fixture: the adapter's job is
// to be told no once and never send that shape to this model again.
func strictHandler(recorded *capture) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(recorded.count() - 1)
		reasoning, _ := body["reasoning"].(map[string]any)
		if enabled, present := reasoning["enabled"].(bool); present && !enabled {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"Reasoning is mandatory for this endpoint and cannot be disabled.","code":400}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"strict/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
}

func newStrictClient(t *testing.T, model string) (*Client, *capture) {
	t.Helper()
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: model,
		HTTPClient:        handlerClient(strictHandler(recorded)),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, recorded
}

func TestAdapterRecoversWhenAnEndpointRefusesToDisableReasoning(t *testing.T) {
	// The failure this repairs: every planning call on MiniMax M2.7 died on a
	// 400, three seconds in, because the harness's own economy asked a model
	// that always thinks to stop thinking.
	quirksAt(t, "strict/always-reasons")
	client, recorded := newStrictClient(t, "strict/always-reasons")
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatalf("a refused disable must be repaired, not returned: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the rejected one and its repair", recorded.count())
	}
	if reasoning, _ := recorded.body(0)["reasoning"].(map[string]any); reasoning["enabled"] != false {
		t.Fatalf("first request = %#v, want the disable that gets refused", recorded.body(0))
	}
	if _, present := recorded.body(1)["reasoning"]; present {
		t.Fatalf("repaired request = %#v, want no reasoning knob at all", recorded.body(1))
	}

	// Learned, not re-learned: the next call knows better before it is sent.
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan the next thing")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 3 {
		t.Fatalf("sent %d requests in total, want one more — the second call must not be refused again", recorded.count())
	}
	if _, present := recorded.body(2)["reasoning"]; present {
		t.Fatalf("second call = %#v, want the knob dropped without being told twice", recorded.body(2))
	}
	if !ReasoningMandatory("~strict/always-reasons") {
		t.Fatal("the fact must be readable by a surface, and by the slug however it is written")
	}
}

func TestAdapterKeepsAnUnrelated400AsTheErrorItIs(t *testing.T) {
	quirksAt(t, "strict/other")
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":{"message":"context length exceeded by 12 tokens"}}`))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "strict/other",
		HTTPClient:        handlerClient(handler),
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	_, err = client.CompleteWithMessages(ctx, userMessages("plan this"))
	if err == nil {
		t.Fatal("an unrelated 400 must surface as an error")
	}
	// The peeked body is put back whole: the caller's error still says what the
	// provider actually complained about.
	if !strings.Contains(err.Error(), "context length exceeded by 12 tokens") {
		t.Fatalf("error = %v, want the provider's own words", err)
	}
	if recorded.count() != 1 {
		t.Fatalf("sent %d requests, want no retry for a 400 that is ours", recorded.count())
	}
	if ReasoningMandatory("strict/other") {
		t.Fatal("an unrelated 400 must not teach a reasoning quirk")
	}
}

func TestLearnedQuirksSurviveTheProcessThatLearnedThem(t *testing.T) {
	dir := quirksAt(t, "strict/persisted")
	client, _ := newStrictClient(t, "strict/persisted")
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatal(err)
	}
	// The write is off the request path, so it is completed here rather than
	// waited on — the assertion is about the file's contents, not its timing.
	quirks.save()

	raw, err := os.ReadFile(filepath.Join(dir, quirksFile))
	if err != nil {
		t.Fatalf("nothing was written down: %v", err)
	}
	var wire quirksWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("memo is not readable: %v", err)
	}
	if _, known := wire.ReasoningMandatory["strict/persisted"]; !known {
		t.Fatalf("memo = %s, want the model that refused", raw)
	}

	// A fresh process reads it and never sends the refused shape at all.
	quirks.mandatory = map[string]time.Time{}
	LoadQuirks(dir)
	if !ReasoningMandatory("strict/persisted") {
		t.Fatal("a memo on disk must be believed at startup")
	}
	client, recorded := newStrictClient(t, "strict/persisted")
	if _, err := client.CompleteWithMessages(ctx, userMessages("plan this")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 1 {
		t.Fatalf("sent %d requests, want one — the refusal was already known", recorded.count())
	}
}

// quirksAt points the memo at a temporary profile for one test and forgets what
// that test learned on the way out. The memo is process-wide by design, and a
// test that left its model in it would be teaching every later test.
func quirksAt(t *testing.T, models ...string) string {
	t.Helper()
	dir := t.TempDir()
	LoadQuirks(dir)
	t.Cleanup(func() {
		// Before anything else: the memo's write is scheduled, not performed, so
		// a test that learned a fact may still have a writer inside the temp dir
		// t.TempDir is about to remove. Waiting here is what makes the removal —
		// and therefore the test — deterministic rather than load-dependent.
		quirks.settle()
		quirks.mutex.Lock()
		defer quirks.mutex.Unlock()
		for _, model := range models {
			delete(quirks.mandatory, normalizeModel(model))
			// EVERY memo, because they are all process-wide and a fact left
			// behind by one test silently changes the request shape of the next.
			delete(quirks.noCacheControl, normalizeModel(model))
			delete(quirks.noReasoningBudget, normalizeModel(model))
			delete(quirks.noReasoningReplay, normalizeModel(model))
		}
		quirks.path = ""
	})
	return dir
}

// count is how many requests reached the handler, read under the same lock the
// handler writes them with.
func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}
