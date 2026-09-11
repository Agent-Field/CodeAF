package tui3

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The default local window talks to an engine through the same wire as SSH,
// but deliberately has no host label. Exercise that actual client capability,
// rather than giving the window the in-process WatchTask interface.
type localTaskRoomEngine struct {
	*farAgent
	journal string
	mu      sync.Mutex
	steered []string
	ids     []uint64
}

func (f *localTaskRoomEngine) TaskJournal(uint64) string { return f.journal }

func (f *localTaskRoomEngine) SteerTask(id uint64, text string) (session.SteerReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steered = append(f.steered, text)
	f.ids = append(f.ids, id)
	return session.SteerReceipt{Landing: session.SteerDelivered(false)}, nil
}

func localTaskRoomLab(t *testing.T) (*app, *localTaskRoomEngine) {
	t.Helper()
	journal := roomJournal(t,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"Checking the parser now."}`,
	)
	engine := &localTaskRoomEngine{farAgent: newFarAgent(), journal: journal}
	a, _, _ := roomApp(t)
	loop, err := remote.Loopback(remote.Hello{Version: remote.Version}, remote.Options{
		Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: engine, SessionFile: a.file, Workspace: a.workspace, TaskRecord: func(_ string, _ int) (session.TaskRecord, error) {
				body, err := os.ReadFile(journal)
				return session.TaskRecord{Journal: body, Kept: true}, err
			}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	a.agent, a.host = loop.Client.Agent(), ""
	a.farRoomRecord = loop.Client.Agent().TaskRoom
	if _, local := a.roomDoors(); local {
		t.Fatal("the test accidentally supplied in-process room doors")
	}
	return a, engine
}

func TestTheLocalHostedTaskClickReadsSteersAndReturns(t *testing.T) {
	a, engine := localTaskRoomLab(t)
	clickRail(t, a, 0)
	if !a.roomOpen() {
		t.Fatalf("the default local window ignored the task click:\n%s", taskText(a))
	}
	if !strings.Contains(roomText(a), "Checking the parser now") {
		t.Fatalf("the clicked task did not show its engine transcript:\n%s", roomText(a))
	}
	a.input.setText("Keep the existing API compatible.")
	drive(t, a, key("enter"))
	engine.mu.Lock()
	got := append([]string(nil), engine.steered...)
	ids := append([]uint64(nil), engine.ids...)
	engine.mu.Unlock()
	if len(got) != 1 || len(ids) != 1 || ids[0] != 7 || got[0] != "Keep the existing API compatible." {
		t.Fatalf("the task did not receive the correction exactly once: %v", got)
	}
	if !strings.Contains(roomText(a), session.SteerDelivered(false)) {
		t.Fatalf("the task page gave no delivery feedback:\n%s", roomText(a))
	}
	a.room.entries[0].full = true
	// The journal can lag behind an acknowledged correction. A refresh must
	// not erase the person's words or close the instruction they just opened.
	msg := a.farRoomPoll(a.room.gen)().(roomRecordMsg)
	a.farRoomRead(msg)
	if !strings.Contains(roomText(a), "Keep the existing API compatible.") || !a.room.entries[0].full {
		t.Fatalf("refresh erased the correction or the reader's expansion:\n%s", roomText(a))
	}

	// Growth is also a refresh: keep the expanded brief and correction while
	// another assistant paragraph arrives, then replace the echo when journaled.
	body, err := os.ReadFile(engine.journal)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`{"type":"message","role":"assistant","content":"The parser check is complete."}`,
		`{"type":"message","role":"user","content":"Keep the existing API compatible.","steer":{"at":"2026-09-05T18:00:00Z","consumed":true,"landing":"delivered"}}`,
	} {
		body = append(body, []byte(line+"\n")...)
		if err := os.WriteFile(engine.journal, body, 0600); err != nil {
			t.Fatal(err)
		}
		a.farRoomRead(a.farRoomPoll(a.room.gen)().(roomRecordMsg))
		corrections := 0
		for _, e := range a.room.entries {
			if e.steer != nil && e.steer.words == "Keep the existing API compatible." {
				corrections++
			}
		}
		if corrections != 1 || !a.room.entries[0].full {
			t.Fatalf("refresh lost or duplicated the correction (%d), or closed the brief", corrections)
		}
	}
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("Escape did not return to the main conversation")
	}
}

// Exercise raw terminal mouse and keyboard bytes through Bubble Tea's input
// decoder and program loop, with the same wire client as the default window.
// The fixture supplies a roster already showing one task; no model is needed.
type localRoomTerminal struct {
	*app
	snapshots chan string
}

func (m *localRoomTerminal) Init() tea.Cmd { return nil }
func (m *localRoomTerminal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.app.Update(msg)
	state := "MAIN"
	if m.roomOpen() {
		state = "TASK\n" + roomText(m.app)
	}
	select {
	case m.snapshots <- state:
	default:
	}
	return m, cmd
}

func TestRawTerminalInputOpensAndSteersTheLocalEngineTask(t *testing.T) {
	a, engine := localTaskRoomLab(t)
	x, y := a.bodyWidth()+2, -1
	for row := a.bodyTop(); row < a.bodyTop()+a.viewHeight(); row++ {
		if n := a.railNodeAt(row); n != nil && n.id == 7 {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("the task has no clickable row")
	}
	input, keyboard := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	model := &localRoomTerminal{app: a, snapshots: make(chan string, 128)}
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithWindowSize(a.width, a.height), tea.WithoutSignalHandler())
	finished := make(chan error, 1)
	go func() { _, err := program.Run(); finished <- err }()
	t.Cleanup(func() {
		cancel()
		_ = keyboard.Close()
		_ = input.Close()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("terminal program did not stop")
		}
	})
	waitFor := func(want string) {
		t.Helper()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case state := <-model.snapshots:
				if strings.Contains(state, want) {
					return
				}
			case err := <-finished:
				t.Fatalf("terminal exited early: %v", err)
			case <-deadline.C:
				t.Fatalf("terminal never showed %q", want)
			}
		}
	}
	typeKeys := func(text string) {
		t.Helper()
		if _, err := io.WriteString(keyboard, text); err != nil {
			t.Fatal(err)
		}
	}
	typeKeys(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1))
	waitFor("Checking the parser now.")
	typeKeys("Keep the parser API.\r")
	waitFor("delivered")
	typeKeys("\x1b")
	waitFor("MAIN")
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.steered) != 1 || engine.steered[0] != "Keep the parser API." || engine.ids[0] != 7 {
		t.Fatalf("wrong task delivery: %v %v", engine.ids, engine.steered)
	}
}

func TestRepeatedTaskCorrectionsStayDistinctAsTheJournalCatchesUp(t *testing.T) {
	a, engine := localTaskRoomLab(t)
	clickRail(t, a, 0)
	for range 2 {
		a.input.setText("Keep the API.")
		drive(t, a, key("enter"))
	}
	body, err := os.ReadFile(engine.journal)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		body = append(body, []byte(fmt.Sprintf("{\"type\":\"message\",\"role\":\"user\",\"content\":\"Keep the API.\",\"steer\":{\"at\":\"2026-09-05T18:00:0%dZ\",\"consumed\":true}}\n", n))...)
		if err := os.WriteFile(engine.journal, body, 0600); err != nil {
			t.Fatal(err)
		}
		a.farRoomRead(a.farRoomPoll(a.room.gen)().(roomRecordMsg))
		count := 0
		for _, e := range a.room.entries {
			if e.steer != nil && e.steer.words == "Keep the API." {
				count++
			}
		}
		if count != 2 {
			t.Fatalf("after %d journaled corrections, the page shows %d instead of two", n, count)
		}
	}
}
