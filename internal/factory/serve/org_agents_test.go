package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (o *wsOrg) agent(t *testing.T, who, name, instructions string) api.OrgAgent {
	t.Helper()
	var a api.OrgAgent
	o.do(t, who, "POST", "/v1/agents", -1, api.OrgAgentNew{Name: name, Instructions: instructions}, 201, &a)
	return a
}

func (o *wsOrg) agents(t *testing.T, who string) map[string]api.OrgAgent {
	t.Helper()
	var list api.OrgAgentList
	o.do(t, who, "GET", "/v1/agents", -1, nil, 200, &list)
	out := map[string]api.OrgAgent{}
	for _, a := range list.Agents {
		out[a.Name] = a
	}
	return out
}

func (o *wsOrg) wsAgents(t *testing.T, who, ws string) []api.WorkstreamAgentRow {
	t.Helper()
	var list api.WorkstreamAgentList
	o.do(t, who, "GET", "/v1/workstreams/"+ws+"/agents", -1, nil, 200, &list)
	return list.Agents
}

// AN AGENT IS ITS PERSON'S, AND NOBODY ELSE IS TOLD IT EXISTS, the operator
// included (OWNER-DECISIONS §8): not in the list, and not by id. The operator's
// totals count it among the space's things, and say nothing more.
func TestAnAgentInMySpaceIsKnownOnlyToItsPerson(t *testing.T) {
	o := newWSOrg(t)
	copywriter := o.agent(t, "santosh", "Copywriter", "Write short, plain launch copy.")
	if !strings.HasPrefix(copywriter.ID, "ag_") || !copywriter.Mine || copywriter.Roster != "" || copywriter.Owner != "santosh" {
		t.Fatalf("made = %+v", copywriter)
	}
	for _, who := range []string{"abir", "owner"} {
		if _, ok := o.agents(t, who)["Copywriter"]; ok {
			t.Fatalf("%s is told santosh's own agent exists", who)
		}
		o.do(t, who, "PATCH", "/v1/agents/"+copywriter.ID, -1, api.OrgAgentPatch{Name: ptr("Mine now")}, 404, nil)
		o.do(t, who, "DELETE", "/v1/agents/"+copywriter.ID, -1, nil, 404, nil)
	}
	var totals api.MySpaceTotals
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, nil)
	o.do(t, "owner", "GET", "/v1/myspace/totals", -1, nil, 200, &totals)
	if len(totals.People) != 1 || totals.People[0].Items != 1 {
		t.Fatalf("totals = %+v", totals.People)
	}
	_, raw := o.spaceCall(t, "owner", "GET", "/v1/myspace/totals")
	if strings.Contains(raw, "Copywriter") {
		t.Fatalf("the totals name santosh's agent: %s", raw)
	}

	o.do(t, "santosh", "POST", "/v1/agents", -1, api.OrgAgentNew{Name: "Afi", Instructions: "x"}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/agents", -1, api.OrgAgentNew{Name: "Empty"}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/agents", -1, api.OrgAgentNew{Name: "Teamless", Instructions: "x", Team: "tm_nowhere"}, 422, nil)
	var renamed api.OrgAgent
	o.do(t, "santosh", "PATCH", "/v1/agents/"+copywriter.ID, -1, api.OrgAgentPatch{Name: ptr("Launch writer")}, 200, &renamed)
	if renamed.Name != "Launch writer" || renamed.Instructions != copywriter.Instructions {
		t.Fatalf("renamed = %+v", renamed)
	}
}

