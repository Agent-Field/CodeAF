package remote

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type finalNoticeAgent struct {
	*fakeAgent
	mu    sync.Mutex
	lines []string
}

func (a *finalNoticeAgent) TakeSealNotice() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.lines) == 0 {
		return ""
	}
	line := a.lines[0]
	a.lines = a.lines[1:]
	return line
}

func TestFinalSavingReturnsItsNoticeToTheClosingWindowOnce(t *testing.T) {
	for _, displaced := range []bool{true, false} {
		t.Run(map[bool]string{true: "displaced", false: "clean"}[displaced], func(t *testing.T) {
			agent := &finalNoticeAgent{fakeAgent: &fakeAgent{}}
			loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
				return &Engine{Agent: agent, FinishSaving: func(WrappedAgent) error {
					if displaced {
						agent.note(chatlist.Superseded("desk"))
					}
					return nil
				}}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			if err := loop.Client.Agent().Close(); err != nil {
				t.Fatal(err)
			}
			// Both generations expose notices as an optional capability at the window boundary.
			source, ok := any(loop.Client.Agent()).(interface{ TakeSavingNotices() []string })
			var got []string
			if ok {
				got = source.TakeSavingNotices()
			}
			if displaced && (len(got) != 1 || got[0] != chatlist.Superseded("desk")) || !displaced && len(got) != 0 {
				t.Fatalf("closing receipt notices: %q", got)
			}
			if ok && len(source.TakeSavingNotices()) != 0 {
				t.Fatal("closing receipt repeated its notice")
			}
			if got := loop.Client.TakeNotice(); got != "" {
				t.Fatalf("final receipt also used the live notice queue: %s", got)
			}
			if err := loop.Client.Agent().Close(); err != nil {
				t.Fatal(err)
			}
			if ok && len(source.TakeSavingNotices()) != 0 {
				t.Fatal("repeated close repeated its notice")
			}
		})
	}
}

