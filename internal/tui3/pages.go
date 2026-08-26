package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE PLACES ──────────────────────────────────────────────────────────────
//
// A person reads the word PLACE. The code writes the word `page`, and the two
// vocabularies are deliberate: `place` is already spoken for in this repository
// and means something else entirely — [session.PlacesRoot], [app.placesRoot],
// `placesDirName`, `SweepPlaces`, `homePlacesCol` and a hundred more identifiers
// all mean THE DIRECTORY OF CONVERSATIONS AND PROJECTS ON THIS DISK. A router
// that reused the word would have `place` meaning a folder in one file and a
// screen in the next, which is how a codebase stops being readable. So the Go
// identifier is `page` everywhere, the manual and every string a person sees say
// *place*, and this comment is the bridge between them.
//
// THE SEVEN ARE A LIST AND NOT A SWITCH. The tab bar's order, the numbers
// `alt+1`…`alt+7` jump to, and the order `tab` walks are ONE fact, held in
// [pages], so a place added later is a row in that slice and nothing else.
//
// The rewind timeline is NOT one of them. It is still a page reached by
// `/rewind`, because it is a thing you do to this conversation rather than a
// room in the machine, and putting it in the bar would put a knife in the
// cutlery drawer.
type page uint8

const (
	pageHome page = iota
	pageTasks
	pageStanding
	pageMemory
	pageSpend
	pageSearch
	pageSettings
)

// pages is the whole set, in the one order that matters: left to right along
// the tab bar, `alt+1` through `alt+7`, and the circle `tab` walks.
//
// THE ORDER IS THE READING ORDER OF A DAY. What wants you (home), what ran
// (tasks), what runs without being asked (standing), what was learned (memory),
// what it cost (spend), then the two that are asked for rather than looked at —
// finding something, and changing something.
func pages() []page {
	return []page{pageHome, pageTasks, pageStanding, pageMemory, pageSpend, pageSearch, pageSettings}
}

// word is the one lowercase word a place is called, on the tab bar and in the
// manual. It is the whole of a place's name: a tab bar of two-word labels is a
// menu, and this is a bar.
func (p page) word() string {
	switch p {
	case pageHome:
		return "home"
	case pageTasks:
		return "tasks"
	case pageStanding:
		return "standing"
	case pageMemory:
		return "memory"
	case pageSpend:
		return "spend"
	case pageSearch:
		return "search"
	case pageSettings:
		return "settings"
	}
	return ""
}

// counted answers whether a number in front of a place would mean anything.
//
// A COLLECTION CAN BE COUNTED AND A STATE CANNOT. Home, tasks, standing and
// memory each hold a pile of things, so "two of them changed" is a fact about
// the place. Spend is a sum, search is something you do, and settings is how
// this machine is set — a number in front of any of the three would be a number
// about nothing, and the tab bar would be teaching a lie about what is in there.
func (p page) counted() bool {
	switch p {
	case pageHome, pageTasks, pageStanding, pageMemory:
		return true
	}
	return false
}

// explain is what a place says when it has nothing of its own to draw yet: three
// sentences telling a person what the place is for.
//
// AN ALMOST-EMPTY PAGE IS THE BEST TEACHER ON THE MACHINE. It can spend the
// whole screen saying what it is for, and a person only ever arrives at one of
// these by walking into it, which is exactly the moment the explanation is
// wanted. A place that HAS a body returns nothing here — a teaching paragraph
// over a list is a page talking over itself.
func (p page) explain() string {
	switch p {
	case pageSpend:
		return "What this machine has cost, by the day, by the model, and by what it was for. " +
			"Every model call writes a line, so the figures here are the bill and not an estimate. " +
			"There is nothing to set here — the allowance is edited on the status line that shows it."
	}
	// SEARCH IS NOT HERE ANY MORE, because it has a body: its own three
	// sentences while the box is empty, and results the moment anything is typed
	// into it (searchplace.go's [searchTeach]). A place that HAS a body returns
	// nothing here — a teaching paragraph over a list is a page talking over
	// itself, which is this function's own rule applied to itself.
	return ""
}

