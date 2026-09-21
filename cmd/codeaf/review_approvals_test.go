package main

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

func TestHeadlessApprovalRequiresAnExplicitChoice(t *testing.T) {
	for _, tc := range []struct {
		name, mode     string
		headless, yolo bool
		want           approval.Action
	}{
		{"interactive default", "", false, false, approval.ActionAllow},
		{"headless default", "", true, false, approval.ActionPrompt},
		{"headless explicit allow", "allow", true, false, approval.ActionAllow},
		{"headless explicit deny", "deny", true, false, approval.ActionDeny},
		{"headless invalid", "garbled", true, false, approval.ActionPrompt},
		{"headless yolo", "", true, true, approval.ActionAllow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := map[string]any{}
			if tc.mode != "" {
				rows["tools.approvalMode"] = tc.mode
			}
			gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: v3Profile(t, rows), headless: tc.headless}
			posture := ""
			if tc.yolo {
				posture = session.PostureAllow
			}
			policy, _, err := gate.Build(posture)
			if err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"rm -rf build", "git reset --hard", "git clean -fdx"} {
				args, _ := json.Marshal(map[string]string{"command": command})
				if got := policy.Check("bash", args).Action; got != tc.want {
					t.Errorf("%s: %s, want %s", command, got, tc.want)
				}
			}
			if got := policy.Check("bash", json.RawMessage(`{"command":"rm -rf /"}`)).Action; got == approval.ActionAllow {
				t.Fatal("critical floor lifted")
			}
		})
	}
}

func TestReopenedConversationRestoresApprovalsAndEffortBeforeUse(t *testing.T) {
	proc := v3TestProcess(t)
	opts := v3Options{Workspace: t.TempDir(), Model: "test/model", Interactive: true}
	launch, err := openV3Launch(proc, opts)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _, err := openV3Agent(launch.Config, launch.Workspace, session.New)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetApprovalPosture(session.PostureAsk); err != nil {
		t.Fatal(err)
	}
	first.SetConversationEffort("high")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	for _, yolo := range []bool{false, true} {
		cfg := launch.Config
		if yolo {
			cfg.ApprovalPosture = session.PostureAllow
			cfg.ApprovalPolicy, _, err = cfg.ApprovalGate.Build(session.PostureAllow)
			if err != nil {
				t.Fatal(err)
			}
		}
		restored, _, _, err := openV3Agent(cfg, launch.Workspace, func(built session.Config) (*session.Agent, error) {
			want := approval.ActionPrompt
			if yolo {
				want = approval.ActionAllow
			}
			if got := built.ApprovalPolicy.Check("write", json.RawMessage(`{"path":"x"}`)).Action; got != want {
				t.Fatalf("constructor gate = %s, want %s", got, want)
			}
			return session.New(built)
		})
		if err != nil {
			t.Fatal(err)
		}
		if restored.ConversationEffort() != "high" {
			t.Fatal("saved effort was dropped")
		}
		if !yolo && restored.StandingApprovalPosture() != session.PostureAllow {
			t.Fatal("saved ask leaked into new conversation default")
		}
		if err := restored.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHeadlessHelloDoesNotInheritInteractiveApprovals(t *testing.T) {
	proc := v3TestProcess(t)
	for _, headless := range []bool{false, true} {
		opts := engineLaunchOptions(remote.Hello{Headless: headless, Model: "test/model"}, t.TempDir(), "")
		launch, err := openV3Launch(proc, opts)
		if err != nil {
			t.Fatal(err)
		}
		want := approval.ActionAllow
		if headless {
			want = approval.ActionPrompt
		}
		if got := launch.Config.ApprovalPolicy.Check("write", json.RawMessage(`{"path":"x"}`)).Action; got != want {
			t.Fatalf("headless=%t: %s, want %s", headless, got, want)
		}
	}
}

func TestPickerResumeRestoresSavedAskAndEffort(t *testing.T) {
	proc := v3TestProcess(t)
	opts := v3Options{Workspace: t.TempDir(), Model: "test/model", Interactive: true}
	launch, err := openV3Launch(proc, opts)
	if err != nil {
		t.Fatal(err)
	}
	seam := &v3Seam{proc: proc, boot: launch, seed: opts}
	first, err := seam.start("")
	if err != nil {
		t.Fatal(err)
	}
	agent := first.Agent.(*session.Agent)
	if err := agent.SetApprovalPosture(session.PostureAsk); err != nil {
		t.Fatal(err)
	}
	agent.SetConversationEffort("high")
	file := first.SessionFile
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := seam.resume("", file)
	if err != nil {
		t.Fatal(err)
	}
	restored := reopened.Agent.(*session.Agent)
	if restored.ResolvedApprovalPosture() != session.PostureAsk || restored.ConversationEffort() != "high" {
		t.Fatalf("picker reopened at %s:%s", restored.ResolvedApprovalPosture(), restored.ConversationEffort())
	}
}
