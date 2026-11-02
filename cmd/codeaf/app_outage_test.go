package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// The relay retains its directory and store while the listener
// and every connection (including hijacked watch sockets) go away and return.
type outageRelay struct {
	t           *testing.T
	clock       *outageClock
	handler     http.Handler
	directory   *directory.Memory
	server      *httptest.Server
	address     string
	mu          sync.Mutex
	connections map[net.Conn]bool
	blocked     atomic.Value // certificate held offline until the competing take finishes
}

type outageClock struct{ offset atomic.Int64 }

func (c *outageClock) Now() time.Time          { return time.Now().Add(time.Duration(c.offset.Load())) }
func (c *outageClock) Advance(d time.Duration) { c.offset.Add(int64(d)) }

func newOutageRelay(t *testing.T) *outageRelay {
	t.Helper()
	clock := &outageClock{}
	r := &outageRelay{t: t, clock: clock, connections: map[net.Conn]bool{}}
	r.directory = directory.NewMemory(clock.Now)
	mux := http.NewServeMux()
	auth := reqsign.AuthenticateAt(clock.Now)
	mux.Handle("/v1/dir/", directory.Handler(auth, func(string) (directory.Directory, error) { return r.directory, nil }))
	store := blobstore.NewMemory()
	mux.Handle("/v1/store/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return store, nil }))
	r.handler = mux
	r.blocked.Store("")
	r.start()
	t.Cleanup(func() { r.stop(); _ = r.directory.Close() })
	return r
}

func (r *outageRelay) start() {
	r.t.Helper()
	address := r.address
	if address == "" {
		address = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		r.t.Fatal(err)
	}
	r.address = listener.Addr().String()
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(reqsign.HeaderCert) == r.blocked.Load().(string) {
			http.Error(w, "test computer still disconnected", http.StatusServiceUnavailable)
			return
		}
		r.handler.ServeHTTP(w, req)
	}))
	_ = s.Listener.Close()
	s.Listener = listener
	s.Config.ConnState = func(c net.Conn, state http.ConnState) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if state == http.StateClosed {
			delete(r.connections, c)
		} else {
			r.connections[c] = true
		}
	}
	s.Start()
	r.server = s
}

func (r *outageRelay) stop() {
	if r.server == nil {
		return
	}
	_ = r.server.Listener.Close()
	r.mu.Lock()
	for c := range r.connections {
		_ = c.Close()
	}
	r.mu.Unlock()
	r.server.Close()
	r.server = nil
}

func outageHomes(t *testing.T, relayURL string) (string, string) {
	t.Helper()
	t.Setenv(syncsetup.URLVar, relayURL)
	t.Setenv(syncsetup.IntervalVar, "50")
	a, b := home.Dir(), e2TempDir(t)
	id, err := identity.Ensure(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(b, id, false); err != nil {
		t.Fatal(err)
	}
	for dir, name := range map[string]string{a: "A", b: "B"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
		s := appDeviceSync(t, dir)
		if err := syncsetup.PutOwnDevice(t.Context(), s.Dir, s.Identity, s.Device, name); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(cellstore.StopSpawned)
	return a, b
}

func outageChatRow(t *testing.T, app *e2App, id string, ready func(appwire.ChatRow) bool) appwire.ChatRow {
	t.Helper()
	var found appwire.ChatRow
	outageWait(t, "E8 chat row "+id, func() bool {
		rows := machineCall[appwire.ChatsResult](t, app.w, "chats.list", struct{}{})
		for _, row := range rows.Chats {
			if (id == "" || row.Cell == id) && ready(row) {
				found = row
				return true
			}
		}
		return false
	})
	return found
}

func TestAppRelayOutageO1O2O3O4O5(t *testing.T)               { appRelayOutage(t, false) }
func TestAppRelayOutageO2CompetingTakeKeepsWork(t *testing.T) { appRelayOutage(t, true) }

func TestAppRelayOutageO3O4StartupRecovery(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	model := appTwoMessageSource(t, "echo offline-start")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	relay := newOutageRelay(t)
	dirA, dirB := outageHomes(t, relay.server.URL)
	a := e2StartApp(t, dirA)
	relay.stop()
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "opened while relay was down")
	relay.start()
	b := e2StartApp(t, dirB)
	outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake && !r.SavedAt.IsZero() })
}

