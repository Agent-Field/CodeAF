package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

type hostedMoveRig struct {
	t              *testing.T
	c              cell.Cell
	work           string
	a, b           *syncsetup.Sync
	ea, eb         cellstore.Engine
	bRoots         string
	modelCalls     atomic.Int32
	mu             sync.Mutex
	requests       []string
	acquires       []time.Time
	acquiredFences []uint64
	beats          []hostedBeat
	changes        chan struct{}
}

// hostedBeat is one heartbeat the relay accepted, with the hold it renewed.
type hostedBeat struct {
	at    time.Time
	lease directory.Lease
}

type moveRenewals struct {
	drive   *syncsetup.Drive
	changes <-chan struct{}
}

func (m moveRenewals) RenewalStatus() (time.Time, time.Duration, bool) {
	return m.drive.RenewalStatus()
}
func (m moveRenewals) RenewalChanges() <-chan struct{} { return m.changes }

func newHostedMoveRig(t *testing.T, wrap ...func(http.Handler) http.Handler) *hostedMoveRig {
	t.Helper()
	cfg, c := doorChat(t)
	short, err := os.MkdirTemp("", "cf-move-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	t.Setenv(home.EnvVar, short)
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	id, err := identity.Ensure(short)
	if err != nil {
		t.Fatal(err)
	}
	h := &hostedMoveRig{t: t, c: c, work: cfg.Place.Workspace, bRoots: t.TempDir(), changes: make(chan struct{}, 1)}
	service := relayserveForMove(t)
	for _, decorate := range wrap {
		service = decorate(service)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.requests = append(h.requests, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/acquire") {
			h.acquires = append(h.acquires, time.Now())
		}
		h.mu.Unlock()
		if acquire, beat := strings.HasSuffix(r.URL.Path, "/acquire"), strings.HasSuffix(r.URL.Path, "/heartbeat"); acquire || beat {
			answer := httptest.NewRecorder()
			service.ServeHTTP(answer, r)
			if answer.Code == http.StatusOK {
				var view directory.CellView
				if err := json.Unmarshal(answer.Body.Bytes(), &view); err != nil {
					t.Errorf("decode %s answer: %v", r.URL.Path, err)
				} else {
					h.mu.Lock()
					if acquire {
						h.acquiredFences = append(h.acquiredFences, view.Cell.Lease.Fence)
					} else {
						h.beats = append(h.beats, hostedBeat{at: time.Now(), lease: view.Cell.Lease})
					}
					h.mu.Unlock()
				}
			}
			for name, values := range answer.Header() {
				w.Header()[name] = values
			}
			w.WriteHeader(answer.Code)
			_, _ = w.Write(answer.Body.Bytes())
			return
		}
		service.ServeHTTP(w, r)
	}))
	t.Cleanup(relay.Close)
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	h.a, _, err = syncsetup.Open(short)
	if err != nil {
		t.Fatal(err)
	}
	bHome := t.TempDir()
	if _, err := identity.Adopt(bHome, id, false); err != nil {
		t.Fatal(err)
	}
	h.b, _, err = syncsetup.Open(bHome)
	if err != nil {
		t.Fatal(err)
	}
	h.ea, h.eb = cellstore.EngineFor(h.work), cellstore.EngineFor("")
	h.ea.DataRoot, h.eb.DataRoot = t.TempDir(), t.TempDir()
	t.Cleanup(cellstore.StopSpawned)
	if err := session.SaveMeta(c.Root, session.Meta{ID: c.ID, Workspace: h.work, Title: "take-back chat"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Place.Transcript(), []byte(fmt.Sprintf("{\"type\":\"session\",\"version\":1,\"id\":%q}\n{\"type\":\"message\",\"role\":\"user\",\"content\":\"saved question\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"1\"}\n", c.ID)), 0600); err != nil {
		t.Fatal(err)
	}
	return h
}

