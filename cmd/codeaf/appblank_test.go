package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// announced reports whether the recorder heard an agent.changed for id.
func announced(rec *appRecorder, id string) bool {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, data := range rec.data {
		if c, ok := data.(appwire.AgentChanged); ok && c.Agent.ID == id {
			return true
		}
	}
	return false
}

// listsAgent reports whether a listing names id.
func listsAgent(agents []appwire.Agent, id string) (appwire.Agent, bool) {
	for _, a := range agents {
		if a.ID == id {
			return a, true
		}
	}
	return appwire.Agent{}, false
}

// A BLANK AGENT IS NEITHER LISTED NOR KEPT, through the real door: an agent
// made with no words is opened in this process and answered, but agents.list
// in every state and search.all leave it out, no window is told of it, and
// quitting takes its folder away. An agent the person spoke in beside it is
// listed and kept.
func TestAppABlankAgentIsNeitherListedNorKept(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	backend.profileDir = filepath.Join(root, "profile")
	closed := false
	defer func() {
		if !closed {
			backend.Close()
		}
	}()
	blank, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	spoken, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatalf("create = %v", err)
	}
	if _, err := backend.Send(spoken.ID, "say done", ""); err != nil {
		t.Fatalf("send = %v", err)
	}
	rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Agent == spoken.ID
	})
	dirOf := func(id string) string {
		a, err := backend.find(id)
		if err != nil {
			t.Fatalf("find %s = %v", id, err)
		}
		var transcript string
		backend.locked(func() { transcript = a.transcript })
		return filepath.Dir(transcript)
	}
	blankDir, spokenDir := dirOf(blank.ID), dirOf(spoken.ID)
	if _, err := os.Stat(blankDir); err != nil {
		t.Fatalf("the blank agent's folder was never made: %v", err)
	}

	for _, state := range []string{"", appwire.ListClosed, appwire.ListArchived, "all"} {
		listed, err := backend.ListAgentsIn("", state, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := listsAgent(listed, blank.ID); ok {
			t.Fatalf("agents.list %q listed the blank agent: %+v", state, listed)
		}
		if _, ok := listsAgent(listed, spoken.ID); !ok && (state == "" || state == "all") {
			t.Fatalf("agents.list %q left out the agent the person spoke in: %+v", state, listed)
		}
	}
	groups, err := backend.SearchAll("", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		for _, h := range g.Hits {
			if h.Agent == blank.ID {
				t.Fatalf("search.all found the blank agent: %+v", h)
			}
		}
	}
	if announced(rec, blank.ID) {
		t.Fatal("a window was told of an agent nobody said anything in")
	}

	closed = true
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(blankDir); !os.IsNotExist(err) {
		t.Fatalf("the blank agent's folder outlived the quit: %v", err)
	}
	if _, err := os.Stat(spokenDir); err != nil {
		t.Fatalf("the spoken agent's folder went with the quit: %v", err)
	}
}

// THE FIRST WORDS ARE WHAT LIST AN AGENT: one made blank is neither listed
// nor said until the person sends to it, and then it is both.
func TestAppABlankAgentIsListedOnceThePersonSpeaks(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	backend, rec, agent := workBackend(t, conv)
	if listed, _ := backend.ListAgents(""); len(listed) != 0 {
		t.Fatalf("a blank agent was listed: %+v", listed)
	}
	if announced(rec, agent.ID) {
		t.Fatal("a blank agent was announced")
	}
	if _, err := backend.Send(agent.ID, "hello", ""); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appTurnDone)
	listed, _ := backend.ListAgents("")
	if _, ok := listsAgent(listed, agent.ID); !ok {
		t.Fatalf("an agent the person spoke in was left out: %+v", listed)
	}
}

// writeMemberSession writes a conversation a manager started, as it stands on
// disk after a relaunch: its first message is the crew's brief, a note rather
// than the person's words, its row has no name, and the person never spoke in
// it, so its folder records no last word of theirs.
func writeMemberSession(t *testing.T, workspace, id, brief string) string {
	t.Helper()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session","version":1,"id":"` + id + `","timestamp":"2026-10-15T23:29:59Z"}`,
		`{"type":"message","role":"user","content":"` + brief + `","note":true,"timestamp":"2026-10-15T23:30:00Z"}`,
	}
	transcript := session.Place{Dir: dir}.Transcript()
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace, Created: time.Date(2026, 10, 15, 23, 29, 59, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	return transcript
}

