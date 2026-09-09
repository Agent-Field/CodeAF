package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

type twoTitleAgent struct {
	*fakeAgent
	full  string
	short string
}

func (a *twoTitleAgent) Title() string      { return a.full }
func (a *twoTitleAgent) ShortTitle() string { return a.short }

// WHAT A CONVERSATION IS CALLED BEFORE IT HAS EARNED A NAME, AND WHEN THE NAME
// ARRIVES.
//
// A session starts naming itself asynchronously on its first accepted message
// (internal/session's title.go states the retry and lifetime bounds). While the
// name is being written, the name column has to say SOMETHING — and what
// it said was `main`, which is this surface's word for a PLACE (`esc/← main`,
// [refusalMainDoor]'s "say it to main") and not a name at all. A person with one
// unnamed conversation open read a tab claiming their chat was called `main`.
//
// These tests pin the two halves of the fix that are the title lifecycle's own:
// the word an unnamed conversation is drawn under, and the arrival of the real
// name on the event the namer already sends. NOTHING HERE MAY MAKE A MODEL CALL
// AND NOTHING IN THE SURFACE DOES — the second half is a field being written by
// [session.EventTitleChanged] and read on the next frame.

// The unnamed conversation is drawn under the SAME word the switcher and entry
// line use, and never under the word this surface uses for the conversation as
// a place.
func TestAnUnnamedConversationIsNotDrawnUnderThePlaceWord(t *testing.T) {
	a := newTestApp(&fakeAgent{})

	if got := a.chatDisplayName(); got != unnamedConversationWord {
		t.Fatalf("an unnamed conversation is called %q, not %q", got, unnamedConversationWord)
	}
	if unnamedConversationWord == roomCrumbRoot {
		t.Fatalf("the name placeholder and the place word are the same string %q — "+
			"a person cannot tell a conversation with no name from one that is called that",
			roomCrumbRoot)
	}
	// AND THE PLACE WORD SURVIVES WHERE IT IS ACTUALLY A PLACE. The refusal every
	// task surface falls back on names the conversation as somewhere words can go
	// (roomrefusal.go), and a fix that renamed the placeholder by moving
	// [roomCrumbRoot] would have taken that sentence with it.
	if !strings.Contains(refusalMainDoor, roomCrumbRoot) {
		t.Fatalf("the door %q stopped naming the conversation as a place", refusalMainDoor)
	}
}

// The name the session gives itself arrives on an event, independently of the turn
// that triggered it, and the surface takes it without being asked twice.
func TestTheNameArrivesOnItsEventAndReplacesThePlaceholder(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	if got := a.chatDisplayName(); got != unnamedConversationWord {
		t.Fatalf("the conversation started out called %q", got)
	}

	// This is the whole of the road: the namer starts beside the first
	// answer and sends its event when ready, [app.applyEvent] hands it to
	// [app.setTitle], and the next read of the name is the name.
	a.applyEvent(session.Event{Kind: session.EventTitleChanged, Text: "porting the parser"}, false)

	if got := a.chatDisplayName(); got != "porting the parser" {
		t.Fatalf("the name the session gave itself did not reach the surface: %q", got)
	}
	if got := a.sessionName(); got != "porting the parser" {
		t.Fatalf("the status line and the window title read %q", got)
	}
}

func TestTabsUseTheCompactNameWhileConversationSurfacesKeepTheFullTitle(t *testing.T) {
	agent := &twoTitleAgent{fakeAgent: &fakeAgent{}, full: "agentfield repository star growth analysis", short: "star growth"}
	a := newTestApp(agent)
	a.width, a.height = 120, 40
	strip := plain(a.tabsRow(a.width))
	if !strings.Contains(strip, "star growth") || strings.Contains(strip, "repository star growth analysis") {
		t.Fatalf("tab strip did not use the compact title: %q", strip)
	}
	if got := a.sessionName(); got != "agentfield repository star growth analysis" {
		t.Fatalf("conversation title = %q", got)
	}

	a.applyEvent(session.Event{Kind: session.EventTitleChanged, Text: "github organization star history and notable followers", ShortTitle: "star history"}, false)
	if strip = plain(a.tabsRow(a.width)); !strings.Contains(strip, "star history") || strings.Contains(strip, "notable followers") {
		t.Fatalf("updated tab strip did not use the compact title: %q", strip)
	}
	if got := a.sessionName(); got != "github organization star history and notable followers" {
		t.Fatalf("updated conversation title = %q", got)
	}
}

