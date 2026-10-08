package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FACTORY PLACE ───────────────────────────────────────────────────────
//
// The handle the registry files (pages.go's [place]) for the page
// factory_page.go describes: the factory floor, reached by `/factory` and the
// ninth digit. It folds into `more` on a narrow bar, which is where a place a
// person visits on purpose rather than every minute belongs.
//
// placeFactory holds no state of its own; the page's state is `a.fp`.
type placeFactory struct{ placeBase }

func init() { registerPlace(placeFactory{}) }

func (placeFactory) id() page     { return pageFactory }
func (placeFactory) word() string { return "factory" }

// open starts the first read of the floor and arms the place's beat. THE BEAT
// IS ARMED WITH NOTHING CONNECTED TOO, as on every place but home: the read it
// asks for is nil then, and a seam wired later is read on the next beat.
//
// A words box left open by walking away is shut on the way back in, with its
// words kept: the narrowed rail is still what the person left, and the next
// letter they press is a key again rather than a character in a box they
// cannot remember opening. A verb's typing row is shut the same way, and its
// words go with it, because they were about an item that may have moved on.
func (placeFactory) open(a *app) tea.Cmd {
	a.fp.typing = false
	a.fp.hover = -1
	a.fp.act.ask = nil
	// THE FLOOR'S SETTINGS ARE SHUT ON THE WAY BACK IN, for the typing row's
	// reason: they were about a moment that has passed, and the picker's
	// ticks were never saved.
	a.fp.pick, a.fp.recipe, a.fp.ghOffer, a.fp.keys = nil, nil, "", false
	return tea.Batch(a.armPlaceClock(), a.factoryRead())
}

// tick re-reads the floor on the three-second beat, off the loop, so a stream
// that moved is on this frame within a beat.
func (placeFactory) tick(a *app, now time.Time) (bool, tea.Cmd) {
	return true, a.factoryRead()
}

func (placeFactory) body(a *app, width, room int) []placeRow { return a.factoryBody(width, room) }

// stops is the rail line of every item, top first.
func (placeFactory) stops(a *app) []int { return a.factoryLines() }

// cursorAt is the rail line the cursor's item stands on, and 0 on a floor with
// no items, which is the first line the frame drew.
func (placeFactory) cursorAt(a *app) int {
	lines := a.factoryLines()
	if a.fp.cursor >= 0 && a.fp.cursor < len(lines) {
		return lines[a.fp.cursor]
	}
	return 0
}

// rowID names the item under the cursor by its id, which a re-read does not
// change.
func (placeFactory) rowID(a *app) string {
	if it, ok := a.factoryCursorItem(); ok {
		return "factory/" + itoa(it.ID)
	}
	return ""
}

// note is the one line the place says when the last read failed, the floor
// drawn above it being the one read before, and while a door a key asked is
// out. A failed read is the louder of the two and wins the line. (`enter` on a
// stage with no room says why on the pane's own action line, not here.)
func (placeFactory) note(a *app, width int) []string {
	switch {
	case a.fp.err != nil:
		return []string{" " + a.pal.dim(noteFit("the factory could not be read · "+a.fp.err.Error(), width-factoryHintInset))}
	case a.factoryDoingNote() != "":
		// A DOOR A KEY ASKED IS STILL OUT (factory_busy.go): the spinner and
		// what it is doing, until it answers.
		return []string{" " + fit(a.factoryDoingNote(), width-factoryHintInset)}
	case a.fp.recipe != nil && a.factoryRecipeNoDir() != "":
		return []string{" " + a.pal.dim(noteFit(a.factoryRecipeNoDir(), width-factoryHintInset))}
	}
	return nil
}

func (placeFactory) about() string { return "the work in flight, by where it stands" }

// hint names the keys the page has. With the words box or a typing row open
// the keys are the box's; with the `?` sheet up, the way to close it.
//
// THE FLOOR'S BOTTOM BAR IS NAVIGATION ONLY (owner decision, 2026-10-08):
// `n new · R repos · m foreman · / filter · tab next place · esc back · ? keys`.
// The row's own verbs are on the peek's strip beside it ([app.factoryVerbRail])
// and every key is on the `?` sheet ([app.factorySheet]), so this line never
// repeats either. A clause whose door is absent is not on it, and WHEN THE
// LINE IS TOO LONG IT DROPS FROM THE RIGHT of the place's own clauses, keeping
// the way out and `?`, which a lost person needs most. `esc` says clear while
// anything narrows the rail, because that is what the first press does. WHILE
// THE ITEM PAGE IS OPEN THE LINE IS THE PAGE'S ([app.factoryItemHint]).
func (placeFactory) hint(a *app) string {
	if a.fp.keys {
		return factoryHintClause(keyBack, wordClose)
	}
	if a.fp.pick != nil {
		return a.factoryPickerHint()
	}
	if a.fp.typing {
		return "type to filter · enter keep · esc clear"
	}
	if ask := a.fp.act.ask; ask != nil {
		return factoryAskHint(ask)
	}
	if a.fp.act.refresh != nil || a.fp.act.launch != nil {
		return "y go · n not now"
	}
	if a.fp.ghOffer != "" {
		return "y use gh · n a token instead · esc not now"
	}
	if a.fp.recipe != nil {
		return a.factoryRecipeHint()
	}
	var head []string
	if a.fp.act.habit != "" {
		head = append(head, "y bank it", "n not yet")
	}
	if a.fp.open {
		if it, ok := a.factoryCursorItem(); ok {
			return a.factoryItemHint(it, head)
		}
	}
	return a.factoryFloorHint(head)
}

