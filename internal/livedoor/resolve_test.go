package livedoor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ld-")
	if err != nil {
		t.Fatalf("MkdirTemp(/tmp, ld-) failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestTheResolverFindsTheHolderOfAChatThroughTheDaemonAndTheRegistryAndNeverStartsOne
// verifies contract V1 and V29: the resolver routes a chat to the workspace
// daemon when Inspect matches the presence PID, routes through the per-PID
// registry file when the daemon does not hold it, refuses missing or unlocked
// or unreachable holders with ErrNotRunning, and never invokes or exposes any
// host-spawning path.
func TestTheResolverFindsTheHolderOfAChatThroughTheDaemonAndTheRegistryAndNeverStartsOne(t *testing.T) {
	// ResolverOptions must not carry any spawn, attach, or boot callback so the
	// resolver is structurally incapable of starting a conversation or daemon.
	optType := reflect.TypeFor[ResolverOptions]()
	for i := 0; i < optType.NumField(); i++ {
		name := strings.ToLower(optType.Field(i).Name)
		if strings.Contains(name, "spawn") || strings.Contains(name, "attach") || strings.Contains(name, "boot") || strings.Contains(name, "start") {
			t.Fatalf("ResolverOptions has start-capable field %q", optType.Field(i).Name)
		}
	}

	root := shortTempDir(t)
	regDir := filepath.Join(root, "v3", "live")
	if err := os.MkdirAll(regDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", regDir, err)
	}

	// Start a unix socket for the live registry route (PID 5151).
	const regPID = 5151
	regSockName := strconv.Itoa(regPID) + ".sock"
	regSockPath := filepath.Join(regDir, regSockName)
	tr := UnixTransport()
	regListener, err := tr.Listen(regSockPath)
	if err != nil {
		t.Fatalf("Listen(%q) failed: %v", regSockPath, err)
	}
	t.Cleanup(func() { _ = regListener.Close() })
	guard.Go("livedoor.test.resolve.regAccept", func() {
		for {
			c, err := regListener.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("from-registry"))
			_ = c.Close()
		}
	})
	regJSON, err := json.Marshal(map[string]any{
		"pid":    regPID,
		"socket": regSockName,
		"build":  "build-reg",
	})
	if err != nil {
		t.Fatalf("Marshal registry json failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, strconv.Itoa(regPID)+".json"), regJSON, 0o600); err != nil {
		t.Fatalf("WriteFile registry json failed: %v", err)
	}

	// Write a registry file pointing at a dead socket (PID 7001).
	const deadPID = 7001
	deadJSON, err := json.Marshal(map[string]any{
		"pid":    deadPID,
		"socket": strconv.Itoa(deadPID) + ".sock",
		"build":  "build-dead",
	})
	if err != nil {
		t.Fatalf("Marshal dead registry json failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, strconv.Itoa(deadPID)+".json"), deadJSON, 0o600); err != nil {
		t.Fatalf("WriteFile dead registry json failed: %v", err)
	}

	chatDir := func(chat string) string {
		return filepath.Join(root, "projects", "bucket", chat)
	}
	transcriptOf := func(chat string) string {
		return (session.Place{Dir: chatDir(chat)}).Transcript()
	}

	cases := []struct {
		name          string
		chat          string
		rows          []session.SessionPresence
		inUse         bool
		daemonPID     int
		inspectErr    error
		wantErr       error
		wantKind      string
		wantPID       int
		wantWorkspace string
		wantBuild     string
		wantBanner    string
		wantNoInspect bool
		wantNoDaemon  bool
		wantNoRegDial bool
	}{
		{
			name: "presence row + in-use + daemon pid matches -> daemon route with the daemon conn",
			chat: "chat-daemon",
			rows: []session.SessionPresence{{
				SessionID: "chat-daemon",
				Workspace: "/work/daemon-proj",
				PID:       4242,
				Build:     "build-daemon",
				Dir:       chatDir("chat-daemon"),
			}},
			inUse:         true,
			daemonPID:     4242,
			wantKind:      "daemon",
			wantPID:       4242,
			wantWorkspace: "/work/daemon-proj",
			wantBuild:     "build-daemon",
			wantBanner:    "from-daemon",
			wantNoRegDial: true,
		},
		{
			name: "presence row + registry file for that pid -> registry route",
			chat: "chat-registry",
			rows: []session.SessionPresence{{
				SessionID: "chat-registry",
				Workspace: "/work/desk-proj",
				PID:       regPID,
				Build:     "build-presence",
				Dir:       chatDir("chat-registry"),
			}},
			inUse:         true,
			inspectErr:    errNothingHolding,
			wantKind:      "registry",
			wantPID:       regPID,
			wantWorkspace: "/work/desk-proj",
			wantBuild:     "build-reg",
			wantBanner:    "from-registry",
			wantNoDaemon:  true,
		},
		{
			name:          "no row -> ErrNotRunning",
			chat:          "chat-missing",
			rows:          nil,
			wantErr:       ErrNotRunning,
			wantNoInspect: true,
			wantNoDaemon:  true,
			wantNoRegDial: true,
		},
		{
			name: "row but InUse false -> ErrNotRunning",
			chat: "chat-stale",
			rows: []session.SessionPresence{{
				SessionID: "chat-stale",
				Workspace: "/work/stale-proj",
				PID:       4242,
				Build:     "build-stale",
				Dir:       chatDir("chat-stale"),
			}},
			inUse:         false,
			daemonPID:     4242,
			wantErr:       ErrNotRunning,
			wantNoInspect: true,
			wantNoDaemon:  true,
			wantNoRegDial: true,
		},
		{
			name: "row, in use, daemon pid mismatch and no registry file -> ErrNotRunning",
			chat: "chat-mismatch",
			rows: []session.SessionPresence{{
				SessionID: "chat-mismatch",
				Workspace: "/work/mismatch-proj",
				PID:       6001,
				Build:     "build-mismatch",
				Dir:       chatDir("chat-mismatch"),
			}},
			inUse:         true,
			daemonPID:     6002,
			wantErr:       ErrNotRunning,
			wantNoDaemon:  true,
			wantNoRegDial: true,
		},
		{
			name: "registry file with a dead socket -> ErrNotRunning",
			chat: "chat-dead",
			rows: []session.SessionPresence{{
				SessionID: "chat-dead",
				Workspace: "/work/dead-proj",
				PID:       deadPID,
				Build:     "build-dead",
				Dir:       chatDir("chat-dead"),
			}},
			inUse:        true,
			inspectErr:   errNothingHolding,
			wantErr:      ErrNotRunning,
			wantNoDaemon: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var inspectCalls, daemonDialCalls, regDialCalls atomic.Int32
			wantTranscript := transcriptOf(tc.chat)

			spyTransport := &spyDialTransport{
				inner: tr,
				onDial: func() {
					regDialCalls.Add(1)
				},
			}

			res := NewResolver(root, ResolverOptions{
				Presence: func(string) []session.SessionPresence {
					return tc.rows
				},
				InUse: func(path string) bool {
					if path != wantTranscript {
						t.Errorf("InUse called with %q, want %q", path, wantTranscript)
						return false
					}
					return tc.inUse
				},
				Inspect: func(workspace string) (Holder, error) {
					inspectCalls.Add(1)
					if tc.inspectErr != nil {
						return Holder{}, tc.inspectErr
					}
					return Holder{
						Self:     remote.HostSelf{PID: tc.daemonPID, Workspace: workspace},
						Answered: true,
					}, nil
				},
				DialDaemon: func(workspace string) (net.Conn, error) {
					daemonDialCalls.Add(1)
					c1, c2 := net.Pipe()
					guard.Go("livedoor.test.resolve.daemonBanner", func() {
						_, _ = c2.Write([]byte("from-daemon"))
						_ = c2.Close()
					})
					return c1, nil
				},
				RegistryDir: regDir,
				Transport:   spyTransport,
				Clock:       NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)),
				PresenceTTL: 2 * time.Second,
			})

			conn, route, err := res.Resolve(context.Background(), tc.chat)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Resolve(%q) err = %v, want %v", tc.chat, err, tc.wantErr)
				}
				if conn != nil {
					_ = conn.Close()
					t.Fatalf("Resolve(%q) returned non-nil conn on error", tc.chat)
				}
			} else {
				if err != nil {
					t.Fatalf("Resolve(%q) unexpected error: %v", tc.chat, err)
				}
				defer conn.Close()
				if route.Kind != tc.wantKind || route.PID != tc.wantPID || route.Session != wantTranscript || route.Workspace != tc.wantWorkspace || route.Build != tc.wantBuild {
					t.Fatalf("Resolve(%q) route = %+v, want Kind=%q PID=%d Session=%q Workspace=%q Build=%q",
						tc.chat, route, tc.wantKind, tc.wantPID, wantTranscript, tc.wantWorkspace, tc.wantBuild)
				}
				var buf [32]byte
				n, _ := conn.Read(buf[:])
				if got := string(buf[:n]); got != tc.wantBanner {
					t.Fatalf("conn read %q, want %q", got, tc.wantBanner)
				}
			}

			if tc.wantNoInspect && inspectCalls.Load() != 0 {
				t.Fatalf("Inspect called %d times, want 0", inspectCalls.Load())
			}
			if tc.wantNoDaemon && daemonDialCalls.Load() != 0 {
				t.Fatalf("DialDaemon called %d times, want 0", daemonDialCalls.Load())
			}
			if tc.wantNoRegDial && regDialCalls.Load() != 0 {
				t.Fatalf("Transport.Dial called %d times, want 0", regDialCalls.Load())
			}
		})
	}
}

