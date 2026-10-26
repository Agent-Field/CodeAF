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

// A HELPER WHOSE CONVERSATION CANNOT OPEN IS STILL SEATED. crews.create does
// not wait for helpers' conversations to open (a fresh open of a large folder
// takes many seconds each), so an open that fails is said beside the manager
// and left to the crew's hold to try again; the crew is made whole, and a
// repeated acceptance answers the same crew without starting anyone twice.
func TestAppCrewProposalSeatsAHelperWhoseOpenFails(t *testing.T) {
	helperRoot := t.TempDir()
	backend, rec, manager := proposingBackend(t, &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal})
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal.ID
	rec.mu.Unlock()
	var minted atomic.Int64
	backend.mint = func(string) (string, error) {
		return filepath.Join(helperRoot, fmt.Sprint(minted.Add(1)), "transcript.jsonl"), nil
	}
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		if strings.Contains(ask.Transcript, string(filepath.Separator)+"2"+string(filepath.Separator)) {
			return nil, fmt.Errorf("scripted engine open failure")
		}
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	request := appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Helpers: []appwire.ProposalHelper{{Handle: "scout", Role: "Scout", Brief: "Find the facts"}, {Handle: "scribe", Brief: "Write the report"}}}}
	crew, err := backend.CrewCreate(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(crew.Members) != 3 {
		t.Fatalf("crew = %+v; want the manager and both helpers seated", crew.Members)
	}
	for _, member := range crew.Members {
		if member.Handle == "scout" && member.Role == "" {
			t.Fatal("a seated helper lost its role")
		}
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		added, ok := data.(appwire.EntryAdded)
		return ok && added.Entry.Agent == manager.ID && added.Entry.Kind == appwire.EntryNotice && strings.Contains(added.Entry.Text, "could not start @scribe")
	})
	again, err := backend.CrewCreate(request)
	if err != nil || again.ID != crew.ID || len(again.Members) != 3 {
		t.Fatalf("repeated acceptance = %+v, %v", again, err)
	}
	entries, err := teamstore.ReadTraffic(backend.profileDir, crew.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	for _, entry := range entries {
		if entry.Kind == teamstore.KindStart {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("starts=%d; want 2", starts)
	}
}

func TestAppCrewProposalCanCorrectNameBeforeCrewExists(t *testing.T) {
	backend, rec, manager := proposingBackend(t, &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal})
	if err := backend.newTeamAs(teamstore.NewID(), "Taken", "", nil, "", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal.ID
	rec.mu.Unlock()
	request := appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Name: "Taken", Helpers: []appwire.ProposalHelper{}}}
	if _, err := backend.CrewCreate(request); err == nil {
		t.Fatal("expected crew name conflict")
	}
	request.Edits.Name = "Available"
	crew, err := backend.CrewCreate(request)
	if err != nil || crew.Name != "Available" {
		t.Fatalf("corrected proposal = %+v, %v", crew, err)
	}
}
