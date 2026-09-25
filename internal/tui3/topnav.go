package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// ── THE TOP NAV: THE PLACES, ON THE WORDMARK'S ROW, ON EVERY PAGE ───────────
//
//	 >● codeaf   home  teams  chats  sessions  spend  settings   2 want you · $1.20  thu 10:31pm
//	 >● codeaf   home  teams  chats  sessions  spend  more ▾                              $1.20
//
// The first row of every frame is the product's name, the places a person can
// go, and the machine's pulse on the far end (pulse.go). It is drawn by ONE
// function ([app.navLine]) for a place, a conversation, a room inside one and
// the grid of open tabs, so the words are in the same cells whichever page is
// up and a hand that learned where `spend` is finds it there everywhere.
//
// ── THE LAWS ──
//
//   - THE PLACE YOU ARE IN IS LIT IN THE ONE ACCENT and every other word is
//     muted. Inside a conversation, or on the grid of its tabs, the place is
//     `chats` ([app.navLit]): the strip under the nav is the chats, so the word
//     that names them is the one lit over it.
//   - EVERY WORD IS A WORD BUTTON: a one-cell pad either side, a ground under
//     the pointer that covers the pads too, and a press anywhere on that ground
//     goes to the place ([app.navPress]). The pad cells are the target because
//     they are drawn as the button; a one-cell miss beside a word is a miss
//     people make. Where colour cannot show a ground, the pointer's word wears
//     the linear mark `·` in its leading pad, as the strip's tabs do.
//   - THE HINT LINE SAYS WHAT A WORD OPENS AND ITS KEY while the pointer rests
//     on it ([app.headHint]), so a word is never a lone label a person has to
//     press to learn about.
//   - THE GAPS ARE FIXED. One cell of inset at each end of the row, [navLead]
//     cells between the wordmark and the first button, no cells between two
//     buttons (their pads are the air, two blank cells between two words), and
//     at least [navTailGap] cells before the pulse. None of them changes with
//     the width; what a narrow row gives up is words and clauses, never air.
//
// ── THE WIDTH LADDER ──
//
// A row too narrow for everything gives things up IN THE OWNER'S ORDER
// (2026-09-24): the clock goes first (and the machine's name with it, the
// other fact every terminal title already carries), then the counts, the
// moving count before the one that wants you, then the allowance behind the
// day's figure, and only then do the trailing places fold into `more ▾`, one
// word at a time from the right. The day's figure is the last clause to go.
// The wordmark, the place you are in and the word under the bar's cursor
// never fold, and a place wearing a count keeps its word, because a number is
// this row saying something moved in a room you are not in.
//
// `more ▾` IS A DOOR: a press opens a small menu of exactly the places it
// folded (navmore.go), so a narrow row still reaches every place by pointer as
// well as by `alt+N`.

// navRow is the row of the frame the nav is drawn on, the first, over the
// strip. The frame records that it drew one in [app.tabRow], which is -1 on a
// frame with no head at all.
const navRow = 0

// tabStripRow is the row the chat strip is drawn on: under the nav and over
// the rule, on every page.
const tabStripRow = 1

const (
	// navInset is the blank cell at each end of the row, the same inset every
	// row of the head keeps.
	navInset = 1
	// navLead is the air between the wordmark and the first place's button.
	// With the button's own pad it reads as three blank cells, which is what
	// tells the product's name from the first word a person can press.
	navLead = 2
	// navTailGap is the least air between the last button and the pulse, so a
	// clause of the pulse never reads as one more place.
	navTailGap = 2
)

// navMoreWord is the fold's own word, `more ▾`, with the caret every menu on
// this surface wears ([app.tabTeamWord]).
func (a *app) navMoreWord(pal palette) string {
	caret := a.linearMark("▾", "v")
	if pal.ascii {
		caret = "v"
	}
	return "more " + caret
}

// navLit is the place the nav lights: the one a person is standing in, and
// `chats` everywhere that is not a place, since every such page is the chats.
func (a *app) navLit() page {
	if a.pageShowing() {
		return a.page
	}
	return pageChats
}

