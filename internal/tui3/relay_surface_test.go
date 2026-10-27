package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Each fixture closes the workers its gestures start before releasing its files.
func keepSurfaceWorkersInTest(t *testing.T, a *app) {
	t.Helper()
	t.Cleanup(func() {
		if !a.quitting {
			a.leaveEverything()
		}
		a.doorLine.close()
	})
	a.closingStarted = func(done <-chan struct{}) { t.Cleanup(func() { <-done }) }
}

// B1 keeps every edit with the opening chat and leaves an early send unsent.
func TestNewOpeningKeepsTypingPasteAndEarlyEnter(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "open", true: "cancel"}[cancel], func(t *testing.T) {
			a := openingLab(t)
			keepSurfaceWorkersInTest(t, a)
			a.closeHome()
			old := a.agent.(*fakeAgent)
			next := &fakeAgent{model: "m"}
			release := make(chan struct{})
			defer close(release)
			a.start = func(string) (Conversation, error) {
				<-release
				return Conversation{Agent: next, SessionFile: "/next/transcript.jsonl"}, nil
			}
			a.input.setText("/new")
			_, pending := a.Update(key("enter"))
			for _, msg := range []tea.Msg{key("a"), key("b"), key("backspace"), tea.PasteMsg{Content: " new chat"}, key("enter")} {
				a.Update(msg)
			}
			if !a.conversationOpening || len(old.sent) != 0 {
				t.Fatalf("opening=%v old sends=%v", a.conversationOpening, old.sent)
			}
			if cancel {
				a.Update(key("esc"))
				if a.input.String() != "a new chat" || !strings.Contains(plain(frame(a)), "opening cancelled; your words are back in this chat, unsent") {
					t.Fatalf("cancel lost its words or explanation: %q\n%s", a.input.String(), plain(frame(a)))
				}
			} else {
				release <- struct{}{}
				spend(t, a, pending)
				if a.input.String() != "a new chat" || len(next.sent) != 0 || a.agent != next {
					t.Fatalf("new composer=%q sends=%v", a.input.String(), next.sent)
				}
			}
		})
	}
}

// B1 keeps a paste that starts before arrival and ends after the chat opens.
func TestNewOpeningKeepsAPasteAcrossArrival(t *testing.T) {
	a := openingLab(t)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	next := &fakeAgent{model: "m"}
	release := make(chan struct{})
	defer close(release)
	a.start = func(string) (Conversation, error) {
		<-release
		return Conversation{Agent: next, SessionFile: "/next/transcript.jsonl"}, nil
	}
	a.input.setText("/new")
	_, cmd := a.Update(key("enter"))
	a.Update(tea.PasteStartMsg{})
	a.Update(tea.PasteMsg{Content: "new chat words"})
	release <- struct{}{}
	spend(t, a, cmd)
	a.Update(tea.PasteEndMsg{})
	if got := a.pastesUnfolded(a.input.String()); got != "new chat words" || len(next.sent) != 0 {
		t.Fatalf("arrived paste=%q sends=%v", got, next.sent)
	}
}

