package tui3

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE TERMINAL'S TAB SAYS WHERE A PERSON IS INSIDE codeaf.
//
// Each row below opens the surface somewhere a person can stand and reads the
// sentence the tab would be sent, spelled exactly. The rows are the ruling's own
// table (title.go), and the strings are written out rather than rebuilt from
// the constants, because the claim is about what a person reads on a tab.

func TestTheTerminalTitleSaysWhereYouAre(t *testing.T) {
	rows := []struct {
		where string
		open  func(t *testing.T) *app
		want  string
	}{
		{"home, nothing waiting", func(t *testing.T) *app { return homeWanting(t, 0) }, "codeaf"},
		{"home, three waiting", func(t *testing.T) *app { return homeWanting(t, 3) }, "3 want you · codeaf"},
		{"a conversation", func(t *testing.T) *app {
			_, a := wired()
			a.title = "Token counter"
			return a
		}, "Token counter · codeaf"},
		{"a conversation with no name yet", func(t *testing.T) *app {
			_, a := wired()
			return a
		}, "new conversation · codeaf"},
		{"a conversation waiting on you", titleAsking, "? Token counter · codeaf"},
		{"a task room", func(t *testing.T) *app {
			a, _, _ := roomApp(t)
			clickRail(t, a, 0)
			if !a.roomOpen() {
				t.Fatal("the task room never opened")
			}
			return a
		}, "Fix the nil-map crash · task · codeaf"},
		{"over --host", func(t *testing.T) *app {
			_, a := wired()
			a.title, a.host = "Token counter", "devbox"
			return a
		}, "Token counter @ devbox · codeaf"},
	}
	for _, row := range rows {
		t.Run(row.where, func(t *testing.T) {
			a := row.open(t)
			got := terminalTitle(a)
			if got != row.want {
				t.Fatalf("the tab says %q, want %q", got, row.want)
			}
			titleIsPlainText(t, got)
		})
	}
}

// EVERY OTHER PLACE IS ITS OWN WORD. The labs are the seven rooms' own
// (placeeveryone_test.go), so a place added to the registry without a title
// here is a row this test names rather than one it silently skips.
func TestEveryPlaceTitlesTheTabWithItsWord(t *testing.T) {
	want := map[page]string{
		pageTasks:    "sessions · codeaf",
		pageTeams:    "teams · codeaf",
		pageStanding: "standing · codeaf",
		pageMemory:   "memory · codeaf",
		pageSpend:    "spend · codeaf",
		pageSearch:   "search · codeaf",
		pageSettings: "settings · codeaf",
	}
	for _, place := range everyPlaceTable() {
		if place.id == pageHome {
			continue
		}
		t.Run(place.id.word(), func(t *testing.T) {
			title, ok := want[place.id]
			if !ok {
				t.Fatalf("the %s place has no title in this table", place.id.word())
			}
			a := place.open(t)
			if got := terminalTitle(a); got != title {
				t.Fatalf("the tab says %q, want %q", got, title)
			}
		})
	}
}

// THE NAME IS CUT, NEVER THE SUFFIX. A tab truncates from the right, so what is
// sent is already short enough to keep ` · codeaf` — the one part that tells a
// codeaf tab from a shell's.
func TestALongNameIsCutBeforeTheSuffix(t *testing.T) {
	_, a := wired()
	a.title = strings.Repeat("a very long conversation name ", 6)
	got := terminalTitle(a)
	if !strings.HasSuffix(got, " · codeaf") {
		t.Fatalf("the suffix was cut: %q", got)
	}
	if width := len([]rune(got)); width > titleMax {
		t.Fatalf("the title ran to %d cells, cap is %d: %q", width, titleMax, got)
	}
	if !strings.Contains(got, glyphMore) {
		t.Fatalf("the cut name carries no ellipsis: %q", got)
	}
}

// THE MARK IS THE ASCII SPELLING WHATEVER TIER THE FRAME IS DRAWN IN. The title
// is drawn by the operating system in its own font, which has never heard of
// the icon repertoire — so a terminal on Font Awesome still sends `?`.
func TestTheNeedsYouMarkIsASCIIOnARichTerminal(t *testing.T) {
	a := titleAsking(t)
	a.pal.icons = tokens.NerdFont
	if got := terminalTitle(a); !strings.HasPrefix(got, "? ") {
		t.Fatalf("a rich terminal's tab = %q, want the ASCII `?` first", got)
	}
}

// A NAME CANNOT CARRY A CONTROL BYTE INTO THE TITLE. A conversation is free to be
// NAMED after an escape sequence; the name still may not end the OSC string
// early and spill the rest into the frame — the seven-bit ESC and BEL, and the
// eight-bit string terminator a C1 terminal would honour just the same.
func TestANameCannotCarryAnEscapeIntoTheTitle(t *testing.T) {
	_, a := wired()
	a.title = "evil\x1b]2;pwned\a name tail"
	got := terminalTitle(a)
	titleIsPlainText(t, got)
	if got != "evil]2pwned name tail · codeaf" {
		t.Fatalf("the sanitized title = %q", got)
	}
}

