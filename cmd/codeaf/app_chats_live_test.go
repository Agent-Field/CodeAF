package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L2/L6 are observed through chats.list and chat.changed, with real directory
// version changes. A lease means an open chat, independent of a model turn.
func TestAppChatsL2L6(t *testing.T) {
	celltest.Available(t)
	var clockAdvance atomic.Int64
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/v1/dir/list" {
				next.ServeHTTP(w, r)
				return
			}
			reply := httptest.NewRecorder()
			next.ServeHTTP(reply, r)
			var listing directory.Listing
			if err := json.Unmarshal(reply.Body.Bytes(), &listing); err != nil {
				t.Errorf("L2 listing clock: %v", err)
				return
			}
			listing.Now += clockAdvance.Load()
			for key, values := range reply.Header() {
				w.Header()[key] = values
			}
			w.WriteHeader(reply.Code)
			_ = json.NewEncoder(w).Encode(listing)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	a := appDeviceSync(t, dirA)
	feed := a.Follow()
	defer feed.Close()
	if err = syncsetup.PutOwnDevice(t.Context(), a.Dir, a.Identity, a.Device, "Mac"); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, title := range []string{"first", "unchanged"} {
		c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := directory.SealName(directory.MetadataKey(id.CellKey()), title)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.Dir.Create(t.Context(), c.ID, directory.CellInit{Head: "sealed-head", Title: sealed}); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	_, rec, w := appDeviceBackend(t, dirB)
	machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{})
	rec.waitFor(t, 10*time.Second, func(name string, data any) bool {
		if name != appwire.EventChatChanged {
			return false
		}
		r := data.(appwire.ChatChanged).Chat
		return r.Cell == ids[1] && r.Online != nil && *r.Online
	})
	before := machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{})
	for _, r := range before.Chats {
		if r.Status != "running" || r.Line != "open on Mac" || strings.Contains(r.TakeoverLine, "running") {
			t.Errorf("L6 idle open row: %+v", r)
		}
	}
	rec.mu.Lock()
	cursor := len(rec.events)
	rec.mu.Unlock()
	// Cross a whole-second age boundary without a chat change. Advancing the
	// directory version for first must not reissue unchanged's relative age.
	clockAdvance.Store(2000)
	changed, err := directory.SealName(directory.MetadataKey(id.CellKey()), "changed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Dir.Publish(t.Context(), ids[0], directory.Publish{Fence: 1, OldHead: "sealed-head", Head: "next-head", Title: changed}); err != nil {
		t.Fatal(err)
	}
	machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{})
	rec.waitFor(t, 10*time.Second, func(name string, data any) bool {
		return name == appwire.EventChatChanged && data.(appwire.ChatChanged).Chat.Title == "changed"
	})
	rec.mu.Lock()
	defer rec.mu.Unlock()
	changedRows, removed := []string{}, 0
	for i := cursor; i < len(rec.events); i++ {
		switch rec.events[i] {
		case appwire.EventChatChanged:
			changedRows = append(changedRows, rec.data[i].(appwire.ChatChanged).Chat.Cell)
		case appwire.EventChatRemoved:
			removed++
		}
	}
	if len(changedRows) != 1 || changedRows[0] != ids[0] || removed != 0 {
		t.Errorf("L2 changes=%v removed=%d", changedRows, removed)
	}
}

