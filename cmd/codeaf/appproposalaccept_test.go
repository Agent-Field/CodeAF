package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestAppCrewProposalSeatsSixHelpersBeforeReturning(t *testing.T) {
	testAppCrewProposalSeatsSixHelpers(t, "scout", "scout")
}

func TestAppCrewProposalReservedManagerHandle(t *testing.T) {
	testAppCrewProposalSeatsSixHelpers(t, "system", "system2")
}

func testAppCrewProposalSeatsSixHelpers(t *testing.T, title, firstHandle string) {
	t.Helper()
	helperRoot := t.TempDir()
	var minted atomic.Int64
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
	backend, rec, manager := proposingBackend(t, conv)
	if _, err := backend.Rename(manager.ID, title); err != nil {
		t.Fatal(err)
	}
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal.ID
	rec.mu.Unlock()
	backend.mint = func(string) (string, error) {
		return filepath.Join(helperRoot, fmt.Sprint(minted.Add(1)), "transcript.jsonl"), nil
	}
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	var helpers []appwire.ProposalHelper
	for _, handle := range []string{firstHandle, "arch", "scribe", "tabul", "fact", "polish"} {
		helpers = append(helpers, appwire.ProposalHelper{Handle: handle, Brief: "Own the " + handle + " assignment", ReportsTo: "manager"})
	}
	crew, err := backend.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Helpers: helpers}})
	if err != nil {
		t.Fatal(err)
	}
	if crew.Manager != manager.ID || len(crew.Members) != 7 {
		t.Fatalf("Make the crew returned manager=%s members=%+v; want original manager and six seated helpers", crew.Manager, crew.Members)
	}
	byHandle := map[string]appwire.CrewMember{}
	for _, member := range crew.Members {
		byHandle[member.Handle] = member
	}
	for _, helper := range helpers {
		member, ok := byHandle[helper.Handle]
		if !ok || member.Agent == manager.ID {
			t.Fatalf("helper %s = %+v", helper.Handle, member)
		}
		if member.ReportsTo == nil || member.ReportsTo.Manager != manager.ID {
			t.Fatalf("helper %s reports to %+v", helper.Handle, member.ReportsTo)
		}
		if brief, err := backend.MemberBrief(crew.ID, member.Agent); err != nil || brief.Text != helper.Brief {
			t.Fatalf("brief for %s = %+v, %v", helper.Handle, brief, err)
		}
	}
	file, err := teamstore.Load(backend.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(crew.ID)
	entries, err := teamstore.ReadTraffic(backend.profileDir, crew.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	run := backend.crews()
	for _, entry := range entries {
		if entry.Kind == teamstore.KindStart {
			if file.ClaimStart(crew.ID, entry.ID) {
				t.Fatalf("start %s was not claimed before returning", entry.ID)
			}
			backend.crewAct(run, appCrewRead{file: file}, team, entry)
		}
	}
	again, err := backend.CrewGet(crew.ID)
	if err != nil || len(again.Members) != 7 || again.Manager != manager.ID {
		t.Fatalf("replayed crew = %+v, %v", again, err)
	}
}

func TestAppCrewProposalRejectsDuplicateHelpersBeforeCreating(t *testing.T) {
	conv := &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal}
	backend, rec, manager := proposingBackend(t, conv)
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal.ID
	rec.mu.Unlock()
	helpers := []appwire.ProposalHelper{}
	for _, handle := range []string{"scout", "arch", "scribe", "tabul", "fact", "scout"} {
		helpers = append(helpers, appwire.ProposalHelper{Handle: handle, Brief: "Own the assignment"})
	}
	if _, err := backend.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Helpers: helpers}}); err == nil || !strings.Contains(err.Error(), "proposed more than once") {
		t.Fatal("duplicate helper handles created a crew")
	}
	crews, err := backend.CrewList(false)
	if err != nil || len(crews) != 0 {
		t.Fatalf("crews after rejected proposal = %+v, %v", crews, err)
	}
}
