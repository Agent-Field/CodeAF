package serve

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// agentFieldCard is the v2 designs' J3 card: five workstreams with the five
// the board lists inside them, one team, three people to invite (two with no email yet).
const agentFieldCard = `{
 "workstreams":[
  {"name":"AgentField","purpose":"The product. Ship release 0.9 by 28 November.","people":["me","Marco","Sam","Lena"],"inside":[{"name":"Control plane"},{"name":"Python SDK"}]},
  {"name":"Marketing","people":["me","Lena"],"inside":[{"name":"Launch 0.9","purpose":"announce it on 28 November"}]},
  {"name":"Company","people":["me"],"inside":[{"name":"Seed round"},{"name":"Hiring"}]},
  {"name":"Northwind pilot","purpose":"Six-week pilot of AgentField, ending 9 January","people":["me","Sam"]},
  {"name":"DevTools Summit","purpose":"Booth on 3 December","people":["me","Lena","Marco"]}
 ],
 "teams":[{"name":"Engineering team","people":["Marco","Sam"]}],
 "invites":[{"name":"Marco","email":"marco@agentfield.example","team":"Engineering team"},{"name":"Sam","team":"Engineering team"},{"name":"Lena"}]
}`

// proposeCard is a runner that proposes card through the set-up door, and
// fails the test if it is anything but the set-up conversation.
func proposeCard(t *testing.T, card string, seen *AgentTurn) AgentRunner {
	return func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		*seen = turn
		if turn.Org == nil || turn.Door != nil {
			t.Errorf("the set-up conversation must carry the set-up door and no workstream door: %+v", turn)
			return AgentTurnEnd{}, nil
		}
		line, err := turn.Org.Setup.ProposeOrgSetup(ctx, json.RawMessage(card))
		if err != nil {
			t.Errorf("propose: %v", err)
		}
		turn.Step(api.AgentEntryStep, "proposed a set-up", "org_propose_setup", false)
		turn.Step(api.AgentEntryAgent, "Here is a starting set-up.", "", false)
		if !strings.Contains(line, `"Create 5 workstreams and invite 3 people"`) || !strings.Contains(line, "Nothing is made") {
			t.Errorf("the tool's answer: %q", line)
		}
		return AgentTurnEnd{}, nil
	}
}

// invitesNow counts the people invited and not yet joined.
func (o *wsOrg) invitesNow(t *testing.T) int {
	t.Helper()
	var list struct {
		Invites []api.Invite `json:"invites"`
	}
	o.do(t, "owner", "GET", "/v1/invites", -1, nil, 200, &list)
	return len(list.Invites)
}

// START YOUR ORGANIZATION: two fields, the operator's alone, and every Afi on
// the server is told the line.
func TestStartingAnOrganizationNamesItAndAfiReadsItsLine(t *testing.T) {
	o := newWSOrg(t)
	name, line := "AgentField", "  Open-source toolkit for running AI agents in production.   Six people. "
	o.do(t, "abir", "PUT", "/v1/org", -1, api.OrgProfilePut{Name: &name}, 403, nil)
	var p api.OrgProfile
	o.do(t, "owner", "PUT", "/v1/org", -1, api.OrgProfilePut{Name: &name, Description: &line}, 200, &p)
	if p.Name != "AgentField" || p.Description != "Open-source toolkit for running AI agents in production. Six people." || p.SetUp {
		t.Fatalf("profile: %+v", p)
	}
	if got := o.h.factoryNow().Name; got != "AgentField" {
		t.Fatalf("the server still answers the name %q", got)
	}
	long := strings.Repeat("x", api.OrgDescriptionMost+1)
	o.do(t, "owner", "PUT", "/v1/org", -1, api.OrgProfilePut{Description: &long}, 422, nil)
	o.do(t, "abir", "GET", "/v1/org", -1, nil, 200, &p)
	if p.Name != "AgentField" || p.Description == "" {
		t.Fatalf("a member reads %+v", p)
	}
	if about := o.h.orgAbout(); !strings.Contains(about, "AgentField: Open-source toolkit") {
		t.Fatalf("Afi is told %q", about)
	}
	// The name survives a restart.
	o.h.Close()
	o.h = New(o.seam, o.cfg)
	t.Cleanup(o.h.Close)
	if got := o.h.factoryNow().Name; got != "AgentField" {
		t.Fatalf("after a restart the server answers %q", got)
	}
}

