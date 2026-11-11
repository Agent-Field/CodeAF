package session_test

// A NOTE SENT ACROSS THE SOCKET LANDS ON THE REAL NODE WITH ITS KEY, AND THE
// OUTCOME THAT COMES BACK IS THE NODE'S OWN.
//
// The far end is a real engine (a session.Agent with a running node and a
// checkpoint file) served by the real server through remote.Loopback, and the
// near end is the remote.Agent every app conversation holds. The page is read
// back over the socket and again from a node restored off the disk.

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestANoteAcrossTheSocketKeepsItsKeyAndItsTypedOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	far, id := session.RunningNodeForTest(t, path)
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
		return &remote.Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if !loop.Client.Welcome().AppTaskReceipt {
		t.Fatal("a real engine did not claim the typed receipt")
	}
	near := loop.Client.Agent()
	task := strconv.FormatUint(id, 10)
	send := func(key string) session.AppTaskReceipt {
		return near.AppTaskActionReceipt(session.AppTaskIntent{ID: task, Action: "note", Text: "same words", ClientID: key})
	}

	// Nobody in the room: held. A reader in it: delivered. Equal words, other
	// keys, one key twice: each its own note.
	if got := send("r-1"); got.Outcome != session.AppTaskHeld || got.ClientID != "r-1" {
		t.Fatalf("held over the socket = %+v", got)
	}
	session.StandAReaderInForTest(t, far, id)
	for _, key := range []string{"r-2", "r-1", ""} {
		if got := send(key); got.Outcome != session.AppTaskDelivered || got.ClientID != key {
			t.Fatalf("delivered %q over the socket = %+v", key, got)
		}
	}
	// A refusal is the engine's, typed, and nothing is delivered.
	refused := near.AppTaskActionReceipt(session.AppTaskIntent{ID: "999", Action: "note", Text: "same words", ClientID: "r-9"})
	if refused.Outcome != session.AppTaskRefused || !strings.Contains(refused.Reason, "no such task") {
		t.Fatalf("refused over the socket = %+v", refused)
	}

	want := "r-1|r-2|r-1|"
	view, found, err := near.AppTask(task)
	if err != nil || !found {
		t.Fatalf("page over the socket: %v %v", found, err)
	}
	if got := noteKeys(view.Notes); got != want {
		t.Fatalf("page keys over the socket = %q, want %q", got, want)
	}
	// The unkeyed note reaches the disk at the next checkpoint, as it always did;
	// the keyed ones were written in their own admissions.
	reopened := session.ReopenedNotesForTest(t, far, id, path)
	if got := noteKeys(reopened); !strings.HasPrefix(got, "r-1|r-2|r-1") {
		t.Fatalf("reopened page keys = %q, want the keyed ones of %q", got, want)
	}
	for i := range 3 {
		if reopened[i].Direction != view.Notes[i].Direction {
			t.Fatalf("reopened note %d is direction %d, the page said %d", i, reopened[i].Direction, view.Notes[i].Direction)
		}
	}
	for i, note := range view.Notes {
		for j := range i {
			if view.Notes[j].Direction == note.Direction {
				t.Fatalf("two notes share direction %d", note.Direction)
			}
		}
	}
}

func noteKeys(notes []session.AppTaskNote) string {
	keys := make([]string, 0, len(notes))
	for _, note := range notes {
		keys = append(keys, note.ClientID)
	}
	return strings.Join(keys, "|")
}
