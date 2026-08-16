package tui2

import (
	"bytes"
	"context"
	"image"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func sized(t *testing.T, w, h int, opts Options) *Shell {
	t.Helper()
	s := NewShell(opts)
	s.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return s
}

// A resize storm is one layout, not forty. The tick is armed once and the size
// it settles on is the last one the terminal claimed.
func TestShellCoalescesAResizeStorm(t *testing.T) {
	s := sized(t, 80, 24, Options{})
	if s.width != 80 {
		t.Fatalf("the first size should apply at once, got %d", s.width)
	}

	armed := 0
	for w := 81; w <= 120; w++ {
		_, cmd := s.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		if cmd != nil {
			armed++
		}
	}
	if armed != 1 {
		t.Fatalf("a 40-event storm armed %d ticks, want 1", armed)
	}
	if s.width != 80 {
		t.Fatalf("layout moved before the storm settled: %d", s.width)
	}

	s.Update(resizeSettledMsg{epoch: s.resizeEpoch})
	if s.width != 120 {
		t.Fatalf("settled at %d, want the last claimed size 120", s.width)
	}
	if s.resizeArmed {
		t.Fatal("tick should be disarmed after it fires")
	}

	// A tick that outlived its reason changes nothing.
	s.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	s.Update(resizeSettledMsg{epoch: s.resizeEpoch - 1})
	if s.width != 120 {
		t.Fatalf("a stale tick applied a size: %d", s.width)
	}
}

func TestShellIgnoresANoOpResize(t *testing.T) {
	s := sized(t, 80, 24, Options{})
	if _, cmd := s.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Fatal("a resize to the same size should arm nothing")
	}
}

// The frame is recomputed when a fact moved, and not otherwise. A second look
// at an unchanged shell must cost nothing at all — that is what makes the
// bytes on the wire proportional to what changed.
func TestShellRepaintsOnlyWhenAFactMoved(t *testing.T) {
	s := sized(t, 100, 30, Options{})
	first := s.Frame(100, 30)
	if first == "" {
		t.Fatal("a sized shell rendered nothing")
	}
	if allocs := testing.AllocsPerRun(100, func() { s.Frame(100, 30) }); allocs != 0 {
		t.Fatalf("an unchanged frame allocated %v times per run", allocs)
	}
	if second := s.Frame(100, 30); second != first {
		t.Fatal("an unchanged shell produced a different frame")
	}

	s.Invalidate()
	if third := s.Frame(100, 30); third != first {
		t.Fatal("invalidation changed the picture without a fact changing")
	}
}

// Every size a terminal can claim, including the ones only a dragged corner
// produces. Nothing here may panic, and no frame may escape its box.
func TestShellSurvivesEverySize(t *testing.T) {
	for _, opts := range []Options{{}, {Linear: true}} {
		for w := 0; w <= 130; w++ {
			for h := 0; h <= 26; h++ {
				s := NewShell(opts)
				s.Update(tea.WindowSizeMsg{Width: w, Height: h})
				frame := s.Frame(w, h)
				if w <= 0 || h <= 0 {
					if frame != "" {
						t.Fatalf("%dx%d rendered %q", w, h, frame)
					}
					continue
				}
				lines := strings.Split(frame, "\n")
				if len(lines) > h {
					t.Fatalf("%dx%d produced %d rows", w, h, len(lines))
				}
				for i, line := range lines {
					if got := ansi.StringWidth(line); got > w {
						t.Fatalf("%dx%d row %d is %d cells wide: %q", w, h, i, got, line)
					}
				}
				// And the whole surface stays operable at that size.
				s.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
				s.Update(tea.MouseClickMsg{Button: tea.MouseLeft})
				s.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: w - 1, Y: h - 1})
				s.Frame(w, h)
			}
		}
	}
}

// What the skeleton actually shows: four regions, each naming itself, its
// allotment and the package that will fill it.
func TestShellDrawsEveryRegionItLaysOut(t *testing.T) {
	s := sized(t, 120, 30, Options{DB: "/tmp/graph.db", Session: "new"})
	frame := s.Frame(120, 30)
	for _, want := range []string{
		"transcript", "rail", "composer",
		"blocks: committed-prefix message parts",
		"blocks: scope map",
		"aforge v2", "wide", "120x30",
		"db /tmp/graph.db", "session new",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the frame never says %q:\n%s", want, frame)
		}
	}
	// Missing plumbing renders as an em dash, never as an invented default.
	bare := sized(t, 120, 30, Options{})
	if !strings.Contains(bare.Frame(120, 30), "session —") {
		t.Fatal("an unset session should render as a dash")
	}
}

