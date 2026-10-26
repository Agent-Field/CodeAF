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
	testAppCrewProposalSeatsSixHelpers(t, "scout", "scout", "")
}

func TestAppCrewProposalReservedManagerHandle(t *testing.T) {
	testAppCrewProposalSeatsSixHelpers(t, "system", "system2", "")
}

func TestAppCrewProposalSeatsCellHelpersBeforeReturning(t *testing.T) {
	testAppCrewProposalSeatsSixHelpers(t, "scout", "scout", ".cell")
}

func testAppCrewProposalSeatsSixHelpers(t *testing.T, title, firstHandle, layout string) {
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
		return filepath.Join(helperRoot, fmt.Sprint(minted.Add(1)), layout, "transcript.jsonl"), nil
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
	agents := map[string]bool{}
	for _, member := range crew.Members {
		if agents[member.Agent] {
			t.Fatalf("Make the crew returned repeated agent %q; want one original manager and six distinct helpers", member.Agent)
		}
		agents[member.Agent] = true
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

func TestAppCrewProposalResumesFailedHelper(t *testing.T) {
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
	var fail atomic.Bool
	fail.Store(true)
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		if fail.Load() && strings.Contains(ask.Transcript, string(filepath.Separator)+"2"+string(filepath.Separator)) {
			return nil, fmt.Errorf("scripted engine open failure")
		}
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	request := appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Helpers: []appwire.ProposalHelper{{Handle: "scout", Role: "Scout", Brief: "Find the facts"}, {Handle: "scribe", Brief: "Write the report"}}}}
	if _, err := backend.CrewCreate(request); err == nil {
		t.Fatal("expected second helper to fail")
	}
	crews, err := backend.CrewList(false)
	if err != nil || len(crews) != 1 {
		t.Fatalf("partial crews = %+v, %v", crews, err)
	}
	partial, err := backend.CrewGet(crews[0].ID)
	if err != nil || len(partial.Members) != 2 {
		t.Fatalf("partial crew = %+v, %v", partial, err)
	}
	if err := backend.crewLayer().write(func(f *appCrewLayerFile) error {
		for _, member := range partial.Members {
			if member.Agent != manager.ID {
				f.member(partial.ID, member.Agent).Role = ""
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	persisted := &appCrewLayer{path: backend.crewLayer().path}
	if err := persisted.read(func(f *appCrewLayerFile) {
		extra := f.Crews[partial.ID]
		if extra == nil || extra.Proposal == nil || extra.Proposal.ID != proposal {
			t.Error("proposal binding was not persisted")
		}
	}); err != nil {
		t.Fatal(err)
	}
	fail.Store(false)
	crew, err := backend.CrewCreate(request)
	if err != nil {
		t.Fatal(err)
	}
	if crew.ID != partial.ID || len(crew.Members) != 3 {
		t.Fatalf("resumed crew = %+v", crew)
	}
	for _, member := range crew.Members {
		if member.Handle == "scout" && member.Role == "" {
			t.Fatal("retry lost seated helper role")
		}
	}
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
	for _, member := range partial.Members {
		found := false
		for _, resumed := range crew.Members {
			if resumed.Agent == member.Agent {
				found = true
			}
		}
		if !found {
			t.Fatalf("retry lost seated member %s", member.Agent)
		}
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

func TestAppCrewProposalCanWaitInInboxAndComeBack(t *testing.T) {
	backend, rec, manager := proposingBackend(t, &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal})
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[at].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	id := manager.ID + "/" + ask.ID
	if err := backend.LaterInbox(id, true); err != nil {
		t.Fatal(err)
	}
	items, counts, err := backend.ListInbox(appwire.InboxLater, "")
	if err != nil || len(items) != 1 || counts.Later != 1 || !items[0].Ask.Later || items[0].Ask.Proposal.ID != ask.Proposal.ID {
		t.Fatalf("deferred proposal = %+v, %+v, %v", items, counts, err)
	}
	reloaded := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { reloaded.Close() })
	reloaded.state = &appState{path: backend.state.where()}
	reloaded.profileDir = backend.profileDir
	reloaded.crews()
	items, _, err = reloaded.ListInbox(appwire.InboxLater, "")
	if err != nil || len(items) != 1 || !items[0].Ask.Later {
		t.Fatalf("reloaded deferred proposal = %+v, %v", items, err)
	}
	reloaded.Close()
	if err := backend.LaterInbox(id, false); err != nil {
		t.Fatal(err)
	}
	items, counts, err = backend.ListInbox(appwire.InboxWaiting, "")
	if err != nil || len(items) != 1 || counts.Later != 0 || items[0].Ask.Later || items[0].Ask.Proposal.ID != ask.Proposal.ID {
		t.Fatalf("restored proposal = %+v, %+v, %v", items, counts, err)
	}
	crew, err := backend.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: ask.Proposal.ID, Edits: &appwire.Proposal{Helpers: []appwire.ProposalHelper{}}})
	if err != nil || crew.Manager != manager.ID {
		t.Fatalf("accepted restored proposal = %+v, %v", crew, err)
	}
}

func TestAppCrewProposalLaterCannotReopenAnsweredAsk(t *testing.T) {
	backend, rec, manager := proposingBackend(t, &proposingConversation{fakeConversation: &fakeConversation{}, answer: helpersProposal})
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	ask := rec.data[at].(appwire.AskOpened).Ask
	rec.mu.Unlock()
	blocked, release := make(chan struct{}), make(chan struct{})
	original := backend.emit
	backend.emit = intentTitleEmitter(func(event string, data any) {
		if opened, ok := data.(appwire.AskOpened); ok && opened.Ask.ID == ask.ID && opened.Ask.Later {
			close(blocked)
			<-release
		}
		original.Emit(event, data)
	})
	laterDone := make(chan error, 1)
	go func() { laterDone <- backend.LaterInbox(manager.ID+"/"+ask.ID, true) }()
	<-blocked
	answerDone := make(chan error, 1)
	go func() { answerDone <- backend.AnswerAsk(manager.ID, ask.ID, appwire.ProposalKeep, "") }()
	var answerErr error
	answered := false
	select {
	case answerErr = <-answerDone:
		answered = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-laterDone; err != nil {
		t.Fatal(err)
	}
	if !answered {
		answerErr = <-answerDone
	}
	if answerErr != nil {
		t.Fatal(answerErr)
	}
	items, _, err := backend.ListInbox(appwire.InboxLater, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("answered proposal resurrected in Later: count=%d, error=%v", len(items), err)
	}
}
