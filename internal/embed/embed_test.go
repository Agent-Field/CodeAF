package embed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

type scriptedWire struct {
	last provider.EmbeddingRequest
	tag  string
	resp *provider.EmbeddingResponse
	err  error
}

func (s *scriptedWire) Embed(ctx context.Context, request provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	s.last = request
	s.tag = provider.CallTagFrom(ctx)
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestTheAdapterTagsSpendAndReturnsVectors(t *testing.T) {
	cost := 0.01
	wire := &scriptedWire{resp: &provider.EmbeddingResponse{
		Model: "openai/text-embedding-3-small",
		Data: []provider.Embedding{
			{Index: 0, Embedding: []float32{0.1, 0.2}},
			{Index: 1, Embedding: []float32{0.3, 0.4}},
		},
		Usage: &ai.Usage{Cost: &cost},
	}}
	var billed string
	var billedCost float64
	client := New(wire, "openai/text-embedding-3-small", func(model string, usage *ai.Usage) {
		billed = model
		if usage != nil && usage.Cost != nil {
			billedCost = *usage.Cost
		}
	})
	vectors, model, version, dim, err := client.Embed(context.Background(), []string{"harbor", "ledger"})
	if err != nil {
		t.Fatal(err)
	}
	if wire.tag != string(roles.RoleEmbed) {
		t.Fatalf("spend tag = %q, want %s", wire.tag, roles.RoleEmbed)
	}
	if wire.tag == string(roles.RoleAuditor) {
		t.Fatal("embeddings must not spend as RoleAuditor")
	}
	if model != "openai/text-embedding-3-small" || version != Version || dim != 2 {
		t.Fatalf("model=%q version=%q dim=%d", model, version, dim)
	}
	if len(vectors) != 2 || vectors[0][0] != 0.1 || vectors[1][0] != 0.3 {
		t.Fatalf("vectors = %v", vectors)
	}
	if billed != model || billedCost != 0.01 {
		t.Fatalf("spend = %q %v", billed, billedCost)
	}
}

func TestAProductionStubCannotReturnEmptyVectors(t *testing.T) {
	wire := &scriptedWire{resp: &provider.EmbeddingResponse{
		Data: []provider.Embedding{{Index: 0, Embedding: nil}},
	}}
	_, _, _, _, err := New(wire, "vendor/embed", nil).Embed(context.Background(), []string{"ok"})
	if err == nil || !strings.Contains(err.Error(), "empty vector") {
		t.Fatalf("empty success must be refused, got %v", err)
	}
}

func TestAvailableDoesNotPrintTheKey(t *testing.T) {
	secret := "sk-or-v1-not-a-real-key"
	client := New(&scriptedWire{}, "openai/text-embedding-3-small", nil).WithSecret(secret)
	model, ok, err := client.Available(context.Background())
	if err != nil || !ok || model != "openai/text-embedding-3-small" {
		t.Fatalf("Available = %q %v %v", model, ok, err)
	}
	leaked := redact(errors.New("authorization Bearer "+secret), secret)
	if strings.Contains(leaked.Error(), secret) {
		t.Fatalf("key printed: %v", leaked)
	}
	if _, ok, _ := (*Client)(nil).Available(context.Background()); ok {
		t.Fatal("a nil client is not available")
	}
}

func TestResolveModelPrefersThePin(t *testing.T) {
	src := func(key string) (string, bool) {
		if key == roles.PinKey(roles.RoleEmbed) {
			return "qwen/qwen3-embedding-8b", true
		}
		return "", false
	}
	if got := ResolveModel(src, nil); got != "qwen/qwen3-embedding-8b" {
		t.Fatalf("pin resolved to %q", got)
	}
	if got := ResolveModel(nil, nil); got != config.FallbackMediaModel("embeddings") {
		t.Fatalf("no pin resolved to %q, want the Spark-inspected fallback %q", got, config.FallbackMediaModel("embeddings"))
	}
}

func TestTheLexicalFallbackIsDegradedNotAReplacement(t *testing.T) {
	got := DegradedLexical("the receipts and the ledger")
	if got.Mode != LabelDegraded {
		t.Fatalf("mode = %q, want %s", got.Mode, LabelDegraded)
	}
	if got.Detail != LabelDelayed {
		t.Fatalf("detail = %q, want %s", got.Detail, LabelDelayed)
	}
	if strings.Contains(got.Mode, Checked) || strings.Contains(got.Detail, Checked) {
		t.Fatalf("degraded path claimed checked: %+v", got)
	}
	joined := strings.Join(got.Terms, " ")
	if !strings.Contains(joined, "receipt") || !strings.Contains(joined, "ledger") {
		t.Fatalf("expansion missed the query: %v", got.Terms)
	}
	if len(got.Terms) == 0 {
		t.Fatal("expansion produced nothing")
	}
}

func TestRoleEmbedIsNotRegisteredAndNotTheAuditor(t *testing.T) {
	if _, ok := roles.TierOf(roles.RoleEmbed); ok {
		t.Fatal("RoleEmbed must stay a pin")
	}
	if roles.RoleEmbed == roles.RoleAuditor {
		t.Fatal("do not route embed through RoleAuditor")
	}
	if roles.PinKey(roles.RoleEmbed) != "roles.embed" {
		t.Fatalf("PinKey = %q", roles.PinKey(roles.RoleEmbed))
	}
}
