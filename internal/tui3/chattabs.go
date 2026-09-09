package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ── THE NAVIGATION HEADER: WHICH CONVERSATION THIS WINDOW HAS, AND WHICH ONE IS UP ──
//
// The frame's first row used to be ONE WORD — the conversation's own name, with
// a `▾` after it where the switcher had somewhere to go (roomcrumbs.go's bar,
// which this replaces). It answered "what is this page" and nothing else, so the
// other conversations a person had been in all day were reachable only through a
// keystroke they had to know about, and there was nowhere on screen that said
// they existed at all.
//
// So the top of the frame is a HEADER PANEL, and the number of rows in it is
// what page you are standing on:
//
//	 │ main ×│ the tree walk  │ chat three  │            Chats ▾    ← the tabs
//	 ────────────────────────────────────────────────────────────   ← the rule
//
//	 │ main ×│ the tree walk  │ chat three  │            Chats ▾    ← the tabs
//	 ─ main ▸ Ship the port ▸ Cut the goldens ──────── esc/← main ─  ← the trail
//	 ─ ⠿ working · 2m 12s · $0.04 · 6 tool calls ───────── Stop ───  ← the facts
//
// THE THREE ROWS ARE THREE QUESTIONS AND THAT IS WHY THEY ARE THREE ROWS. The
// tabs say WHICH CONVERSATION; the trail says WHERE INSIDE IT, and nothing else
// — no state glyph, no model, no money, because a path with telemetry threaded
// through it is a path nobody can read as a path; the facts row says WHAT THE
// WORK IS DOING and ends in the rule that separates the header from everything
// under it. Out in the conversation there is no trail and no facts — `main` with
// nothing after it is the tab above said twice — so the panel is the tabs and
// one low-contrast rule, which is what gives the transcript and the roster
// beside it an edge that does not depend on the terminal's own background.
//
// ── WHAT A TAB IS ALLOWED TO CLAIM ──────────────────────────────────────────
//
// A TAB IS A CONVERSATION THIS WINDOW HAS BEEN IN, and that is the whole of the
// claim. It is NOT a promise that the conversation is running: over a shared
// engine handle (`Options.SharedAgent`, which is every `--host` door and the
// ordinary socket onto this machine's own engine) the far side holds ONE
// conversation at a time and ends the previous one on the swap, and a strip
// drawing eight live sessions there would be the surface inventing seven of
// them. What is true in both worlds is that these are the conversations a person
// has been in from this window and can go back to in one press — which is what
// the browser tab it looks like promises too.
//
// ── AND THE ✕ ON A TAB CLOSES A VIEW, NEVER WORK ────────────────────────────
//
// This is the law the whole close gesture hangs off, and it is the opposite of
// what the strip used to say. Dismissing a tab TAKES THE TAB OFF THE ROW and
// does nothing else: the agent behind it goes on running, its draft, its caret
// and its attachments are kept exactly where the person left them, and the
// conversation is still on the switcher `ctrl+k` opens — which is where
// reopening it brings the tab, and the draft, back. Nothing on this row calls
// [app.closeFront] or [app.closeKept], and nothing on it interrupts an agent.
//
// So the mark can be drawn on every tab honestly, over a shared handle included:
// it is a claim about a ROW OF THIS WINDOW, and this window owns every one of
// them. Ending work is `Stop` on the facts row, spelled out in a word, and
// ending the program is `/quit` — two gestures a person types on purpose, and
// neither of them is a mark on a tab.
//
// ── AND WHY THE ORDER NEVER MOVES ───────────────────────────────────────────
//
// [app.prev] is a RECENCY stack — the order `tab` walks and the switcher's own
// ring — and a strip drawn from it would re-order itself on every switch, which
// is the one thing tabs may not do: a person reaches for the position, not for
// the word. So the strip keeps its OWN list in the order each conversation was
// first entered ([app.chatTabs]), and recency decides only which tab is up and,
// when the list outgrows the cap, which one falls off the end.

const (
	// tabSep is the rule between two tabs. It is ONE CELL AND IT IS INERT: the
	// row is a set of padded targets and the separator is what tells them apart,
	// so it answers to no pointer and no press (the strip claims the row so a
	// press between two tabs stops there rather than falling through).
	tabSep      = "│"
	tabSepASCII = "|"
	// tabCloseMark is the dismissal on a tab, and tabCloseCells is the room kept
	// for it whether or not it is drawn. The CELLS ARE RESERVED ON EVERY TAB
	// because the mark appears under the pointer: a row that grew two cells when
	// a hand crossed it would re-pack every label beside it, and the tab somebody
	// was reaching for would move out from under them.
	tabCloseMark  = "×"
	tabCloseASCII = "x"
	tabCloseCells = 3
	// The filled tab includes an inset before its status mark and after its close.
	tabInsetCells = 1
	// tabWordCap is the widest a tab's label is drawn on a frame with room to
	// spare. A tab is a label a person recognises rather than a sentence they
	// read, and past about this many cells one long name is the whole strip.
	tabWordCap = 32
	// tabWordFloor is the fewest cells a name is worth cutting to. Under it the
	// word has stopped identifying a conversation and the strip is better off
	// folding the tab into the count at the row's right end.
	tabWordFloor = 6
	// tabsCap is how many tabs the strip remembers. It is the switcher's own
	// order it falls back on when it overflows, so the tab that goes is the one
	// nobody has been in for longest.
	tabsCap = 32
	// tabsWord is the LABELLED control after the visible tabs, and the label is
	// the whole point of it: a bare `▾` floating at the end of a row of words is
	// a mark nobody can read as a door. It opens the switcher `ctrl+k` opens,
	// showing every conversation on this machine rather than only the ones this
	// window holds.
	tabsWord = "Chats"
	// tabMoreWord is the mark after that label, and tabHiddenLead leads the count
	// of tabs the row could not spell. Both are decoration in front of the word:
	// a narrow frame drops them in that order and keeps `Chats`, because the word
	// is what says the control is a door.
	tabMoreWord   = "▾"
	tabHiddenLead = "+"
)

