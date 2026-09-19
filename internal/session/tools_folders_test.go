package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/workspace"
)

type fakeFolders struct {
	mu        sync.Mutex
	listed    []FolderRef
	members   map[string]map[string]bool // collection id → conversation ids
	fileErr   map[string]error
	unfileErr map[string]error
	moveErr   error
}

func (f *fakeFolders) List(context.Context) ([]FolderRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FolderRef, len(f.listed))
	copy(out, f.listed)
	return out, nil
}

func (f *fakeFolders) File(_ context.Context, collectionID, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.fileErr[collectionID]; ok {
		return err
	}
	if f.members == nil {
		f.members = map[string]map[string]bool{}
	}
	if f.members[collectionID] == nil {
		f.members[collectionID] = map[string]bool{}
	}
	f.members[collectionID][conversationID] = true
	return nil
}

func (f *fakeFolders) Unfile(_ context.Context, collectionID, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.unfileErr[collectionID]; ok {
		return err
	}
	if bag := f.members[collectionID]; bag != nil {
		delete(bag, conversationID)
	}
	return nil
}

func (f *fakeFolders) Move(_ context.Context, fromID, toID, conversationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.moveErr != nil {
		return f.moveErr
	}
	if err, ok := f.fileErr[toID]; ok {
		return err
	}
	if f.members == nil {
		f.members = map[string]map[string]bool{}
	}
	if bag := f.members[fromID]; bag != nil {
		delete(bag, conversationID)
	}
	if f.members[toID] == nil {
		f.members[toID] = map[string]bool{}
	}
	f.members[toID][conversationID] = true
	return nil
}

func foldersAgent(t *testing.T, folders Folders) *Agent {
	t.Helper()
	place := Place{Dir: filepath.Join(t.TempDir(), "aaaaaaaaaaaaaaaa")}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Folders = folders
		config.Place = place
	})
	return agent
}

func callFolders(t *testing.T, agent *Agent, args string) (string, bool) {
	t.Helper()
	var execute func(context.Context, json.RawMessage) (string, bool, error)
	for _, candidate := range agent.belt() {
		if candidate.Name == "folders" {
			execute = candidate.Execute
			break
		}
	}
	if execute == nil {
		t.Fatal("folders is not on the belt")
	}
	out, failed, err := execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("folders returned a Go error: %v", err)
	}
	return out, failed
}

// CAPABILITY ABSENT, NOT BROKEN. No seam is no verb.
func TestFoldersIsOffTheBeltWhenTheSeamIsNil(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if beltHas(agent, "folders") {
		t.Fatal("folders is on the belt of a session that has no Folders seam")
	}
	if beltHas(agent, "folder") {
		t.Fatal("a folder tool must not alias filesystem /folder")
	}
}

func TestFoldersIsOnTheBeltWhenWired(t *testing.T) {
	agent := foldersAgent(t, &fakeFolders{})
	if !beltHas(agent, "folders") {
		t.Fatal("folders is not on the belt of a session with Folders wired")
	}
	if beltHas(agent, "folder") {
		t.Fatal("the folders tool must not be named folder")
	}
}

func TestFoldersListFileUnfileAndMoveAgainstAFake(t *testing.T) {
	fake := &fakeFolders{
		listed: []FolderRef{
			{ID: "billingbilling00", Name: "Billing"},
			{ID: "securityyyyyyyy0", Name: "Security"},
		},
	}
	agent := foldersAgent(t, fake)
	chat := agent.config.Place.ID()

	listed, failed := callFolders(t, agent, `{"action":"list"}`)
	if failed {
		t.Fatalf("list refused: %s", listed)
	}
	if !strings.Contains(listed, "Billing") || !strings.Contains(listed, "billingbilling00") {
		t.Fatalf("list missing folders:\n%s", listed)
	}

	out, failed := callFolders(t, agent, `{"action":"file","id":"billingbilling00"}`)
	if failed {
		t.Fatalf("file refused: %s", out)
	}
	if fake.members["billingbilling00"][chat] != true {
		t.Fatalf("file did not record membership: %+v", fake.members)
	}

	out, failed = callFolders(t, agent, `{"action":"move","from":"billingbilling00","id":"securityyyyyyyy0"}`)
	if failed {
		t.Fatalf("move refused: %s", out)
	}
	if fake.members["billingbilling00"][chat] {
		t.Fatal("move left the source placement")
	}
	if !fake.members["securityyyyyyyy0"][chat] {
		t.Fatal("move did not file the destination")
	}

	out, failed = callFolders(t, agent, `{"action":"unfile","id":"securityyyyyyyy0"}`)
	if failed {
		t.Fatalf("unfile refused: %s", out)
	}
	if fake.members["securityyyyyyyy0"][chat] {
		t.Fatal("unfile left the placement")
	}
}

func TestFoldersRefusesCyclesAndUnknownIdsInResultText(t *testing.T) {
	fake := &fakeFolders{
		fileErr: map[string]error{
			"cyclecyclecycle0": workspace.ErrCycle,
			"deadbeefdeadbeef": workspace.ErrNotFound,
		},
		moveErr: workspace.ErrCycle,
	}
	agent := foldersAgent(t, fake)

	out, failed := callFolders(t, agent, `{"action":"file","id":"deadbeefdeadbeef"}`)
	if !failed {
		t.Fatal("unknown id succeeded")
	}
	if !strings.Contains(strings.ToLower(out), "unknown") {
		t.Fatalf("unknown id did not say so: %q", out)
	}

	out, failed = callFolders(t, agent, `{"action":"file","id":"cyclecyclecycle0"}`)
	if !failed {
		t.Fatal("cycle succeeded")
	}
	if !strings.Contains(strings.ToLower(out), "cycle") {
		t.Fatalf("cycle did not say so: %q", out)
	}

	out, failed = callFolders(t, agent, `{"action":"move","from":"aaaa","id":"bbbb"}`)
	if !failed {
		t.Fatal("moving into a cycle succeeded")
	}
	if !strings.Contains(strings.ToLower(out), "cycle") {
		t.Fatalf("move cycle did not say so: %q", out)
	}
}

func TestFoldersFileWithoutAConversationIdRefusesInResultText(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Folders = &fakeFolders{}
	})
	out, failed := callFolders(t, agent, `{"action":"file","id":"billingbilling00"}`)
	if !failed {
		t.Fatal("filing a session with no id succeeded")
	}
	if !strings.Contains(strings.ToLower(out), "no id") {
		t.Fatalf("empty place did not say so: %q", out)
	}
}

func TestFoldersIsNotNamedFolderOnTheBashBeltEither(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.bashBelt = true
		config.Folders = &fakeFolders{listed: []FolderRef{{ID: "a", Name: "A"}}}
		config.Place = Place{Dir: filepath.Join(t.TempDir(), "bbbbbbbbbbbbbbbb")}
	})
	if !beltHas(agent, "folders") {
		t.Fatal("folders is missing from a bash belt that was handed the seam")
	}
	if beltHas(agent, "folder") {
		t.Fatal("the bash belt must not alias /folder")
	}
}

func TestFoldersUnknownErrorDoesNotPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("folders panicked: %v", recovered)
		}
	}()
	fake := &fakeFolders{fileErr: map[string]error{"x": errors.New("unknown id")}}
	agent := foldersAgent(t, fake)
	out, failed := callFolders(t, agent, `{"action":"file","id":"x"}`)
	if !failed || !strings.Contains(strings.ToLower(out), "unknown") {
		t.Fatalf("got %q failed=%v", out, failed)
	}
}