// ONE MESSAGE GIVES ONE CARD, AND NOTHING IS MADE OR SENT UNTIL THE PRESS.
// The press makes the teams, the workstreams, the workstreams inside them and
// the invites, in that order, and the organization is then set up.
func TestAfisSetupCardMakesTheWholeOrganizationInOnePress(t *testing.T) {
	o := newWSOrg(t)
	var seen AgentTurn
	o.serverMode(t, proposeCard(t, agentFieldCard, &seen))
	line := "Open-source toolkit for running AI agents."
	o.do(t, "owner", "PUT", "/v1/org", -1, api.OrgProfilePut{Description: &line}, 200, nil)

	var view api.OrgSetupView
	o.do(t, "abir", "GET", "/v1/org/setup", -1, nil, 403, nil)
	o.do(t, "owner", "GET", "/v1/org/setup", -1, nil, 200, &view)
	if view.SetUp || view.Card != nil || !view.Agent.CanTalk {
		t.Fatalf("a fresh organization: %+v", view)
	}
	o.do(t, "owner", "POST", "/v1/org/setup/messages", -1, api.AgentMessageNew{Text: "We ship 0.9 on Nov 28 and want a proper launch."}, 202, nil)
	o.settle()
	if seen.Org.Description != line || seen.Key != testOrgKey {
		t.Fatalf("the turn knew %+v", seen.Org)
	}
	o.do(t, "owner", "GET", "/v1/org/setup", -1, nil, 200, &view)
	if view.Card == nil || len(view.Card.Workstreams) != 5 || view.Card.InsideCount() != 5 || len(view.Card.Invites) != 3 {
		t.Fatalf("the card: %+v", view.Card)
	}
	if n := len(view.Agent.Entries); n < 3 {
		t.Fatalf("the conversation: %+v", view.Agent.Entries)
	}
	// Nothing is made and nobody is invited before the press.
	if all, _ := o.st.Workstreams(); len(all) != 0 {
		t.Fatalf("proposing made %d workstreams", len(all))
	}
	if n := o.invitesNow(t); n != 0 {
		t.Fatalf("proposing invited %d people", n)
	}
	// Another person never reads the operator's card.
	o.do(t, "mia", "GET", "/v1/org/setup", -1, nil, 403, nil)

	var applied api.OrgSetupApplied
	o.do(t, "owner", "POST", "/v1/org/setup/apply", -1, map[string]any{"card": view.Card}, 200, &applied)
	if !applied.Done || applied.Failed != "" {
		t.Fatalf("applied: %+v", applied)
	}
	all, _ := o.st.Workstreams()
	top, inside := 0, 0
	byName := map[string]factory.OrgWorkstream{}
	for _, ws := range all {
		byName[ws.Name] = ws
		if ws.Parent == "" {
			top++
		} else {
			inside++
		}
	}
	if top != 5 || inside != 5 {
		t.Fatalf("made %d top-level and %d inside", top, inside)
	}
	if byName["Launch 0.9"].Parent != byName["Marketing"].ID || byName["Launch 0.9"].Purpose != "announce it on 28 November" {
		t.Fatalf("Launch 0.9: %+v", byName["Launch 0.9"])
	}
	teams, _ := o.st.Teams()
	var eng string
	for _, tm := range teams {
		if tm.Name == "Engineering team" {
			eng = tm.ID
		}
	}
	if eng == "" {
		t.Fatalf("no Engineering team: %+v", teams)
	}
	// The team whose people are all in AgentField is added to it; Northwind
	// pilot has only Sam, so it is not.
	if !hasTeamShare(byName["AgentField"], eng) || hasTeamShare(byName["Northwind pilot"], eng) {
		t.Fatalf("team shares: AgentField %+v, Northwind %+v", byName["AgentField"].Shares, byName["Northwind pilot"].Shares)
	}
	var invites api.Invites
	o.do(t, "owner", "GET", "/v1/invites", -1, nil, 200, &invites)
	if len(invites.Invites) != 3 {
		t.Fatalf("invited %+v", invites.Invites)
	}
	for _, inv := range invites.Invites {
		if inv.Name == "Marco" && (inv.Email != "marco@agentfield.example" || inv.Team != eng || len(inv.Workstreams) != 2) {
			t.Fatalf("Marco's invite carries %+v", inv)
		}
		if inv.Name == "Lena" && (inv.Email != "" || inv.Team != "" || len(inv.Workstreams) != 3) {
			t.Fatalf("Lena's invite carries %+v", inv)
		}
	}
	var p api.OrgProfile
	o.do(t, "owner", "GET", "/v1/org", -1, nil, 200, &p)
	if !p.SetUp || p.ByHand {
		t.Fatalf("after the press: %+v", p)
	}
	view = api.OrgSetupView{}
	o.do(t, "owner", "GET", "/v1/org/setup", -1, nil, 200, &view)
	if view.Card != nil || !view.SetUp {
		t.Fatalf("the card stays after the press: %+v", view)
	}
}

