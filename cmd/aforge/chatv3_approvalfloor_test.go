package main

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── the floor under the exceptions row ──────────────────────────────────────
//
// The built-in allowances used to be an ALTERNATIVE to the person's rules,
// which meant the consent card's always — one entry written into the same row —
// silently deleted them. These tests are that bug's headstone: they assert the
// floor holds under a banked rule, that a rule the person wrote still beats the
// floor for the tool it names, and that nothing on the floor can reach past the
// machine it runs on.

// gateOf builds the policy one profile directory produces, with no repository
// layer in the way.
func gateOf(t *testing.T, profileDir string) approval.Policy {
	t.Helper()
	cfg, err := applyV3Governance(session.Config{}, profileDir, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}
	if cfg.ApprovalPolicy == nil {
		t.Fatal("no gate reached the session")
	}
	return *cfg.ApprovalPolicy
}

func wantAction(t *testing.T, policy approval.Policy, tool, arguments string, want approval.Action) {
	t.Helper()
	if got := policy.Check(tool, json.RawMessage(arguments)); got.Action != want {
		t.Fatalf("%s %s → %s, want %s", tool, arguments, got, want)
	}
}

// THE BUG, DIRECTLY. Pressing always on one tool wrote a row, and the row
// replaced the built-in allowances wholesale, so every read started asking. The
// action taken to be asked less permanently increased the asking.
func TestBankingOneRuleLeavesTheOtherToolsAllowed(t *testing.T) {
	dir := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})

	// Before: a fresh install already allows the reads.
	before := gateOf(t, dir)
	for _, tool := range []string{"read", "grep", "find", "ls", "jobs"} {
		wantAction(t, before, tool, `{}`, approval.ActionAllow)
	}

	// The consent card's always, taken exactly as the surface takes it.
	if err := config.RememberToolApproval(dir, "edit", "allow"); err != nil {
		t.Fatalf("the always could not be written: %v", err)
	}

	after := gateOf(t, dir)
	wantAction(t, after, "edit", `{"path":"x"}`, approval.ActionAllow)
	for _, tool := range []string{"read", "grep", "find", "ls", "jobs"} {
		wantAction(t, after, tool, `{}`, approval.ActionAllow)
	}
	// And the tools the built-ins never spoke for are still asked about.
	wantAction(t, after, "write", `{"path":"x"}`, approval.ActionPrompt)
	wantAction(t, after, "bash", `{"command":"ls"}`, approval.ActionPrompt)
}

// A floor nobody could stand on would be a rule set with a part that cannot be
// turned off. What the person wrote about a tool wins for that tool.
func TestAWrittenRuleBeatsTheFloorForTheToolItNames(t *testing.T) {
	dir := v3Profile(t, map[string]any{
		"tools.approvalMode": "prompt",
		"tools.approval":     "read:prompt, remember:deny",
	})
	policy := gateOf(t, dir)
	wantAction(t, policy, "read", `{"path":"x"}`, approval.ActionPrompt)
	wantAction(t, policy, "remember", `{}`, approval.ActionDeny)
	// And naming those two said nothing at all about the rest of the floor.
	for _, tool := range []string{"grep", "find", "ls", "jobs", "track", "recall"} {
		wantAction(t, policy, tool, `{}`, approval.ActionAllow)
	}
}

// The agent's bookkeeping about its own store is not a question worth asking.
// commit is the deliberate exception: it is the one hand that declares a
// tracked subgoal finished.
func TestTheAgentsOwnBookkeepingDoesNotAskAndCommitStillDoes(t *testing.T) {
	policy := gateOf(t, v3Profile(t, map[string]any{"tools.approvalMode": "prompt"}))
	for _, tool := range []string{"remember", "track", "recall"} {
		wantAction(t, policy, tool, `{}`, approval.ActionAllow)
	}
	wantAction(t, policy, "commit", `{"id":"b1"}`, approval.ActionPrompt)
}

// The door's half of the live gate: the always is written down, and the policy
// rebuilt from the rows a moment later — which is the exact thing
// refreshV3Policy hands the running session — already carries it. The push
// itself is internal/session's test; what belongs here is that the rebuild the
// push is given is a rebuild that changed.
func TestTheBankingSeamWritesTheRowAndTheRebuildCarriesIt(t *testing.T) {
	dir := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})

	bank := bankToolApproval(nil, "", dir, false)
	if err := bank("edit"); err != nil {
		t.Fatalf("banking a tool failed: %v", err)
	}
	bankBash := bankBashApproval(nil, "", dir, false)
	if err := bankBash("git status"); err != nil {
		t.Fatalf("banking a command failed: %v", err)
	}

	policy, err := v3Policy("", dir, false)
	if err != nil {
		t.Fatalf("the rebuild failed: %v", err)
	}
	wantAction(t, *policy, "edit", `{"path":"x"}`, approval.ActionAllow)
	wantAction(t, *policy, "bash", `{"command":"git status"}`, approval.ActionAllow)
	// The reads are still allowed, and bash as a whole is still asked about.
	wantAction(t, *policy, "read", `{"path":"x"}`, approval.ActionAllow)
	wantAction(t, *policy, "bash", `{"command":"rm -rf build"}`, approval.ActionPrompt)
}