// navMemo is the nav as it was last laid out, reused while nothing it is drawn
// from has changed. THE ROW IS ON EVERY FRAME OF A SCROLL, and the scroll is
// held to an allocation ceiling (inputsmooth_test.go); a row that said the
// same thing for four thousand lines is laid out once. Every field is compared
// as a value, so the check costs nothing.
type navMemo struct {
	width                  int
	ink                    uint64
	page, hover            page
	bar                    barCursor
	every, moreOn, moreHot bool
	facts                  machineFacts
	minute                 int64
	host                   string
	counts                 [16]int
	profile                tokens.Profile
	ascii, linear, places  bool
	accent, muted          hue
	line                   string
	spans                  []placeTabSpan
	more                   hudSpan
	folded                 []page
}

// navMemoKey is the memo's key half for this frame.
func (a *app) navMemoKey(width int, pal palette) navMemo {
	m := navMemo{width: width, ink: a.inkState, page: a.page, hover: a.tabHover, bar: a.bar,
		every: a.mapShowing, moreOn: a.navMore.on, moreHot: a.navMore.hot, facts: a.machine,
		host: a.host, profile: pal.profile, ascii: pal.ascii, linear: a.linear || pal.linear, places: pal.placeRows,
		accent: pal.ramp.accent, muted: pal.ramp.muted}
	if now := a.now(); !now.IsZero() {
		m.minute = now.Unix() / 60
	}
	for i, id := range placeOrder {
		if i < len(m.counts) {
			m.counts[i] = a.placeCount(id)
		}
	}
	return m
}

func (m navMemo) same(k navMemo) bool {
	return m.line != "" && m.width == k.width && m.ink == k.ink && m.page == k.page && m.hover == k.hover &&
		m.bar == k.bar && m.every == k.every && m.moreOn == k.moreOn && m.moreHot == k.moreHot &&
		m.facts == k.facts && m.minute == k.minute && m.host == k.host && m.counts == k.counts &&
		m.profile == k.profile && m.ascii == k.ascii && m.linear == k.linear && m.places == k.places &&
		m.accent == k.accent && m.muted == k.muted
}

// navLine is the head's first row: the wordmark, the places, and the pulse,
// exactly `width` cells wide at most. It writes where every button landed
// ([app.tabs], [navMore.span]) as it draws them.
func (a *app) navLine(width int, pal palette) string {
	key := a.navMemoKey(width, pal)
	if a.navMemo.same(key) {
		a.tabs = a.navMemo.spans
		a.navMore.span, a.navMore.folded = a.navMemo.more, a.navMemo.folded
		return a.navMemo.line
	}
	line := a.navLay(width, pal)
	key.line = line
	key.spans = append([]placeTabSpan(nil), a.tabs...)
	key.more = a.navMore.span
	key.folded = append([]page(nil), a.navMore.folded...)
	a.navMemo = key
	return line
}

// navLay does the layout [app.navLine] memoises.
func (a *app) navLay(width int, pal palette) string {
	a.tabs = a.tabs[:0]
	a.navMore.span, a.navMore.folded = hudSpan{}, a.navMore.folded[:0]
	// HOME'S LINE LEAVES ITS COUNTS TO THE PANELS UNDER IT, and every other
	// frame's carries them (pulse.go's [pulseBudget]).
	mode := pulseWhole
	if a.at(pageHome) {
		mode = pulseBudget
	}
	name := strings.Repeat(" ", navInset) + pal.wordmark(width)
	lead := ansi.StringWidth(name) + navLead
	lit := a.navLit()
	shown := barPages(lit, a.mapShowing)
	cost := func(id page) int { return ansi.StringWidth(a.barChipWord(id, a.mapShowing)) + tabPadCols }
	all := 0
	for _, id := range shown {
		all += cost(id)
	}
	tails := a.navTails(pal, mode)
	tailCost := func(tail string) int {
		if tail == "" {
			return 0
		}
		return navTailGap + ansi.StringWidth(tail)
	}
	// FIRST THE PULSE GIVES UP ITS CLAUSES, every place still on the row.
	for _, tail := range tails {
		if lead+all+tailCost(tail)+navInset <= width {
			return a.navPaint(width, pal, name, lead, shown, nil, tail)
		}
	}
	// THEN THE TRAILING PLACES FOLD INTO `more ▾`, from the right, with the
	// day's figure still on the row, and last of all the figure goes too.
	keep := func(id page) bool { return id == lit || a.barKeeps(id) || a.placeCount(id) > 0 }
	moreCost := ansi.StringWidth(a.navMoreWord(pal)) + tabPadCols
	fixed := 0
	var free []page
	for _, id := range shown {
		if keep(id) {
			fixed += cost(id)
		} else {
			free = append(free, id)
		}
	}
	for _, tail := range []string{tails[len(tails)-1], ""} {
		used := fixed
		for _, id := range free {
			used += cost(id)
		}
		for n := len(free) - 1; n >= 0; n-- {
			used -= cost(free[n])
			if lead+used+moreCost+tailCost(tail)+navInset <= width {
				return a.navPaint(width, pal, name, lead, shown, free[n:], tail)
			}
		}
	}
	// A ROW TOO NARROW EVEN FOR THE NAME, THE PLACE YOU ARE IN AND THE FOLD is
	// drawn with every foldable word folded and cut at the frame's edge; a
	// button the cut reached is not a target ([app.navPaint]).
	return a.navPaint(width, pal, name, lead, shown, free, "")
}

