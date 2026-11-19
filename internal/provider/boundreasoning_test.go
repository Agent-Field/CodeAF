package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── RUN21: THE CHECKER'S THINKING WAS BOUND TO A MACHINE ITS OWN REQUEST REFUSED ─
//
// The rota's checker ran on google/gemini-4-carbon. Five calls were answered by
// Google AI Studio, each carrying encrypted `reasoning_details` that only Google
// AI Studio can read. Then the ledger struck Google AI Studio, the sixth call
// went out with it in `provider.ignore`, and the router answered with the
// sentence below — through every rung of the ladder, because every rung
// carried the same encrypted working. The task settled failed over finished
// work. The fake router here answers the way OpenRouter did.

const run21BoundRefusal = `{"error":{"code":404,"message":"Your request contains encrypted reasoning or compaction content that can only be processed by the provider that created it (google-ai-studio), but your request's provider.ignore preference excludes that provider. Allow google-ai-studio or start a new conversation without the encrypted items."}}`

// run21Router refuses any request that carries encrypted working while its
// `provider.ignore` names the machine that made it, and serves everything else
// from the other machine behind the model.
func run21Router(t *testing.T) (http.Handler, *int) {
	t.Helper()
	var mu sync.Mutex
	refusals := 0
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []map[string]json.RawMessage `json:"messages"`
			Provider *providerPrefs               `json:"provider"`
		}
		// The capture in front of this handler has already read the body, so
		// the request is read again the way the client itself replays it.
		replay, err := request.GetBody()
		if err != nil {
			t.Errorf("replay: %v", err)
			return
		}
		if err := json.NewDecoder(replay).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		bound := false
		for _, message := range body.Messages {
			if raw, ok := message["reasoning_details"]; ok && strings.Contains(string(raw), "reasoning.encrypted") {
				bound = true
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		if bound && body.Provider != nil && namesEndpoint(body.Provider.Ignore, "Google AI Studio") {
			mu.Lock()
			refusals++
			mu.Unlock()
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(run21BoundRefusal))
			return
		}
		_, _ = writer.Write([]byte(`{"model":"google/gemini-4-carbon","provider":"Google Vertex",` +
			`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"holds: every row checks"}}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}), &refusals
}

// run21Working is the sidecar the checker's sixth call carried: the assistant
// step's own words, its readable reasoning, and Google AI Studio's encrypted
// continuation of it.
func run21Working() []MessageReasoning {
	return []MessageReasoning{{}, {
		Field: "reasoning", Text: "the rota reads every row against the staff sheet",
		Details: json.RawMessage(`[{"type":"reasoning.encrypted","data":"EoEBCn8BaRR9ExmDmXCd","format":"google-gemini-v1","index":0}]`),
		Model:   "google/gemini-4-carbon",
	}, {}}
}

func TestRun21ACheckerWhoseThinkingIsBoundToAStruckMachineStillAnswers(t *testing.T) {
	handler, refusals := run21Router(t)
	client, recorded := routedClient(t, RoutingLatency, handler)
	const model = "google/gemini-4-carbon"
	client.config.Model = model
	// Two machines serve the model, and the ledger has struck the one that made
	// the checker's working, exactly as run21's sixth call found it.
	client.velocity.brisk(model, "Google AI Studio")
	client.velocity.brisk(model, "Google Vertex")
	client.velocity.pace(model, "Google AI Studio", 5*time.Minute)

	ctx := WithMessageReasoning(context.Background(), run21Working())
	response, err := client.CompleteWithMessages(ctx, reasoningTranscript())
	if err != nil {
		t.Fatalf("the checker's call failed as run21's did: %v", err)
	}
	if got := response.Text(); got != "holds: every row checks" {
		t.Fatalf("answer = %q", got)
	}
	if *refusals != 1 || recorded.count() != 2 {
		t.Fatalf("refusals = %d over %d requests, want the one refusal and its repair", *refusals, recorded.count())
	}
	repaired := wireMessages(t, recorded.body(1))[1]
	if _, present := repaired["reasoning_details"]; present {
		t.Fatalf("the repair still carried the encrypted working: %#v", repaired)
	}
	// ONLY THE BOUND HALF CAME OFF. The words and the readable reasoning are
	// the conversation, and they still go.
	if got := repaired["reasoning"]; got != "the rota reads every row against the staff sheet" {
		t.Fatalf("readable reasoning = %#v, want it kept", got)
	}
	if got := repaired["content"]; got != "looking" {
		t.Fatalf("the message itself must still go, got %#v", got)
	}
	// AND THE VETO STANDS. It was put there for a reason this request knows
	// nothing about, so the repair changes what the request carries, never
	// where it may go.
	if prefs := prefsOn(t, recorded, 1); prefs == nil || !strings.Contains(strings.Join(anyStrings(prefs["ignore"]), ","), "Google AI Studio") {
		t.Fatalf("repair = %#v, want the struck machine still struck", prefs)
	}
	if reasoningReplayRefused(model) {
		t.Fatal("a refusal about one machine's encrypted working must not teach the model to drop replay")
	}
}

// The same refusal without the encryption sentence is still recognised, from
// the shape alone: the router refusing on its own account a request that
// carried encrypted working under a membership filter.
func TestABoundReasoningRefusalIsReadByItsShapeNotItsWords(t *testing.T) {
	response := &http.Response{StatusCode: http.StatusNotFound, Request: requestCarrying(t, `{"provider":{"ignore":["Google AI Studio"]}}`)}
	reworded := []byte(`{"error":{"code":404,"message":"Nothing can take this request as it stands."}}`)
	if !refusesBoundReasoning(response, reworded, run21Working()) {
		t.Fatal("a reworded refusal of encrypted working under an ignore list went unrecognised")
	}
	if refusesBoundReasoning(response, reworded, []MessageReasoning{{Field: "reasoning", Text: "plain"}}) {
		t.Fatal("plain reasoning text binds nothing and must not be stripped")
	}
	unfiltered := &http.Response{StatusCode: http.StatusNotFound, Request: requestCarrying(t, `{}`)}
	if refusesBoundReasoning(unfiltered, reworded, run21Working()) {
		t.Fatal("a refusal with no filter on the wire and no word about encryption is not this class")
	}
	if !refusesBoundReasoning(unfiltered, []byte(run21BoundRefusal), run21Working()) {
		t.Fatal("the router's own word about encrypted content covers an exclusion the wire does not show")
	}
}

func requestCarrying(t *testing.T, body string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "https://openrouter.ai/api/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func anyStrings(value any) []string {
	list, _ := value.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
