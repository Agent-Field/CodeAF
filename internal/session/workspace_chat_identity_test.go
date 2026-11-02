package session

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestOwnershipChatIdentitySurvivesMetadataRefresh(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	place := Place{Dir: filepath.Join(home.Dir(), "chats", "0123456789abcdef"), Workspace: project}
	stale := Meta{ID: place.ID(), Workspace: project, Title: "Mock handoff"}
	if err := SaveMeta(place.Dir, stale); err != nil {
		t.Fatal(err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = project
		config.Place = place
		config.SessionFile = place.Transcript()
	})
	current, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["chat_uri"] != agent.config.WorkspaceURI {
		t.Fatalf("chat without tasks did not persist its identity: %s", raw)
	}
	if err := SaveMeta(place.Dir, stale); err != nil {
		t.Fatal(err)
	}
	if !agent.SetConversationEffort("high") {
		t.Fatal("conversation effort change refused")
	}
	agent.SettleWrites()
	current, err = LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["chat_uri"] != agent.config.WorkspaceURI {
		t.Fatal("stale save or session metadata refresh erased chat identity")
	}
}

func TestOwnershipUnknownChatIdentityCannotMintReplacement(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	place := Place{Dir: filepath.Join(t.TempDir(), "0123456789abcdef"), Workspace: project}
	meta := Meta{ID: place.ID(), Workspace: project, ChatURI: "codeaf-workspace://foreign/unknown"}
	if err := SaveMeta(place.Dir, meta); err != nil {
		t.Fatal(err)
	}
	config := Config{Workspace: project, Place: place, Model: "test/model", System: "SYSTEM"}
	if agent, err := newAgent(config, &scriptedCompleter{}); err == nil {
		agent.Close()
		t.Fatal("unknown carried chat identity silently minted a replacement")
	}
	current, err := LoadMeta(place.Dir)
	if err != nil || current.ChatURI != meta.ChatURI {
		t.Fatal("refused chat identity altered carried metadata")
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Lookup(workspaceidentity.Chat, place.Dir); err == nil {
		t.Fatal("unknown chat identity created a different path binding")
	}
}

func TestOwnershipConflictingMetadataIdentityIsRefused(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	project := t.TempDir()
	place := Place{Dir: filepath.Join(t.TempDir(), "0123456789abcdef"), Workspace: project}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = project; config.Place = place })
	meta, err := LoadMeta(place.Dir)
	if err != nil {
		t.Fatal(err)
	}
	meta.ChatURI = "codeaf-workspace://foreign/replacement"
	if err := SaveMeta(place.Dir, meta); err == nil {
		t.Fatal("metadata save replaced established conversation identity")
	}
	current, err := LoadMeta(place.Dir)
	if err != nil || current.ChatURI != agent.config.WorkspaceURI {
		t.Fatal("refused metadata save changed chat identity")
	}
}

func TestOwnershipSealedTasklessChatCarriesIdentity(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv(furrow.BinaryEnvVar, os.Getenv("CODEAF_TEST_FURROW"))
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Skipf("requires native cell engine: %v", err)
	}
	// Stop native writers before TempDir cleanup removes their state folders.
	defer cellstore.StopSpawned()
	source, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "input.txt"), []byte("mock taskless project\n"), 0600); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: source.Root, Workspace: project}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = project
		config.Place = place
		config.SessionFile = place.Transcript()
	})
	expected := agent.config.WorkspaceURI
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	agent.SettleWrites()
	sourceMeta, err := LoadMeta(source.Root)
	if err != nil || sourceMeta.ChatURI != expected {
		t.Fatal("source chat identity missing before seal")
	}
	engine := cellstore.Engine{Binary: bin, DataRoot: t.TempDir(), Workspace: project, Transport: cellstore.Spawn{Binary: bin}}
	sealed, err := engine.Seal(context.Background(), source, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := filepath.Join(t.TempDir(), source.ID)
	if err := os.MkdirAll(filepath.Join(targetRoot, cell.StateDir), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(source.Root, cell.MetaPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetRoot, cell.MetaPath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	target, err := cell.OpenAt(targetRoot, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	targetProject := t.TempDir()
	receiver := engine
	receiver.Workspace = targetProject
	if _, err := receiver.Seal(context.Background(), target, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err := receiver.Restore(context.Background(), target, sealed.Turn.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(targetRoot, placeMeta)); !os.IsNotExist(err) {
		t.Fatal("derived metadata travelled with sealed tree")
	}
	carried, err := LoadMetaIn(targetRoot, targetProject)
	if err != nil || carried.ChatURI != expected {
		t.Fatalf("taskless sealed chat lost identity: %+v %v", carried, err)
	}
	t.Setenv(home.EnvVar, t.TempDir())
	if err := AdoptChatWorkspaceIdentity(Place{Dir: targetRoot, Workspace: targetProject}); err != nil {
		t.Fatal(err)
	}
	opened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = targetProject
		config.Place = Place{Dir: targetRoot, Workspace: targetProject}
		config.SessionFile = config.Place.Transcript()
	})
	defer func() {
		_ = opened.Close()
		opened.SettleWrites()
	}()
	if opened.config.WorkspaceURI != expected {
		t.Fatal("receiver chat minted a new identity after accepted adoption")
	}
	if _, err := os.Stat(opened.config.Place.Tasks()); !os.IsNotExist(err) {
		t.Fatal("taskless fixture unexpectedly carries task copies")
	}
}
