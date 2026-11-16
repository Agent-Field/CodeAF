package remote

import (
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// C1, C4, C5: the linked-local wire distinguishes temporary and explicit picks.
func TestRunOnlyModelCrossesTheWireWithoutReplacingTheSavedPair(t *testing.T) {
	dir := t.TempDir()
	a, err := session.New(session.Config{Workspace: t.TempDir(), Place: session.Place{Dir: dir}, Model: "vendor/configured", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1",
		ConversationModels: &session.ConversationModels{Main: "vendor/configured", Fallback: "vendor/backup"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: a}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	agent := loop.Client.Agent()
	for _, model := range []string{"available/model", "vendor/configured", "available/model"} {
		if err := agent.UseModelForNow(model); err != nil {
			t.Fatal(err)
		}
		if agent.Model() != model || agent.ConfiguredModel() != "vendor/configured" {
			t.Fatalf("live=%q configured=%q", agent.Model(), agent.ConfiguredModel())
		}
		a.SettleWrites()
		meta, err := session.LoadMeta(dir)
		if err != nil || meta.Model != "vendor/configured" || meta.ModelIdentity == nil || meta.ModelIdentity.Models == nil || meta.ModelIdentity.Models.Main != "vendor/configured" || meta.ModelIdentity.Models.Fallback != "vendor/backup" {
			t.Fatalf("saved pair=%+v identity=%+v error=%v", meta, meta.ModelIdentity, err)
		}
	}
	agent.SetModel("available/model")
	a.SettleWrites()
	meta, err := session.LoadMeta(dir)
	if err != nil || meta.Model != "available/model" || agent.ConfiguredModel() != "available/model" {
		t.Fatalf("explicit pick=%+v error=%v", meta, err)
	}
}

// C6: an engine without the additive door retains the old setter behavior.
func TestRunOnlyModelFallsBackToTheOlderEngineSetter(t *testing.T) {
	for _, refusal := range []string{fmt.Sprintf("engine: no such method %q", MethodUseModelForNow), modelForNowUnsupported} {
		t.Run(refusal, func(t *testing.T) {
			client, engine := newEngine(t)
			engine.fails[MethodUseModelForNow] = refusal
			engine.answers[MethodSetModel] = nil
			if err := client.Agent().UseModelForNow("available/model"); err != nil {
				t.Fatal(err)
			}
			if client.Agent().Model() != "available/model" {
				t.Fatal("older engine setter was not used")
			}
			if len(engine.calls(MethodUseModelForNow)) != 1 || len(engine.calls(MethodSetModel)) != 1 || client.CallsMade() != 2 {
				t.Fatal("older engine did not receive the temporary request followed by SetModel")
			}
		})
	}
}

// C2, C6: other failures must never turn a temporary pick into a durable one.
func TestRunOnlyModelFailureDoesNotCallThePersistentSetter(t *testing.T) {
	client, engine := newEngine(t)
	engine.fails[MethodUseModelForNow] = "connection unavailable"
	if err := client.Agent().UseModelForNow("available/model"); err == nil {
		t.Fatal("failed selection reported success")
	}
	if len(engine.calls(MethodUseModelForNow)) != 1 || len(engine.calls(MethodSetModel)) != 0 {
		t.Fatal("a failed temporary request reached SetModel")
	}
}
