package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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

// controlledApprovalGate holds each settings snapshot until the test releases
// its Build. Channels order the callers and completions without sleeps.
type controlledApprovalGate struct {
	ApprovalGate
	entered chan chan struct{}
}

func (g *controlledApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	policy, guardian, err := g.ApprovalGate.Build(posture)
	release := make(chan struct{})
	g.entered <- release
	<-release
	return policy, guardian, err
}

func TestApprovalBuildDoesNotBlockStoppingWork(t *testing.T) {
	for _, postureChange := range []bool{false, true} {
		build := "refresh"
		if postureChange {
			build = "posture"
		}
		for _, operation := range []string{"Interrupt", "StopWork"} {
			t.Run(build+"/"+operation, func(t *testing.T) {
				gate := &controlledApprovalGate{
					ApprovalGate: &fakeApprovalGate{},
					entered:      make(chan chan struct{}),
				}
				ctx, cancel := context.WithCancelCause(context.Background())
				defer cancel(nil)
				agent := &Agent{config: Config{ApprovalGate: gate}, cancel: cancel}
				built := make(chan error, 1)
				go func() {
					if postureChange {
						built <- agent.SetApprovalPosture(PostureAsk)
					} else {
						built <- agent.RebuildApprovalGate()
					}
				}()
				release := <-gate.entered
				defer func() {
					close(release)
					if err := <-built; err != nil {
						t.Errorf("approval build: %v", err)
					}
				}()
				// Fail deterministically instead of hanging in the stop call when
				// Build owns the general mutex. No other goroutine uses it here.
				if !agent.mu.TryLock() {
					t.Fatalf("%s cannot return: blocked approval Build holds Agent.mu", operation)
				}
				agent.mu.Unlock()
				if operation == "Interrupt" {
					agent.Interrupt()
				} else if err := agent.StopWork(); err != nil {
					t.Fatal(err)
				}
				if context.Cause(ctx) == nil {
					t.Fatalf("%s returned without cancelling work while Build remains blocked", operation)
				}
			})
		}
	}
}

func (g *controlledApprovalGate) waitForBuild(t *testing.T, agent *Agent) chan struct{} {
	t.Helper()
	release := <-g.entered
	// These bare agents have no other lock users. This assertion makes a build
	// outside the publication lock fail regardless of contender scheduling.
	if agent.approvalMu.TryLock() {
		agent.approvalMu.Unlock()
		t.Error("approval Build must hold the publication mutex")
	}
	return release
}

func TestApprovalRefreshKeepsPromptWhileReplacementBuilds(t *testing.T) {
	for _, toolFirst := range []bool{true, false} {
		name := "rebuild_finishes_first"
		if toolFirst {
			name = "tool_finishes_first"
		}
		t.Run(name, func(t *testing.T) {
			profile := t.TempDir()
			row, _ := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovalMode)
			initial := "allow"
			if toolFirst {
				initial = "prompt"
			}
			if err := row.Apply(initial); err != nil {
				t.Fatal(err)
			}
			gate := &controlledApprovalGate{
				ApprovalGate: settingsApprovalGate{profile: profile},
				entered:      make(chan chan struct{}),
			}
			agent := &Agent{config: Config{
				ApprovalGate:   gate,
				ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
			}}
			decision := make(chan approval.Decision, 1)
			rebuilt := make(chan error, 1)
			tool := func() {
				got, _ := agent.decide(gateCall("edit", `{"path":"x"}`))
				decision <- got
			}
			rebuild := func() { rebuilt <- agent.RebuildApprovalGate() }
			first, second := tool, rebuild
			if !toolFirst {
				first, second = rebuild, tool
			}
			go first()
			releaseFirst := gate.waitForBuild(t, agent)
			if !toolFirst {
				if err := row.Apply("prompt"); err != nil {
					close(releaseFirst)
					<-rebuilt
					t.Fatal(err)
				}
			}
			requested := make(chan struct{})
			go func() {
				close(requested)
				second()
			}()
			<-requested
			close(releaseFirst)
			releaseSecond := gate.waitForBuild(t, agent)
			if toolFirst {
				// A must return its prompt snapshot while B remains in Build.
				got := <-decision
				close(releaseSecond)
				if err := <-rebuilt; err != nil {
					t.Fatal(err)
				}
				if got.Action != approval.ActionPrompt {
					t.Fatalf("tool decision = %s while replacement still builds; want prompt", got)
				}
			} else {
				// B completes while A remains in Build; A must also ask.
				err := <-rebuilt
				close(releaseSecond)
				got := <-decision
				if err != nil {
					t.Fatal(err)
				}
				if got.Action != approval.ActionPrompt {
					t.Fatalf("tool decision = %s after replacement finished; want prompt", got)
				}
			}
		})
	}
}

func TestApprovalRefreshDoesNotUndoANewerConversationPosture(t *testing.T) {
	for _, postureFirst := range []bool{false, true} {
		name := "refresh_first"
		if postureFirst {
			name = "posture_first"
		}
		t.Run(name, func(t *testing.T) {
			profile := t.TempDir()
			row, _ := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovalMode)
			if err := row.Apply("prompt"); err != nil {
				t.Fatal(err)
			}
			gate := &controlledApprovalGate{
				ApprovalGate: settingsApprovalGate{profile: profile},
				entered:      make(chan chan struct{}),
			}
			agent := &Agent{config: Config{ApprovalGate: gate, ApprovalPolicy: promptAll()}}
			first := agent.RebuildApprovalGate
			second := func() error { return agent.SetApprovalPosture(PostureAllow) }
			if postureFirst {
				first, second = second, first
			}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- first() }()
			releaseFirst := gate.waitForBuild(t, agent)
			requested := make(chan struct{})
			go func() {
				close(requested)
				secondDone <- second()
			}()
			<-requested
			close(releaseFirst)
			releaseSecond := gate.waitForBuild(t, agent)
			close(releaseSecond)
			for _, done := range []chan error{firstDone, secondDone} {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			decision := make(chan approval.Decision, 1)
			go func() {
				got, _ := agent.decide(gateCall("edit", `{"path":"x"}`))
				decision <- got
			}()
			close(gate.waitForBuild(t, agent))
			if got := <-decision; got.Action != approval.ActionAllow {
				t.Fatalf("tool decision = %s; want the conversation's allow override", got)
			}
			if got := agent.ApprovalPosture(); got != PostureAllow {
				t.Fatalf("conversation posture = %q; want allow", got)
			}
		})
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
