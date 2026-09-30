package tui3

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

// The gate holds only the first write. Both roads still apply every operation
// through a real session and store, so a forget racing a save loses real data.
type fifoMemorySession struct {
	*session.Agent
	entered, release chan struct{}
	saved            chan struct{}
	once             sync.Once
	mu               sync.Mutex
	applied          []string
}

func (s *fifoMemorySession) Remember(text string) (string, error) {
	if strings.HasPrefix(text, "cobalt") {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	title, err := s.Agent.Remember(text)
	s.mu.Lock()
	s.applied = append(s.applied, "remember "+text)
	s.mu.Unlock()
	if strings.HasPrefix(text, "cobalt") {
		close(s.saved)
	}
	return title, err
}

func (s *fifoMemorySession) Forget(query string) (string, error) {
	title, err := s.Agent.Forget(query)
	s.mu.Lock()
	s.applied = append(s.applied, "forget "+query)
	s.mu.Unlock()
	return title, err
}

func fifoMemoryApp(t *testing.T, hosted bool) (*app, *fifoMemorySession, func()) {
	t.Helper()
	brain, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = brain.Close() })
	far, err := session.New(session.Config{Workspace: t.TempDir(), Model: "stub/memory-fifo", APIKey: "fixture", BaseURL: "http://127.0.0.1:1/v1", System: "Test only.", PromptProfile: "full", Memory: brain})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = far.Close() })
	s := &fifoMemorySession{Agent: far, entered: make(chan struct{}), release: make(chan struct{}), saved: make(chan struct{})}
	var agent Agent = s
	if hosted {
		loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) { return &remote.Engine{Agent: s}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = loop.Close() })
		agent = loop.Client.Agent()
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(s.release) }) }
	t.Cleanup(release)
	return newTestApp(agent), s, release
}

func fifoMemoryReply(t *testing.T, replies <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case reply := <-replies:
		return reply
	case <-time.After(5 * time.Second):
		t.Fatal("queued memory receipt did not arrive")
		return nil
	}
}

func TestMemoryTwoSavesApplyAndReportInIssueOrder(t *testing.T) {
	testMemoryFIFO(t, "/remember amber dispatch beacon", "remembered ·", []string{"remember cobalt shipment fixture", "remember amber dispatch beacon"})
}

func TestMemorySaveThenForgetAppliesAndReportsInIssueOrder(t *testing.T) {
	testMemoryFIFO(t, "/forget cobalt", "forgot ·", []string{"remember cobalt shipment fixture", "forget cobalt"})
}

func TestMemoryIssueOrderSurvivesReversedCommandScheduling(t *testing.T) {
	a, s, release := fifoMemoryApp(t, false)
	first := a.slash("/remember cobalt shipment fixture")
	second := a.slash("/forget cobalt")
	secondReplies := make(chan tea.Msg, 1)
	go func() { secondReplies <- second() }()
	select {
	case reply := <-secondReplies:
		a.Update(reply)
		t.Fatal("scheduling the second command first applied a forget before its save")
	case <-time.After(100 * time.Millisecond):
	}
	firstReplies := make(chan tea.Msg, 1)
	go func() { firstReplies <- first() }()
	select {
	case <-s.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first-issued save did not start")
	}
	release()
	a.Update(fifoMemoryReply(t, firstReplies))
	a.Update(fifoMemoryReply(t, secondReplies))
	if got := plain(lastNote(t, a)); !strings.Contains(got, "forgot · cobalt shipment fixture") {
		t.Fatalf("later forget receipt = %q", got)
	}
}