// parsePageWord is the typed surface's half of the tab bar: a person who types
// `sta` is offered the standing place beside the chats that match (SCREEN 1g).
//
// AN EXACT WORD BEATS A PREFIX, and a prefix that fits two places is no answer
// at all — `s` is `standing`, `spend`, `search` and `settings` at once, and
// offering the first of those would be the surface guessing. So an ambiguous
// prefix offers nothing, and the person types one more letter.
func parsePageWord(s string) (page, bool) {
	word := strings.ToLower(strings.TrimSpace(s))
	if word == "" {
		return 0, false
	}
	for _, id := range pages() {
		if id.word() == word {
			return id, true
		}
	}
	found, count := page(0), 0
	for _, id := range pages() {
		if strings.HasPrefix(id.word(), word) {
			found, count = id, count+1
		}
	}
	return found, count == 1
}

// ── the counts a tab may wear ───────────────────────────────────────────────

// placeCounts is the seam the tab bar's numbers come through, and it is stated
// as an interface here because the records that answer it live in
// internal/session and are another lane's to build.
//
// A TAB WEARS A COUNT ONLY WHEN SOMETHING IN IT CHANGED. Not how many things are
// in there — a permanent `20` beside `home` is furniture, and furniture is what
// people stop seeing. The question is "how many things in this place have moved
// since you last looked AT THIS PLACE", which needs one look stamp per place
// rather than the single stamp home writes today, and until those stamps exist
// this seam is nil and every tab is bare. THAT IS THE CORRECT EMPTY STATE and
// not a gap: the emptiness law says an unknown number is drawn as nothing.
//
// It must NOT block. The tab bar is drawn on every frame of every place, so an
// implementation that walks a directory here is an implementation that walks it
// sixty times a second — the same law every home seam is held to (tui3.go).
type placeCounts interface {
	// ChangedIn is how many things in one place have moved since that place was
	// last looked at. Zero, or any negative number, draws nothing.
	ChangedIn(place string) int
}

// placeCount is the count for one place, or zero when nothing answers.
func (a *app) placeCount(id page) int {
	if a.places == nil || !id.counted() {
		return 0
	}
	if n := a.places.ChangedIn(id.word()); n > 0 {
		return n
	}
	return 0
}

// ── the tab bar ─────────────────────────────────────────────────────────────

// placeTabBar is the second row of every place: the seven words, the one you are
// standing in wearing the band, and a number beside any place that has something
// new in it.
//
// IT IS [sheetTabBar] WITH THE TITLES PASSED IN, and it is drawn with that
// function's own geometry — [tabLead], [tabGap], [tabPad] — for the reason that
// function's comment already gives: "this panel IS a tab bar — the same object
// the task strip is, drawn the same way, so that 'which page am I on' is one
// visual question across the app rather than two". The settings panel keeps its
// own inner bar under this one, and the two are told apart by what they are
// made of rather than by a decoration: this one is the seven places, that one is
// settings' own sections.
//
// ── THE WIDTH LADDER ────────────────────────────────────────────────────────
//
// A bar that is cut in half is a bar that lies about how many places there are,
// so it gives up words in a stated order rather than being trimmed:
//
//  1. every word, every count — while they fit;
//  2. the place you are standing in, and the places with something new in them —
//     which is the whole reading a narrow bar has room to be useful for;
//  3. the place you are standing in, alone.
//
// `numbered` is the map ([app.mapShowing]): every chip grows the digit that
// jumps to it, in the cells the words were already in, and nothing moves that a
// person has to re-find when the map goes away.
func (a *app) placeTabBar(width int, numbered bool, pal palette) string {
	full, ok := a.tabBarAt(width, numbered, pal, func(id page) bool { return true })
	if ok {
		return full
	}
	worth := func(id page) bool { return id == a.page || a.placeCount(id) > 0 }
	if some, ok := a.tabBarAt(width, numbered, pal, worth); ok {
		return some
	}
	alone, _ := a.tabBarAt(width, numbered, pal, func(id page) bool { return id == a.page })
	return alone
}