// One column by one row is a real terminal, briefly, on the way to a bigger
// one. It shows the line that says what this is.
func TestShellAtOneByOne(t *testing.T) {
	s := sized(t, 1, 1, Options{})
	frame := s.Frame(1, 1)
	if strings.Contains(frame, "\n") {
		t.Fatalf("1x1 frame has more than one row: %q", frame)
	}
	if ansi.StringWidth(frame) > 1 {
		t.Fatalf("1x1 frame is %d cells: %q", ansi.StringWidth(frame), frame)
	}
}

func TestShellClickFocusesThePaneUnderThePointer(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	if s.focus != LayerTranscript {
		t.Fatalf("initial focus = %v", s.focus)
	}

	rail, ok := s.comp.rect(LayerRail)
	if !ok {
		t.Fatal("a 120-column frame should carry a rail")
	}
	s.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: rail.Min.X + 2, Y: rail.Min.Y + 3})
	if s.focus != LayerRail {
		t.Fatalf("focus after clicking the rail = %v", s.focus)
	}
	if !s.hitOK || s.hitID != LayerRail || s.hitAt != image.Pt(2, 3) {
		t.Fatalf("hit record = %v %v %v", s.hitID, s.hitAt, s.hitOK)
	}

	// Chrome does not take the conversation.
	status, _ := s.comp.rect(LayerStatus)
	s.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: status.Min.Y})
	if s.focus != LayerRail {
		t.Fatalf("clicking the status line moved focus to %v", s.focus)
	}
}

func TestShellRoutesMouseToThePaneInPaneLocalCoordinates(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	var got image.Point
	var hits int
	s.SetPane(LayerRail, &recordingPane{onMouse: func(p image.Point) { got, hits = p, hits+1 }})

	rail, _ := s.comp.rect(LayerRail)
	s.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: rail.Min.X + 5, Y: rail.Min.Y + 7})
	if hits != 1 {
		t.Fatalf("pane saw %d mouse events", hits)
	}
	if got != image.Pt(5, 7) {
		t.Fatalf("pane was handed %v, want (5,7)", got)
	}
}

func TestShellKeysReachTheFocusedPane(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	pane := &recordingPane{}
	s.SetPane(LayerTranscript, pane)
	s.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if pane.keys != 1 {
		t.Fatalf("focused pane saw %d keys", pane.keys)
	}

	// ctrl+c belongs to the shell and never reaches a pane.
	_, cmd := s.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if pane.keys != 1 {
		t.Fatal("ctrl+c leaked into a pane")
	}
}

// 5.15, made structural: scope is the same layer at every width, so a pane
// bound to it is reached by the same id, the same keys and the same clicks
// whether it is drawn as a column or as the whole pane.
func TestShellScopeIsTheSameObjectAtEveryWidth(t *testing.T) {
	wide := sized(t, 120, 30, Options{})
	if _, ok := wide.comp.rect(LayerRail); !ok {
		t.Fatal("wide frame has no rail")
	}

	narrow := sized(t, 70, 30, Options{})
	if handle, ok := narrow.comp.rect(LayerRail); ok && handle.Dx() > DefaultMetrics().RailSlimWidth {
		// The HANDLE may stand here before scope is asked for — it is the door,
		// one column wide. A rail wider than that would be the column arriving
		// below its breakpoint.
		t.Fatalf("narrow frame drew a %d-column rail before scope was asked for", handle.Dx())
	}
	narrow.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	rail, ok := narrow.comp.rect(LayerRail)
	if !ok {
		t.Fatal("narrow frame could not reach scope at all")
	}
	if rail.Min.X != 0 {
		t.Fatalf("narrow scope should own the main column, got %v", rail)
	}
	if _, ok := narrow.comp.rect(LayerTranscript); ok {
		t.Fatal("narrow scope should replace the transcript, not split it")
	}
}

