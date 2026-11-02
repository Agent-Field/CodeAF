package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// M1/M2/M5/M9/M10 use real saved histories and a loopback relay through the
// desktop's existing methods, including a joining backend restart and events.
func TestM1M2M5M9M10AppPairRealHomes(t *testing.T) {
	celltest.RequireEngine(t)
	t.Cleanup(cellstore.StopSpawned)
	var down atomic.Bool
	relay := e2Relay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if down.Load() && strings.Contains(r.URL.Path, "/store/") && r.Method != http.MethodGet {
				http.Error(w, "test upload interrupted", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	seed := func(home, name string) (cell.Cell, []byte) {
		t.Setenv("CODEAF_HOME", home)
		if _, err := identity.EnsureSolo(home); err != nil {
			t.Fatal(err)
		}
		c, err := cell.CreateIn(filepath.Join(home, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
		if err != nil {
			t.Fatal(err)
		}
		work := filepath.Join(c.Root, "work")
		if err := os.MkdirAll(work, 0700); err != nil {
			t.Fatal(err)
		}
		if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: name, Workspace: work, Owned: true, Created: time.Now(), LastUserAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(c.Root, cell.TranscriptPath)
		e := cellstore.Engine{Workspace: work, DataRoot: filepath.Join(home, "v3", "stores")}
		var history string
		for i := 1; i <= 3; i++ {
			history += fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":%q}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":%q}\n", fmt.Sprintf("%s question %d", name, i), fmt.Sprintf("%s answer %d", name, i))
			if err := os.WriteFile(file, []byte(history), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
				t.Fatal(err)
			}
			if err := cellstore.Await(t.Context(), c); err != nil {
				t.Fatal(err)
			}
		}
		return c, []byte(history)
	}
	chatA, wantA := seed(dirA, "A chat")
	chatB, wantB := seed(dirB, "B chat")
	_, recA, wa := appDeviceBackend(t, dirA)
	oldB, recB, wb := appDeviceBackend(t, dirB)
	defer assertAppPairCopyM9(t, recA)
	defer assertAppPairCopyM9(t, recB)
	for _, w := range []*actionWire{wa, wb} {
		if rows := machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{}).Chats; len(rows) != 0 {
			t.Fatalf("M1 solo relay inventory: %+v", rows)
		}
	}
	offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code"}).Pairing
	code := e2PairWait(t, wa, offer.ID, "waiting").Code
	join := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: code}).Pairing
	question := e2PairWait(t, wa, offer.ID, "question").Question
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer.ID, Question: question.ID, Option: "y"})
	e2PairWait(t, wa, offer.ID, "done")
	if done := e2PairWait(t, wb, join.ID, "done"); !done.Restart {
		t.Fatal("M10 missing restart")
	}
	if err := oldB.Close(); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	_, restarted, nextWire := appDeviceBackend(t, dirB)
	wb = nextWire
	restarted.waitFor(t, 10*time.Second, func(event string, data any) bool {
		return event == appwire.EventSyncChanged && strings.Contains(data.(appwire.SyncChanged).Sync.Reason, "could not be shared")
	})
	down.Store(false)
	// No list request retries B's upload: its background loop must recover.
	recA.waitFor(t, 15*time.Second, func(event string, data any) bool {
		return event == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Cell == chatB.ID
	})
	rowsFor := func(w *actionWire) map[string]appwire.ChatRow {
		rows := map[string]appwire.ChatRow{}
		for _, row := range machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{}).Chats {
			rows[row.Cell] = row
		}
		return rows
	}
	var rowsA, rowsB map[string]appwire.ChatRow
	appSyncWait(t, "M2 both inventories", func() bool {
		rowsA, rowsB = rowsFor(wa), rowsFor(wb)
		return len(rowsA) == 2 && len(rowsB) == 2
	})
	if !rowsA[chatA.ID].Here || !rowsB[chatB.ID].Here || !rowsA[chatB.ID].CanTake || !rowsB[chatA.ID].CanTake {
		t.Fatalf("M2 A=%+v B=%+v", rowsA, rowsB)
	}
	// A's existing subscriber receives B's background arrival without a new
	// protocol method. The desktop uses this same chat.changed event.
	recA.waitFor(t, 10*time.Second, func(event string, data any) bool {
		return event == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Cell == chatB.ID
	})
	for _, own := range []struct {
		chat cell.Cell
		want []byte
	}{{chatA, wantA}, {chatB, wantB}} {
		if raw, err := os.ReadFile(filepath.Join(own.chat.Root, cell.TranscriptPath)); err != nil || !bytes.Equal(raw, own.want) {
			t.Fatalf("M1 local history changed: %v", err)
		}
	}
}
