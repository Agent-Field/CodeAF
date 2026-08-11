package tui2

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// termShell builds a shell with the terminal channels under test control. It
// pins the multiplexer too: NewShell reads the real environment, and a suite
// that behaved differently inside tmux would be a suite nobody could trust.
func termShell(t *testing.T, term TerminalOptions, mux Multiplexer, notify support) *Shell {
	t.Helper()
	s := NewShell(Options{Terminal: &term})
	s.caps.Mux = mux
	s.caps.Notification = notify
	s.note.now = func() time.Time { return time.Unix(0, 0) }
	s.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// Drain the resize; nothing here should have produced a write, and if it
	// did the tests below would blame the wrong call.
	s.out = s.out[:0]
	return s
}

// raw runs a command and returns the bytes it would put on the wire. A nil
// command is no bytes, which is the answer most of these tests want.
func raw(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		return ""
	}
	return collectRaw(t, cmd())
}

func collectRaw(t *testing.T, msg tea.Msg) string {
	t.Helper()
	switch m := msg.(type) {
	case nil:
		return ""
	case tea.RawMsg:
		s, ok := m.Msg.(string)
		if !ok {
			t.Fatalf("a raw write carried %T, not a string", m.Msg)
		}
		return s
	case tea.BatchMsg:
		var b strings.Builder
		for _, c := range m {
			b.WriteString(raw(t, c))
		}
		return b.String()
	default:
		return ""
	}
}

