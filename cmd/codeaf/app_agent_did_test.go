package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentdid"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// writeSpokenSession writes a conversation the person spoke in, as it stands
// on disk, with identity as its agent identity (nil for none).
func writeSpokenSession(t *testing.T, workspace, id string, identity *session.AgentIdentity) {
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
		`{"type":"session","version":1,"id":"` + id + `","timestamp":"2026-10-29T09:00:00Z"}`,
		`{"type":"message","role":"user","content":"hello","timestamp":"2026-10-29T09:00:01Z"}`,
	}
	if err := os.WriteFile(session.Place{Dir: dir}.Transcript(), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := session.Meta{ID: id, Title: "hello", Workspace: workspace, AgentIdentity: identity, LastUserAt: time.Date(2026, 10, 29, 9, 0, 1, 0, time.UTC), Created: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)}
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
}

// THE AGENT LIST CARRIES EACH AGENT'S DID, so the app can name the agent a
// canvas actor came from; an agent with none says nothing rather than "".
func TestAppAgentListCarriesTheAgentDID(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	did, err := agentdid.New()
	if err != nil {
		t.Fatal(err)
	}
	const withID, withoutID = "a1b2c3d4e5f60718", "0f1e2d3c4b5a6978"
	writeSpokenSession(t, workspace, withID, &session.AgentIdentity{DID: did, Session: withID})
	writeSpokenSession(t, workspace, withoutID, nil)

	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	backend.profileDir = filepath.Join(root, "profile")
	backend.state = &appState{path: filepath.Join(root, appStateName)}
	t.Cleanup(func() { backend.Close() })

	listed, err := backend.ListAgentsIn("", "all", "")
	if err != nil {
		t.Fatal(err)
	}
	with, ok := listsAgent(listed, withID)
	if !ok {
		t.Fatalf("the agent was left out of the list: %+v", listed)
	}
	raw, err := json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	if with.DID != did || !strings.Contains(string(raw), `"did":"`+did+`"`) {
		t.Fatalf("the listed agent does not carry its DID: %s", raw)
	}
	without, ok := listsAgent(listed, withoutID)
	if !ok {
		t.Fatalf("the agent with no DID was left out of the list: %+v", listed)
	}
	raw, err = json.Marshal(without)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"did"`) {
		t.Fatalf("an agent with no DID says one: %s", raw)
	}
}
