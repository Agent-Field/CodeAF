package appapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func makeTestSubCrew(t *testing.T, f *agentFixture, parent Crew, mid string, budget string) Crew {
	t.Helper()
	f.registry.mu.Lock()
	base := f.registry.live[mid].conn.Engine.(*fakeAgentEngine)
	f.registry.live[mid].conn.Engine = &crewProposalEngine{fakeAgentEngine: base, reply: fmt.Sprintf(`{"name":"Backend","tint":"tide","helpers":[{"role":"Migrate","model":"test/model","budget":{"usd":"1.00"}}],"budget":{"usd":%q}}`, budget)}
	f.registry.mu.Unlock()
	var proposal HelperProposal
	path := "/app/v1/crews/" + parent.ID + "/bring-in-helpers"
	crewTestRead(t, f.request(t, "POST", path, fmt.Sprintf(`{"member_id":%q}`, mid)), 200, &proposal)
	// A proposal is only a card; the parent member still owns the same chat.
	before := crewTestGet(t, f, parent.ID)
	if len(before.Tree) != 2 {
		t.Fatal("proposal changed membership")
	}
	var crew Crew
	body := fmt.Sprintf(`{"from":"proposal","proposal_id":%q}`, proposal.ID)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", body), 201, &crew)
	if crew.ParentID != parent.ID || crew.ManagerID != mid || crew.Share != 50 || crew.Budget.USD != budget {
		t.Fatal(crew)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", body), 201, &crew)
	return crew
}
func TestSubCrewKeepsChatDepthThreeShareAndQuestionPath(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	source := crewTestGet(t, f, c.ID).Tree[1].AgentID
	child := makeTestSubCrew(t, f, c, source, "10.00")
	source2 := crewTestGet(t, f, child.ID).Tree[1].AgentID
	grandchild := makeTestSubCrew(t, f, child, source2, "5.00")
	if child.Depth != 2 || grandchild.Depth != 3 {
		t.Fatal(child, grandchild)
	}
	leaf := crewTestGet(t, f, grandchild.ID).Tree[1].AgentID
	denied := f.request(t, "POST", "/app/v1/crews/"+grandchild.ID+"/bring-in-helpers", fmt.Sprintf(`{"member_id":%q}`, leaf))
	crewTestRead(t, denied, 409, nil)
	if !strings.Contains(denied.Body.String(), "Crews nest 3 deep at most.") {
		t.Fatal(denied.Body.String())
	}
	path, err := f.registry.CrewQuestionPath(leaf)
	if err != nil || len(path) != 4 || path[0].ID != leaf || path[1].ID != grandchild.ID || path[2].ID != child.ID || path[3].ID != c.ID {
		t.Fatalf("path=%+v err=%v", path, err)
	}
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+child.ID, `{"budget":{"usd":"10.01"}}`), 422, nil)
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, `{"budget":{"usd":"19.99"}}`), 422, nil)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+c.ID+"/bring-in-helpers", fmt.Sprintf(`{"member_id":%q}`, c.ManagerID)), 422, nil)
	nativeCrewReport(t, f, grandchild, "")
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+grandchild.ID+"/finish", `{}`), 200, nil)
	manager := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+grandchild.ManagerID, ""), 200)
	if manager.CrewID != child.ID {
		t.Fatal("finishing sub-crew stranded its manager in the closed child", manager)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+grandchild.ID+"/reopen", `{}`), 200, nil)
	manager = readAgent(t, f.request(t, "GET", "/app/v1/agents/"+grandchild.ManagerID, ""), 200)
	if manager.CrewID != grandchild.ID {
		t.Fatal("reopen did not restore sub-crew manager", manager)
	}
}
func TestSubCrewSpendCountsOnceAndParentPoolStopsDescendants(t *testing.T) {
	f := newAgentFixture(t)
	f.server.Events().Close()
	c := workTestCrew(t, f)
	source := crewTestGet(t, f, c.ID).Tree[1].AgentID
	child := makeTestSubCrew(t, f, c, source, "10.00")
	rows := crewTestGet(t, f, child.ID).Tree
	f.registry.mu.Lock()
	manager := f.registry.live[source].conn.Engine.(*crewProposalEngine).fakeAgentEngine
	costManager := &crewCostEngine{fakeAgentEngine: manager, cost: 3}
	f.registry.live[source].conn.Engine = costManager
	helper := rows[1].AgentID
	costHelper := &crewCostEngine{fakeAgentEngine: f.registry.live[helper].conn.Engine.(*fakeAgentEngine), cost: 4}
	f.registry.live[helper].conn.Engine = costHelper
	f.registry.mu.Unlock()
	if parent := crewTestGet(t, f, c.ID); parent.Spent == nil || parent.Spent.USD != "7.00" {
		t.Fatal("parent did not count unique descendant spend", parent.Spent)
	}
	if current := crewTestGet(t, f, child.ID); current.Spent == nil || current.Spent.USD != "7.00" {
		t.Fatal(current.Spent)
	}
	costHelper.mu.Lock()
	costHelper.cost = 21
	costHelper.mu.Unlock()
	if err := f.registry.CrewAllowsMessage(helper); err == nil {
		t.Fatal("descendant bypassed parent pool")
	}
	if err := f.server.EnforceCrewBudgets(context.Background()); err != nil {
		t.Fatal(err)
	}
	asks, err := f.registry.CrewBudgetAsks()
	if err != nil || len(asks) == 0 {
		t.Fatal(asks, err)
	}
	found := false
	for _, ask := range asks {
		if ask.Source.ID == c.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("parent budget did not ask", asks)
	}
	for _, row := range crewTestGet(t, f, child.ID).Tree {
		if row.State != "stopped" {
			t.Fatal("descendant was not stopped", row)
		}
	}
}
func TestSubCrewProposalStaleParentAndExcessShare(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	mid := crewTestGet(t, f, c.ID).Tree[1].AgentID
	base := f.registry.live[mid].conn.Engine.(*fakeAgentEngine)
	engine := &crewProposalEngine{fakeAgentEngine: base, reply: `{"name":"Backend","tint":"tide","helpers":[{"role":"Migrate","model":"test/model","budget":{"usd":"1.00"}}],"budget":{"usd":"10.01"}}`}
	f.registry.mu.Lock()
	f.registry.live[mid].conn.Engine = engine
	f.registry.mu.Unlock()
	path := "/app/v1/crews/" + c.ID + "/bring-in-helpers"
	crewTestRead(t, f.request(t, "POST", path, fmt.Sprintf(`{"member_id":%q}`, mid)), 502, nil)
	engine.reply = strings.ReplaceAll(engine.reply, "10.01", "10.00")
	var proposal HelperProposal
	crewTestRead(t, f.request(t, "POST", path, fmt.Sprintf(`{"member_id":%q}`, mid)), 200, &proposal)
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, `{"goal":"Changed"}`), 200, nil)
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews", fmt.Sprintf(`{"from":"proposal","proposal_id":%q}`, proposal.ID)), 409, nil)
	file, err := teams.Snapshot(f.home)
	if err != nil || len(file.Teams) != 1 {
		t.Fatal(file, err)
	}
}
