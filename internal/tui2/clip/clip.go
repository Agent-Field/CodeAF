// Package clip puts text on the reader's clipboard, over the terminal itself.
//
// OSC 52 is the only clipboard a terminal application can reach without asking
// the host operating system for one: the bytes `ESC ] 52 ; c ; <base64> BEL`
// travel up the same pipe the frame does, and the terminal emulator — which is
// the thing that actually has a clipboard — performs the write. It works over
// ssh, inside tmux and in a container, which is exactly where a product that
// runs in a terminal is used and exactly where a native clipboard binding is
// not there to be bound.
//
// THE BYTES LEAVE THROUGH BUBBLE TEA, NEVER THROUGH THIS PACKAGE. [Write] is a
// [tea.Cmd] around [tea.SetClipboard], which the runtime executes beside the
// frame it is already writing. Nothing here opens the terminal, and that is the
// same discipline internal/tui2/osc.go keeps for notifications and for the same
// reason: a write that goes around the renderer lands in the middle of whatever
// row it was drawing, and atomic frames exist to make that impossible. [Copy]
// builds the sequence the runtime will write, so the shape is checkable in a
// test without a terminal, and [Written] reads a command's payload back — which
// is how a caller's own test asserts that a click really did reach the
// clipboard, with the text it claimed.
package clip

import (
	"encoding/base64"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// The sequence, spelled once. `52` is the clipboard operation, `c` is the
// system selection (`p`, the X11 primary selection, is a different clipboard
// and not the one a person means by "copy"), and BEL is the terminator: the
// classic OSC ending, understood by every emulator that understands 52 at all.
const (
	oscLead = "\x1b]52;c;"
	oscTail = "\x07"
)

// Limit is how much text one write carries, in bytes before encoding.
//
// The cap is on the ENCODED payload, which is what terminals actually bound:
// tmux and xterm both refuse an OSC 52 whose base64 runs past roughly 74KB, and
// a refused write is a silent one — the clipboard simply still holds whatever it
// held before. So the text is cut to a size whose base64 fits: base64 spends 4
// bytes for every 3, so 55,500 bytes of text encode to 74,000 bytes, just under
// the ceiling with room for the sequence around it.
//
// A cut is not a failure and is not announced. What a person means by copying a
// 200KB record is copying the record; a write that refused it entirely because
// it did not all fit would leave them with nothing at all.
const Limit = 55_500

// Copy is the OSC 52 write for text: the sequence the terminal is given.
//
// It is what [Write] causes the runtime to emit, byte for byte, which is the
// whole point of it being here — the sequence can be asserted on in a test that
// never opens a terminal, and [Write] can still be the ordinary door.
func Copy(text string) string {
	return oscLead + base64.StdEncoding.EncodeToString([]byte(Fit(text))) + oscTail
}

// Fit cuts text to what one write carries, on a rune boundary.
//
// The boundary matters: half of a multi-byte rune is not a character, and a
// paste that ended in one would be handing the reader broken UTF-8 that no
// editor can render. Text that already fits is returned unchanged, with no
// allocation, which is every ordinary message.
func Fit(text string) string {
	if len(text) <= Limit {
		return text
	}
	cut := Limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// Write is the command that puts text on the clipboard.
//
// Empty text returns no command at all: a clipboard write with an empty payload
// is the sequence terminals read as "clear the clipboard", and a copy door that
// found nothing to copy must leave what the reader already had alone.
func Write(text string) tea.Cmd {
	text = Fit(text)
	if text == "" {
		return nil
	}
	// Belt and suspenders. OSC 52 is the right door and the only one that
	// works over ssh — but macOS Terminal.app does not speak it at all, and
	// iTerm2 ships with it OFF, so a local reader's first copy would silently
	// do nothing (user-reported, 2026-08-11). When the host OS has a clipboard
	// tool on PATH, the same text is handed to it too, off the render
	// goroutine, best-effort: two writers racing to store the same bytes
	// cannot disagree about the outcome.
	osCopy(text)
	return tea.SetClipboard(text)
}

// osCopy hands text to the host clipboard tool, when one exists: pbcopy on
// macOS, wl-copy then xclip on Linux. Failure is silent by design — OSC 52
// already left through the renderer, and this is the fallback, not the door.
func osCopy(text string) {
	tool, args := clipTool()
	if tool == "" {
		return
	}
	go func() {
		cmd := exec.Command(tool, args...)
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
	}()
}

// clipTool names the host clipboard writer, once per process.
func clipTool() (string, []string) {
	clipToolOnce.Do(func() {
		for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
			if _, err := exec.LookPath(c[0]); err == nil {
				clipToolName, clipToolArgs = c[0], c[1:]
				return
			}
		}
	})
	return clipToolName, clipToolArgs
}

var (
	clipToolOnce sync.Once
	clipToolName string
	clipToolArgs []string
)

// Written reports the text a message carries to the clipboard, and whether it
// is one at all.
//
// It exists for the surfaces' own tests. A click that copies is only correct if
// the bytes that left are the bytes it promised, and the message the runtime
// acts on is unexported by Bubble Tea — deliberately, since nothing but the
// runtime should be handling it. Reading it back through reflection is the one
// place that fact is worked around, in one function, pinned by a test in this
// package: [TestWriteEmitsAClipboardMessageThisPackageCanRead] fails the day the
// runtime renames or retypes it, rather than the day a surface's copy door
// quietly stops being checked.
func Written(msg tea.Msg) (string, bool) {
	value := reflect.ValueOf(msg)
	if !value.IsValid() || value.Kind() != reflect.String {
		return "", false
	}
	kind := value.Type()
	if kind.PkgPath() != bubbleteaPkg || !strings.HasSuffix(kind.Name(), "ClipboardMsg") {
		return "", false
	}
	return value.String(), true
}

// bubbleteaPkg is the runtime whose clipboard messages [Written] recognizes. A
// string that came from anywhere else is not a clipboard write, however much it
// looks like one.
const bubbleteaPkg = "charm.land/bubbletea/v2"
