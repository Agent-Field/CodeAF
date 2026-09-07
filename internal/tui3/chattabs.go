package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE TAB STRIP: WHICH CONVERSATIONS THIS WINDOW HAS, AND WHICH ONE IS UP ──
//
// The frame's first row used to be ONE WORD — the conversation's own name, with
// a `▾` after it where the switcher had somewhere to go (roomcrumbs.go's bar,
// which this replaces). It answered "what is this page" and nothing else, so the
// other conversations a person had been in all day were reachable only through a
// keystroke they had to know about, and there was nowhere on screen that said
// they existed at all.
//
// So the top of the frame is now TWO INSTRUMENTS ON TWO ROWS, and the split is
// the whole point:
//
//	 chat one · ▸the tree walk · chat three   ▾      ← the tabs: which conversation
//	 main ▸ Ship the port ▸ Cut the goldens          ← the trail: where inside it
//
// The tabs are CONVERSATIONS. The trail under them is the chain of work inside
// the one that is up, and it is drawn only when there is a chain — a task page
// has one, the conversation itself does not, and a row that said `main` and
// nothing else every day would be a row spending a line of somebody's terminal
// on a fact they can read off the tab above it.
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
// So there is no `✕` on a tab and no `+` at the end of the strip. Closing is
// `ctrl+w` and it closes an agent this process is HOLDING, which over a shared
// handle is never true of anything but the one in front; a mark drawn on every
// tab that only worked on some of them is worse than no mark at all (the
// capability law: a control that cannot work is absent, not broken).
//
// ── AND WHY THE ORDER NEVER MOVES ───────────────────────────────────────────
//
// [app.prev] is a RECENCY stack — the order `tab` walks and the switcher's own
// ring — and a strip drawn from it would re-order itself on every switch, which
// is the one thing tabs may not do: a person reaches for the position, not for
// the word. So the strip keeps its OWN list in the order each conversation was
// first entered ([app.chatTabs]), and recency decides only which tab is up and, when
// the list outgrows the cap, which one falls off the end.

const (
	// tabSep separates padded click targets without making the header read
	// like the telemetry line below the transcript.
	tabSep = " "
	// tabWordCap is the widest a tab is drawn on a frame with room to spare. A
	// tab is a label a person recognises rather than a sentence they read, and
	// past about this many cells one long name is the whole strip.
	tabWordCap = 32
	// tabWordFloor is the fewest cells a name is worth cutting to. Under it the
	// word has stopped identifying a conversation and the strip is better off
	// folding the tab into the count at its end.
	tabWordFloor = 6
	// tabsCap is how many tabs the strip remembers. It is the switcher's own
	// order it falls back on when it overflows, so the tab that goes is the one
	// nobody has been in for longest.
	tabsCap = 8
	// tabMoreWord is the control at the strip's right end where nothing is
	// hidden: the same `▾` the conversation's bar wore, opening the same picker
	// `ctrl+k` opens.
	tabMoreWord = "▾"
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
	// here says this is the conversation in front. It is the ONE tab that is
	// lit, whatever page of it is on screen.
	here bool
	// held says this process is still holding the agent behind this tab
	// (keeper.go). It is not drawn — the strip makes no claim about what is
	// running — and it decides only whether a press is an attach or an open.
	held bool
}

// tabKind is what one drawn piece of the strip IS, which is what decides whether
// it is a door and what that door does.
type tabKind int