func TestAppTakeC1UnavailableModelUsesLocalDefaultOnce(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	t.Setenv(config.APIKeyEnv, "")
	model := appTwoMessageSource(t, "echo model-recovery")
	t.Cleanup(model.Close)
	var requestedMu sync.Mutex
	var requested []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var request struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &request)
		if request.Model != "" {
			requestedMu.Lock()
			requested = append(requested, request.Model)
			requestedMu.Unlock()
		}
		if strings.HasPrefix(request.Model, "ollama/") || strings.HasPrefix(request.Model, "receiver/") {
			http.Error(w, "unavailable model reached the router", http.StatusBadRequest)
			return
		}
		model.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("CODEAF_BASE_URL", endpoint.URL)
	relay := e2WireRelay(t)
	dirA, dirB := outageHomes(t, relay.URL)
	if err := config.WriteSources(dirA, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: endpoint.URL}}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteChatModel(dirA, "ollama/llama3.2:latest"); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteSources(dirB, []config.PersistedSource{{ID: "custom-receiver", Written: "receiver", Address: endpoint.URL, Key: "receiver-key"}}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteChatModel(dirB, "receiver/local-default"); err != nil {
		t.Fatal(err)
	}
	configured := func(dir string) *e2App {
		return e2StartAppWithSetup(t, dir, func(app *e2App) {
			machineCall[any](t, app.w, "setup.get", struct{}{})
			machineCall[appwire.ChatsResult](t, app.w, "chats.list", struct{}{})
		})
	}
	a, b := configured(dirA), configured(dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace, "model": "ollama/llama3.2:latest"}).Agent
	if created.Model != "ollama/llama3.2:latest" {
		t.Fatalf("C1 source model = %q", created.Model)
	}
	e2Turn(t, a, created.ID, "from Ollama")
	e2Quit(t, a)
	row := outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake })
	meta, err := session.LoadMeta(takeRootForAt(dirA, row.Cell))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "ollama/llama3.2:latest" {
		t.Fatalf("C1 source saved model = %q", meta.Model)
	}
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Model != "receiver/local-default" {
		t.Fatalf("C1 taken model = %q, want the usable local default", taken.Model)
	}
	e2Turn(t, b, taken.ID, "first local turn")
	e2Turn(t, b, taken.ID, "second local turn")
	requestedMu.Lock()
	usedDefault := false
	for _, name := range requested {
		usedDefault = usedDefault || name == "local-default"
	}
	requestedMu.Unlock()
	if !usedDefault {
		t.Fatal("C1 no request used the receiver's default")
	}
	b.mu.Lock()
	events := append([]e2Frame(nil), b.events...)
	b.mu.Unlock()
	count := 0
	for _, f := range events {
		if f.Event != appwire.EventEntryAdded {
			continue
		}
		var event appwire.EntryAdded
		_ = json.Unmarshal(f.Data, &event)
		if strings.Contains(event.Entry.Text, "isn't set up here") && strings.Contains(event.Entry.Text, "receiver/local-default") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("C1 model recovery notices = %d, want one", count)
	}
	// The new main has never been called, so the last request's provider is
	// not enough to describe the saved model choice on the returning computer.
	machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, b.w, "agents.set_models", map[string]any{"id": taken.ID, "main": "receiver/never-called", "fallback": ""})
	e2Quit(t, b)
	if err := config.WriteAPIKey(dirA, "original-router-key"); err != nil {
		t.Fatal(err)
	}
	a = configured(dirA)
	back := outageChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.CanTake && r.Seen.Device != row.Seen.Device })
	returned := machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: back.Seen}).Agent
	if returned.Model != "ollama/llama3.2:latest" {
		t.Fatalf("C1 returning home used %q", returned.Model)
	}
	e2Turn(t, a, returned.ID, "back on original computer")
}