// tabBarAt draws the bar over the places `keep` admits, and says whether it fit.
func (a *app) tabBarAt(width int, numbered bool, pal palette, keep func(page) bool) (string, bool) {
	line, plain := strings.Repeat(" ", tabLead), strings.Repeat(" ", tabLead)
	first := true
	for i, id := range pages() {
		if !keep(id) {
			continue
		}
		if !first {
			line += strings.Repeat(" ", tabGap)
			plain += strings.Repeat(" ", tabGap)
		}
		first = false
		word := id.word()
		if numbered {
			// THE MAP GROWS THE NUMBER IN THE CELL THE WORD WAS ALREADY IN
			// (SCREEN 3b). Nothing shifts, nothing pops up, and letting go of the
			// map leaves the bar exactly where the eye left it.
			word = itoa(i+1) + " " + word
		}
		if n := a.placeCount(id); n > 0 {
			word += " " + itoa(n)
		}
		chip := tabPad + word + tabPad
		if id == a.page {
			// THE WORD YOU ARE STANDING IN IS TIER 1, BOLD, AND NOT AN ACCENT.
			// SCREEN 2a's first level is spelled out: "1 · page — bright, bold,
			// one word, only in the tab bar", and the accent on a place is spent
			// on the two live states and on nothing else (styles.go's THE
			// ONE-ACCENT LAW). The band under it is what says "here".
			line += pal.selected(pal.bold(pal.ink(chip)), ansi.StringWidth(word)+tabPadCols)
		} else {
			line += pal.dim(chip)
		}
		plain += chip
	}
	return line, ansi.StringWidth(plain) <= width
}

// ── the frame every place is drawn in ───────────────────────────────────────

// placeRow is one row of a place's body: the text, and whatever that place
// resolves a pointer against. The hit is a type parameter because the places
// answer the pointer in their own words — the task page in `taskSheetHit`, the
// settings panel in `sheetHit`, home in a line number and two column maps — and
// a router that flattened all of them into one int would be a router that lets a
// click land on a row the draw did not put there.
type placeRow[H any] struct {
	text string
	hit  H
}

// placeFrame is THE frame. Every place is drawn in it, and the head, the foot
// and the clamp below belong to the router rather than to any place:
//
//	row 0        the pulse — this machine's vital signs (pulse.go)
//	row 1        the tab bar — the seven places, and where you are
//	row 2        a dim rule
//	row 3        blank
//	...          the body — the place's own rows
//	...          blank, then a dim rule
//	...          the composer, with the scope chip at the right of its box row
//	...          the strip — the row's verbs, or an answer that can be given here
//	...          the hint line
//
// THE FOOT IS MEASURED BEFORE THE BODY IS GIVEN ITS ROOM. A draft that wraps to
// a second and third row takes those rows FROM the body, never from the frame —
// home learned that the hard way and the whole surface inherits the lesson here
// ([app.homeFrame]'s own note has the story).
//
// THE FRAME IS EXACTLY THE WHOLE TERMINAL, at every width, with no borders and
// no viewport. A frame too short for its own contents keeps row 0 and the last
// `height-1` rows, AND THE CARET RIDES THAT CLAMP: coordinates computed before
// the cut would leave the terminal's cursor standing a row below the box.
func placeFrame[H any](a *app, width, height int, blank H, body func(width, room int) []placeRow[H]) ([]string, []H, int, int) {
	return placeFrameWithBar(a, width, height, blank, body, nil)
}

