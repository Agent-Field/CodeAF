//go:build !windows

package cellstore

import (
	"context"
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/cell"
	"path/filepath"
	"testing"
)

// TestE3ChatsShareOneLongLivedDaemon proves E3/D1: distinct chats keep their
// own open handles in one process rather than starting a daemon for each seal.
func TestE3ChatsShareOneLongLivedDaemon(t *testing.T) {
	e := realEngine(t)
	d := daemonFor(t, e.Binary).(Daemon)
	e.Transport = d
	var firstPID int
	for _, c := range []cell.Cell{newCell(t), newCell(t)} {
		for range 2 {
			if _, err := e.Seal(t.Context(), c, TurnInfo{}); err != nil {
				t.Fatal(err)
			}
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
	if health.Stores != 2 {
		t.Fatalf("retained handles=%d, want two timelines sharing %s", health.Stores, filepath.Base(e.Root()))
	}
}
