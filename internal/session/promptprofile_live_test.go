package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

func TestAutomaticProfileFollowsModelSwitchOnNextRequest(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	var agent *Agent
	wantLean := true
	script := &scriptedCompleter{}
	check := func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		if got := agent.config.promptProfile().lean(); got != wantLean {
			t.Errorf("lean=%v, want %v", got, wantLean)
		}
		if hasSection := strings.Contains(messageContentText(messages[0]), "# Interrupts and steering"); hasSection == wantLean {
			t.Error("prompt did not change with the tool belt")
		}
		if hasTask := agent.hasTool("propose_task"); hasTask == wantLean {
			t.Error("default task schema did not follow the profile")
		}
		return textResponse("done"), nil
	}
	script.steps = []step{check, check}
	agent, _ = newTestAgent(t, script, func(c *Config) {
		buildShippedConversation(t, c)
		c.Memory = nil
		c.System = ""
		c.Model = "test/large"
		c.ContextWindowFor = func(model string) int {
			if model == "test/small" {
				return 16385
			}
			return 128000
		}
	})
	if agent.config.promptProfile().lean() {
		t.Fatal("fixture did not start full")
	}
	for _, model := range []string{"test/small", "test/large"} {
		wantLean = model == "test/small"
		agent.SetModel(model)
		for _, event := range collect(t, mustSubmit(t, agent, "hello")) {
			if event.Kind == EventError {
				t.Fatal(event.Err)
			}
		}
	}
}

func TestAutomaticProfilePreservesLoadedCapabilities(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	agent := leanShapedAgent(t)
	if _, failed := agent.loadCapability("tasks"); failed {
		t.Fatal("load tasks")
	}
	agent.SetContextWindow(128000)
	if err := agent.preparePromptProfile(agent.Model()); err != nil {
		t.Fatal(err)
	}
	if agent.config.promptProfile().lean() || !agent.hasTool("propose_task") || !agent.remembers() {
		t.Fatal("full profile did not restore capabilities and memory")
	}
	agent.SetContextWindow(16385)
	if err := agent.preparePromptProfile(agent.Model()); err != nil {
		t.Fatal(err)
	}
	if !agent.config.promptProfile().lean() || !agent.hasTool("propose_task") || agent.remembers() || agent.hasTool("remember") {
		t.Fatal("lean profile lost explicitly loaded tasks or retained memory")
	}
}

func TestAutomaticProfileHonorsExplicitPins(t *testing.T) {
	for _, pin := range []string{"lean", "full"} {
		t.Run(pin, func(t *testing.T) {
			t.Setenv(promptProfileEnv, pin)
			agent := leanShapedAgent(t)
			agent.SetContextWindow(128000)
			if err := agent.preparePromptProfile(agent.Model()); err != nil {
				t.Fatal(err)
			}
			if got := string(agent.config.promptProfile()); got != pin {
				t.Fatalf("profile=%s", got)
			}
			agent.SetContextWindow(6000)
			if err := agent.preparePromptProfile(agent.Model()); err != nil {
				t.Fatal(err)
			}
			if got := string(agent.config.promptProfile()); got != pin {
				t.Fatalf("profile=%s", got)
			}
		})
	}
}

func TestAutomaticProfileSwitchDuringToolTurn(t *testing.T) {
	t.Setenv(promptProfileEnv, "")
	var agent *Agent
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("choose-1", "choose", `{}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !agent.config.promptProfile().lean() || strings.Contains(messageContentText(messages[0]), "# Interrupts and steering") {
				t.Error("second request kept the large model's profile")
			}
			assertPaired(t, messages)
			if !agent.hasTool("choose") {
				t.Error("model switch dropped a dynamically armed tool")
			}
			return textResponse("done"), nil
		},
	}}
	agent, _ = newTestAgent(t, script, func(c *Config) {
		c.System = ""
		c.ContextWindowFor = func(model string) int {
			if model == "test/small" {
				return 6144
			}
			return 128000
		}
	})
	changes := 0
	_, err := agent.armFamily([]bare.Tool{{Name: "choose", Description: "selects the smaller model", Schema: json.RawMessage(`{"type":"object"}`), Execute: func(context.Context, json.RawMessage) (string, bool, error) {
		changes++
		agent.SetModel("test/small")
		return "selected", false, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range collect(t, mustSubmit(t, agent, "choose")) {
		if event.Kind == EventError {
			t.Fatal(event.Err)
		}
	}
	if changes != 1 || script.requests() != 2 {
		t.Fatalf("changes=%d requests=%d", changes, script.requests())
	}
}