// navTails is every spelling the pulse end of the row may take, widest first,
// in the order the ladder gives clauses up (this file's header). The last one
// is the day's figure alone, which the places fold beside.
func (a *app) navTails(pal palette, mode pulseMode) [6]string {
	p := a.pulseParts(a.now(), pal, mode)
	host := ""
	if name := strings.TrimSpace(a.host); name != "" {
		// THE MACHINE THESE PAGES ARE ABOUT, dim, and nothing at all on a local
		// session: it is invisible when there is nothing to say (host.go). It is
		// `on spark` rather than a bare `spark` so it cannot read as a place.
		host = pal.dim(placeMachineLead + name)
	}
	rung := func(parts ...string) string {
		out := ""
		for _, part := range parts {
			if part == "" {
				continue
			}
			if out != "" {
				out += pulseGap
			}
			out += part
		}
		return out
	}
	return [6]string{
		rung(p.wants, p.hands, p.money, host, p.clock),
		rung(p.wants, p.hands, p.money, host),
		rung(p.wants, p.hands, p.money),
		rung(p.wants, p.money),
		rung(p.money),
		rung(p.spend),
	}
}

// navPaint draws the row with `folded` behind `more ▾` and `tail` on the far
// end, and records where every button landed.
func (a *app) navPaint(width int, pal palette, name string, lead int, shown, folded []page, tail string) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteString(strings.Repeat(" ", navLead))
	at := lead
	lit := a.navLit()
	isFolded := func(id page) bool {
		for _, f := range folded {
			if f == id {
				return true
			}
		}
		return false
	}
	for _, id := range shown {
		if isFolded(id) {
			continue
		}
		word := a.barChipWord(id, a.mapShowing)
		w := ansi.StringWidth(word) + tabPadCols
		b.WriteString(a.navChipPaint(pal, id, word, id == lit))
		// A BUTTON THE FRAME'S EDGE CUT IS NOT A TARGET: a press resolves
		// against what was drawn, and half a word is not a door.
		if at+w <= width {
			a.tabs = append(a.tabs, placeTabSpan{id: id, from: at, to: at + w})
		}
		at += w
	}
	if len(folded) > 0 {
		word := a.navMoreWord(pal)
		w := ansi.StringWidth(word) + tabPadCols
		b.WriteString(a.navMorePaint(pal, word))
		if at+w <= width {
			a.navMore.span = hudSpan{from: at, to: at + w}
			a.navMore.folded = append(a.navMore.folded, folded...)
		}
		at += w
	}
	line := b.String()
	if tail != "" {
		line += strings.Repeat(" ", max(width-at-ansi.StringWidth(tail)-navInset, navTailGap)) + tail + strings.Repeat(" ", navInset)
	}
	if ansi.StringWidth(line) > width {
		return ansi.Truncate(line, width, "")
	}
	return line + strings.Repeat(" ", width-ansi.StringWidth(line))
}

