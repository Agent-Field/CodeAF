package session

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	preferences "github.com/Agent-Field/codeaf/internal/config"
)

// profileToolApprovalGate is a profile-backed ApprovalGate matching
// cmd/codeaf's v3ApprovalGate caching semantics: it reuses the built Policy
// while the profile's tool-approval rows are unchanged and rebuilds when they
// move.
type profileToolApprovalGate struct {
	profile string
	mu      sync.Mutex
	mode    string
	tools   string
	policy  *approval.Policy
}

func (g *profileToolApprovalGate) Build(posture string) (*approval.Policy, bool, error) {
	mode := preferences.ToolApprovalModeAt(g.profile)
	if mode == "" {
		mode = string(approval.ActionPrompt)
	}
	if posture == PostureAllow {
		mode = string(approval.ActionAllow)
	}
	rawTools := preferences.ToolApprovalsAt(g.profile)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.policy != nil && g.mode == mode && g.tools == rawTools {
		return g.policy, false, nil
	}
	parsed, err := preferences.ParseToolApprovals(rawTools)
	if err != nil {
		return nil, false, err
	}
	tools := make(map[string]approval.Action, len(parsed))
	for name, action := range parsed {
		tools[name] = approval.Action(action)
	}
	g.mode, g.tools = mode, rawTools
	g.policy = &approval.Policy{
		Default: approval.Action(mode),
		Tools:   tools,
	}
	return g.policy, false, nil
}

func (g *profileToolApprovalGate) Standing() string {
	if preferences.ToolApprovalModeAt(g.profile) == "allow" {
		return PostureAllow
	}
	return PostureAsk
}

