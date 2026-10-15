package main

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// crewBackend is a work backend whose crews are the teams of a profile of its
// own, with one team whose manager is the backend's held fake agent and one
// member that is only on disk.
func crewBackend(t *testing.T) (*appBackend, *appRecorder, appwire.Agent, string) {
	t.Helper()
	return crewBackendOn(t, &fakeConversation{})
}

// crewBackendOn is [crewBackend] whose conversations are conv.
func crewBackendOn(t *testing.T, conv *fakeConversation) (*appBackend, *appRecorder, appwire.Agent, string) {
	t.Helper()
	backend, rec, agent := workBackend(t, conv)
	backend.profileDir = t.TempDir()
	id := teamstore.NewID()
	err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "harbor", Made: time.Now()})
		if err := f.AddMember(id, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Where: "/w", Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(id, teamstore.Member{Key: "/state/v3/places/proj/b0b0b0b0/transcript.jsonl", File: "/state/v3/places/proj/b0b0b0b0/transcript.jsonl", Where: "/w", Word: "web", Handle: "web"}); err != nil {
			return err
		}
		return f.SetManager(id, appFakeTranscript)
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend, rec, agent, id
}

// A CREW IS ITS TEAM, FIELD FOR FIELD: the same id, name, manager and members
// (by agent id), a tint derived from the team's hue, the settings in force with
// where each comes from, and the app layer's fields kept beside the teams file,
// never in it.
func TestAppCrewIsItsTeamAndTheLayerStaysOutOfTheTeamsFile(t *testing.T) {
	backend, _, agent, id := crewBackend(t)
	crews, err := backend.CrewList(false)
	if err != nil || len(crews) != 1 {
		t.Fatalf("crews = %+v, %v", crews, err)
	}
	crew := crews[0]
	if crew.ID != id || crew.Name != "harbor" || crew.Manager != agent.ID || len(crew.Members) != 2 || crew.State == "" {
		t.Fatalf("the crew = %+v", crew)
	}
	if crew.Members[1].Agent != "b0b0b0b0" || crew.Members[1].Handle != "web" {
		t.Fatalf("the member = %+v", crew.Members[1])
	}
	if crew.Effective.CapFrom.Kind == "" || crew.Effective.DepthLimit == 0 {
		t.Fatalf("the settings in force = %+v", crew.Effective)
	}
	sage := "sage"
	goal := "ship the release"
	updated, err := backend.CrewUpdate(appwire.CrewUpdate{ID: id, Tint: &sage, Goal: &goal})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Tint != "sage" || updated.Hue != 145 || updated.Goal != goal {
		t.Fatalf("after the update = %+v", updated)
	}
	if _, err := backend.CrewLink(id, "pr", "#1830", "memory"); err != nil {
		t.Fatal(err)
	}
	items, _ := backend.CrewItems(id)
	if len(items) != 1 || items[0].Ref != "#1830" {
		t.Fatalf("the items = %+v", items)
	}
	raw, err := os.ReadFile(teamstore.Path(backend.profileDir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), goal) || strings.Contains(string(raw), "#1830") {
		t.Fatalf("the app layer reached the teams file:\n%s", raw)
	}
}