// The other door: a conversation opened with /new or /resume after a rule was
// banked must open BEHIND that rule. It is the same complaint one door over —
// the launch config carries the launch's gate, and a second conversation built
// from it would ask again about the tool somebody had just answered for.
func TestASecondConversationOpensOnTheGateAsItStandsNow(t *testing.T) {
	dir := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})
	launch, err := applyV3Governance(session.Config{Model: "m"}, dir, false)
	if err != nil {
		t.Fatalf("the launch did not load: %v", err)
	}
	wantAction(t, *launch.ApprovalPolicy, "edit", `{"path":"x"}`, approval.ActionPrompt)

	// Seconds later, on a consent card in the first conversation.
	if err := bankToolApproval(nil, "", dir, false)("edit"); err != nil {
		t.Fatalf("banking a tool failed: %v", err)
	}

	next := v3CurrentGate(launch, dir, dir, false)
	wantAction(t, *next.ApprovalPolicy, "edit", `{"path":"x"}`, approval.ActionAllow)
	// The launch's own config is untouched — a copy went out, not a mutation.
	wantAction(t, *launch.ApprovalPolicy, "edit", `{"path":"x"}`, approval.ActionPrompt)
	// And everything else about the launch travelled with it.
	if next.Model != "m" {
		t.Fatalf("the second conversation lost the launch's model: %q", next.Model)
	}
}

// --yolo is a property of the launch and it has to survive the re-read: a
// person who started this window with the flag did not un-say it by pressing
// always on a card.
func TestTheSecondConversationKeepsTheLaunchsPosture(t *testing.T) {
	dir := v3Profile(t, map[string]any{"tools.approvalMode": "prompt"})
	launch, err := applyV3Governance(session.Config{Model: "m"}, dir, true)
	if err != nil {
		t.Fatalf("the launch did not load: %v", err)
	}
	next := v3CurrentGate(launch, dir, dir, true)
	wantAction(t, *next.ApprovalPolicy, "edit", `{"path":"x"}`, approval.ActionAllow)
	// And the floors the flag never lifted are still where they were.
	wantAction(t, *next.ApprovalPolicy, "bash", `{"command":"rm -rf /"}`, approval.ActionPrompt)
}

// A rebuild that cannot read the rows leaves the launch's gate in place. The
// answer to "I could not read the rules" is never a session with no rules.
func TestAnUnreadableRowLeavesTheSecondConversationOnTheLaunchsGate(t *testing.T) {
	launch, err := applyV3Governance(session.Config{Model: "m"}, t.TempDir(), false)
	if err != nil {
		t.Fatalf("the launch did not load: %v", err)
	}
	broken := v3Profile(t, map[string]any{"tools.approval": "bash:always"})
	next := v3CurrentGate(launch, "", broken, false)
	if next.ApprovalPolicy != launch.ApprovalPolicy {
		t.Fatal("a broken settings row replaced the gate the launch was standing on")
	}
	wantAction(t, *next.ApprovalPolicy, "edit", `{"path":"x"}`, approval.ActionPrompt)
}

// A write that fails is the error the surface has to hear: it is about to say
// "saved", and the disk is what makes that true or false.
func TestAFailedWriteIsToldToTheSurface(t *testing.T) {
	bank := bankToolApproval(nil, "", t.TempDir(), false)
	if err := bank("   "); err == nil {
		t.Fatal("a tool with no name was reported as saved")
	}
	bankBash := bankBashApproval(nil, "", t.TempDir(), false)
	if err := bankBash("cd /tmp && rm -rf build"); err == nil {
		t.Fatal("a compound line was reported as saved; an allow answers for one whole command")
	}
}

// THE FLOOR IS A FLOOR AND NOT A HOLE. Nothing seeded on it acts outside this
// machine, and the two things that sit above every rule still sit above it.
func TestTheFloorDoesNotWidenAnySafetyFloor(t *testing.T) {
	dir := v3Profile(t, map[string]any{"tools.approvalMode": "allow"})
	policy := gateOf(t, dir)
	// A blanket allow still cannot vouch for a message leaving in the person's
	// name, and the seed did not add a tool that could.
	wantAction(t, policy, "gmail_send", `{"to":"a@b.c"}`, approval.ActionPrompt)
	wantAction(t, policy, "stripe_request", `{"method":"POST"}`, approval.ActionPrompt)
	// The critical-command table still overrides an allow.
	wantAction(t, policy, "bash", `{"command":"rm -rf /"}`, approval.ActionPrompt)
	// And a bash call whose command cannot be read degrades rather than runs.
	wantAction(t, policy, "bash", `{}`, approval.ActionPrompt)
}