func TestShellFocusFollowsAPaneOffTheScreen(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	rail, _ := s.comp.rect(LayerRail)
	s.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: rail.Min.X + 1, Y: 1})
	if s.focus != LayerRail {
		t.Fatalf("focus = %v", s.focus)
	}

	// Narrow the window past the breakpoint: the rail is gone, and focus must
	// land on something that is actually drawn.
	s.Update(tea.WindowSizeMsg{Width: 70, Height: 30})
	s.Update(resizeSettledMsg{epoch: s.resizeEpoch})
	if _, ok := s.comp.rect(s.focus); !ok {
		t.Fatalf("focus %v is not on screen", s.focus)
	}
}

// Linear accessible mode (10.1.5): one column at any width, and motion off.
func TestShellLinearModeIsSingleColumnAndStill(t *testing.T) {
	s := sized(t, 160, 40, Options{Linear: true})
	if s.Motion() {
		t.Fatal("linear mode still allows motion")
	}
	if _, ok := s.comp.rect(LayerRail); ok {
		t.Fatal("linear mode drew a rail")
	}
	transcript, ok := s.comp.rect(LayerTranscript)
	if !ok {
		t.Fatal("linear mode lost the transcript")
	}
	if transcript.Min.X != 0 || transcript.Max.X != 160 {
		t.Fatalf("linear transcript is not full width: %v", transcript)
	}
	frame := s.Frame(160, 40)
	if !strings.Contains(frame, "linear") {
		t.Fatal("the status line does not say the surface is linear")
	}
	// No drawn frames: a box is a picture of a boundary and a hundred and forty
	// punctuation marks to a screen reader. §16 BORDERS has since banned the
	// box from the ordinary rendering too — TestNoRegionDrawsABox holds both
	// modes to it — so this is no longer what separates linear from wide.
	for _, glyph := range []string{"┌", "┐", "└", "┘", "│", "─"} {
		if strings.Contains(frame, glyph) {
			t.Fatalf("linear mode drew box art (%q):\n%s", glyph, frame)
		}
	}
	if !strings.Contains(frame, "transcript") {
		t.Fatal("linear mode lost the region heading")
	}
	// What separates them now is the centring: the ordinary rendering floats a
	// region's words in the middle of its rectangle, and the accessible mode
	// keeps them at the left edge, where leading spaces are not read out as
	// nothing. So this is still a mode rather than a deletion.
	for _, row := range strings.Split(frame, "\n") {
		if strings.HasPrefix(row, " ") && strings.TrimSpace(row) != "" {
			t.Fatalf("linear mode indented a row:\n%q", row)
		}
	}
	ordinary := sized(t, 160, 40, Options{}).Frame(160, 40)
	if !strings.Contains(ordinary, "    transcript") {
		t.Fatalf("the ordinary rendering stopped centring its regions:\n%s", ordinary)
	}
}

// The truncation law's structural half (12.5.2). A turn cut short by a length
// cap has to render visibly cut, so the frame that carries it owes it a row —
// which means the shell may never quietly keep one back. Every pane is handed
// exactly the rectangle the layout gave it, to the cell.
func TestPanesReceiveTheirWholeAllotment(t *testing.T) {
	for _, opts := range []Options{{}, {Linear: true}} {
		for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 8}, {40, 3}} {
			s := sized(t, size[0], size[1], opts)
			var got [numLayers][2]int
			for id := LayerTranscript; id < numLayers; id++ {
				id := id
				s.SetPane(id, paneFunc(func(w, h int) string {
					got[id] = [2]int{w, h}
					return ""
				}))
			}
			s.Frame(size[0], size[1])
			for _, sl := range s.layout.Slots {
				want := [2]int{sl.Rect.Dx(), sl.Rect.Dy()}
				if got[sl.ID] != want {
					t.Fatalf("%v at %v: pane was given %v of a %v allotment",
						sl.ID, size, got[sl.ID], want)
				}
			}
		}
	}
}

