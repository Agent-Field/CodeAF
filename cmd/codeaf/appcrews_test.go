package main

import (
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
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
		Question: "Ship on Friday or Monday?", Irreversible: true,
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
	if !opened.Irreversible || opened.Agent != agent.ID || opened.ID != appCrewAskPrefix+p.ID || len(opened.Path) != 2 || opened.Path[0] != "harbor" || opened.Path[1] != "you" || opened.DecisionKind != teamstore.PacketQuestion {
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

// ARCHIVING A CREW IS THE ENGINE'S OWN ACT, separate from close and delete: an
// open crew is refused, a closed one is marked, leaves the closed list, shows in
// the archived list and in get with archived_at, keeps its members, survives a
// fresh backend on the same store, and reopens as it was.
func TestAppCrewArchiveIsSeparateFromCloseAndDelete(t *testing.T) {
	backend, rec, _, id := crewBackend(t)
	backend.crews()
	var wire *appwire.Error
	if _, err := backend.CrewArchive(id); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("an open crew was archived: %v", err)
	}
	if open, _ := backend.CrewList(false); len(open) != 1 || open[0].ArchivedAt != nil {
		t.Fatalf("a refused archive changed the crew: %+v", open)
	}
	if _, err := backend.CrewArchive("nope"); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("an unknown crew: %v", err)
	}
	if err := backend.CrewClose(id); err != nil {
		t.Fatal(err)
	}
	crew, err := backend.CrewArchive(id)
	if err != nil || crew.State != appwire.CrewClosed || crew.ArchivedAt == nil || len(crew.Members) != 2 {
		t.Fatalf("archived = %+v, %v", crew, err)
	}
	if closed, _ := backend.CrewList(true); len(closed) != 0 {
		t.Fatalf("an archived crew is still in the closed list: %+v", closed)
	}
	archived, err := backend.CrewListArchived()
	if err != nil || len(archived) != 1 || archived[0].ID != id || archived[0].ClosedAt == nil {
		t.Fatalf("the archived crews = %+v, %v", archived, err)
	}
	if got, err := backend.CrewGet(id); err != nil || got.ArchivedAt == nil {
		t.Fatalf("get = %+v, %v", got, err)
	}
	var run *appCrewRun
	backend.locked(func() { run = backend.crewRun })
	backend.crewTick(run, false)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.CrewChanged)
		return ok && c.Crew.ID == id && c.Crew.ArchivedAt != nil
	})
	// The store itself, read fresh, holds the archive.
	f, err := teamstore.Load(backend.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if team, _ := f.Team(id); !team.Archived() || len(team.Members) != 2 {
		t.Fatalf("the store after reload = %+v", team)
	}
	back, err := backend.CrewReopen(id)
	if err != nil || back.State == appwire.CrewClosed || back.ArchivedAt != nil || len(back.Members) != 2 {
		t.Fatalf("reopened = %+v, %v", back, err)
	}
	if list, _ := backend.CrewListArchived(); len(list) != 0 {
		t.Fatalf("a reopened crew is still archived: %+v", list)
	}
}

