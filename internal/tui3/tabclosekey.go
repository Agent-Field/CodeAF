package tui3

import tea "charm.land/bubbletea/v2"

// ── CLOSING THE TAB FROM THE KEYBOARD ───────────────────────────────────────
//
// closeTabChord is `ctrl+w`, and it is the ✕ on the tab in front said with the
// keyboard instead of with the pointer.
//
// IT IS THE BROWSER'S OWN READING OF THE KEY, on a row that is drawn as tabs and
// beside the chord that opens one ([newChatChord] is `ctrl+t`, chatstart.go). A
// person who has ctrl+t for a new tab reaches for ctrl+w to shut one without
// being told to, and until this wave the only way to shut one was the pointer or
// a trip through the switcher card.
//
// IT CLOSES NOTHING AND INTERRUPTS NOTHING, because it is one call into
// chattabs.go's [app.tabDismiss] and that function holds the whole of the law:
// the agent goes on running, the unsent sentence and its caret stay where they
// were, the transcript is untouched, and the conversation is still on the
// switcher. Reopening it from home or from `ctrl+k` brings the tab and the draft
// straight back. There is deliberately no second lifecycle here — a close that
// preserved drafts its own way would be a second answer to a question the ✕ has
// already answered, kept in step with the first by nothing.
//
// SO IT IS SAFE TO LEAN ON. Pressing it again is clicking the ✕ again: the next
// conversation this window holds comes forward and can be shut in turn, and the
// last one leaves the window on home with that conversation still alive behind
// it.
const closeTabChord = "ctrl+w"

// closeTabKey is that chord's whole claim on the keyboard, and it reports
// whether it took the key.
//
// IT IS READ UNDER EVERY MODAL AND OVER THE MESSAGE BOX (input.go), which is the
// rung [app.newChatKey] is read at and for the same reasons. Under the modals,
// because a filter box, the switcher card and a page that has taken the whole
// frame each spell ctrl+w their own way and are looking at the person when they
// press it — the switcher's own ctrl+w puts the row under its cursor away
// (hop.go), and every filterable overlay edits its search with it. Over the box,
// because a chord is never a letter of anybody's sentence.
//
// THE WORD KILL KEEPS THE TWO NAMES PEOPLE ACTUALLY PRESS. `alt+backspace` is
// what a Mac keyboard sends and `ctrl+backspace` is what Windows sends, and both
// still delete the word behind the caret in the message box and in every filter
// (input.go). What this chord takes is readline's third spelling of that edit,
// which is the one a terminal person shares with their shell — and the trade is
// stated on the key sheet and in the manual rather than left to be discovered.
//
// IT ANSWERS FROM A HELD ROSTER AS WELL, for [app.newChatKey]'s reason: the
// roster gives the keyboard back on the way, because the screen about to be
// drawn is a different conversation or home, and a keyboard pointed at a list
// nobody can see is the bug chordfocus.go states.
func (a *app) closeTabKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != closeTabChord {
		return nil, false
	}
	if a.railHold {
		a.railTake(false)
	}
	return a.tabDismiss(a.frontChatTab()), true
}

// frontChatTab is the tab whose ✕ this chord presses.
//
// THE NEW-CHAT PAGE OWNS A SYNTHETIC TAB and it is the one in front while that
// page is up (chattabs.go's [app.tabsRow] appends it). Its key is empty on
// purpose — nothing has been created yet — so it is named by its `start` flag
// and closed the way the ✕ on it closes it, which is `esc`: the page comes down
// and the conversation underneath comes back with its draft, its tray and its
// reading position exactly as they were ([app.cancelChatStart]). Asking
// [app.frontTabKey] there would name the conversation BEHIND the page and shut
// the wrong thing.
//
// Otherwise it is the conversation on screen, identified exactly as the switcher
// identifies it when it puts the row you are standing on away (hop.go's
// [app.hopAway]): the front key, the file it was taken from, the workspace it
// runs in, and the name this window is showing for it.
func (a *app) frontChatTab() chatTab {
	if a.startingChat() {
		return chatTab{here: true, start: true}
	}
	return chatTab{
		key:   a.frontTabKey(),
		file:  a.file,
		where: a.workspace,
		word:  a.chatDisplayName(),
		here:  true,
	}
}