// A FAILURE NAMES THE PART, AND A PRESS AGAIN DOES ONLY WHAT IS LEFT.
func TestASetupPressAfterAFailureMakesOnlyWhatIsLeft(t *testing.T) {
	o := newWSOrg(t)
	card, err := factory.ParseOrgSetupCard(json.RawMessage(agentFieldCard))
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"card": card}
	o.h.setupFault = func(key string) error {
		if key == "inside:company/hiring" {
			return errors.New("the disk is full")
		}
		return nil
	}
	var applied api.OrgSetupApplied
	o.do(t, "owner", "POST", "/v1/org/setup/apply", -1, body, 200, &applied)
	if applied.Done || applied.Failed != "Hiring could not be made: the disk is full. Press again to make what is left." {
		t.Fatalf("a failed press: %+v", applied)
	}
	all, _ := o.st.Workstreams()
	if len(all) != 9 || o.invitesNow(t) != 0 {
		t.Fatalf("before the failure %d workstreams and %d invites were made; want 9 and none", len(all), o.invitesNow(t))
	}
	var p api.OrgProfile
	o.do(t, "owner", "GET", "/v1/org", -1, nil, 200, &p)
	if p.ByHand {
		t.Fatalf("a failed press is not by hand: %+v", p)
	}

	o.h.setupFault = nil
	applied = api.OrgSetupApplied{}
	o.do(t, "owner", "POST", "/v1/org/setup/apply", -1, body, 200, &applied)
	if !applied.Done || applied.Failed != "" || len(applied.Made) != 1+10+3 {
		t.Fatalf("the press again: %+v", applied)
	}
	all, _ = o.st.Workstreams()
	teams, _ := o.st.Teams()
	engineering := 0
	for _, tm := range teams {
		if tm.Name == "Engineering team" {
			engineering++
		}
	}
	if len(all) != 10 || engineering != 1 || o.invitesNow(t) != 3 {
		t.Fatalf("after the retry: %d workstreams, %d Engineering teams, %d invites", len(all), engineering, o.invitesNow(t))
	}
}

// "SET UP BY HAND" MAKES NOTHING, and the organization counts as set up.
func TestSettingAnOrganizationUpByHandMakesNothing(t *testing.T) {
	o := newWSOrg(t)
	var p api.OrgProfile
	o.do(t, "abir", "POST", "/v1/org/setup/by-hand", -1, nil, 403, nil)
	o.do(t, "owner", "POST", "/v1/org/setup/by-hand", -1, nil, 200, &p)
	if !p.SetUp || !p.ByHand {
		t.Fatalf("by hand: %+v", p)
	}
	if all, _ := o.st.Workstreams(); len(all) != 0 {
		t.Fatalf("by hand made %d workstreams", len(all))
	}
}

// WITH NO MODEL KEY AFI CANNOT TALK, AND SAYS WHAT TO DO.
func TestTheSetupConversationSaysWhyAfiCannotTalk(t *testing.T) {
	o := newWSOrg(t)
	o.h.config.WorkstreamAgent = func(context.Context, AgentTurn) (AgentTurnEnd, error) { return AgentTurnEnd{}, nil }
	var view api.OrgSetupView
	o.do(t, "owner", "GET", "/v1/org/setup", -1, nil, 200, &view)
	if view.Agent.CanTalk || !strings.Contains(view.Agent.Cannot, "model key") {
		t.Fatalf("no key: %+v", view.Agent)
	}
	o.do(t, "owner", "POST", "/v1/org/setup/messages", -1, api.AgentMessageNew{Text: "hello"}, 409, nil)
}

func hasTeamShare(ws factory.OrgWorkstream, team string) bool {
	for _, s := range ws.Shares {
		if s.Kind == factory.ShareToTeam && s.ID == team {
			return true
		}
	}
	return false
}
