package llmcall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

func TestResolveAssemblySortsAndInjectsNoop(t *testing.T) {
	service := &Service{DisableRouting: true}
	call, err := service.ResolveAndAssemble(context.Background(), StreamInput{
		SessionID: "s",
		Model:     Model{ProviderID: "litellm-proxy", ID: "m", APIID: "m"},
		Messages: []msgmodel.ModelMessage{{
			Role:    "assistant",
			Content: []any{map[string]any{"type": "tool-call"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(call.Params.Tools) != 1 || call.Params.Tools[0].Name != "_noop" {
		t.Fatalf("tools = %#v", call.Params.Tools)
	}
	if string(call.Params.Tools[0].InputSchema) != `{"type":"object","properties":{"reason":{"type":"string","description":"Unused"}}}` {
		t.Fatalf("schema = %s", call.Params.Tools[0].InputSchema)
	}
}

func TestResolveUsesRouterChoiceAndFallback(t *testing.T) {
	seed := float64(1)
	router := adaptive.NewAdaptiveModelRouter(adaptive.AdaptiveRouterConfig{
		HighModels: []adaptive.ModelCandidate{{ID: "p/routed", Tier: adaptive.ModelTierHigh}},
		RandomSeed: &seed,
	})
	resolver := ModelResolverFunc(func(_ context.Context, provider, model string) (Model, error) {
		if provider == "p" && model == "routed" {
			return Model{ProviderID: provider, ID: model}, nil
		}
		if provider == "orig" {
			return Model{ProviderID: provider, ID: model}, nil
		}
		return Model{}, errors.New("missing")
	})
	service := &Service{Router: router, Models: resolver}
	call, err := service.ResolveAndAssemble(context.Background(), StreamInput{
		Model: Model{ProviderID: "orig", ID: "m"},
		Agent: Agent{Name: "coder", Tier: adaptive.ModelTierHigh},
	})
	if err != nil {
		t.Fatal(err)
	}
	if call.Model.ProviderID != "p" || call.Model.ID != "routed" || call.Choice == nil {
		t.Fatalf("call = %#v", call)
	}
	// Release the pick for tests that share no process router state.
	router.Register(*call.Choice, 0, 0, nil)
}

func TestStreamPropagatesContextAndExactParams(t *testing.T) {
	var got orclient.RequestParams
	client := &fakeClient{run: func(ctx context.Context, params orclient.RequestParams) (Stream, error) {
		if ctx.Value(contextKey{}) != "value" {
			t.Fatal("context not propagated")
		}
		got = params
		return &fakeStream{}, nil
	}}
	service := &Service{DisableRouting: true, Clients: ClientFactoryFunc(func(context.Context, Model, *adaptive.RouteChoice, *adaptive.AdaptiveModelRouter) (StreamClient, error) {
		return client, nil
	})}
	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	stream, err := service.Stream(ctx, StreamInput{
		Model:  Model{ProviderID: "p", ID: "m"},
		System: []string{"sys"},
		Tools:  []orclient.Tool{{Name: "z", InputSchema: json.RawMessage(`{}`)}, {Name: "a", InputSchema: json.RawMessage(`{}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if got.ModelID != "m" || len(got.Prompt) != 1 || got.Tools[0].Name != "a" {
		t.Fatalf("params = %#v", got)
	}
}

type contextKey struct{}
type fakeClient struct {
	run func(context.Context, orclient.RequestParams) (Stream, error)
}

func (f *fakeClient) DoStream(ctx context.Context, params orclient.RequestParams) (Stream, error) {
	return f.run(ctx, params)
}

type fakeStream struct{}

func (*fakeStream) Next() (orclient.StreamPart, error) { return nil, io.EOF }
func (*fakeStream) Close() error                       { return nil }
