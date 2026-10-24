//go:build !windows

package cellstore

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/cell"
	"os"
	"path/filepath"
	"testing"
)

// TestE3ChatsShareOneLongLivedDaemon proves E3/U3/P2: chats in one folder keep
// private timeline heads while one long-lived process retains one shared store.
// The engine's stores counter counts physical data directories, not timelines.
func TestE3ChatsShareOneLongLivedDaemon(t *testing.T) {
	e := realEngine(t)
	d := daemonFor(t, e.Binary).(Daemon)
	e.Transport = d
	e.Workspace = t.TempDir()
	cells := []cell.Cell{newCell(t), newCell(t)}
	heads := map[string]string{}
	var firstPID int
	for _, c := range cells {
		for i := range 2 {
			if err := os.WriteFile(filepath.Join(e.Workspace, "work"), []byte(fmt.Sprintf("%s:%d", c.ID, i)), 0600); err != nil {
				t.Fatal(err)
			}
			sealed, err := e.Seal(t.Context(), c, TurnInfo{})
			if err != nil {
				t.Fatal(err)
			}
			heads[c.ID] = sealed.Turn.ID
			conn, err := dial(t.Context(), d.Socket)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "health"})
			_ = conn.Close()
			if err != nil {
				t.Fatal(err)
			}
			var health struct{ PID, Stores int }
			if err = json.Unmarshal(answer.Ok, &health); err != nil {
				t.Fatal(err)
			}
			if firstPID == 0 {
				firstPID = health.PID
			}
			if health.PID == 0 || health.PID != firstPID {
				t.Fatalf("daemon restarted: %+v first=%d", health, firstPID)
			}
		}
	}
	conn, err := dial(context.Background(), d.Socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	answer, err := exchange(conn, wireRequest{V: wireVersion, ID: nextID.Add(1), Verb: "health"})
	if err != nil {
		t.Fatal(err)
	}
	var health struct{ Stores int }
	if err = json.Unmarshal(answer.Ok, &health); err != nil {
		t.Fatal(err)
	}
	if health.Stores != 1 {
		t.Fatalf("retained physical stores=%d, want one shared data directory", health.Stores)
	}
	for _, c := range cells {
		head, err := Head(c)
		if err != nil || head == nil || head.Turn.ID != heads[c.ID] {
			t.Fatalf("another chat replaced this timeline's head: %+v %v", head, err)
		}
		if e.target(c).DataDir != e.target(cells[0]).DataDir {
			t.Fatal("chats did not share the physical store")
		}
	}
	if heads[cells[0].ID] == heads[cells[1].ID] {
		t.Fatal("distinct captured bytes did not keep distinct heads")
	}
}
