package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// THE DATA-POLICY SENTENCE IS A REFUSAL, AND A REFUSED CEILING IS NOT SENT TWICE.
//
// The router's price ceiling is list × 1.25, and for a model whose resellers
// all charge well above list that admits exactly one endpoint: the first-party
// one. When the account's privacy setting excludes that provider the ceiling
// leaves nothing, and the router reports the LAST filter that emptied the set
// — "no endpoints available matching your guardrail restrictions and data
// policy" — rather than the price. That sentence matched nothing in
// [endpointRefusalPhrases], so the ladder built to drop the ceiling never
// fired and a headless run died on three identical retries (2026-08-28).
//
// Two things are held here. The ladder now recognises the sentence and lands
// the call on its first rung, ceiling gone. And the ledger remembers: the
// second call to the same model carries no ceiling from the start, so it costs
// one request and not a 404 plus a retry.
func TestADataPolicyRefusalDropsTheCeilingAndTeachesTheLedger(t *testing.T) {
	const policyBody = `{"error":{"message":"No endpoints available matching your guardrail ` +
		`restrictions and data policy. Configure: https://openrouter.ai/settings/privacy","code":404}}`
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		body := recorded.body(len(recorded.bodies) - 1)
		writer.Header().Set("Content-Type", "application/json")
		if prefs, _ := body["provider"].(map[string]any); prefs != nil && prefs["max_price"] != nil {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(policyBody))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop",` +
			`"message":{"role":"assistant","content":"ok"}}]}`))
	})
	config := Config{
		APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1",
		HTTPClient: handlerClient(handler), Model: "sim/model",
		// A known list price is what puts a ceiling on the wire at all.
		ModelPrice: func(string) (float64, float64, bool) { return 0.66e-6, 1.98e-6, true },
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	// THE LEDGER THIS TEST TEACHES IS THE PROCESS'S. `refuseCeiling` writes into
	// the velocity ledger every client folds into, and the memo is deliberately
	// process-lifetime and never expiring (velocity.go's `noCeiling`), so a
	// second run of this test would open with the ceiling already dropped and
	// see one request where it demands two. It is the same fault #432 names, in
	// a rig that is not the hedge rig, so it takes the same one-line answer.
	t.Cleanup(resetSharedLearners)

	var notices []string
	if _, err := client.CompleteWithMessages(noticeContext(context.Background(), &notices), userMessages("hi")); err != nil {
		t.Fatalf("the first rung should have landed the call: %v", err)
	}
	if got := len(recorded.bodies); got != 2 {
		t.Fatalf("first call made %d requests, want 2: the refused one and the relaxed retry", got)
	}
	if prefs, _ := recorded.body(0)["provider"].(map[string]any); prefs == nil || prefs["max_price"] == nil {
		t.Fatal("the first request did not carry a ceiling, so this test proves nothing")
	}
	if prefs, _ := recorded.body(1)["provider"].(map[string]any); prefs != nil && prefs["max_price"] != nil {
		t.Fatal("the retry still carried the ceiling — the first rung did not take it off")
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "relaxed the endpoint filter") {
		t.Fatalf("notices = %#v, want the endpoint filter relaxed first", notices)
	}

	// THE SECOND CALL IS SHAPED RIGHT FROM THE START.
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("again")); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if got := len(recorded.bodies); got != 3 {
		t.Fatalf("second call made %d requests, want exactly 1 — the ledger should have dropped the ceiling", got-2)
	}
	if prefs, _ := recorded.body(2)["provider"].(map[string]any); prefs != nil && prefs["max_price"] != nil {
		t.Fatal("the second call carried the ceiling the router already refused")
	}
}
