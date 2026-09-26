package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The two seams a delegated run's model API reaches this package through
// (internal/provider/modelapi): the account pool's own "can a service answer
// this model" test, and a call that keeps the program's own cache lineage.

// SERVESMODEL IS THE POOL'S OWN TEST, NOT A SECOND ONE: the service a model's
// id resolves to — a prefix read off it — holds a key, or needs none.
func TestServesModelIsThePoolsOwnTest(t *testing.T) {
	router := modelsource.DefaultSource("https://openrouter.ai/api/v1")
	proxy := modelsource.Source{ID: modelsource.CustomID, Written: "mybox", Name: "mybox", Address: "http://127.0.0.1:9000/v1"}
	local := modelsource.Source{ID: "custom-ollama", Written: "ollama", Name: "ollama", Address: "http://127.0.0.1:11434/v1", KeyOptional: true}
	keyless := modelsource.NewSet(
		modelsource.Connected{Source: router, Address: router.Address},
		modelsource.Connected{Source: proxy, Key: "local", Address: proxy.Address},
		modelsource.Connected{Source: local, Address: local.Address},
	)
	keyed := modelsource.NewSet(modelsource.Connected{Source: router, Key: "sk-or-v1-routerkey0000000000", Address: router.Address})
	for _, row := range []struct {
		sources modelsource.Set
		model   string
		want    bool
	}{
		{keyless, "deepseek/deepseek-v4-flash-0731", false},
		{keyless, "openrouter/deepseek/deepseek-v4-flash-0731", false},
		{keyless, "mybox/qwen3-coder", true},
		{keyless, "ollama/llama4", true},
		{keyed, "deepseek/deepseek-v4-flash-0731", true},
		{keyed, "openrouter/deepseek/deepseek-v4-flash-0731", true},
		{keyed, "", false},
		{modelsource.Set{}, "deepseek/deepseek-v4-flash-0731", false},
	} {
		if got := ServesModel(row.sources, row.model); got != row.want {
			t.Errorf("ServesModel(%q) = %v, want %v", row.model, got, row.want)
		}
	}
	// And it agrees with the pool: a model the pool would move to the seat is
	// exactly one this answers no for.
	pool := &modelClientPool{config: Config{Sources: keyless}, seat: "mybox/qwen3-coder"}
	if seated := pool.seatedModel("deepseek/deepseek-v4-flash-0731"); seated != "mybox/qwen3-coder" || ServesModel(keyless, "deepseek/deepseek-v4-flash-0731") {
		t.Fatalf("the pool seated %q and ServesModel disagrees with it", seated)
	}
}

// keyCapture is a completer that remembers the cache key each call carried.
type keyCapture struct{ keys *[]string }

func (c keyCapture) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	*c.keys = append(*c.keys, provider.CacheKeyFrom(ctx))
	return &ai.Response{}, nil
}

// A CALL MARKED AS BRINGING ITS OWN LINEAGE KEEPS IT, and nothing else
// changes: an unmarked call, and a marked one that carries no key, are
// stamped with the conversation's key exactly as before.
func TestAMarkedCallKeepsItsOwnCacheLineage(t *testing.T) {
	var keys []string
	wrapper := sessionCompleter{inner: keyCapture{keys: &keys}, cacheKey: "conversation"}
	program := provider.WithCacheKey(context.Background(), "program-thread")
	for _, ctx := range []context.Context{
		WithOwnCacheLineage(program),
		program,
		WithOwnCacheLineage(context.Background()),
		context.Background(),
	} {
		if _, err := wrapper.CompleteWithMessages(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"program-thread", "conversation", "conversation", "conversation"}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("keys = %q, want %q", keys, want)
		}
	}
}