func relayserveForMove(t *testing.T) http.Handler {
	t.Helper()
	dir := directory.NewMemory(time.Now)
	t.Cleanup(func() { dir.Close() })
	store := blobstore.NewMemory()
	auth := reqsign.AuthenticateAt(time.Now)
	mux := http.NewServeMux()
	mux.Handle("/v1/dir/", directory.Handler(auth, func(string) (directory.Directory, error) { return dir, nil }))
	mux.Handle("/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return store, nil }))
	return mux
}

func (h *hostedMoveRig) wait(what string, ready func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(cellstore.SealWithin)
	for !ready() {
		if time.Now().After(deadline) {
			h.t.Fatal("waiting for " + what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *hostedMoveRig) publish(s *syncsetup.Sync, e cellstore.Engine, c cell.Cell, name string) *syncsetup.Drive {
	h.t.Helper()
	d, err := s.Drive(h.t.Context(), e, c, syncsetup.DriveOptions{DeviceName: name, OnRenewalChange: func() {
		select {
		case h.changes <- struct{}{}:
		default:
		}
	}})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = d.Close(context.Background()) })
	sealed, err := d.Store(e).Seal(h.t.Context(), c, cellstore.TurnInfo{})
	if err != nil {
		h.t.Fatal(err)
	}
	d.Idle()
	h.wait("publication", func() bool {
		v, err := s.Dir.Cell(h.t.Context(), c.ID)
		return err == nil && v.Cell.Head == sealed.Turn.ID
	})
	return d
}

func (h *hostedMoveRig) taker(s *syncsetup.Sync, e cellstore.Engine, root func(string) string, phases func(handoff.Phase)) takeover {
	return takeover{s.Continuer(e, syncsetup.TakeOptions{DeviceName: "receiver", RootFor: root, OnPhase: phases})}
}

func (h *hostedMoveRig) host(d *syncsetup.Drive, c cell.Cell, work string, e cellstore.Engine) *remote.Client {
	h.t.Helper()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			h.modelCalls.Add(1)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	h.t.Cleanup(model.Close)
	first := true
	done := make(chan error, 1)
	go func() {
		done <- enginehost.Run(work, enginehost.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			driver := d
			if !first {
				var err error
				driver, err = h.a.Drive(h.t.Context(), e, c, syncsetup.DriveOptions{DeviceName: "receiver"})
				if err != nil {
					return nil, err
				}
			}
			first = false
			seat, err := cellstore.SeatOver(executor.HostBound, c, work, nil, nil, func(cellstore.Engine) cellstore.Store { return driver.Store(e) })
			if err != nil {
				return nil, err
			}
			agent, err := session.New(session.Config{Workspace: work, Place: session.Place{Dir: c.Root, Workspace: work}, SessionFile: session.Place{Dir: c.Root}.Transcript(), Model: "stub/chat", APIKey: "stub", BaseURL: model.URL, Seat: seat, Renewals: moveRenewals{driver, h.changes}})
			if err != nil {
				return nil, err
			}
			return &remote.Engine{Agent: agent, Workspace: work, SessionFile: agent.SessionPath(), Place: session.Place{Dir: c.Root, Workspace: work}, ReleaseOnLeave: func(remote.WrappedAgent) bool { return true }, FinishSaving: func(remote.WrappedAgent) error { return driver.Close(context.Background()) }}, nil
		}})
	}()
	var err error
	var conn net.Conn
	h.wait("host socket", func() bool { conn, err = enginehost.Dial(work); return err == nil })
	client, err := remote.Dial(conn, "", remote.Hello{})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { client.Close(); _ = enginehost.Retire(work, true); <-done })
	return client
}

func (h *hostedMoveRig) surface(client *remote.Client, taker takeover, landing bool) (*surfaceFrameWriter, *io.PipeWriter) {
	h.t.Helper()
	input, typed := io.Pipe()
	screen := &surfaceFrameWriter{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(h.t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: client.Agent(), Workspace: h.work, SessionFile: session.Place{Dir: h.c.Root}.Transcript(), Landing: landing, Machines: surfaceMachines{h.a}, Takeover: taker, Input: input, Output: screen, Width: 120, Height: 35, Open: func(where, file string) (tui3.Conversation, error) {
			conn, err := enginehost.Dial(h.work)
			if err != nil {
				return tui3.Conversation{}, err
			}
			opened, err := remote.Dial(conn, "", remote.Hello{Session: file})
			if err != nil {
				conn.Close()
				return tui3.Conversation{}, err
			}
			h.t.Cleanup(func() { opened.Close() })
			return tui3.Conversation{Agent: opened.Agent(), Workspace: where, SessionFile: file}, nil
		}})
	}()
	h.t.Cleanup(func() { cancel(); input.Close(); typed.Close(); <-done })
	h.t.Cleanup(func() {
		if h.t.Failed() {
			h.t.Logf("surface: %s", screen.String())
		}
	})
	<-screen.ready
	return screen, typed
}

