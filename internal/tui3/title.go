package tui3

import (
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE TERMINAL'S OWN TAB SAYS WHERE A PERSON IS INSIDE AFORGE.
//
// A terminal title is read exactly when nobody is looking at the window — in a
// tab bar, the cmd-tab switcher, a tmux status line — so it answers the one
// question a glance from outside can use: which place in aforge is this tab
// standing in, and is anything in it waiting on me. It is the place and the
// thing's own name, then the product, so a tab bar truncating from the right
// gives up the product first and the name last:
//
//	aforge                              home, nothing waiting
//	3 want you · aforge                 home, three things stopped on a person
//	porting the parser · aforge         a conversation
//	new conversation · aforge           one that has not been named yet
//	? porting the parser · aforge       one waiting on the person
//	Fix the nil-map crash · task · aforge   a task room
//	memory · aforge                     any other place, by its own word
//	porting the parser @ devbox · aforge    any of them over --host
//
// WHAT IT DELIBERATELY IS NOT IS A STATUS LINE. No spinner, no model, no cost,
// no clock: each would move the one piece of chrome a person cannot scroll away
// from, to say something that is not worth a glance from another workspace. So
// the title changes when a FACT changes — a place entered, a name arriving, a
// question coming up — and never on a timer.
//
// AND IT IS SENT TWICE OVER, ON PURPOSE, BECAUSE TERMINALS DISAGREE ABOUT WHICH
// TITLE A TAB WEARS. Bubble Tea declares the title as OSC 2, the window title,
// and that is what tmux, kitty, Ghostty and the window bar of every terminal
// read. Terminal.app and iTerm2 label a TAB with the icon name, OSC 1, and until
// this file wrote one nothing did — so the owner's Terminal.app tab read
// `<home>/…`, the running binary's path, beside a shell tab whose own
// program had set a real name. [app.retitle] writes OSC 1 and [app.View]
// declares OSC 2, and both are the same sentence ([app.titleSent]).

// titleMax bounds the sentence in cells. A tab shows twenty-odd characters and
// a window bar under a hundred; past sixty the bytes are pure cost down a
// channel that exists to say one line. It is the NAME that is cut to fit, never
// the suffix: ` · aforge` is how a person tells an aforge tab from a shell's.
const titleMax = 60

// The words the sentence adds to the names it borrows, quoted in
// internal/manual/chat/screen.md exactly as they are spelled here.
const (
	// titleTaskWord follows a task's label, so a task room's tab cannot be read
	// as a conversation that happens to share its name.
	titleTaskWord = "task"
	// titleHostGap puts the machine a hosted session runs on after its name.
	titleHostGap = " @ "
)

// titleParts is where a person is, as the sentence spells it: a mark in front
// (the needs-human glyph's ASCII spelling, or nothing), the name a tab bar may
// cut, and the kind of thing it names, which is never cut.
type titleParts struct{ mark, name, kind string }

// terminalTitle is the ONE FUNCTION THAT OWNS THE SENTENCE. Every part of it is
// borrowed from the reading that already draws the same fact inside the frame —
// the pulse's count, the tab strip's name and its needs-person mark, the task
// page's heading, the place bar's word — so the outside of the window and the
// inside of it cannot come to disagree.
//
// PLAIN TEXT ONLY: the title goes to the operating system, not the frame, so no
// ANSI, no icon glyph, and no control byte a name could smuggle into an OSC
// string ([oscSafe]).
func terminalTitle(a *app) string {
	p := titleWhere(a)
	suffix := pulseGap + product
	if a.hosted() {
		suffix = titleHostGap + oscSafe(a.host) + suffix
	}
	name := oscSafe(p.name)
	kind := ""
	switch {
	case p.kind != "" && name != "":
		kind = pulseGap + p.kind
	case p.kind != "":
		name = p.kind
	}
	if name == "" {
		// HOME AT REST IS THE PRODUCT'S BARE NAME, which is the emptiness law
		// applied to the space outside the frame: nothing waiting says nothing.
		if a.hosted() {
			return product + titleHostGap + oscSafe(a.host)
		}
		return product
	}
	room := titleMax - ansi.StringWidth(p.mark) - ansi.StringWidth(kind) - ansi.StringWidth(suffix)
	return p.mark + fit(name, room) + kind + suffix
}

// titleWhere reads where the person is standing, in the order the frame itself
// decides it (view.go's [app.frameBody]): the first-run setup, then a place,
// then a task room, and otherwise the conversation.
func titleWhere(a *app) titleParts {
	switch {
	case a.setup.open:
		return titleParts{}
	case a.at(pageHome):
		// THE PULSE'S OWN COUNT AND ITS OWN WORDS, read off the same reading
		// the top line draws from ([app.machine], taken on a beat by
		// [app.readMachine]), so the tab and the line under it are one reading
		// and never two counts.
		if wants := a.machine.wants; wants > 0 {
			return titleParts{name: itoa(wants) + pulseWantWord}
		}
		return titleParts{}
	case a.showing() != nil:
		return titleParts{name: a.page.word()}
	case a.roomOpen():
		return titleParts{name: a.roomHereWord(), kind: titleTaskWord}
	}
	p := titleParts{name: titleConversation(a)}
	// THE TAB STRIP'S OWN NEEDS-PERSON READING ([app.frontSignal]): a question
	// in this conversation, or a task proposal that is really waiting rather
	// than counting down. The mark is the vocabulary's ASCII spelling, through
	// the glyph door, because the title is drawn by the operating system in a
	// font that has never heard of the icon tiers.
	if a.frontSignal() == tabNeedsPerson {
		p.mark = tokens.ASCII.Glyph(tokens.GNeedsHuman) + " "
	}
	return p
}

// titleConversation is the conversation's name as the tab strip spells it
// ([app.chatTabDisplayName]). Before naming finishes, it uses the same
// placeholder every other conversation-name surface uses.
func titleConversation(a *app) string {
	if strings.TrimSpace(a.shortTitle) == "" && a.sessionName() == "" {
		return unnamedConversationWord
	}
	return a.chatTabDisplayName()
}

// retitle is the ONE PLACE THE SENTENCE IS SENT, and it sends it only when it
// changed: a title written on every message is a title flickering in some
// terminals, and a surface repainting thirty times a second while a turn runs
// would otherwise say the same line thirty times a second down the channel.
//
// It is called from [app.Update] after every message, because the title is a
// command and View cannot return one. A message the pointer fold swallowed
// changed nothing the frame reads (coalesce.go's [pointerFold.still]), so it
// cannot have changed the title either and is not even asked.
func (a *app) retitle() tea.Cmd {
	if a.ptr.still {
		return nil
	}
	title := terminalTitle(a)
	if title == a.titleSent {
		return nil
	}
	a.titleSent = title
	return titleSend(title)
}

// titleSend is the tab's half of the sentence: OSC 1, the icon name, which is
// what Terminal.app and iTerm2 draw on a tab. The window's half is declared on
// the view (view.go), where Bubble Tea's renderer compares it with the last
// frame's and writes OSC 2 only when it moved.
func titleSend(title string) tea.Cmd { return tea.Raw(ansi.SetIconName(title)) }

// titleFarewell is written to the terminal after the program has stopped, on
// EVERY road out — the ordinary quit, a signal, a cancelled context. A title
// left behind is a lie about a process that is gone: Terminal.app keeps an icon
// name until something replaces it, and a shell prompt that does not set one
// would leave the tab saying `aforge` over a shell. The empty string hands the
// tab back to the terminal's own default. Bubble Tea's renderer already clears
// the window title it declared as it closes, so this is the tab's half only.
func titleFarewell(out io.Writer, a *app) {
	if a.titleSent == "" {
		return
	}
	_, _ = io.WriteString(out, ansi.SetIconName(""))
}