// chatTab is one conversation as the strip remembers it. Every field is read
// from this process's own memory — the keeper's map, the surface's own title —
// and NOTHING here opens a file or crosses a wire: the strip is laid out on
// every frame, and a reading that touched the disk would be a world scan thirty
// times a second, or a call to another machine over `--host` (hop.go states the
// same law about the switcher's rows).
type chatTab struct {
	// key is the identity, and it is the canonical session file (detach.go's
	// [app.convKey]) rather than a title or a number. Two conversations may
	// share a name, and an unnamed one has none at all.
	key string
	// file is the address a press hands to the door, and where is the workspace
	// that door needs when the conversation is not open any more.
	file  string
	where string
	// word is what the tab says. A conversation this window cannot name is not
	// drawn at all rather than drawn as a path — a tab keyed on a file name is a
	// tab nobody can read.
	word string
	// full is the conversation name revealed on hover without widening its tab.
	full string
	// here says this is the conversation in front. It is the ONE tab that is
	// lit, whatever page of it is on screen.
	here bool
	// held says this process is still holding the agent behind this tab
	// (keeper.go). It is not drawn — the strip makes no claim about what is
	// running — and it decides only whether a press is an attach or an open.
	held   bool
	start  bool
	signal tabSignal
}

// tabKind is what one drawn piece of the strip IS, which is what decides whether
// it is a door and what that door does.
type tabKind int

const (
	// tabHere is the conversation in front. Its door is "come back out of the
	// page you walked into", which is the root crumb's door and `esc`'s; on the
	// conversation itself there is nowhere to go and the press does nothing —
	// but the tab still answers the pointer, because a row of tabs where the one
	// you are on is the only dead cell is a row that teaches the wrong thing.
	tabHere tabKind = iota
	// tabOther is another conversation: a switch.
	tabOther
	// tabClose is the ✕ riding one tab, and it is a target of its own so that a
	// press aimed at it can never be read as a press aimed at the label beside
	// it (hover.go's law: what lights is exactly what the press acts on).
	tabClose
	// tabMore is the labelled control after the tabs — the switcher, and the
	// count of what the row could not spell.
	tabMore
	// tabFold is that same count on a frame where the switcher cannot open. It
	// is drawn WITHOUT the `Chats` word and does nothing, exactly as the trail's
	// own `…` is inert when everything it hides is (roomcrumbs.go's law 4): a
	// count is a fact and stays true, while a labelled control that opened
	// nothing would be a door painted on a wall.
	tabFold
	tabNew
	tabHome
	tabScrollLeft
	tabScrollRight
)

// tabHit is where one piece was drawn and what pressing it does. It is the
// bargain every pointer target on this surface makes (render.go's [hudSpan]):
// the render writes down where the words landed and the press resolves against
// that, never against a second computation of the same layout.
type tabHit struct {
	span hudSpan
	kind tabKind
	tab  chatTab
}

// door reports whether pressing this piece would take a person anywhere. The
// tab already up is the one piece that answers the pointer without being one:
// see [tabHit.lights].
func (h tabHit) door(a *app) bool {
	switch h.kind {
	case tabHere:
		return a.roomOpen() || a.startingChat()
	case tabOther, tabClose, tabMore, tabNew, tabHome, tabScrollLeft, tabScrollRight:
		return true
	}
	return false
}

// lights reports whether this piece reacts to a pointer resting on it.
//
// IT IS WIDER THAN [tabHit.door] AND THAT IS DELIBERATE, against the rest of
// this surface's habit. Everywhere else what lights is exactly what a press acts
// on, because a hover on a dead cell is a promise of a door that is not there.
// A ROW OF TABS IS THE EXCEPTION AND THE REASON IS THE ROW ITSELF: it is one
// control made of adjacent targets, and a strip where every tab answered the
// hand except the one you are standing on would read as the current tab being
// broken. So the tab that is up takes the pointer too — it is the only tab whose
// selection SURVIVES the pointer leaving, which is what tells the two states
// apart — and the inert count at the right end still does not, because that one
// is a fact rather than a target.
func (h tabHit) lights() bool { return h.kind != tabFold }

// tabBar is the strip as it was last laid out, kept from frame to frame on the
// terms every cached row on this surface is kept (render.go): rebuilt when the
// width, the ink, the hover or the words change, and reused otherwise. The
// strip says one unchanging thing while a person scrolls four thousand lines
// past it, and laying it out again thirty times a second would spend the
// frame's allocation budget on a row that cannot have moved (PERF.md's scroll
// law).
//
// IT HOLDS THE HITS AS WELL AS THE LINE because the two are one act: a frame
// that reused the line and rebuilt the map would be paying for the layout it
// just skipped, and one that reused the line and kept no map would be a row
// that stopped answering the pointer.
type tabBar struct {
	width int
	ink   uint64
	// hot is the column of the piece the pointer was on, or -1.
	hot     int
	more    bool
	newChat bool
	home    bool
	tabs    []chatTab
	line    string
	hits    []tabHit
}

// same reports whether a freshly built list would draw the same strip. It is a
// FIELD COMPARISON AND NOT A KEY BUILT OUT OF STRINGS: this runs on every frame,
// and a signature assembled per frame would be an allocation per frame for a row
// that has not changed since the session started (PERF.md's scroll law).
//
// The kept list is a COPY for the same reason — [app.tabList] refills the
// surface's own slice in place, so a memo holding that slice would be comparing
// the list against itself and could never notice a name changing.
func (m tabBar) same(width int, ink uint64, hot int, more bool, tabs []chatTab) bool {
	if m.line == "" || m.width != width || m.ink != ink || m.hot != hot || m.more != more || len(m.tabs) != len(tabs) {
		return false
	}
	for at, tab := range tabs {
		if m.tabs[at] != tab {
			return false
		}
	}
	return true
}

// tabIdentity is [app.convKey] for the conversation in front, remembered against
// the file it was taken from.
//
// IT IS MEMOISED BECAUSE THE KEY IS A DISK QUESTION. convKey resolves a
// transcript through the filesystem, and the strip asks who is in front on every
// frame — so a fresh answer per frame would be a stat call per frame, which is
// exactly the reading this file is written to avoid.
type tabIdentity struct{ file, key string }

// frontTabKey is the canonical identity of the conversation on screen.
func (a *app) frontTabKey() string {
	if a.chatTabWho.file == a.file {
		return a.chatTabWho.key
	}
	a.chatTabWho = tabIdentity{file: a.file, key: a.convKey(a.file)}
	return a.chatTabWho.key
}