// placeFrameWithBar is [placeFrame] with the hint line replaced by a BAR a thumb
// can press.
//
// At [tierPhone] a line naming four keys is a line naming four keys nobody has,
// and the way out has to be a target rather than a legend — which is the rule
// the task page and home's own phone sheet both already followed with their own
// feet. The bar carries the place's own hit so the press resolves against the
// row that was actually drawn, exactly as every other row on the frame does.
func placeFrameWithBar[H any](a *app, width, height int, blank H,
	body func(width, room int) []placeRow[H], bar func(width int) (string, H, bool)) ([]string, []H, int, int) {
	// THE PLACE LADDER IS IN FORCE FOR THE WHOLE OF THIS FRAME, and it is put back
	// before this function returns (styles.go's [palette.onPlaces]). Every row
	// below — the pulse, the tab bar, the body the place itself builds, the
	// composer and the foot — asks `a.pal` for its colours, so re-pointing three
	// roles here is what makes THE ONE-ACCENT LAW reach two thousand call sites
	// without one of them being edited. The inks are the conversation's own; what
	// a place does not do is draw the question's violet or the tick's olive.
	was := a.pal
	a.pal = was.onPlaces()
	defer func() { a.pal = was }()
	pal := a.pal
	lines := make([]string, 0, height)
	hits := make([]H, 0, height)
	add := func(text string, hit H) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	add(a.pulseLine(width, pal), blank)
	add(a.placeTabBar(width, a.mapShowing, pal), blank)
	add(pal.dim(rule(width)), blank)
	add("", blank)

	box := a.placeBox()
	var draftRows []string
	var draftCX, draftCY int
	if box != nil && !box.empty() {
		draftRows, draftCX, draftCY = draftBlock(box, pal, width-2, homeDraftRows, "", "")
	}
	draftHeight := len(draftRows)
	if draftHeight < 1 {
		draftHeight = 1
	}
	strip := a.placeStrip(width)
	note := a.placeNote(width)
	foot := 2 + len(note) + draftHeight + len(strip)
	room := height - len(lines) - foot - spacingRuleClearance
	if room < 1 {
		room = 1
	}

	for _, row := range body(width, room) {
		add(row.text, row.hit)
	}
	add("", blank)
	add(pal.dim(rule(width)), blank)
	// A PLACE MAY SAY ONE LINE ABOUT WHAT IT IS HOLDING, and it says it here:
	// under the rule and above the composer, where every place's own count,
	// filter line or open editor's label goes. It is the router's one concession
	// to the places having bodies that are not all lists — and it is a LINE, not
	// a foot: a place that wanted three rows here would be a place drawing a
	// second frame inside this one.
	for _, row := range note {
		add(row, blank)
	}

	caretX, caretY := 0, 0
	// THE BOX ROW CARRIES THE SCOPE CHIP AT ITS RIGHT EDGE, and it carries it at
	// rest too. `alt+enter` sends what is typed off as a task from any place, and
	// a verb that is always in reach has to always say where it will land —
	// otherwise "start a task from anywhere" is "start a task somewhere".
	chip := a.scopeChip()
	if len(draftRows) == 0 {
		add(a.placeChipped(" "+pal.dim(fit(a.placeRestWord(), width-2)), chip, width, pal), blank)
		// AT REST THERE IS NOTHING TO TYPE INTO, so the caret is hidden rather
		// than left blinking at the frame's origin. The moment a character lands
		// the box stops being empty and the caret comes back, in the box.
		a.caret = false
	} else {
		for i, row := range draftRows {
			if i == 0 {
				add(a.placeChipped(" "+row, chip, width, pal), blank)
				continue
			}
			add(" "+row, blank)
		}
		caretX, caretY = 1+draftCX, len(lines)-len(draftRows)+draftCY
	}
	if caretX > width-1 {
		caretX = width - 1
	}
	for _, row := range strip {
		add(row, blank)
	}
	switch line, hit, ok := "", blank, false; {
	case bar != nil:
		if line, hit, ok = bar(width); ok {
			add(line, hit)
			break
		}
		fallthrough
	default:
		if msg, ok := a.placeMsgLine(width); ok {
			add(msg, blank)
		} else {
			add(" "+paintHint(fit(a.placeHint(), width-2), pal, pal.dim), blank)
		}
	}

	if len(lines) > height {
		removed := len(lines) - height
		keep, keepHits := lines[:1], hits[:1]
		lines = append(keep, lines[len(lines)-(height-1):]...)
		hits = append(keepHits, hits[len(hits)-(height-1):]...)
		switch {
		case caretY >= 1+removed:
			caretY -= removed
		case caretY > 0:
			a.caret = false
		}
	}
	for len(lines) < height {
		add("", blank)
	}
	// AND NO GROUND GOES ON AT ALL. A place paints the rows it built and nothing
	// under them: the terminal's own background shows through every cell this
	// frame owns, exactly as it does behind a conversation (styles.go's THE GROUND
	// LADDER, and the reversal note under it). The only lifted cells on the whole
	// frame are the ones a person put a pointer or a cursor on.
	return lines, hits, caretX, caretY
}