// L1: a completed chat precedes pairing. Only app startup and pairing run;
// neither computer opens the chat or sends a turn to publish it.
func TestAppChatsExistingBeforePairL1(t *testing.T) {
	celltest.RequireEngine(t)
	var creates atomic.Int64
	relay := e2WireRelay(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" && cell.ValidID(strings.TrimPrefix(r.URL.Path, "/v1/dir/cells/")) {
				creates.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	if _, err := identity.Ensure(dirA); err != nil {
		t.Fatal(err)
	}
	a, _, wa := appDeviceBackend(t, dirA)
	if _, err := a.ListChats(); err != nil {
		t.Fatal(err)
	}
	c := appFinishedCell(t, dirA, "Before pairing")
	b, _, wb := appDeviceBackend(t, dirB)
	offer := machineCall[appwire.PairResult](t, wa, "pair.start", appwire.PairStartParams{Mode: "offer_code", Name: "A"}).Pairing
	code := e2PairWait(t, wa, offer.ID, "waiting").Code
	join := machineCall[appwire.PairResult](t, wb, "pair.start", appwire.PairStartParams{Mode: "use_code", Code: code, Name: "B"}).Pairing
	question := e2PairWait(t, wa, offer.ID, "question")
	machineCall[appwire.PairResult](t, wa, "pair.answer", appwire.PairAnswerParams{ID: offer.ID, Question: question.Question.ID, Option: "y"})
	e2PairWait(t, wa, offer.ID, "done")
	e2PairWait(t, wb, join.ID, "done")
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, wb = appDeviceBackend(t, dirB)
	appSyncWait(t, "L1 pre-pair chat on B", func() bool {
		rows := machineCall[appwire.ChatsResult](t, wb, "chats.list", struct{}{}).Chats
		for _, r := range rows {
			if r.Cell == c.ID {
				return !r.Here && r.Holder != nil && r.Holder.Name == "A" && r.CanTake && r.LocalAgent == ""
			}
		}
		return false
	})
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	before := creates.Load()
	startup := appFinishedCell(t, dirA, "Before restart")
	restarted, _, _ := appDeviceBackend(t, dirA)
	appSyncWait(t, "L1 startup publishes without a list call on A", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, wb, "chats.list", struct{}{}).Chats {
			if r.Cell == startup.ID {
				return !r.Here && r.Holder != nil && r.Holder.Name == "A"
			}
		}
		return false
	})
	// Synchronize with the startup's single publication pass.
	if _, err := restarted.ListChats(); err != nil {
		t.Fatal(err)
	}
	if n := creates.Load(); n != before+1 || before != 1 {
		t.Fatalf("L1 creates before=%d after=%d; want one create for each distinct cell", before, n)
	}
	restarted.locked(func() {
		if len(restarted.agents) != 0 {
			t.Error("L1 startup opened a chat")
		}
	})
}

func appFinishedCell(t *testing.T, dir, title string) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(filepath.Join(dir, "v3", "projects", "local"), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err = session.SaveMeta(c.Root, session.Meta{ID: c.ID, Title: title, Workspace: work, LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Root, cell.TranscriptPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	eng := cellstore.Engine{DataRoot: filepath.Join(dir, "v3", "stores"), Workspace: work}
	if _, err = eng.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	return c
}

// L1 retains #2002 even when an older home-folder chat already has a
// completed conversion copy from an earlier release.
func TestAppChatsOlderCopyStaysLocalL1(t *testing.T) {
	celltest.RequireEngine(t)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dir := t.TempDir()
	if _, err := identity.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(dir, "v3", "projects", "old", "old-chat")
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.SaveMeta(original, session.Meta{ID: "old-chat", Workspace: userHome, LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	copied := cell.CopyRoot(original)
	c, err := cell.CreateIn(filepath.Dir(copied), cell.Options{Class: cell.FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(c.Root, copied); err != nil {
		t.Fatal(err)
	}
	c.Root = copied
	c.ID = filepath.Base(copied)
	if err = session.SaveMeta(copied, session.Meta{ID: c.ID, Title: "Older home chat", Owned: true, LastUserAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(copied, cell.TranscriptPath), nil, 0600); err != nil {
		t.Fatal(err)
	}
	e := cellstore.Engine{DataRoot: filepath.Join(dir, "v3", "stores")}
	if _, err = e.Seal(t.Context(), c, cellstore.TurnInfo{}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(copied, cell.StateDir, "copy-ready"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, w := appDeviceBackend(t, dir)
	rows := machineCall[appwire.ChatsResult](t, w, "chats.list", struct{}{}).Chats
	if len(rows) != 0 {
		t.Fatalf("L1 older home-folder copy was published: %+v", rows)
	}
}