// tabList is the strip's model: every conversation this window has been in that
// it can still name, still reach and has not dismissed, in the order it first
// entered them.
//
// IT REFILLS THE SURFACE'S OWN SLICE IN PLACE and asks both membership questions
// with a walk rather than with a map, because this runs on every frame and a map
// is two allocations where a slice is one (PERF.md's scroll law).
//
// AND THE CANDIDATES ARE BOUNDED, which they were not. The second pass walked
// [app.prev] whole and asked a linear membership question per key — and prev is
// as long as the number of conversations this window has been in, which the
// keeper deliberately does not cap (keeper.go). Sixty open conversations was
// sixty times sixty comparisons per frame for a row that can only ever draw
// the presentation cap. The pass walks prev FROM THE MOST RECENT END and stops as soon as
// it has [tabsCap] candidates, which is the same answer — the cap below drops by
// exactly that recency — without constructing an unbounded candidate list. The recency scan can still
// walk older dismissed entries; only the candidate membership checks are bounded.
//
// It is called ONCE PER FRAME, by the draw. The geometry ([app.tabsHeight]) does
// not ask it — there is always at least one tab, the conversation on screen — so
// nothing else can observe the slice mid-refill.
func (a *app) tabList() []chatTab {
	front := a.frontTabKey()
	tabs := a.chatTabs[:0]
	for _, tab := range a.chatTabs {
		if tab.start || tab.key == "" || tabsHold(tabs, tab.key) {
			continue
		}
		held := a.behind[tab.key]
		// The previous-stack is what says a conversation is still one this window
		// has: [app.closeFront] takes a closed one off it, so a tab whose key has
		// left it is a tab whose conversation is gone.
		if tab.key != front && held == nil && !keysHold(a.prev, tab.key) {
			continue
		}
		// AND A DISMISSED TAB STAYS OFF THE ROW. This is the half of the ✕ that
		// makes it a close rather than a flicker: the conversation is still held,
		// still running and still on the switcher, so every pass below would put
		// its tab straight back the frame after it was taken off.
		if tab.key != front && a.tabShut[tab.key] {
			continue
		}
		if tab = a.tabAs(tab, held, front); strings.TrimSpace(tab.word) == "" {
			// A tab this window can no longer name. It is dropped rather than drawn
			// blank: a nameless tab is a door with nothing written on it.
			continue
		}
		tabs = append(tabs, tab)
	}
	// AND EVERY CONVERSATION THE KEEPER IS HOLDING IS A TAB whether or not the
	// strip has seen it before — a window that resumed one beside another has
	// two, and the strip's own list starts empty.
	for at := len(a.prev) - 1; at >= 0 && len(tabs) < tabsCap; at-- {
		key := a.prev[at]
		if tabsHold(tabs, key) || a.tabShut[key] {
			continue
		}
		held := a.behind[key]
		if held == nil {
			// A key with no agent behind it and no memory of what it was called.
			// A tab that cannot be named is not drawn (see [chatTab.word]).
			continue
		}
		tabs = append(tabs, a.tabAs(chatTab{key: key, file: held.conv.SessionFile}, held, front))
	}
	// AND THE CONVERSATION ON SCREEN IS ALWAYS A TAB, including the one this
	// window has no transcript for yet. A session gets its file when it is first
	// written to, and a strip that waited for that would be a strip missing from
	// the frame a person meets aforge on — which is exactly the frame where being
	// told what this window is holding is worth most.
	if !tabsHold(tabs, front) {
		tabs = append(tabs, a.tabAs(chatTab{key: front, file: a.file}, nil, front))
	}
	return tabsCapped(tabs, a.prev)
}

// tabsHold and keysHold are the two membership questions this file asks, walked
// rather than mapped: the list being walked is bounded by [tabsCap] and a walk
// over the bounded candidates costs no allocation at all.
func tabsHold(tabs []chatTab, key string) bool {
	for _, tab := range tabs {
		if tab.key == key {
			return true
		}
	}
	return false
}

func keysHold(keys []string, key string) bool {
	for _, held := range keys {
		if held == key {
			return true
		}
	}
	return false
}

// tabAs fills one remembered tab in from what this window knows RIGHT NOW: the
// title the surface is showing for the one in front, the agent's own for one the
// keeper holds, and the last name it went by for one that is neither.
func (a *app) tabAs(tab chatTab, held *kept, front string) chatTab {
	tab.here = tab.key == front
	tab.held = held != nil
	switch {
	case tab.here:
		// THE SURFACE'S OWN SPELLING FOR THE ONE ON SCREEN, which is the word the
		// trail's root wears and the word the status line is showing this instant.
		// Two names for one conversation on one screen is the defect this avoids.
		//
		// AND THE NAME IT WENT BY OUTLIVES A MOMENT WITH NO NAME AT ALL. A switch
		// re-reads the title off the agent it just attached (detach.go), and an
		// agent that has not published one yet would drop a named tab to `main` for
		// as long as that takes — the tab flickering to a word that means "unnamed"
		// about a conversation somebody named last week.
		if name := a.sessionName(); name != "" || strings.TrimSpace(tab.word) == "" {
			tab.word = a.chatTabDisplayName()
		}
		tab.full = a.chatDisplayName()
		tab.file, tab.where = a.file, a.workspace
	case held != nil:
		tab.word = chatTabName(shortTitleWithSide(held.conv.Agent, held.side))
		tab.full = chatTabName(hopRawTitle(held.conv.Agent, held.side))
		tab.file, tab.where = held.conv.SessionFile, held.conv.Workspace
	}
	if strings.TrimSpace(tab.file) == "" {
		tab.file = tab.key
	}
	tab.signal = a.tabSignalFor(tab.key, tab.here)
	return tab
}