const (
	// tabHere is the conversation in front. Its door is "come back out of the
	// page you walked into", which is the root crumb's door and `esc`'s; on the
	// conversation itself there is nowhere to go and it is inert.
	tabHere tabKind = iota
	// tabOther is another conversation: a switch.
	tabOther
	// tabMore is the control at the right end — the picker, and the count of
	// what the frame could not spell.
	tabMore
	// tabFold is that same count on a frame where the picker cannot open. It is
	// drawn because it is true and it does nothing, exactly as the trail's own
	// `…` is inert when everything it hides is (roomcrumbs.go's law 4).
	tabFold
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

// door reports whether pressing this piece would take a person anywhere.
func (h tabHit) door(a *app) bool {
	switch h.kind {
	case tabHere:
		return a.roomOpen()
	case tabOther:
		return true
	case tabMore:
		return true
	}
	return false
}

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
	// hot is the column of the tab the pointer was on, or -1.
	hot  int
	more bool
	tabs []chatTab
	line string
	hits []tabHit
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
// it can still name and still reach, in the order it first entered them.
//
// IT REFILLS THE SURFACE'S OWN SLICE IN PLACE, filtering forwards over the list
// it is reading, and it asks the previous-stack with a walk rather than with a
// map. Both are for the same reason: this runs on every frame, a map is two
// allocations and a slice is one, and a row that says the same thing all day may
// not spend the frame's allocation budget saying it (PERF.md's scroll law). Both
// lists are bounded by [tabsCap], so a walk is the cheaper structure anyway.
//
// It is called ONCE PER FRAME, by the draw. The geometry ([app.tabsHeight]) does
// not ask it — there is always at least one tab, the conversation on screen — so
// nothing else can observe the slice mid-refill.
func (a *app) tabList() []chatTab {
	front := a.frontTabKey()
	tabs := a.chatTabs[:0]
	for _, tab := range a.chatTabs {
		if tab.key == "" || tabsHold(tabs, tab.key) {
			continue
		}
		held := a.behind[tab.key]
		// The previous-stack is what says a conversation is still one this window
		// has: [app.closeFront] takes a closed one off it, so a tab whose key has
		// left it is a tab whose conversation is gone.
		if tab.key != front && held == nil && !keysHold(a.prev, tab.key) {
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
	for _, key := range a.prev {
		if tabsHold(tabs, key) {
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
// rather than mapped: both lists are bounded by [tabsCap] and a walk of eight
// costs no allocation at all.
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
			tab.word = a.chatCrumbWord()
		}
		tab.file, tab.where = a.file, a.workspace
	case held != nil:
		tab.word = hopTitle(held.conv.Agent, held.side)
		tab.file, tab.where = held.conv.SessionFile, held.conv.Workspace
	}
	if strings.TrimSpace(tab.file) == "" {
		tab.file = tab.key
	}
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

// tabsHeight is what the strip costs the body region, and it is asked rather
// than assumed for [app.headHeight]'s reason: a row the frame drew and the
// scrolling did not subtract puts the last row of the conversation under the
// input box.
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
	return 1
}

// roomHeadRow is the frame row the room's own header — the trail, the state, the
// ✕ — is drawn on, which is the row under the tab strip wherever there is one.
//
// EVERY POINTER TARGET UP HERE RESOLVES THROUGH IT. The strip, the trail and the
// ✕ are three rows' worth of controls stacked in whatever order the frame can
// afford, and a press answered against a hard-coded zero would open the wrong
// one the moment the strip stood down (hover.go's law).
func (a *app) roomHeadRow() int {
	width, _ := a.size()
	return a.tabsHeight(width)
}

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
	if len(tabs) == 0 {
		return ""
	}
	hot := -1
	if a.hot.kind == hoverTab {
		hot = a.hot.index
	}
	more := a.hopAvailable()
	if memo := a.chatTabBar; memo.same(width, a.inkState, hot, more, tabs) {
		a.chatTabHits = memo.hits
		return memo.line
	}
	pieces, hits := a.tabsFit(tabs, max(width-headLabelAt, 0))
	if len(pieces) == 0 {
		return ""
	}
	a.chatTabHits = tabsAt(hits, headLabelAt)
	line := strings.Repeat(" ", headLabelAt) + a.tabsPaint(pieces)
	a.chatTabBar = tabBar{width: width, ink: a.inkState, hot: hot, more: more, line: line, hits: a.chatTabHits,
		tabs: append([]chatTab(nil), tabs...)}
	return line
}

// tabPiece is one drawn segment of the strip: the word, and what it is.
type tabPiece struct {
	word string
	kind tabKind
	tab  chatTab
	// sep says this piece is the punctuation between two tabs. It answers to
	// nothing and is painted at the strip's quietest step.
	sep bool
}

// tabsFit lays the strip out in the cells it has, and the ladder it walks is one
// law: THE TAB THAT IS UP IS ALWAYS ON THE STRIP. What gives way is the tabs at
// the ends, and they give way to a count — never to silence, because a strip
// that quietly drew three of somebody's six conversations would be a strip that
// says they have three.
func (a *app) tabsFit(tabs []chatTab, room int) ([]tabPiece, []tabHit) {
	if room <= 0 || len(tabs) == 0 {
		return nil, nil
	}
	active := 0
	for at, tab := range tabs {
		if tab.here {
			active = at
		}
	}
	// The names, cut to a tab's own width: a share of the row where there are
	// several, and the whole row where there is one, because a lone tab is the
	// conversation's own name and the row has nothing else to spend itself on.
	wordCap := room
	if len(tabs) > 1 {
		wordCap = max(tabWordFloor, min(tabWordCap, room/len(tabs)))
	}
	words := make([]string, len(tabs))
	widths := make([]int, len(tabs))
	for at, tab := range tabs {
		words[at] = tabLabel(tab, wordCap)
		widths[at] = ansi.StringWidth(words[at])
	}
	// The right end is reserved BEFORE the fitting, because a control squeezed in
	// afterwards would be a control drawn over the last tab's own cells. Two cells
	// hold `…7`, which is the widest count a strip of [tabsCap] can report, and
	// one holds the `▾`.
	sepW := ansi.StringWidth(tabSep)
	reserve := 0
	if len(tabs) > 1 || a.hopAvailable() {
		reserve = sepW + 2
	}
	budget := room - reserve
	if budget < tabWordFloor {
		budget = room
		reserve = 0
	}
	from, to := active, active+1
	for start := 0; start <= active; start++ {
		at, end := 0, start
		for i := start; i < len(tabs); i++ {
			lead := 0
			if i > start {
				lead = sepW
			}
			if widths[i] == 0 || at+lead+widths[i] > budget {
				break
			}
			at, end = at+lead+widths[i], i+1
		}
		if end > active {
			from, to = start, end
			break
		}
	}
	pieces := make([]tabPiece, 0, 2*(to-from)+2)
	hits := make([]tabHit, 0, to-from+1)
	at := 0
	for i := from; i < to; i++ {
		if i > from {
			pieces = append(pieces, tabPiece{word: tabSep, sep: true})
			at += sepW
		}
		word := words[i]
		if to-from == 1 {
			// The one tab that is left takes whatever the row has, cut. A name with
			// an ellipsis in it still says which conversation this is; a blank row
			// says nothing at all.
			word = tabLabel(tabs[i], budget)
		}
		kind := tabOther
		if tabs[i].here {
			kind = tabHere
		}
		width := ansi.StringWidth(word)
		if width == 0 {
			continue
		}
		pieces = append(pieces, tabPiece{word: word, kind: kind, tab: tabs[i]})
		hits = append(hits, tabHit{span: hudSpan{from: at, to: at + width}, kind: kind, tab: tabs[i]})
		at += width
	}
	if hidden := len(tabs) - (to - from); reserve > 0 {
		word, kind := "", tabMore
		switch {
		case hidden > 0:
			// THE COUNT IS A FACT AND THE PICKER IS A DOOR, and they are the same
			// mark: pressing it opens the list every hidden conversation is on. Where
			// the picker cannot open — a decision already on screen, a frozen
			// viewport (hop.go's [app.hopMayOpen]) — the count is still true and is
			// drawn inert, which is what the trail's own fold does with ancestors it
			// cannot open.
			word = glyphMore + itoa(hidden)
			if !a.hopAvailable() {
				kind = tabFold
			}
		case a.hopAvailable():
			word = tabMoreWord
		}
		if width := ansi.StringWidth(word); width > 0 && at+sepW+width <= room {
			pieces = append(pieces, tabPiece{word: tabSep, sep: true})
			at += sepW
			pieces = append(pieces, tabPiece{word: word, kind: kind})
			hits = append(hits, tabHit{span: hudSpan{from: at, to: at + width}, kind: kind})
		}
	}
	return pieces, hits
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

// tabsPaint uses the existing selected-surface tint for the current chat.
// Spacing distinguishes the controls from prose; the current tab also retains
// a plain-text marker so color is never its only sign of selection.
func (a *app) tabsPaint(pieces []tabPiece) string {
	hot, lit := a.hotTab()
	line := ""
	for _, piece := range pieces {
		switch {
		case piece.sep:
			line += a.pal.dim(piece.word)
		case piece.kind == tabHere:
			line += a.pal.underline(a.pal.tint(piece.word, a.pal.accent))
		case lit && hot.kind == piece.kind && hot.tab.key == piece.tab.key && hot.kind != tabMore:
			// ONE STEP UP FROM WHERE THE ROW ALREADY IS, which is this surface's
			// whole answer to a pointer (render.go): a brightening, never a band.
			line += a.pal.accent(piece.word)
		case lit && piece.kind == tabMore && hot.kind == tabMore:
			line += a.pal.accent(piece.word)
		default:
			line += a.pal.dim(piece.word)
		}
	}
	return line
}

// hotTab is the piece the pointer is on, when it is on one that would do
// something. A piece that is not a door never lights: what lights is what a
// press acts on.
func (a *app) hotTab() (tabHit, bool) {
	if a.hot.kind != hoverTab {
		return tabHit{}, false
	}
	for _, hit := range a.chatTabHits {
		if hit.span.from == a.hot.index && hit.door(a) {
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
	if y != 0 || a.tabsHeight(width) == 0 {
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
	if !ok || !hit.door(a) {
		return hoverAt{}, false
	}
	return hoverAt{kind: hoverTab, index: hit.span.from}, true
}

// tabPress answers a press on the strip and reports whether it took it.
//
// A PIECE THAT IS NOT A DOOR TAKES THE PRESS AND DOES NOTHING WITH IT. The tab
// that is up is drawn because it is true, and a press on it that opened
// something else would be the strip acting on a promise it never made. The row
// is the strip's own and has nothing under it, so a press between two tabs is a
// press on the row and stops there.
func (a *app) tabPress(x, y int) (tea.Cmd, bool) {
	hit, ok := a.tabAt(x, y)
	if !ok {
		return nil, false
	}
	if !hit.door(a) {
		return nil, true
	}
	switch hit.kind {
	case tabMore:
		// THE OVERFLOW IS THE PICKER AND NOT A MENU OF ITS OWN. `ctrl+k` already
		// draws every conversation this machine has, ranked, with what each of them
		// wants from you on it (hop.go); a second list built here would be a second
		// answer to the same question, kept in step with the first by nothing.
		a.hopOpen()
		return nil, true
	case tabHere:
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
func (a *app) tabGo(tab chatTab) tea.Cmd {
	if cmd, ours := a.bringForward(tab.file); ours {
		return cmd
	}
	return a.hopStart(hopRow{file: tab.file, where: tab.where, title: tab.word})
}
