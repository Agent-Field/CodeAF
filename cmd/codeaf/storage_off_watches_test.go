package main

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

func storageOffNoticeConfig(t *testing.T) session.Config {
	t.Helper()
	cfg, _ := spokenDoorChat(t)
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(svc.Handler)
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if err := syncsetup.SaveStorage(home.Dir(), srv.URL, id.ID(), "off"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(syncDrives.closeAll)
	t.Cleanup(cellstore.StopSpawned)
	return cfg
}

// Item 4 exercises session retirement on retained in-process drives and ordinary engine cleanup.
func TestNoticeWatchesRetire(t *testing.T) {
	for _, engine := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-process reopening", true: "engine cleanup"}[engine], func(t *testing.T) {
			base := storageOffNoticeConfig(t)
			for i := 0; i < 4; i++ {
				cfg := v3Seated(base)
				d := syncDrives.renewalsOf(cfg.Place.ID())
				startupFinished(d)
				cfg.Seals.(*cellstore.SealWatch).OnTail = nil
				if got := storageOffWatchCount(d.notices); got != 1 {
					t.Fatalf("reopen %d live watches=%d", i, got)
				}
				// Closing the real seated agent must release its subscription and pending captures.
				agent, err := session.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = agent.Close() })
				// Clarification conversations inherit the parent's watch without owning it.
				childCfg := cfg
				childCfg.Seat, childCfg.Place, childCfg.SessionFile = nil, session.Place{}, ""
				childCfg.Errand = true
				child, err := session.New(childCfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := child.Close(); err != nil {
					t.Fatal(err)
				}
				if got := storageOffWatchCount(d.notices); got != 1 {
					t.Fatalf("child retired its parent's watch: %d", got)
				}
				if err := agent.Close(); err != nil {
					t.Fatal(err)
				}
				if engine {
					if err := syncDrives.closeCell(cfg.Place.ID()); err != nil {
						t.Fatal(err)
					}
				}
				if got := storageOffWatchCount(d.notices); got != 0 {
					t.Fatalf("closed session retained %d watches", got)
				}
				if engine && syncDrives.renewalsOf(cfg.Place.ID()) != nil {
					t.Fatal("engine kept a closed drive")
				}
			}
		})
	}
}

func storageOffWatchCount(n *driveNotices) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.watches)
}

// Item 4 also retires a seated watch when construction fails before an agent exists.
func TestFailedConstructionRetiresNoticeWatch(t *testing.T) {
	cfg := storageOffNoticeConfig(t)
	_, seated, _, err := openV3Agent(cfg, cfg.Place.Workspace, func(session.Config) (*session.Agent, error) { return nil, errors.New("fixture construction failure") })
	// No agent owns this failed fixture's warm capture, so join it before removing its files.
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		if err := executor.Drain(ctx, seated.Seat); err != nil {
			t.Error(err)
		}
	}()
	if err == nil {
		t.Fatal("construction unexpectedly succeeded")
	}
	d := syncDrives.renewalsOf(cfg.Place.ID())
	startupFinished(d)
	if got := storageOffWatchCount(d.notices); got != 0 {
		t.Fatalf("failed construction retained %d watches", got)
	}
}

// Item 5 replays only an unresolved startup refusal, never announcements or recovered conditions.
func TestNoticeReplayIsCurrent(t *testing.T) {
	for _, mode := range []string{"first run", "storage full recovers", "storage off remains"} {
		t.Run(mode, func(t *testing.T) {
			_ = storageOffNoticeConfig(t)
			n := &driveNotices{}
			if mode == "first run" {
				n.say("first-run announcement")
			} else {
				entered := make(chan struct{})
				startErr := wireauth.ErrRelayFull
				if mode == "storage off remains" {
					startErr = wireauth.ErrStorageOff
				}
				l := newLiveDrive(func(func()) (*syncsetup.Drive, error) { <-entered; return nil, startErr }, n)
				l.after = func(time.Duration, func()) func() bool { return func() bool { return true } }
				close(entered)
				startupFinished(l)
				defer l.Close(t.Context())
				if mode == "storage full recovers" {
					l.start = func() (*syncsetup.Drive, error) { return &syncsetup.Drive{}, nil }
					l.retryAt = time.Time{}
					l.try()
					startupFinished(l)
					// The empty fixture drive has no process to close.
					l.drive = nil
				}
			}
			watch := &cellstore.SealWatch{}
			n.add(watch)
			got := watch.Take()
			want := ""
			if mode == "storage off remains" {
				want = chatlist.RelayStorageOff
			}
			if got != want || watch.Take() != "" {
				t.Fatalf("replayed %q want %q, exactly once", got, want)
			}
		})
	}
}

// Item 6 delivers a later notice to an idle in-process window through its bell, exactly once.
func TestIdleLocalWindowGetsNoticeAtOnce(t *testing.T) {
	previousDesk := surfaceNotices
	surfaceNotices = tui3.NewNotices()
	defer func() { surfaceNotices = previousDesk }()
	cfg := storageOffNoticeConfig(t)
	cfg = v3Seated(cfg)
	d := syncDrives.renewalsOf(cfg.Place.ID())
	startupFinished(d)
	watch := cfg.Seals.(*cellstore.SealWatch)
	watch.OnTail = nil
	for watch.Take() != "" {
	}
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	screen := &syncOffFrameCapture{}
	seam := sealSeamOf(agent, cfg.Seals)
	pushed := make(chan struct{}, 1)
	// Observe the bell binding without requiring its new field in the before-fix tree.
	listener := reflect.ValueOf(&seam).Elem().FieldByName("Listen")
	if listener.IsValid() && !listener.IsNil() {
		listen := listener.Interface().(func(func()) func())
		listener.Set(reflect.ValueOf(func(wake func()) func() {
			return listen(func() {
				select {
				case pushed <- struct{}{}:
				default:
				}
				wake()
			})
		}))
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- tui3.Run(ctx, tui3.Options{Agent: agent, SessionFile: cfg.SessionFile, Workspace: cfg.Place.Workspace, Seal: seam, Notices: surfaceNotices, Input: input, Output: screen, Width: 200, Height: 40, Linear: true})
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for screen.Plain() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	// The first frame has settled with no pending notice, key or three-second beat.
	time.Sleep(50 * time.Millisecond)
	select {
	case <-pushed:
	default:
	}
	line := "this chat moved to another computer"
	d.notices.say(line)
	select {
	case <-pushed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued sentence did not ring the idle window's notice bell")
	}
	deadline = time.Now().Add(500 * time.Millisecond)
	for !strings.Contains(screen.Plain(), line) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	shown := strings.Join(strings.Fields(screen.Plain()), " ")
	if !strings.Contains(shown, line) {
		t.Fatalf("idle window missed push: %s", shown)
	}
	// Give an additional paint time to expose accidental duplicate consumers.
	time.Sleep(100 * time.Millisecond)
	shown = strings.Join(strings.Fields(screen.Plain()), " ")
	if strings.Count(shown, line) != 1 || watch.Take() != "" {
		t.Fatalf("idle sentence was not consumed exactly once: %s", shown)
	}
}