// A DECISION ON THE PERSON IS AN ASK on the manager that brought it, with the
// path it climbed; answering the ask decides the packet in the store, and the
// ask closes with its receipt.
func TestAppCrewDecisionIsAnAskAndAnsweringItDecides(t *testing.T) {
	backend, rec, agent, id := crewBackend(t)
	backend.crews()
	p, err := teamstore.Raise(backend.profileDir, teamstore.Packet{
		Kind: teamstore.PacketQuestion, Team: teamstore.Person, Origin: id, RaisedBy: teamstore.FromManager,
		Question: "Ship on Friday or Monday?",
		Options: []teamstore.Option{
			{ID: "fri", Label: "Friday", Consequence: "ships before the weekend"},
			{ID: "mon", Label: "Monday", Consequence: "one more day of tests"},
		},
		Recommendation: &teamstore.Recommendation{Option: "mon", Reason: "the suite is still red"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var run *appCrewRun
	backend.locked(func() { run = backend.crewRun })
	backend.crewTick(run, false)
	var opened appwire.Ask
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		if ok && o.Ask.Subkind == appwire.SubkindCrewDecision {
			opened = o.Ask
		}
		return ok && o.Ask.Subkind == appwire.SubkindCrewDecision
	})
	if opened.Agent != agent.ID || opened.ID != appCrewAskPrefix+p.ID || len(opened.Path) != 2 || opened.Path[0] != "harbor" || opened.Path[1] != "you" {
		t.Fatalf("the ask = %+v", opened)
	}
	asks, _ := backend.ListAsks(agent.ID)
	if len(asks) == 0 || asks[len(asks)-1].ID != opened.ID {
		t.Fatalf("the agent's asks = %+v", asks)
	}
	if err := backend.AnswerAsk(agent.ID, opened.ID, "mon", ""); err != nil {
		t.Fatal(err)
	}
	decided, err := teamstore.PacketByID(backend.profileDir, p.ID)
	if err != nil || decided.State != teamstore.PacketDecided || decided.Decision != "mon" || decided.DecidedBy != teamstore.Person {
		t.Fatalf("the packet = %+v, %v", decided, err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AskClosed)
		return ok && c.ID == opened.ID
	})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.ID == "r"+opened.ID && e.Entry.Kind == appwire.EntryReceipt
	})
}

// A NEW LINE ON A CREW'S LOG IS SAID AS IT LANDS, and closing a crew keeps it
// readable, closed, until it is reopened as it was.
func TestAppCrewLogLinesAndCloseReopen(t *testing.T) {
	backend, rec, _, id := crewBackend(t)
	backend.crews()
	if err := teamstore.AppendTraffic(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindNote, From: "web", To: teamstore.ToManager, Text: "tests pass"}); err != nil {
		t.Fatal(err)
	}
	var run *appCrewRun
	backend.locked(func() { run = backend.crewRun })
	backend.crewTick(run, false)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		l, ok := data.(appwire.CrewLogEvent)
		return ok && l.Line.Text == "tests pass" && l.Line.Agent == "b0b0b0b0" && l.Line.Crew == id
	})
	lines, cursor, err := backend.CrewLog(id, "", 0)
	if err != nil || len(lines) == 0 || cursor == "" {
		t.Fatalf("the log = %+v %q %v", lines, cursor, err)
	}
	if err := backend.CrewClose(id); err != nil {
		t.Fatal(err)
	}
	closed, _ := backend.CrewList(true)
	if len(closed) != 1 || closed[0].State != appwire.CrewClosed || closed[0].ClosedAt == nil {
		t.Fatalf("the closed crews = %+v", closed)
	}
	open, _ := backend.CrewList(false)
	if len(open) != 0 {
		t.Fatalf("a closed crew is still open: %+v", open)
	}
	again, err := backend.CrewReopen(id)
	if err != nil || again.State == appwire.CrewClosed || len(again.Members) != 2 {
		t.Fatalf("reopened = %+v, %v", again, err)
	}
	var wire *appwire.Error
	if _, err := backend.CrewDelete(id); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("an open crew was deleted: %v", err)
	}
}

// A PROPOSAL IS READ FROM THE AGENT'S ONE JSON BLOCK; a helper without a usable
// handle or a brief is left out, and an answer with none is no proposal.
func TestAppProposalIsReadFromTheAgentsAnswer(t *testing.T) {
	p, ok := appParseProposal("Here it is:\n```json\n{\"name\": \"release\", \"goal\": \"ship\", \"helpers\": [{\"handle\": \"@Web\", \"role\": \"frontend\", \"brief\": \"fix the page\"}, {\"handle\": \"x\", \"brief\": \"too short\"}]}\n```")
	if !ok || p.Name != "release" || len(p.Helpers) != 1 || p.Helpers[0].Handle != "web" || p.Helpers[0].Role != "frontend" {
		t.Fatalf("the proposal = %+v, %v", p, ok)
	}
	if _, ok := appParseProposal("I would rather do it alone."); ok {
		t.Fatal("words with no proposal were read as one")
	}
}

