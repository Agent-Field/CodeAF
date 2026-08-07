package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

type briefRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn briefRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestMorningBriefUsesOnePanelRoutedCompletion(t *testing.T) {
	var calls int
	var requestBody map[string]any
	httpClient := &http.Client{Transport: briefRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusNotFound
		body := `{}`
		if strings.HasSuffix(request.URL.Path, "/chat/completions") {
			calls++
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &requestBody); err != nil {
				t.Fatal(err)
			}
			content, _ := json.Marshal(`{"headline":"While you were away: the report landed.","items":[{"seq":7,"body":"The report landed."}]}`)
			body = `{"model":"small/brief","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":` + string(content) + `}}]}`
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	panel, err := router.New(router.Panel{Models: []router.Spec{{
		Slug: "small/brief", Price: 0.05,
	}}}, provider.Config{
		APIKey: "test-key", BaseURL: "http://panel.test", HTTPClient: httpClient,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panel.Close() })

	settings := config.Config{Model: "small/brief", Reasoning: provider.EffortOff}
	client := &liveClient{settings: settings, model: "small/brief", client: panel}
	draft, err := composeMorningBrief(settings, client, nil)(context.Background(), resident.BriefActivity{
		Events: []resident.BriefEvent{{
			Seq: 7, Kind: store.BriefDone, Text: "Release report landed.", Ref: "report",
		}},
		Done: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("completion calls = %d, want exactly one", calls)
	}
	if got, _ := requestBody["model"].(string); got != "small/brief" {
		t.Fatalf("routed model = %q, want small/brief", got)
	}
	if requestBody["response_format"] == nil {
		t.Fatalf("routed brief omitted its JSON schema: %#v", requestBody)
	}
	if draft.Headline != "While you were away: the report landed." ||
		len(draft.Items) != 1 || draft.Items[0].Seq != 7 {
		t.Fatalf("draft = %+v", draft)
	}
}
