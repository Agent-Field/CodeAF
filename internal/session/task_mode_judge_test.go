package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestTheModeJudgeUnIsolatesWhenTheyAskedToWorkInTheirFolder(t *testing.T) {
	repo := newTestRepo(t)
	client := &scriptedCompleter{
		steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"title":"the fix","brief":"Fix it.","acceptance":"It is fixed."}`), nil
		}},
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			if !isPlaceCall(messages) {
				return nil, false
			}
			return textResponse(`{"in_place":true}`), true
		},
	}
	agent, ran := shapeAgentOn(t, client, repo)
	id, _, _, err := agent.StartTask(t.Context(), "just edit my working copy, do not make a branch")
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	node := agent.graph().node(id)
	if node.spec.mode != TaskModeInPlace {
		t.Fatalf("mode = %q, want in-place: they asked to work in the folder itself", node.spec.mode)
	}
	if node.spec.personMode != TaskModeInPlace || node.spec.where != "in place" {
		t.Fatalf("personMode = %q where = %q", node.spec.personMode, node.spec.where)
	}
}

func TestTheModeJudgeFailsClosedWhenItCannotAnswer(t *testing.T) {
	repo := newTestRepo(t)
	client := &scriptedCompleter{
		steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"title":"the fix","brief":"Fix it.","acceptance":"It is fixed."}`), nil
		}},
		aside: func(messages []ai.Message) (*ai.Response, bool) {
			if !isPlaceCall(messages) {
				return nil, false
			}
			return textResponse("sure, maybe?"), true
		},
	}
	agent, ran := shapeAgentOn(t, client, repo)
	id, _, _, err := agent.StartTask(t.Context(), "fix the crash")
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	if got := agent.graph().node(id).spec.mode; got != TaskModeWorktree {
		t.Fatalf("mode = %q, want worktree: a judge that cannot answer is a no", got)
	}
}

func TestThePlaceRoleSitsOnTheCheapTier(t *testing.T) {
	tier, ok := roles.TierOf(roles.RolePlace)
	if !ok || tier != roles.TierLow {
		t.Fatalf("place role tier = %q ok=%v, want the low tier", tier, ok)
	}
}
