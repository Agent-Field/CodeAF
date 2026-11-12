package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppCrewProposalRepairsOverlongHandles(t *testing.T) {
	lines := []string{
		`{"t":"name","name":"Gathering","goal":"Prepare the gathering"}`,
		`{"t":"member","handle":"attendee","brief":"Check invitations","reports_to":"manager"}`,
		`{"t":"member","handle":"attendee-lead","brief":"Lead attendee planning","reports_to":"manager"}`,
		`{"t":"member","handle":"abcdefghijklmnop","brief":"Check access","reports_to":"attendee-lead"}`,
		`{"t":"member","handle":"abcdefghijklqrst","brief":"Check timing","reports_to":"manager"}`,
	}
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{strings.Join(lines, "\n") + "\n"}}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	p := rec.data[at].(appwire.AskOpened).Ask.Proposal
	want := []appwire.ProposalHelper{
		{Handle: "attendee", Brief: "Check invitations", ReportsTo: "manager"},
		{Handle: "attendee2", Brief: "Lead attendee planning", ReportsTo: "manager"},
		{Handle: "abcdefghijkl", Brief: "Check access", ReportsTo: "attendee2"},
		{Handle: "abcdefghijk2", Brief: "Check timing", ReportsTo: "manager"},
	}
	if p == nil || !reflect.DeepEqual(p.Helpers, want) {
		t.Fatalf("proposal helpers = %+v, want %+v", p, want)
	}
	var streamed []appwire.ProposalHelper
	for i, event := range rec.events[:at] {
		if event != "crew.proposal.delta" {
			continue
		}
		raw, err := json.Marshal(rec.data[i])
		if err != nil {
			t.Fatal(err)
		}
		var delta appwire.CrewProposalDelta
		if err := json.Unmarshal(raw, &delta); err != nil {
			t.Fatal(err)
		}
		var member appwire.ProposalHelper
		if err := json.Unmarshal(delta.Line, &member); err != nil {
			t.Fatal(err)
		}
		if member.Handle != "" {
			streamed = append(streamed, member)
		}
	}
	if !reflect.DeepEqual(streamed, want) {
		t.Fatalf("streamed helpers = %+v, want %+v", streamed, want)
	}
}

func TestAppCrewProposalKeepsOverlongTeamLead(t *testing.T) {
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{
		`{"t":"name","name":"Gathering","goal":"Prepare the gathering"}` + "\n" +
			`{"t":"team","id":"people","name":"People","lead":"attendee-lead"}` + "\n" +
			`{"t":"member","handle":"attendee-lead","brief":"Lead attendee planning","team":"people"}` + "\n" +
			`{"t":"member","handle":"access","brief":"Check access","team":"people","reports_to":"attendee-lead"}` + "\n",
	}}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	p := rec.data[at].(appwire.AskOpened).Ask.Proposal
	if p == nil || len(p.Helpers) != 2 || len(p.Teams) != 1 || p.Helpers[0].Handle != "attendee" || p.Helpers[1].ReportsTo != "attendee" || p.Teams[0].Lead != "attendee" {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestAppCrewProposalTeamDeltaKeepsRepairedLeadOnCollision(t *testing.T) {
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{
		`{"t":"name","name":"Gathering"}` + "\n" +
			`{"t":"member","handle":"attendee","brief":"Check invitations"}` + "\n" +
			`{"t":"team","id":"people","name":"People","lead":"attendee-lead"}` + "\n" +
			`{"t":"member","handle":"attendee-lead","brief":"Lead attendee planning","team":"people"}` + "\n",
	}}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	p := rec.data[at].(appwire.AskOpened).Ask.Proposal
	if p == nil || len(p.Helpers) != 2 || len(p.Teams) != 1 || p.Helpers[1].Handle != "attendee2" || p.Teams[0].Lead != "attendee2" {
		t.Fatalf("proposal = %+v", p)
	}
	for i, event := range rec.events[:at] {
		if event != "crew.proposal.delta" {
			continue
		}
		raw, err := json.Marshal(rec.data[i])
		if err != nil {
			t.Fatal(err)
		}
		var delta appwire.CrewProposalDelta
		if err := json.Unmarshal(raw, &delta); err != nil {
			t.Fatal(err)
		}
		var team appwire.ProposalTeam
		if err := json.Unmarshal(delta.Line, &team); err != nil {
			t.Fatal(err)
		}
		if team.ID == "people" && team.Lead != p.Teams[0].Lead {
			t.Fatalf("team delta lead = %q, final = %q", team.Lead, p.Teams[0].Lead)
		}
	}
}