// navChipPaint is one place's button: the cursor's band while the bar's
// cursor is on it, the pointer's ground under the pointer, the one accent on
// the place you are in, and muted otherwise.
func (a *app) navChipPaint(pal palette, id page, word string, lit bool) string {
	chip := tabPad + word + tabPad
	ink, hover := pal.muted, pal.ink
	if lit {
		ink = func(s string) string { return pal.bold(pal.accent(s)) }
		hover = ink
	}
	switch {
	case a.bar.on && id == a.bar.at:
		// THE CURSOR'S OWN BAND, and it replaces the hover and the lit ink
		// rather than stacking on them: while the cursor is up here the row
		// is the one a person is standing on ([barCursor]).
		return pal.cursor(pal.bold(pal.ink(chip)), 0)
	case id == a.tabHover:
		return a.navHoverPaint(pal, word, hover)
	case lit && pal.profile == tokens.NoColor:
		// WITH NO COLOUR TO LIGHT IT, the place you are in wears brackets in
		// its pad cells, as the strip's tab in front does ([tabLabel]); the
		// button is the same width either way.
		return pal.bold("[" + word + "]")
	}
	return ink(chip)
}

// navHoverPaint is a word button under the pointer: the strip's own hover
// ground ([app.tabHoverPaint]) over the pads and the word, and the linear mark
// in the leading pad where the terminal cannot show a ground.
func (a *app) navHoverPaint(pal palette, word string, ink func(string) string) string {
	chip := tabPad + word + tabPad
	if pal.profile < tokens.ANSI256 {
		chip = a.linearMark("·", ".") + word + tabPad
	}
	if pal.linear {
		return ink(chip)
	}
	return pal.background(ink(chip), 0, pal.ramp.mark)
}

// navMorePaint is the fold's button: muted, on the pointer's ground under the
// pointer and while its menu is open, as the team chip is.
func (a *app) navMorePaint(pal palette, word string) string {
	if a.navMore.hot || a.navMore.on {
		return a.navHoverPaint(pal, word, pal.ink)
	}
	return pal.muted(tabPad + word + tabPad)
}

// ── THE POINTER ─────────────────────────────────────────────────────────────

// navAt is whether row y is the nav: the row the last frame drew it on, on a
// frame that still has one. A CONVERSATION UNDER THE STRIP'S FLOORS DRAWS NO
// HEAD AT ALL ([app.tabsHeight]), and its body starts on row zero; the same
// question [app.tabAt] asks of the strip keeps a press there from being read
// as a press on a nav that a taller frame drew a moment ago.
func (a *app) navAt(y int) bool {
	if a.tabRow < 0 || y != a.tabRow {
		return false
	}
	if a.pageShowing() || a.headCovers() {
		return true
	}
	width, _ := a.size()
	return a.tabsHeight(width) > 0
}

// navPress is a press on the nav's row: the place whose button it landed in,
// or the fold's menu. THE WHOLE ROW IS THE NAV'S, so a press in the air
// between two buttons, or on the wordmark or the pulse, stops here rather than
// falling through to whatever the page under it would make of row zero.
//
// PRESSING THE PLACE YOU ARE ALREADY IN DOES NOTHING. Going there is closing
// and reopening it, which throws away the filter somebody typed and the row
// they were standing on. The one exception is `chats` pressed over a page that
// covers the chats (the run's work tab): that is the way back to them.
func (a *app) navPress(x, y int) (tea.Cmd, bool) {
	if !a.navAt(y) {
		return nil, false
	}
	if a.navMore.span.holds(x) {
		if a.navMore.on {
			a.closeNavMore()
		} else {
			a.openNavMore()
		}
		return nil, true
	}
	for _, span := range a.tabs {
		if x < span.from || x >= span.to {
			continue
		}
		if a.headCovers() {
			a.headUncover()
		}
		if span.id == a.navLit() {
			return nil, true
		}
		return a.showPage(span.id), true
	}
	return nil, true
}

// headCovers is whether a full-frame view that is not a place is drawn over
// the chats: the grid of open tabs, or the run's work tab. Both wear the head,
// and the nav's doors lead out of them.
func (a *app) headCovers() bool { return a.wall.on || a.workTabOn }

