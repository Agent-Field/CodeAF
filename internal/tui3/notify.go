package tui3

import (
	"strings"

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

// notifyDoneWord is what the banner says about a turn that has ended. It is
// spelled once so that a turn ending in the conversation on screen and one
// ending in a conversation this process holds but is not drawing cannot be
// described in two different ways.
const notifyDoneWord = "turn done"

// notifyBody is the sentence in the banner. It names the conversation, which is
// the one fact that tells a person WHICH terminal to go back to.
func (a *app) notifyBody() string {
	// The name a person READS, which is the same one the status line draws
	// (names.go): a banner is the one place this conversation is named outside
	// its own window, so it must not be the one place a machine name shows. It
	// is spelled once, in [app.notifyName], because there are two banners now.
	return a.notifyName() + notifyDoneWord
}

// notifyDone is the command a finished turn returns, or nil when the person is
// already looking at the answer.
//
// A FOCUSED TERMINAL IS NO LONGER EVIDENCE THAT ANYBODY IS LOOKING AT THIS
// CONVERSATION. This process can hold several, and the person may be sitting in
// front of a different one — so the suppression is `focused AND in front`, and
// this door is the in-front half of it. The other half is [app.notifyBehind],
// which never suppresses, because a conversation in the keeper is by
// construction not the one being read.
func (a *app) notifyDone() tea.Cmd {
	if a.focused {
		return nil
	}
	return tea.Raw(notifySeq(notifyTitle, a.notifyBody()))
}

// notifyBehind is the banner for a conversation this process holds and is not
// drawing: the same two sentences, named with that conversation rather than
// with the one on screen.
func (a *app) notifyBehind(held *kept, word string) tea.Cmd {
	name := humanName(Session{File: held.conv.SessionFile})
	if title := agentTitle(held.conv.Agent); title != "" {
		name = title
	}
	if name == "" {
		name = held.conv.Place
	}
	if name != "" {
		name += " · "
	}
	return tea.Raw(notifySeq(notifyTitle, name+word))
}

// agentTitle is the name a conversation gave itself, or "" for one that has not
// been named yet.
func agentTitle(agent Agent) string {
	if agent == nil {
		return ""
	}
	return strings.TrimSpace(agent.Title())
}

// ── THE SECOND BANNER: A QUESTION NOBODY CAN SEE ────────────────────────────
//
// A finished turn is not the only thing that happens on a screen a person has
// walked away from. The approval gate stops one tool call and BLOCKS it
// (session's consent.go), and while that question is up the session is doing
// nothing at all — which is the one state where "you will find out when you next
// look" is the wrong bargain, because nothing is going to happen in the meantime
// to make the looking worthwhile.
//
// It used to be answered FOR them: the countdown denied the call ten seconds
// later on a window nobody was reading, so an unfocused session looked like a
// session that had simply stopped working. [app.tickAsk] no longer runs that
// clock while the window is blurred, which is right and which is also why this
// banner has to exist — a question that now waits indefinitely must be a
// question the person was told about.

// notifyAskWord is what the banner says a conversation is doing. It is the
// presence file's own word for the same state (session's taskpresence.go's
// PresenceWaiting), because the sentence on the desktop and the row on another
// window's home page are describing one fact and must not spell it two ways.
const notifyAskWord = "waiting on you"

// notifyAsk is the command a raised question returns, or nil when the person is
// already looking at the question.
func (a *app) notifyAsk() tea.Cmd {
	if a.focused {
		return nil
	}
	return tea.Raw(notifySeq(notifyTitle, a.notifyName()+notifyAskWord))
}

// notifyName is the conversation's name and the separator that follows it, or
// "" when nothing here has a name worth putting in a banner. Both banners are
// built from it so that a session named in one is named in the other.
func (a *app) notifyName() string {
	name := a.sessionName()
	if name == "" {
		name = a.place
	}
	if name == "" {
		return ""
	}
	return name + " · "
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
