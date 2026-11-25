package acl

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// organizationPolicyBeforeChatRules is a policy document as an organization stored it before the policy had a `chats`
// section: the four starting roles and every section that existed then.
const organizationPolicyBeforeChatRules = `{
  "rev": 4,
  "roles": [
    {"name": "view", "verbs": ["view"]},
    {"name": "comment", "verbs": ["view", "comment"]},
    {"name": "edit", "verbs": ["view", "comment", "edit", "run", "steer", "answer"]},
    {"name": "full", "verbs": ["view", "comment", "edit", "run", "steer", "answer", "share", "manage"]}
  ],
  "owner_role": "full",
  "org_role": "view",
  "defaults": {"chat": {"visibility": "private"}},
  "sharing": {"who": "share_verb"},
  "operator": {"role": "view", "sees_private": false, "may_govern": true},
  "agents": {"may_create": true, "may_share": false, "may_grant": false, "default_scope": "full"}
}`

// Contract: who may move a chat is a policy setting, and the owner alone is the default.
// A policy document that has no `chats` section, whether the default policy or one stored before the section existed,
// reads as owner-only, is still valid, and writes back without the section.
func TestChatMoveRuleIsOwnerOnlyUntilTheOrganizationChangesIt(t *testing.T) {
	data, err := json.Marshal(DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"chats"`) {
		t.Fatalf("the default policy now carries a chats section: %s", data)
	}
	if got := DefaultPolicy().Chats.MoveByFor(); got != MoveByOwner {
		t.Fatalf("default policy: who moves a chat = %q, want %q", got, MoveByOwner)
	}

	var old Policy
	if err := json.Unmarshal([]byte(organizationPolicyBeforeChatRules), &old); err != nil {
		t.Fatal(err)
	}
	if got := old.Chats.MoveByFor(); got != MoveByOwner {
		t.Fatalf("a policy stored before the rule existed: who moves a chat = %q, want %q", got, MoveByOwner)
	}
	if err := old.Validate(Policy{}, nil); err != nil {
		t.Fatalf("a policy stored before the rule existed is no longer valid: %v", err)
	}
	again, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(again), `"chats"`) {
		t.Fatalf("saving an old policy again adds a chats section: %s", again)
	}
}

// Contract: the setting takes two words, `owner` and `manage`, and refuses any other.
func TestChatMoveRuleTakesOnlyItsTwoWords(t *testing.T) {
	for _, tc := range []struct{ set, want string }{
		{"", MoveByOwner}, {"owner", MoveByOwner}, {"manage", MoveByManage},
	} {
		p := DefaultPolicy()
		p.Chats = Chats{MoveBy: tc.set}
		if err := p.Validate(Policy{}, nil); err != nil {
			t.Fatalf("%q is refused: %v", tc.set, err)
		}
		if got := p.Chats.MoveByFor(); got != tc.want {
			t.Errorf("%q resolves to %q, want %q", tc.set, got, tc.want)
		}
	}
	p := DefaultPolicy()
	var validation *ValidationError
	for _, word := range []string{"anyone", "edit", "Manage", "share"} {
		p.Chats = Chats{MoveBy: word}
		err := p.Validate(Policy{}, nil)
		if !errors.As(err, &validation) || err.Error() != "who may move a chat must be the owner or people with manage permission" {
			t.Errorf("%q: %v", word, err)
		}
	}
	p.Chats = Chats{MoveBy: MoveByManage}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"chats":{"move_by":"manage"}`) {
		t.Fatalf("the setting is not written under chats.move_by: %s", data)
	}
}

