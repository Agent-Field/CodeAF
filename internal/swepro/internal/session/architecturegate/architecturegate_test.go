package architecturegate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
)

type gatePromptOps struct {
	requests []PromptRequest
	onPrompt func(call int, request PromptRequest) error
}

func (ops *gatePromptOps) ResolvePromptParts(context.Context, string) ([]any, error) {
	return []any{"resolved"}, nil
}

func (ops *gatePromptOps) Prompt(_ context.Context, input any) (any, error) {
	request := input.(PromptRequest)
	ops.requests = append(ops.requests, request)
	if ops.onPrompt != nil {
		return nil, ops.onPrompt(len(ops.requests), request)
	}
	return nil, nil
}

type gateObserver struct {
	tracked   []observer.ObservedSession
	untracked []string
}

func (observer *gateObserver) Track(input observer.ObservedSession) {
	observer.tracked = append(observer.tracked, input)
}
func (observer *gateObserver) Untrack(sessionID string) {
	observer.untracked = append(observer.untracked, sessionID)
}

func baseDependencies(workspace string, prompt *gatePromptOps) Dependencies {
	session := 0
	message := 0
	return Dependencies{
		Models: ModelResolverFunc(func(string) []string { return []string{"provider/model"} }),
		Sessions: SessionCreatorFunc(func(context.Context, string, string) (string, error) {
			session++
			return "session-" + intString(session), nil
		}),
		NewMessageID: func() string {
			message++
			return "message-" + intString(message)
		},
		NowMillis: func() int64 { return 1700000000000 },
	}
}

func TestReviewSchemaStrictAndUTF16Limits(t *testing.T) {
	valid := (ReviewSchema{}).SafeParse([]byte(
		`{"approved":true,"feedback":"good","summary":"ship"}`,
	))
	if !valid.Success() || !valid.Data.Approved {
		t.Fatalf("valid=%#v", valid)
	}
	for _, raw := range []string{
		`{"approved":true,"feedback":"","summary":"ship"}`,
		`{"approved":true,"feedback":"good","summary":"ship","extra":1}`,
		`{"approved":"yes","feedback":"good","summary":"ship"}`,
	} {
		if got := (ReviewSchema{}).SafeParse([]byte(raw)); got.Success() {
			t.Fatalf("invalid review accepted: %s", raw)
		}
	}
}

func TestSinglePassRetriesFreshSessionsAndObserver(t *testing.T) {
	workspace := t.TempDir()
	architecturePath := filepath.Join(workspace, ".codeaf", "plan", "architecture.md")
	prompt := &gatePromptOps{}
	prompt.onPrompt = func(call int, _ PromptRequest) error {
		if call == 2 {
			return os.WriteFile(architecturePath, []byte("# Architecture"), 0o644)
		}
		return nil
	}
	observer := &gateObserver{}
	deps := baseDependencies(workspace, prompt)
	deps.Observer = observer
	result, err := RunArchitectureGate(context.Background(), Input{
		Workspace: workspace, ParentSessionID: "parent", PromptOps: prompt,
		Mode: ModeSinglePass, UserPrompt: "Build it",
	}, deps)
	if err != nil || result.Status != "wrote" || len(prompt.requests) != 2 {
		t.Fatalf("result=%#v err=%v requests=%#v", result, err, prompt.requests)
	}
	if prompt.requests[0].SessionID == prompt.requests[1].SessionID ||
		len(observer.tracked) != 2 || len(observer.untracked) != 2 {
		t.Fatalf("fresh sessions/observer drift: %#v %#v", prompt.requests, observer)
	}
	if observer.tracked[0].TaskSummary != "architecture-gate attempt 1: Build it" {
		t.Fatalf("summary=%q", observer.tracked[0].TaskSummary)
	}
	if observer.tracked[0].SessionID != "session-1" ||
		observer.tracked[0].AgentRole != "architect" ||
		observer.tracked[0].StartedAt != 1700000000000 ||
		observer.tracked[0].Workspace != workspace ||
		observer.tracked[0].ParentSessionID != "parent" {
		t.Fatalf("observer payload=%#v", observer.tracked[0])
	}
}