func appRelayOutage(t *testing.T, competing bool) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	model := appTwoMessageSource(t, "echo outage")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	relay := newOutageRelay(t)
	dirA, dirB := outageHomes(t, relay.server.URL)
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "before take")
	e2Quit(t, a)
	row := outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake })
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	e2Turn(t, b, taken.ID, "before outage")
	a = e2StartApp(t, dirA)
	before := outageChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.SavedAt.After(row.SavedAt) && r.Seen.Device != row.Seen.Device })
	relay.stop()
	relay.clock.Advance(directory.LeaseTTL + time.Second)
	e2Turn(t, b, taken.ID, "saved during outage")
	c, err := cell.OpenAt(takeRootForAt(dirB, row.Cell), row.Cell)
	if err != nil {
		t.Fatal(err)
	}
	transcript, err := os.ReadFile(session.Place{Dir: c.Root}.Transcript())
	if err != nil || !bytes.Contains(transcript, []byte("saved during outage")) {
		t.Fatalf("O4 outage transcript missing: %v", err)
	}
	end := int64(len(transcript))
	var latest *cellstore.Sealed
	appSyncWait(t, "O4 local outage turn saved", func() bool {
		latest, err = cellstore.Head(c)
		return err == nil && latest != nil && latest.Receipt.Transcript.End >= end
	})
	if competing {
		s := appDeviceSync(t, dirB)
		cert, _ := json.Marshal(s.Device.Cert)
		relay.blocked.Store(base64.RawURLEncoding.EncodeToString(cert))
	}
	relay.start()
	if competing {
		fresh, err := appDeviceSync(t, dirA).Dir.Cell(t.Context(), row.Cell)
		if err != nil {
			t.Fatal(err)
		}
		machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: appwire.Seen{Device: fresh.Cell.Lease.Device, Fence: fresh.Cell.Lease.Fence, Head: fresh.Cell.Head}})
		relay.blocked.Store("")
		b.wait(t, func(f e2Frame) bool {
			if f.Event != appwire.EventAgentChanged {
				return false
			}
			var event appwire.AgentChanged
			_ = json.Unmarshal(f.Data, &event)
			l := event.Agent.Location
			return event.Agent.ID == taken.ID && l != nil && !l.Here && l.Holder != nil && l.Holder.Name == "A" && strings.Contains(l.Line, "A continued this chat")
		})
		outageWait(t, "O2 pending work kept on relay", func() bool {
			listing, err := appDeviceSync(t, dirA).Dir.List(t.Context())
			if err != nil {
				return false
			}
			for _, record := range listing.Cells {
				if record.ParentCell == row.Cell && record.OrphanTurns > 0 {
					// A late accounting seal may extend the outage turn while
					// displacement settles; the kept head must include that turn.
					turns, err := cellstore.Turns(c)
					if err != nil {
						t.Fatal(err)
					}
					contains := false
					for _, turn := range turns {
						contains = contains || turn.ID == latest.Turn.ID
						if turn.ID == record.Head && contains {
							return true
						}
					}
				}
			}
			return false
		})
		_, refusal := b.w.call("agents.send", map[string]any{"id": taken.ID, "text": "after A took", "client_id": "after-take"})
		if refusal == nil || !strings.Contains(string(e2JSON(refusal.Detail)), "held_elsewhere") {
			t.Fatalf("O2 lost ownership did not refuse plainly: %+v", refusal)
		}
		return
	}
	saved, err := cellstore.SavedAt(c, latest.Turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	recovered := outageChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool {
		return r.Seen.Head == latest.Turn.ID && r.SavedAt.UnixMilli() == saved && r.Online != nil && *r.Online && !strings.Contains(r.Line, "offline")
	})
	if recovered.Seen.Device != before.Seen.Device || recovered.Seen.Fence < before.Seen.Fence {
		t.Fatalf("O5 wrong recovered holder: %+v", recovered)
	}
	// O3 recovery above required no send, quit or reopen. A later send still works.
	e2Turn(t, b, taken.ID, "after recovery")
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, f := range b.events {
		if f.Event == appwire.EventAgentChanged {
			var event appwire.AgentChanged
			_ = json.Unmarshal(f.Data, &event)
			l := event.Agent.Location
			if event.Agent.ID == taken.ID && l != nil && !l.Here && l.Holder != nil && l.Holder.ID == before.Seen.Device {
				t.Fatalf("O1 own chat became a viewer: %s", f.Data)
			}
		}
		if f.Event == appwire.EventEntryAdded && strings.Contains(string(f.Data), "B continued this chat") {
			t.Fatalf("O1 self displacement notice: %s", f.Data)
		}
	}
}

func outageWait(t *testing.T, what string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
