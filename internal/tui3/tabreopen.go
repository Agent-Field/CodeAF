package tui3

import tea "charm.land/bubbletea/v2"

// ── REOPENING A TAB YOU JUST SHUT ───────────────────────────────────────────
//
// reopenTabChord is `ctrl+shift+t`, and it is the third key of the browser's own
// tab grammar: `ctrl+t` opens one (chatstart.go), `ctrl+w` shuts one
// (tabclosekey.go), and this one puts the last thing you shut back. A person who
// closed the wrong tab reaches for it without being told to, and until this wave
// the only way back was to remember which conversation it had been and find it
// again on `ctrl+k`.
//
// A held local conversation returns with its draft through the keeper. A
// remembered remote conversation resumes through the existing connection's open
// door, with its normal session-switch semantics. Refusal leaves the entry on
// the stack so the same key can retry it.
//
// ── WHAT IS ON THE STACK, AND WHAT DELIBERATELY IS NOT ──────────────────────
//
// The stack is [app.closedTabs]: the conversations whose tab this window shut, in
// the order it shut them, most recent last. Pressing the chord repeatedly walks
// back through them in reverse closure order.
//
// THE NEW-CHAT PAGE IS NOT ON IT. Its tab is synthetic — nothing has been created
// yet, so it has no key, no transcript and no conversation to come back to — and
// closing it parks its half-written first message through
// [app.cancelChatStart], which `ctrl+t` hands straight back the next time the
// page opens. So the page's own way back is the chord that made it, and putting
// an empty key on this stack would only make the chord skip a turn.
//
// ── AND THE KEY IS THE ONE THE TERMINAL ACTUALLY SENDS ──────────────────────
//
// This chord is matched on [tea.KeyPressMsg.String], which spells a shifted
// control key `ctrl+shift+t`. A terminal that cannot tell `ctrl+shift+t` from
// `ctrl+t` sends the latter and gets a NEW CHAT, which is what that key has
// always done here: this file never claims `ctrl+t`, because a key that opened a
// tab on one terminal and reopened a different one on the next is a key nobody
// can predict. Where the distinct event does arrive it is answered whatever the
// terminal negotiated — nothing here asks for a keyboard protocol flag. The
// manual says so on the key sheet rather than leaving it to be discovered.
const reopenTabChord = "ctrl+shift+t"

// reopenTabKey is that chord's whole claim on the keyboard, and it reports
// whether it took the key.
//
// IT IS READ AT [app.closeTabKey]'S RUNG (input.go) — under every modal, panel
// and page, and over the message box — because it is that gesture's inverse and
// the two must not be reachable in different places. It TAKES the key with an
// empty stack as well: there is nothing to reopen, and a chord that fell through
// to the box would be a chord whose effect depended on how many tabs you had shut.
//
// IT ANSWERS FROM A HELD ROSTER, on [app.newChatKey]'s reason: the screen about
// to be drawn is a different conversation, and a keyboard pointed at a list
// nobody can see is the bug chordfocus.go states.
func (a *app) reopenTabKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if msg.String() != reopenTabChord {
		return nil, false
	}
	if a.at(pageHome) {
		if a.homeSheetShowing() {
			return nil, false
		}
		if ex := a.paneExchange(); ex != nil && ex.focused {
			return nil, false
		}
	}
	if a.railHold {
		a.railTake(false)
	}
	return a.reopenClosedTab(), true
}