func TestFullModeKeepsOffByOneReviewsAndStaleRevision(t *testing.T) {
	workspace := t.TempDir()
	architecturePath := filepath.Join(workspace, ".codeaf", "plan", "architecture.md")
	reviewPath := filepath.Join(workspace, ".codeaf", "plan", "architecture-review.json")
	prompt := &gatePromptOps{}
	prompt.onPrompt = func(call int, _ PromptRequest) error {
		if call == 1 {
			return os.WriteFile(architecturePath, []byte("# Initial"), 0o644)
		}
		// Revision calls deliberately do not touch the existing file.
		return nil
	}
	reviews := 0
	deps := baseDependencies(workspace, prompt)
	deps.AgentJSON = agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
			return []string{"provider/model"}
		}),
		Client: agentjson.ClientFunc(func(context.Context, agentjson.Request) error {
			reviews++
			return os.WriteFile(reviewPath, []byte(
				`{"approved":false,"feedback":"revise it","summary":"not yet"}`,
			), 0o644)
		}),
		NewID: func(prefix string) string { return prefix + "-id" },
	}
	deps.LookupEnv = func(string) (string, bool) { return "", false }
	result, err := RunArchitectureGate(context.Background(), Input{
		Workspace: workspace, ParentSessionID: "parent", PromptOps: prompt,
		Mode: ModeFull, UserPrompt: "Build it",
	}, deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.Status != "force_approved" || result.ArchitectIterations != 3 ||
		result.TechLeadIterations != 3 || reviews != 3 ||
		result.Reason == nil || *result.Reason != "auto-approved after 2 tech-lead iterations" {
		t.Fatalf("result=%#v reviews=%d", result, reviews)
	}
	// One initial prompt plus one prompt per nominal revision: each stale
	// pre-existing architecture passes the access check after its first try.
	if len(prompt.requests) != 3 {
		t.Fatalf("architect requests=%d, want 3", len(prompt.requests))
	}
}

func TestFullModeApprovesFirstReview(t *testing.T) {
	workspace := t.TempDir()
	architecturePath := filepath.Join(workspace, ".codeaf", "plan", "architecture.md")
	reviewPath := filepath.Join(workspace, ".codeaf", "plan", "architecture-review.json")
	prompt := &gatePromptOps{onPrompt: func(int, PromptRequest) error {
		return os.WriteFile(architecturePath, []byte("# Arch"), 0o644)
	}}
	deps := baseDependencies(workspace, prompt)
	deps.AgentJSON = agentjson.Dependencies{
		Resolver: agentjson.ResolverFunc(func(baked.Tier) []string { return []string{"p/m"} }),
		Client: agentjson.ClientFunc(func(context.Context, agentjson.Request) error {
			return os.WriteFile(reviewPath, []byte(
				`{"approved":true,"feedback":"minor note","summary":"approved"}`,
			), 0o644)
		}),
		NewID: func(prefix string) string { return prefix + "-id" },
	}
	result, err := RunArchitectureGate(context.Background(), Input{
		Workspace: workspace, PromptOps: prompt, Mode: ModeFull, UserPrompt: "x",
	}, deps)
	if err != nil || result.Status != "wrote" ||
		result.ArchitectIterations != 1 || result.TechLeadIterations != 1 ||
		result.LastReview == nil || !result.LastReview.Approved {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestMaxArchReviewIterationsParseIntSemantics(t *testing.T) {
	tests := map[string]int{
		"2tail": 2,
		"  +3x": 3,
		"0x10":  0,
		"-1":    2,
		"":      2,
	}
	for raw, want := range tests {
		raw, want := raw, want
		t.Run(raw, func(t *testing.T) {
			got := maxArchReviewIterations(func(string) (string, bool) { return raw, true })
			if got != want {
				t.Fatalf("got %d want %d", got, want)
			}
		})
	}
}