// placeChipped puts the scope chip against the right edge of the box row, and
// drops it rather than crowding the sentence when there is no room for both.
func (a *app) placeChipped(row, chip string, width int, pal palette) string {
	if chip == "" {
		return row
	}
	painted := pal.dim(chip)
	gap := width - ansi.StringWidth(row) - ansi.StringWidth(chip) - 1
	if gap < 1 {
		return row
	}
	return row + strings.Repeat(" ", gap) + painted
}

// scopeChip is the right of the box row: WHERE what you type will land.
//
// It is derived and never stored, from the three answers that already exist, in
// this order: the project the cursor is standing on (home's own [homeWhere],
// which is what `ctrl+t` already asks when it decides where a fresh conversation
// goes), then this window's own workspace ([app.placePath], the same answer the
// phone's status sheet prints as its `place` row). A person who typed a path
// outranks both, and that is the composer's business rather than the chip's.
func (a *app) scopeChip() string {
	if a.page == pageHome && a.home.open {
		if line, ok := a.home.previewLine(); ok {
			if where := homeWhere(line); where != "" {
				return placeScopeWord + " " + where
			}
		}
	}
	if at := a.placePath(0); at != "" {
		return placeScopeWord + " " + at
	}
	return ""
}

// The sentences the router says. Each is quoted in the manual exactly as it is
// spelled here.
const (
	// placeScopeWord leads the scope chip. One word, because the chip's whole job
	// is the path beside it.
	placeScopeWord = "here"
	// placeRestWord is what the box row says on a place with nothing typed into
	// it — home included, exactly as SCREEN 2b draws it. What home's box ALSO
	// does is filter, and that is said on the foot rather than in the box, where
	// the design puts it ([homeRestHint]).
	placeRestWord = "say what you want done"
	// placeHintWords is the second line of the composer, AND IT IS THE DESIGN'S
	// OWN SENTENCE WORD FOR WORD (SCREEN 2b, and FIDELITY.md item 3 quotes it as
	// the composer's foot). Four clauses: what enter does, what the chord does,
	// how the map appears, and the way to the next place.
	//
	// TWO THINGS ABOUT IT WERE DRIFT AND ARE NOW FIXED. It said `alt+. map`,
	// which is a key and a noun rather than a key and what it does — every other
	// clause on this line is a verb phrase — and it carried a fifth clause,
	// `esc close`, that the design does not draw here. `esc` still closes: SCREEN
	// 3a puts it in the first of the six key classes, beside ↑↓, enter and tab,
	// as a key that is true on every screen and therefore does not have to be
	// re-advertised on each one.
	placeHintWords = "enter talk about it · alt+enter send it off as a task · alt+. for the map · tab next place"
	// placeHintTail is what every other place's hint ends with, appended rather
	// than written into each sentence so that a hint and the router can never
	// disagree about which keys exist.
	//
	// IT IS THE ONE CLAUSE THE DESIGN PUTS ON EVERY PLACE'S FOOT. Screens 1e, 1f,
	// 2c, 2d and 2f each end their own sentence with `tab next place` and with
	// nothing after it; `alt+. for the map` belongs to the composer's line above,
	// where FIDELITY.md item 3 puts it, and a tail that repeated it would put the
	// same chord on two lines of the same frame.
	placeHintTail = "tab next place"
	// placeMapWords is the hint line while the map is drawn (SCREEN 3b): the
	// chord list, in the cells the hint was already in.
	placeMapWords = "alt+1…7 go to a place · alt+enter send it off as a task · → verbs on this row · esc close"
)

