package tui3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// localPrivacyAgent scripts tool calls through the real seat while the surface
// uses its ordinary agent interface. No provider or external service is involved.
type localPrivacyAgent struct {
	*fakeAgent
	seat executor.Seat
	work string
}

func (a *localPrivacyAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	for _, name := range []string{"one.txt", "two.txt"} {
		call := executor.Call{Tool: "write", Args: []byte(`{"path":"` + name + `"}`)}
		if err := a.seat.Around(ctx, call, func() ([]byte, bool) {
			return nil, os.WriteFile(filepath.Join(a.work, name), []byte(text), 0o600) != nil
		}); err != nil {
			return nil, err
		}
	}
	out := make(chan session.Event)
	close(out)
	return out, nil
}

// TestC8ReadingAddDeviceInstructionsAndChattingSendsNothing proves C8 through
// home's alt+d key, local tool calls, directory reads and the real quit door.
func TestC8ReadingAddDeviceInstructionsAndChattingSendsNothing(t *testing.T) {
	celltest.RequireEngine(t)
	if _, err := furrow.ResolveOwned(); err != nil {
		t.Skipf("no engine: %v", err)
	}
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected relay request", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)

	t.Setenv("CODEAF_ENGINE_DAEMON", "0")
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	s, ok, err := syncsetup.OpenFirst(root)
	if err != nil || !ok {
		t.Fatalf("first launch: %v, %v", ok, err)
	}
	c, err := cell.Create(root, cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	seat, err := cellstore.SeatFor(executor.HostBound, c, work, nil, func(err error) {
		if err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	agent := &localPrivacyAgent{fakeAgent: &fakeAgent{model: "scripted"}, seat: seat, work: work}
	a := newTestApp(agent)
	a.approvals, a.fleet = &fakeApprovals{}, s
	a.width, a.height = 80, 30
	if cmd := a.askFleet(); cmd == nil {
		t.Fatal("home did not ask about devices")
	} else {
		drive(t, a, cmd())
	}
	a.openHome()
	drive(t, a, key("alt+d"))
	if !a.addMachine.open {
		t.Fatal("alt+d did not open instructions")
	}
	drive(t, a, key("alt+d"))
	if a.addMachine.open || !syncsetup.Quiet(root) {
		t.Fatal("closing the instructions changed the home's pairing state")
	}
	if d, err := s.Drive(context.Background(), cellstore.EngineFor(work), c, syncsetup.DriveOptions{}); d != nil || !errors.Is(err, syncsetup.ErrQuiet) {
		t.Fatalf("a never-paired chat started syncing: drive %v, %v", d, err)
	}
	if _, err := agent.Submit(context.Background(), "write two local files"); err != nil {
		t.Fatal(err)
	}
	if turns, err := cellstore.Turns(c); err != nil || len(turns) != 2 {
		t.Fatalf("tool calls sealed %d turns, %v", len(turns), err)
	}
	_, _ = s.Roster(context.Background())
	_, _ = s.Rows(context.Background())
	a.quit()
	if requests.Load() != 0 {
		t.Fatalf("a never-paired home sent %d relay requests", requests.Load())
	}
}
