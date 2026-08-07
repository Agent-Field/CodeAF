package main

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/head"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func wordsCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	client := &http.Client{Transport: voiceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		payload := `{"data":[
			{"id":"google/gemini-3-pro","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"google/gemini-3-flash","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"anthropic/claude-opus-5","architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"paint/image","architecture":{"input_modalities":["text"],"output_modalities":["image"]}}
		]}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	return catalog.Load(context.Background(), catalog.Options{
		BaseURL: "https://example.invalid/api/v1", Dir: t.TempDir(), HTTPClient: client,
	})
}

func TestWorkModelWordsResolveAgainstTheSameCandidacyFilterTalkAndWorkUse(t *testing.T) {
	models := wordsCatalog(t)
	boost := func() string { return "anthropic/claude-opus-5" }

	slot := resolveWorkModelWords(head.ModelWords{Boost: true}, models, boost)
	if slot.Model != "anthropic/claude-opus-5" || slot.Requested != "the boost model" {
		t.Fatalf("boost word = %+v", slot)
	}
	named := resolveWorkModelWords(head.ModelWords{Names: []string{"opus"}}, models, boost)
	if named.Model != "anthropic/claude-opus-5" || len(named.Candidates) != 0 {
		t.Fatalf("fuzzy name = %+v", named)
	}
	ambiguous := resolveWorkModelWords(head.ModelWords{Names: []string{"gemini"}, Explicit: true}, models, boost)
	if ambiguous.Model != "" ||
		!reflect.DeepEqual(ambiguous.Candidates, []string{"google/gemini-3-pro", "google/gemini-3-flash"}) {
		t.Fatalf("ambiguous name = %+v", ambiguous)
	}
	// An image model is not a candidate for work, exactly as the palette's
	// work slot refuses to list it.
	wrong := resolveWorkModelWords(head.ModelWords{Names: []string{"paint"}, Explicit: true}, models, boost)
	if wrong.Model != "" || len(wrong.Candidates) != 0 || wrong.Requested != "paint" {
		t.Fatalf("wrong-slot name = %+v", wrong)
	}
}

type recordingClient struct {
	model string
	calls int
}

func (c *recordingClient) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	return &ai.Response{Model: c.model, Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}}},
	}}}, nil
}

func (c *recordingClient) Model() string { return c.model }

// The pin is per job, and its carrier is the node's own provenance: a leaf of
// the pinned job is served by the named model, and every other leaf keeps the
// current work client even when both run in the same session.
func TestPinnedWorkClientServesOnlyTheJobThatAskedForIt(t *testing.T) {
	pinnedClient := &recordingClient{model: "google/gemini-3-pro"}
	pool := &messageClientPool{clients: map[string]*liveClient{
		"google/gemini-3-pro": {model: "google/gemini-3-pro", client: pinnedClient},
	}}

	pinnedNode := store.Node{ID: "bench-leaf", Provenance: store.Provenance{
		Intent: "benchmark the parser with the gemini model", WorkModel: "google/gemini-3-pro",
	}}
	client, ok := pinnedWorkClient(pool, pinnedNode)
	if !ok {
		t.Fatal("a job that named a model was not pinned")
	}
	model, leafClient := client.Snapshot()
	if model != "google/gemini-3-pro" {
		t.Fatalf("leaf model = %q", model)
	}
	if _, err := leafClient.CompleteWithMessages(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if pinnedClient.calls != 1 {
		t.Fatalf("pinned model served %d leaf calls, want 1", pinnedClient.calls)
	}

	ordinary := store.Node{ID: "other-leaf", Provenance: store.Provenance{Intent: "read the file"}}
	if _, ok := pinnedWorkClient(pool, ordinary); ok {
		t.Fatal("ordinary work was pinned to another job's model")
	}
	if _, ok := pinnedWorkClient(nil, pinnedNode); ok {
		t.Fatal("a missing pool still claimed a pin")
	}
}
