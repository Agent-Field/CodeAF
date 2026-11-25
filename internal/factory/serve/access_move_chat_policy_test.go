package serve

import (
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// chatMoveFixture is santosh's chat in a workstream of his, shared with abir at `full` and with mia at `edit`, and a
// workstream of abir's that abir can edit to move it into.
type chatMoveFixture struct {
	o    *wsOrg
	chat api.AgentChat
	to   string
	path string
}

func newChatMoveFixture(t *testing.T) chatMoveFixture {
	t.Helper()
	o := newWSOrg(t)
	from := o.create(t, "santosh", api.WorkstreamNew{Name: "Original"}).ID
	to := o.create(t, "abir", api.WorkstreamNew{Name: "Destination"}).ID
	chat := contractChat(t, o, from, "")
	path := "/v1/resources/chat/" + chat.ID
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("abir", "full"), 201, nil)
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("mia", "edit"), 201, nil)
	return chatMoveFixture{o: o, chat: chat, to: to, path: path}
}

// refusedMove asks for the move as who and returns the sentence it was refused with.
func (f chatMoveFixture) refusedMove(t *testing.T, who, to string) string {
	t.Helper()
	w := f.o.raw(t, who, "POST", f.path+"/move", api.ResourceMove{To: to})
	if w.Code != http.StatusForbidden {
		t.Fatalf("%s moving the chat: %d %s, want 403", who, w.Code, w.Body)
	}
	return errorOf(t, w.Body.Bytes())
}

// Contract: with the default policy, only the owner moves a chat. A person who holds `manage` on it, as the `full` role
// does, is refused, and reads today's sentence word for word.
func TestChatMoveDefaultPolicyRefusesAManageHolder(t *testing.T) {
	f := newChatMoveFixture(t)
	const today = "Only the person who owns this chat can move it, and they need permission to manage it."
	if got := f.refusedMove(t, "abir", f.to); got != today {
		t.Fatalf("a manage holder under the default policy reads %q, want %q", got, today)
	}
	if got := f.refusedMove(t, "mia", f.to); got != today {
		t.Fatalf("an edit holder under the default policy reads %q, want %q", got, today)
	}
	// Saying `owner` out loud is the same as saying nothing.
	f.o.setAccessPolicy(t, func(p *acl.Policy) { p.Chats.MoveBy = acl.MoveByOwner })
	if got := f.refusedMove(t, "abir", f.to); got != today {
		t.Fatalf("a manage holder under move_by owner reads %q, want %q", got, today)
	}
	var moved api.ResourceMoved
	contractMove(t, f.o, "santosh", f.chat.ID, contractSpace(t, f.o, "santosh"), http.StatusOK, &moved)
	if moved.Resource.Owner.ID != "santosh" {
		t.Fatalf("the owner's move: %+v", moved)
	}
}

// Contract: when the organization's policy lets people with `manage` move a chat, a person who holds `manage` on it can
// move it, and a person who holds only `edit` still cannot. The chat stays its owner's.
func TestChatMoveManagePolicyLetsAManageHolderMoveButNotAnEditHolder(t *testing.T) {
	f := newChatMoveFixture(t)
	f.o.setAccessPolicy(t, func(p *acl.Policy) { p.Chats.MoveBy = acl.MoveByManage })

	const words = "Only the person who owns this chat, or someone with permission to manage it, can move it."
	if got := f.refusedMove(t, "mia", f.to); got != words {
		t.Fatalf("an edit holder under move_by manage reads %q, want %q", got, words)
	}
	accessAgentRequest(t, f.o, "POST", f.path+"/move", "abir", "afi", api.ResourceMove{To: f.to}, http.StatusForbidden, nil)

	var moved api.ResourceMoved
	contractMove(t, f.o, "abir", f.chat.ID, f.to, http.StatusOK, &moved)
	if moved.Resource.Parent == nil || moved.Resource.Parent.ID != f.to || moved.Resource.Owner.ID != "santosh" {
		t.Fatalf("a manage holder's move: %+v", moved)
	}
	// The chat is where the manage holder put it, and still belongs to its owner, who can still see it there.
	var seen api.OrgResource
	f.o.do(t, "santosh", "GET", f.path, -1, nil, http.StatusOK, &seen)
	if seen.Parent == nil || seen.Parent.ID != f.to || seen.Owner.ID != "santosh" {
		t.Fatalf("the owner reads %+v after the move", seen)
	}
}

// Contract: even where a manage holder may move a chat, another person's chat never goes into the mover's own My space,
// and the owner's own moves are unchanged.
func TestChatMoveManagePolicyKeepsAnotherPersonsChatOutOfTheMoversSpace(t *testing.T) {
	f := newChatMoveFixture(t)
	f.o.setAccessPolicy(t, func(p *acl.Policy) { p.Chats.MoveBy = acl.MoveByManage })

	const words = "Only the person who owns this chat can move it into their My space."
	if got := f.refusedMove(t, "abir", contractSpace(t, f.o, "abir")); got != words {
		t.Fatalf("a manage holder moving the chat into their My space reads %q, want %q", got, words)
	}
	var moved api.ResourceMoved
	contractMove(t, f.o, "santosh", f.chat.ID, contractSpace(t, f.o, "santosh"), http.StatusOK, &moved)
	if moved.Resource.Owner.ID != "santosh" || moved.Resource.Visibility != acl.Private {
		t.Fatalf("the owner's move into My space: %+v", moved)
	}
}

// Contract: the rule can be turned back. Once the policy says `owner` again, a manage holder is refused as before.
func TestChatMoveRuleTurnsBackToOwnerOnly(t *testing.T) {
	f := newChatMoveFixture(t)
	f.o.setAccessPolicy(t, func(p *acl.Policy) { p.Chats.MoveBy = acl.MoveByManage })
	f.o.setAccessPolicy(t, func(p *acl.Policy) { p.Chats = acl.Chats{} })
	var saved acl.Policy
	f.o.do(t, "mia", "GET", policyPath, -1, nil, http.StatusOK, &saved)
	if saved.Chats.MoveByFor() != acl.MoveByOwner {
		t.Fatalf("the saved policy says %q", saved.Chats.MoveByFor())
	}
	const today = "Only the person who owns this chat can move it, and they need permission to manage it."
	if got := f.refusedMove(t, "abir", f.to); got != today {
		t.Fatalf("after turning the rule back a manage holder reads %q", got)
	}
}

// Contract: only the operator sets the rule, and it takes only its two words.
func TestChatMoveRuleIsTheOperatorsToSetAndTakesTwoWords(t *testing.T) {
	o := newWSOrg(t)
	var p acl.Policy
	o.do(t, "mia", "GET", policyPath, -1, nil, http.StatusOK, &p)
	p.Chats.MoveBy = acl.MoveByManage
	o.do(t, "mia", "PUT", policyPath, p.Rev, p, http.StatusForbidden, nil)
	p.Chats.MoveBy = "anyone"
	o.do(t, "owner", "PUT", policyPath, p.Rev, p, http.StatusUnprocessableEntity, nil)
}