// tabsCapped drops the tabs a person has not been in for longest, once the strip
// has more than it remembers. The conversation in front never goes, and the
// order of what is left never changes — the cap takes from the middle of the
// list where it has to, because the list is a place and not a queue.
func tabsCapped(tabs []chatTab, prev []string) []chatTab {
	if len(tabs) <= tabsCap {
		return tabs
	}
	rank := make(map[string]int, len(prev))
	for at, key := range prev {
		rank[key] = at + 1
	}
	for len(tabs) > tabsCap {
		drop, worst := -1, 0
		for at, tab := range tabs {
			if tab.here {
				continue
			}
			if drop < 0 || rank[tab.key] < worst {
				drop, worst = at, rank[tab.key]
			}
		}
		if drop < 0 {
			break
		}
		tabs = append(tabs[:drop], tabs[drop+1:]...)
	}
	return tabs
}

// ── THE HEADER'S GEOMETRY, STATED ONCE ──────────────────────────────────────
//
// EVERY POINTER TARGET AND EVERY SUBTRACTION UP HERE RESOLVES THROUGH THESE
// FOUR FUNCTIONS. The panel is one, two or three rows deep depending on the page
// and on how much terminal there is, and a press answered against a hard-coded
// zero or one would open the wrong row the moment a floor moved (hover.go's
// law). Nothing outside this block may spell those numbers.

// tabsHeight is what the tab row costs the body region.
//
// IT STANDS DOWN ON THE TWO FLOORS THE CONVERSATION'S BAR STOOD DOWN ON. A frame
// too narrow for a name and a way out is too narrow for this, and a terminal too
// short for a blank above the draft has no row to spare for a fact that is true
// all day — a person on a twelve-row terminal is reading the conversation.
//
// IT ASKS NOTHING OF THE DISK, and nothing of the list either: there is always
// at least one tab — the conversation on screen — so the answer is the two
// floors and nothing else.
func (a *app) tabsHeight(width int) int {
	if width < roomHeadFloor || a.breathingRows() < 2 {
		return 0
	}
	return 1 + a.tabsLineRow() + a.tabsBottomPad()
}

// tabsLineRow leaves one row of air around the navigation on roomy frames.
// Compact terminals keep the same controls without spending reading rows.
func (a *app) tabsLineRow() int {
	width, height := a.size()
	if width >= 4*roomHeadFloor && height >= 2*airyFloor {
		return 1
	}
	return 0
}

// tabsBottomPad adds a second navigation gap only after the frame has earned
// another four reading rows, so resizing never trades a larger body for chrome.
func (a *app) tabsBottomPad() int {
	width, height := a.size()
	if width >= 4*roomHeadFloor && height >= 2*airyFloor+4 {
		return 1
	}
	return 0
}

// chatRuleHeight is the low-contrast rule under the tabs OUT IN THE CONVERSATION,
// where there is no trail and no facts row to close the panel off.
//
// IT IS WHAT SEPARATES THE HEADER FROM THE TRANSCRIPT AND FROM THE ROSTER BESIDE
// IT, and it is a drawn rule rather than a blank because a blank separates
// nothing on a terminal whose background this program does not control. It is
// the FIRST piece of chrome to collapse as the frame gets short — one row of a
// twenty-row terminal is worth more to the conversation than to a seam — so it
// stands on a floor of its own above the strip's.
func (a *app) chatRuleHeight(width int) int {
	_, height := a.size()
	if a.room != nil || a.tabsHeight(width) == 0 || height < chatRuleFloor {
		return 0
	}
	return 1
}

// chatRuleFloor is the terminal height the seam is worth a row of. It is above
// the strip's own floor on purpose: [airyFloor] is where a second blank above
// the draft became affordable, and a window at exactly that height has eleven
// rows of conversation left — a reply's worth, and not a row to spend on an
// edge. Four rows further up it is.
const chatRuleFloor = airyFloor + 4

// roomHeadRow is the frame row a room's TRAIL is drawn on — the breadcrumbs and
// the way out — which is the row under the tab strip wherever there is one.
func (a *app) roomHeadRow() int {
	width, _ := a.size()
	return a.tabsHeight(width)
}

// roomFactsRow is the row under that one: what the work is doing, what it has
// cost, and the `Stop` that ends it. It is -1 on a frame too short to draw it,
// so a pointer target resolved through it cannot land on the trail above
// (room.go's [app.roomHeadHeight] states why it is the row that gives way).
func (a *app) roomFactsRow() int {
	width, _ := a.size()
	if a.roomHeadHeight(width) < roomHeadRowCount {
		return -1
	}
	return a.roomHeadRow() + 1
}

// ── THE ROW, LAID OUT ───────────────────────────────────────────────────────

// tabsRow lays the strip out and says where every piece landed. It is the
// frame's FIRST row wherever it is drawn at all.
func (a *app) tabsRow(width int) string {
	a.chatTabHits = nil
	if a.tabsHeight(width) == 0 {
		return ""
	}
	tabs := a.tabList()
	// The list is kept so the next frame remembers the names of conversations
	// this one can still see and a later one may not (a shared handle ends the
	// conversation it swaps away from, and its title goes with it).
	a.chatTabs = tabs
	if a.startingChat() {
		for i := range tabs {
			tabs[i].here = false
		}
		tabs = append(tabs, chatTab{word: "New chat", here: true, start: true})
	}
	if len(tabs) == 0 {
		return ""
	}
	hot := -1
	if a.hot.kind == hoverTab {
		hot = a.hot.index
	}
	more := a.hopAvailable()
	home := a.homeDoorOpen() && width-headLabelAt >= len(" Home ")+2+tabWordFloor+tabCloseCells+tabInsetCells
	if memo := a.chatTabBar; memo.home == home && memo.newChat == a.canStart() && memo.same(width, a.inkState, hot, more, tabs) {
		a.chatTabHits = memo.hits
		return memo.line
	}
	room := max(width-headLabelAt, 0)
	homeWidth := 0
	if home {
		homeWidth = len(" Home ") + 2
	}
	pieces, hits := a.tabsFit(tabs, room-homeWidth)
	if home {
		hits = tabsAt(hits, homeWidth)
		hits = append([]tabHit{{span: hudSpan{from: 0, to: len(" Home ")}, kind: tabHome}}, hits...)
		pieces = append([]tabPiece{{word: " Home ", kind: tabHome}, {word: "  ", quiet: true}}, pieces...)
	}
	if len(pieces) == 0 {
		return ""
	}
	a.chatTabHits = tabsAt(hits, headLabelAt)
	line := strings.Repeat(" ", headLabelAt) + a.tabsPaint(pieces)
	a.chatTabBar = tabBar{width: width, ink: a.inkState, hot: hot, more: more, newChat: a.canStart(), home: home, line: line, hits: a.chatTabHits,
		tabs: append([]chatTab(nil), tabs...)}
	return line
}