// A SELECTED CLOSED CHILD IS ARCHIVED ALONE: its closed parent and sibling stay
// in the closed list, nothing is deleted, the child's record is whole, it cannot
// be reopened under the closed parent, and reopening the parent brings the family
// back with the mark cleared.
func TestAppCrewArchiveOfAClosedChildTouchesOnlyTheChild(t *testing.T) {
	backend, _, _, parent := crewBackend(t)
	child, sibling := teamstore.NewID(), teamstore.NewID()
	err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for _, id := range []string{child, sibling} {
			f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "sub-" + id[:4], Parent: parent, Made: time.Now()})
			if err := f.AddMember(id, teamstore.Member{Key: "/k/" + id, File: "/k/" + id, Where: "/w", Word: "w", Handle: "w" + id[:3]}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	backend.crews()
	var wire *appwire.Error
	if _, err := backend.CrewArchive(child); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("an open child was archived: %v", err)
	}
	if err := backend.CrewClose(parent); err != nil {
		t.Fatal(err)
	}
	got, err := backend.CrewArchive(child)
	if err != nil || got.ID != child || got.ArchivedAt == nil || len(got.Members) != 1 {
		t.Fatalf("archived child = %+v, %v", got, err)
	}
	closed, _ := backend.CrewList(true)
	ids := map[string]bool{}
	for _, c := range closed {
		ids[c.ID] = true
		if c.ArchivedAt != nil {
			t.Fatalf("a closed-list crew is archived: %+v", c)
		}
	}
	if len(closed) != 2 || !ids[parent] || !ids[sibling] || ids[child] {
		t.Fatalf("the closed list = %v", ids)
	}
	if archived, _ := backend.CrewListArchived(); len(archived) != 1 || archived[0].ID != child {
		t.Fatalf("the archived list = %+v", archived)
	}
	if _, err := backend.CrewArchive(child); err != nil {
		t.Fatalf("archiving twice: %v", err)
	}
	if again, _ := backend.CrewGet(child); again.ArchivedAt == nil || !again.ArchivedAt.Equal(*got.ArchivedAt) {
		t.Fatalf("archiving twice moved the time: %+v", again.ArchivedAt)
	}
	if _, err := backend.CrewReopen(child); !errors.As(err, &wire) || wire.Code != appwire.CodeBadParams {
		t.Fatalf("a child reopened under a closed parent: %v", err)
	}
	if still, _ := backend.CrewGet(child); still.ArchivedAt == nil || still.State != appwire.CrewClosed {
		t.Fatalf("the refused reopen changed the child: %+v", still)
	}
	if _, err := backend.CrewReopen(parent); err != nil {
		t.Fatal(err)
	}
	if back, _ := backend.CrewGet(child); back.State == appwire.CrewClosed || back.ArchivedAt != nil || len(back.Members) != 1 {
		t.Fatalf("the child after the parent reopened = %+v", back)
	}
	for _, id := range []string{parent, child, sibling} {
		if _, err := backend.CrewGet(id); err != nil {
			t.Fatalf("crew %s was lost: %v", id, err)
		}
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

// An open crew keeps its aged member visible, but only visiting the idle
// member opens it. The first visit announces one agent.changed.
func TestAppAgedCrewMemberIsLightUntilViewedAndSaidOnceWhenHeld(t *testing.T) {
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
	if backend.isHeld(id) {
		t.Fatal("idle crew member opened before a view")
	}
	if _, err := backend.OpenAgent(id); err != nil {
		t.Fatal(err)
	}
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

// A CREW'S FORMATION IS A FACT THE MANAGER'S CONVERSATION KEEPS, said as the
// fact and never as words: the entry the manager got live is, field for field,
// the entry its record gives back after a restart, under one id, once.
func TestAppCrewFormationReceiptIsTheSameFactLiveAndAfterRestart(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{})
	backend.profileDir = t.TempDir()
	crew, err := backend.CrewCreate(appwire.CrewCreate{Name: "harbor", Agents: []string{agent.ID}, Manager: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	var live []appwire.Entry
	rec.mu.Lock()
	for _, data := range rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && added.Entry.Kind == appwire.EntryReceipt {
			live = append(live, added.Entry)
		}
	}
	rec.mu.Unlock()
	if len(live) != 1 || live[0].ID != "rcrew:"+crew.ID || live[0].Agent != agent.ID {
		t.Fatalf("live receipts = %+v", live)
	}
	if live[0].Text != "" || live[0].Op == nil || live[0].Op.Kind != appwire.OpCrewFormed || live[0].Op.Crew != crew.ID || live[0].Op.Name != "harbor" {
		t.Fatalf("the live receipt is not the bare fact: %+v", live[0])
	}

	// A RESTART: another backend over the same teams file, which has no memory
	// of the live event.
	fresh := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { fresh.Close() })
	fresh.profileDir = backend.profileDir
	back := fresh.crewFormedEntries(agent.ID)
	if len(back) != 1 || !reflect.DeepEqual(back[0], live[0]) {
		t.Fatalf("after restart = %+v, live = %+v", back, live[0])
	}
	// Read among the transcript's own entries it is added once, and a second
	// read does not add it again.
	user := appwire.Entry{ID: "u0", Kind: appwire.EntryUser, At: live[0].At.Add(time.Minute)}
	once := appWithFormed([]appwire.Entry{user}, back, time.Time{})
	if len(once) != 2 || once[0].ID != live[0].ID {
		t.Fatalf("placed = %+v", once)
	}
	if again := appWithFormed(once, back, time.Time{}); len(again) != 2 {
		t.Fatalf("read twice = %+v", again)
	}
	// A crew the conversation did not found is not its fact.
	if other := fresh.crewFormedEntries("0123456789abcdef"); len(other) != 0 {
		t.Fatalf("another agent's = %+v", other)
	}
}

// A MEMBER'S BRIEF IS ITS START LINE, never its first user message: a person's
// later message (or a files-only prompt) to the member does not become what its
// manager told it, it survives a restart, and a member nobody started has none.
func TestAppMemberBriefIsTheManagersStartLineAndNothingElse(t *testing.T) {
	backend, _, _, id := crewBackend(t)
	webAgent := "b0b0b0b0"
	// The member was started by its manager with this brief.
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		for i := range f.Teams {
			if f.Teams[i].ID != id {
				continue
			}
			for j := range f.Teams[i].Members {
				if f.Teams[i].Members[j].Handle == "web" {
					f.Teams[i].Members[j].Started = true
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	const brief = "Own the web client; BIRCH-9304 is yours."
	if err := teamstore.AppendTraffic(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, FromKey: appFakeTranscript, To: "web", Text: brief}); err != nil {
		t.Fatal(err)
	}
	// Then you message it directly: a later line that is not the brief.
	if err := teamstore.AppendTraffic(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindYou, From: teamstore.FromYou, To: "web", Text: "see the attached screenshot"}); err != nil {
		t.Fatal(err)
	}
	got, err := backend.MemberBrief(id, webAgent)
	if err != nil || got.Text != brief || got.From != "lead" {
		t.Fatalf("brief = %+v, %v", got, err)
	}
	// A restart reads the same words from the log.
	fresh := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { fresh.Close() })
	fresh.profileDir = backend.profileDir
	if again, err := fresh.MemberBrief(id, webAgent); err != nil || again.Text != brief {
		t.Fatalf("after restart = %+v, %v", again, err)
	}
	// The manager was not started by a start line: no brief, none invented.
	if none, err := backend.MemberBrief(id, appMemberAgent(teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript})); err != nil || none.Text != "" {
		t.Fatalf("manager brief = %+v, %v", none, err)
	}
	var wire *appwire.Error
	if _, err := backend.MemberBrief(id, "ffffffff"); !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("a stranger has a brief: %v", err)
	}
}