// headUncover takes those views down, so the page a nav word opens is the page
// on screen.
func (a *app) headUncover() {
	if a.wall.on {
		a.closeWall()
	}
	if a.workTabOn {
		a.workTabOn = false
		a.closeTaskPlan()
		a.chatTabBar = tabBar{}
	}
	a.touch()
}

// navHover is the pointer over the nav's row: the button under it takes the
// pointer's ground and nothing else on the frame moves. It reports whether the
// pointer is on the row at all.
//
// THE POINTER LEAVING THE ROW IS NEWS TOO, and it is the half that is easy to
// forget: a word left lit after the hand moved away is a door that claims to be
// under a pointer that is somewhere else.
func (a *app) navHover(x, y int) bool {
	if !a.navAt(y) {
		a.barHover(pageNone)
		a.navMoreHot(false)
		return false
	}
	under := pageNone
	for _, span := range a.tabs {
		if x >= span.from && x < span.to {
			under = span.id
			break
		}
	}
	a.barHover(under)
	a.navMoreHot(a.navMore.span.holds(x))
	return true
}

// headHover is the pointer over the head: the nav on every page, and the
// strip on a place, where the conversation's own hover ([app.setHover]) is not
// asked. It reports whether the pointer is on a row the head answers for.
func (a *app) headHover(x, y int) bool {
	onNav := a.navHover(x, y)
	if !a.pageShowing() {
		if onNav {
			// THE CONVERSATION'S OWN HOVER LETS GO of whatever it was on, a tab
			// of the strip included, since the pointer is on neither.
			a.setHover(x, y)
		}
		return onNav
	}
	next := hoverAt{}
	onStrip := false
	if !onNav && y == tabStripRow && a.tabRow >= 0 {
		if at, ok := a.tabHoverAt(x, y); ok {
			next, onStrip = at, true
		} else {
			_, onStrip = a.tabAt(x, y)
		}
	}
	if next != a.hot && (next.kind == hoverTab || a.hot.kind == hoverTab) {
		a.hot = next
		a.touch()
	}
	return onNav || onStrip
}

// headStripPress is a press on the strip while a place is up. THE STRIP IS ON
// EVERY PAGE AND IT MEANS ONE THING ON ALL OF THEM: a tab opens its chat, `+`
// opens a new one, the chip opens the team switcher and `▦ All` the grid of
// open tabs. A press that opens a chat leaves the place first, so the chat is
// what is on screen; the chip and the grid keep it underneath.
func (a *app) headStripPress(x, y int) (tea.Cmd, bool) {
	if !a.pageShowing() || a.tabRow < 0 || y != tabStripRow {
		return nil, false
	}
	hit, ok := a.tabAt(x, y)
	if !ok {
		return nil, false
	}
	switch hit.kind {
	case tabOther, tabHere, tabNew, tabManager:
		leave := a.showPage(pageNone)
		cmd, _ := a.tabPress(x, y)
		return tea.Batch(leave, cmd), true
	}
	cmd, _ := a.tabPress(x, y)
	return cmd, true
}

// ── THE HINT ────────────────────────────────────────────────────────────────

// headHint is the hint line's sentence while the pointer rests on a word of
// the head that is a door the key legend does not already name, and "" when it
// rests on none. It is `alt+2 teams · the teams you hand work to`: the key,
// the word, and what it opens.
//
// `chats` AND `▦ All` ARE TOLD APART HERE, because they sit one over the other
// and both are about conversations: `chats` is the place, every conversation
// one at a time, and `▦ All` is the grid of the tabs this window has open,
// all at once.
func (a *app) headHint() string {
	switch {
	case a.navMore.on:
		return navMoreFootWords
	case placeFor(a.tabHover) != nil:
		return a.chords.say(placeChord(a.tabHover)+" "+a.tabHover.word()) + hintSegment + placeFor(a.tabHover).about()
	case a.navMore.hot:
		return "more · the places this row has no room for"
	case a.hot.kind == hoverTab:
		// THE STRIP'S DOORS SAY WHAT THEY SAY IN A CONVERSATION, in the one
		// place those sentences are written (walldock.go's
		// [app.dockHoverWords]): `▦ All`, the team chip, the manager's place.
		return a.dockHoverWords()
	}
	return ""
}