// A MEMBER A MANAGER STARTED IS NOT BLANK, AND IS NEVER A ROW WITH NO NAME AND
// A FIRST-CENTURY DATE. Held again after a relaunch, it is listed, named by
// the word its crew calls it (its brief's first words), told to the window
// under that name, and carries no last_active_at, because the person never
// spoke in it; quitting keeps it.
func TestAppAManagerStartedMemberIsListedByItsBriefWithNoLastActive(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	const id, word = "274d8ea85b245a74", "Take the researcher's findings and…"
	transcript := writeMemberSession(t, workspace, id, "Take the researcher's findings and check each claim against two sources.")
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	backend.profileDir = filepath.Join(root, "profile")
	backend.state = &appState{path: filepath.Join(root, appStateName)}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		team := teamstore.NewID()
		f.Teams = append(f.Teams, teamstore.Team{ID: team, Name: "Trudeau Watch", Made: time.Now()})
		return f.AddMember(team, teamstore.Member{Key: transcript, File: transcript, Where: workspace, Word: word, Handle: "verifier"})
	}); err != nil {
		t.Fatal(err)
	}
	opened, err := backend.OpenAgent(id)
	if err != nil {
		t.Fatalf("open = %v", err)
	}
	if opened.Title != word {
		t.Fatalf("the member opened as %q", opened.Title)
	}
	rec.waitFor(t, 2*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == id && c.Agent.Title == word
	})

	listed, err := backend.ListAgentsIn("", "all", "")
	if err != nil {
		t.Fatal(err)
	}
	member, ok := listsAgent(listed, id)
	if !ok {
		t.Fatalf("the member was left out of the list: %+v", listed)
	}
	if member.Title != word {
		t.Fatalf("the member is listed as %q", member.Title)
	}
	raw, err := json.Marshal(member)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "last_active_at") || strings.Contains(string(raw), "0001-01-01") {
		t.Fatalf("the wire says a time nobody knows: %s", raw)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(transcript); err != nil {
		t.Fatalf("quitting took a member's conversation: %v", err)
	}
}

// AT QUIT ONLY WHAT IS PROVABLY BLANK GOES. Of three conversations this process
// made with no words, the one a crew names (a manager made before its brief
// went out) stays, and the one the person spoke in elsewhere stays; the third
// is taken away, and with it the budget the app kept for it.
func TestAppQuitTakesAwayOnlyBlankConversationsNobodyHolds(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	ids := []string{"00000000000000a1", "00000000000000a2", "00000000000000a3"}
	next := 0
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	backend.mint = func(workspace string) (string, error) {
		bucket, err := v3ProjectDir(workspace)
		if err != nil {
			return "", err
		}
		dir := filepath.Join(bucket, ids[next])
		next++
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		if err := session.SaveMeta(dir, session.Meta{ID: filepath.Base(dir), Workspace: workspace, Created: time.Now()}); err != nil {
			return "", err
		}
		return session.Place{Dir: dir}.Transcript(), nil
	}
	backend.profileDir = filepath.Join(root, "profile")
	backend.state = &appState{path: filepath.Join(root, appStateName)}
	budget := 2.0
	crewed, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	spokenElsewhere, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	blank, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, BudgetUSD: &budget})
	if err != nil {
		t.Fatal(err)
	}
	transcriptOf := func(id string) string {
		var transcript string
		backend.locked(func() { transcript = backend.agents[id].transcript })
		return transcript
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		team := teamstore.NewID()
		f.Teams = append(f.Teams, teamstore.Team{ID: team, Name: "harbor", Made: time.Now()})
		return f.AddMember(team, teamstore.Member{Key: transcriptOf(crewed.ID), File: transcriptOf(crewed.ID), Where: workspace, Word: "harbor manager", Handle: "boss"})
	}); err != nil {
		t.Fatal(err)
	}
	said := `{"type":"session","version":1,"id":"` + spokenElsewhere.ID + `"}` + "\n" + `{"type":"message","role":"user","content":"hello"}` + "\n"
	if err := os.WriteFile(transcriptOf(spokenElsewhere.ID), []byte(said), 0o600); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Dir(transcriptOf(blank.ID))
	kept := []string{filepath.Dir(transcriptOf(crewed.ID)), filepath.Dir(transcriptOf(spokenElsewhere.ID))}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the blank conversation outlived the quit: %v", err)
	}
	for _, dir := range kept {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("quitting took %s: %v", dir, err)
		}
	}
	_ = backend.state.read(func(f *appStateFile) {
		if _, ok := f.Budgets[blank.ID]; ok {
			t.Fatal("the app kept a budget for a conversation that is gone")
		}
	})
}
