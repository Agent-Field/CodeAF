package tui3

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// resizePaints counts the complete repaint messages without spending unrelated
// commands. Bubble Tea keeps that message's concrete type private.
func resizePaints(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	msg := cmd()
	if reflect.TypeOf(msg) == reflect.TypeOf(tea.ClearScreen()) {
		return 1
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var count int
		for _, child := range batch {
			count += resizePaints(child)
		}
		return count
	}
	return 0
}

// A DRAG OWES ONE COMPLETE REPAINT. The terminal may have reflowed cells the
// renderer still believes it owns, so a diff at the new width is insufficient.
// Repainting each intermediate size would charge a flicker for every step.
func TestAResizeSettlementAsksForTheWholeScreenOnce(t *testing.T) {
	a := hoverApp(t)
	a.View()
	for width := 80; width <= 160; width += 4 {
		_, cmd := a.route(tea.WindowSizeMsg{Width: width, Height: 45})
		if paints := resizePaints(cmd); paints != 0 {
			t.Fatalf("an intermediate size asked for %d complete repaints", paints)
		}
	}
	_, cmd := a.route(resizeSettledMsg{})
	if paints := resizePaints(cmd); paints != 1 {
		t.Fatalf("the settled frame asked for %d complete repaints, want one", paints)
	}
	_, cmd = a.route(resizeSettledMsg{})
	if paints := resizePaints(cmd); paints != 0 {
		t.Fatalf("a settlement already spent asked for %d more repaints", paints)
	}
}

// SETUP HAS NO TRANSCRIPT CACHE, but its words occupy real terminal cells.
// Its resize must receive the same settlement as the conversation beneath it.
func TestAResizedFirstRunSheetAsksForTheWholeScreen(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	a.width, a.height = 80, 24
	a.rows = nil
	a.View()
	if a.rows != nil {
		t.Fatal("the setup sheet unexpectedly built the conversation's row cache")
	}
	_, cmd := a.route(tea.WindowSizeMsg{Width: 160, Height: 45})
	if cmd == nil || !a.sizing {
		t.Fatal("the drawn setup sheet armed no resize settlement")
	}
	if paints := resizePaints(cmd); paints != 0 {
		t.Fatalf("setup repainted %d times before settlement", paints)
	}
	_, cmd = a.route(resizeSettledMsg{})
	if paints := resizePaints(cmd); paints != 1 {
		t.Fatalf("the resized setup sheet asked for %d repaints, want one", paints)
	}
}

// THE KEYBOARD NEVER WAITS FOR A DRAG. The repaint is a terminal command and
// must neither consume the typed words nor defer them behind the grace period.
func TestTypingDuringAResizeKeepsTheWholeDraft(t *testing.T) {
	a := hoverApp(t)
	a.View()
	a.route(tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, r := range "resize keeps my draft" {
		a.route(key(string(r)))
	}
	if got := a.input.String(); got != "resize keeps my draft" {
		t.Fatalf("before settlement the draft is %q", got)
	}
	_, cmd := a.route(resizeSettledMsg{})
	if paints := resizePaints(cmd); paints != 1 {
		t.Fatalf("the typed resize asked for %d repaints, want one", paints)
	}
	if got := a.input.String(); got != "resize keeps my draft" {
		t.Fatalf("after settlement the draft is %q", got)
	}
}

// A LONG DRAG STILL ENDS WITH ONE REPAINT. A grace tick that finds a newer
// size waits again instead of repainting while the window is still moving.
func TestAResizeThatOutlivesTheGraceRepaintsOnlyAfterItStops(t *testing.T) {
	a := hoverApp(t)
	a.View()
	_, tick := a.route(tea.WindowSizeMsg{Width: 80, Height: 24})
	if tick == nil {
		t.Fatal("the drag armed no settlement")
	}
	for _, width := range []int{100, 60, 160} {
		a.route(tea.WindowSizeMsg{Width: width, Height: 45})
		_, next := a.route(tick())
		if paints := resizePaints(next); paints != 0 {
			t.Fatalf("the moving window asked for %d repaints", paints)
		}
		if next == nil || !a.sizing {
			t.Fatal("the moving window stopped waiting for settlement")
		}
		tick = next
	}
	_, cmd := a.route(tick())
	if paints := resizePaints(cmd); paints != 1 || a.sizing {
		t.Fatalf("the stopped window asked for %d repaints, sizing=%v", paints, a.sizing)
	}
}

// resizeTerminal keeps the real renderer and substitutes only the grace clock.
// The test delivers settlement after the resized setup has physically painted,
// so the two frames cannot collapse into one renderer tick.
type resizeTerminal struct {
	*app
	cleared chan struct{}
}

func (m *resizeTerminal) Init() tea.Cmd { return nil }
func (m *resizeTerminal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.app.route(msg)
	if _, size := msg.(tea.WindowSizeMsg); size {
		return m, nil
	}
	if reflect.TypeOf(msg) == reflect.TypeOf(tea.ClearScreen()) {
		m.cleared <- struct{}{}
	}
	return m, cmd
}

// resizeOutput hands complete writes to the test, so waiting for a frame costs
// no sleep and reads no buffer while the renderer is writing it.
type resizeOutput struct{ writes chan []byte }

func (w resizeOutput) Write(p []byte) (int, error) {
	w.writes <- append([]byte(nil), p...)
	return len(p), nil
}

// THE REPAINT REACHES THE TERMINAL. Commands alone cannot prove that Bubble
// Tea clears physical cells before revealing a shorter greeting after setup.
func TestTheRealRendererClearsAResizedSetupBeforeTheGreeting(t *testing.T) {
	a, _, _ := setupApp(t, nil)
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.width, a.height = 80, 24
	model := &resizeTerminal{app: a, cleared: make(chan struct{}, 1)}
	output := resizeOutput{writes: make(chan []byte, 128)}
	ctx, cancel := context.WithCancel(t.Context())
	program := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(nil),
		tea.WithOutput(output), tea.WithWindowSize(80, 24), tea.WithoutSignalHandler(),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}))
	finished := make(chan error, 1)
	go func() { _, err := program.Run(); finished <- err }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Error("the renderer did not close")
		}
	})
	awaitText := func(needle string) []byte {
		t.Helper()
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		var outputBytes []byte
		for {
			select {
			case p := <-output.writes:
				outputBytes = append(outputBytes, p...)
				if bytes.Contains(outputBytes, []byte(needle)) {
					return outputBytes
				}
			case err := <-finished:
				t.Fatalf("the renderer ended before %q: %v", needle, err)
			case <-deadline.C:
				t.Fatalf("the renderer never painted %q: %q", needle, outputBytes)
			}
		}
	}
	awaitText("connect openrouter")
	program.Send(tea.WindowSizeMsg{Width: 160, Height: 45})
	awaitText("connect openrouter")
	program.Send(resizeSettledMsg{})
	select {
	case <-model.cleared:
	case <-time.After(3 * time.Second):
		t.Fatal("settlement did not reach the renderer's clear-screen door")
	}
	program.Send(key("esc"))
	paint := awaitText("Choose a starting point or type your request.")
	if !bytes.Contains(paint, []byte(ansi.EraseEntireScreen)) {
		t.Fatal("the greeting was painted without erasing the setup's physical cells")
	}
}
