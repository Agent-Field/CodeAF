//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

// `--variant` takes codeaf's one ladder: every rung, and `none` (or `auto`,
// `off`, nothing) for no `reasoning` at all. Anything else is refused rather
// than sent for a provider to reject call after call. The default is a rung.
func TestVariantWordsAreTheLadder(t *testing.T) {
	for word, want := range map[string]string{
		"low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh", "max": "max",
		"HIGH": "high", "none": "", "auto": "", "off": "", "": "",
	} {
		if got, ok := ParseVariant(word); !ok || got != want {
			t.Fatalf("ParseVariant(%q) = %q, %v; want %q", word, got, ok, want)
		}
	}
	for _, word := range []string{"hgih", "extreme", "1"} {
		if _, ok := ParseVariant(word); ok {
			t.Fatalf("ParseVariant(%q) was accepted", word)
		}
	}
	if got, ok := ParseVariant(DefaultVariant); !ok || got == "" {
		t.Fatalf("the default %q is not a rung", DefaultVariant)
	}
}

// THE CODER THINKS AT THE RUN'S EFFORT; ITS HISTORY SUMMARIES DO NOT. A summary
// recurs through a long run and rewrites what is already written, so it sends
// no `reasoning` at all, while every coder call asks for the run's rung.
func TestOnlyTheCodersCallsAskForTheRunsEffort(t *testing.T) {
	for _, summary := range []bool{false, true} {
		var body map[string]any
		backend := &modelAPIBackend{api: testModelAPI, variant: "high", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			return recordedResponse(r, 200, "text/event-stream", chatReply("answer", 10)), nil
		})}}
		client := newSeniorDevLLM(backend, "ses", "openrouter", "vendor/model", "coder", "high", nil, &turnLedger{}, false)
		params := orclient.RequestParams{ModelID: "vendor/model", Prompt: []msgmodel.ModelMessage{msgmodel.UserText("the work")}}
		var stream steploop.PartStream
		var err error
		if summary {
			stream, err = (seniorDevSummaryClient{owner: client}).Stream(context.Background(), params)
		} else {
			stream, err = client.Stream(context.Background(), params)
		}
		if err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := stream.Next(); err != nil {
				break
			}
		}
		_ = stream.Close()
		reasoning, sent := body["reasoning"].(map[string]any)
		if summary && sent {
			t.Fatalf("a history summary asked for reasoning %v", reasoning)
		}
		if !summary && reasoning["effort"] != "high" {
			t.Fatalf("a coder call asked for reasoning %v, want the run's high", body["reasoning"])
		}
	}
}
