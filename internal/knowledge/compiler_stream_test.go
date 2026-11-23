package knowledge

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// STRUCTURED OUTPUT NEVER TYPES INTO A CHAT. A job's memory is compiled on a
// context carried over from the turn that wrote it, and that context still has
// the chat's stream observer. A call whose answer is a claims document would
// then draw it in the conversation as if the agent had said it.
func TestMemoryCallsDoNotStreamIntoTheChat(t *testing.T) {
	profile := t.TempDir()
	if !config.MemoryEnabledAt(profile) {
		if err := config.WriteMemoryEnabled(profile, true); err != nil {
			t.Fatal(err)
		}
	}
	heard := 0
	turn := provider.WithStreamObserver(context.Background(), func(provider.StreamEvent) { heard++ })
	for name, calls := range map[string]func(context.Context) (context.Context, error){
		"with a session's calls": func(ctx context.Context) (context.Context, error) { return ctx, nil },
		"on its own":             nil,
	} {
		m := compilerModel{root: filepath.Join(profile, "memory"), calls: calls}
		ctx, err := m.callContext(turn)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		provider.EmitEvent(ctx, provider.StreamEvent{})
	}
	if heard != 0 {
		t.Fatalf("memory output reached the chat's stream %d times", heard)
	}
}