// tabPiece is one drawn segment of the strip: the word, and what it is.
type tabPiece struct {
	word string
	kind tabKind
	tab  chatTab
	// quiet says this piece is furniture — the rule between two tabs, or the gap
	// beside the switcher. It answers to nothing and is
	// painted at the row's quietest step.
	quiet bool
}

// A single quiet cell separates tab targets; the close mark follows the glyph floor.
func (a *app) tabSepWord() string   { return " " }
func (a *app) tabCloseWord() string { return a.linearMark(tabCloseMark, tabCloseASCII) }

// tabsFit keeps names readable and exposes overflow through a scrolling window.
// Selection is revealed unless the person explicitly browsed away from it; the
// hidden count and directional controls describe everything outside that window.
func (a *app) tabsFit(tabs []chatTab, room int) ([]tabPiece, []tabHit) {
	if room <= 0 || len(tabs) == 0 {
		return nil, nil
	}
	fullRoom := room
	showNew := a.canStart() && room >= tabWordFloor+tabCloseCells+tabInsetCells+7
	if showNew {
		room -= 4
	}
	active := 0
	for at, tab := range tabs {
		if tab.here {
			active = at
		}
	}
	sepW := ansi.StringWidth(a.tabSepWord())
	// The controls are reserved BEFORE the fitting, because a control squeezed in
	// afterwards would be a control drawn over the last tab's own cells. What it
	// asks for is the widest spelling it could want; what it gets is decided
	// again once the tabs have taken their share ([app.tabsMoreWord]).
	reserve := 0
	if wide := ansi.StringWidth(a.tabsMoreWord(len(tabs)-1, true)); wide > 0 {
		reserve = wide + tabsMoreGap
	}
	budget := room - reserve
	if budget < tabWordFloor+tabCloseCells+tabInsetCells {
		budget, reserve = room, 0
	}
	// The names, cut to a tab's own width: a share of the row where there are
	// several, and the whole row where there is one, because a lone tab is the
	// conversation's own name and the row has nothing else to spend itself on.
	// EVERY TAB CARRIES ITS CLOSE CELLS whether or not the mark is drawn in them,
	// so the widths a hover reads are the widths the layout wrote.
	cell := budget
	if len(tabs) > 1 {
		cell = min(tabWordCap, budget-2*sepW)
	}
	words := make([]string, len(tabs))
	widths := make([]int, len(tabs))
	for at, tab := range tabs {
		words[at] = a.tabName(tab, cell-tabCloseCells-tabInsetCells)
		if tab.start {
			words[at] = tabLabel(tab, min(10, budget-tabCloseCells-tabInsetCells-2*sepW))
		}
		widths[at] = ansi.StringWidth(words[at]) + tabInsetCells + tabCloseCells
	}
	from, to, scroll := a.tabWindow(tabs, widths, budget, active)
	windowBudget := budget
	if scroll {
		windowBudget -= 2 * tabArrowCells
	}
	pieces := make([]tabPiece, 0, 3*(to-from)+3)
	hits := make([]tabHit, 0, 2*(to-from)+1)
	at := 0
	if scroll {
		piece, hit := a.tabArrowPiece(false, from > 0, at)
		pieces = append(pieces, piece)
		if hit != nil {
			hits = append(hits, *hit)
		}
		at += tabArrowCells
	}
	for i := from; i < to; i++ {
		word := words[i]
		if to-from == 1 {
			// The one tab that is left takes whatever the row has, cut. A name with
			// an ellipsis in it still says which conversation this is; a blank row
			// says nothing at all.
			word = a.tabName(tabs[i], windowBudget-tabCloseCells-tabInsetCells-2*sepW)
		}
		width := ansi.StringWidth(word) + tabInsetCells
		if width == tabInsetCells {
			continue
		}
		kind := tabOther
		if tabs[i].here {
			kind = tabHere
		}
		pieces = append(pieces, tabPiece{word: a.tabSepWord(), quiet: true})
		at += sepW
		pieces = append(pieces, tabPiece{word: word, kind: kind, tab: tabs[i]})
		hits = append(hits, tabHit{span: hudSpan{from: at, to: at + width}, kind: kind, tab: tabs[i]})
		at += width
		// THE CLOSE CELLS ARE A TARGET OF THEIR OWN AND THEY ARE ALWAYS THERE.
		// What changes under the pointer is whether the mark is painted into them
		// ([app.tabsPaint]), never how many cells they are.
		pieces = append(pieces, tabPiece{word: strings.Repeat(" ", tabCloseCells), kind: tabClose, tab: tabs[i]})
		hits = append(hits, tabHit{span: hudSpan{from: at, to: at + tabCloseCells}, kind: tabClose, tab: tabs[i]})
		at += tabCloseCells
	}
	if len(pieces) > 0 {
		pieces = append(pieces, tabPiece{word: a.tabSepWord(), quiet: true})
		at += sepW
	}
	if scroll {
		arrowAt := budget - tabArrowCells
		if at < arrowAt {
			pieces = append(pieces, tabPiece{word: strings.Repeat(" ", arrowAt-at), quiet: true})
		}
		piece, hit := a.tabArrowPiece(true, to < len(tabs), arrowAt)
		pieces = append(pieces, piece)
		if hit != nil {
			hits = append(hits, *hit)
		}
		at = budget
	}
	// New chat follows the visible names. A scrolling viewport uses its whole
	// budget, placing these same controls at the edge without a second layout.
	if showNew {
		pieces = append(pieces, tabPiece{word: " + ", kind: tabNew})
		hits = append(hits, tabHit{span: hudSpan{from: at, to: at + 3}, kind: tabNew})
		at += 3
	}
	hidden := len(tabs) - (to - from)
	word, kind := a.tabsMoreWord(hidden, false), tabMore
	if !a.hopAvailable() {
		word, kind = a.tabsFoldWord(hidden), tabFold
	}
	for width := ansi.StringWidth(word); width > 0; width = ansi.StringWidth(word) {
		if at+tabsMoreGap+width <= fullRoom {
			pieces = append(pieces, tabPiece{word: strings.Repeat(" ", tabsMoreGap), quiet: true})
			at += tabsMoreGap
			pieces = append(pieces, tabPiece{word: word, kind: kind})
			hits = append(hits, tabHit{span: hudSpan{from: at, to: at + width}, kind: kind})
			break
		}
		word = a.tabsShorter(word, hidden)
	}
	return pieces, hits
}

