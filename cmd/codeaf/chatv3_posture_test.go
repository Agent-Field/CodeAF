package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE WHEEL'S DOOR ONTO THE ROWS. A conversation moving its own gate goes
// through [v3ApprovalGate], and what that must guarantee is that every posture
// is the launch's own policy with one thing named: the blanket answer, and
// whether the guardian stands in. The exceptions, the shell rules and the floor
// under them are the same rows the launch reads, so a rule a person wrote is
// honoured at every stop on the wheel.

func TestEachPostureBuildsTheLaunchsOwnGateWithTheBlanketAnswerNamed(t *testing.T) {
	profile := v3Profile(t, map[string]any{
		"tools.approvalMode": "prompt",
		"approval.guardian":  "on",
	})
	if err := config.RememberToolApproval(profile, "write", "deny"); err != nil {
		t.Fatalf("the card's write failed: %v", err)
	}
	gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}

	cases := []struct {
		posture  string
		blanket  approval.Action
		guardian bool
	}{
		{session.PostureAsk, approval.ActionPrompt, false},
		{session.PostureGuardian, approval.ActionPrompt, true},
		{session.PostureAllow, approval.ActionAllow, false},
		{session.PostureDeny, approval.ActionDeny, false},
		// The rows as they stand: prompt, with the guardian the row turned on.
		{"", approval.ActionPrompt, true},
	}
	for _, tc := range cases {
		policy, guardian, err := gate.Build(tc.posture)
		if err != nil {
			t.Fatalf("%q did not build: %v", tc.posture, err)
		}
		if got := policy.Check("bash", json.RawMessage(`{"command":"make build"}`)); got.Action != tc.blanket {
			t.Fatalf("%q: an ordinary call → %s, want the blanket %s", tc.posture, got, tc.blanket)
		}
		// A RULE THE PERSON WROTE STILL WINS FOR THE TOOL IT NAMES, at every stop.
		if got := policy.Check("write", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionDeny {
			t.Fatalf("%q: the written deny on write was lost: %s", tc.posture, got)
		}
		if guardian != tc.guardian {
			t.Fatalf("%q: guardian %v, want %v", tc.posture, guardian, tc.guardian)
		}
	}
}

// THE FLOOR HOLDS UNDER THE OPEN GATE exactly as it holds under --yolo: a
// grave command is asked about whichever posture the wheel is on.
func TestTheOpenPostureCannotLiftTheCriticalFloor(t *testing.T) {
	profile := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})
	gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
	policy, _, err := gate.Build(session.PostureAllow)
	if err != nil {
		t.Fatalf("allow did not build: %v", err)
	}
	if got := policy.Check("bash", json.RawMessage(`{"command":"rm -rf /"}`)); got.Action == approval.ActionAllow {
		t.Fatalf("the open posture let a grave command through: %s", got)
	}
}

// STANDING SAYS WHAT THE ROWS AMOUNT TO, in the ladder's words: the guardian row
// folds into `guardian`, and allow and deny are themselves.
func TestStandingReadsTheRowsInTheLaddersWords(t *testing.T) {
	cases := []struct {
		mode, guardian, want string
	}{
		{"prompt", "off", session.PostureAsk},
		{"prompt", "on", session.PostureGuardian},
		{"allow", "on", session.PostureAllow},
		{"deny", "off", session.PostureDeny},
	}
	for _, tc := range cases {
		profile := v3Profile(t, map[string]any{"tools.approvalMode": tc.mode, "approval.guardian": tc.guardian})
		gate := v3ApprovalGate{workspace: t.TempDir(), profileDir: profile}
		if got := gate.Standing(); got != tc.want {
			t.Fatalf("mode %s guardian %s → %q, want %q", tc.mode, tc.guardian, got, tc.want)
		}
	}
}

// THE WHEEL'S DOOR AND THE FLAG'S ARE ONE FUNCTION. --yolo's policy and the
// allow posture's are built by the same road, so they cannot drift.
func TestTheFlagAndTheAllowPostureBuildTheSameGate(t *testing.T) {
	profile := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})
	if err := config.RememberBashApproval(profile, "git status*"); err != nil {
		t.Fatalf("the card's write failed: %v", err)
	}
	workspace := t.TempDir()
	flagged, err := v3Policy(workspace, profile, true)
	if err != nil {
		t.Fatalf("--yolo did not build: %v", err)
	}
	walked, _, err := v3ApprovalGate{workspace: workspace, profileDir: profile}.Build(session.PostureAllow)
	if err != nil {
		t.Fatalf("allow did not build: %v", err)
	}
	for _, call := range []string{`{"command":"git status --short"}`, `{"command":"make build"}`, `{"command":"rm -rf *"}`} {
		a, b := flagged.Check("bash", json.RawMessage(call)), walked.Check("bash", json.RawMessage(call))
		if a.Action != b.Action {
			t.Fatalf("%s: --yolo says %s and the allow posture says %s", call, a, b)
		}
	}
}