// A MEMBER'S BRIEF IS ITS OWN START LINE BY IDENTITY, not by handle: after the
// member is renamed and a second member takes the old handle, each reads the
// brief it was started with, across a restart, and a member whose line is gone
// or was never recorded has none rather than a neighbour's.
func TestAppMemberBriefSurvivesRenameAndHandleReuse(t *testing.T) {
	backend, _, _, id := crewBackend(t)
	profile := backend.profileDir
	first := "b0b0b0b0"
	const (
		briefOne = "Own the web client; BIRCH-9304 is yours."
		briefTwo = "Take the billing export; ASH-2217 is yours."
	)
	startOne, err := teamstore.AppendTrafficID(profile, id, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "web", Text: briefOne})
	if err != nil {
		t.Fatal(err)
	}
	if err := teamstore.AppendTraffic(profile, id, teamstore.Entry{Kind: teamstore.KindYou, From: teamstore.FromYou, To: "web", Text: "a later prompt, not a brief"}); err != nil {
		t.Fatal(err)
	}
	secondKey := "/state/v3/places/proj/c1c1c1c1/transcript.jsonl"
	secondAgent := appMemberAgent(teamstore.Member{Key: secondKey, File: secondKey})
	// The first member is renamed before anyone else is started under "web".
	err = teamstore.Update(profile, func(f *teamstore.File) error {
		for i := range f.Teams {
			for j := range f.Teams[i].Members {
				if f.Teams[i].Members[j].Handle == "web" {
					f.Teams[i].Members[j].Started, f.Teams[i].Members[j].StartEntry = true, startOne
				}
			}
		}
		return f.SetHandle(id, "/state/v3/places/proj/b0b0b0b0/transcript.jsonl", "api")
	})
	if err != nil {
		t.Fatal(err)
	}
	startTwo, err := teamstore.AppendTrafficID(profile, id, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "web", Text: briefTwo})
	if err != nil {
		t.Fatal(err)
	}
	err = teamstore.Update(profile, func(f *teamstore.File) error {
		return f.AddMember(id, teamstore.Member{Key: secondKey, File: secondKey, Where: "/w", Word: "billing", Handle: "web", Started: true, StartEntry: startTwo})
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(b *appBackend, label string) {
		t.Helper()
		if got, err := b.MemberBrief(id, first); err != nil || got.Text != briefOne || got.From != "lead" {
			t.Fatalf("%s: renamed member = %+v, %v", label, got, err)
		}
		if got, err := b.MemberBrief(id, secondAgent); err != nil || got.Text != briefTwo || got.From != "lead" {
			t.Fatalf("%s: reusing member = %+v, %v", label, got, err)
		}
	}
	check(backend, "live")
	fresh := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { fresh.Close() })
	fresh.profileDir = profile
	check(fresh, "after restart")

	// A recorded line that is gone is unknown: nobody else's words, no manager.
	err = teamstore.Update(profile, func(f *teamstore.File) error {
		for i := range f.Teams {
			for j := range f.Teams[i].Members {
				if f.Teams[i].Members[j].Key == secondKey {
					f.Teams[i].Members[j].StartEntry = "000000000999"
				}
				if f.Teams[i].Members[j].Handle == "api" {
					f.Teams[i].Members[j].StartEntry, f.Teams[i].Members[j].Handle = "", "web"
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := fresh.MemberBrief(id, secondAgent); err != nil || got.Text != "" || got.From != "" || got.At != nil {
		t.Fatalf("absent line = %+v, %v", got, err)
	}
	// The first record kept no id, but the log wrote its key onto the line when
	// it was appended: that is exact provenance, and it is still its own words.
	if got, err := fresh.MemberBrief(id, first); err != nil || got.Text != briefOne {
		t.Fatalf("legacy by recipient key = %+v, %v", got, err)
	}
}

// A STARTED MEMBER'S BRIEF SURVIVES RENAME AND HANDLE REUSE THROUGH THE REAL
// START: the membership crewStart itself writes keeps the id of its start line,
// so nothing here records StartEntry by hand. Two members are started under one
// handle, the first is renamed in between, and each reads its own words, live
// and after a restart.
func TestAppCrewStartRecordsTheStartLineAndTheBriefFollowsIt(t *testing.T) {
	var (
		mu      sync.Mutex
		minted  int
		release int
	)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, nil)
	backend.mint = func(string, ...func(string) error) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		minted++
		return filepath.Join(t.TempDir(), "conv-"+strconv.Itoa(minted), "transcript.jsonl"), nil
	}
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		if ask.Transcript == "" {
			return nil, errors.New("opened without a transcript")
		}
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { mu.Lock(); release++; mu.Unlock() }}, nil
	}
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	backend.profileDir = t.TempDir()
	lead, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	leadMember, err := backend.agentMember(lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	leadMember.Handle = "lead"
	id := teamstore.NewID()
	err = teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "harbor", Made: time.Now()})
		if err := f.AddMember(id, leadMember); err != nil {
			return err
		}
		return f.SetManager(id, leadMember.Key)
	})
	if err != nil {
		t.Fatal(err)
	}
	start := func(brief string) string {
		t.Helper()
		sid, err := teamstore.AppendTrafficID(backend.profileDir, id, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, FromKey: leadMember.Key, To: "web", Text: brief})
		if err != nil {
			t.Fatal(err)
		}
		e, ok, err := teamstore.ReadEntry(backend.profileDir, id, sid)
		if err != nil || !ok {
			t.Fatalf("start line %s: %v %v", sid, ok, err)
		}
		file, err := teamstore.Load(backend.profileDir)
		if err != nil {
			t.Fatal(err)
		}
		team, _ := file.Team(id)
		manager, _ := team.Member(team.Manager)
		backend.crewStart(team, manager, e)
		return sid
	}
	const (
		briefOne = "Own the web client; BIRCH-9304 is yours."
		briefTwo = "Take the billing export; ASH-2217 is yours."
	)
	startOne := start(briefOne)
	file, _ := teamstore.Load(backend.profileDir)
	team, _ := file.Team(id)
	first, ok := team.ByHandle("web")
	if !ok || first.StartEntry != startOne || !first.Started {
		t.Fatalf("crewStart recorded %+v, want start entry %s", first, startOne)
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error { return f.SetHandle(id, first.Key, "api") }); err != nil {
		t.Fatal(err)
	}
	startTwo := start(briefTwo)
	file, _ = teamstore.Load(backend.profileDir)
	team, _ = file.Team(id)
	second, ok := team.ByHandle("web")
	if !ok || second.Key == first.Key || second.StartEntry != startTwo {
		t.Fatalf("second member %+v (first %+v), want start entry %s", second, first, startTwo)
	}
	check := func(b *appBackend, label string) {
		t.Helper()
		if got, err := b.MemberBrief(id, appMemberAgent(first)); err != nil || got.Text != briefOne {
			t.Fatalf("%s: renamed member = %+v, %v", label, got, err)
		}
		if got, err := b.MemberBrief(id, appMemberAgent(second)); err != nil || got.Text != briefTwo {
			t.Fatalf("%s: reusing member = %+v, %v", label, got, err)
		}
	}
	check(backend, "live")
	fresh := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { fresh.Close() })
	fresh.profileDir = backend.profileDir
	check(fresh, "after restart")
}

