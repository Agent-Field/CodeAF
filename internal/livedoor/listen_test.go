package livedoor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestAListenerServesAChatItHoldsAndRefusesOneItDoesNotAndWritesItsRegistryFile
// verifies contract V29: Listen removes stale registry files of dead PIDs,
// writes a 0600 registry JSON file with the current PID and a relative socket
// name containing no "/", welcomes a client that sends a Join hello for a held
// transcript, refuses an unknown transcript with a refusal sentence without
// booting anything, and removes both files on an idempotent Close.
func TestAListenerServesAChatItHoldsAndRefusesOneItDoesNotAndWritesItsRegistryFile(t *testing.T) {
	homeDir := shortTempDir(t)
	liveDir := filepath.Join(homeDir, "v3", "live")
	if err := os.MkdirAll(liveDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", liveDir, err)
	}

	// Seed a stale registry file of a dead PID before calling Listen.
	staleJSON := filepath.Join(liveDir, "999999.json")
	staleSock := filepath.Join(liveDir, "999999.sock")
	if err := os.WriteFile(staleJSON, []byte(`{"pid":999999,"socket":"999999.sock","build":"old"}`), 0o600); err != nil {
		t.Fatalf("WriteFile stale json failed: %v", err)
	}
	if err := os.WriteFile(staleSock, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile stale sock failed: %v", err)
	}

	heldTranscript := filepath.Join(homeDir, "v3", "projects", "p", "chat-1", "transcript.jsonl")
	heldSession := remote.NewSession(&remote.Engine{
		Agent:       &minimalWrappedAgent{title: "Held Chat"},
		Workspace:   "/work/p",
		SessionFile: heldTranscript,
	}, true)
	t.Cleanup(func() { _ = heldSession.Close() })

	var openCalls atomic.Int32
	tr := UnixTransport()
	ln, err := Listen(homeDir, func(transcript string) (*remote.Session, bool) {
		openCalls.Add(1)
		if filepath.Clean(transcript) == filepath.Clean(heldTranscript) {
			return heldSession, true
		}
		return nil, false
	}, tr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Stale file of the dead PID must have been removed by Listen.
	if _, err := os.Stat(staleJSON); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale registry file %q still exists after Listen (err=%v)", staleJSON, err)
	}

	// Verify the registry JSON file exists with mode 0600, holds the right PID,
	// and carries no "/" anywhere in its raw JSON bytes.
	jsonPath := ln.Path()
	info, err := os.Stat(jsonPath)
	if err != nil {
		t.Fatalf("Stat(%q) failed: %v", jsonPath, err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("registry JSON mode = %04o, want 0600", perm)
		}
	}
	rawJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", jsonPath, err)
	}
	if strings.Contains(string(rawJSON), "/") || strings.Contains(string(rawJSON), `\`) {
		t.Fatalf("registry JSON contains a path separator: %s", rawJSON)
	}

	var entry struct {
		PID    int    `json:"pid"`
		Socket string `json:"socket"`
		Build  string `json:"build"`
	}
	if err := json.Unmarshal(rawJSON, &entry); err != nil {
		t.Fatalf("Unmarshal registry JSON failed: %v", err)
	}
	if entry.PID != os.Getpid() {
		t.Fatalf("registry PID = %d, want %d", entry.PID, os.Getpid())
	}
	if entry.Socket == "" || filepath.IsAbs(entry.Socket) {
		t.Fatalf("registry socket = %q, want non-empty relative name", entry.Socket)
	}
	if strings.TrimSpace(entry.Build) == "" {
		t.Fatalf("registry build is empty in %s", rawJSON)
	}

	sockPath := filepath.Join(liveDir, entry.Socket)

	// 1. Dialing and sending a Join hello for the held transcript receives a welcome.
	conn1, err := tr.Dial(context.Background(), sockPath)
	if err != nil {
		t.Fatalf("Dial(%q) for held transcript failed: %v", sockPath, err)
	}
	client, err := remote.Dial(conn1, "desk", remote.Hello{
		Workspace: "/work/p",
		Session:   heldTranscript,
		Join:      true,
	})
	if err != nil {
		_ = conn1.Close()
		t.Fatalf("remote.Dial for held transcript failed: %v", err)
	}
	if got := client.Welcome().SessionFile; got != heldTranscript {
		_ = client.Close()
		t.Fatalf("Welcome().SessionFile = %q, want %q", got, heldTranscript)
	}
	if got := client.Agent().Title(); got != "Held Chat" {
		_ = client.Close()
		t.Fatalf("client.Agent().Title() = %q, want %q", got, "Held Chat")
	}
	_ = client.Close()

	// 2. Dialing and sending a Join hello for an unknown transcript gets a refusal
	// sentence ("that conversation is not open here") and never boots one.
	conn2, err := tr.Dial(context.Background(), sockPath)
	if err != nil {
		t.Fatalf("Dial(%q) for unknown transcript failed: %v", sockPath, err)
	}
	otherTranscript := filepath.Join(homeDir, "v3", "projects", "p", "chat-2", "transcript.jsonl")
	badClient, err := remote.Dial(conn2, "desk", remote.Hello{
		Workspace: "/work/p",
		Session:   otherTranscript,
		Join:      true,
	})
	if err == nil {
		_ = badClient.Close()
		t.Fatal("remote.Dial for unknown transcript succeeded, want refusal error")
	}
	if !strings.Contains(err.Error(), "that conversation is not open here") {
		t.Fatalf("refusal error = %q, want substring %q", err.Error(), "that conversation is not open here")
	}
	if got := openCalls.Load(); got != 2 {
		t.Fatalf("open callback called %d times, want 2", got)
	}

	// 3. Close removes both the JSON file and the socket file, and a second Close is a no-op.
	if err := ln.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	if _, err := os.Stat(jsonPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registry JSON %q still exists after Close (err=%v)", jsonPath, err)
	}
	if _, err := os.Stat(sockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket file %q still exists after Close (err=%v)", sockPath, err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
}

// TestCloseChatEndsEveryViewerOfThatChatAndNoOtherAndHoldsUnderASymlinkedSpelling
// verifies the phase-1 completion of lane C: when a holder lets one chat go,
// every connection that opened THAT chat ends (a viewer reads it as the holder
// having gone) and a connection to any other chat of the same holder is left
// alone, whichever spelling of the path either side used.
func TestCloseChatEndsEveryViewerOfThatChatAndNoOtherAndHoldsUnderASymlinkedSpelling(t *testing.T) {
	homeDir := shortTempDir(t)
	physical := filepath.Join(homeDir, "physical")
	if err := os.MkdirAll(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(homeDir, "linked")
	if err := os.Symlink(physical, linked); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
	chatA := filepath.Join(physical, "a.jsonl")
	chatB := filepath.Join(physical, "b.jsonl")
	held := func(file string) *remote.Session {
		sess := remote.NewSession(&remote.Engine{Agent: &minimalWrappedAgent{title: file}, Workspace: "/work/p", SessionFile: file}, true)
		t.Cleanup(func() { _ = sess.Close() })
		return sess
	}
	sessions := map[string]*remote.Session{chatKey(chatA): held(chatA), chatKey(chatB): held(chatB)}

	tr := UnixTransport()
	ln, err := Listen(homeDir, func(transcript string) (*remote.Session, bool) {
		sess, ok := sessions[chatKey(transcript)]
		return sess, ok
	}, tr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	sock := filepath.Join(homeDir, "v3", "live", strconv.Itoa(os.Getpid())+".sock")

	view := func(file string) *remote.Client {
		t.Helper()
		conn, err := tr.Dial(context.Background(), sock)
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		client, err := remote.Dial(conn, "laptop", remote.Hello{Workspace: "/work/p", Session: file, Join: true})
		if err != nil {
			_ = conn.Close()
			t.Fatalf("remote.Dial %s: %v", file, err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	// Two viewers of chat A and one of chat B. The viewers name the chat the way
	// the session runs it; it is the HOLDER'S call that is spelled through the
	// symlink below, because a holder may know its chat by another spelling than
	// the one a viewer's hello carried.
	a1 := view(chatA)
	a2 := view(chatA)
	b := view(chatB)

	if n := ln.CloseChat(filepath.Join(linked, "a.jsonl")); n != 2 {
		t.Fatalf("CloseChat ended %d connections, want the 2 viewers of that chat", n)
	}
	for name, client := range map[string]*remote.Client{"first viewer of A": a1, "second viewer of A": a2} {
		deadline := time.Now().Add(5 * time.Second)
		for client.Err() == nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if client.Err() == nil {
			t.Fatalf("%s was still connected after its chat was closed", name)
		}
	}
	if err := b.Err(); err != nil {
		t.Fatalf("a viewer of another chat was ended with the first: %v", err)
	}
	if _, err := b.Ping(); err != nil {
		t.Fatalf("the other chat's viewer cannot call: %v", err)
	}
	if n := ln.CloseChat(filepath.Join(physical, "unknown.jsonl")); n != 0 {
		t.Fatalf("CloseChat of a chat nobody opened ended %d connections", n)
	}
	if n := ln.CloseChat(""); n != 0 {
		t.Fatalf("CloseChat of nothing ended %d connections", n)
	}
}

// TestCloseAllChatsEndsTheViewersOfEveryChatAndLeavesTheListenerServing is what
// turning remote control off asks of the listener: every viewer of every chat
// ends, and the listener is still there to refuse a new one itself.
func TestCloseAllChatsEndsTheViewersOfEveryChatAndLeavesTheListenerServing(t *testing.T) {
	homeDir := shortTempDir(t)
	chats := []string{filepath.Join(homeDir, "a.jsonl"), filepath.Join(homeDir, "b.jsonl")}
	sessions := map[string]*remote.Session{}
	for _, file := range chats {
		sess := remote.NewSession(&remote.Engine{Agent: &minimalWrappedAgent{title: file}, Workspace: "/work/p", SessionFile: file}, true)
		t.Cleanup(func() { _ = sess.Close() })
		sessions[chatKey(file)] = sess
	}
	tr := UnixTransport()
	ln, err := Listen(homeDir, func(transcript string) (*remote.Session, bool) {
		sess, ok := sessions[chatKey(transcript)]
		return sess, ok
	}, tr)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	sock := filepath.Join(homeDir, "v3", "live", strconv.Itoa(os.Getpid())+".sock")
	var viewers []*remote.Client
	for _, file := range chats {
		conn, err := tr.Dial(context.Background(), sock)
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		client, err := remote.Dial(conn, "laptop", remote.Hello{Workspace: "/work/p", Session: file, Join: true})
		if err != nil {
			_ = conn.Close()
			t.Fatalf("remote.Dial %s: %v", file, err)
		}
		t.Cleanup(func() { _ = client.Close() })
		viewers = append(viewers, client)
	}

	if n := ln.CloseAllChats(); n != len(chats) {
		t.Fatalf("CloseAllChats ended %d connections, want the %d viewers", n, len(chats))
	}
	for i, client := range viewers {
		deadline := time.Now().Add(5 * time.Second)
		for client.Err() == nil && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if client.Err() == nil {
			t.Fatalf("viewer %d was still connected after every chat was closed", i)
		}
	}
	if n := ln.CloseAllChats(); n != 0 {
		t.Fatalf("a second CloseAllChats ended %d connections, want none left", n)
	}
	conn, err := tr.Dial(context.Background(), sock)
	if err != nil {
		t.Fatalf("the listener stopped serving after CloseAllChats: %v", err)
	}
	_ = conn.Close()
}

// minimalWrappedAgent is the smallest implementation of remote.WrappedAgent
// needed to back a real remote.Session in package livedoor's tests.
type minimalWrappedAgent struct {
	title string
}

func (m *minimalWrappedAgent) Submit(context.Context, string) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (m *minimalWrappedAgent) SubmitImage(context.Context, string, []session.Image) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (m *minimalWrappedAgent) FollowUp(string) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (m *minimalWrappedAgent) Steer(string) (<-chan session.Event, error) {
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (m *minimalWrappedAgent) Interrupt()                    {}
func (m *minimalWrappedAgent) InterruptFor(session.StopDoor) {}
func (m *minimalWrappedAgent) Compact(context.Context) error { return nil }
func (m *minimalWrappedAgent) Close() error                  { return nil }
func (m *minimalWrappedAgent) Model() string                 { return "stub-model" }
func (m *minimalWrappedAgent) SetModel(string)               {}
func (m *minimalWrappedAgent) SetContextWindow(int)          {}
func (m *minimalWrappedAgent) ReasoningFor(string) string    { return "" }
func (m *minimalWrappedAgent) ReasoningLevels() map[string]string {
	return nil
}
func (m *minimalWrappedAgent) SetReasoningFor(string, string)                            {}
func (m *minimalWrappedAgent) ResolveConsent(uint64, bool)                               {}
func (m *minimalWrappedAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}
func (m *minimalWrappedAgent) ResolveHarness(uint64, bool, string)                       {}
func (m *minimalWrappedAgent) ResolveConnect(string, bool)                               {}
func (m *minimalWrappedAgent) ResolveConnectKey(string, string)                          {}
func (m *minimalWrappedAgent) NoteConnected(string, string)                              {}
func (m *minimalWrappedAgent) Title() string                                             { return m.title }
func (m *minimalWrappedAgent) Usage() session.Usage                                      { return session.Usage{} }
func (m *minimalWrappedAgent) ContextTokens() int                                        { return 0 }
func (m *minimalWrappedAgent) Transcript() []session.DisplayEntry                        { return nil }
func (m *minimalWrappedAgent) EarlierHistory() session.EarlierHistory {
	return session.EarlierHistory{}
}
func (m *minimalWrappedAgent) RewindPoints() []session.RewindPoint          { return nil }
func (m *minimalWrappedAgent) RewindAt(int) ([]session.DisplayEntry, error) { return nil, nil }
