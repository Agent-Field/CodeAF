package fixgenerator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/baked"
	"github.com/Agent-Field/swe-pro-go/internal/session/agentjson"
	"github.com/Agent-Field/swe-pro-go/internal/session/auditorgate"
)

func TestDispatchFixGeneratorUsesAgentJSON(t *testing.T) {
	workspace := t.TempDir()
	var requests []agentjson.Request
	deps := Dependencies{
		PlanDB: PlanDBRunnerFunc(func(args []string) (PlanDBResult, error) {
			return PlanDBResult{Stdout: []byte("frozen t-1: file_scope=src/frozen.go")}, nil
		}),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(tier baked.Tier) []string {
				if tier != baked.TierHigh {
					t.Fatalf("tier = %q", tier)
				}
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(_ context.Context, request agentjson.Request) error {
				requests = append(requests, request)
				target := filepath.Join(
					workspace, ".codeaf", "agents", "fix-generator", "cycle-2.json",
				)
				return os.WriteFile(target, []byte(
					`{"action":"dispatch_fixes","reason":"fixable","fixes":[{"title":"Fix parser","kind":"code","deps":null,"description":"blocker: wrong output"}],"summary":null}`,
				), 0o600)
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	file := "src/a.go"
	step := 2.0
	verdict := auditorgate.AuditorVerdict{
		Verdict: auditorgate.VerdictFail,
		Blockers: []auditorgate.Blocker{{
			File: &file, Step: &step, Detail: "wrong output",
		}},
	}
	result, err := DispatchFixGenerator(context.Background(), DispatchInput{
		Workspace:       workspace,
		ParentSessionID: "parent",
		UserGoal:        "Fix the parser",
		Verdict:         verdict,
		Cycle:           1,
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Data.Action != ActionDispatchFixes || len(result.Data.Fixes) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	request := requests[0]
	if request.Agent != "fix-generator" || request.ParentSessionID != "parent" {
		t.Fatalf("request identity = %#v", request)
	}
	for _, want := range []string{
		"Cycle: 2 of 3", "wrong output", "src/a.go",
		"frozen t-1: file_scope=src/frozen.go",
		workspace + "/.codeaf/agents/fix-generator/cycle-2.json",
	} {
		if !contains(request.TaskPrompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestDispatchFixGeneratorFallsBack(t *testing.T) {
	deps := Dependencies{
		PlanDB: PlanDBRunnerFunc(func([]string) (PlanDBResult, error) {
			return PlanDBResult{}, nil
		}),
		AgentJSON: agentjson.Dependencies{
			Resolver: agentjson.ResolverFunc(func(baked.Tier) []string {
				return []string{"provider/model"}
			}),
			Client: agentjson.ClientFunc(func(context.Context, agentjson.Request) error {
				return nil
			}),
			NewID: func(prefix string) string { return prefix + "-id" },
		},
	}
	result, err := DispatchFixGenerator(context.Background(), DispatchInput{
		Workspace: t.TempDir(), ParentSessionID: "p", UserGoal: "g",
		Verdict: auditorgate.AuditorVerdict{Verdict: auditorgate.VerdictFail},
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.UsedFallback || result.Data.Action != ActionGiveUp {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyContinuesAfterRunnerError(t *testing.T) {
	calls := []string{}
	runner := PlanDBRunnerFunc(func(args []string) (PlanDBResult, error) {
		calls = append(calls, args[2])
		if args[2] == "bad" {
			return PlanDBResult{}, os.ErrPermission
		}
		return PlanDBResult{}, nil
	})
	result := ApplyFixGeneratorDecision(FixGeneratorDecision{
		Action: ActionDispatchFixes,
		Reason: "x",
		Fixes: []Fix{
			{Title: "bad", Kind: FixCode, Description: "d"},
			{Title: "good", Kind: FixTest, Deps: []string{"t-1"}, Description: "d"},
		},
	}, runner)
	if result.Count != 1 || !result.TasksAdded {
		t.Fatalf("result = %#v", result)
	}
	if !reflect.DeepEqual(calls, []string{"bad", "good"}) {
		t.Fatalf("calls = %#v", calls)
	}
}

func contains(text, substring string) bool {
	for i := 0; i+len(substring) <= len(text); i++ {
		if text[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
