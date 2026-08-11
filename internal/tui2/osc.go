package tui2

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// The terminal protocols the surface speaks outside the frame.
//
// Everything in this file builds bytes that leave the program WITHOUT passing
// through internal/sanitize, and that is deliberate: the sanitizer's job is to
// neuter somebody else's escape sequences on their way to the screen, and
// running our own chrome through it would strip exactly the sequences this file
// exists to emit. The two are opposite ends of one rule — content is
// sanitized, chrome is authored — and the rule holds only because these
// functions are the ONLY place the surface authors an OSC, and because every
// piece of text they interpolate is filtered by [oscText] first (10.2.6).
//
// The distinction that makes that filter necessary rather than decorative:
// sanitize.Text KEEPS SGR sequences, because a colored row is a legitimate
// row. An SGR sequence begins with ESC. An ESC inside an OSC payload ENDS the
// OSC string, and everything after it spills into the frame as printable
// garbage. So a notification title cannot be sanitized the way a row is; a
// payload's rule is stricter than a row's, and [oscText] is that stricter rule.

// The payload bounds. A notification is an interruption, not a document: a
// title that does not fit a desktop notification's one line is a title nobody
// reads, and a body assembled from a runaway error message is a kilobyte down
// a channel that exists to say one sentence.
const (
	oscTitleMax = 120
	oscBodyMax  = 240
	oscURIMax   = 2048
)

// oscText makes a string safe to carry inside an OSC payload: printable runes
// only, no ';', bounded length.
//
// The three rules, and why each one is not paranoia:
//
//   - Control bytes go. ESC would terminate the OSC string early (see the file
//     comment); BEL would too, since it is the classic OSC terminator; the rest
//     of C0/C1 are the same class of thing and none of them mean anything in a
//     notification title.
//   - ';' becomes ','. Every OSC we emit is a semicolon-separated field list,
//     and a ';' arriving inside a payload is a field the terminal reads as a
//     parameter we did not write. One rule for every sequence is calmer than a
//     per-sequence escape table, and a comma reads the same to a human.
//   - The length is bounded, in runes, and the cut is clean: a multi-byte rune
//     is never severed, so the payload is always valid UTF-8.
//
// Text that needs none of this — the ordinary case — is returned unchanged with
// no allocation.
func oscText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if clean, runes := oscClean(s); clean && runes <= max {
		return s
	}
	var b strings.Builder
	b.Grow(min(len(s), max*4))
	n := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == utf8.RuneError && size == 1:
			// A byte that is not valid UTF-8. Dropping it keeps the payload
			// decodable, which is the whole point of walking runes here.
			continue
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			continue
		case r == ';':
			b.WriteByte(',')
		default:
			b.WriteRune(r)
		}
		n++
		if n >= max {
			break
		}
	}
	return b.String()
}

// oscClean reports whether s already satisfies oscText's rules, and how many
// runes it has. It is the fast path: one pass, no allocation, and the answer to
// "do we have to build anything at all".
func oscClean(s string) (bool, int) {
	n := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return false, n
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == ';' {
			return false, n
		}
		i += size
		n++
	}
	return true, n
}

// oscSafeURI reports whether a URI may be put inside an OSC 8 link. Unlike a
// title, a URI is not repaired — a link whose target we had to edit is a link
// that points somewhere other than where the caller said, which is the exact
// failure OSC 8 spoofing is about. So the answer is yes or no, and a no renders
// as plain text (5.21's "graceful plain text where not").
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

// notifyProbe asks whether the terminal speaks the kitty desktop-notification
// protocol (OSC 99). A terminal that knows it answers with an OSC 99 of its
// own; a terminal that does not consumes the sequence and prints nothing, which
// is a correct answer and the reason this is safe to send blind.
//
// The id is ours so that an answer is recognizably to our question, and the
// query carries no payload — this asks, it does not notify.
const notifyProbe = "\x1b]99;i=" + oscAppID + ":p=?;\x1b\\"

// oscAppID identifies our notifications to a terminal that groups or replaces
// them by id. It is not the window title (that is [TerminalOptions.AppName],
// which the operator may change); it is a protocol identifier, so it is fixed.
const oscAppID = "aforge"

// notifyTier is which rung of the notification ladder a terminal has earned.
// The ladder is 10.5.27's, and it is short on purpose.
type notifyTier uint8

const (
	// tierBell is the floor: a BEL. It is what every terminal and every
	// multiplexer in history does something with, and it carries no text.
	tierBell notifyTier = iota
	// tier777 is the widely-implemented OSC 777 "notify" extension. It carries
	// a title and a body and needs no probe, but it is only reached when we are
	// talking to the emulator directly.
	tier777
	// tier99 is the kitty desktop-notification protocol, and the only rung we
	// have positive evidence for, because it is the only one that answers a
	// question.
	tier99
)

func (t notifyTier) String() string {
	switch t {
	case tier99:
		return "osc99"
	case tier777:
		return "osc777"
	default:
		return "bell"
	}
}

// notifyTier picks the rung.
//
// The middle rung is where the judgement is. OSC 777 cannot be probed, so
// sending it is a bet — and the bet is only sound when we are speaking to the
// emulator directly, because an unknown OSC is consumed silently by whoever
// reads it first. Inside a multiplexer, that reader is the multiplexer: the
// sequence vanishes there, the emulator never sees it, and the user gets
// NOTHING instead of a notification. A BEL, which every multiplexer forwards
// or turns into its own activity signal, is the honest floor for that case —
// silence is worse than a bell (10.1.2's degrade-safely rule, applied to the
// interruption budget).
func (c Capabilities) notifyTier() notifyTier {
	if c.Notification == supportYes {
		return tier99
	}
	if c.Mux == MuxNone {
		return tier777
	}
	return tierBell
}

