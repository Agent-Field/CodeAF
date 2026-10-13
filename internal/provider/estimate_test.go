package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestCompletionAdmissionUsesTheEncodedThinkingCeiling(t *testing.T) {
	c, err := NewClient(Config{APIKey: "test", BaseURL: "http://127.0.0.1:1", Model: "test/model", Direct: true, ModelPrice: func(string) (float64, float64, bool) { return 0, 1, true }, ReasoningProfile: func(string) (ReasoningProfile, bool) {
		return ReasoningProfile{Mandatory: true, Default: EffortHigh, Efforts: []Effort{EffortHigh}}, true
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	estimate, known, err := c.EstimateCompletionCost(ctx, []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}}, ai.WithMaxTokens(8))
	if err != nil || !known || estimate <= 8 {
		t.Fatalf("thinking estimate=%v, known=%v, err=%v", estimate, known, err)
	}
}
func TestCostAdmissionRefusesUnknownPricesAndUnboundedAttempts(t *testing.T) {
	c, err := NewClient(Config{APIKey: "test", BaseURL: "http://127.0.0.1:1", Model: "test/model", Direct: true, ModelPrice: func(string) (float64, float64, bool) { return .01, .02, true }})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("no budget")
	ctx := WithCostAdmission(context.Background(), func(cost float64, known bool) error {
		if known {
			t.Fatal("unknown request admitted")
		}
		return sentinel
	})
	for _, body := range []string{`{}`, `{"max_tokens":null}`, `{"max_tokens":0}`} {
		if err := c.admitCost(ctx, "test/model", []byte(body)); !errors.Is(err, sentinel) {
			t.Fatal(err)
		}
	}
	seen := 0.0
	ctx = WithCostAdmission(context.Background(), func(cost float64, known bool) error {
		if !known {
			t.Fatal("bounded request not priced")
		}
		seen = cost
		return nil
	})
	body := []byte(`{"max_completion_tokens":10}`)
	if err := c.admitCost(ctx, "test/model", body); err != nil || seen != float64(len(body))*.01+10*.02 {
		t.Fatalf("actual shaped ceiling: %v %v", seen, err)
	}
	c.config.ModelPrice = nil
	if err := c.admitCost(WithCostAdmission(context.Background(), func(_ float64, known bool) error {
		if known {
			t.Fatal("missing price invented")
		}
		return sentinel
	}), "test/model", body); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func TestCostAdmissionRunsBeforeTheActualTransport(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer upstream.Close()
	c, err := NewClient(Config{APIKey: "test", BaseURL: upstream.URL, Model: "test/model", Direct: true, ModelPrice: func(string) (float64, float64, bool) { return .001, .002, true }})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("admission refused")
	admitted := 0
	ctx := WithCostAdmission(context.Background(), func(cost float64, known bool) error {
		admitted++
		if !known || cost <= 0 {
			t.Error("actual send was not priced")
		}
		return sentinel
	})
	_, err = c.CompleteWithMessages(ctx, []ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "hello"}}}}, ai.WithMaxTokens(8))
	if !errors.Is(err, sentinel) || admitted != 1 || calls != 0 {
		t.Fatalf("err=%v admission=%d calls=%d", err, admitted, calls)
	}
}
