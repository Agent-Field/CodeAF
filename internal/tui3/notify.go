package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// THE PING: a turn that ended on a screen nobody is looking at.
//
// The commonest thing a person does with a long turn is start it and go
// somewhere else. The surface's whole vocabulary for "it is done" — the state
// word going idle, the spinner stopping, the reply arriving — is drawn on a
// window they are not looking at, so the fact that the work finished reaches
// them whenever they next happen to look.
//
// So a turn that ends while the terminal is UNFOCUSED sends OSC 777, which is
// the desktop-notification sequence: rxvt's originally, and now what kitty,
// WezTerm, foot, Ghostty and tmux's own passthrough all speak.
//
//	ESC ] 777 ; notify ; <title> ; <body> BEL
//
// A terminal that does not know it prints nothing — an unrecognized OSC is
// swallowed, not displayed, which is what makes this safe to send unasked.
//
// ── FOCUS, AND HOW IT IS KNOWN ──
//
// Bubble Tea v2.0.8 does report focus: [tea.FocusMsg] and [tea.BlurMsg] arrive
// when the View asks for them (`ReportFocus`, set in view.go), carrying the
// terminal's own CSI I / CSI O focus events. So the gate is real and this file
// does not have to fall back to ringing the bell unconditionally — which was
// the alternative, and which would beep at a person watching the turn it was
// telling them about.
//
// The one thing focus reporting cannot say is "this terminal does not do focus
// reporting". There is no negative answer — a terminal that ignores the request
// simply never sends an event — so a surface that has heard NOTHING is treated
// as focused and stays quiet. That is the honest default of the two: the cost
// of this choice is a notification somebody did not get, and the cost of the
// other one is a notification on every turn, forever, on a screen being
// watched. [app.seenFocus] is the seam if that trade is ever re-decided; it is
// already tracked and already false in exactly that case.
//
// There is no settings row, deliberately. A notification that fires only when
// you are not there is not a preference — it is either working or it is not —
// and the honest place for a row would be "notify me even when focused", which
// is a request nobody has made.

// notifyTitle is what the desktop banner is headed with. It is the product's
// own name (styles.go) because a banner has no window chrome to say which
// program it came from.
const notifyTitle = product

// notifyBody is the sentence in the banner. It names the conversation, which is
// the one fact that tells a person WHICH terminal to go back to.
func (a *app) notifyBody() string {
	name := a.title
	if name == "" {
		name = a.place
	}
	if name == "" {
		return "turn done"
	}
	return name + " · turn done"
}

// notifyDone is the command a finished turn returns, or nil when the person is
// already looking at the answer.
func (a *app) notifyDone() tea.Cmd {
	if a.focused {
		return nil
	}
	return tea.Raw(notifySeq(notifyTitle, a.notifyBody()))
}

// notifySeq builds the sequence. The two fields are sanitized for the one
// character that would end the string early — the BEL that terminates it — and
// for the semicolon that separates them; a title with a ';' in it would
// otherwise arrive as a title and half a body.
func notifySeq(title, body string) string {
	return "\x1b]777;notify;" + oscSafe(title) + ";" + oscSafe(body) + "\a"
}

// oscSafe strips what an OSC payload may not carry. It drops rather than
// escapes, because there is no escape form inside an OSC string and a banner
// with a stray backslash in it is worse than one with a missing semicolon.
func oscSafe(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == ';', r == '\a', r == '\x1b', r < ' ':
			continue
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