func TestMemoryLateSaveKeepsForgetQueuedThroughItsGrace(t *testing.T) {
	a, s, release := fifoMemoryApp(t, true)
	first := a.slash("/remember cobalt shipment fixture")
	// Only the grace clock is controlled. The engine's ordinary wire deadline
	// really expires while the real session's write remains held by a channel.
	grace := make(chan time.Time, 1)
	graceStarted := make(chan time.Duration, 1)
	origin := memoryConversation{surface: a, agent: a.agent, file: a.file}
	memoryTails.Lock()
	firstTicket := memoryTails.tickets[origin]
	firstTicket.after = func(wait time.Duration) <-chan time.Time { graceStarted <- wait; return grace }
	memoryTails.Unlock()
	second := a.slash("/forget cobalt")
	firstReplies, secondReplies := make(chan tea.Msg, 1), make(chan tea.Msg, 1)
	go func() { firstReplies <- first() }()
	select {
	case <-s.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save did not reach the real engine")
	}
	go func() { secondReplies <- second() }()
	var firstReply tea.Msg
	select {
	case firstReply = <-firstReplies:
	case <-time.After(15 * time.Second):
		t.Fatal("real wire wait did not time out")
	}
	a.Update(firstReply)
	if got := plain(lastNote(t, a)); !strings.Contains(got, "saving that has not answered yet") {
		t.Fatalf("late save receipt = %q", got)
	}
	select {
	case wait := <-graceStarted:
		if wait != roles.PatienceFor(roles.RoleReflex) {
			t.Fatalf("grace = %v, want the memory decider's patience", wait)
		}
	default:
		t.Fatal("wire timeout did not arm a bounded grace")
	}
	select {
	case <-secondReplies:
		t.Fatal("forget overtook the engine's still-running late write")
	case <-time.After(100 * time.Millisecond):
	}
	a.Update(key("x"))
	if a.input.String() != "x" || !strings.Contains(plain(frame(a)), "x") {
		t.Fatal("late-save grace blocked typing or repaint")
	}
	release()
	select {
	case <-s.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not finish its late save")
	}
	// Older engines have discarded this call's waiting receipt. Expiring the
	// grace is therefore the bounded path that releases the queued operation.
	grace <- time.Time{}
	a.Update(fifoMemoryReply(t, secondReplies))
	if got := plain(lastNote(t, a)); !strings.Contains(got, "forgot · cobalt shipment fixture") {
		t.Fatalf("queued forget receipt = %q", got)
	}
	lines, err := s.Agent.Memories("cobalt")
	if err != nil || len(lines) != 0 {
		t.Fatalf("late save survived its later forget: %+v, %v", lines, err)
	}
	s.mu.Lock()
	got := append([]string(nil), s.applied...)
	s.mu.Unlock()
	if strings.Join(got, "\n") != "remember cobalt shipment fixture\nforget cobalt" {
		t.Fatalf("late operation order or retries = %q", got)
	}
	memoryTails.Lock()
	_, outstanding := memoryTails.tickets[origin]
	memoryTails.Unlock()
	if outstanding {
		t.Fatal("delivered final receipt retained its conversation queue")
	}
}

func testMemoryFIFO(t *testing.T, secondLine, secondReceipt string, want []string) {
	t.Helper()
	for _, hosted := range []bool{false, true} {
		road := "no-host"
		if hosted {
			road = "engine"
		}
		t.Run(road, func(t *testing.T) {
			a, s, release := fifoMemoryApp(t, hosted)
			first := a.slash("/remember cobalt shipment fixture")
			second := a.slash(secondLine)
			if first == nil || second == nil {
				t.Fatal("memory operations were not scheduled")
			}
			firstReplies, secondReplies := make(chan tea.Msg, 1), make(chan tea.Msg, 1)
			go func() { firstReplies <- first() }()
			select {
			case <-s.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first save did not reach its held write")
			}
			go func() { secondReplies <- second() }()
			select {
			case reply := <-secondReplies:
				release()
				a.Update(fifoMemoryReply(t, firstReplies))
				a.Update(reply)
				t.Fatal("later memory operation completed before the held first save")
			case <-time.After(100 * time.Millisecond):
			}
			a.Update(key("x"))
			if a.input.String() != "x" || !strings.Contains(plain(frame(a)), "x") {
				t.Fatal("the FIFO stopped typing or repaint")
			}
			release()
			firstReply := fifoMemoryReply(t, firstReplies)
			// Application alone cannot release the next operation: its receipt
			// might reach Update first and reverse the person's transcript.
			select {
			case <-secondReplies:
				t.Fatal("later receipt escaped before the first receipt was delivered")
			case <-time.After(100 * time.Millisecond):
			}
			a.Update(firstReply)
			if got := plain(lastNote(t, a)); !strings.Contains(got, "remembered · cobalt shipment fixture") {
				t.Fatalf("first receipt = %q", got)
			}
			a.Update(fifoMemoryReply(t, secondReplies))
			if got := plain(lastNote(t, a)); !strings.Contains(got, secondReceipt) {
				t.Fatalf("second receipt = %q", got)
			}
			s.mu.Lock()
			got := append([]string(nil), s.applied...)
			s.mu.Unlock()
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("application order = %q, want %q", got, want)
			}
			lines, err := s.Agent.Memories("cobalt")
			if err != nil || (secondReceipt == "forgot ·" && len(lines) != 0) || (secondReceipt == "remembered ·" && len(lines) != 1) {
				t.Fatalf("final saved cobalt notes = %+v, %v", lines, err)
			}
		})
	}
}