// THE CAPABILITY MATRIX, at the shell. Same event, three terminals, three
// writes — and never a silent one, because silence is the failure mode the
// ladder exists to prevent.
func TestNotifyLadderPerCapabilityState(t *testing.T) {
	cases := []struct {
		name   string
		mux    Multiplexer
		notify support
		want   string
	}{
		{
			name: "probed: the terminal answered OSC 99", mux: MuxNone, notify: supportYes,
			want: "\x1b]99;i=aforge:d=0:p=title;aforge\x07\x1b]99;i=aforge:d=1:p=body;a question is waiting\x07",
		},
		{
			name: "fallback: no answer, but we speak to the emulator directly",
			mux:  MuxNone, notify: supportNo,
			want: "\x1b]777;notify;aforge;a question is waiting\x07",
		},
		{
			name: "fallback: not asked yet degrades the same way",
			mux:  MuxNone, notify: supportUnknown,
			want: "\x1b]777;notify;aforge;a question is waiting\x07",
		},
		{
			name: "floor: tmux would eat an OSC it does not know",
			mux:  MuxTmux, notify: supportNo,
			want: "\a",
		},
		{
			name: "floor: screen, likewise",
			mux:  MuxScreen, notify: supportUnknown,
			want: "\a",
		},
		{
			name: "a probed terminal INSIDE tmux still gets the rich channel",
			mux:  MuxTmux, notify: supportYes,
			want: "\x1b]99;i=aforge:d=0:p=title;aforge\x07\x1b]99;i=aforge:d=1:p=body;a question is waiting\x07",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := termShell(t, DefaultTerminalOptions(), tc.mux, tc.notify)
			got := raw(t, s.Notify(AttentionNeedsInput, "aforge", "a question is waiting"))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The taxonomy is closed (10.5.27). There is no fourth kind and no
// general-purpose one, and the zero value is silence rather than a
// notification — a forgotten field must not become an interruption.
func TestNotifyTaxonomyIsClosed(t *testing.T) {
	for _, kind := range []AttentionKind{AttentionNone, AttentionKind(4), AttentionKind(200)} {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		if got := raw(t, s.Notify(kind, "aforge", "body")); got != "" {
			t.Fatalf("kind %d notified with %q", kind, got)
		}
	}
	// And all three real kinds do reach the desktop.
	for _, kind := range []AttentionKind{AttentionNeedsInput, AttentionDelivery, AttentionFailure} {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		if raw(t, s.Notify(kind, "aforge", kind.String())) == "" {
			t.Fatalf("%v produced no notification", kind)
		}
	}
}

// The focus gate, and the degradation that matters more than the gate.
//
// Focus reporting is off in tmux by default and this shell never asks for it
// (View.ReportFocus stays false, 10.1.2), so "we were never told" is the state
// most users are in. It has to mean NOTIFY. A gate written the other way round
// would silently disable notifications for every tmux user.
func TestFocusGateDegradesToNotifying(t *testing.T) {
	t.Run("never told: notify", func(t *testing.T) {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		if s.note.focus != supportUnknown {
			t.Fatal("focus should start unknown")
		}
		if raw(t, s.Notify(AttentionDelivery, "aforge", "done")) == "" {
			t.Fatal("an unknown focus state suppressed a notification")
		}
	})

	t.Run("told blurred: notify", func(t *testing.T) {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		s.Update(tea.BlurMsg{})
		if raw(t, s.Notify(AttentionDelivery, "aforge", "done")) == "" {
			t.Fatal("a blurred terminal suppressed a notification")
		}
	})

	t.Run("told focused: suppress", func(t *testing.T) {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		s.Update(tea.FocusMsg{})
		if got := raw(t, s.Notify(AttentionDelivery, "aforge", "done")); got != "" {
			t.Fatalf("a focused terminal was interrupted with %q", got)
		}
	})

	t.Run("focus comes back: notify again", func(t *testing.T) {
		s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
		s.Update(tea.FocusMsg{})
		s.Update(tea.BlurMsg{})
		if raw(t, s.Notify(AttentionDelivery, "aforge", "done")) == "" {
			t.Fatal("blur did not reopen the gate")
		}
	})
}

// One notification per event. A polling surface re-derives the same fact many
// times a second and must not turn that into a storm.
func TestNotifyStormGuards(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
	now := time.Unix(0, 0)
	s.note.now = func() time.Time { return now }

	if raw(t, s.Notify(AttentionFailure, "aforge", "worker 3 died")) == "" {
		t.Fatal("the first notification was suppressed")
	}
	// The same event, re-noticed by the next poll.
	now = now.Add(50 * time.Millisecond)
	if got := raw(t, s.Notify(AttentionFailure, "aforge", "worker 3 died")); got != "" {
		t.Fatalf("an identical repeat got through as %q", got)
	}
	// A different failure in the same instant is still one interruption.
	if got := raw(t, s.Notify(AttentionFailure, "aforge", "worker 4 died")); got != "" {
		t.Fatalf("a same-kind burst got through as %q", got)
	}
	// A different KIND is never swallowed: a failure behind a delivery is
	// exactly the pair you must not lose.
	if raw(t, s.Notify(AttentionNeedsInput, "aforge", "a question")) == "" {
		t.Fatal("a different kind was swallowed by the same-kind floor")
	}
	// Past the floor, the same kind speaks again.
	now = now.Add(notifyFloor + time.Millisecond)
	if raw(t, s.Notify(AttentionFailure, "aforge", "worker 5 died")) == "" {
		t.Fatal("the same-kind floor never lifted")
	}
	// Past the repeat window, even the identical event speaks again.
	now = now.Add(notifyRepeat + time.Millisecond)
	if raw(t, s.Notify(AttentionFailure, "aforge", "worker 5 died")) == "" {
		t.Fatal("the repeat window never lifted")
	}
}

// Every channel is optional, and every channel off is a shell that still works
// and writes nothing.
func TestTerminalChannelsAreOptional(t *testing.T) {
	t.Run("notifications off", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.Notify = false
		s := termShell(t, opts, MuxNone, supportYes)
		if got := raw(t, s.Notify(AttentionFailure, "aforge", "boom")); got != "" {
			t.Fatalf("notifications were off and it wrote %q", got)
		}
	})

	t.Run("bell off silences only the floor", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.Bell = false

		floor := termShell(t, opts, MuxTmux, supportNo)
		if got := raw(t, floor.Notify(AttentionFailure, "aforge", "boom")); got != "" {
			t.Fatalf("the bell was off and the floor rang: %q", got)
		}

		rich := termShell(t, opts, MuxNone, supportYes)
		if raw(t, rich.Notify(AttentionFailure, "aforge", "boom")) == "" {
			t.Fatal("turning the bell off silenced OSC 99 too")
		}
	})

	t.Run("progress off", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.Progress = false
		s := termShell(t, opts, MuxNone, supportYes)
		if got := raw(t, s.SetBusy(true)); got != "" {
			t.Fatalf("progress was off and it wrote %q", got)
		}
		if !s.Busy() {
			t.Fatal("the shell forgot the fact along with the write")
		}
	})

	t.Run("prompt marks off", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.PromptMarks = false
		s := termShell(t, opts, MuxNone, supportYes)
		if got := raw(t, s.MarkPrompt()); got != "" {
			t.Fatalf("prompt marks were off and it wrote %q", got)
		}
	})

	t.Run("hyperlinks off", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.Hyperlinks = false
		s := termShell(t, opts, MuxNone, supportYes)
		if s.Linker().Enabled() {
			t.Fatal("hyperlinks were off and the linker says otherwise")
		}
	})

	t.Run("titles off", func(t *testing.T) {
		opts := DefaultTerminalOptions()
		opts.Title = false
		s := termShell(t, opts, MuxNone, supportYes)
		s.SetAttention(3)
		if title := s.View().WindowTitle; title != "" {
			t.Fatalf("titles were off and the view declared %q", title)
		}
	})
}

