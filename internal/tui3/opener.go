package tui3

import (
	"errors"
	"os/exec"
	"runtime"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

// THE TWO WAYS A LINK LEAVES THIS SURFACE.
//
// A sign-in finishes in a browser, and a terminal has two honest ways to get a
// person there:
//
//   - HAND IT TO THE PLATFORM. `open` on a Mac, `xdg-open` on Linux: the same
//     port internal/tui already makes (its clipboard.go), because a second
//     spelling of "open this" is a second thing to keep in step with the
//     platforms.
//   - WRITE IT DOWN. The link is drawn as text, always, whether or not the
//     handoff worked — and the text is wrapped in OSC 8 where the sequence is
//     safe, so a terminal that understands hyperlinks makes it clickable and one
//     that does not shows exactly the characters a person can select and copy.
//     THIS IS THE SSH CASE and it is not an edge: a browser opened on the far
//     end of a connection is a browser nobody is sitting at.
//
// Neither of them is trusted with the other's job. The handoff can fail on a
// headless box and the link stays; the link can be unclickable and the browser
// still opened. What must never happen is a sign-in a person cannot reach.

// processOpener is where this file reaches out of the program. It is a variable
// so a test can watch what would have been opened without a browser appearing
// on the machine running it — the same seam, and the same reason, as
// internal/tui's.
var processOpener = startOpener

// openerCommand is what this platform calls "open this". An empty name is a
// platform with no answer, which is a fact the caller reports rather than
// papers over.
func openerCommand() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "linux":
		return "xdg-open", nil
	}
	return "", nil
}

// startOpener hands the target to the platform and does not wait for whatever
// opens it: a browser left open must not hold a goroutine here.
func startOpener(target string) error {
	name, args := openerCommand()
	if name == "" {
		return errors.New("this machine has no way to open a browser")
	}
	command := exec.Command(name, append(append([]string(nil), args...), target)...)
	if err := command.Start(); err != nil {
		return errors.New("the browser did not open")
	}
	guard.Go("tui3/open", func() { _ = command.Wait() })
	return nil
}

// ── the link, as text ───────────────────────────────────────────────────────

// oscURIMax is the longest link this surface will put inside an OSC 8. It is
// internal/tui2's number, and it is a bound on the SEQUENCE rather than a
// judgement about links: an escape sequence a terminal decides is too long is a
// sequence it may print instead of consume.
const oscURIMax = 2048

// oscSafeURI reports whether a link may go inside an OSC 8.
//
// It is a yes or a no and never a repair. A link whose target we had to edit is
// a link that points somewhere other than where the caller said, which is the
// exact failure OSC 8 spoofing is about — so a no renders as plain text, which
// is what every caller here draws anyway.
func oscSafeURI(uri string) bool {
	if uri == "" || len(uri) > oscURIMax {
		return false
	}
	for i := 0; i < len(uri); {
		r, size := utf8.DecodeRuneInString(uri[i:])
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return false
		}
		i += size
	}
	return true
}

// linkOpen is the sequence that starts an anchor, or the empty string when the
// link may not be written — which is the caller's signal to draw plain text and
// emit no [linkClose] either. A dangling opener is the one failure mode of OSC 8
// that a person actually sees: everything after it, to the end of the screen,
// becomes one link.
func linkOpen(uri string) string {
	if !oscSafeURI(uri) {
		return ""
	}
	return ansi.SetHyperlink(uri)
}

// linkClose ends an anchor. It is a function rather than a constant so that the
// two halves of a link are spelled by the same package, and a caller can never
// close with bytes that do not match what opened.
func linkClose() string { return ansi.ResetHyperlink() }

// linkify wraps a label as a hyperlink to uri, and returns the label untouched
// when the sequence would not be safe.
//
// A hyperlink occupies no cells, so a caller that has already fitted its label
// to a width does not have to measure again — which is the whole reason this is
// applied last, to text the layout has finished with.
func linkify(label, uri string) string {
	open := linkOpen(uri)
	if label == "" || open == "" {
		return label
	}
	return open + label + linkClose()
}
