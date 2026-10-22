package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// C5 requires a moved chat's snapshot to include composer ! work, including
// a file changed after an initial snapshot that could otherwise be reused.
func TestUserBashChangesReachTheTurnEndSnapshot(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "1")
	t.Setenv("CODEAF_HOME", t.TempDir())
	// The session suite pins the engine away by default. This real-store proof
	// deliberately resolves the configured test engine or the embedded engine.
	t.Setenv(furrow.BinaryEnvVar, os.Getenv("CODEAF_TEST_FURROW"))
	bin, err := furrow.ResolveBinary()
	if err != nil {
		t.Skipf("requires the cell engine: %v", err)
	}
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	var engine cellstore.Engine
	var sealError error
	completer := &scriptedCompleter{}
	agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
		cfg.Place = Place{Dir: c.Root, Workspace: cfg.Workspace}
		cfg.SessionFile = cfg.Place.Transcript()
		must(t, os.WriteFile(filepath.Join(cfg.Workspace, "tracked.txt"), []byte("first"), 0o600))
		seat, err := cellstore.SeatOver(executor.HostBound, c, cfg.Workspace, nil, func(err error) { sealError = err }, func(e cellstore.Engine) cellstore.Store { engine = e; engine.Binary = bin; return engine })
		if err != nil {
			t.Fatal(err)
		}
		cfg.Seat = seat
	})
	defer agent.Close()
	if _, err := engine.Seal(t.Context(), c, cellstore.TurnInfo{Trigger: cellstore.AgentRun}); err != nil {
		t.Fatal(err)
	}
	events, err := agent.SubmitBash(t.Context(), "!printf second > tracked.txt")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if sealError != nil {
		t.Fatal(sealError)
	}
	if completer.requests() != 0 || completer.asideRequests() != 0 {
		t.Fatal("composer command reached the model")
	}
	// Restore the newest engine snapshot after removing the working file; reading
	// it back proves stored content rather than merely observing the shell write.
	head, err := cellstore.Head(c)
	if err != nil || head == nil {
		t.Fatalf("head: %+v %v", head, err)
	}
	if err := os.Remove(filepath.Join(workspace, "tracked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := engine.Restore(t.Context(), c, head.Turn.ID, []string{"tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "tracked.txt"))
	if err != nil || string(raw) != "second" {
		t.Fatalf("newest snapshot held %q: %v", raw, err)
	}
}