// A TINT IS THE NAMED HUE NEAREST THE TEAM'S, and graphite for the root and an
// uncoloured team.
func TestAppTintIsTheNearestNamedHue(t *testing.T) {
	cases := []struct {
		hue  float64
		want string
	}{{10, "rose"}, {350, "rose"}, {80, "sand"}, {150, "sage"}, {220, "tide"}, {290, "iris"}}
	for _, c := range cases {
		team := teamstore.Team{}
		team.SetHue(teamstore.HueSpec{Hue: c.hue})
		if got := appTintOf(team); got != c.want {
			t.Fatalf("hue %v = %s, want %s", c.hue, got, c.want)
		}
	}
	if got := appTintOf(teamstore.Team{}); got != appTintGraphite {
		t.Fatalf("an uncoloured team = %s", got)
	}
}

// A CREW MOVES UNDER ANOTHER, AND ONE MADE UNDER A PARENT STAYS THERE. A
// member that runs the crew under its own is that crew in the parent's view,
// not a plain member, and a move the check refuses is refused in words.
func TestAppCrewsNestByMoveAndByCreate(t *testing.T) {
	backend, _, _, harbor := crewBackend(t)
	web := teamstore.Member{Key: "/state/v3/places/proj/b0b0b0b0/transcript.jsonl", File: "/state/v3/places/proj/b0b0b0b0/transcript.jsonl", Where: "/w", Word: "web", Handle: "web"}
	api := teamstore.Member{Key: "/state/v3/places/proj/c0c0c0c0/transcript.jsonl", File: "/state/v3/places/proj/c0c0c0c0/transcript.jsonl", Where: "/w", Word: "api", Handle: "api"}
	dock := teamstore.NewID()
	err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: dock, Name: "dock", Made: time.Now()})
		if err := f.AddMember(dock, api); err != nil {
			return err
		}
		return f.SetManager(dock, api.Key)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.CrewMove([]string{dock}, harbor); err != nil {
		t.Fatalf("the move was refused: %v", err)
	}
	if err := backend.CrewMove([]string{harbor}, dock); err == nil || !strings.Contains(err.Error(), "inside it") {
		t.Fatalf("a crew went under its own child: %v", err)
	}
	pier, err := backend.newTeam("pier", harbor, []teamstore.Member{web}, web.Key, nil, nil, nil)
	if err != nil {
		t.Fatalf("a crew made under a parent was refused: %v", err)
	}
	crew, err := backend.CrewGet(pier)
	if err != nil || crew.Parent != harbor {
		t.Fatalf("the new crew = %+v, %v", crew, err)
	}
	parent, err := backend.CrewGet(harbor)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range parent.Members {
		if m.Handle == "web" && m.Leads != pier {
			t.Fatalf("the member running pier reads as a plain member: %+v", m)
		}
		if m.Handle == "lead" && m.Leads != "" {
			t.Fatalf("the parent's manager leads a child it does not run: %+v", m)
		}
	}
}