// factoryFloorHint is the floor's navigation line under head (a habit
// offer's keys, when one is drawn).
func (a *app) factoryFloorHint(head []string) string {
	nav := a.factoryNavClauses()
	out := factoryHintClause(keyBack, wordBack)
	if a.factoryNarrowed() {
		out = factoryHintClause(keyBack, wordClear)
	}
	tail := []string{out}
	if a.factoryConnected() {
		tail = append(tail, factoryHintClause(keySheet, wordSheet))
	}
	line := func() string {
		parts := append(append(append([]string{}, head...), nav...), tail...)
		return strings.Join(parts, " · ")
	}
	for len(nav) > 0 && a.width > 0 && ansi.StringWidth(placeTailed(line())) > a.width-factoryHintInset {
		nav = nav[:len(nav)-1]
	}
	return line()
}

// factoryNavClauses is the floor's navigation, in its one order, each only
// where its door is: new work (not while the row under the cursor needs you,
// where `n` answers no), the repositories, the foreman and the filter.
func (a *app) factoryNavClauses() []string {
	var nav []string
	connected := a.factoryConnected()
	it, ok := a.factoryCursorItem()
	if connected && a.factory.Has("new") && (!ok || it.State != factory.StateNeedsYou) {
		nav = append(nav, factoryHintClause(keyNew, wordNew))
	}
	if connected && a.factory.Has("repos") && a.factory.Has("setrepos") {
		nav = append(nav, factoryHintClause(keyRepos, wordRepos))
	}
	if connected {
		nav = append(nav, a.factoryForemanHint()...)
	}
	if a.factoryFloorHas() {
		nav = append(nav, factoryHintClause(keyFilter, wordFilter))
	}
	return nav
}

// factoryItemHint is the hint line while the item page is open, and it is
// FOUR CLAUSES AT MOST (owner decision, 2026-10-08): `↑↓ rows · enter <what
// enter does on this row> · esc floor · ? keys`, after the habit offer's keys
// when one is drawn. The item's verbs are on the column at the right and the
// pane's own action line ([app.factoryPageAction]) and every key is on the
// `?` sheet. `enter` is named only where it acts: on the issue and the
// manager row it opens the item's chat (or its page on github where there is
// no chat door), on a stage with a room it walks into the conversation, and
// on a landed item's proof stage it approves or requests changes. A key that
// opens nothing is not a verb.
func (a *app) factoryItemHint(it factory.Item, head []string) string {
	parts := append([]string{}, head...)
	parts = append(parts, factoryHintClause(keyWalk, wordRows))
	if w := a.factoryItemEnterWord(it); w != "" {
		parts = append(parts, factoryHintClause(keyOpen, w))
	}
	parts = append(parts, factoryHintClause(keyBack, wordFloorName), factoryHintClause(keySheet, wordSheet))
	return strings.Join(parts, " · ")
}

// factoryItemEnterWord is what `enter` does on the item page's row, as the
// bottom line names it, and "" where it does nothing.
func (a *app) factoryItemEnterWord(it factory.Item) string {
	if r, ok := a.factoryPageRowAt(it); ok && a.fp.open && r.kind == factoryPageManager && a.factory.Has("talk") {
		return wordTalk
	}
	if a.factoryOnIssueRow(it) {
		switch a.factoryIssueEnter(it) {
		case factoryIssueTalk:
			return wordChat
		case factoryIssueForge:
			return wordOpenGitHub
		}
		return ""
	}
	if _, room := a.factoryRoomRow(it); room {
		return wordConversation
	}
	if a.factoryOnProof(it) {
		switch {
		case factoryFirstFailed(it) != "" && a.factory.Has("sendback"):
			return wordRequestChanges
		case factoryFirstFailed(it) == "" && a.factory.Has("signoff"):
			return wordApprove
		}
	}
	return ""
}

