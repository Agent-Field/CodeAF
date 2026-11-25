package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// spaceRunner answers every turn with one line and spends usd.
func spaceRunner(usd float64, turns *[]AgentTurn) AgentRunner {
	return func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		*turns = append(*turns, turn)
		turn.Step(api.AgentEntryAgent, "Noted, "+turn.Asker+".", "", false)
		return AgentTurnEnd{USD: usd}, nil
	}
}

// driverMode gives the server a runner and a key but leaves Afi's pane on the
// laptop: My space and a person's own chats must run here anyway.
func (o *wsOrg) driverMode(t *testing.T, runner AgentRunner) {
	t.Helper()
	o.h.config.WorkstreamAgent = runner
	o.do(t, "owner", "PUT", "/v1/org/keys/openrouter", -1, api.OrgKeyPut{Key: testOrgKey}, 200, nil)
	driver := "driver"
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &driver}, 200, nil)
}

func (o *wsOrg) spaceCall(t *testing.T, who, method, path string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens[who])
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// MY SPACE IS ONE PER PERSON, MADE ON FIRST READ, AND NOBODY ELSE REACHES IT,
// the operator included: it is never listed, read by id or named to them
// (OWNER-DECISIONS §6, §8).
func TestMySpaceIsOnePerPersonAndNobodyElseReachesIt(t *testing.T) {
	o := newWSOrg(t)
	var first, again api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &first)
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &again)
	if first.Workstream.ID == "" || first.Workstream.ID != again.Workstream.ID {
		t.Fatalf("two reads made two spaces: %q, %q", first.Workstream.ID, again.Workstream.ID)
	}
	if !first.Workstream.Space || !first.Workstream.Private || first.Workstream.Owner != (api.WorkstreamOwner{Kind: "person", ID: "santosh"}) {
		t.Fatalf("space = %+v", first.Workstream.WorkstreamSummary)
	}
	if first.LimitUSD != api.MySpaceLimitDefaultUSD || first.AtLimit {
		t.Fatalf("a new space's limit = %v, at limit %v", first.LimitUSD, first.AtLimit)
	}
	var abirs api.MySpace
	o.do(t, "abir", "GET", "/v1/myspace", -1, nil, 200, &abirs)
	if abirs.Workstream.ID == first.Workstream.ID {
		t.Fatal("abir was answered santosh's space")
	}
	id := first.Workstream.ID
	for _, who := range []string{"abir", "owner"} {
		o.do(t, who, "GET", "/v1/workstreams/"+id, -1, nil, 404, nil)
		if code, body := o.spaceCall(t, who, "GET", "/v1/workstreams"); code != 200 || strings.Contains(body, id) {
			t.Fatalf("%s's list named santosh's space: %d %s", who, code, body)
		}
	}
	// Not even its owner's list holds it: a space is not one of the workstreams.
	if _, body := o.spaceCall(t, "santosh", "GET", "/v1/workstreams"); strings.Contains(body, id) {
		t.Fatalf("santosh's workstream list holds their space: %s", body)
	}
	if _, body := o.spaceCall(t, "santosh", "GET", "/v1/workstreams/requestable"); strings.Contains(body, id) {
		t.Fatalf("a space can be sent requests: %s", body)
	}
}

// A SPACE HOLDS NO TASKS AND IS NEVER SHARED, HANDED ON, NESTED, ARCHIVED OR
// DELETED: each is refused in words that say where the thing belongs.
func TestMySpaceRefusesTasksSharesAndArchive(t *testing.T) {
	o := newWSOrg(t)
	var mine api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &mine)
	path := "/v1/workstreams/" + mine.Workstream.ID
	o.do(t, "santosh", "POST", path+"/tasks", -1, api.TaskCreate{Title: "Draft the launch post"}, 422, nil)
	if _, body := o.spaceCall(t, "santosh", "POST", path+"/tasks"); !strings.Contains(body, "My space holds no tasks") {
		t.Fatalf("the task refusal does not say why: %s", body)
	}
	edit := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	rev := mine.Workstream.Rev
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Shares: &edit, Rev: &rev}, 422, nil)
	o.do(t, "santosh", "PUT", path+"/owner", -1, map[string]any{"owner": api.WorkstreamOwner{Kind: "person", ID: "abir"}, "rev": rev}, 422, nil)
	o.do(t, "santosh", "DELETE", path, -1, nil, 422, nil)
	name := "Secret plans"
	o.do(t, "santosh", "PATCH", path, rev, api.WorkstreamPatch{Name: &name}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "Inside", Parent: mine.Workstream.ID}, 422, nil)
}

