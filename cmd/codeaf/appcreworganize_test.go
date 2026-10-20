package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

type appOrganizeFake struct {
	calls int
	in    session.TeamProposalInput
	res   session.TeamProposal
	err   error
}

func (f *appOrganizeFake) ProposeTeams(_ context.Context, in session.TeamProposalInput) (session.TeamProposal, error) {
	f.calls++
	f.in = in
	return f.res, f.err
}

func TestCrewsOrganizeBothPassesLeaveTheTeamsFileAlone(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvVar, dir)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	b := &appBackend{profileDir: dir, now: func() time.Time { return now }, state: &appState{path: filepath.Join(dir, "app.json")}, agents: map[string]*appAgent{}}
	for id, folder := range map[string]string{"a": "/docs", "b": "/docs/", "c": "/one", "d": "/two", "e": "/three", "held": "/one"} {
		b.agents[id] = &appAgent{view: appwire.Agent{ID: id, Title: "title " + id, Workspace: folder, State: appwire.StateIdle}, opening: true, users: 1}
	}
	old := teamstore.Team{ID: "old", Name: "release", Made: now.Add(-9 * 24 * time.Hour), Members: []teamstore.Member{{Key: filepath.Join(dir, "held", "transcript.jsonl")}}}
	body := ovTeams(t, old)
	path := filepath.Join(dir, teamstore.FileName)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	usd := 0.021
	fake := &appOrganizeFake{res: session.TeamProposal{Model: "cheap/namer", CostUSD: &usd, New: []session.ProposedTeam{{Name: "topic", Members: []string{"a", "c", "d"}, Reason: "related"}}, Additions: []session.ProposedAddition{{TeamID: "old", Members: []string{"b", "e"}, Reason: "release"}}}}
	b.organizeProposer = fake
	folders, err := b.CrewsOrganize(context.Background(), appwire.OrganizeAsk{Pass: "folders"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 0 || *folders.AgentsUnassigned != 5 || *folders.CrewsOpen != 1 || len(folders.New) != 1 || folders.New[0].Folder != "/docs" || folders.New[0].Why != "from the folder" || len(*folders.Quiet) != 1 || (*folders.Quiet)[0].IdleDays != 9 || (*folders.Quiet)[0].Agents != 1 {
		t.Fatalf("folders %+v; quiet %+v", folders, folders.Quiet)
	}
	model, err := b.CrewsOrganize(context.Background(), appwire.OrganizeAsk{Pass: "model"})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || len(fake.in.Conversations) != 5 || len(fake.in.Teams) != 1 || len(model.New) != 1 || !reflect.DeepEqual(model.New[0].Agents, []string{"c", "d"}) || len(model.Additions) != 1 || !reflect.DeepEqual(model.Additions[0].Agents, []string{"e"}) || model.CostUSD == nil || *model.CostUSD != usd || model.Model != "cheap/namer" {
		t.Fatalf("model %+v; input %+v", model, fake.in)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != body {
		t.Fatalf("teams changed: %s (%v)", after, err)
	}
	if b.crewRun != nil {
		t.Fatal("organizing started the crew watch")
	}
	for _, failure := range []error{session.ErrNoNamingModel, context.DeadlineExceeded, errors.New("provider failed")} {
		fake.err = failure
		_, err = b.CrewsOrganize(context.Background(), appwire.OrganizeAsk{Pass: "model"})
		var wire *appwire.Error
		if !errors.As(err, &wire) {
			t.Fatal(err)
		}
		want := appwire.CodeEngine
		if failure == session.ErrNoNamingModel {
			want = appwire.CodeNoNamingModel
		}
		if wire.Code != want {
			t.Fatalf("%v => %v", failure, err)
		}
	}
}

// The production naming path must not acquire a journal or run team repair.
func TestCrewsOrganizeProductionNamingLeavesRepairableStoreAlone(t *testing.T) {
	b, workspace := organizeProductionBackend(t)
	profile := b.crewProfile()
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(`{"models.tiers.low":"test/model"}`), 0600); err != nil {
		t.Fatal(err)
	}
	body := ovTeams(t, teamstore.Team{ID: "old", Name: "old", Members: []teamstore.Member{{Key: "/elsewhere/transcript.jsonl", Word: "missing handle"}}})
	path := filepath.Join(profile, teamstore.FileName)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.CrewsOrganize(t.Context(), appwire.OrganizeAsk{Pass: "model"})
	after, readErr := os.ReadFile(path)
	if readErr != nil || string(after) != body {
		t.Fatalf("naming repaired teams.json before Apply: %s (%v)", after, readErr)
	}
	if err != nil || out.Model == "" {
		t.Fatalf("production naming: %+v (%v), workspace %s", out, err, workspace)
	}
	if b.crewRun != nil {
		t.Fatal("naming started a crew watch")
	}
	for _, a := range b.agents {
		if a.live != nil {
			t.Fatal("naming opened a conversation")
		}
	}
}

func TestCrewsOrganizeDiscoversInitiallyUnheldHostBeforeLocalNaming(t *testing.T) {
	b, workspace := organizeProductionBackend(t)
	agents, err := b.ListAgents("")
	if err != nil || len(agents) != 2 {
		t.Fatalf("agents %v (%v)", agents, err)
	}
	id := agents[0].ID
	file := b.agents[id].transcript
	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(workspace, enginehost.Options{Boot: func(hello remote.Hello) (*remote.Engine, error) {
			proc, err := openAppEngineProcess()
			if err != nil {
				return nil, err
			}
			launch, err := openV3Launch(proc, v3Options{Workspace: workspace, Model: "test/model", Session: file})
			if err != nil {
				return nil, err
			}
			launch.Config.OneModel = true
			agent, err := session.New(launch.Config)
			if err != nil {
				return nil, err
			}
			proc.track(agent)
			return &remote.Engine{Agent: agent, Workspace: workspace, SessionFile: file}, nil
		}})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(workspace)
		select {
		case err := <-stopped:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("host did not stop")
		}
	})
	waitEnvironmentHost(t, workspace, func(self remote.HostSelf) bool { return self.PID == os.Getpid() })
	client := environmentWindow(t, workspace, file)
	defer client.Close()
	if b.held(id) {
		t.Fatal("fixture already held")
	}
	out, err := b.CrewsOrganize(t.Context(), appwire.OrganizeAsk{Pass: "model"})
	if err != nil || out.Model != "test/model" {
		t.Fatalf("unheld hosted naming: %+v (%v)", out, err)
	}
}