// SHARING AN AGENT GIVES THE WORKSTREAM A LINK, NOT A COPY (DESIGN §2.2,
// §4.6): everyone in it sees the agent by name in People and rules › Agents,
// only its person changes it, and unsharing takes it off.
func TestSharingAnAgentIntoAWorkstreamListsItThereAndNowhereElse(t *testing.T) {
	o := newWSOrg(t)
	copywriter := o.agent(t, "santosh", "Copywriter", "Write short, plain launch copy.")
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	share := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &share, Rev: &ws.Rev}, 200, nil)

	// abir edits Pantry but cannot share santosh's own agent into it.
	o.do(t, "abir", "PUT", "/v1/workstreams/"+ws.ID+"/agents/"+copywriter.ID, -1, nil, 404, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/agents/"+copywriter.ID, -1, nil, 200, nil)
	rows := o.wsAgents(t, "abir", ws.ID)
	if len(rows) != 1 || rows[0].Name != "Copywriter" || rows[0].Kind != "agent" || rows[0].From != "space" || rows[0].By != "santosh" {
		t.Fatalf("Pantry's agents, to abir = %+v", rows)
	}
	if mine := o.agents(t, "santosh")["Copywriter"]; len(mine.Workstreams) != 1 || mine.Workstreams[0] != ws.ID {
		t.Fatalf("santosh's agent says it is shared into %v", mine.Workstreams)
	}
	// Shared is not known by everyone: abir still does not list it as an agent.
	if _, ok := o.agents(t, "abir")["Copywriter"]; ok {
		t.Fatal("sharing into a workstream put the agent on abir's own list")
	}
	// mia is not in Pantry: its list is not hers to read.
	o.do(t, "mia", "GET", "/v1/workstreams/"+ws.ID+"/agents", -1, nil, 404, nil)

	// The space takes no links: an agent is already there.
	var space api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &space)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+space.Workstream.ID+"/agents/"+copywriter.ID, -1, nil, 422, nil)

	o.do(t, "santosh", "DELETE", "/v1/workstreams/"+ws.ID+"/agents/"+copywriter.ID, -1, nil, 200, nil)
	if rows := o.wsAgents(t, "abir", ws.ID); len(rows) != 0 {
		t.Fatalf("unshared, Pantry still lists %+v", rows)
	}

	// Deleting the agent takes it off every workstream.
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/agents/"+copywriter.ID, -1, nil, 200, nil)
	o.do(t, "santosh", "DELETE", "/v1/agents/"+copywriter.ID, -1, nil, 204, nil)
	if rows := o.wsAgents(t, "santosh", ws.ID); len(rows) != 0 {
		t.Fatalf("a deleted agent is still listed: %+v", rows)
	}
}

// THE ORGANIZATION'S LIST IS APPROVED BY THE OPERATOR. Its person asks; the
// operator reads it and approves; then anyone who edits a workstream may add
// it. Taken off the list, it leaves the workstreams others added it to.
func TestTheOrganizationsAgentListIsAskedForAndApproved(t *testing.T) {
	o := newWSOrg(t)
	reviewer := o.agent(t, "santosh", "Reviewer", "Read the change and list what could break.")
	var asked api.OrgAgent
	o.do(t, "santosh", "PUT", "/v1/agents/"+reviewer.ID+"/roster", -1, api.OrgAgentRosterPut{On: true}, 200, &asked)
	if asked.Roster != factory.AgentRosterAsked {
		t.Fatalf("asked = %q", asked.Roster)
	}
	if _, ok := o.agents(t, "abir")["Reviewer"]; ok {
		t.Fatal("an agent waiting for the operator is on everyone's list")
	}
	waiting, ok := o.agents(t, "owner")["Reviewer"]
	if !ok || waiting.Instructions == "" || waiting.Mine {
		t.Fatalf("the operator cannot read what they are approving: %+v", waiting)
	}
	o.do(t, "abir", "PUT", "/v1/agents/"+reviewer.ID+"/roster", -1, api.OrgAgentRosterPut{On: true}, 404, nil)
	o.do(t, "owner", "PUT", "/v1/agents/"+reviewer.ID+"/roster", -1, api.OrgAgentRosterPut{On: true}, 200, nil)
	onList, ok := o.agents(t, "abir")["Reviewer"]
	if !ok || onList.Roster != factory.AgentRosterOrg || onList.OwnerName == "" && onList.Owner != "santosh" {
		t.Fatalf("abir's view of the organization's agent = %+v %v", onList, ok)
	}
	// Only its person changes it, even on the organization's list.
	o.do(t, "abir", "PATCH", "/v1/agents/"+reviewer.ID, -1, api.OrgAgentPatch{Name: ptr("Mine")}, 403, nil)

	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Release 2.4"})
	o.do(t, "abir", "PUT", "/v1/workstreams/"+ws.ID+"/agents/"+reviewer.ID, -1, nil, 200, nil)
	if rows := o.wsAgents(t, "abir", ws.ID); len(rows) != 1 || rows[0].From != "org" || rows[0].By != "abir" {
		t.Fatalf("Release 2.4's agents = %+v", rows)
	}
	o.do(t, "santosh", "PUT", "/v1/agents/"+reviewer.ID+"/roster", -1, api.OrgAgentRosterPut{On: false}, 200, nil)
	if rows := o.wsAgents(t, "abir", ws.ID); len(rows) != 0 {
		t.Fatalf("off the organization's list, it stays where abir added it: %+v", rows)
	}
	if _, ok := o.agents(t, "abir")["Reviewer"]; ok {
		t.Fatal("off the list, abir still sees it")
	}
}