// reopenClosedTab brings the most recently dismissed tab back, and does nothing
// at all when there is none.
//
// AN ENTRY WHOSE TAB IS ALREADY BACK IS DROPPED RATHER THAN OPENED. Every road
// to the front lifts the dismissal through [app.rememberOpen], so a conversation
// somebody reopened by hand from `ctrl+k` or from home is no longer shut — and a
// chord that "reopened" the tab a person is looking at would be a keystroke that
// did nothing visible. The walk continues to the one under it, which is the tab
// they meant.
func (a *app) reopenClosedTab() tea.Cmd {
	for len(a.closedTabs) > 0 {
		last := len(a.closedTabs) - 1
		tab := a.closedTabs[last]
		if tab.key == "" || !a.tabShut[tab.key] {
			a.closedTabs = a.closedTabs[:last]
			continue
		}
		if tab.key == a.frontTabKey() {
			a.closedTabs = a.closedTabs[:last]
			a.rememberOpen(tab.key)
			// Escape from Home may already have exposed this conversation. Skip
			// that stale closure and keep walking to the next actual closed tab.
			if !a.pageShowing() && !a.startingChat() {
				continue
			}
			if a.startingChat() {
				cmd := a.parkChatStart()
				a.tabReveal()
				a.touch()
				return cmd
			}
			a.closeHome()
			a.tabReveal()
			a.touch()
			return nil
		}
		// These doors report refusal and resolve remote paths on the owning
		// machine. A failed attempt must neither consume history nor dismiss Home.
		cmd, held := a.bringForward(tab.file)
		refusal := ""
		if !held {
			var opened tea.Cmd
			opened, refusal = a.openBeside(tab.where, tab.file)
			cmd = tea.Batch(cmd, opened)
		}
		if refusal != "" {
			a.note(refusal)
			if a.at(pageHome) {
				a.pageMsg = refusal
			}
			return cmd
		}
		if a.frontTabKey() != tab.key {
			return cmd
		}
		a.closedTabs = a.closedTabs[:last]
		a.rememberOpen(tab.key)
		a.closeHome()
		a.tabReveal()
		a.touch()
		return cmd
	}
	return nil
}

// rememberClosedTab puts one conversation on the reopen stack, and it is called
// by [app.tabShutKey] BEFORE that function takes the tab off the row — the row is
// where the address, the workspace and the name a reopen needs are kept, and
// after the removal they are gone.
//
// THE ENTRY IS THE TAB AS IT WAS, WITH ITS PASSING STATE DROPPED. `here`, `held`
// and the status mark are facts about a frame rather than about a conversation,
// and a stack that carried them would give the next frame a claim that was true
// whenever the tab was shut. What is kept is the identity, the address, the
// workspace and the word — which is exactly what the switcher hands the same
// door (hop.go's [hopRow]).
//
// ONE ENTRY PER CONVERSATION, and the newest closure wins. A tab shut, reopened
// and shut again is one thing a person shut, so an earlier mention is removed
// rather than left to be walked past — otherwise leaning on the chord would
// reopen the same conversation twice with nothing in between.
func (a *app) rememberClosedTab(key string) {
	if key == "" {
		return
	}
	tab, found := chatTabAt(a.chatTabs, key)
	if !found {
		// THE ROW IS REFILLED BY THE DRAW, so a dismissal made between two frames
		// can be about a conversation the strip's own list no longer holds — which
		// is every close of the tab IN FRONT, because the switch away from it runs
		// before the key is written down. The keeper has it by then, with the
		// address and the workspace on the conversation itself, and [app.tabAs] is
		// the one reading of a held conversation this surface makes.
		if held := a.behind[key]; held != nil {
			tab, found = a.tabAs(chatTab{key: key}, held, ""), true
		} else if front := a.frontChatTab(); front.key == key && !front.start {
			// AND THE CONVERSATION STILL IN FRONT, which is what shutting the LAST
			// tab leaves: the window goes to home with it alive behind, so it is in
			// neither the row nor the keeper.
			tab, found = front, true
		}
	}
	if !found {
		return
	}
	tab.here, tab.held, tab.start, tab.signal = false, false, false, tabIdle
	if tab.file == "" {
		tab.file = tab.key
	}
	kept := a.closedTabs[:0]
	for _, old := range a.closedTabs {
		if old.key != key {
			kept = append(kept, old)
		}
	}
	a.closedTabs = append(kept, tab)
	// AND THE STACK IS BOUNDED BY THE ROW'S OWN CAP. A window is allowed to have
	// been in more conversations than that ([tabsCap] is what the strip
	// remembers), and a list that grew without one would be this surface holding
	// every name of a long day for a gesture that walks the last few.
	if over := len(a.closedTabs) - tabsCap; over > 0 {
		a.closedTabs = a.closedTabs[:copy(a.closedTabs, a.closedTabs[over:])]
	}
}

// chatTabAt is the tab with this key, and whether the list holds one. It is the
// walk [tabsHold] is, answering with the tab rather than with a yes.
func chatTabAt(tabs []chatTab, key string) (chatTab, bool) {
	for _, tab := range tabs {
		if tab.key == key {
			return tab, true
		}
	}
	return chatTab{}, false
}
