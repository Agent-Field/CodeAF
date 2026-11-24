package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// The contract for this file is NOTES.md section 1, item A of the org-follow
// lane: chats are private by default, and a private chat is hidden from every
// shared list. Each test names the contract item it comes from.

// chatsSharedWorkstream makes a workstream santosh owns, shared for edit with
// abir and for view with mia, and returns its id and the path of its chat list.
func chatsSharedWorkstream(t *testing.T, o *wsOrg) (id, list string) {
	t.Helper()
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}, {Kind: "person", ID: "mia", Role: "view"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &shares}, 200, nil)
	return ws.ID, "/v1/workstreams/" + ws.ID + "/chats"
}

func othersOf(t *testing.T, o *wsOrg, who, list string) []api.OtherChat {
	t.Helper()
	var rows api.AgentChatList
	o.do(t, who, "GET", list, -1, nil, 200, &rows)
	return rows.Others
}

// A1. The policy says chats start private, with no stored policy and with a
// stored one that never named chats.
func TestContractA1ThePolicyDefaultForChatsIsPrivate(t *testing.T) {
	o := newWSOrg(t)
	var p acl.Policy
	o.do(t, "mia", "GET", policyPath, -1, nil, 200, &p)
	if got := p.Visibility("chat"); got != acl.Private || p.Defaults["chat"].Visibility != acl.Private {
		t.Fatalf("the default policy starts chats %q (%+v)", got, p.Defaults["chat"])
	}
	if got := (acl.Policy{Defaults: map[string]acl.KindDefault{}}).Visibility("chat"); got != acl.Private {
		t.Fatalf("a policy that never named chats starts them %q", got)
	}
	if got := (acl.Policy{}).Visibility("chat"); got != acl.Private {
		t.Fatalf("an empty policy starts chats %q", got)
	}
}

// A2, A3. Another member and the operator are not told a private chat exists:
// no title, owner, state or time. Its owner still lists it.
func TestContractA2APrivateChatIsHiddenFromTheWorkstreamAndTheOperator(t *testing.T) {
	o := newWSOrg(t)
	ws, list := chatsSharedWorkstream(t, o)
	chat := contractChat(t, o, ws, "")
	for _, who := range []string{"abir", "mia", "owner"} {
		if rows := othersOf(t, o, who, list); len(rows) != 0 {
			t.Fatalf("%s is shown a private chat: %+v", who, rows)
		}
	}
	var mine api.AgentChatList
	o.do(t, "santosh", "GET", list, -1, nil, 200, &mine)
	if len(mine.Chats) != 1 || mine.Chats[0].ID != chat.ID || mine.Chats[0].Title != chat.Title || len(mine.Others) != 0 {
		t.Fatalf("the owner's own list = %+v", mine)
	}
	// The id is no way in either (unchanged).
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws+"/agent?chat="+chat.ID, -1, nil, 404, nil)
}

// A4. A grant on the chat lists it to that person alone, with its metadata and
// never its lines; removing the grant, or its lapse, takes it away again.
func TestContractA4SharingAChatListsItToThatPersonAlone(t *testing.T) {
	o := newWSOrg(t)
	ws, list := chatsSharedWorkstream(t, o)
	chat := contractChat(t, o, ws, "")
	path := "/v1/resources/chat/" + chat.ID
	var grant acl.Grant
	o.do(t, "santosh", "POST", path+"/grants", -1, personGrant("abir", "view"), 201, &grant)
	rows := othersOf(t, o, "abir", list)
	if len(rows) != 1 || rows[0].ID != chat.ID || rows[0].Title != chat.Title || rows[0].Owner != "santosh" ||
		rows[0].Workstream != ws || rows[0].State == "" || rows[0].LastActivity == "" {
		t.Fatalf("the grantee's list = %+v", rows)
	}
	for _, who := range []string{"mia", "owner"} {
		if rows := othersOf(t, o, who, list); len(rows) != 0 {
			t.Fatalf("%s holds no grant and is shown %+v", who, rows)
		}
	}
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws+"/agent?chat="+chat.ID, -1, nil, 404, nil)
	o.do(t, "santosh", "DELETE", path+"/grants/"+grant.ID, -1, nil, 204, nil)
	if rows := othersOf(t, o, "abir", list); len(rows) != 0 {
		t.Fatalf("a removed grant still lists the chat: %+v", rows)
	}
	contractGrantExpiry(t, o, path, "abir", o.now.Add(time.Minute))
	if rows := othersOf(t, o, "abir", list); len(rows) != 1 {
		t.Fatalf("a live grant does not list the chat: %+v", rows)
	}
	o.now = o.now.Add(2 * time.Minute)
	if rows := othersOf(t, o, "abir", list); len(rows) != 0 {
		t.Fatalf("a lapsed grant still lists the chat: %+v", rows)
	}
}