// A CHAT CAN TALK TO A NAMED AGENT: in My space one's own, in a workstream one
// it lists. The turn is that agent; once it is unshared the chat refuses.
func TestAChatTalksToANamedAgentWhileItIsUsableThere(t *testing.T) {
	o := newWSOrg(t)
	var turns []AgentTurn
	o.driverMode(t, spaceRunner(0.1, &turns))
	critic := o.agent(t, "santosh", "Critic", "Find the weakest claim and say why.")
	var space api.MySpace
	o.do(t, "santosh", "GET", "/v1/myspace", -1, nil, 200, &space)
	sp := "/v1/workstreams/" + space.Workstream.ID
	var chat api.AgentChat
	o.do(t, "santosh", "POST", sp+"/chats", -1, api.AgentChatNew{Agent: critic.ID}, 201, &chat)
	if chat.Agent == nil || chat.Agent.Name != "Critic" {
		t.Fatalf("chat = %+v", chat)
	}
	o.do(t, "santosh", "POST", sp+"/agent/messages", -1, api.AgentMessageNew{Text: "Is the pricing page convincing?", Chat: chat.ID}, 202, nil)
	o.settle()
	if len(turns) != 1 || turns[0].Agent == nil || turns[0].Agent.Name != "Critic" || turns[0].Agent.Instructions != critic.Instructions {
		t.Fatalf("the turn = %+v", turns)
	}
	var list api.AgentChatList
	o.do(t, "santosh", "GET", sp+"/chats", -1, nil, 200, &list)
	if len(list.Chats) != 1 || list.Chats[0].Agent == nil || list.Chats[0].Agent.ID != critic.ID {
		t.Fatalf("chats = %+v", list.Chats)
	}

	// In a workstream, only an agent it lists.
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	wp := "/v1/workstreams/" + ws.ID
	o.do(t, "santosh", "POST", wp+"/chats", -1, api.AgentChatNew{Agent: critic.ID}, 404, nil)
	o.do(t, "santosh", "PUT", wp+"/agents/"+critic.ID, -1, nil, 200, nil)
	var there api.AgentChat
	o.do(t, "santosh", "POST", wp+"/chats", -1, api.AgentChatNew{Agent: critic.ID}, 201, &there)
	o.do(t, "santosh", "DELETE", wp+"/agents/"+critic.ID, -1, nil, 200, nil)
	o.do(t, "santosh", "POST", wp+"/agent/messages", -1, api.AgentMessageNew{Text: "Still there?", Chat: there.ID}, 409, nil)
	if len(turns) != 1 {
		t.Fatalf("an unshared agent was run: %+v", turns)
	}
}

// A LEAVING PERSON'S AGENTS GO TO THE HEIR, and the workstreams they shared
// one into keep it, now shared by the heir.
func TestALeavingPersonsAgentsGoToTheHeir(t *testing.T) {
	o := newWSOrg(t)
	copywriter := o.agent(t, "santosh", "Copywriter", "Write short, plain launch copy.")
	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Pantry"})
	share := []factory.Share{{Kind: "person", ID: "santosh", Role: "edit"}}
	o.do(t, "abir", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &share, Rev: &ws.Rev}, 200, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/agents/"+copywriter.ID, -1, nil, 200, nil)
	o.do(t, "owner", "DELETE", "/v1/people/santosh/sessions?heir=abir", -1, nil, 200, nil)
	if a, ok := o.agents(t, "abir")["Copywriter"]; !ok || !a.Mine {
		t.Fatalf("the heir does not hold the agent: %+v %v", a, ok)
	}
	if rows := o.wsAgents(t, "abir", ws.ID); len(rows) != 1 || rows[0].By != "abir" || rows[0].From != "space" {
		t.Fatalf("Pantry lost the agent santosh shared: %+v", rows)
	}
}