// TestTheResolverCachesPresenceWithinItsWindowAndCoalescesConcurrentReads
// verifies contract (resolver cache): two Resolve calls inside PresenceTTL read
// presence once, advancing the fake clock past PresenceTTL triggers a fresh read,
// and 16 concurrent Resolve calls coalesce into a single presence read.
func TestTheResolverCachesPresenceWithinItsWindowAndCoalescesConcurrentReads(t *testing.T) {
	root := shortTempDir(t)
	fc := NewFakeClock(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC))
	var presenceReads atomic.Int32

	res := NewResolver(root, ResolverOptions{
		Presence: func(string) []session.SessionPresence {
			presenceReads.Add(1)
			return nil
		},
		InUse:       func(string) bool { return false },
		Clock:       fc,
		PresenceTTL: 2 * time.Second,
	})

	if _, _, err := res.Resolve(context.Background(), "chat-a"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("first Resolve err = %v, want ErrNotRunning", err)
	}
	fc.Advance(time.Second)
	if _, _, err := res.Resolve(context.Background(), "chat-b"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("second Resolve inside TTL err = %v, want ErrNotRunning", err)
	}
	if got := presenceReads.Load(); got != 1 {
		t.Fatalf("presence reads inside PresenceTTL = %d, want 1", got)
	}

	// Pass the 2 s TTL window; the third Resolve must read presence again.
	fc.Advance(2 * time.Second)
	if _, _, err := res.Resolve(context.Background(), "chat-c"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("third Resolve after TTL err = %v, want ErrNotRunning", err)
	}
	if got := presenceReads.Load(); got != 2 {
		t.Fatalf("presence reads after PresenceTTL = %d, want 2", got)
	}

	// 16 concurrent resolves on an expired cache must read presence once (single-flight).
	fc.Advance(3 * time.Second)
	const workers = 16
	var flightReads atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})

	concurrentRes := NewResolver(root, ResolverOptions{
		Presence: func(string) []session.SessionPresence {
			if flightReads.Add(1) == 1 {
				close(entered)
			}
			<-release
			return nil
		},
		InUse:       func(string) bool { return false },
		Clock:       fc,
		PresenceTTL: 2 * time.Second,
	})

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		if i == 1 {
			// Ensure the first worker has entered Presence before launching the rest.
			<-entered
		}
		guard.Go("livedoor.test.resolve.singleflight", func() {
			defer wg.Done()
			_, _, _ = concurrentRes.Resolve(context.Background(), "chat-concurrent")
		})
	}
	close(release)
	wg.Wait()

	if got := flightReads.Load(); got != 1 {
		t.Fatalf("16 concurrent Resolve calls read presence %d times, want 1", got)
	}
}