// notifyBytes renders one notification at one tier. It is pure: the same
// arguments give the same bytes, which is what makes the capability matrix a
// table test rather than a terminal.
func notifyBytes(tier notifyTier, title, body string) string {
	title = oscText(title, oscTitleMax)
	body = oscText(body, oscBodyMax)
	switch tier {
	case tier99:
		switch {
		case title == "" && body == "":
			return ""
		case body == "":
			return ansi.DesktopNotification(title, "i="+oscAppID, "p=title")
		case title == "":
			return ansi.DesktopNotification(body, "i="+oscAppID, "p=body")
		default:
			// Two chunks of one notification: d=0 says "more of this is
			// coming", d=1 closes it. The id is what joins them.
			return ansi.DesktopNotification(title, "i="+oscAppID, "d=0", "p=title") +
				ansi.DesktopNotification(body, "i="+oscAppID, "d=1", "p=body")
		}
	case tier777:
		if title == "" && body == "" {
			return ""
		}
		var b strings.Builder
		b.Grow(len(title) + len(body) + 16)
		b.WriteString("\x1b]777;notify;")
		b.WriteString(title)
		if body != "" {
			b.WriteByte(';')
			b.WriteString(body)
		}
		b.WriteByte(0x07)
		return b.String()
	default:
		return "\a"
	}
}

// progressBytes is OSC 9;4, the taskbar progress protocol (10.5.27: progress
// goes here and to the title, and NEVER to a desktop notification).
//
// There are two states and no percentage. A turn has no denominator — an agent
// does not know how many tokens its answer will be — so a bar that filled to
// 60% would be an estimate rendered as a fact, which 10.2.8 refuses. The
// indeterminate state is the honest shape of "something is running".
//
// Nothing here is probed, because OSC 9;4 has no query: it is write-only. What
// makes that safe is that an unknown OSC is consumed rather than printed, so
// the sequence costs six bytes and changes nothing on a terminal that has never
// heard of it. tmux 3.7 forwards it (10.1.2), and older tmux swallows it — the
// same six bytes, the same nothing.
func progressBytes(running bool) string {
	if running {
		return ansi.SetIndeterminateProgressBar
	}
	return ansi.ResetProgressBar
}

// promptMarkBytes is OSC 133 A: "a prompt zone starts here" (7.2).
//
// This is the whole of our shell-integration vocabulary, and it is one mark
// rather than the protocol's four. A/B/C/D describe a shell's command
// lifecycle — prompt, input, output, exit code — and three of those four have
// no honest referent in a chat: there is no command line and no exit status,
// and emitting D with a made-up status would be shell-integration cosplay.
// What we do have is the thing the feature is actually for: every terminal's
// "jump to previous prompt" keys on A marks, so one A per committed user
// message makes ctrl-shift-Z (and its friends) walk the conversation.
const promptMarkBytes = "\x1b]133;A\x07"

// Linker renders OSC 8 hyperlinks for TRUSTED chrome (5.21).
//
// Two boundaries are load-bearing and neither is negotiable:
//
//   - It links a label the caller authored to a URI the caller authored. It
//     does not scan text for paths or URLs and rewrite them. A helper that
//     rewrote content would be minting links out of somebody else's bytes,
//     which is the exact thing internal/sanitize strips OSC 8 to prevent —
//     the sequence was never the threat, untrusted text minting one was.
//   - It degrades to the plain label, always: when hyperlinks are off, when the
//     URI is empty, and when the URI carries anything that could break out of
//     the sequence. There is no state to reset and nothing half-emitted.
//
// The zero Linker is a working Linker that renders plain text, which is why a
// renderer can hold one without asking whether it was given one.
//
// OSC 8 is also the one sequence in this file that travels INSIDE frame
// content rather than beside it: the cell buffer under the compositor carries a
// hyperlink as a cell attribute, so a link written into a pane's rows survives
// composition and reaches the terminal through the ordinary diffing renderer.
// Everything else here is out-of-band, because a cell buffer has nowhere to put
// a notification.
type Linker struct{ on bool }

// Wrap returns label as a hyperlink to uri, or label unchanged when this
// terminal, this build or this URI does not support one.
func (l Linker) Wrap(label, uri string) string {
	if !l.on || label == "" || !oscSafeURI(uri) {
		return label
	}
	return ansi.SetHyperlink(uri) + label + ansi.ResetHyperlink()
}

// Enabled reports whether Wrap will actually link. Renderers that lay out to a
// column width do not need it — a hyperlink occupies no cells — but a renderer
// deciding whether to show a "click to open" affordance does.
func (l Linker) Enabled() bool { return l.on }

// attentionTitle is the terminal title (7.2): the app name, and the number of
// things waiting for a human, and nothing else.
//
// It is deliberately not a status line. A title that carried the model, the
// room and the elapsed time would redraw on every token, and the title bar is
// the one piece of chrome the user cannot scroll away from — so it says the one
// fact that is worth a glance from another workspace, which is whether anything
// is waiting. Zero waiting renders as the bare name, so the ordinary state is
// the quiet one.
func attentionTitle(app string, n int) string {
	if app == "" {
		app = defaultAppName
	}
	if n <= 0 {
		return app
	}
	if n > 99 {
		return app + " (99+)"
	}
	return app + " (" + strconv.Itoa(n) + ")"
}
