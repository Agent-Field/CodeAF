package provider

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
)

func TestReasoningRefusalS2(t *testing.T) {
	for _, message := range []string{`\"llama3.2:latest\" does not support thinking`, `This model does not support reasoning`} {
		t.Run(message, func(t *testing.T) {
			quirksAt(t, "ollama/llama3.2:latest")
			recorded := &capture{}
			cfg := Config{APIKey: "test-key", BaseURL: "http://provider.test", Model: "ollama/llama3.2:latest"}
			cfg.HTTPClient = handlerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recorded.record(r)
				w.Header().Set("Content-Type", "application/json")
				if recorded.body(recorded.count() - 1)["reasoning"] != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"message":"` + message + `","code":400}}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
			}))
			ctx := WithConfiguredEffortRung(context.Background(), effort.High)
			for i := range 2 {
				client, err := NewClient(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
					t.Fatalf("S2 call %d: %v", i, err)
				}
				if got := recorded.count(); got != i+2 {
					t.Fatalf("S2 sent %d requests, want %d", got, i+2)
				}
			}
			if recorded.body(0)["reasoning"] == nil || recorded.body(1)["reasoning"] != nil || recorded.body(2)["reasoning"] != nil {
				t.Fatal("S2 must remove reasoning from repair and future clients")
			}
		})
	}
}

func TestCatalogDisablesReasoningS2(t *testing.T) {
	client, recorded := newTestClient(t, Config{SupportsParameter: func(string, string) (bool, bool) { return false, true }})
	ctx := WithConfiguredEffortRung(context.Background(), effort.High)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 1 || recorded.body(0)["reasoning"] != nil {
		t.Fatal("S2 catalog refusal must prevent reasoning before sending")
	}
}

func TestReasoningRefusalIsPermanentS2(t *testing.T) {
	quirksAt(t, "refusal/always")
	recorded := &capture{}
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "refusal/always",
		HTTPClient: handlerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorded.record(r)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"This model does not support thinking","metadata":{"provider_name":"local"}}}`))
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CompleteWithMessages(WithConfiguredEffortRung(context.Background(), effort.High), userMessages("hello"))
	var failure *APIError
	if !errors.As(err, &failure) || !failure.ReasoningUnsupported || !failure.OurRequest() {
		t.Fatalf("S2 refusal lost its permanent classification: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("S2 sent %d requests, want one refusal and one repair", recorded.count())
	}
}
