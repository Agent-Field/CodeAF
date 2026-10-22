package session

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/store"
)

func rememberToolV2(t *testing.T, agent *Agent) (func(context.Context, json.RawMessage) (string, bool, error), json.RawMessage) {
	t.Helper()
	for _, tool := range agent.memoryTools() {
		if tool.Name == "remember" {
			return tool.Execute, tool.Schema
		}
	}
	t.Fatal("writable conversation has no remember tool")
	return nil, nil
}

func TestRememberToolV2ForwardsOptionalDurability(t *testing.T) {
	cases := []struct{ name, durability, text, want string }{
		{"standing rule", "standing_rule", "The preview runner uses isolated environments", store.MemoryPreference},
		{"decision", "decision", "The service stores job receipts in sqlite", store.MemoryDecision},
		{"project progress", "project_state", "The preview smoke run reached checkpoint nine", store.MemoryProjectState},
		{"explicit durable fact", "durable_fact", "Always route the mock endpoint through the amber proxy", store.MemoryFact},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "mock-project" })
			agent.memory.typed.Store(true)
			execute, _ := rememberToolV2(t, agent)
			args, _ := json.Marshal(map[string]string{"text": tc.text, "durability": tc.durability})
			out, isErr, err := execute(context.Background(), args)
			if err != nil || isErr {
				t.Fatalf("remember failed: %q %v %v", out, isErr, err)
			}
			rows, err := brain.MemoriesFor([]string{store.OwnerProject("mock-project")})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Text != tc.text || rows[0].Type != tc.want || rows[0].Origin != store.OriginModel {
				t.Fatalf("durability not forwarded into stored memory: %+v", rows)
			}
		})
	}
}

func TestRememberToolV2OldCallsKeepTypeAndScopeDefaults(t *testing.T) {
	cases := []struct{ text, want string }{{"The mock fixture uses amber dispatch queues", store.MemoryFact}, {"Always verify the mock endpoint before dispatch", store.MemoryPreference}}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "mock-project" })
			agent.memory.typed.Store(true)
			execute, _ := rememberToolV2(t, agent)
			args, _ := json.Marshal(map[string]string{"text": tc.text})
			_, isErr, err := execute(context.Background(), args)
			if err != nil || isErr {
				t.Fatalf("legacy call rejected: %v %v", isErr, err)
			}
			rows, err := brain.MemoriesFor([]string{store.OwnerProject("mock-project")})
			if err != nil || len(rows) != 1 || rows[0].Type != tc.want {
				t.Fatalf("legacy type/scope changed: %+v %v", rows, err)
			}
		})
	}
}

func TestRememberToolV2RejectsEphemeralAndUnknownDurability(t *testing.T) {
	for _, durability := range []string{"ephemeral", "unsupported-kind"} {
		t.Run(durability, func(t *testing.T) {
			agent, brain := brainAgent(t, &reflexScript{}, func(c *Config) { c.MemoryProjectKey = "mock-project" })
			agent.memory.typed.Store(true)
			execute, _ := rememberToolV2(t, agent)
			args, _ := json.Marshal(map[string]string{"text": "Run the mock check once for this turn", "durability": durability})
			_, isErr, err := execute(context.Background(), args)
			if err != nil || !isErr {
				t.Fatalf("invalid durability needs an actionable tool rejection: isErr=%v err=%v", isErr, err)
			}
			rows, err := brain.ListMemories(nil, 0)
			if err != nil || len(rows) != 0 {
				t.Fatalf("ephemeral or invalid capture persisted: %+v %v", rows, err)
			}
		})
	}
}

func TestRememberToolV2SchemaKeepsDurabilityOptionalAndDurableOnly(t *testing.T) {
	agent, _ := brainAgent(t, &reflexScript{}, nil)
	_, raw := rememberToolV2(t, agent)
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	field, ok := schema.Properties["durability"]
	if !ok || field.Type != "string" {
		t.Fatal("model cannot name capture durability")
	}
	want := []string{"standing_rule", "decision", "durable_fact", "project_state"}
	slices.Sort(field.Enum)
	slices.Sort(want)
	if !slices.Equal(field.Enum, want) {
		t.Fatalf("durability schema=%v want=%v", field.Enum, want)
	}
	if slices.Contains(schema.Required, "durability") {
		t.Fatal("legacy remember calls require a new field")
	}
	if !slices.Contains(schema.Required, "text") {
		t.Fatal("remember no longer requires the words to keep")
	}
}
