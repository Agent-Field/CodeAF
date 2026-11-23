package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

func appJoinTestBudget(t *testing.T, wait time.Duration) {
	t.Helper()
	old := appHostJoinWait
	appHostJoinWait = wait
	t.Cleanup(func() { appHostJoinWait = old })
}

// appJoinTestHost uses the same short home and socket layout as answeringHost.
// Cleanup closes accepted sockets too, so a regression cannot strand a test.
func appJoinTestHost(t *testing.T, workspace string, answer func(net.Conn)) {
	t.Helper()
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatal(err)
	}
	path, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var sockets []net.Conn
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			sockets = append(sockets, conn)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				answer(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mu.Lock()
		for _, conn := range sockets {
			_ = conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
}

func appJoinTestChat(t *testing.T) (workspace, transcript string) {
	t.Helper()
	shortStateRoot(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(config.APIKeyEnv, "")
	t.Setenv("CODEAF_SYNC_URL", "off")
	workspace = t.TempDir()
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "0123456789abcdef", "saved words", time.Now())
	if err := session.SaveMeta(dir, session.Meta{ID: "0123456789abcdef", Workspace: workspace, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	transcript = session.Place{Dir: dir}.Transcript()
	file, err := os.OpenFile(transcript, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	return workspace, transcript
}

// Contract 1: first open is bounded, independent reads answer, and the next
// open reaches the host again after the failed attempt releases its gate.
func TestAppHostJoinSilentOpenReleasesGate(t *testing.T) {
	workspace, transcript := appJoinTestChat(t)
	before, err := os.ReadFile(transcript)
	if err != nil {
		t.Fatal(err)
	}
	appJoinTestBudget(t, 80*time.Millisecond)
	b := newFakeBackend(newAppRecorder(), appOpenInProcess)
	b.profileDir = t.TempDir()
	t.Cleanup(func() { _ = b.Close() })
	hellos := make(chan remote.Hello, 4)
	closed := make(chan int64, 4)
	appJoinTestHost(t, workspace, func(conn net.Conn) {
		var frame remote.Frame
		if err := json.NewDecoder(conn).Decode(&frame); err != nil {
			return
		}
		var hello remote.Hello
		_ = json.Unmarshal(frame.Payload, &hello)
		hellos <- hello
		n, _ := io.Copy(io.Discard, conn)
		closed <- n
	})
	for attempt := range 4 {
		started := time.Now()
		opened := make(chan error, 1)
		go func() { _, err := b.OpenAgent(appAgentID(transcript)); opened <- err }()
		select {
		case hello := <-hellos:
			if !hello.Join || hello.Session != transcript || hello.New {
				t.Fatalf("open was not a join: %+v", hello)
			}
		case <-time.After(time.Second):
			t.Fatal("open did not reach the host")
		}
		reads := make(chan error, 1)
		go func() {
			_, err := b.ListAgents("")
			if err == nil {
				_, err = b.ListModels()
			}
			reads <- err
		}()
		select {
		case err := <-reads:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(150 * time.Millisecond):
			t.Fatal("agent/model reads waited behind the silent host")
		}
		select {
		case err := <-opened:
			t.Fatalf("open finished before the concurrent reads: %v", err)
		default:
		}
		select {
		case err := <-opened:
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != appwire.CodeUnreachable || wire.Message != "This chat didn’t answer from the other codeaf window. Try again, or continue there." {
				t.Fatalf("attempt %d: open error = %v", attempt, err)
			}
		case <-time.After(time.Until(started.Add(200 * time.Millisecond))):
			t.Fatal("first open exceeded the shared join budget plus slack")
		}
		t.Logf("silent open %d: %s", attempt+1, time.Since(started))
		select {
		case n := <-closed:
			if n != 0 {
				t.Fatalf("failed open sent %d bytes after hello", n)
			}
		case <-time.After(time.Second):
			t.Fatal("failed open left its socket open")
		}
		if !session.InUse(transcript) {
			t.Fatal("failed open took the journal from its original writer")
		}
	}
	if files, err := filepath.Glob(filepath.Join(session.DirOf(transcript), "tasks", "*")); err != nil || len(files) != 0 {
		t.Fatalf("failed open created tasks: %v, %v", files, err)
	}
	if after, err := os.ReadFile(transcript); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed opens changed the journal: %v", err)
	}
}

// Contract 2: discovery has one deadline even after earlier candidates have
// used part of it, and exhaustion never moves on to another silent socket.
func TestAppHostJoinCandidatesShareBudget(t *testing.T) {
	for _, refusals := range []int32{0, 2} {
		t.Run(fmt.Sprintf("refusals=%d", refusals), func(t *testing.T) {
			workspace, transcript := appJoinTestChat(t)
			appJoinTestBudget(t, 120*time.Millisecond)
			var attempts atomic.Int32
			closed := make(chan struct{}, 4)
			for range 4 {
				appJoinTestHost(t, t.TempDir(), func(conn net.Conn) {
					var hello remote.Frame
					if json.NewDecoder(conn).Decode(&hello) != nil {
						return
					}
					if attempts.Add(1) <= refusals {
						time.Sleep(35 * time.Millisecond)
						_ = json.NewEncoder(conn).Encode(remote.Frame{Kind: "fatal", Error: "not held here"})
					}
					_, _ = io.Copy(io.Discard, conn)
					closed <- struct{}{}
				})
			}
			started := time.Now()
			opened := make(chan error, 1)
			go func() {
				_, err := appJoinExisting(appOpenAsk{Workspace: workspace, Transcript: transcript}, nil)
				opened <- err
			}()
			select {
			case err := <-opened:
				var wire *appwire.Error
				if !errors.As(err, &wire) || wire.Code != appwire.CodeUnreachable {
					t.Fatalf("join error = %v", err)
				}
			case <-time.After(170 * time.Millisecond):
				t.Fatal("candidate handshakes multiplied the overall join budget")
			}
			t.Logf("shared join: %s, %d candidates", time.Since(started), attempts.Load())
			if got := attempts.Load(); got != refusals+1 {
				t.Fatalf("asked %d candidates, want %d", got, refusals+1)
			}
			for range refusals + 1 {
				select {
				case <-closed:
				case <-time.After(time.Second):
					t.Fatal("a candidate socket was not closed")
				}
			}
		})
	}
}

type appJoinScriptedAgent struct{ scriptedAgent }

func (a *appJoinScriptedAgent) ModelIdentity() session.ModelIdentity {
	return session.ModelIdentity{Selected: "a/b", Models: &session.ConversationModels{Main: "a/b"}}
}

// Contracts 3 and 4: Try again joins the same live session without a send or
// takeover. A subsequent explicit turn survives beyond the handshake budget.
func TestAppHostJoinRetryAndLongTurn(t *testing.T) {
	workspace, transcript := appJoinTestChat(t)
	appJoinTestBudget(t, 40*time.Millisecond)
	agent := &appJoinScriptedAgent{scriptedAgent: scriptedAgent{file: transcript}}
	sess := remote.NewSession(&remote.Engine{Agent: agent, Workspace: workspace, SessionFile: transcript}, true)
	t.Cleanup(func() { _ = sess.Close() })
	var ready atomic.Bool
	closed := make(chan struct{}, 1)
	b := newFakeBackend(newAppRecorder(), appOpenInProcess)
	b.profileDir = t.TempDir()
	t.Cleanup(func() { _ = b.Close() })
	appJoinTestHost(t, workspace, func(conn net.Conn) {
		if !ready.Load() {
			_, _ = io.Copy(io.Discard, conn)
			closed <- struct{}{}
			return
		}
		_ = remote.ServeAttach(conn, conn, remote.AttachOptions{
			Open: func(hello remote.Hello) (*remote.Session, error) {
				if !hello.Join || hello.New || hello.Session != transcript {
					return nil, fmt.Errorf("expected existing conversation join: %+v", hello)
				}
				return sess, nil
			},
		})
	})
	if _, err := b.OpenAgent(appAgentID(transcript)); err == nil {
		t.Fatal("silent host unexpectedly opened")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("failed open left a socket behind")
	}
	ready.Store(true)
	view, err := b.OpenAgent(appAgentID(transcript))
	if err != nil || view.ID != appAgentID(transcript) {
		t.Fatalf("Try again failed to open the same chat: %+v, %v", view, err)
	}
	agent.mu.Lock()
	sends, interrupts, closes := len(agent.said), agent.interrupts, agent.closes
	agent.mu.Unlock()
	if sends != 0 || interrupts != 0 || closes != 0 || !session.InUse(transcript) {
		t.Fatalf("open mutated the host: sends=%d interrupts=%d closes=%d", sends, interrupts, closes)
	}
	a, err := b.find(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	live := b.liveOf(a)
	if live.conn.Transcript != transcript || live.conn.Assembled {
		t.Fatal("retry did not join the original host conversation")
	}
	events, err := live.conn.Conv.Submit(context.Background(), "one deliberate turn")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("joined turn never started")
	}
	time.Sleep(2 * appHostJoinWait)
	agent.mu.Lock()
	turn := agent.turn
	turn <- session.Event{Kind: session.EventTextDelta, Text: "past the join budget"}
	close(turn)
	agent.turn = nil
	agent.mu.Unlock()
	seen, ended := false, false
	deadline := time.After(time.Second)
	for !ended {
		select {
		case event, ok := <-events:
			ended = !ok
			seen = seen || event.Text == "past the join budget"
		case <-deadline:
			t.Fatal("long turn did not finish after its producer closed")
		}
	}
	if !seen {
		t.Fatal("handshake deadline interrupted the long turn")
	}
	if _, err := b.OpenAgent(view.ID); err != nil {
		t.Fatalf("joined conversation failed after the budget: %v", err)
	}
}

// Contract 4: refusals, incompatible welcomes and truncated/closed welcomes
// return promptly without accumulating sockets or handshake goroutines.
func TestAppHostJoinFailedCandidatesClose(t *testing.T) {
	for _, mode := range []string{"refused", "incompatible", "partial", "eof"} {
		t.Run(mode, func(t *testing.T) {
			workspace, transcript := appJoinTestChat(t)
			appJoinTestBudget(t, 100*time.Millisecond)
			closed := make(chan struct{}, 32)
			appJoinTestHost(t, workspace, func(conn net.Conn) {
				var hello remote.Frame
				if json.NewDecoder(conn).Decode(&hello) != nil {
					return
				}
				switch mode {
				case "refused":
					_ = json.NewEncoder(conn).Encode(remote.Frame{Kind: "fatal", Error: "not held here"})
				case "incompatible":
					payload, _ := json.Marshal(remote.Welcome{Version: remote.Version + 1})
					_ = json.NewEncoder(conn).Encode(remote.Frame{Kind: "welcome", Payload: payload})
				case "partial":
					_, _ = io.WriteString(conn, `{"kind":"welcome","payload":`)
					_ = conn.Close()
				case "eof":
					_ = conn.Close()
				}
				_, _ = io.Copy(io.Discard, conn)
				closed <- struct{}{}
			})
			open := func() {
				t.Helper()
				started := time.Now()
				conn, err := appJoinExisting(appOpenAsk{Workspace: workspace, Transcript: transcript}, nil)
				if conn != nil {
					conn.Release()
					t.Fatal("a refused host was joined")
				}
				var wire *appwire.Error
				if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict {
					t.Fatalf("a prompt refusal lost its existing conflict: %v", err)
				}
				if elapsed := time.Since(started); elapsed >= appHostJoinWait {
					t.Fatalf("a failed welcome spent the whole budget: %s", elapsed)
				}
				for range 2 {
					select {
					case <-closed:
					case <-time.After(time.Second):
						t.Fatal("a failed welcome left its socket open")
					}
				}
			}
			open()
			fds, fdErr := os.ReadDir("/proc/self/fd")
			goroutines := runtime.NumGoroutine()
			for range 8 {
				open()
			}
			until := time.Now().Add(100 * time.Millisecond)
			for {
				after, err := os.ReadDir("/proc/self/fd")
				fdLeak := fdErr == nil && err == nil && len(after) > len(fds)+1
				goroutineLeak := runtime.NumGoroutine() > goroutines+2
				if !fdLeak && !goroutineLeak {
					break
				}
				if time.Now().After(until) {
					t.Fatalf("failed joins retained resources: fds %d -> %d; goroutines %d -> %d", len(fds), len(after), goroutines, runtime.NumGoroutine())
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

// Contracts 1 and 2: a starting host's remove-to-listen grace also shares the
// join deadline. An unheld stale socket still answers without that grace.
func TestAppHostJoinStartingHostSharesBudget(t *testing.T) {
	for _, held := range []bool{false, true} {
		t.Run(fmt.Sprintf("host-lock=%t", held), func(t *testing.T) {
			workspace, transcript := appJoinTestChat(t)
			appJoinTestBudget(t, 60*time.Millisecond)
			if _, err := enginehost.Dir(workspace); err != nil {
				t.Fatal(err)
			}
			path, err := enginehost.SocketPath(workspace)
			if err != nil {
				t.Fatal(err)
			}
			stale, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			stale.(*net.UnixListener).SetUnlinkOnClose(false)
			_ = stale.Close()
			lockPath, err := enginehost.LockPath(workspace)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if held {
				if err := filelock.Lock(file, true, true); err != nil {
					t.Fatal(err)
				}
			}
			started := time.Now()
			_, err = appJoinExisting(appOpenAsk{Workspace: workspace, Transcript: transcript}, nil)
			var wire *appwire.Error
			want := appwire.CodeConflict
			if held {
				want = appwire.CodeUnreachable
			}
			if !errors.As(err, &wire) || wire.Code != want {
				t.Fatalf("starting host error = %v, want %s", err, want)
			}
			limit := 100 * time.Millisecond
			if !held {
				limit = 40 * time.Millisecond
			}
			if elapsed := time.Since(started); elapsed > limit {
				t.Fatalf("starting/stale host exceeded its budget: %s", elapsed)
			}
		})
	}
}