// tabsMoreGap is the least space between the last tab and the switcher, so the
// control never reads as the next tab along.
const tabsMoreGap = 2

// tabsMoreWord is the switcher's label at its widest that still says everything
// true: the word, the count of tabs this row could not spell, and the mark.
func (a *app) tabsMoreWord(hidden int, widest bool) string {
	if !a.hopAvailable() {
		return a.tabsFoldWord(hidden)
	}
	word := tabsWord
	if hidden > 0 {
		word += " " + tabHiddenLead + itoa(hidden)
	}
	return word + " " + tabMoreWord
}

// tabsFoldWord is the count alone, for a frame where the switcher cannot open
// (hop.go's [app.hopMayOpen]). The count is still true; the word is not drawn,
// because a labelled control that opens nothing is a door painted on a wall.
func (a *app) tabsFoldWord(hidden int) string {
	if hidden <= 0 {
		return ""
	}
	return tabHiddenLead + itoa(hidden)
}

// tabsShorter is the switcher's degradation ladder, one rung per call: the mark
// goes first, then the count, and THE WORD IS WHAT SURVIVES. Both of the things
// dropped are decoration in front of it — a person reading a narrow row needs to
// know the control is there far more than they need to know how many rows it is
// standing for.
func (a *app) tabsShorter(word string, hidden int) string {
	switch {
	case strings.HasSuffix(word, " "+tabMoreWord):
		return strings.TrimSuffix(word, " "+tabMoreWord)
	case hidden > 0 && strings.HasSuffix(word, " "+tabHiddenLead+itoa(hidden)):
		return strings.TrimSuffix(word, " "+tabHiddenLead+itoa(hidden))
	}
	return ""
}

// tabLabel gives each tab a padded target. Brackets identify the selected
// conversation even with NO_COLOR, where both tint and underline are absent.
func tabLabel(tab chatTab, width int) string {
	if width < 2 {
		return fit(tab.word, width)
	}
	word := fit(tab.word, width-2)
	if tab.here {
		return "[" + word + "]"
	}
	return " " + word + " "
}

// A reserved status slot keeps labels stable across work transitions. The start
// page has no agent and makes no status claim.
func (a *app) tabName(tab chatTab, width int) string {
	if width < tabSignalWidth+3 || tab.start {
		return tabLabel(tab, width)
	}
	ascii := a.linearMark(tabSep, tabSepASCII) == tabSepASCII
	return tabSignalSlot(tab.signal, ascii) + tabLabel(tab, width-tabSignalWidth)
}

// tabsPaint draws the pieces in the incumbent palette and nothing else: the
// selected step for the tab that is up, the pointer's own step for whichever one
// the hand is on, and the row's dim for the rest.
//
// THE HOVER IS A GROUND AND NOT A BRIGHTENING, which is a deliberate departure
// from the rest of this surface. Everywhere else a hover is one step of ink,
// because the thing under the pointer shares its row with prose. This row is
// nothing but adjacent controls, and the tab that is UP already wears the
// loudest ink on it — so a hover spelled as ink could not be told apart from
// selection on the one tab a person most needs it on. Ground says where the
// pointer is, ink and the brackets say which tab is chosen, and the two channels
// stay legible together where two shades of one would not (stop.go's answers row
// makes the same trade for the same reason).
func (a *app) tabsPaint(pieces []tabPiece) string {
	hot, lit := a.hotTab()
	line := ""
	for _, piece := range pieces {
		on := lit && hot.tab.key == piece.tab.key && hot.tab.start == piece.tab.start && hot.kind != tabMore && hot.kind != tabFold && hot.kind != tabNew && hot.kind != tabHome && hot.kind != tabScrollLeft && hot.kind != tabScrollRight
		switch {
		case piece.quiet:
			line += a.pal.dim(piece.word)
		case piece.kind == tabClose:
			line += a.tabClosePaint(piece, hot, lit, on)
		case piece.kind == tabHere:
			word := a.tabActivePaint(a.tabWordPaint(piece, a.pal.ink))
			if on {
				word = a.pal.underline(word)
			}
			line += word
		case piece.kind == tabMore || piece.kind == tabFold || piece.kind == tabNew || piece.kind == tabHome || piece.kind == tabScrollLeft || piece.kind == tabScrollRight:
			if lit && hot.kind == piece.kind {
				word := piece.word
				if a.pal.profile < tokens.ANSI256 {
					switch piece.kind {
					case tabHome:
						word = a.linearMark("·", ".") + "Home "
					case tabNew:
						word = a.linearMark("·", ".") + "+ "
					case tabMore:
						word = strings.ToUpper(word)
					case tabScrollLeft, tabScrollRight:
						word = a.linearMark("·", ".") + strings.TrimSpace(word) + " "
					}
				}
				line += a.pal.cursor(a.pal.ink(word), 0)
				continue
			}
			line += a.pal.dim(piece.word)
		case on:
			line += a.pal.background(a.tabWordPaint(piece, a.pal.ink), 0, a.pal.ramp.mark)
		default:
			line += a.pal.selected(a.tabWordPaint(piece, a.pal.muted), 0)
		}
	}
	return line
}