// factoryOnIssueRow says whether the item page's left column stands on the
// issue or the manager row, the rows whose `enter` opens the item's own
// conversation.
func (a *app) factoryOnIssueRow(it factory.Item) bool {
	if !a.fp.open {
		return false
	}
	r, ok := a.factoryPageRowAt(it)
	return ok && (r.kind == factoryPageIssue || r.kind == factoryPageManager)
}

// press is a press on a row: the cursor lands on the item drawn there, and
// the peek beside the rows shows it. THE FLOOR SELECTS ON ONE PRESS AND OPENS
// ON TWO (owner ruling, 2026-10-08), and the second press, the divider and the
// item page's rail are read with the column before this is asked
// (factory_split.go's [app.factoryPointer]). A press on a row that holds no
// item, or on the item page itself, does nothing.
func (placeFactory) press(a *app, y int) (tea.Cmd, bool) {
	// A PRESS ON THE `?` SHEET PUTS IT AWAY, as a press off a crew panel
	// closes it: the sheet is read, never pressed through.
	if a.fp.keys {
		a.fp.keys = false
		a.touch()
		return nil, true
	}
	a.factoryPress(y)
	return nil, true
}

// wheel walks the cursor, so the wheel over the floor never reaches the
// conversation behind it.
func (placeFactory) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.factoryMove(delta)
	return nil, true
}

// owns is the words box, which has the whole keyboard while it is open, as
// every box inside a place does; `space` on a new item, which marks it; and
// `space` on a running item, which pauses it or resumes it.
//
// SPACE IS CLAIMED HERE AND NOT IN key BECAUSE THE ROUTER READS A BARE SPACE
// AS HALF OF THE DOOR HOME before a place's own keys are asked
// ([app.placeHomeGesture]). It is claimed only on a new item and on a running
// one with a pause door, so everywhere else on the floor two spaces still go
// home.
func (placeFactory) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// THE `?` SHEET HAS THE KEYBOARD WHILE IT STANDS, and `?` opens it from
	// the floor and the item page alike (factory_keysheet.go).
	if cmd, took := a.factorySheetKey(msg); took {
		return cmd, true
	}
	// THE PICKER, THE GH OFFER AND THE RECIPE PAGE HAVE THE KEYBOARD while one
	// stands, before the layout reads `enter` as opening an item underneath
	// (factory_settings.go).
	if cmd, took := a.factorySettingsOwns(msg); took {
		return cmd, true
	}
	// THE LAYOUT'S KEYS ARE READ FIRST, here rather than in key, because the
	// item page walks its stages with `↑` and the router would otherwise read
	// `↑` off the first row as the way onto the tab bar (factory_item.go).
	if cmd, took := a.factoryLayoutKey(msg); took {
		return cmd, true
	}
	// The box keeps every key but the router's walk between places and its alt
	// chords, which the hint line goes on naming while the box is open.
	if a.fp.typing {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryFilterKey(msg), true
	}
	// A VERB'S TYPING ROW HAS THE KEYBOARD ON THE SAME TERMS, and a habit
	// offer has `y` and `n` (factory_keys.go's [app.factoryOwns]).
	if cmd, took := a.factoryOwns(msg); took {
		return cmd, true
	}
	if msg.String() == "space" {
		if a.factoryMark() {
			return nil, true
		}
		if cmd, took := a.factoryPauseKey(); took {
			return cmd, true
		}
	}
	return nil, false
}

// key is the item's verbs, then the cursor, the rail's narrowings and the way
// out; the router's classes are read first. The verbs are asked first because
// none of them shares a key with the rail (factory_keys.go's [app.factoryKey]
// answers false for every key it does not take). `esc` CLEARS A NARROWED RAIL
// BEFORE IT LEAVES, so a person who filtered does not lose the page to the
// same key that drops the filter.
//
// WHILE THE ITEM PAGE IS OPEN only the verbs are asked: the rail's narrowings
// are about the floor, which is underneath.
func (placeFactory) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, took := a.factoryKey(msg); took {
		return cmd
	}
	if a.fp.open {
		return nil
	}
	switch k := msg.String(); k {
	case "esc":
		if a.factoryNarrowed() {
			a.factoryClear()
			return nil
		}
		a.leavePlace()
	case "up", "ctrl+p":
		a.factoryMove(-1)
	case "down", "ctrl+n":
		a.factoryMove(1)
	case "/":
		if a.factoryFloorHas() {
			a.factoryOpenFilter()
		}
	case "[":
		a.factoryCycleRepo(-1)
	case "]":
		a.factoryCycleRepo(1)
	case "A", "shift+a":
		a.factoryToggleBacklog()
	}
	return nil
}
