package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type e2Frame struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}
type e2App struct {
	w      *actionWire
	mu     sync.Mutex
	events []e2Frame
	more   chan struct{}
	done   chan error
	log    string
}

func e2StartApp(t *testing.T, dir string) *e2App {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "app", "--home", dir)
	cmd.Env = append(env.EnvironWithout(home.EnvVar, config.ProfileDirEnv), carriedChildEnv+"=real", "CODEAF_ENGINE_DAEMON=0")
	inR, inW := io.Pipe()
	cmd.Stdin = inR
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(os.TempDir(), "e2-app-*.log")
	log := ""
	if f != nil {
		log = f.Name()
	}
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = f
	t.Logf("app %s log: %s", dir, log)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	app := &e2App{w: &actionWire{t: t, in: inW, replyWithin: 2 * time.Minute, waiting: map[int]chan actionFrame{}}, more: make(chan struct{}, 1), done: make(chan error, 1), log: log}
	go func() { app.done <- cmd.Wait(); _ = f.Close(); _ = inR.Close() }()
	t.Cleanup(func() {
		app.w.close()
		select {
		case err := <-app.done:
			if err != nil {
				raw, _ := os.ReadFile(log)
				t.Errorf("app exit: %v\n%s", err, raw)
			}
		case <-time.After(90 * time.Second):
			_ = cmd.Process.Kill()
			t.Error("app did not exit")
		}
	})
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	hello := make(chan struct{})
	go func() {
		if !scanner.Scan() {
			close(hello)
			return
		}
		var first struct {
			Data appwire.Hello `json:"data"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &first)
		app.w.hello = first.Data.Features
		close(hello)
		for scanner.Scan() {
			var reply actionFrame
			if json.Unmarshal(scanner.Bytes(), &reply) != nil {
				continue
			}
			if reply.ID != 0 {
				app.w.mu.Lock()
				ch := app.w.waiting[reply.ID]
				delete(app.w.waiting, reply.ID)
				app.w.mu.Unlock()
				if ch != nil {
					ch <- reply
				}
				continue
			}
			var frame e2Frame
			_ = json.Unmarshal(scanner.Bytes(), &frame)
			app.mu.Lock()
			app.events = append(app.events, frame)
			app.mu.Unlock()
			select {
			case app.more <- struct{}{}:
			default:
			}
		}
		// The channel is closed only once; normal output termination is separate.
	}()
	select {
	case <-hello:
	case <-time.After(30 * time.Second):
		t.Fatal("app hello timed out")
	}

	machineCall[any](t, app.w, "setup.get", struct{}{})
	machineCall[appwire.Empty](t, app.w, "keys.set", map[string]any{"provider": "openrouter", "key": "test-key"})
	machineCall[appwire.ChatsResult](t, app.w, "chats.list", struct{}{})
	return app
}
func (a *e2App) wait(t *testing.T, match func(e2Frame) bool) {
	t.Helper()
	deadline := time.After(45 * time.Second)
	seen := 0
	for {
		a.mu.Lock()
		for ; seen < len(a.events); seen++ {
			if match(a.events[seen]) {
				a.mu.Unlock()
				return
			}
		}
		a.mu.Unlock()
		select {
		case <-a.more:
		case <-deadline:
			raw, _ := os.ReadFile(a.log)
			t.Fatalf("app event timed out\n%s", raw)
		}
	}
}
func e2Turn(t *testing.T, a *e2App, id, text string) {
	t.Helper()
	got := machineCall[struct {
		Turn uint64 `json:"turn"`
	}](t, a.w, "agents.send", map[string]any{"id": id, "text": text, "client_id": text})
	a.wait(t, func(f e2Frame) bool {
		if f.Event != appwire.EventTurnDone {
			return false
		}
		var d appwire.TurnDone
		_ = json.Unmarshal(f.Data, &d)
		if d.Turn != got.Turn {
			return false
		}
		if !d.OK {
			t.Fatalf("turn failed: %s", f.Data)
		}
		return true
	})
}

// E-T1, E-T2, E-Q1: actual app commands, two homes, a scripted model and a
// loopback relay exercise takeover, idle notification, stale consent and quit.
func TestAppTakeET1ET2QuitEQ1(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	model := appTwoMessageSource(t, "printf codeaf-e2")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)

	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	dirA, dirB := home.Dir(), t.TempDir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	for dir, name := range map[string]string{dirA: "A", dirB: "B"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0600); err != nil {
			t.Fatal(err)
		}

		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	// A completed pairing has published both computers before continuation.
	for _, dir := range []string{dirA, dirB} {
		s := appDeviceSync(t, dir)
		if err := syncsetup.PutOwnDevice(context.Background(), s.Dir, s.Identity, s.Device, devname.Name(dir)); err != nil {
			t.Fatal(err)
		}
	}
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "first")
	// Start continuation from a chat saved by the real app leaving path. This
	// exercises a saved chat independently of background first-publication timing.
	e2Quit(t, a)
	a = e2StartApp(t, dirA)
	machineCall[appwire.TakeResult](t, a.w, "agents.open", appwire.IDParams{ID: created.ID})

	t.Logf("created %s", string(e2JSON(created)))
	t.Logf("A sync: %s", string(e2JSON(machineCall[appwire.SyncResult](t, a.w, "sync.get", struct{}{}))))
	var row appwire.ChatRow
	appSyncWait(t, "published chat", func() bool {
		rows := machineCall[appwire.ChatsResult](t, b.w, "chats.list", struct{}{})
		for _, r := range rows.Chats {
			if r.Title != "" && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Location == nil || !taken.Location.Here {
		t.Fatalf("taken: %+v", taken)
	}
	a.wait(t, func(f e2Frame) bool {
		if f.Event != appwire.EventAgentChanged {
			return false
		}
		var d appwire.AgentChanged
		_ = json.Unmarshal(f.Data, &d)
		return d.Agent.ID == created.ID && d.Agent.Location != nil && !d.Agent.Location.Here && d.Agent.Location.Holder != nil && d.Agent.Location.Holder.Name == "B"
	})
	_, refusal := a.w.call("agents.send", map[string]any{"id": created.ID, "text": "kept draft", "client_id": "refused"})
	if refusal == nil || refusal.Code != appwire.CodeConflict || !strings.Contains(string(e2JSON(refusal.Detail)), "held_elsewhere") {
		t.Fatalf("send refusal: %+v", refusal)
	}
	_, conflict := a.w.call("agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen})
	if conflict == nil || conflict.Code != appwire.CodeConflict || !strings.Contains(string(e2JSON(conflict.Detail)), "holder_changed") {
		t.Fatalf("stale: %+v", conflict)
	}
	// A backend remembering a saved chat for the first time also refuses work
	// from the inventory's known location, before opening a conversation.
	cold, _, coldWire := appDeviceBackend(t, dirA)
	machineCall[appwire.ChatsResult](t, coldWire, "chats.list", struct{}{})
	_, savedRefusal := coldWire.call("agents.send", map[string]any{"id": created.ID, "text": "saved draft", "client_id": "saved-refusal"})
	if savedRefusal == nil || savedRefusal.Code != appwire.CodeConflict || !strings.Contains(string(e2JSON(savedRefusal.Detail)), "held_elsewhere") {
		t.Fatalf("saved chat refusal: %+v", savedRefusal)
	}
	coldWire.close()
	_ = cold.Close()
	e2Turn(t, b, taken.ID, "after take")
	// EOF immediately after turn.done must flush and release, with no empty turn.
	e2Quit(t, b)
	s := appDeviceSync(t, dirA)
	v, err := s.Dir.Cell(context.Background(), row.Cell)
	if err != nil {
		t.Fatal(err)
	}
	if v.Cell.Lease.Expires != 0 {
		t.Fatalf("quit stranded the hold: %+v", v.Cell.Lease)
	}
	if v.Cell.OrphanTurns != 0 {
		t.Fatalf("empty turn branch: %+v", v.Cell)
	}
	again := machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: appwire.Seen{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence}}).Agent
	if again.Location == nil || !again.Location.Here {
		t.Fatal(again)
	}
	entries, readErr := a.w.call("agents.entries", appwire.IDParams{ID: again.ID})
	if readErr != nil || !strings.Contains(string(entries), "after take") {
		t.Fatalf("quit lost the final turn: %s %+v", entries, readErr)
	}
	listing, err := s.Dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range listing.Cells {
		if c.ParentCell != "" {
			t.Fatalf("quit created a branch: %+v", c)
		}
	}
}
func e2JSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

// The engine door is exercised against the established directory/store wire
// rig. CODEAF_E2_DURABLE_RELAY=1 selects relayserve's durable backend.
func e2WireRelay(t *testing.T, wrappers ...func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	links := directory.NewLinks(time.Now, directory.DefaultLinkLimits)
	dirs := map[string]directory.Directory{}
	stores := map[string]blobstore.Store{}
	auth := reqsign.AuthenticateAt(time.Now)
	mux := http.NewServeMux()
	mux.Handle(directory.LinkPath+"/", directory.LinkHandler(links, pairbox.SocketPeer))
	box := pairbox.Handler(pairbox.NewMemory(pairbox.DefaultLimits, time.Now), pairbox.SocketPeer)
	mux.Handle(pairbox.Path, box)
	mux.Handle(pairbox.Path+"/", box)

	mux.Handle("/v1/dir/", directory.Handler(auth, func(id string) (directory.Directory, error) {
		mu.Lock()
		defer mu.Unlock()
		if dirs[id] == nil {
			m := directory.NewMemory(time.Now)
			m.SetLinks(links)
			dirs[id] = m
		}
		return dirs[id], nil
	}))
	mux.Handle("/v1/store/", blobstore.Handler(auth, func(id string) (blobstore.Store, error) {
		mu.Lock()
		defer mu.Unlock()
		if stores[id] == nil {
			stores[id] = blobstore.NewMemory()
		}
		return stores[id], nil
	}))
	var handler http.Handler = mux
	for _, wrap := range wrappers {
		handler = wrap(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func e2Quit(t *testing.T, a *e2App) {
	t.Helper()
	a.w.close()
	select {
	case err := <-a.done:
		if err != nil {
			raw, _ := os.ReadFile(a.log)
			t.Fatalf("quit: %v\n%s", err, raw)
		}
		a.done <- nil
	case <-time.After(90 * time.Second):
		t.Fatal("quit did not finish")
	}
	raw, _ := os.ReadFile(a.log)
	if strings.Contains(string(raw), "could not save") {
		t.Fatalf("save failure: %s", raw)
	}
}

func e2Relay(t *testing.T) *httptest.Server {
	t.Helper()
	if os.Getenv("CODEAF_E2_DURABLE_RELAY") == "1" {
		return appSyncRelay(t)
	}
	return e2WireRelay(t)
}

// E-T2: stale consent is refused before any restore, even for a chat that has
// never been copied here; the directory's computer and fence do not move.
func TestAppTakeET2(t *testing.T) {
	celltest.Available(t)
	relay := e2WireRelay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	dirA, dirB := t.TempDir(), t.TempDir()
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	s := appDeviceSync(t, dirA)
	chat, err := cell.NewID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Dir.Create(context.Background(), chat, directory.CellInit{})
	if err != nil {
		t.Fatal(err)
	}
	_, _, w := appDeviceBackend(t, dirB)
	_, missing := w.call("agents.take", appwire.TakeParams{Cell: "unknown-chat", Seen: appwire.Seen{Device: before.Cell.Lease.Device, Fence: 0}})
	if missing == nil || missing.Code != appwire.CodeNotFound {
		t.Fatalf("unknown chat: %+v", missing)
	}
	_, failure := w.call("agents.take", appwire.TakeParams{Cell: chat, Seen: appwire.Seen{Device: before.Cell.Lease.Device, Fence: 0}})
	if failure == nil || failure.Code != appwire.CodeConflict || failure.Message != chatlist.LostRace || !strings.Contains(string(e2JSON(failure.Detail)), "holder_changed") {
		t.Fatalf("stale consent: %+v", failure)
	}
	after, err := s.Dir.Cell(context.Background(), chat)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cell.Lease.Device != before.Cell.Lease.Device || after.Cell.Lease.Fence != before.Cell.Lease.Fence {
		t.Fatalf("stale consent moved the chat: %+v", after.Cell.Lease)
	}
}
