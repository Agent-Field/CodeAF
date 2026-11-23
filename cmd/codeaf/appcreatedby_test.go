package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func createdByBackend(t *testing.T, state string) (*appBackend, *appRecorder) {
	t.Helper()
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	backend.mint = appMint
	backend.state = &appState{path: state}
	t.Cleanup(func() { backend.Close() })
	return backend, rec
}

func listedCreatedBy(t *testing.T, b *appBackend, id string) string {
	t.Helper()
	agents, err := b.ListAgents("")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.ID == id {
			return a.CreatedBy
		}
	}
	t.Fatalf("%s is not listed", id)
	return ""
}

// AN AGENT CARRIES WHO STARTED IT: the person by default, the app's own word
// when it names one, on the create's answer, its event and every later list,
// including one a fresh process reads.
func TestAppAgentsCarryWhoCreatedThem(t *testing.T) {
	useLocalHome(t)
	state := filepath.Join(t.TempDir(), appStateName)
	b, rec := createdByBackend(t, state)
	ws := t.TempDir()
	mine, err := b.CreateAgent(appwire.CreateAsk{Workspace: ws, Text: "fix the parser"})
	if err != nil {
		t.Fatal(err)
	}
	afis, err := b.CreateAgent(appwire.CreateAsk{Workspace: ws, Text: "tidy the docs", CreatedBy: "afi:a1"})
	if err != nil {
		t.Fatal(err)
	}
	if mine.CreatedBy != appwire.CreatedByPerson || afis.CreatedBy != "afi:a1" {
		t.Fatalf("answers say %q and %q", mine.CreatedBy, afis.CreatedBy)
	}
	rec.waitFor(t, 10*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return event == appwire.EventAgentChanged && ok && changed.Agent.ID == afis.ID && changed.Agent.CreatedBy == "afi:a1"
	})
	if got := listedCreatedBy(t, b, afis.ID); got != "afi:a1" {
		t.Fatalf("the list says %q", got)
	}
	fresh, _ := createdByBackend(t, state)
	for id, want := range map[string]string{mine.ID: appwire.CreatedByPerson, afis.ID: "afi:a1"} {
		opened, err := fresh.OpenAgent(id)
		if err != nil {
			t.Fatal(err)
		}
		if opened.CreatedBy != want {
			t.Fatalf("a fresh process opens %s as created by %q, want %q", id, opened.CreatedBy, want)
		}
	}
}

// A CREW THE APP MAKES IS THE PERSON'S; a member a manager's start line makes
// is the manager's; a team read from before the field omits it.
func TestAppCrewsCarryWhoCreatedThem(t *testing.T) {
	useLocalHome(t)
	b, _ := createdByBackend(t, filepath.Join(t.TempDir(), appStateName))
	b.profileDir = t.TempDir()
	lead, err := b.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "lead"})
	if err != nil {
		t.Fatal(err)
	}
	crew, err := b.CrewCreate(appwire.CrewCreate{Name: "harbor", Agents: []string{lead.ID}, Manager: lead.ID})
	if err != nil {
		t.Fatal(err)
	}
	if crew.CreatedBy != appwire.CreatedByPerson {
		t.Fatalf("the person's crew says %q", crew.CreatedBy)
	}
	team, _, err := b.crewTeam(crew.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := team.Member(team.Manager)
	if err := b.crewStart(team, manager, teamstore.Entry{Kind: teamstore.KindStart, From: teamstore.FromManager, To: "web", Text: "build it", At: time.Now()}, appMemberAgent(manager)); err != nil {
		t.Fatal(err)
	}
	view, err := b.CrewGet(crew.ID)
	if err != nil {
		t.Fatal(err)
	}
	member := ""
	for _, m := range view.Members {
		if m.Handle == "web" {
			member = m.Agent
		}
	}
	a, err := b.find(member)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	b.locked(func() { got = a.view.CreatedBy })
	if got != lead.ID {
		t.Fatalf("the started member says %q, want its manager %s", got, lead.ID)
	}
	old := teamstore.NewID()
	if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: old, Name: "older", Made: time.Now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if older, err := b.CrewGet(old); err != nil || older.CreatedBy != "" {
		t.Fatalf("a team from before says %q (%v)", older.CreatedBy, err)
	}
}