// A PERSON'S CHATS RUN ON THE SERVER EVEN WHILE AFI'S PANE IS THE LAPTOP'S,
// each is private to its person, and its title comes from its first message.
func TestMySpaceChatsRunOnTheServerAndStayPrivate(t *testing.T) {
	o := newWSOrg(t)
	var turns []AgentTurn
	o.driverMode(t, spaceRunner(0.25, &turns))
	var mine api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &mine)
	path := "/v1/workstreams/" + mine.Workstream.ID
	var chat api.AgentChat
	o.do(t, "santosh", "POST", path+"/chats", -1, nil, 201, &chat)
	if !chatIDPattern.MatchString(chat.ID) {
		t.Fatalf("chat id %q", chat.ID)
	}
	o.do(t, "santosh", "POST", path+"/agent/messages", -1, api.AgentMessageNew{Text: "Outline the layoff plan\nwith dates", Chat: chat.ID}, 202, nil)
	o.settle()
	if len(turns) != 1 || turns[0].Chat != chat.ID || !turns[0].Space || turns[0].Key != testOrgKey {
		t.Fatalf("the turn = %+v", turns)
	}
	var got api.WorkstreamAgent
	o.do(t, "santosh", "GET", path+"/agent?chat="+chat.ID, -1, nil, 200, &got)
	if got.Chat != chat.ID || len(got.Entries) != 2 || got.Entries[1].Text != "Noted, santosh." || !got.CanTalk {
		t.Fatalf("the chat = %+v", got)
	}
	var afi api.WorkstreamAgent
	o.do(t, "santosh", "GET", path+"/agent", -1, nil, 200, &afi)
	if len(afi.Entries) != 0 {
		t.Fatalf("the chat's lines reached Afi's conversation: %+v", afi.Entries)
	}
	var list api.AgentChatList
	o.do(t, "santosh", "GET", path+"/chats", -1, nil, 200, &list)
	if len(list.Chats) != 1 || list.Chats[0].Title != "Outline the layoff plan" {
		t.Fatalf("chats = %+v", list.Chats)
	}

	// In a shared workstream, someone else's chat id is not a way in.
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	share := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &share, Rev: &ws.Rev}, 200, nil)
	var side api.AgentChat
	o.do(t, "santosh", "POST", "/v1/workstreams/"+ws.ID+"/chats", -1, api.AgentChatNew{Title: "Side"}, 201, &side)
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID+"/agent?chat="+side.ID, -1, nil, 404, nil)
	o.do(t, "abir", "POST", "/v1/workstreams/"+ws.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "hi", Chat: side.ID}, 404, nil)
	var abirs api.AgentChatList
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID+"/chats", -1, nil, 200, &abirs)
	if len(abirs.Chats) != 0 {
		t.Fatalf("abir lists santosh's chat: %+v", abirs.Chats)
	}
	// CONTRACT CHANGE (org-follow A2, A4, Abir 2026-11-24): a chat is private by
	// default, so abir is told nothing of santosh's chat called Side (v2 ruling C10
	// once listed its title, state and last activity to the whole workstream) until
	// santosh shares it. Shared, abir sees that it exists, its state and when it
	// last moved, and nothing that was said in it.
	if len(abirs.Others) != 0 {
		t.Fatalf("abir is shown santosh's private chat: %+v", abirs.Others)
	}
	o.do(t, "santosh", "POST", "/v1/resources/chat/"+side.ID+"/grants", -1, personGrant("abir", "view"), 201, nil)
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID+"/chats", -1, nil, 200, &abirs)
	if len(abirs.Others) != 1 || abirs.Others[0].ID != side.ID || abirs.Others[0].Title != "Side" || abirs.Others[0].Owner != "santosh" ||
		abirs.Others[0].Workstream != ws.ID || abirs.Others[0].LastActivity == "" || abirs.Others[0].State == "" {
		t.Fatalf("abir's view of others' chats = %+v", abirs.Others)
	}
	// santosh's own chats are not listed back to santosh as someone else's,
	// and a space lists nobody's.
	var santoshs api.AgentChatList
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/chats", -1, nil, 200, &santoshs)
	if len(santoshs.Others) != 0 {
		t.Fatalf("santosh is shown their own chat as another's: %+v", santoshs.Others)
	}
	o.do(t, "santosh", "GET", path+"/chats", -1, nil, 200, &santoshs)
	if len(santoshs.Others) != 0 {
		t.Fatalf("a space lists others' chats: %+v", santoshs.Others)
	}
}

