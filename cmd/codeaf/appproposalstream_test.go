package main

import (
	"context"
	"encoding/json"
	"errors"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

type streamingProposalConversation struct {
	*proposingConversation
	chunks []string
	stop   error
}

func (p *streamingProposalConversation) SideAskStream(ctx context.Context, question string, text func(string)) (string, error) {
	for _, chunk := range p.chunks {
		text(chunk)
	}
	return strings.Join(p.chunks, ""), p.stop
}

func TestAppCrewProposalStreamsCompleteLines(t *testing.T) {
	lines := []string{
		`{"t":"shape","size":2,"teams":0,"why":"two checks"}`,
		`{"t":"name","name":"Launch checks","goal":"Check the launch"}`,
		`{"t":"member","handle":"review","role":"Reviewer","brief":"Review the plan","reports_to":"manager"}`,
		`{"t":"member","handle":"verify","brief":"Check the claims","reports_to":"review"}`,
	}
	body := strings.Join(lines[:3], "\n") + "\nnot json\n" + `{"t":"member","handle":"everyone","brief":"Reserved address"}` + "\n" + `{"t":"member","handle":"Bad","brief":"Invalid handle"}` + "\n" + `{"t":"member","handle":"review","brief":"Duplicate handle"}` + "\n" + lines[3] + "\n"
	model, workspace := helpersEnvironment(t)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	model.mu.Lock()
	model.proposal = body
	model.proposalRelease = release
	model.mu.Unlock()
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	defer func() { unblock(); backend.Close() }()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool { return event == "crew.proposal.delta" })
	unblock()
	at := rec.waitFor(t, 30*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var got []string
	for i, event := range rec.events[:at] {
		if event != "crew.proposal.delta" {
			continue
		}
		raw, err := json.Marshal(rec.data[i])
		if err != nil {
			t.Fatal(err)
		}
		var delta struct {
			Agent string
			Seq   int
			Line  json.RawMessage
		}
		if err := json.Unmarshal(raw, &delta); err != nil {
			t.Fatal(err)
		}
		if delta.Agent != agent.ID || delta.Seq != len(got)+1 {
			t.Fatalf("delta = %s", raw)
		}
		got = append(got, string(delta.Line))
	}
	if !reflect.DeepEqual(got, lines) {
		t.Fatalf("deltas = %q, want %q", got, lines)
	}
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal
	if proposal == nil || proposal.Name != "Launch checks" || len(proposal.Helpers) != 2 {
		t.Fatalf("proposal = %+v", proposal)
	}
	want := []appwire.ProposalHelper{
		{Handle: "review", Role: "Reviewer", Brief: "Review the plan", ReportsTo: "manager"},
		{Handle: "verify", Brief: "Check the claims", ReportsTo: "review"},
	}
	if proposal.Goal != "Check the launch" || !reflect.DeepEqual(proposal.Helpers, want) || proposal.Note != "" {
		t.Fatalf("assembled proposal = %+v", proposal)
	}
}

func TestAppCrewProposalKeepsMembersAfterStreamStops(t *testing.T) {
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{
		`{"t":"name","name":"Checks","goal":"Check the plan"}` + "\n" + `{"t":"member","handle":"review","brief":"Review the plan","reports_to":"manager"}` + "\n" + `{"t":"member","handle":"half`,
	}, stop: errors.New("stream disconnected")}
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
	ask := rec.data[at].(appwire.AskOpened).Ask
	if ask.Proposal == nil || len(ask.Proposal.Helpers) != 1 || !strings.Contains(ask.Detail, "Stopped after 1 helper") {
		t.Fatalf("ask = %+v", ask)
	}
}

func TestAppCrewProposalOrgAcceptStaysFlat(t *testing.T) {
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{
		`{"t":"shape","size":4,"teams":2,"why":"two separate streams"}` + "\n" +
			`{"t":"name","name":"Workshop","goal":"Prepare the workshop"}` + "\n" +
			`{"t":"team","id":"venue","name":"Venue","lead":"places"}` + "\n" +
			`{"t":"team","id":"invite","name":"Invitations","lead":"outreach"}` + "\n" +
			`{"t":"member","handle":"places","brief":"Lead the venue plan","team":"venue","reports_to":"manager"}` + "\n" +
			`{"t":"member","handle":"outreach","brief":"Lead invitations","team":"invite","reports_to":"manager"}` + "\n" +
			`{"t":"member","handle":"access","brief":"Check venue access","team":"venue","reports_to":"places"}` + "\n" +
			`{"t":"member","handle":"guests","brief":"Check attendee needs","team":"invite","reports_to":"outreach"}` + "\n",
	}}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := *rec.data[i].(appwire.AskOpened).Ask.Proposal
	rec.mu.Unlock()
	wantTeams := []appwire.ProposalTeam{{ID: "venue", Name: "Venue", Lead: "places"}, {ID: "invite", Name: "Invitations", Lead: "outreach"}}
	if !reflect.DeepEqual(proposal.Teams, wantTeams) || len(proposal.Helpers) != 4 {
		t.Fatalf("proposal = %+v", proposal)
	}
	backend.mint = func(string) (string, error) { return filepath.Join(t.TempDir(), "transcript.jsonl"), nil }
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	crew, err := backend.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: proposal.ID})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		crew, err = backend.CrewGet(crew.ID)
		if err == nil && len(crew.Members) == 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || len(crew.Members) != 5 {
		t.Fatalf("crew = %+v, %v", crew, err)
	}
	byHandle := map[string]appwire.CrewMember{}
	for _, member := range crew.Members {
		byHandle[member.Handle] = member
	}
	if got := byHandle["access"].ReportsTo; got == nil || got.Manager != byHandle["places"].Agent {
		t.Fatalf("access reports to %+v", got)
	}
	file, err := teamstore.Load(backend.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Teams) != 1 {
		t.Fatalf("created %d crews", len(file.Teams))
	}
	team, _ := file.Team(crew.ID)
	member, _ := team.ByHandle("access")
	if member.ReportsTo != "places" {
		t.Fatalf("saved member = %+v", member)
	}
}

func TestAppCrewProposalStoppedWithoutMembersIsQuiet(t *testing.T) {
	conv := &streamingProposalConversation{proposingConversation: &proposingConversation{fakeConversation: &fakeConversation{}}, chunks: []string{`{"t":"shape","size":2,"teams":1,"why":"two checks"}` + "\n"}, stop: errors.New("stream disconnected")}
	backend, rec, agent := proposingBackend(t, conv.proposingConversation)
	backend.mu.Lock()
	backend.agents[agent.ID].live.conn.Conv = conv
	backend.mu.Unlock()
	if err := backend.CrewPropose(agent.ID); err != nil {
		t.Fatal(err)
	}
	if got := notice(t, rec, agent.ID); !strings.HasPrefix(got, appNoAnswer+" · ") {
		t.Fatalf("notice = %q", got)
	}
}