// #2677: After banking `always, this tool` for `write` and then dropping that
// rule in `/permissions`, the very next `write` in that conversation (and in
// any other conversation on the same engine) asks again instead of riding the
// stale in-memory session memo, and does not write the file until allowed.
func TestDroppingABankedAlwaysRuleMakesTheNextToolDecisionAskAgain_2677(t *testing.T) {
	t.Run("engine_approval_gate", func(t *testing.T) {
		profile := t.TempDir()
		row, ok := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovalMode)
		if !ok {
			t.Fatal("missing tools.approvalMode setting")
		}
		if err := row.Apply("prompt"); err != nil {
			t.Fatal(err)
		}
		toolsRow, ok := preferences.NewSettings(preferences.SettingsOptions{ProfileDir: profile}).Row(preferences.KeyToolApprovals)
		if !ok {
			t.Fatal("missing tools.approval setting")
		}

		makeAgent := func(completer Completer) (*Agent, string) {
			gate := &profileToolApprovalGate{profile: profile}
			initial, _, err := gate.Build("")
			if err != nil {
				t.Fatal(err)
			}
			return newTestAgent(t, completer, func(cfg *Config) {
				cfg.ApprovalPolicy = initial
				cfg.ApprovalGate = gate
				cfg.AskConsent = true
			})
		}

		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("w1", "write", `{"path":"ask1.txt","content":"hello\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("wrote ask1"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("w2", "write", `{"path":"ask2.txt","content":"after-drop\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("wrote ask2"), nil
			},
		}}
		agent, workspace := makeAgent(completer)

		otherCompleter := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("other-w1", "write", `{"path":"other.txt","content":"x\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("other done"), nil
			},
		}}
		otherAgent, otherWorkspace := makeAgent(otherCompleter)

		// Turn 1: bank `always, this tool` for `write`, exactly as tui3 does
		// (persist `write:allow` to profile, then resolve with ConsentToolSession).
		turn1 := drainAnswering(t, mustSubmit(t, agent, "write ask1"), func(req Event) {
			if err := preferences.RememberToolApproval(profile, "write", "allow"); err != nil {
				t.Errorf("RememberToolApproval: %v", err)
			}
			agent.ResolveConsentRemember(req.ID, true, ConsentToolSession)
		})
		if got := countKind(turn1, EventConsentRequest); got != 1 {
			t.Fatalf("turn 1 asked %d times, want 1", got)
		}
		if body, err := os.ReadFile(filepath.Join(workspace, "ask1.txt")); err != nil || string(body) != "hello\n" {
			t.Fatalf("turn 1 ask1.txt = %q, err = %v; want %q", string(body), err, "hello\n")
		}

		// Drop `write` in `/permissions` using the same registry Apply call the
		// surface's dropPermission makes (even with no intermediate tool call).
		if err := toolsRow.Apply(""); err != nil {
			t.Fatalf("drop write rule: %v", err)
		}

		// Turn 2: the very next `write` in the same conversation must ask again,
		// must not have written the file before approval, and writes it once allowed.
		ask2Path := filepath.Join(workspace, "ask2.txt")
		existedBeforeApproval := true
		turn2 := drainAnswering(t, mustSubmit(t, agent, "write ask2"), func(req Event) {
			if _, err := os.Stat(ask2Path); os.IsNotExist(err) {
				existedBeforeApproval = false
			}
			agent.ResolveConsent(req.ID, true)
		})
		if got := countKind(turn2, EventConsentRequest); got != 1 {
			t.Fatalf("after dropping write in /permissions, turn 2 asked %d times, want 1", got)
		}
		if existedBeforeApproval {
			t.Fatal("ask2.txt was written before consent was granted")
		}
		if body, err := os.ReadFile(ask2Path); err != nil || string(body) != "after-drop\n" {
			t.Fatalf("turn 2 ask2.txt after approval = %q, err = %v; want %q", string(body), err, "after-drop\n")
		}

		// And another conversation on the same engine also asks on its next write
		// and does not write the file when denied.
		otherPath := filepath.Join(otherWorkspace, "other.txt")
		otherTurn := drainAnswering(t, mustSubmit(t, otherAgent, "write other"), func(req Event) {
			otherAgent.ResolveConsent(req.ID, false)
		})
		if got := countKind(otherTurn, EventConsentRequest); got != 1 {
			t.Fatalf("other conversation on the same engine asked %d times, want 1", got)
		}
		if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
			t.Fatalf("other conversation wrote %s despite denial (err=%v)", otherPath, err)
		}
	})

	t.Run("pushed_policy_no_host", func(t *testing.T) {
		completer := &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("w1", "write", `{"path":"ask1.txt","content":"hello\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("wrote ask1"), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("w2", "write", `{"path":"ask2.txt","content":"after-drop\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("wrote ask2"), nil
			},
		}}
		agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
			cfg.ApprovalPolicy = promptAll()
			cfg.AskConsent = true
		})

		// Bank `always, this tool` for `write` and push the updated policy.
		turn1 := drainAnswering(t, mustSubmit(t, agent, "write ask1"), func(req Event) {
			agent.SetApprovalPolicy(&approval.Policy{
				Default: approval.ActionPrompt,
				Tools:   map[string]approval.Action{"write": approval.ActionAllow},
			})
			agent.ResolveConsentRemember(req.ID, true, ConsentToolSession)
		})
		if got := countKind(turn1, EventConsentRequest); got != 1 {
			t.Fatalf("turn 1 asked %d times, want 1", got)
		}
		if body, err := os.ReadFile(filepath.Join(workspace, "ask1.txt")); err != nil || string(body) != "hello\n" {
			t.Fatalf("turn 1 ask1.txt = %q, err = %v; want %q", string(body), err, "hello\n")
		}

		// Drop `write` in `/permissions` and push the updated policy (`--no-host`).
		agent.SetApprovalPolicy(promptAll())

		// Next `write` must ask again, must not exist before approval, and writes once allowed.
		ask2Path := filepath.Join(workspace, "ask2.txt")
		existedBeforeApproval := true
		turn2 := drainAnswering(t, mustSubmit(t, agent, "write ask2"), func(req Event) {
			if _, err := os.Stat(ask2Path); os.IsNotExist(err) {
				existedBeforeApproval = false
			}
			agent.ResolveConsent(req.ID, true)
		})
		if got := countKind(turn2, EventConsentRequest); got != 1 {
			t.Fatalf("after dropping write under --no-host, turn 2 asked %d times, want 1", got)
		}
		if existedBeforeApproval {
			t.Fatal("ask2.txt was written before consent was granted")
		}
		if body, err := os.ReadFile(ask2Path); err != nil || string(body) != "after-drop\n" {
			t.Fatalf("turn 2 ask2.txt after approval = %q, err = %v; want %q", string(body), err, "after-drop\n")
		}
	})
}