// A CREW SAYS SINCE WHEN NO MEMBER HAS RUN, on the fake clock: idle or waiting
// on the person, it is the latest instant a member stopped, a member at rest at
// the end of its last turn and a member whose open turn a question holds when
// that question was put. It is absent while a member runs or may run, and
// while any member's instant is unknown.
func TestAppCrewSaysSinceWhenNoMemberHasRun(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}, hold: hold}
	backend, rec, agent, id := crewBackendOn(t, conv)
	var mu sync.Mutex
	t0 := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := t0
	backend.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	set := func(at time.Time) { mu.Lock(); clock = at; mu.Unlock() }
	check := func(state string, want time.Time) appwire.Crew {
		t.Helper()
		c, err := backend.CrewGet(id)
		if err != nil {
			t.Fatal(err)
		}
		if c.State != state || (want.IsZero() != (c.IdleSince == nil)) || (c.IdleSince != nil && !c.IdleSince.Equal(want)) {
			t.Fatalf("the crew = %q, idle since %v; want %q, %v", c.State, c.IdleSince, state, want)
		}
		return c
	}

	// The disk-only member's instant is not known: nothing is said.
	check(appwire.CrewIdle, time.Time{})
	rested := t0.Add(-time.Hour)
	backend.locked(func() { backend.agentLocked("b0b0b0b0").view.UpdatedAt = rested })
	c := check(appwire.CrewIdle, rested)
	if raw, _ := json.Marshal(c); !strings.Contains(string(raw), `"idle_since":"2030-01-02T02:04:05Z"`) {
		t.Fatalf("the wire = %s", raw)
	}

	if _, err := backend.Send(agent.ID, "go", ""); err != nil {
		t.Fatal(err)
	}
	check(appwire.CrewWorking, time.Time{})
	// A question that does not hold the turn: the member may still be running.
	free := session.Question{ID: 1, Kind: session.QuestionAsk, Head: "which one?", Asked: t0.Add(time.Minute)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &free})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	check(appwire.CrewNeedsYou, time.Time{})
	// Questions that hold the turn: it stopped when the first was put.
	for n, at := range []time.Duration{3 * time.Minute, 2 * time.Minute} {
		q := session.Question{ID: uint64(n + 2), Kind: session.QuestionConsent, Head: "bash", Blocking: session.Blocking{Turn: true}, Asked: t0.Add(at)}
		conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		o, ok := data.(appwire.AskOpened)
		return ok && o.Ask.ID == "consent:3"
	})
	check(appwire.CrewNeedsYou, t0.Add(2*time.Minute))

	// The turn ends with its questions open: the member stopped at its end.
	ended := t0.Add(10 * time.Minute)
	set(ended)
	close(hold)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		a, ok := data.(appwire.AgentChanged)
		return ok && a.Agent.ID == agent.ID && a.Agent.UpdatedAt.Equal(ended)
	})
	set(ended.Add(time.Hour))
	check(appwire.CrewNeedsYou, ended)
	// The other member stopped later: the crew is idle since the last stop.
	later := ended.Add(5 * time.Minute)
	backend.locked(func() { backend.agents["b0b0b0b0"].view.UpdatedAt = later })
	check(appwire.CrewNeedsYou, later)
}

