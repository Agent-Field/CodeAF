package cellstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// Contract A1 inspects incoming metadata without restoring the working tree.
func TestIncomingAdmissionRestoresOnlyMetadata(t *testing.T) {
	celltest.RequireEngine(t)
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	source := newCell(t)
	for name, body := range map[string]string{
		".cell/session.json": `{"id":"incoming","workspace_policy":{"version":1,"mode":"private-tasks"}}`,
		".cell/tasks.json":   `{"type":"tasks","version":1,"seq":0}`,
		"work.txt":           "the working tree waits for the lease",
	} {
		if err := os.WriteFile(filepath.Join(source.Root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	engine := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: daemonFor(t, bin)}
	sealed, err := engine.Seal(t.Context(), source, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	receiver := newCell(t)
	if err := engine.StageAdmission(t.Context(), receiver, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".cell/session.json", ".cell/tasks.json"} {
		want, err := os.ReadFile(filepath.Join(source.Root, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(receiver.Root, name))
		if err != nil || string(got) != string(want) {
			t.Fatalf("incoming metadata %s=%q: %v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(receiver.Root, "work.txt")); !os.IsNotExist(err) {
		t.Fatalf("working tree restored during metadata inspection: %v", err)
	}
	if dirty, err := engine.Dirty(t.Context(), source); err != nil || dirty {
		t.Fatalf("metadata inspection changed the sender's binding: dirty=%v err=%v", dirty, err)
	}
}

// Contract A1 keeps a full admission restore independent of the chat's timeline.
func TestIncomingWorkspaceAdmissionLeavesItsOriginalTimelineAlone(t *testing.T) {
	celltest.RequireEngine(t)
	bin, err := furrow.ResolveOwned()
	if err != nil {
		t.Fatal(err)
	}
	source := newCell(t)
	marker := filepath.Join(source.Root, "source-only.txt")
	if err := os.WriteFile(marker, []byte("original folder"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Binary: bin, DataRoot: t.TempDir(), Transport: daemonFor(t, bin)}
	sealed, err := engine.Seal(t.Context(), source, TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	stage := cell.Cell{ID: source.ID, Root: t.TempDir()}
	if err := engine.StageWorkspace(t.Context(), stage, sealed.Turn.ID); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(stage.Root, "source-only.txt")); err != nil || string(body) != "original folder" {
		t.Fatalf("full staged tree missing: %q %v", body, err)
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "original folder" {
		t.Fatalf("admission changed original files: %q %v", body, err)
	}
	if dirty, err := engine.Dirty(t.Context(), source); err != nil || dirty {
		t.Fatalf("admission changed original native binding: dirty=%v err=%v", dirty, err)
	}
}