// THE TITLE IS RE-SENT WHEN THE NAME ARRIVES, and the window's half moves with
// the tab's: both are the one sentence last sent.
func TestTheTitleIsSentAgainWhenTheNameArrives(t *testing.T) {
	_, a := wired()
	if a.titleSent != "new conversation · codeaf" {
		t.Fatalf("the surface opened on %q", a.titleSent)
	}
	a.Update(titleEventMsg{gen: a.titleGen, ev: session.Event{
		Kind: session.EventTitleChanged, Text: "counting the tokens in a transcript", ShortTitle: "Token counter",
	}})
	if a.titleSent != "counting the tokens in a transcript · codeaf" {
		t.Fatalf("after the name arrived the tab was sent %q", a.titleSent)
	}
	if got := a.View().WindowTitle; got != a.titleSent {
		t.Fatalf("the window declares %q, the tab was sent %q", got, a.titleSent)
	}
}

// A FRAME WITH NO CHANGE SENDS NO SECOND TITLE. A title written on every
// message is a title flickering in some terminals, so what is asked after a
// message that moved nothing is answered with nothing at all.
func TestAnUnchangedTitleIsNotSentTwice(t *testing.T) {
	_, a := wired()
	a.width, a.height = 100, 30
	a.title = "Token counter"

	first := a.retitle()
	if first == nil {
		t.Fatal("a changed title was not sent")
	}
	if got := first(); got != (tea.RawMsg{Msg: "\x1b]1;Token counter · codeaf\a"}) {
		t.Fatalf("the tab was sent %#v, want OSC 1 with the sentence", got)
	}
	if again := a.retitle(); again != nil {
		t.Fatalf("the same title was sent twice: %#v", again())
	}
	// And through the one door every message passes: a size the terminal has
	// already sent changes nothing, and asks for nothing — no title included.
	if _, cmd := a.Update(tea.WindowSizeMsg{Width: 100, Height: 30}); cmd != nil {
		t.Fatalf("a message that moved nothing sent %#v", cmd())
	}
}

// ON QUIT THE TAB IS HANDED BACK EMPTY. Terminal.app keeps an icon name until
// something replaces it, so a title left behind is a lie about a process that
// is gone. The surface is booted with no terminal at all, sent its title, and
// quit — and the last title the stream says is the empty one, on both halves.
func TestQuittingHandsTheTabBackEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	in, keyboard := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Agent: &fakeAgent{model: "m"}, Workspace: "/tmp/lab",
			Input: in, Output: out, Width: 80, Height: 24,
		})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), " · codeaf\a") && !strings.Contains(out.String(), "\x1b]1;codeaf\a") {
		if time.Now().After(deadline) {
			t.Fatalf("the surface never sent its title:\n%q", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := keyboard.Write([]byte("/quit\r")); err != nil {
		t.Fatalf("write to the surface: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the surface returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("/quit did not close the surface")
	}
	stream := out.String()
	for _, osc := range []string{"\x1b]1;", "\x1b]2;"} {
		last := stream[strings.LastIndex(stream, osc):]
		if !strings.HasPrefix(last, osc+"\a") {
			t.Fatalf("the last %q title left behind is %q", osc, last[:min(len(last), 40)])
		}
	}
}

// homeWanting is home, standing at rest, over a machine where `wants` things
// have stopped on a person. The count is put straight into the reading the
// pulse draws ([app.machine]), because the subject is the sentence and the
// counters have their own tests (pulsemoney_test.go).
func homeWanting(t *testing.T, wants int) *app {
	t.Helper()
	a, _ := pulseLab(t)
	a.openHome()
	a.machine = machineFacts{wants: wants}
	if !a.at(pageHome) {
		t.Fatal("home never opened")
	}
	return a
}

// titleAsking is a named conversation with a permission question up.
func titleAsking(t *testing.T) *app {
	t.Helper()
	_, a := wired([]session.Event{
		toolBegin("bash", "bash go test ./..."),
		consentEvent(7, "bash", "bash go test ./...", `bash pattern "go test *"`),
	})
	a.title = "Token counter"
	typeLine(t, a, "run the tests")
	if !a.asking() {
		t.Fatal("the question never came up")
	}
	return a
}

// titleIsPlainText pins the rule the title is written under: it goes to the
// operating system, so it carries no control byte at all, and the only
// characters beyond ASCII in a sentence about an ASCII name are its own two
// typographic marks — the separator every clause list on this surface uses and
// the ellipsis a cut name ends in. An icon glyph from the vocabulary is neither.
func titleIsPlainText(t *testing.T, title string) {
	t.Helper()
	for _, r := range title {
		switch {
		case unicode.IsControl(r):
			t.Fatalf("the title carries control byte %U: %q", r, title)
		case r > unicode.MaxASCII && !strings.ContainsRune(pulseGap+glyphMore, r):
			t.Fatalf("the title carries %q beyond its own marks: %q", r, title)
		}
	}
}