// Navigation tint belongs to the name; the small status mark keeps its meaning.
func (a *app) tabWordPaint(piece tabPiece, ink func(string) string) string {
	pad := strings.Repeat(" ", tabInsetCells)
	// Color carries selection as a filled tab; plain terminals keep brackets.
	// Replace the two furniture cells only, preserving every hit coordinate.
	if piece.tab.here && a.pal.profile >= tokens.ANSI256 {
		lead := 0
		if !piece.tab.start && ansi.StringWidth(piece.word) >= tabSignalWidth+3 {
			lead = tabSignalWidth
		}
		end := ansi.StringWidth(piece.word)
		if ansi.Cut(piece.word, lead, lead+1) == "[" && ansi.Cut(piece.word, end-1, end) == "]" {
			piece.word = ansi.Cut(piece.word, 0, lead) + " " + ansi.Cut(piece.word, lead+1, end-1) + " "
		}
	}
	if piece.tab.start || piece.tab.signal == tabIdle || ansi.StringWidth(piece.word) < tabSignalWidth+3 {
		return ink(pad + piece.word)
	}
	return ink(pad) + a.pal.tabSignalInk(piece.tab.signal, ansi.Cut(piece.word, 0, tabSignalWidth)) + ink(ansi.Cut(piece.word, tabSignalWidth, ansi.StringWidth(piece.word)))
}

// tabClosePaint draws one tab's close cells.
//
// THE MARK IS THERE ON THE TAB THAT IS UP AND ON THE TAB UNDER THE HAND, and the
// cells are blank on every other one. That is the clutter trade the row is worth
// making: eight ✕ marks across the top of the frame is eight invitations to end
// something, on a row whose job is to say where you are. A blank target is still
// a target — the hand finds it by moving onto the tab, which is when the mark
// appears — and the cells never change width either way, so nothing re-packs
// under the pointer.
//
// THE MARK IS ALSO THE ROW'S ONE PLAIN-TEXT HOVER AFFORDANCE, which is what a
// terminal with NO_COLOR has instead of the ground: a `×` that was not there a
// moment ago says the hand is on this tab as plainly as any tint.
func (a *app) tabClosePaint(piece tabPiece, hot tabHit, lit, on bool) string {
	if !piece.tab.here && !on {
		return a.pal.selected(a.pal.dim(piece.word), 0)
	}
	mark := " " + a.tabCloseWord() + strings.Repeat(" ", tabInsetCells)
	if on && a.pal.profile == tokens.NoColor {
		pointer := "·"
		if a.pal.ascii {
			pointer = "."
		}
		mark = pointer + a.tabCloseWord() + strings.Repeat(" ", tabInsetCells)
	}
	if piece.tab.here {
		painted := a.tabActivePaint(mark)
		if on && hot.kind == tabClose {
			painted = a.pal.underline(painted)
		}
		return painted
	}
	switch {
	case on && hot.kind == tabClose:
		// THE POINTER IS ON THE MARK ITSELF, so the mark takes the ink and the
		// cells keep the ground the rest of the tab is wearing. A press here
		// dismisses the tab and a press one cell left selects it, and the two must
		// never look like one target (hover.go's law).
		return a.pal.background(a.pal.ink(mark), 0, a.pal.ramp.mark)
	case on:
		return a.pal.background(a.pal.dim(mark), 0, a.pal.ramp.mark)
	}
	return a.pal.dim(mark)
}

// hotTab is the piece the pointer is on, when it is on one that reacts.
func (a *app) hotTab() (tabHit, bool) {
	if a.hot.kind != hoverTab {
		return tabHit{}, false
	}
	for _, hit := range a.chatTabHits {
		if hit.span.from == a.hot.index && hit.lights() {
			return hit, true
		}
	}
	return tabHit{}, false
}