// Contract: who approves a tool call in a shared chat, and who starts a chat that is not running, are policy settings
// whose defaults are what the server did before they existed: the owner approves, and anyone who can see the chat opens
// it. A policy with no `chats` section, whether the default policy or one stored before the settings existed, reads that
// way, is still valid, and writes back without them.
func TestChatApprovalAndWakeRulesAreTodaysBehaviourUntilTheOrganizationChangesThem(t *testing.T) {
	if got := DefaultPolicy().Chats.ApprovalsByFor(); got != ApprovalsByOwner {
		t.Fatalf("default policy: who approves a tool call = %q, want %q", got, ApprovalsByOwner)
	}
	if got := DefaultPolicy().Chats.WakeByFor(); got != WakeByView {
		t.Fatalf("default policy: who starts a chat = %q, want %q", got, WakeByView)
	}
	var old Policy
	if err := json.Unmarshal([]byte(organizationPolicyBeforeChatRules), &old); err != nil {
		t.Fatal(err)
	}
	if old.Chats.ApprovalsByFor() != ApprovalsByOwner || old.Chats.WakeByFor() != WakeByView {
		t.Fatalf("a policy stored before the settings existed reads approvals %q, wake %q", old.Chats.ApprovalsByFor(), old.Chats.WakeByFor())
	}
	if err := old.Validate(Policy{}, nil); err != nil {
		t.Fatalf("a policy stored before the settings existed is no longer valid: %v", err)
	}
	// Saying the default out loud is the same behaviour, and is written down as said.
	p := DefaultPolicy()
	p.Chats = Chats{ApprovalsBy: ApprovalsByOwner, WakeBy: WakeByView}
	if p.Chats.ApprovalsByFor() != ApprovalsByOwner || p.Chats.WakeByFor() != WakeByView {
		t.Fatalf("the defaults said out loud read %q and %q", p.Chats.ApprovalsByFor(), p.Chats.WakeByFor())
	}
}

// Contract: approvals_by takes `owner` and `answer`, wake_by takes `view`, `run` and `owner`, any other word is refused
// with a sentence that names the words that are allowed, and a setting is written under `chats` by its own name.
func TestChatApprovalAndWakeRulesTakeOnlyTheirWords(t *testing.T) {
	for _, tc := range []struct {
		set                    Chats
		wantApproval, wantWake string
	}{
		{Chats{}, ApprovalsByOwner, WakeByView},
		{Chats{ApprovalsBy: "owner", WakeBy: "owner"}, ApprovalsByOwner, WakeByOwner},
		{Chats{ApprovalsBy: "answer", WakeBy: "run"}, ApprovalsByAnswer, WakeByRun},
		{Chats{WakeBy: "view"}, ApprovalsByOwner, WakeByView},
	} {
		p := DefaultPolicy()
		p.Chats = tc.set
		if err := p.Validate(Policy{}, nil); err != nil {
			t.Fatalf("%+v is refused: %v", tc.set, err)
		}
		if p.Chats.ApprovalsByFor() != tc.wantApproval || p.Chats.WakeByFor() != tc.wantWake {
			t.Errorf("%+v resolves to %q and %q, want %q and %q", tc.set, p.Chats.ApprovalsByFor(), p.Chats.WakeByFor(), tc.wantApproval, tc.wantWake)
		}
	}
	var validation *ValidationError
	for _, tc := range []struct {
		name  string
		set   func(string) Chats
		words []string
	}{
		{"approvals_by", func(w string) Chats { return Chats{ApprovalsBy: w} }, []string{"owner", "answer"}},
		{"wake_by", func(w string) Chats { return Chats{WakeBy: w} }, []string{"view", "run", "owner"}},
	} {
		for _, word := range []string{"anyone", "manage", "Answer", "edit"} {
			p := DefaultPolicy()
			p.Chats = tc.set(word)
			err := p.Validate(Policy{}, nil)
			if !errors.As(err, &validation) {
				t.Errorf("%s %q: %v", tc.name, word, err)
				continue
			}
			for _, allowed := range tc.words {
				if !strings.Contains(err.Error(), allowed) {
					t.Errorf("%s %q is refused with %q, which does not name %q", tc.name, word, err, allowed)
				}
			}
		}
	}
	p := DefaultPolicy()
	p.Chats = Chats{ApprovalsBy: ApprovalsByAnswer, WakeBy: WakeByRun}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"chats":{"approvals_by":"answer","wake_by":"run"}`) {
		t.Fatalf("the settings are not written under chats by their own names: %s", data)
	}
}
