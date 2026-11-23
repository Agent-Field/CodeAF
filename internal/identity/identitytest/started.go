// Package identitytest builds a closed, unused chat through its storage owners.
package identitytest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellbudget"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// OpenAndQuit exercises session creation without sending a request to a model.
// Its extra surface files were observed after skipping setup and typing /quit.
func OpenAndQuit(t *testing.T, home string, owned bool) session.Place {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", home)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("CODEAF_NO_UPDATE_CHECK", "1")
	t.Setenv("CODEAF_NO_AUTOMATIONS", "1")
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	c, err := cell.CreateIn(filepath.Join(home, "v3/projects/project"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: c.Root, Workspace: t.TempDir(), Owned: owned}
	if owned {
		place.Workspace = filepath.Join(c.Root, "work")
		if err := os.MkdirAll(place.Workspace, 0o700); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=codeaf", "-c", "user.email=test@localhost", "commit", "--allow-empty", "--no-verify", "-qm", "session opened"}} {
			cmd := exec.Command("git", args...) //codeaf:plumbing An owned chat starts with an empty repository.
			cmd.Dir = place.Workspace
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("prepare empty workspace: %s: %v", out, err)
			}
		}
	}
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: place.Workspace, Owned: owned}); err != nil {
		t.Fatal(err)
	}
	a, err := session.New(session.Config{Workspace: place.Workspace, Model: "stub/scripted", BaseURL: "http://127.0.0.1:9/api/v1", APIKey: "stub", Place: place, SessionFile: place.Transcript()})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := inventory.Record(c.Root, func(inv *inventory.Inventory) { inv.Workspace = place.Workspace }); err != nil {
		t.Fatal(err)
	}
	if err := (cellstore.Guard{Home: home}).Screen(c, place.Workspace, filepath.Join(c.Root, cell.StateDir), nil); err != nil {
		t.Fatal(err)
	}
	manager := cellbudget.New(cellstore.Engine{DataRoot: filepath.Join(home, "v3/stores")}, func(cell.Cell) string { return place.Workspace }, nil)
	if err := manager.Open(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"v3/draft-started.json": `{"version":1,"owner":"chat"}`,
		"v3/history.jsonl":      "{\"text\":\"/quit\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return place
}