// THE OPERATOR SEES TOTALS ONLY: spend this month and a count per person,
// never a title. At the person's limit their space's chats take no more
// messages and say so.
func TestOperatorSeesSpaceTotalsOnlyAndTheLimitStopsTheChats(t *testing.T) {
	o := newWSOrg(t)
	var turns []AgentTurn
	o.serverMode(t, spaceRunner(0.84, &turns))
	var mine api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &mine)
	path := "/v1/workstreams/" + mine.Workstream.ID
	var chat api.AgentChat
	o.do(t, "santosh", "POST", path+"/chats", -1, api.AgentChatNew{Title: "Layoff plan"}, 201, &chat)
	o.do(t, "santosh", "POST", path+"/agent/messages", -1, api.AgentMessageNew{Text: "who goes first", Chat: chat.ID}, 202, nil)
	o.settle()

	o.do(t, "santosh", "GET", "/v1/myspace/totals", -1, nil, 403, nil)
	code, body := o.spaceCall(t, "owner", "GET", "/v1/myspace/totals")
	if code != 200 {
		t.Fatalf("totals = %d %s", code, body)
	}
	for _, secret := range []string{"Layoff", "who goes first", chat.ID, mine.Workstream.ID} {
		if strings.Contains(body, secret) {
			t.Fatalf("the operator's totals say %q: %s", secret, body)
		}
	}
	var totals api.MySpaceTotals
	if err := json.Unmarshal([]byte(body), &totals); err != nil {
		t.Fatal(err)
	}
	if len(totals.People) != 1 || totals.People[0].Person != "santosh" || totals.People[0].SpentUSD != 0.84 || totals.People[0].Items != 1 {
		t.Fatalf("totals = %+v", totals)
	}

	o.do(t, "owner", "PUT", "/v1/myspace/limit", -1, api.MySpaceLimitPut{LimitUSD: 0.5}, 200, &totals)
	if totals.LimitUSD != 0.5 || !totals.People[0].AtLimit {
		t.Fatalf("after the limit: %+v", totals)
	}
	o.do(t, "santosh", "PUT", "/v1/myspace/limit", -1, api.MySpaceLimitPut{LimitUSD: 100}, 403, nil)
	var got api.WorkstreamAgent
	o.do(t, "santosh", "GET", path+"/agent?chat="+chat.ID, -1, nil, 200, &got)
	if got.CanTalk || got.Cannot != spaceAtLimit {
		t.Fatalf("at the limit the chat says %q (can talk %v)", got.Cannot, got.CanTalk)
	}
	o.do(t, "santosh", "POST", path+"/agent/messages", -1, api.AgentMessageNew{Text: "one more", Chat: chat.ID}, 409, nil)
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &mine)
	if !mine.AtLimit || mine.SpentUSD != 0.84 {
		t.Fatalf("my space at the limit = %+v", mine)
	}
}

// THE OPERATOR IS NEVER TOLD A PRIVATE WORKSTREAM EXISTS, not its name: the
// name-only row is gone (org-space DESIGN Q1).
func TestOperatorIsNotToldAPrivateWorkstreamsName(t *testing.T) {
	o := newWSOrg(t)
	private := o.create(t, "santosh", api.WorkstreamNew{Name: "Layoff plan", Private: true})
	if _, body := o.spaceCall(t, "owner", "GET", "/v1/workstreams"); strings.Contains(body, private.ID) || strings.Contains(body, "Layoff") {
		t.Fatalf("the operator's list names a private workstream: %s", body)
	}
}

// A LEAVING PERSON'S SPACE GOES WHOLE TO THE HEIR the operator names, as one
// private workstream, and the heir keeps their own space.
func TestALeavingPersonsSpaceGoesToTheHeir(t *testing.T) {
	o := newWSOrg(t)
	var leaving, heirs api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &leaving)
	o.do(t, "abir", "GET", "/v1/myspace", -1, nil, 200, &heirs)
	o.do(t, "owner", "DELETE", "/v1/people/santosh/sessions?heir=abir", -1, nil, 200, nil)
	got := o.get(t, "abir", leaving.Workstream.ID)
	if got.Space || !got.Private || got.Name != "santosh's space" || got.Owner.ID != "abir" {
		t.Fatalf("the handed-on space = %+v", got.WorkstreamSummary)
	}
	var still api.MySpace
	o.do(t, "abir", "GET", "/v1/myspace", -1, nil, 200, &still)
	if still.Workstream.ID != heirs.Workstream.ID {
		t.Fatalf("the heir's own space changed: %q, was %q", still.Workstream.ID, heirs.Workstream.ID)
	}
}
