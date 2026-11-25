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