// Contract F1 requires the hosted viewer's first Enter to ask before any turn runs.
func TestHostedDisplacedWindowAsksBeforeSending(t *testing.T) {
	h := newHostedMoveRig(t)
	d := h.publish(h.a, h.ea, h.c, "sender")
	client := h.host(d, h.c, h.work, h.ea)
	rootB := func(id string) string { return filepath.Join(h.bRoots, id) }
	if _, err := h.taker(h.b, h.eb, rootB, nil).Take(t.Context(), h.c.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckLease(t.Context()); err != nil {
		t.Fatal(err)
	}
	// A retained receipt must never override the live viewer state.
	staleHoldForMove(t, h.a, h.c.ID, 1)
	screen, typed := h.surface(client, h.taker(h.a, h.ea, func(string) string { return h.c.Root }, nil), false)
	h.mu.Lock()
	h.requests = nil
	h.mu.Unlock()
	if _, err := io.WriteString(typed, "Add 1 to your last answer. Just the number.\r"); err != nil {
		t.Fatal(err)
	}
	h.wait("continue card", func() bool { return strings.Contains(screen.String(), "Continue this chat here?") })
	if h.modelCalls.Load() != 0 {
		t.Fatalf("displaced Enter made %d model calls", h.modelCalls.Load())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.requests {
		if strings.HasSuffix(r, "/publish") || strings.HasPrefix(r, "POST /v1/dir/cells/") && !strings.HasSuffix(r, "/heartbeat") {
			t.Errorf("displaced Enter mutated relay: %s", r)
		}
	}
}

func staleHoldForMove(t *testing.T, s *syncsetup.Sync, id string, fence uint64) {
	t.Helper()
	sum := sha256.Sum256([]byte(s.Ledger + "/" + s.Device.ID() + "/" + id))
	path := filepath.Join(s.Home, "v3", "sync", "holds", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"cell":%q,"fence":%d}`, id, fence)), 0600); err != nil {
		t.Fatal(err)
	}
}

// Contract F3 bounds the real host's retirement and forbids uploads of shared work.
func TestHostedTakeBackReachesAcquireWithoutSharedUploadsOrReleaseDelay(t *testing.T) {
	h := newHostedMoveRig(t)
	d := h.publish(h.a, h.ea, h.c, "sender")
	client := h.host(d, h.c, h.work, h.ea)
	if err := h.b.Rename(t.Context(), "other computer"); err != nil {
		t.Fatal(err)
	}
	rootB := func(id string) string { return filepath.Join(h.bRoots, id) }
	if _, err := h.taker(h.b, h.eb, rootB, nil).Take(t.Context(), h.c.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckLease(t.Context()); err != nil {
		t.Fatal(err)
	}
	staleHoldForMove(t, h.a, h.c.ID, 1)
	var started time.Time
	phases := func(p handoff.Phase) {
		if p.Ended.IsZero() {
			t.Logf("phase %s at %s", p.Name, p.Started.Sub(started))
		} else {
			t.Logf("phase %s: %s", p.Name, p.Ended.Sub(p.Started))
		}
	}
	screen, typed := h.surface(client, h.taker(h.a, h.ea, func(string) string { return h.c.Root }, phases), false)
	if _, err := io.WriteString(typed, "take back this chat\r"); err != nil {
		t.Fatal(err)
	}
	h.wait("take-back card", func() bool { return strings.Contains(screen.String(), "Continue this chat here?") })
	h.mu.Lock()
	h.requests = nil
	h.acquires = nil
	h.acquiredFences = nil
	h.beats = nil
	h.mu.Unlock()
	started = time.Now()
	if _, err := io.WriteString(typed, "1\r"); err != nil {
		t.Fatal(err)
	}
	h.wait("hosted take-back arrival", func() bool { return strings.Contains(screen.String(), "Moved from other computer") })
	// The reopened writer resumes the take's own hold with a heartbeat rather
	// than a second acquire. Only a drive heartbeats, and the relay accepts one
	// only at the current fence, so an accepted beat at the take's fence from
	// this computer is the reopened writer driving the chat.
	var resumed hostedBeat
	h.wait("reopened hosted writer resuming the take's hold", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		if len(h.acquires) == 0 || len(h.acquiredFences) == 0 {
			return false
		}
		for _, beat := range h.beats {
			if beat.at.After(h.acquires[0]) && beat.lease.Device == h.a.Device.ID() && beat.lease.Fence == h.acquiredFences[0] {
				resumed = beat
				return true
			}
		}
		return false
	})
	view, err := h.a.Dir.Cell(t.Context(), h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	leaseCell, fence, err := h.a.SendLease(h.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.acquires) == 0 || len(h.acquiredFences) == 0 {
		t.Fatalf("take made %d acquire requests and %d successful answers; want at least one", len(h.acquires), len(h.acquiredFences))
	}
	takenFence := h.acquiredFences[0]
	if takenFence == 0 || view.Cell.Lease.Device != h.a.Device.ID() || view.Cell.Lease.Fence != takenFence {
		t.Errorf("relay hold after arrival = %+v; want A at take fence %d", view.Cell.Lease, takenFence)
	}
	if leaseCell != h.c.ID || fence != takenFence {
		t.Errorf("reopened writer's send lease = %s at fence %d; want %s at take fence %d", leaseCell, fence, h.c.ID, takenFence)
	}
	gap := resumed.at.Sub(h.acquires[0])
	t.Logf("take acquire to reopened writer heartbeat: %s", gap)
	if gap >= 5*time.Second {
		t.Errorf("take acquire to reopened writer heartbeat gap=%s", gap)
	}
	t.Logf("relay first acquire after key: %s", h.acquires[0].Sub(started))
	if elapsed := h.acquires[0].Sub(started); elapsed > time.Second {
		t.Errorf("first acquire took %s; want within 1s", elapsed)
	}
	for i := 1; i < len(h.acquires); i++ {
		t.Logf("relay acquire %d after key: %s; gap: %s", i+1, h.acquires[i].Sub(started), h.acquires[i].Sub(h.acquires[i-1]))
		if gap := h.acquires[i].Sub(h.acquires[i-1]); gap >= 5*time.Second {
			t.Errorf("acquire gap=%s", gap)
		}
	}
	for _, r := range h.requests {
		if strings.HasSuffix(r, "/acquire") {
			break
		}
		if strings.HasPrefix(r, "POST /v1/store/frames") {
			t.Errorf("shared work uploaded before acquire: %s", r)
		}
	}
}

type crashedWriterConfig struct {
	Home, Root, Work, Data string
}

// hostedAltChord mirrors internal/tui3/chords.go detectChords so the wait reads
// the modifier the surface displays on this platform.
func hostedAltChord(key string) string {
	if runtime.GOOS == "darwin" {
		return "opt+" + key
	}
	return "alt+" + key
}

// TestCrashedWriterProcess is the disposable writer used by the crash contract.
func TestCrashedWriterProcess(t *testing.T) {
	raw := os.Getenv("CODEAF_CRASHED_WRITER")
	if raw == "" {
		return
	}
	celltest.RequireEngine(t)
	var cfg crashedWriterConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv(home.EnvVar, cfg.Home)
	s, ok, err := syncsetup.Open(cfg.Home)
	if err != nil || !ok {
		t.Fatalf("open crashed writer: %v", err)
	}
	c, err := cell.OpenAt(cfg.Root, filepath.Base(cfg.Root))
	if err != nil {
		t.Fatal(err)
	}
	e := cellstore.EngineFor(cfg.Work)
	e.DataRoot = cfg.Data
	d, err := s.Drive(t.Context(), e, c, syncsetup.DriveOptions{DeviceName: "killed computer"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := session.New(session.Config{Workspace: cfg.Work, Place: session.Place{Dir: c.Root, Workspace: cfg.Work}, SessionFile: session.Place{Dir: c.Root}.Transcript(), Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = agent
	sealed, err := d.Store(e).Seal(t.Context(), c, cellstore.TurnInfo{})
	if err != nil {
		t.Fatal(err)
	}
	d.Idle()
	for {
		v, err := s.Dir.Cell(t.Context(), c.ID)
		_, fence, heldErr := s.SendLease(c.ID)
		shared, sharedErr := cellstore.Shared(c)
		if err == nil && v.Cell.Head == sealed.Turn.ID && heldErr == nil && fence != 0 && sharedErr == nil && shared {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Fprintln(os.Stdout, "writer holds the chat")
	<-t.Context().Done()
}

func (h *hostedMoveRig) killedWriter() {
	h.t.Helper()
	raw, err := json.Marshal(crashedWriterConfig{h.a.Home, h.c.Root, h.work, h.ea.DataRoot})
	if err != nil {
		h.t.Fatal(err)
	}
	child := exec.CommandContext(h.t.Context(), os.Args[0], "-test.run=^TestCrashedWriterProcess$", "-test.timeout=2m")
	child.Env = append(os.Environ(), "CODEAF_CRASHED_WRITER="+string(raw))
	output, err := child.StdoutPipe()
	if err != nil {
		h.t.Fatal(err)
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		h.t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			child.Process.Kill()
			child.Wait()
		}
	}()
	ready := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(output)
		var lines []string
		for scan.Scan() {
			lines = append(lines, scan.Text())
			if scan.Text() == "writer holds the chat" {
				ready <- ""
				return
			}
		}
		ready <- strings.Join(lines, "\n")
	}()
	select {
	case failure := <-ready:
		if failure != "" {
			h.t.Fatalf("writer failed: %s\n%s", failure, stderr.String())
		}
	case <-time.After(30 * time.Second):
		h.t.Fatal("writer did not publish before its kill")
	}
	if err := child.Process.Kill(); err != nil {
		h.t.Fatal(err)
	}
	_ = child.Wait()
	joined = true
	if _, fence, err := h.a.SendLease(h.c.ID); err != nil || fence == 0 {
		h.t.Fatalf("kill did not leave its receipt: %v", err)
	}
}

// Contract F2 takes a killed writer's chat back on the first Home or card attempt.
func TestKilledComputerTakesBackFromHomeAndLocalCard(t *testing.T) {
	for _, door := range []string{"home", "card"} {
		t.Run(door, func(t *testing.T) {
			h := newHostedMoveRig(t)
			h.killedWriter()
			if err := h.b.Rename(t.Context(), "other computer"); err != nil {
				t.Fatal(err)
			}
			rootB := func(id string) string { return filepath.Join(h.bRoots, id) }
			if _, err := h.taker(h.b, h.eb, rootB, nil).Take(t.Context(), h.c.ID); err != nil {
				t.Fatal(err)
			}
			restarted, _, err := syncsetup.Open(h.a.Home)
			if err != nil {
				t.Fatal(err)
			}
			h.a = restarted
			d, err := h.a.Drive(t.Context(), h.ea, h.c, syncsetup.DriveOptions{DeviceName: "killed computer"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { d.Close(context.Background()) })
			if _, _, err := h.a.SendLease(h.c.ID); !os.IsNotExist(err) {
				t.Errorf("startup retained a stale receipt: %v", err)
			}
			client := h.host(d, h.c, h.work, h.ea)
			screen, typed := h.surface(client, h.taker(h.a, h.ea, func(string) string { return h.c.Root }, nil), door == "home")
			if door == "home" {
				if _, err := io.WriteString(typed, "  "); err != nil {
					t.Fatal(err)
				}
				h.wait("Home continuation offer", func() bool { return strings.Contains(screen.String(), hostedAltChord("c")) })
				if _, err := io.WriteString(typed, "\x1bc"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := io.WriteString(typed, "take back this chat\r"); err != nil {
					t.Fatal(err)
				}
				h.wait("card naming the current holder", func() bool {
					return strings.Contains(screen.String(), "Continue this chat here?") && strings.Contains(screen.String(), "other computer")
				})
				if _, err := io.WriteString(typed, "1\r"); err != nil {
					t.Fatal(err)
				}
			}
			h.wait("Moved from after the first take", func() bool { return strings.Contains(screen.String(), "Moved from other computer") })
			v, err := h.a.Dir.Cell(t.Context(), h.c.ID)
			if err != nil || v.Cell.Lease.Device != h.a.Device.ID() {
				t.Fatalf("take-back holder=%+v: %v", v.Cell.Lease, err)
			}
		})
	}
}