// A CREW PROPOSAL OUTLIVES A RESTART: it is kept in the app layer until it is
// answered, and its ask comes back whole, carrying the proposal itself.
func TestAppCrewProposalIsAskedAgainWhole(t *testing.T) {
	backend, rec, agent, _ := crewBackend(t)
	p := appwire.Proposal{ID: "prop1", Agent: agent.ID, Name: "site", Goal: "ship the site",
		Helpers: []appwire.ProposalHelper{{Handle: "builder", Role: "writes pages", Brief: "write index.html"}}}
	if err := backend.crewLayer().write(func(f *appCrewLayerFile) error {
		if f.Proposals == nil {
			f.Proposals = map[string]*appwire.Proposal{}
		}
		f.Proposals[p.ID] = &p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	backend.reopenProposals(backend.crews())
	rec.waitFor(t, time.Second, func(event string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Proposal != nil && opened.Ask.Proposal.Helpers[0].Handle == "builder"
	})
	asks, err := backend.ListAsks(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ask := range asks {
		found = found || ask.Subkind == appwire.SubkindCrewProposal && ask.Proposal != nil && ask.Proposal.Name == "site"
	}
	if !found {
		t.Fatalf("the proposal's ask is not listed: %+v", asks)
	}
}

// CLEARING A CREW'S HUE LEAVES IT UNCOLOURED ACROSS RELOADS, A LATER TINT
// COLOURS IT AGAIN, AND ASKING FOR BOTH IN ONE UPDATE IS REFUSED.
func TestAppCrewUpdateClearHuePersistsAndConflictsAreRefused(t *testing.T) {
	backend, _, _, id := crewBackend(t)
	sage, rose := "sage", "rose"
	if _, err := backend.CrewUpdate(appwire.CrewUpdate{ID: id, Tint: &sage}); err != nil {
		t.Fatal(err)
	}
	hue := 40.0
	for _, ask := range []appwire.CrewUpdate{
		{ID: id, Hue: &hue, Clear: []string{"hue"}},
		{ID: id, Tint: &rose, Clear: []string{"hue"}},
	} {
		if _, err := backend.CrewUpdate(ask); err == nil {
			t.Fatalf("%+v was accepted", ask)
		}
	}
	crew, err := backend.CrewUpdate(appwire.CrewUpdate{ID: id, Clear: []string{"hue"}})
	if err != nil || crew.Tint != appTintGraphite {
		t.Fatalf("cleared crew = %+v, %v", crew, err)
	}
	for i := 0; i < 2; i++ {
		f, err := teamstore.LoadHued(backend.profileDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tm := f.Teams[teamstore.Index(f.Teams, id)]; tm.Hued() || !tm.HueCleared() {
			t.Fatalf("reload %d: %+v", i, tm)
		}
	}
	if crew, err = backend.CrewUpdate(appwire.CrewUpdate{ID: id, Tint: &rose}); err != nil || crew.Tint != "rose" {
		t.Fatalf("recoloured crew = %+v, %v", crew, err)
	}
}

// AN OPEN CREW'S MEMBER IS NEVER READ CLOSED FOR AGE, AND IS SAID ONCE WHEN THE
// CREW OPENS IT. A member nobody spoke to for longer than the idle-close
// setting lists as closed only while no open crew keeps it; the crew's own look
// opens it, and the app is told with one agent.changed, as heldLive tells it.
func TestAppAgedCrewMemberIsKeptOpenAndSaidOnceWhenHeld(t *testing.T) {
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "00000000000000b1", "hello", time.Now().Add(-10*24*time.Hour))
	transcript := session.Place{Dir: dir}.Transcript()

	gate := make(chan struct{})
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		<-gate
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	err = teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		id := teamstore.NewID()
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "harbor", Made: time.Now()})
		return f.AddMember(id, teamstore.Member{Key: transcript, File: transcript, Where: "/w", Word: "old", Handle: "old"})
	})
	if err != nil {
		t.Fatal(err)
	}
	agentState := func() (string, string) {
		agents, err := backend.ListAgents("")
		if err != nil || len(agents) != 1 {
			t.Fatalf("agents = %+v, %v", agents, err)
		}
		return agents[0].ID, agents[0].State
	}
	id, state := agentState()
	if state != appwire.StateClosed {
		t.Fatalf("before any crew keeps it, the aged member reads %q", state)
	}

	run := backend.crews()
	backend.crewTick(run, false)
	// The open is still waiting at the gate: the crew keeps the row open anyway.
	if _, state := agentState(); state == appwire.StateClosed {
		t.Fatal("an open crew's aged member listed closed")
	}
	close(gate)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == id && c.Agent.State != appwire.StateClosed
	})
	backend.crewTick(run, false)
	changed := 0
	rec.mu.Lock()
	for _, data := range rec.data {
		if c, ok := data.(appwire.AgentChanged); ok && c.Agent.ID == id {
			changed++
		}
	}
	rec.mu.Unlock()
	if changed != 1 {
		t.Fatalf("agent.changed said %d times for the held member, want 1", changed)
	}
}
