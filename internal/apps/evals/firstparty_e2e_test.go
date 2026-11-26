//go:build e2e

package evals

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
)

func TestLiveTalkChangeOnDocsFork(t *testing.T) {
	key := evalKey()
	if key == "" {
		t.Skip("the profile did not resolve an OpenRouter key")
	}
	binary := os.Getenv("CODEAF_APP_EVAL_BINARY")
	if binary == "" {
		t.Skip("set CODEAF_APP_EVAL_BINARY to the built app engine")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 18*time.Minute)
	defer cancel()
	engine, err := launch(ctx, binary, t.TempDir(), key, builderModel)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	shelf, err := readReply[struct{ Apps []store.Summary }](engine.cli(ctx, "list", map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(shelf.Apps, func(a store.Summary) bool { return a.Name == "Docs" })
	if i < 0 {
		t.Fatal("Docs is missing on a fresh profile")
	}
	fork, err := readReply[builder.Proposal](engine.cli(ctx, "fork", map[string]string{"app": shelf.Apps[i].ID}))
	if err != nil || fork.Checkpoint == "" {
		t.Fatalf("fork: %v", err)
	}
	live, err := readReply[struct{ Meta store.Meta }](engine.cli(ctx, "apply", map[string]string{"draft": fork.Draft}))
	if err != nil || live.Meta.ID == shelf.Apps[i].ID {
		t.Fatalf("kept fork: %v", err)
	}
	const ask = "add a status field (Draft, Review, Final) and a review step that asks me before Final"
	change, err := readReply[draftReply](engine.cli(ctx, "change", map[string]string{"app": live.Meta.ID, "request": ask}))
	if err != nil {
		t.Fatal(err)
	}
	addr := map[string]string{"draft": change.Draft}
	if _, err := engine.waitCheck(ctx, addr); err != nil {
		state, _ := readReply[struct {
			State   string
			Problem string
		}](engine.cli(ctx, "build_state", addr))
		t.Fatalf("model change: %v (%s: %s)", err, state.State, state.Problem)
	}
	preview, err := readReply[struct{ App json.RawMessage }](engine.cli(ctx, "preview", addr))
	if err != nil {
		t.Fatal(err)
	}
	def, err := schema.ParseApp(preview.App)
	if err != nil {
		t.Fatal(err)
	}
	field := def.Records["document"].Fields["status"]
	if field.Type != "enum" || !slices.Equal(field.Values, []string{"Draft", "Review", "Final"}) {
		t.Fatalf("model authored status %+v", field)
	}
}