// TestTheResolverRejectsUnsafeAndOversizedChatNamesWithoutReadingPresence
// verifies contract (resolver names): "../x", "a/b", "", ".", and a
// 300-character name all return ErrNotRunning without touching presence.
func TestTheResolverRejectsUnsafeAndOversizedChatNamesWithoutReadingPresence(t *testing.T) {
	root := shortTempDir(t)
	var presenceReads atomic.Int32
	res := NewResolver(root, ResolverOptions{
		Presence: func(string) []session.SessionPresence {
			presenceReads.Add(1)
			return nil
		},
	})

	badNames := []string{
		"../x",
		"a/b",
		"",
		".",
		"..",
		`a\b`,
		strings.Repeat("x", 300),
	}
	for _, bad := range badNames {
		_, _, err := res.Resolve(context.Background(), bad)
		if !errors.Is(err, ErrNotRunning) {
			t.Fatalf("Resolve(%q) err = %v, want ErrNotRunning", bad, err)
		}
	}
	if got := presenceReads.Load(); got != 0 {
		t.Fatalf("presence was read %d times for invalid chat names, want 0", got)
	}
}

type spyDialTransport struct {
	inner  Transport
	onDial func()
}

func (s *spyDialTransport) Listen(addr string) (net.Listener, error) {
	return s.inner.Listen(addr)
}

func (s *spyDialTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if s.onDial != nil {
		s.onDial()
	}
	return s.inner.Dial(ctx, addr)
}