// THE ZERO-BYTES BAR. A shell built from the zero TerminalOptions — which is
// what a headless driver and the golden harness get — says nothing to the
// terminal, ever, no matter what the wiring calls.
func TestZeroTerminalOptionsEmitNothing(t *testing.T) {
	s := termShell(t, TerminalOptions{}, MuxNone, supportYes)
	calls := []tea.Cmd{
		s.Notify(AttentionNeedsInput, "aforge", "a question"),
		s.Notify(AttentionDelivery, "aforge", "done"),
		s.Notify(AttentionFailure, "aforge", "boom"),
		s.SetBusy(true),
		s.SetBusy(false),
		s.MarkPrompt(),
	}
	for i, cmd := range calls {
		if got := raw(t, cmd); got != "" {
			t.Fatalf("call %d wrote %q from a zero-value terminal", i, got)
		}
	}
	s.SetAttention(7)
	if title := s.View().WindowTitle; title != "" {
		t.Fatalf("a zero-value terminal declared the title %q", title)
	}
	if s.Linker().Enabled() {
		t.Fatal("a zero-value terminal linked")
	}
}

// The progress state machine: two states, one write per transition, and no
// write at all for a fact that did not move.
func TestProgressTransitionsAreDiffed(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)

	if got := raw(t, s.SetBusy(false)); got != "" {
		t.Fatalf("idle-to-idle wrote %q", got)
	}
	if got, want := raw(t, s.SetBusy(true)), "\x1b]9;4;3\x07"; got != want {
		t.Fatalf("start = %q, want %q", got, want)
	}
	for i := range 5 {
		if got := raw(t, s.SetBusy(true)); got != "" {
			t.Fatalf("re-assert %d wrote %q", i, got)
		}
	}
	if got, want := raw(t, s.SetBusy(false)), "\x1b]9;4;0\x07"; got != want {
		t.Fatalf("stop = %q, want %q", got, want)
	}
	if got := raw(t, s.SetBusy(false)); got != "" {
		t.Fatalf("stop-again wrote %q", got)
	}
}

// A spinning taskbar bar outlives the process. Quitting clears it first, and
// clears it only when there is something to clear.
func TestQuitClearsTheTaskbar(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
	s.SetBusy(true)
	s.out = s.out[:0]

	_, cmd := s.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if s.Busy() {
		t.Fatal("the shell still believes a turn is running")
	}
	// The reset went INTO the returned command rather than being dropped: an
	// empty queue after the fact is what proves it was handed over. (The
	// command itself is a tea.Sequence, whose contents the runtime unpacks —
	// the bytes it carries are pinned by TestProgressTransitionsAreDiffed.)
	if len(s.out) != 0 {
		t.Fatalf("%d writes were stranded in the queue", len(s.out))
	}

	idle := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
	if _, cmd := idle.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c on an idle shell returned no command")
	}
}

