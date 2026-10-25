package session

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	preferences "github.com/Agent-Field/codeaf/internal/config"
)

// settingsApprovalGate reads a real settings profile through the gate interface.
// The production adapter also resolves project rules and conversation postures.
type settingsApprovalGate struct{ profile string }

func (g settingsApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	value, err := preferences.ProjectStringAt("", g.profile, preferences.KeyToolApprovalMode)
	if err != nil {
		return nil, false, err
	}
	mode := approval.Action(value)
	if posture == PostureAllow {
		mode = approval.ActionAllow
	}
	return &approval.Policy{Default: mode}, false, nil
}

func TestApprovalRefreshRetainsPolicyAfterProfileError(t *testing.T) {
	profile := t.TempDir()
	path := filepath.Join(profile, "config.json")
	if err := os.WriteFile(path, []byte(`{"tools.approvalMode":"prompt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := settingsApprovalGate{profile: profile}
	policy, _, err := gate.Build("")
	if err != nil {
		t.Fatal(err)
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("broken", "bash", `{"command":"printf retained"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			if err := os.WriteFile(path, []byte(`{"tools.approvalMode":"allow"}`), 0o600); err != nil {
				return nil, err
			}
			return toolResponse("repaired", "bash", `{"command":"printf repaired"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalGate, config.ApprovalPolicy, config.AskConsent = gate, policy, true
	})
	if err := os.WriteFile(path, []byte(`{"tools.approvalMode":`), 0o600); err != nil {
		t.Fatal(err)
	}
	asked := map[string]bool{}
	drainAnswering(t, mustSubmit(t, agent, "run both commands"), func(event Event) {
		asked[event.CallID] = true
		agent.ResolveConsent(event.ID, true)
	})
	if !asked["broken"] || asked["repaired"] {
		t.Fatalf("approval requests = %v; want retained prompt during error and allow after repair", asked)
	}
}

type pausedApprovalGate struct {
	ApprovalGate
	entered, release chan struct{}
	once             sync.Once
}

// interleavedApprovalGate holds an old settings read while a newer refresh
// publishes its policy. Only the first build waits, so refreshes can overlap.
type interleavedApprovalGate struct {
	ApprovalGate
	entered, release chan struct{}
	builds           atomic.Int32
}

func (g *interleavedApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	policy, guardian, err := g.ApprovalGate.Build(posture)
	if g.builds.Add(1) == 1 {
		close(g.entered)
		<-g.release
	}
	return policy, guardian, err
}

func TestApprovalRefreshDoesNotPublishOlderSettings(t *testing.T) {
	profile := t.TempDir()
	row, _ := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovalMode)
	if err := row.Apply("allow"); err != nil {
		t.Fatal(err)
	}
	gate := &interleavedApprovalGate{
		ApprovalGate: settingsApprovalGate{profile: profile},
		entered:      make(chan struct{}), release: make(chan struct{}),
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("command", "bash", `{"command":"printf refresh"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.ApprovalGate = gate
		config.AskConsent = true
	})
	events := mustSubmit(t, agent, "run the command")
	<-gate.entered
	if err := row.Apply("prompt"); err != nil {
		t.Fatal(err)
	}
	err := agent.RebuildApprovalGate()
	close(gate.release)
	if err != nil {
		t.Fatal(err)
	}
	collected := drainAnswering(t, events, func(event Event) { agent.ResolveConsent(event.ID, true) })
	if countKind(collected, EventConsentRequest) != 1 {
		t.Fatal("an older allow refresh bypassed the newer prompt policy")
	}
}

func (g *pausedApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	policy, guardian, err := g.ApprovalGate.Build(posture)
	if posture == "" {
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	}
	return policy, guardian, err
}

func TestApprovalRefreshDoesNotUndoANewerConversationPosture(t *testing.T) {
	profile := t.TempDir()
	row, _ := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovalMode)
	if err := row.Apply("prompt"); err != nil {
		t.Fatal(err)
	}
	gate := &pausedApprovalGate{
		ApprovalGate: settingsApprovalGate{profile: profile},
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("command", "bash", `{"command":"printf posture"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = promptAll()
		config.ApprovalGate = gate
		config.AskConsent = true
	})
	events := mustSubmit(t, agent, "run the command")
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("approval refresh did not reach settings")
	}
	if err := agent.SetApprovalPosture(PostureAllow); err != nil {
		t.Fatal(err)
	}
	close(gate.release)
	collected := drainAnswering(t, events, func(event Event) { agent.ResolveConsent(event.ID, true) })
	if countKind(collected, EventConsentRequest) != 0 {
		t.Fatal("an older settings refresh undid the conversation's new allow posture")
	}
}

func (g settingsApprovalGate) Standing() string {
	if preferences.ToolApprovalModeAt(g.profile) == "allow" {
		return PostureAllow
	}
	return PostureAsk
}

func TestApprovalModeChangesBeforeTheNextToolCall(t *testing.T) {
	for _, initial := range []string{"allow", "prompt"} {
		t.Run(initial, func(t *testing.T) {
			gate := settingsApprovalGate{profile: t.TempDir()}
			row, ok := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: gate.profile}).Row(preferences.KeyToolApprovalMode)
			if !ok {
				t.Fatal("approval mode setting is missing")
			}
			if err := row.Apply(initial); err != nil {
				t.Fatal(err)
			}
			next := "prompt"
			if initial == "prompt" {
				next = "allow"
			}
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return toolResponse("first", "bash", `{"command":"printf first"}`), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					// The first command has finished. Change the same row Settings
					// saves before returning the next command in this running turn.
					if err := row.Apply(next); err != nil {
						return nil, err
					}
					return toolResponse("second", "bash", `{"command":"printf second"}`), nil
				},
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("done"), nil
				},
			}}
			policy, _, _ := gate.Build("")
			agent, _ := newTestAgent(t, completer, func(config *Config) {
				config.ApprovalPolicy = policy
				config.ApprovalGate = gate
				config.AskConsent = true
			})
			asked := map[string]bool{}
			events := drainAnswering(t, mustSubmit(t, agent, "run both commands"), func(event Event) {
				asked[event.CallID] = true
				agent.ResolveConsent(event.ID, true)
			})
			if asked["first"] != (initial == "prompt") || asked["second"] != (next == "prompt") {
				t.Fatalf("changed %s -> %s during a running turn: approval requests = %v", initial, next, asked)
			}
			if got := countKind(events, EventToolEnd); got != 2 {
				t.Fatalf("finished commands = %d, want both commands to run", got)
			}
		})
	}
}