// placeRestWord is what this place's box row says with nothing typed in it, and
// it is the same sentence on every place — home included (SCREEN 2b).
func (a *app) placeRestWord() string {
	return "› " + placeRestWord
}

// placeHint is the line under the composer. Home writes its own sentence for
// every row it can stand on ([app.homeHint]) and gains the router's tail; every
// other place says the router's own line.
func (a *app) placeHint() string {
	if a.mapShowing {
		return placeMapWords
	}
	if a.strip.open {
		return stripHint
	}
	// EVERY PLACE'S OWN SENTENCE, WITH THE ROUTER'S KEYS ON THE END OF IT. The
	// places that had a keys line of their own keep it — it is about the row a
	// person is standing on, which is knowledge this file does not have — and the
	// two keys that are true everywhere are appended rather than written into
	// seven sentences.
	switch a.page {
	case pageHome:
		// HOME OWNS ITS WHOLE LINE, the router's own keys included. At rest that
		// line is the design's sentence word for word and names four keys exactly
		// (SCREEN 1a, home.go's [app.homeHint]); a tail appended here would make it
		// five.
		return a.homeHint()
	case pageTasks:
		if a.taskSheet.open {
			return placeTailed(a.taskSheetKeysLine())
		}
	case pageStanding:
		if a.standPage.open {
			return placeTailed(standPageVerbs)
		}
	case pageMemory:
		if a.memPanel.open {
			if a.memPanel.edit != nil {
				return placeTailed(memoryEditHint)
			}
			return placeTailed(memoryFilterHint)
		}
	case pageSettings:
		if a.sheet.open {
			return placeTailed(a.sheet.keysLine())
		}
	}
	return placeHintWords
}

// placeTailed puts the router's own keys on a place's sentence, and puts them
// BEFORE THE WAY OUT: every hint on this surface ends with `esc`, because the
// way out is the last thing a person needs to be told and the first thing they
// look for (homebridge.go says the same about its own clause).
func placeTailed(hint string) string {
	if strings.Contains(hint, placeHintTail) {
		return hint
	}
	if at := strings.LastIndex(hint, " · esc"); at >= 0 {
		return hint[:at] + " · " + placeHintTail + hint[at:]
	}
	return hint + " · " + placeHintTail
}

// placeMsgLine is the one refusal line this place has to say, drawn instead of
// the hint. It replaces rather than stacks, being one field: pressing a door
// twice says the same thing once.
//
// DIM, AND NOT THE FAULT COLOUR. Every refusal these places have is a fact about
// a door — that conversation is open somewhere, that project is not this one —
// and none of them is anybody's mistake. AND THE PLACE IT SENDS YOU IS A DOOR:
// the sentence that names a directory opens it, applied to the FITTED text after
// the width was measured, and a directory that is not there stays plain
// (pathlink.go).
func (a *app) placeMsgLine(width int) (string, bool) {
	msg, path := a.pageMsg, ""
	if a.page == pageHome {
		msg, path = a.home.msg, a.home.msgPath
	}
	if msg == "" {
		return "", false
	}
	return " " + a.pal.dim(a.pathLink(path, fit(msg, width-2))), true
}

// ── opening a place ─────────────────────────────────────────────────────────