// tabsAt moves a set of hits from the strip's own columns into the frame's, so
// the press and the hover can ask about them in the coordinates a mouse arrives
// in.
func tabsAt(hits []tabHit, from int) []tabHit {
	if len(hits) == 0 {
		return nil
	}
	out := make([]tabHit, 0, len(hits))
	for _, hit := range hits {
		hit.span = hudSpan{from: hit.span.from + from, to: hit.span.to + from}
		out = append(out, hit)
	}
	return out
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// tabAt is the piece under a pointer, and whether there is one. It answers only
// for the strip's own row, which is row zero of the frame wherever the strip is
// drawn at all — [app.view] draws it first and [app.headHeight] charges for it.
func (a *app) tabAt(x, y int) (tabHit, bool) {
	width, _ := a.size()
	if y != a.tabsLineRow() || a.tabsHeight(width) == 0 {
		return tabHit{}, false
	}
	for _, hit := range a.chatTabHits {
		if hit.span.holds(x) {
			return hit, true
		}
	}
	return tabHit{}, false
}

// tabHoverAt is the hover this row answers with, and whether it answers at all.
//
// THE PIECE IS HELD BY THE COLUMN IT STARTS ON rather than by its place in the
// strip, for the reason the crumbs are (roomcrumbs.go): the strip re-packs
// itself as the terminal is resized and as a title arrives, so a hover stored as
// "the second tab" would follow the packing instead of following the words.
func (a *app) tabHoverAt(x, y int) (hoverAt, bool) {
	hit, ok := a.tabAt(x, y)
	if !ok || !hit.lights() {
		return hoverAt{}, false
	}
	return hoverAt{kind: hoverTab, index: hit.span.from}, true
}

// tabPress answers a press on the strip and reports whether it took it.
//
// THE WHOLE ROW IS THE STRIP'S, whether or not a piece was under the press. The
// separators between the tabs are furniture and the gap in front of the switcher
// is furniture, and a press on either of them stops here rather than falling
// through to whatever the next rung would have made of it.
func (a *app) tabPress(x, y int) (tea.Cmd, bool) {
	width, _ := a.size()
	if y < 0 || y >= a.tabsHeight(width) {
		return nil, false
	}
	if y != a.tabsLineRow() {
		return nil, true
	}
	hit, ok := a.tabAt(x, y)
	if !ok || !hit.door(a) {
		return nil, true
	}
	switch hit.kind {
	case tabScrollLeft:
		a.tabScroll(-1)
		return nil, true
	case tabScrollRight:
		a.tabScroll(1)
		return nil, true
	case tabHome:
		return a.openHome(), true
	case tabNew:
		return a.openChatStart(), true
	case tabMore:
		// THE SWITCHER AND NOT A MENU OF ITS OWN. `ctrl+k` already draws every
		// conversation this machine has, ranked, with what each of them wants from
		// you on it (hop.go); a second list built here would be a second answer to
		// the same question, kept in step with the first by nothing. It opens on
		// ALL of them rather than on the ones this window holds, because the label
		// says `Chats` and a list that showed three of somebody's twelve would be
		// the control lying about what it opens.
		a.hopOpenAll()
		return nil, true
	case tabClose:
		return a.tabDismiss(hit.tab), true
	case tabHere:
		a.tabReveal()
		if hit.tab.start {
			return nil, true
		}
		if a.startingChat() {
			return a.cancelChatStart(), true
		}
		// The conversation this page hangs off. Its door is the way back out, which
		// is `esc` and the trail's own root crumb.
		a.closeRoom()
		return nil, true
	}
	return a.tabGo(hit.tab), true
}

// tabGo is the switch itself, and it is the switcher's own two doors in the
// switcher's own order (hop.go's [app.hopTake]): a conversation this process is
// holding is ATTACHED, and one it is not is OPENED beside — with the same two
// refusals said in the same words, because one gesture with two spellings of
// "that folder is gone" is two features to keep in step.
func (a *app) tabGo(tab chatTab) (cmd tea.Cmd) {
	a.tabReveal()
	if tab.start {
		return a.openChatStart()
	}
	if a.startingChat() {
		back := a.parkChatStart()
		defer func() { cmd = tea.Batch(back, cmd) }()
	}
	if cmd, ours := a.bringForward(tab.file); ours {
		return cmd
	}
	return a.hopStart(hopRow{file: tab.file, where: tab.where, title: tab.word})
}

// ── DISMISSING A TAB ────────────────────────────────────────────────────────

// tabDismiss removes a conversation's tab from this window. Its transcript and
// recipient-scoped draft remain available when the person reopens it.
//
// An inactive tab simply leaves the row. Closing the active tab selects the
// most recently used remaining tab, using the same attach/open seam as ordinary
// navigation. Home is the fallback only when no other tab remains. A refused
// open keeps the outgoing tab selected and its draft intact.
//
// Local held conversations remain alive. Shared connections retain their normal
// single-conversation switching contract: the engine performs the swap, and this
// surface must never close the handle afterward because it now names the target.
// A TAB WITH SOMETHING IN FLIGHT IS ASKED ABOUT FIRST (tabclose.go). The card
// offers the three acts this gesture can be — keep the work and drop the tab,
// stop the work and drop the tab, or change your mind — and its default is the
// one this function has always performed. A conversation at rest is not asked
// about at all: there is nothing to decide, and a question with one useful answer
// is friction.
func (a *app) tabDismiss(tab chatTab) (cmd tea.Cmd) {
	if a.closingTab() {
		// The card is up about a tab already; a second press on a ✕ is not a
		// second question. The keyboard is the card's and so is the pointer.
		return nil
	}
	if a.tabCloseAsks(tab) {
		a.tabReveal()
		a.askTabClose(tab)
		return nil
	}
	return a.tabDismissNow(tab)
}

// tabDismissNow is the act itself, with the question already answered or never
// worth asking. Every road to a tab leaving the row ends here.
func (a *app) tabDismissNow(tab chatTab) (cmd tea.Cmd) {
	a.tabReveal()
	if tab.start {
		return a.cancelChatStart()
	}
	if a.startingChat() && tab.key == a.frontTabKey() {
		tab.here = true
		back := a.parkChatStart()
		defer func() { cmd = tea.Batch(back, cmd) }()
	}
	if tab.key == "" && !tab.here {
		return nil
	}
	if tab.key != "" && tab.key != a.frontTabKey() {
		a.tabShutKey(tab.key)
		a.touch()
		return nil
	}
	if next, ok := a.lastVisibleTab(); ok {
		cmd, held := a.bringForward(next.file)
		refusal := ""
		if !held {
			cmd, refusal = a.openBeside(next.where, next.file)
		}
		if refusal != "" {
			a.note(refusal)
			return nil
		}
		if a.convKey(a.file) == next.key {
			a.tabShutKey(tab.key)
			a.touch()
		}
		return cmd
	}
	// Home is a place in this window; the last conversation stays behind it.
	a.tabShutKey(tab.key)
	cmd = a.showPage(pageHome)
	a.touch()
	return cmd
}

// Closing selects the last visited surviving tab, including remembered tabs
// whose agent is no longer held. The stable row order is the fallback when a
// tab has not acquired a recency entry yet.
func (a *app) lastVisibleTab() (chatTab, bool) {
	eligible := func(tab chatTab) bool {
		return !tab.start && tab.key != "" && tab.key != a.frontTabKey() && !a.tabShut[tab.key]
	}
	for at := len(a.prev) - 1; at >= 0; at-- {
		for _, tab := range a.chatTabs {
			if tab.key == a.prev[at] && eligible(tab) {
				return tab, true
			}
		}
	}
	for at := len(a.chatTabs) - 1; at >= 0; at-- {
		if tab := a.chatTabs[at]; eligible(tab) {
			return tab, true
		}
	}
	return chatTab{}, false
}

// tabShutKey records one conversation as dismissed from the row. The map is the
// only new state the whole gesture costs, and [app.rememberOpen] is where a key
// leaves it again — every door that brings a conversation forward goes through
// that one function, so there is no road back onto the screen that forgets to
// put the tab back.
func (a *app) tabShutKey(key string) {
	if key == "" {
		return
	}
	// The reopen stack is written HERE, before the row is rebuilt below, because
	// the row is where this conversation's address, workspace and name are kept
	// (tabreopen.go's [app.rememberClosedTab]).
	a.rememberClosedTab(key)
	if a.tabShut == nil {
		a.tabShut = map[string]bool{}
	}
	a.tabShut[key] = true
	// Keep the positions of every surviving tab. Recency is a different order
	// and must not rebuild the row just because one destination was dismissed.
	kept := a.chatTabs[:0]
	for _, tab := range a.chatTabs {
		if tab.key != key {
			kept = append(kept, tab)
		}
	}
	a.chatTabs = kept
	a.chatTabBar = tabBar{}
}

// tabActivePaint gives the chosen tab a full contrasting surface, including
// its close target. Plain terminals retain the existing bracket selection.
func (a *app) tabActivePaint(word string) string {
	if a.pal.profile < tokens.ANSI256 {
		return a.pal.bold(word)
	}
	return a.pal.background(a.pal.bold(a.pal.paint(ansi.Strip(word), a.pal.ramp.selected)), 0, a.pal.ramp.ink)
}