// The prompt mark: one per committed user message, no state, no pairing.
func TestPromptMarksAreOnePerCommit(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
	for i := range 3 {
		got := raw(t, s.MarkPrompt())
		if got != "\x1b]133;A\x07" {
			t.Fatalf("mark %d = %q", i, got)
		}
	}
}

// The title is declared, not written, so Bubble Tea's renderer diffs it: an
// idle surface that re-renders sixty times a second sends the title zero times.
// What this pins is the half we own — the same facts produce the same string.
func TestTitleCarriesTheAttentionCount(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)
	if got := s.View().WindowTitle; got != "aforge" {
		t.Fatalf("idle title = %q", got)
	}
	s.SetAttention(2)
	if got := s.View().WindowTitle; got != "aforge (2)" {
		t.Fatalf("busy title = %q", got)
	}
	if s.View().WindowTitle != s.View().WindowTitle {
		t.Fatal("the title is not a function of the state")
	}
	s.SetAttention(0)
	if got := s.View().WindowTitle; got != "aforge" {
		t.Fatalf("settled title = %q", got)
	}
	s.SetAttention(-4)
	if s.Attention() != 0 {
		t.Fatalf("a negative count survived as %d", s.Attention())
	}

	opts := DefaultTerminalOptions()
	opts.AppName = "af\x1b]0;pwn\aorge"
	named := termShell(t, opts, MuxNone, supportYes)
	if strings.ContainsRune(named.View().WindowTitle, 0x1b) {
		t.Fatalf("an app name broke out of the title: %q", named.View().WindowTitle)
	}
}

// Two facts moving in one message leave as one write, and a message that moved
// nothing still returns no command — the resize path must stay nil-clean.
func TestFlushCoalescesAndStaysNilWhenIdle(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)

	s.queue("A")
	s.queue("")
	s.queue("B")
	if got := raw(t, s.flush()); got != "AB" {
		t.Fatalf("flush = %q, want %q", got, "AB")
	}
	if s.flush() != nil {
		t.Fatal("a second flush produced a write")
	}
	if s.withFlush(nil) != nil {
		t.Fatal("withFlush invented a command out of an empty queue")
	}

	// An ordinary message with nothing queued still returns nothing.
	if _, cmd := s.Update(tea.WindowSizeMsg{Width: 80, Height: 24}); cmd != nil {
		t.Fatal("a no-op resize returned a command")
	}
}

// The negotiation and the observation, together: the probe's answer upgrades
// us, and the trailing question dates its silence.
func TestCapabilitiesObserveTheNotificationProbe(t *testing.T) {
	t.Run("an OSC 99 reply is the answer", func(t *testing.T) {
		var c Capabilities
		if !c.observe(uv.UnknownOscEvent("\x1b]99;i=aforge:p=?;i,d,p\x1b\\")) {
			t.Fatal("the reply moved nothing")
		}
		if c.Notification != supportYes {
			t.Fatalf("Notification = %v, want yes", c.Notification)
		}
	})

	t.Run("the 8-bit form counts too", func(t *testing.T) {
		var c Capabilities
		c.observe(uv.UnknownOscEvent("\x9d99;i=aforge\x9c"))
		if c.Notification != supportYes {
			t.Fatalf("Notification = %v, want yes", c.Notification)
		}
	})

	t.Run("an unrelated OSC is not evidence", func(t *testing.T) {
		var c Capabilities
		if c.observe(uv.UnknownOscEvent("\x1b]7;file://host/tmp\x1b\\")) {
			t.Fatal("an unrelated OSC moved a capability")
		}
		if c.Notification != supportUnknown {
			t.Fatalf("Notification = %v, want unknown", c.Notification)
		}
	})

	t.Run("the trailing question turns silence into a no", func(t *testing.T) {
		var c Capabilities
		if !c.observe(uv.PrimaryDeviceAttributesEvent{6}) {
			t.Fatal("DA1 did not settle the probe")
		}
		if c.Notification != supportNo {
			t.Fatalf("Notification = %v, want no", c.Notification)
		}
		// And it cannot un-answer an answer that already arrived.
		c.observe(uv.UnknownOscEvent("\x1b]99;i=aforge\x1b\\"))
		if c.observe(uv.PrimaryDeviceAttributesEvent{6}) || c.Notification != supportYes {
			t.Fatalf("DA1 downgraded a probed yes to %v", c.Notification)
		}
	})

	t.Run("a late answer still upgrades", func(t *testing.T) {
		var c Capabilities
		c.observe(uv.PrimaryDeviceAttributesEvent{6})
		c.observe(uv.UnknownOscEvent("\x1b]99;i=aforge\x1b\\"))
		if c.Notification != supportYes {
			t.Fatalf("Notification = %v, want yes", c.Notification)
		}
	})
}