// A5. A chat in My space is nobody else's, however it is asked.
func TestContractA5AChatInMySpaceIsListedToNobodyElse(t *testing.T) {
	o := newWSOrg(t)
	space := contractSpace(t, o, "santosh")
	contractChat(t, o, space, "")
	for _, who := range []string{"abir", "owner"} {
		o.do(t, who, "GET", "/v1/workstreams/"+space+"/chats", -1, nil, 404, nil)
	}
	if rows := othersOf(t, o, "santosh", "/v1/workstreams/"+space+"/chats"); len(rows) != 0 {
		t.Fatalf("a space lists others' chats: %+v", rows)
	}
}

// A6. The operator can make new chats visible to the workstream through the
// policy; chats made before keep what they were registered as.
func TestContractA6ThePolicyDefaultAppliesToNewChatsOnly(t *testing.T) {
	for _, visibility := range []string{acl.Owner, acl.Org} {
		t.Run(visibility, func(t *testing.T) {
			o := newWSOrg(t)
			ws, list := chatsSharedWorkstream(t, o)
			before := contractChat(t, o, ws, "")
			var p acl.Policy
			o.do(t, "owner", "GET", policyPath, -1, nil, 200, &p)
			p.Defaults["chat"] = acl.KindDefault{Visibility: visibility}
			o.do(t, "owner", "PUT", policyPath, -1, p, 200, &p)
			after := contractChat(t, o, ws, "")
			for _, who := range []string{"abir", "mia"} {
				rows := othersOf(t, o, who, list)
				if len(rows) != 1 || rows[0].ID != after.ID || rows[0].Title != after.Title {
					t.Fatalf("%s with the %s default is shown %+v, want only the chat made after it (%s, not %s)", who, visibility, rows, after.ID, before.ID)
				}
			}
			// A chat made before the change stays private for the people it was
			// private from, and a policy that goes back to private hides the new
			// one again.
			p.Defaults["chat"] = acl.KindDefault{Visibility: acl.Private}
			o.do(t, "owner", "PUT", policyPath, -1, p, 200, &p)
			if rows := othersOf(t, o, "abir", list); len(rows) != 1 || rows[0].ID != after.ID {
				t.Fatalf("changing the default back rewrote a chat that already exists: %+v", rows)
			}
		})
	}
}

// A7. Creating a private chat tells nobody; sharing it tells the people it
// reaches (unchanged). A chat's agent events reach its owner alone.
func TestContractA7ACreatedPrivateChatEmitsNoResourceEvent(t *testing.T) {
	o := newWSOrg(t)
	ws, _ := chatsSharedWorkstream(t, o)
	chat := contractChat(t, o, ws, "")
	if events := contractResourceEvents(t, o, chat.ID); len(events) != 0 {
		t.Fatalf("a private chat announced itself: %+v", events)
	}
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+chat.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	if events := contractResourceEvents(t, o, chat.ID); len(events) != 1 {
		t.Fatalf("sharing announced %d events, want one: %+v", len(events), events)
	}
}

// A8. A chat folder that has no registry row is unknown, and unknown is hidden.
func TestContractA8AChatWithNoRegistryRowIsNeverListed(t *testing.T) {
	o := newWSOrg(t)
	ws, list := chatsSharedWorkstream(t, o)
	stray := "ch_" + "0123456789abcdefghijklmnop"
	dir := o.h.chatDir(ws, "santosh", stray)
	if err := writeChatMeta(dir, chatMeta{Title: "Strays", CreatedAt: "2026-11-24T10:00:00Z", UpdatedAt: "2026-11-24T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := o.st.Resource("chat", stray); ok {
		t.Fatal("the fixture has a registry row")
	}
	for _, who := range []string{"abir", "mia", "owner"} {
		if rows := othersOf(t, o, who, list); len(rows) != 0 {
			t.Fatalf("%s is shown a chat nobody registered: %+v", who, rows)
		}
	}
}

// A9. A private chat that moves from My space into a workstream stays private:
// the workstream's people do not see it until it is shared.
func TestContractA9AMovedPrivateChatStaysPrivateInTheWorkstream(t *testing.T) {
	o := newWSOrg(t)
	space := contractSpace(t, o, "santosh")
	ws, list := chatsSharedWorkstream(t, o)
	chat := contractChat(t, o, space, "")
	contractMove(t, o, "santosh", chat.ID, ws, 200, nil)
	if rows := othersOf(t, o, "abir", list); len(rows) != 0 {
		t.Fatalf("a moved private chat is listed: %+v", rows)
	}
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+chat.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	if rows := othersOf(t, o, "abir", list); len(rows) != 1 || rows[0].ID != chat.ID {
		t.Fatalf("a shared chat is not listed: %+v", rows)
	}
	contractMove(t, o, "santosh", chat.ID, space, 200, nil)
	if rows := othersOf(t, o, "abir", list); len(rows) != 0 {
		t.Fatalf("a chat moved back to My space is still listed: %+v", rows)
	}
}