func TestCrewHistoryTasksCarryTheirOwner(t *testing.T) {
	kept := &appCrewExtra{History: []string{"7", "9"}, HistoryOwner: "agent-a"}
	got := crewHistoryTasks(kept, "agent-b")
	want := []appwire.CrewHistoryTask{{Task: "agent-a:7", Owner: "agent-a"}, {Task: "agent-a:9", Owner: "agent-a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owner kept at creation must win over the founder: %v", got)
	}
	// A layer from before the owner was kept: the founder, the manager it was made under.
	legacy := &appCrewExtra{History: []string{"7"}}
	if got := crewHistoryTasks(legacy, "agent-b"); len(got) != 1 || got[0].Owner != "agent-b" || got[0].Task != "agent-b:7" {
		t.Fatalf("legacy falls back to the founder: %v", got)
	}
	// Neither: nothing is guessed. No ids: nothing to say.
	if got := crewHistoryTasks(legacy, ""); got != nil {
		t.Fatalf("no owner, no records: %v", got)
	}
	if got := crewHistoryTasks(&appCrewExtra{HistoryOwner: "agent-a"}, "agent-b"); got != nil {
		t.Fatalf("no ids, no records: %v", got)
	}
	// The legacy string list is still what the layer file keeps.
	raw, _ := json.Marshal(kept)
	if !strings.Contains(string(raw), `"history":["7","9"]`) {
		t.Fatalf("legacy history lost: %s", raw)
	}
}

func TestCrewLegacyCanonicalHistoryNeedsNoFounder(t *testing.T) {
	extra := &appCrewExtra{History: []string{"original:1", "original:3", "original:2"}}
	want := []appwire.CrewHistoryTask{{Task: "original:1", Owner: "original"}, {Task: "original:3", Owner: "original"}, {Task: "original:2", Owner: "original"}}
	if got := crewHistoryTasks(extra, ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("lost canonical original owner: %#v", got)
	}
	if got := crewHistoryTasks(extra, "replacement"); !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement founder cannot rewrite embedded ownership: %#v", got)
	}
	if got := crewHistoryTasks(&appCrewExtra{History: []string{"3 tasks from before"}}, ""); len(got) != 0 {
		t.Fatalf("descriptive legacy text is not a task reference: %#v", got)
	}
}