func organizeProductionBackend(t *testing.T) (*appBackend, string) {
	t.Helper()
	_, workspace := wakeEnvironment(t, wakeRoot(t))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"{\"new\":[],\"add\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0.001}}`)
		fmt.Fprint(w, "\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_BASE_URL", server.URL)
	profile := os.Getenv(home.EnvVar)
	t.Setenv(config.ProfileDirEnv, profile)
	if err := os.MkdirAll(profile, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(`{"models.tiers.low":""}`), 0600); err != nil {
		t.Fatal(err)
	}
	rec := newAppRecorder()
	b := newAppBackend(rec, appOpenInProcess)
	for _, title := range []string{"topic a", "topic b"} {
		a, err := b.CreateAgent(appwire.CreateAsk{Workspace: workspace, Text: title})
		if err != nil {
			t.Fatal(err)
		}
		rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
			done, ok := data.(appwire.TurnDone)
			return ok && done.Agent == a.ID
		})
	}
	b.Close()
	b = newAppBackend(newAppRecorder(), appOpenInProcess)
	t.Cleanup(func() { b.Close() })
	return b, workspace
}

func TestCrewsOrganizeProductionRequiresNamingOnlyAtDesktopEntry(t *testing.T) {
	b, _ := organizeProductionBackend(t)
	_, err := b.CrewsOrganize(t.Context(), appwire.OrganizeAsk{Pass: "model"})
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeNoNamingModel {
		t.Fatalf("missing naming role: %v", err)
	}
	for _, a := range b.agents {
		if a.live != nil {
			t.Fatal("missing naming role opened a conversation")
		}
	}
}