func TestDetectMultiplexer(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(k string) string { return pairs[k] }
	}
	cases := []struct {
		name string
		vars map[string]string
		want Multiplexer
	}{
		{"bare terminal", map[string]string{"TERM": "xterm-256color"}, MuxNone},
		{"tmux says so itself", map[string]string{"TMUX": "/tmp/tmux-1000/default,9,0", "TERM": "screen-256color"}, MuxTmux},
		{"screen says so itself", map[string]string{"STY": "9.pts-0.host", "TERM": "screen"}, MuxScreen},
		{"tmux by TERM alone", map[string]string{"TERM": "tmux-256color"}, MuxTmux},
		{"screen by TERM alone", map[string]string{"TERM": "screen"}, MuxScreen},
		{"nothing at all", map[string]string{}, MuxNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectMultiplexer(env(tc.vars)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if got := detectMultiplexer(nil); got != MuxNone {
		t.Fatalf("a nil lookup gave %v", got)
	}
}

// OSC 8 is the one sequence in this lane that travels INSIDE frame content,
// and this is why that is allowed to work: the cell buffer under the
// compositor carries a hyperlink as a cell attribute, so a link written into a
// pane's rows survives composition and reaches the terminal through the
// ordinary diffing renderer.
//
// The second half is the reason everything ELSE in this lane is written beside
// the frame: a prompt mark is not a cell attribute, has nowhere to live in a
// cell, and is parsed away during composition. Both halves are asserted here
// because together they are the argument for the whole design, and a change in
// the cell buffer that quietly broke either one would otherwise be found by a
// user rather than by CI.
func TestOnlyHyperlinksSurviveComposition(t *testing.T) {
	s := termShell(t, DefaultTerminalOptions(), MuxNone, supportYes)

	link := s.Linker().Wrap("navigate.rs", "file:///src/navigate.rs")
	s.SetPane(LayerTranscript, paneFunc(func(width, height int) string { return link }))
	frame := s.Frame(80, 24)
	if !strings.Contains(frame, "file:///src/navigate.rs") {
		t.Fatal("a hyperlink did not survive composition; the Linker would be decorative")
	}
	if !strings.Contains(frame, "navigate.rs") {
		t.Fatal("the label did not survive composition")
	}

	s.SetPane(LayerTranscript, paneFunc(func(width, height int) string {
		return promptMarkBytes + "you: hello"
	}))
	s.Invalidate()
	frame = s.Frame(80, 24)
	if strings.Contains(frame, "133;A") {
		t.Fatal("a prompt mark survived composition; MarkPrompt could be a blocks seam after all")
	}
	if !strings.Contains(frame, "you: hello") {
		t.Fatal("the row itself did not survive composition")
	}
}

// Nothing here panics on degenerate input: a zero-size shell, an empty
// notification, a mark before the first frame.
func TestTerminalChannelsSurviveDegenerateInput(t *testing.T) {
	s := NewShell(Options{})
	s.caps.Mux = MuxNone
	raw(t, s.MarkPrompt())
	raw(t, s.SetBusy(true))
	raw(t, s.Notify(AttentionFailure, "", ""))
	s.SetAttention(1 << 30)
	s.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	if s.render() != "" {
		t.Fatal("a zero-size shell drew something")
	}
	_ = s.View()
}
