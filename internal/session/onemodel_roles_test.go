package session

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/store"
)

// elsewhereSource is a roles source that names another model for every pin
// and every tier, the shape a profile with a full crew has. Under one model
// none of it may be reached.
func elsewhereSource(key string) (string, bool) { return "elsewhere/" + key, true }

// oneModelAgent is a one-model conversation whose config still carries a
// profile's crew rows, which is what any door that forgets to withhold them
// hands the session.
func oneModelAgent(t *testing.T, c Completer) *Agent {
	t.Helper()
	a, _ := newTestAgent(t, c, func(cfg *Config) {
		cfg.OneModel = true
		cfg.RolesSource = elsewhereSource
	})
	return a
}

// sentModel records the model each request went out on.
func sentModel(sent *[]string) *replyCompleter {
	return &replyCompleter{answer: func(_ context.Context, _ []ai.Message, req ai.Request) (*ai.Response, error) {
		*sent = append(*sent, req.Model)
		return textResponse("ok"), nil
	}}
}

// EVERY REGISTERED TEXT ROLE RIDES THE CONVERSATION'S MODEL UNDER ONE MODEL.
// The registry is enumerated, so a role added tomorrow is covered the day it
// registers. Media roles are capability-qualified and stay out ([roles.IsMedia]).
func TestOneModelEveryRegisteredRoleResolvesToTheConversationModel(t *testing.T) {
	for _, role := range roles.Registered() {
		if roles.IsMedia(role) {
			continue
		}
		a := oneModelAgent(t, sentModel(new([]string)))
		for _, floor := range []string{"", "floor/model"} {
			rungs, err := a.roleLadder(role, floor)
			if err != nil || len(rungs) != 1 || rungs[0].Model != "test/model" {
				t.Errorf("%s (floor %q) resolved to %+v, %v; want only test/model", role, floor, rungs, err)
			}
		}
	}
}

// AND EVERY ERRAND GOES OUT ON IT. The mark reader is the crew-only role the
// run d6d968960c795fc7 sent to the mastermind tier's model under --one-model.
func TestOneModelErrandsGoOutOnTheConversationModel(t *testing.T) {
	for _, role := range roles.Registered() {
		if roles.IsMedia(role) {
			continue
		}
		var sent []string
		a := oneModelAgent(t, sentModel(&sent))
		_, model, err := a.callRole(t.Context(), role, "", []ai.Message{textMessage("user", "hi")})
		if err != nil {
			t.Errorf("%s: %v", role, err)
			continue
		}
		if model != "test/model" || len(sent) != 1 || sent[0] != "test/model" {
			t.Errorf("%s answered on %q, requests went to %v; want test/model only", role, model, sent)
		}
	}
}

// THE REFLEX, which builds its own bound client, rides it too: the run sent two
// reflex calls to the reflex tier's model under --one-model.
func TestOneModelReflexGoesOutOnTheConversationModel(t *testing.T) {
	var sent []string
	brain, err := store.Open(filepath.Join(t.TempDir(), "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	a, _ := newTestAgent(t, sentModel(&sent), func(cfg *Config) {
		cfg.OneModel, cfg.RolesSource, cfg.Memory = true, elsewhereSource, brain
	})
	client := a.reflexClient()
	if client == nil {
		t.Fatal("no reflex client under one model")
	}
	if _, err := client.CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "test/model" {
		t.Fatalf("reflex requests went to %v; want test/model only", sent)
	}
}