// The overlay plane is a real region from the first wave, not a Wave 3
// discovery: raising it takes the cells and the clicks, and dropping it gives
// them back without leaving a ghost.
func TestShellOverlayTakesTheCellsAndTheClicks(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	if _, ok := s.comp.rect(LayerOverlay); ok {
		t.Fatal("the overlay plane was raised without being asked for")
	}

	s.SetOverlay(true)
	rect, ok := s.comp.rect(LayerOverlay)
	if !ok {
		t.Fatal("SetOverlay(true) raised nothing")
	}
	if !s.OverlayOpen() {
		t.Fatal("the shell forgot the overlay is open")
	}
	// A click inside it belongs to the overlay even though the transcript is
	// still laid out underneath.
	id, _, _ := s.comp.hit(rect.Min.X+1, rect.Min.Y+1)
	if id != LayerOverlay {
		t.Fatalf("a click inside the overlay reached %v", id)
	}
	frame := s.Frame(120, 30)
	if !strings.Contains(frame, "wave 3: dialogs") {
		t.Fatalf("the overlay drew nothing:\n%s", frame)
	}

	s.SetOverlay(false)
	if _, ok := s.comp.rect(LayerOverlay); ok {
		t.Fatal("SetOverlay(false) left the plane raised")
	}
	if after := s.Frame(120, 30); strings.Contains(after, "wave 3: dialogs") {
		t.Fatalf("the overlay left a ghost:\n%s", after)
	}
	if id, _, _ := s.comp.hit(rect.Min.X+1, rect.Min.Y+1); id == LayerOverlay {
		t.Fatal("a dropped overlay still takes clicks")
	}
}

// No chord may require Shift+Enter (10.1.2). The advertised binding may
// improve when the terminal can tell the two enters apart; the accepted set
// always contains the one tmux can deliver.
func TestNewlineBindingNeverRequiresTheProtocol(t *testing.T) {
	for _, caps := range []Capabilities{{}, {Disambiguation: true}, {Disambiguation: true, EventTypes: true}} {
		keys := caps.NewlineKeys()
		if len(keys) == 0 {
			t.Fatalf("%+v accepts no newline binding at all", caps)
		}
		found := false
		for _, k := range keys {
			if k == LegacyNewlineKey {
				found = true
			}
		}
		if !found {
			t.Fatalf("%+v dropped the legacy binding: %v", caps, keys)
		}
		if keys[0] != caps.NewlineKey() {
			t.Fatalf("%+v advertises %q but leads with %q", caps, caps.NewlineKey(), keys[0])
		}
	}
	if SendKey != "enter" {
		t.Fatalf("send binding = %q", SendKey)
	}
}

// The status line shortens by dropping columns, never by wrapping (10.5.22).
func TestStatusLineDropsColumnsInsteadOfWrapping(t *testing.T) {
	cols := []column{
		{text: "aforge v2", rank: 0},
		{text: "wide", rank: 1},
		{text: "120×30", rank: 2},
		{text: "focus transcript", rank: 3},
		{text: "hit rail 3,7", rank: 6},
	}
	previous := 999
	for w := 120; w >= 1; w-- {
		line := composeStatus(cols, w)
		if strings.Contains(line, "\n") {
			t.Fatalf("width %d wrapped: %q", w, line)
		}
		got := ansi.StringWidth(line)
		if got > w {
			t.Fatalf("width %d produced %d cells: %q", w, got, line)
		}
		if got > previous {
			t.Fatalf("width %d grew the line back to %d cells", w, got)
		}
		previous = got
	}
	// The wordmark is rank 0 and survives to the end.
	if line := composeStatus(cols, 3); !strings.HasPrefix("aforge v2", line) {
		t.Fatalf("last column standing = %q", line)
	}
}

// Every capability starts pessimistic, and the surface has to be usable while
// it stays there. The legacy newline binding exists precisely for that state.
func TestCapabilitiesDegradeRatherThanDisappear(t *testing.T) {
	s := sized(t, 100, 30, Options{})
	caps := s.Capabilities()
	if caps.Disambiguation || caps.EventTypes {
		t.Fatal("capabilities should start denied, not assumed")
	}
	if caps.SyncOutput != supportUnknown || caps.UnicodeCore != supportUnknown {
		t.Fatal("unasked questions should read as unknown, not as no")
	}
	if got := caps.NewlineKey(); got != LegacyNewlineKey {
		t.Fatalf("newline binding without the protocol = %q", got)
	}

	s.Update(tea.KeyboardEnhancementsMsg{Flags: 1})
	if got := s.Capabilities().NewlineKey(); got != "shift+enter" {
		t.Fatalf("newline binding with disambiguation = %q", got)
	}

	s.Update(tea.ModeReportMsg{Mode: ansi.ModeSynchronizedOutput, Value: ansi.ModeReset})
	if s.Capabilities().SyncOutput != supportYes {
		t.Fatal("a recognized mode 2026 should read as supported")
	}
	s.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeNotRecognized})
	if s.Capabilities().UnicodeCore != supportNo {
		t.Fatal("an unrecognized mode 2027 should read as unsupported")
	}
}

