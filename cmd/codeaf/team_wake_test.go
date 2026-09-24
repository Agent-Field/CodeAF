package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// TEAM TRAFFIC WAKES A CONVERSATION ON THE ENGINE, through the doors the
// product goes through: the engine's own [bootEngine] builds the conversation
// with nothing set that a person does not set, the team is written where the
// ordinary launch keeps it, and the model endpoint only records what it is
// sent. The engine is what a window over --host talks to as well, so what is
// asserted here is the engine side of both roads.

// wakeRoot is a short state root for a test that listens on a socket, whose
// path has to fit in a socket name.
func wakeRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "af-wake-")
	if err != nil {
		return resolvedTempDir(t)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	return root
}

// wakeEnvironment is the ordinary launch's environment under root, a recording
// model, and a workspace; it returns the model and the workspace.
func wakeEnvironment(t *testing.T, root string) (*recordingModel, string) {
	t.Helper()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv(config.ProfileDirEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	model := newRecordingModel(t)
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(workspace)
	freshEngineProcess(t)
	return model, workspace
}

// managedTeamWith writes a team whose manager is a conversation nobody has
// open and whose one member, @web, is the transcript given.
func managedTeamWith(t *testing.T, root, member, workspace string) string {
	t.Helper()
	teamID := teams.NewID()
	manager := filepath.Join(root, "elsewhere", "manager.jsonl")
	err := teams.Update("", func(file *teams.File) error {
		file.Teams = append(file.Teams, teams.Team{ID: teamID, Name: "harbor"})
		for _, m := range []teams.Member{
			{Key: manager, File: manager, Word: "harbor manager", Handle: "boss"},
			{Key: filepath.Clean(member), File: member, Where: workspace, Word: "web frontend", Handle: "web"},
		} {
			if err := file.AddMember(teamID, m); err != nil {
				return err
			}
		}
		return file.SetManager(teamID, manager)
	})
	if err != nil {
		t.Fatalf("make the team: %v", err)
	}
	return teamID
}

// waitForAsk waits until the model has been sent a message holding want.
func (m *recordingModel) waitForAsk(t *testing.T, want string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, request := range m.requests {
			if request.messageContaining(want) != "" {
				m.mu.Unlock()
				return
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the model was never sent %q", want)
}

// A DIRECTIVE WAKES A MEMBER THE ENGINE HOLDS, with no surface and nobody
// typing: its turn opens on the directive, marked as the manager's.
func TestTeamWakeTheEngineWakesAnIdleMemberOnADirective(t *testing.T) {
	root := resolvedTempDir(t)
	model, workspace := wakeEnvironment(t, root)
	engine, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	defer func() { _ = engine.Agent.Close() }()
	member := strings.TrimSpace(engine.SessionFile)
	if member == "" {
		t.Fatal("the engine named no transcript")
	}
	teamID := managedTeamWith(t, root, member, workspace)
	if err := teams.AppendTraffic("", teamID, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."}); err != nil {
		t.Fatal(err)
	}
	model.waitForAsk(t, "◆ directive from manager: Fix the header.", 20*time.Second)
}

// A MEMBER NOBODY HOLDS IS OPENED BY ITS FOLDER'S HOST, and wakes there. The
// member is a real conversation, closed; the host is a real host on a real
// socket, serving the engine's own boot; the door is the one an engine arms
// ([resumeTeamConversation]), dialling that host rather than spawning one.
func TestTeamWakeAMemberNobodyHoldsIsOpenedHeadlessByItsHost(t *testing.T) {
	root := wakeRoot(t)
	model, workspace := wakeEnvironment(t, root)

	first, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace}, "", "")
	if err != nil {
		t.Fatalf("the engine door did not open: %v", err)
	}
	member := strings.TrimSpace(first.SessionFile)
	if err := first.Agent.Close(); err != nil {
		t.Fatalf("close the member: %v", err)
	}
	if member == "" {
		t.Fatal("the engine named no transcript")
	}

	stopped := make(chan error, 1)
	go func() {
		stopped <- enginehost.Run(workspace, enginehost.Options{
			Boot: func(hello remote.Hello) (*remote.Engine, error) { return bootEngine(hello, workspace, "") },
			Key:  func(hello remote.Hello) string { return engineHelloKey(hello, workspace, "") },
		})
	}()
	t.Cleanup(func() {
		_, _ = enginehost.Stop(workspace)
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Log("the host did not stop within ten seconds")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := enginehost.Dial(workspace)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no host answered on the socket: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	teamID := managedTeamWith(t, root, member, workspace)
	if err := teams.AppendTraffic("", teamID, teams.Entry{Kind: teams.KindDirective, From: teams.FromManager, To: "web", Text: "Fix the header."}); err != nil {
		t.Fatal(err)
	}
	dial := func(folder string) (net.Conn, error) { return enginehost.Dial(folder) }
	if err := resumeTeamConversation(member, workspace, dial); err != nil {
		t.Fatalf("the host did not open the member: %v", err)
	}
	model.waitForAsk(t, "◆ directive from manager: Fix the header.", 20*time.Second)

	// AND A WINDOW OPENING IT NOW JOINS THE RUNNING ONE rather than booting a
	// second agent onto its journal.
	conn, err := enginehost.Dial(workspace)
	if err != nil {
		t.Fatal(err)
	}
	client, err := remote.Dial(conn, "", remote.Hello{Workspace: workspace, Session: member, Join: true})
	if err != nil {
		t.Fatalf("a window could not join the member the host opened: %v", err)
	}
	_ = client.Close()
}

// THE DOOR REFUSES WHAT IT CANNOT OPEN, with the reason the Traffic will carry.
func TestTeamWakeTheResumeDoorSaysWhyItCannotOpen(t *testing.T) {
	never := func(string) (net.Conn, error) {
		t.Fatal("the door dialled a host for a member it cannot open")
		return nil, nil
	}
	if err := resumeTeamConversation("", "/tmp", never); err == nil {
		t.Error("a member with no transcript was opened")
	}
	if err := resumeTeamConversation("/tmp/x.jsonl", "", never); err == nil || !strings.Contains(err.Error(), "no folder") {
		t.Errorf("a member with no folder gave %v", err)
	}
}
