package tui3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

type railWireFrame struct {
	task uint64
	text string
}
type railWireWindow struct {
	*app
	frames chan railWireFrame
}

func (m *railWireWindow) Init() tea.Cmd { return nil }
func (m *railWireWindow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.app.Update(msg)
	shown := railWireFrame{text: plain(frame(m.app))}
	if m.roomOpen() {
		shown.task = m.room.id
	}
	select {
	case m.frames <- shown:
	default:
	}
	return m, cmd
}

// Coordinates come from the rendered frame, independently of railLineAt and
// railNodeAt. This catches disagreement between layout and the click geometry.
func railWirePoint(t *testing.T, shown string, label string, minimumX int, leading bool) (int, int) {
	t.Helper()
	for y, line := range strings.Split(shown, "\n") {
		for start := 0; start < len(line); {
			relative := strings.Index(line[start:], label)
			if relative < 0 {
				break
			}
			at := start + relative
			start = at + len(label)
			x := ansi.StringWidth(line[:at])
			if x < minimumX {
				continue
			}
			if leading {
				x -= 2
			}
			return x, y
		}
	}
	t.Fatalf("the visible frame has no %q:\n%s", label, shown)
	return 0, 0
}

// Drive the default local wire client through Bubble Tea's actual mouse decoder.
// The family has a decision, a running child and settled work. A second click
// switches rooms using the new frame's geometry, without returning to main first.
func TestRenderedChatSidebarClicksOpenTheExactLocalEngineTask(t *testing.T) {
	for _, width := range []int{100, 119, 120, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a, _ := localTaskRoomLab(t)
			a.width, a.height = width, 40
			a.taskUpdate(update(1, "Ship the streaming parser", session.TaskRunning, session.TaskNotice{}))
			a.taskUpdate(update(2, "Read the parser contract", session.TaskDone, session.TaskNotice{}))
			a.taskUpdate(update(3, "Write the tree walker", session.TaskRunning, session.TaskNotice{}))
			a.taskUpdate(update(4, "Check the public API", session.TaskUnverified, session.TaskNotice{}))
			railKinship(a, 1, 2, 3, 4)
			a.touch()
			railLeft := a.bodyWidth()
			firstX, firstY := railWirePoint(t, plain(frame(a)), "Ship the", railLeft, true)
			input, keyboard := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			model := &railWireWindow{app: a, frames: make(chan railWireFrame, 128)}
			program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithWindowSize(width, a.height), tea.WithoutSignalHandler())
			finished := make(chan error, 1)
			go func() { _, err := program.Run(); finished <- err }()
			t.Cleanup(func() {
				cancel()
				_ = keyboard.Close()
				_ = input.Close()
				select {
				case <-finished:
				case <-time.After(3 * time.Second):
					t.Error("terminal did not close")
				}
			})
			click := func(x, y int) {
				t.Helper()
				if _, err := fmt.Fprintf(keyboard, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x+1, y+1, x+1, y+1); err != nil {
					t.Fatal(err)
				}
			}
			awaitTask := func(id uint64) railWireFrame {
				t.Helper()
				deadline := time.NewTimer(5 * time.Second)
				defer deadline.Stop()
				var last railWireFrame
				for {
					select {
					case shown := <-model.frames:
						last = shown
						if shown.task == id && strings.Contains(shown.text, "Checking the parser now") {
							return shown
						}
					case err := <-finished:
						t.Fatalf("terminal exited: %v", err)
					case <-deadline.C:
						t.Fatalf("rendered sidebar click did not open task %d and its engine transcript; last task %d:\n%s", id, last.task, last.text)
					}
				}
			}
			click(firstX, firstY)
			opened := awaitTask(1)
			nextX, nextY := railWirePoint(t, opened.text, "Write the", railLeft, false)
			click(nextX, nextY)
			awaitTask(3)
		})
	}
}

func TestRepeatedSidebarClicksKeepTheTaskAndItsDraftOpen(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)
	opened := a.room
	if opened == nil {
		t.Fatal("task did not open")
	}
	a.input.setText("Keep this task's draft here")
	clickRail(t, a, 0)
	if a.room != opened || a.input.String() != "Keep this task's draft here" {
		t.Fatal("clicking the selected sidebar row closed or replaced the page and its draft")
	}
}

func TestGuestTaskDoesNotSelectTheLocalSidebarTaskWithTheSameNumber(t *testing.T) {
	a, _ := guestLab(t)
	enterAway(t, a)
	if a.roomStandingOn(a.tasks[7]) {
		t.Fatal("a foreign task selected this conversation's sidebar row")
	}
	clickRail(t, a, 0)
	if a.room == nil || a.roomIsGuest() || a.room.id != 7 {
		t.Fatal("clicking local task 7 from a guest task 7 did not switch owners")
	}
}