// A NAME IS NEVER REPLACED BY THE PLACEHOLDER. The placeholder answers exactly
// one question — this conversation has no name yet — and a conversation that has
// one is past that question for good, whoever minted the name.
func TestANamedConversationIsNeverRedrawnAsUnnamed(t *testing.T) {
	for _, name := range []string{
		// The session namer's own shape: eight lowercase words.
		"porting the parser",
		// A name somebody typed, which this surface must not re-spell.
		"Q3 billing",
		// A name that arrived welded, which is read back as words WHERE IT IS
		// DRAWN and is still a name (names.go's [readableName]).
		"port_b_parser_fix",
	} {
		if got := chatTabName(name); got == unnamedConversationWord {
			t.Fatalf("the named conversation %q was drawn as unnamed", name)
		}
	}
	if got := chatTabName("Q3 billing"); got != "Q3 billing" {
		t.Fatalf("a name somebody chose was re-spelled as %q", got)
	}
	// AND WHITESPACE IS NOT A NAME. A title line that is a space is the same
	// fact as no title line, and drawing a blank tab would be a door with
	// nothing written on it (chattabs.go drops one).
	if got := chatTabName("   "); got != unnamedConversationWord {
		t.Fatalf("a blank name drew %q rather than the placeholder", got)
	}
}

// AND THE NEW-CHAT PAGE KEEPS ITS OWN WORD. `+` opens a page and makes nothing —
// no agent, no session file, nothing to name — so its tab says what the page IS
// and must not borrow the placeholder for a conversation that does not exist
// (chatstart.go's first law). The conversation standing behind it keeps its own
// name and its own draft.
func TestTheStartPageTabIsNotTheUnnamedConversation(t *testing.T) {
	a := newStartLab(t).a
	a.applyEvent(session.Event{Kind: session.EventTitleChanged, Text: "porting the parser"}, false)
	a.width, a.height = 120, 40

	a.openChatStart()
	if !a.startingChat() {
		t.Fatalf("the start page did not open")
	}
	strip := plain(tabsRowOf(a))
	if !strings.Contains(strip, "New chat") {
		t.Fatalf("the start page's tab does not say what the page is:\n%q", strip)
	}
	if strings.Contains(strip, unnamedConversationWord) {
		t.Fatalf("the start page borrowed the placeholder for a conversation that does not exist:\n%q", strip)
	}
	// AND THE CONVERSATION BEHIND IT IS STILL ON THE ROW UNDER ITS OWN NAME.
	if !strings.Contains(strip, "porting the parser") {
		t.Fatalf("the conversation behind the start page lost its name:\n%q", strip)
	}
}

// The actual tab and task trail must agree before and after the title event.
func TestTitleArrivalUpdatesTheTabAndTaskBreadcrumbTogether(t *testing.T) {
	a := newTestApp(&fakeAgent{})
	a.width, a.height = 120, 40
	if got := plain(tabsRowOf(a)); !strings.Contains(got, unnamedConversationWord) {
		t.Fatalf("missing unnamed tab: %q", got)
	}
	if a.chatCrumbWord() != unnamedConversationWord {
		t.Fatal("breadcrumb disagrees with unnamed tab")
	}
	a.applyEvent(session.Event{Kind: session.EventTitleChanged, Text: "porting the parser"}, false)
	got := plain(tabsRowOf(a))
	if !strings.Contains(got, "porting the parser") || strings.Contains(got, unnamedConversationWord) || a.chatCrumbWord() != "porting the parser" {
		t.Fatalf("title event did not update the visible navigation: %q", got)
	}
}