// We never ask the keyboard for anything a tmux user cannot have.
func TestShellRequestsNoKeyboardEnhancements(t *testing.T) {
	s := sized(t, 100, 30, Options{})
	view := s.View()
	if view.KeyboardEnhancements != (tea.KeyboardEnhancements{}) {
		t.Fatalf("the shell asked for %+v", view.KeyboardEnhancements)
	}
	if view.ReportFocus {
		t.Fatal("the shell enabled focus reporting")
	}
	if !view.AltScreen {
		t.Fatal("the shell left the alt screen")
	}
	// All motion, because a hover preview is what makes a wordless affordance
	// visible (5.22 rule 5) and there is no other report that carries it. The
	// bandwidth law is kept on the OUTPUT side instead: a motion that does not
	// change which target is under the pointer produces no frame at all —
	// TestAPointerCrossingOneTargetCostsNothing is that promise.
	if view.MouseMode != tea.MouseModeAllMotion {
		t.Fatalf("mouse mode = %v", view.MouseMode)
	}
}

// No binding may require a key release, because tmux will never deliver one.
// The shell does not request release reporting and does not read it either, so
// the law holds even if some other layer turns the protocol on.
func TestShellIgnoresKeyReleases(t *testing.T) {
	s := sized(t, 120, 30, Options{})
	pane := &recordingPane{}
	s.SetPane(LayerTranscript, pane)
	before := s.Frame(120, 30)

	_, cmd := s.Update(tea.KeyReleaseMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("a key release produced a command")
	}
	if pane.keys != 0 {
		t.Fatal("a key release reached a pane")
	}
	if after := s.Frame(120, 30); after != before {
		t.Fatal("a key release changed the picture")
	}
}

// The anti-jank ledger names the window (10.1.4); a constant that drifted out
// of it would be a doctrine change disguised as a tuning tweak.
func TestResizeDebounceStaysInsideTheLedgersWindow(t *testing.T) {
	if resizeDebounce < 16*time.Millisecond || resizeDebounce > 30*time.Millisecond {
		t.Fatalf("resize debounce is %v, outside the 16–30ms window", resizeDebounce)
	}
}

// The surface comes up and goes down with no terminal attached at all, which
// is how it is checked in CI and how the golden harness will drive it.
func TestShellBootsHeadlessly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	in, keyboard := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, RunOptions{
			Options: Options{DB: "/tmp/none.db", Session: "new"},
			Input:   in,
			Output:  out,
		})
	}()

	// What goes down the wire is the evidence for the doctrine: the alt screen
	// is entered, the mouse is captured, and the two questions we ask at start
	// are actually asked — mode 2026 and 2027 (DECRQM), then truecolor from
	// terminfo (XTGETTCAP for Tc and RGB) rather than from COLORTERM.
	wanted := []struct{ seq, why string }{
		{"\x1b[?1049h", "alt screen"},
		{"\x1b[?1006h", "SGR mouse"},
		{"\x1b[?2026$p", "synchronized output query"},
		{"\x1b[?2027$p", "unicode core query"},
		{"\x1bP+q5463\x1b\\", "XTGETTCAP Tc"},
		{"\x1bP+q524742\x1b\\", "XTGETTCAP RGB"},
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		wire := out.String()
		missing := ""
		for _, want := range wanted {
			if !strings.Contains(wire, want.seq) {
				missing = want.why
				break
			}
		}
		if missing == "" {
			if strings.Contains(wire, "\x1b[?1004h") {
				t.Fatal("the surface enabled focus reporting")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the surface never asked for %s", missing)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := keyboard.Write([]byte{0x03}); err != nil { // ctrl+c
		t.Fatalf("write ctrl+c: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("headless boot: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("the surface never came down")
	}
	_ = keyboard.Close()
}

// syncBuffer is a writer the test can read while the program is still writing
// to it, which is the only way to watch a boot rather than autopsy one.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// recordingPane is a Pane that counts what the shell handed it.
type recordingPane struct {
	keys    int
	onMouse func(image.Point)
}

func (p *recordingPane) Render(w, h int) string { return "" }

func (p *recordingPane) Key(tea.KeyPressMsg) tea.Cmd {
	p.keys++
	return nil
}

func (p *recordingPane) Mouse(_ tea.MouseMsg, local image.Point) tea.Cmd {
	if p.onMouse != nil {
		p.onMouse(local)
	}
	return nil
}