// B2 preserves the project label when a removed computer revisits a chat.
func TestRemovedComputerReopensWithTheSameProjectLabel(t *testing.T) {
	for _, copy := range []bool{false, true} {
		t.Run(map[bool]string{false: "project", true: "copy"}[copy], func(t *testing.T) {
			work := t.TempDir()
			want := work
			if copy {
				work = movedFolder(t, `{"id":"x","title":"fix it","origin":"/srv/other/ws9"}`)
				want = "ws9 (copy from another machine)"
			}
			a := newTestApp(&fakeAgent{model: "m"})
			keepSurfaceWorkersInTest(t, a)
			a.file = "/tmp/old/transcript.jsonl"
			spend(t, a, a.takeBeside(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/copy/transcript.jsonl", Workspace: work}))
			a.title = "Copy chat"
			spend(t, a, a.takeBeside(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/next/transcript.jsonl"}))
			a.title = "Next chat"
			a.Update(homeMachinesMsg{err: directory.ErrRevoked})
			a.width = 160
			a.touch()
			a.tabsRow(a.width)
			span := tabSpanFor(t, a, "Copy chat")
			drive(t, a, tea.MouseClickMsg{X: span.from + 1, Y: tabStripRow, Button: tea.MouseLeft})
			if got := plain(a.hintRow(a.width)); !strings.Contains(got, "project: "+want) {
				t.Fatalf("project label changed: %s", got)
			}
		})
	}
}

// B3 excludes the local holder even when stale records deny local ownership.
func TestResumeNeverOffersThisComputersOwnDevice(t *testing.T) {
	for _, status := range []chatlist.Status{chatlist.Running, chatlist.Off, chatlist.Idle} {
		a := leftOffApp(&fakeTaker{})
		keepSurfaceWorkersInTest(t, a)
		a.deviceID = "self"
		a.Update(homeMachinesMsg{rows: []chatlist.Row{{Cell: "chat", DeviceID: "self", Device: "laptop", Status: status}}})
		if got := leftOffFrame(a); strings.Contains(got, "Continue laptop's latest chat here?") {
			t.Fatalf("offered this computer for %s: %s", status, got)
		}
	}
	a := leftOffApp(&fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.Update(homeMachinesMsg{rows: []chatlist.Row{{Cell: "chat", DeviceID: "self", Device: "laptop", Status: chatlist.Off}}})
	a.Update(rosterMsg{devices: []chatlist.Device{{ID: "self", Name: "laptop", Self: true}}})
	if got := leftOffFrame(a); strings.Contains(got, "Continue laptop's latest chat here?") {
		t.Fatalf("the self-device reading left its stale offer: %s", got)
	}
}

type openingMachineSource struct {
	ready chan struct{}
	row   chatlist.Row
}

func (s *openingMachineSource) Rows(context.Context) ([]chatlist.Row, error) {
	return nil, errors.New("display has not read yet")
}

func (s *openingMachineSource) RowsForSend(ctx context.Context) ([]chatlist.Row, uint64, error) {
	select {
	case <-s.ready:
		return []chatlist.Row{s.row}, 1, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

// B4 waits for an actual reading before the first send decides the move card.
func TestFirstEnterWaitsForOwnershipReading(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	keepSurfaceWorkersInTest(t, a)
	a.file = "/tmp/chat/transcript.jsonl"
	a.taker = &fakeTaker{}
	src := &openingMachineSource{ready: make(chan struct{}), row: chatlist.Row{Cell: "chat", DeviceID: "other", Device: "tower", Status: chatlist.Running, Fence: 3}}
	a.machines = src
	a.input.setText("keep these words")
	_, cmd := a.Update(key("enter"))
	close(src.ready)
	spend(t, a, cmd)
	if got := plain(frame(a)); strings.Contains(got, chatlist.Unreachable) || !strings.Contains(got, "Continue this chat here?") {
		t.Fatalf("first send did not raise the card: %s", got)
	}
	if a.input.String() != "keep these words" {
		t.Fatalf("draft changed to %q", a.input.String())
	}
}

type renewalAgent struct {
	*fakeAgent
	since time.Time
	held  bool
}

func (a *renewalAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return a.since, 10 * time.Second, a.held
}

func (a *renewalAgent) RenewalChanges() <-chan struct{} { return nil }

// Contract D1 says a sustained outage and recovery once through the real agent.
func TestHolderReportsRenewalOutageAndRecovery(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	source := &renewalAgent{fakeAgent: &fakeAgent{model: "m"}, held: true}
	agent, err := session.New(session.Config{
		Workspace: t.TempDir(), Model: "test/renewal", APIKey: "fixture",
		BaseURL: "http://127.0.0.1:1/v1", System: "Test only", Renewals: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := newTestApp(agent)
	keepSurfaceWorkersInTest(t, a)
	now := time.Now()
	a.clock = func() time.Time { return now }
	source.since = now
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	if got := plain(frame(a)); strings.Contains(got, "this chat is not syncing") {
		t.Fatalf("a brief renewal failure was announced: %s", got)
	}
	now = now.Add(11 * time.Second)
	for range 3 {
		a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	}
	outage := "this chat is not syncing because other machines are unreachable; it keeps working here"
	if got := plain(frame(a)); strings.Count(got, outage) != 1 {
		t.Fatalf("outage notice count is wrong: %s", got)
	}
	source.since = time.Time{}
	for range 3 {
		a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	}
	if got := plain(frame(a)); strings.Count(got, "this chat is syncing again") != 1 {
		t.Fatalf("recovery notice count is wrong: %s", got)
	}
}

type blockedCloseAgent struct {
	*fakeAgent
	release chan struct{}
	entered chan struct{}
}

func (a *blockedCloseAgent) Close() error {
	if a.entered != nil {
		close(a.entered)
	}
	<-a.release
	return nil
}

// B7 returns from the close key before an unreachable engine answers.
func TestClosingChatNeverBlocksTheUpdateLoop(t *testing.T) {
	agent := &blockedCloseAgent{fakeAgent: &fakeAgent{model: "m"}, release: make(chan struct{}), entered: make(chan struct{})}
	a := newTestApp(agent)
	keepSurfaceWorkersInTest(t, a)
	a.file = "/tmp/closing/transcript.jsonl"
	keepThree(t, a)
	a.input.setText("/quit")
	returned := make(chan tea.Cmd, 1)
	go func() { _, cmd := a.Update(key("enter")); returned <- cmd }()
	var cmd tea.Cmd
	select {
	case cmd = <-returned:
		defer close(agent.release)
	case <-time.After(500 * time.Millisecond):
		close(agent.release)
		<-returned
		t.Fatal("closing held the update loop while the engine was unreachable")
	}
	finished := make(chan []tea.Msg, 1)
	go func() { finished <- runCmd(cmd) }()
	select {
	case <-agent.entered:
	case <-time.After(time.Second):
		t.Fatal("close never started off the update loop")
	}
	a.input.setText("another chat's draft")
	a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	a.Update(key("x"))
	a.Update(key("enter"))
	if a.input.String() != "another chat's draft" || len(a.agent.(*fakeAgent).sent) != 0 {
		t.Fatal("keys during close reached another chat's draft")
	}
	select {
	case msgs := <-finished:
		for _, msg := range msgs {
			a.Update(msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("close command has no bound")
	}
	if a.conversationClosing {
		t.Fatal("close never gave the keyboard back")
	}
}

// B7 also keeps the loop responsive when the last chat closes the window.
func TestQuittingLastChatNeverBlocksTheUpdateLoop(t *testing.T) {
	agent := &blockedCloseAgent{fakeAgent: &fakeAgent{model: "m"}, release: make(chan struct{}), entered: make(chan struct{})}
	a := newTestApp(agent)
	keepSurfaceWorkersInTest(t, a)
	a.input.setText("/quit")
	returned := make(chan tea.Cmd, 1)
	go func() { _, cmd := a.Update(key("enter")); returned <- cmd }()
	select {
	case cmd := <-returned:
		defer close(agent.release)
		finished := make(chan tea.Msg, 1)
		go func() { finished <- cmd() }()
		select {
		case <-agent.entered:
		case <-time.After(time.Second):
			t.Fatal("last chat never started closing")
		}
		a.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
		select {
		case msg := <-finished:
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Fatalf("quit returned %T", msg)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("last chat close has no bound")
		}
	case <-time.After(500 * time.Millisecond):
		close(agent.release)
		<-returned
		t.Fatal("last chat close held the update loop")
	}
}

// Contract D3 keeps a second interrupt available while a close is stuck.
func TestSecondCtrlCLeavesWhileQuitIsStillClosing(t *testing.T) {
	agent := &blockedCloseAgent{fakeAgent: &fakeAgent{model: "m"}, release: make(chan struct{}), entered: make(chan struct{})}
	a := newTestApp(agent)
	keepSurfaceWorkersInTest(t, a)
	forced := false
	a.forceQuit = func() { forced = true }
	_, first := a.Update(key("ctrl+c"))
	if first == nil {
		t.Fatal("the first interrupt did not start quitting")
	}
	finished := make(chan tea.Msg, 1)
	go func() { finished <- first() }()
	defer func() { close(agent.release); <-finished }()
	select {
	case <-agent.entered:
	case <-time.After(time.Second):
		t.Fatal("quit never started closing")
	}
	_, second := a.Update(key("ctrl+c"))
	if second == nil {
		t.Fatal("quitting swallowed the second interrupt")
	}
	if msg := second(); msg == nil {
		t.Fatal("the second interrupt did not end the surface")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("second interrupt returned %T", msg)
	}
	if !forced {
		t.Fatal("the second interrupt did not release process cleanup")
	}
}
