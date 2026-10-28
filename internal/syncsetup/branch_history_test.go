package syncsetup

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

// API-level fixture, with fictional checkpoint metadata. Terminal acceptance
// creates its source frontier through actual /task instead of this fixture.
func TestIndependentBranchGetsNewOwnerAndKeepsOnlyTaskHistory(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := workspaceidentity.Open(home.Join("workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := registry.Register(workspaceidentity.Chat, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root, Owned: true}
	if err := session.SaveMeta(c.Root, session.Meta{ID: "parent", ChatURI: parent.URI, Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(place.Transcript(), []byte("{\"type\":\"session\",\"id\":\"parent\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"historical answer\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rootURI := "codeaf-workspace://11111111111111111111111111111111/22222222222222222222222222222222"
	runPrefix := "codeaf-workspace://11111111111111111111111111111111/"
	checkpoint := []byte(fmt.Sprintf(`{"type":"tasks","version":1,"seq":2,"runs":[{"id":1,"title":"report only","state":"done","report":"kept answer","copy":{"uri":%q,"chat_uri":%q,"project_uri":%q,"ground_uri":%q,"dir":"session:trees/1","ground":"workspace:.","mode":"mirror"}},{"id":2,"title":"still active","state":"running","pendingRun":{},"copy":{"uri":%q,"chat_uri":%q,"project_uri":%q,"ground_uri":%q,"dir":"session:trees/2","ground":"workspace:.","mode":"mirror"}}]}`, runPrefix+"33333333333333333333333333333333", parent.URI, rootURI, rootURI, runPrefix+"44444444444444444444444444444444", parent.URI, rootURI, rootURI))
	if err := os.WriteFile(place.Tasks(), checkpoint, 0600); err != nil {
		t.Fatal(err)
	}
	carrier := filepath.Join(c.Root, cell.CarriedTreesPath, "2")
	if err := os.MkdirAll(carrier, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(carrier, "tree.json"), []byte(`{"head":"preserved-active-archive"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "plandb.db"), []byte("old frontier"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := branchIdentity(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	meta, err := session.LoadMeta(c.Root)
	if err != nil || meta.ChatURI == parent.URI || meta.ChatURI == "" {
		t.Fatalf("fork reused parent identity: %+v %v", meta, err)
	}
	if original, err := registry.Resolve(parent.URI); err != nil || original != parent {
		t.Fatal("fork rebound parent's identity")
	}
	current, err := registry.Resolve(meta.ChatURI)
	if err != nil || current.Path != c.Root {
		t.Fatalf("new owner missing: %+v %v", current, err)
	}
	raw, err := os.ReadFile(place.Tasks())
	if err != nil {
		t.Fatal(err)
	}
	var tasks struct {
		Nodes []json.RawMessage
		Runs  []struct{ Copy, PendingRun json.RawMessage }
	}
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks.Nodes) != 0 || len(tasks.Runs) != 2 {
		t.Fatal("history lost or executable nodes retained")
	}
	for _, row := range tasks.Runs {
		if len(row.Copy) > 0 || len(row.PendingRun) > 0 {
			t.Fatal("fork retained continuation authority")
		}
	}
	if _, err := os.Stat(filepath.Join(c.Root, "plandb.db")); !os.IsNotExist(err) {
		t.Fatal("fork retained active plan store")
	}
	saved, err := os.ReadFile(filepath.Join(session.ForkHistoryDir(place), "trees", "2", "tree.json"))
	if err != nil || !strings.Contains(string(saved), "preserved-active") {
		t.Fatalf("active archive lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(c.Root, cell.CarriedTreesPath)); !os.IsNotExist(err) {
		t.Fatal("fork will restore active task copies")
	}
	newFrontier := []byte(`{"type":"tasks","version":1,"seq":3,"runs":[{"id":3,"title":"independent new work","state":"running","copy":{"dir":"local new run"}}]}`)
	if err := os.WriteFile(place.Tasks(), newFrontier, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "plandb.db"), []byte("new independent frontier"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := branchIdentity(context.Background(), c); err != nil {
		t.Fatalf("repeat fork opening: %v", err)
	}
	after, err := os.ReadFile(place.Tasks())
	if err != nil || string(after) != string(newFrontier) {
		t.Fatal("reopening fork rewrote its independent work")
	}
	if raw, err := os.ReadFile(filepath.Join(c.Root, "plandb.db")); err != nil || string(raw) != "new independent frontier" {
		t.Fatal("reopening fork removed its own plan")
	}
}