// showPage opens one place and closes whatever was standing where it is about to
// stand.
//
// IT IS A WRAPPER OVER THE EXCLUSION THAT ALREADY EXISTED, not a replacement for
// it. [app.standDownFullscreen] still closes every page that takes the frame,
// every page still carries its own `open bool`, and view.go's frame still asks
// those booleans in the same order — so `a.page` is a LABEL on state the
// booleans already carry, and the two page-stack laws in chrome_test.go hold
// without an edit to what they assert.
//
// A place that refuses to open leaves `a.page` where it was. The task page
// refuses when there is nothing to show, the memory place refuses when memory is
// off, and a router that moved the tab bar's band onto a place that did not open
// would be a bar pointing at an empty room.
func (a *app) showPage(id page) tea.Cmd {
	// WHAT IS STANDING IS ASKED BEFORE ANYTHING IS CLOSED, because the answer is
	// what a refusal below has to put back.
	was, standing := a.page, a.pageShowing()
	// LEAVING A PLACE IS THE LOOK. The stamp the next count is measured from is
	// written HERE and where a place is closed by `esc`, and never on the way in:
	// a stamp taken on arrival would declare everything seen the instant it
	// appeared (placecounts.go's [app.leavePage] and session's look.go both hold
	// the argument). It is written before the stand-down so that it is the place
	// that was actually being looked at which gets stamped, and it is one call for
	// EVERY counted place rather than a list of them here — a list would be a
	// second answer to which places can wear a number.
	if standing && was != id {
		a.leavePage(was)
	}
	a.standDownFullscreen()
	a.closeStrip()
	a.mapShowing = false
	a.pageMsg = ""
	a.page = id
	cmd, opened := a.openPage(id)
	if opened {
		return cmd
	}
	// A REFUSAL PUTS BACK WHAT WAS STANDING. The stand-down above closed it, so
	// a router that merely moved the band back would leave the person looking at
	// the conversation they were not in — which reads as the key having thrown
	// them out rather than as the place having nothing to show. The refusal's own
	// sentence is already in the transcript; this is only about which screen they
	// are left on.
	a.page = was
	if !standing {
		return cmd
	}
	back, _ := a.openPage(was)
	return tea.Batch(cmd, back)
}

// pageShowing is whether the place the router is pointing at is actually up. It
// asks the pages' own `open` flags rather than [app.page], because that field is
// a label on them and a label is not evidence.
func (a *app) pageShowing() bool {
	switch a.page {
	case pageHome:
		return a.home.open
	case pageTasks:
		return a.taskSheet.open
	case pageStanding:
		return a.standPage.open
	case pageMemory:
		return a.memPanel.open
	case pageSpend, pageSearch:
		return a.teach.open && a.teach.at == a.page
	case pageSettings:
		return a.sheet.open
	}
	return false
}

// openPage opens the one place's own state, and says whether it took.
func (a *app) openPage(id page) (tea.Cmd, bool) {
	switch id {
	case pageHome:
		return a.openHome(), a.home.open
	case pageTasks:
		return a.openTaskPage(), a.taskSheet.open
	case pageStanding:
		a.openStanding()
		return nil, a.standPage.open
	case pageMemory:
		return a.openMemory(), a.memPanel.open
	case pageSpend:
		a.teach.open, a.teach.at = true, pageSpend
		return a.openSpend(), true
	case pageSearch:
		a.teach.open, a.teach.at = true, pageSearch
		return a.openSearch(), true
	case pageSettings:
		a.openSettings()
		return nil, a.sheet.open
	}
	return nil, false
}

// nextPage is `tab`: the place after this one, and round again from the last.
// `back` is `shift+tab`, the same circle walked the other way.
func nextPage(at page, back bool) page {
	all := pages()
	for i, id := range all {
		if id != at {
			continue
		}
		if back {
			return all[(i+len(all)-1)%len(all)]
		}
		return all[(i+1)%len(all)]
	}
	return pageHome
}