func TestSavingNoticeFieldPreservesBothWireGenerations(t *testing.T) {
	raw := []byte(`{"kind":"result","id":1,"localSaved":true,"savingNotices":["saved on a branch"]}`)
	var current Frame
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"savingNotices":["saved on a branch"]`) {
		t.Fatalf("new peer lost the optional field: %s", encoded)
	}
	var old struct {
		Kind       string `json:"kind"`
		ID         uint64 `json:"id"`
		LocalSaved bool   `json:"localSaved"`
	}
	if err := json.Unmarshal(encoded, &old); err != nil || !old.LocalSaved {
		t.Fatalf("old peer rejected the new receipt: %+v %v", old, err)
	}
	if err := json.Unmarshal([]byte(`{"kind":"result","id":1,"localSaved":true}`), &current); err != nil {
		t.Fatal(err)
	}
	// Decode into a fresh frame, as the wire reader does for every message.
	current = Frame{}
	if err := json.Unmarshal([]byte(`{"kind":"result","id":1,"localSaved":true}`), &current); err != nil {
		t.Fatal(err)
	}
	encoded, err = json.Marshal(current)
	if err != nil || strings.Contains(string(encoded), "savingNotices") {
		t.Fatalf("old engine acquired a new notice: %s %v", encoded, err)
	}
}

func (a *finalNoticeAgent) note(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lines = append(a.lines, line)
}

type blockedNoticeWriter struct {
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
	armed       atomic.Bool
	onlyNotices bool
	out         io.Writer
}

func (w *blockedNoticeWriter) Write(p []byte) (int, error) {
	if w.armed.Load() && (!w.onlyNotices || bytes.Contains(p, []byte(`"kind":"notice"`))) {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return w.out.Write(p)
}

func TestClosingAResponsiveWindowFinishesSavingWithABlockedPeer(t *testing.T) {
	for _, blockedKind := range []string{"facts", "notice"} {
		t.Run(blockedKind, func(t *testing.T) {
			agent := &finalNoticeAgent{fakeAgent: &fakeAgent{}}
			saved := make(chan struct{})
			sess := NewSession(&Engine{Agent: agent, FinishSaving: func(WrappedAgent) error { close(saved); return nil }}, true)
			attach := AttachOptions{Open: func(Hello) (*Session, error) { return sess, nil }}
			loop, err := loopOver(Hello{}, func(pipe io.ReadWriteCloser) error { return ServeAttach(pipe, pipe, attach) })
			if err != nil {
				t.Fatal(err)
			}
			defer loop.Close()
			blocked := &blockedNoticeWriter{entered: make(chan struct{}), release: make(chan struct{}), onlyNotices: blockedKind == "notice"}
			var release sync.Once
			unblock := func() { release.Do(func() { close(blocked.release) }) }
			defer unblock()
			peer, err := loopOver(Hello{}, func(pipe io.ReadWriteCloser) error { blocked.out = pipe; return ServeAttach(pipe, blocked, attach) })
			if err != nil {
				t.Fatal(err)
			}
			defer func() { unblock(); _ = peer.Close() }()
			// A completed call on each connection puts both handshakes behind the test.
			loop.Client.Recent()
			peer.Client.Recent()
			blocked.armed.Store(true)
			agent.note(chatlist.Superseded("desk"))
			select {
			case <-blocked.entered:
			case <-time.After(time.Second):
				t.Fatal("pump never reached blocked peer")
			}
			closed := make(chan error, 1)
			start := time.Now()
			go func() { closed <- loop.Client.Agent().Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2300 * time.Millisecond):
				unblock()
				<-closed
				t.Fatal("blocked peer prevented closing and saving within two seconds")
			}
			select {
			case <-saved:
			default:
				t.Fatal("close returned before final saving")
			}
			counts := func() [2]int {
				agent.fakeAgent.mu.Lock()
				defer agent.fakeAgent.mu.Unlock()
				return [2]int{agent.closes, agent.interrupts}
			}()
			if counts != [2]int{1, 1} {
				t.Fatalf("close and interrupt counts = %v, want [1 1]", counts)
			}
			if time.Since(start) > 2300*time.Millisecond {
				t.Fatal("close exceeded bound")
			}
			lines := loop.Client.Agent().TakeSavingNotices()
			if live := loop.Client.TakeNotice(); live != "" {
				lines = append(lines, live)
			}
			if len(lines) != 1 || lines[0] != chatlist.Superseded("desk") {
				t.Fatalf("responsive window got %q, want sentence once", lines)
			}
			unblock()
			if err := peer.Close(); err != nil {
				t.Fatal(err)
			}
			sess.pumps.Wait()
			if got := loop.Client.TakeNotice(); got != "" {
				t.Fatalf("late duplicate: %s", got)
			}
			if got := loop.Client.Agent().TakeSavingNotices(); len(got) != 0 {
				t.Fatalf("late receipt duplicate: %q", got)
			}
		})
	}
}

func TestSuccessiveSessionSwapsDeliverEveryFinalSentenceOnce(t *testing.T) {
	first := &finalNoticeAgent{fakeAgent: &fakeAgent{}}
	second := &finalNoticeAgent{fakeAgent: &fakeAgent{}}
	third := &fakeAgent{}
	loop, err := Loopback(Hello{}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{
			Agent: first, Fresh: func() (WrappedAgent, string, error) { return second, "/two.jsonl", nil },
			Open: func(string) (WrappedAgent, bool, error) { return third, true, nil },
			FinishSaving: func(a WrappedAgent) error {
				if a == first {
					first.note("first final sentence")
				}
				if a == second {
					second.note("second final sentence")
				}
				return nil
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if _, err := loop.Client.NewSession(); err != nil {
		t.Fatal(err)
	}
	if _, err := loop.Client.OpenSession("/three.jsonl"); err != nil {
		t.Fatal(err)
	}
	got := loop.Client.Agent().TakeSavingNotices()
	if strings.Join(got, "|") != "first final sentence|second final sentence" {
		t.Fatalf("swap receipts: %q", got)
	}
	if got := loop.Client.Agent().TakeSavingNotices(); len(got) != 0 {
		t.Fatalf("duplicate swap receipts: %q", got)
	}
}
