package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── THE WAY BACK TO THE CHATS ───────────────────────────────────────────────
//
// The places' bar is where a person stands to read the machine, and until this
// it had no word for the one room they came from: the chats, the conversation
// surface with its tab strip. `esc` went back, and nothing on the bar said so.
// So the bar carries `chats`, third, right after home and teams:
//
//	home  teams  chats  sessions  spend  settings
//
// A press on it, its digit and `enter` on it with the bar's cursor all do one
// thing: the place closes and the conversation that was in front is in front
// again, as `esc` does; with no conversation open at all it opens a new chat.
// It is the word the key legend already says for the chats (`alt+k chats`),
// so one word means one thing; `alt+k` itself stays the chats SWITCHER, which
// chooses among them, where this goes back to the one you were in.
//
// IT IS A PLACE IN THE LIST AND NOT A ROOM. It is registered so the bar, the
// digits and the map read it off [placeOrder] like every other word, and
// [app.showPage] turns it into the road out before any place would open; it
// has no rows, no clock and no count. `tab`, which walks the rooms, steps over
// it ([nextPage]).

// placeChats is the chats' word on the registry.
type placeChats struct{ placeBase }

func init() { registerPlace(placeChats{}) }

func (placeChats) id() page             { return pageChats }
func (placeChats) word() string         { return "chats" }
func (placeChats) cursorAt(a *app) int  { return 0 }
func (placeChats) hint(a *app) string   { return "back to your chats" }
func (placeChats) open(a *app) tea.Cmd  { return nil }
func (placeChats) enter(a *app) tea.Cmd { return a.goChats() }

// goChats is the way back: the place closes and the conversation in front is
// in front again, or, with none open, the new-chat page opens.
func (a *app) goChats() tea.Cmd {
	a.showPage(pageNone)
	a.touch()
	if len(a.tabList()) == 0 && a.canStart() {
		return a.openChatStart()
	}
	return nil
}
