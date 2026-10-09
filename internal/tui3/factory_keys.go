package tui3

import (
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FACTORY'S VERBS ─────────────────────────────────────────────────────
//
// The keys that change the floor: run, stop, pause, answer, steer,
// approve, request changes, the budget, the thinking and the stages
// (their words are factory_words.go's). place_factory.go routes a key here
// once the rail has had its own keys; this file reads the item under the
// cursor, picks the door its state allows, and asks it.
//
// EVERY VERB HAS ONE SHAPE. The door is checked first, and a nil door is a key
// that does nothing and is named nowhere on the hint line (a capability that
// cannot work is absent, not broken). Then the door is asked OFF THE LOOP, the
// floor is read with Load in the same ask, and the snapshot is folded in with
// [app.factoryFold] so the cursor stays on the item it was on, by id. A door
// that refuses says its own sentence on the note line beside the hint; the
// page never crashes on a refusal and never reads the disk in a frame.
//
// A VERB IS A GESTURE, SO IT STANDS IN THE ORDERED LINE ([app.offLoop]). The
// read nobody pressed for (the three-second re-read) is asked beside it ([app.besideLine]); a launch and the steer typed after it are
// seen by the floor in the order they were made.

// `ENTER` IS NOT A VERB HERE: on a floor row it opens the item page
// (factory_item.go), and on that page it walks into a stage's room, except on a
// landed item's proof, which calls [app.factoryLandedKey] with it.
//
// EVERY VERB SAYS WHAT BECAME OF THE ITEM on the note line once its door has
// answered (`#12 is running`, `#12 stopped · branch kept`), read off the floor
// the same ask folded in ([app.factoryVerb]); a door that refuses says its own
// sentence there instead, verbatim.

// factoryHabitSentence is the habit the offer banks after three clean
// sign-offs. It is spelled once so the offer, the door and the tests agree.
const factoryHabitSentence = "factory PRs from your own issues self-ship when the proof is green"

// factoryCaps is the ladder `c` climbs, and it wraps to its first rung.
var factoryCaps = []float64{2, 5, 8, 15, 30}

// factoryEfforts is the order `e` cycles a stage's effort in. The empty word is
// the knee: the crew picks the effort it would for this class of work.
var factoryEfforts = []string{"", "cheap", "strong"}

// factoryAskKind is what a typing row submits to.
type factoryAskKind int

const (
	factoryAskStage    factoryAskKind = iota // AddStage
	factoryAskWords                          // the gate, cap and effort in words on a new item
	factoryAskSteer                          // Steer on a stream
	factoryAskAnswer                         // Answer with words
	factoryAskSendBack                       // SendBack
	factoryAskNew                            // New on a repo
	factoryAskManager                        // the item's manager, from the timeline's box
)

// factoryAsk is the one typing row at the bottom of the pane: what it is for,
// the item (or repo) it is about, its label, a dim example, and the words.
type factoryAsk struct {
	kind    factoryAskKind
	id      int
	repo    string
	label   string
	example string
	text    string
}

// factoryActs is the verbs' own state on the page, held on `a.fp.act`.
//
// THE PAGE'S OTHER FIELDS ARE THE RAIL'S AND THE READ'S; these are what only
// a verb leaves behind, such as an open typing row or a habit offer waiting
// for `y` or `n`.
type factoryActs struct {
	// ask is the open typing row, and nil when none is open.
	ask *factoryAsk
	// habit is the repo a habit offer is about, and "" when none is drawn.
	habit string
	// recipeOffer is the repo the first offer of a recipe file is about, ""
	// when none stands, and recipeAsked the repos this window has asked the
	// seam about already (factory_recipeoffer.go).
	recipeOffer string
	recipeAsked map[string]bool
	// talk is the conversation `T` last opened from the floor, by its key
	// ([app.convKey]): while it is the one in front, `esc` on its empty box
	// with nothing running goes back to the floor ([app.factoryTalkBack]).
	// "" when `T` has opened nothing, or the way back was taken.
	talk string
	// doing is what a door a key asked is doing while it is out, in the
	// words the note line says after the spinner (`banking…`), and "" when
	// none is (factory_busy.go).
	doing string
	// refresh is `U`'s question while it stands, nil when none does.
	refresh *factoryRefreshAsk
	// launch is `L`'s question while marks stand, nil when none does
	// (factory_run.go).
	launch *factoryLaunchAsk
	// launched is a launch's note the fold still owes, nil when none is
	// ([app.factoryLaunchNote]).
	launched *factoryLaunchNote
	// clone is the question a launch asks when its repository has no
	// checkout here, nil when none stands (factory_clone.go).
	clone *factoryCloneAsk
}

// factoryLaunchNote is a launch whose note waits on the floor's next read:
// the item, and what the note says after its state.
type factoryLaunchNote struct {
	id   int
	tail string
}

// ── asking a door ───────────────────────────────────────────────────────────

// factoryDo asks one gesture's doors off the loop, in the order pressed, then
// reads the floor in the same ask and folds it in. `then` runs on the loop
// after the fold with what the doors said, for the verbs that move the cursor
// or open an offer. A refusal's own sentence goes on the note line.
func (a *app) factoryDo(act func(s factory.Seam) error, then func(err error)) tea.Cmd {
	return a.factoryDoThen(act, func(err error) tea.Cmd {
		if then != nil {
			then(err)
		}
		return nil
	})
}

// factoryDoThen is [app.factoryDo] whose `then` hands the loop a command of
// its own, for the save that arms a re-read (factory_settings.go).
func (a *app) factoryDoThen(act func(s factory.Seam) error, then func(err error) tea.Cmd) tea.Cmd {
	seam := a.factory
	return a.offLoop(func() func(bool) tea.Cmd {
		err := act(seam)
		var snap factory.Snapshot
		var lerr error
		if seam.Load != nil {
			snap, lerr = seam.Load()
		}
		return func(bool) tea.Cmd {
			// THE DOOR HAS ANSWERED, so whatever the note line said it was
			// doing is over (factory_busy.go).
			a.fp.act.doing = ""
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case seam.Load != nil:
				a.factoryFold(snap)
			}
			if err != nil {
				a.pageMsg = strings.TrimSpace(err.Error())
			}
			a.touch()
			return then(err)
		}
	})
}

// factoryVerb asks one verb's doors like [app.factoryDo] and, when they
// answered without a refusal, says on the note line what became of the item:
// said reads the item as the fold left it, and "" says nothing.
func (a *app) factoryVerb(id int, act func(s factory.Seam) error, said func(it factory.Item) string) tea.Cmd {
	return a.factoryDo(act, func(err error) {
		if err != nil || said == nil {
			return
		}
		if it, ok := a.factoryItemByID(id); ok {
			if w := said(it); w != "" {
				a.factorySay(w)
			}
		}
	})
}

// factoryLaunchedWords is what the note line says after a launch, from where
// the item stands once the floor was read again: queued behind full benches,
// parked on a question already, or running.
func factoryLaunchedWords(it factory.Item) string {
	switch it.State {
	case factory.StateQueued:
		return it.Ref() + " is queued" + rowSep + "a bench frees it"
	case factory.StateNeedsYou:
		return it.Ref() + " is waiting on you"
	}
	return it.Ref() + " is running"
}

// factoryLaunchNote is what the note line says the moment a launch's door has
// answered: the item's state words when the floor already reads it past the
// queue, and nothing yet when it reads it queued, which the next fold resolves
// ([app.factoryFoldLaunchNote]).
//
// A LAUNCH IS QUEUED FOR AN INSTANT EVEN ON A FREE BENCH. The door writes
// queued and the bench takes the item a moment later, so the read the door's
// own ask makes sees queued; on the 2026-10-08 hand run `r` said `#1 is queued
// · a bench frees it` while #1 started that second. The note waits for the
// floor's next read, and says queued only when the item is still there.
func (a *app) factoryLaunchNote(it factory.Item, tail string) string {
	if it.State == factory.StateQueued {
		a.fp.act.launched = &factoryLaunchNote{id: it.ID, tail: tail}
		return ""
	}
	a.fp.act.launched = nil
	return factoryLaunchedWords(it) + tail
}

// factoryFoldLaunchNote says the note a launch left owing, on the first fold
// after it: running, waiting on you, or queued behind full benches. A note
// somebody put on the line since is not written over.
func (a *app) factoryFoldLaunchNote() {
	n := a.fp.act.launched
	if n == nil {
		return
	}
	a.fp.act.launched = nil
	if a.pageMsg != "" {
		return
	}
	if it, ok := a.factoryItemByID(n.id); ok {
		a.factorySay(factoryLaunchedWords(it) + n.tail)
	}
}

// factorySteerable says whether `S` steers the item: a stream to hand words
// to, on a bench, waiting for one or parked on a question, and a door.
func (a *app) factorySteerable(it factory.Item) bool {
	if !a.factory.Has("steer") || it.Stream == nil {
		return false
	}
	switch it.State {
	case factory.StateQueued, factory.StateRunning, factory.StateNeedsYou:
		return true
	}
	return false
}

// factoryPauseKey is `space` on a running item: it pauses, and on a paused
// one resumes, through the one Pause door. It answers false on any other
// item, where `space` goes on meaning what it meant.
func (a *app) factoryPauseKey() (tea.Cmd, bool) {
	it, ok := a.factoryCursorItem()
	if !ok || it.State != factory.StateRunning || !a.factory.Has("pause") {
		return nil, false
	}
	a.pageMsg = ""
	a.fp.said = false
	paused := it.Stream != nil && it.Stream.Paused
	return a.factoryVerb(it.ID, func(s factory.Seam) error { return s.Pause(it.ID) }, func(it factory.Item) string {
		if paused {
			return it.Ref() + " resumed"
		}
		return it.Ref() + " paused"
	}), true
}

// factorySay puts a sentence on the note line beside the hint.
func (a *app) factorySay(words string) {
	a.pageMsg = words
	a.touch()
}

// factoryFocus puts the cursor on the item with id when the rail draws it.
func (a *app) factoryFocus(id int) {
	for at, i := range a.factoryWalkNow() {
		if a.fp.snap.Items[i].ID == id {
			a.fp.cursor = at
			a.touch()
			return
		}
	}
}

// ── the keys ────────────────────────────────────────────────────────────────

// factoryOwns is the verbs' claim on the whole keyboard: an open typing row
// takes every key but the router's walk between places and its alt chords, as
// the rail's words box does, and a habit offer takes `y` and `n`.
func (a *app) factoryOwns(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// `U`'S QUESTION HAS THE KEYBOARD while it stands, on the typing row's
	// terms: `y` goes, `n` and `esc` do not (factory_busy.go).
	if a.fp.act.refresh != nil {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryRefreshKey(msg.String()), true
	}
	// THE CLONE QUESTION HAS IT ON THE SAME TERMS (factory_clone.go).
	if a.fp.act.clone != nil {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryCloneKey(msg.String()), true
	}
	// `L`'S QUESTION HAS THE KEYBOARD ON THE SAME TERMS (factory_run.go).
	if a.fp.act.launch != nil {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryLaunchKey(msg.String()), true
	}
	if a.fp.act.ask != nil {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryAskKey(msg), true
	}
	if a.fp.act.habit != "" {
		switch msg.String() {
		case "y":
			return a.factoryHabitYes(), true
		case "n":
			a.fp.act.habit = ""
			a.touch()
			return nil, true
		}
	}
	// THE FIRST OFFER OF A RECIPE FILE takes `y` and `n` on the same terms,
	// under a habit offer when both stand (factory_recipeoffer.go).
	if cmd, took := a.factoryRecipeOfferKey(msg.String()); took {
		return cmd, true
	}
	return nil, false
}

// factoryKey is one key on the floor or the item page that the layout did not
// take, read against the item under the cursor. It answers false for a key that means nothing
// here, so the rail's own arms still see it.
func (a *app) factoryKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	// THE ANYWHERE KEYS come first: new work and the floor's settings, which
	// need no item under the cursor.
	if cmd, took := a.factorySettingsKey(k); took {
		return cmd, true
	}
	switch k {
	case "O", "shift+o":
		// `O` cycles the floor's order within each section (factory_order.go).
		if a.fp.open || !a.factoryFloorHas() {
			return nil, false
		}
		a.factoryCycleOrder()
		return nil, true
	case "U", "shift+u":
		// `U` reads the whole floor again, and asks first (factory_busy.go).
		if a.fp.open || !a.factory.Has("refreshall") || !a.factoryFloorHas() {
			return nil, false
		}
		return a.factoryRereadAll(), true
	case "m":
		// `m` opens the foreman, the floor's own conversation (factory_foreman.go).
		return a.factoryForemanKey()
	case "S", "shift+s":
		// `S` STEERS THE ITEM UNDER THE CURSOR WHEREVER IT CAN BE STEERED, in
		// words, and means nothing on any other item.
		if it, ok := a.factoryCursorItem(); ok && a.factorySteerable(it) {
			a.pageMsg = ""
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSteer, id: it.ID, label: "steer ›", example: "“redo it stronger, keep the old flag”"})
			return nil, true
		}
		return nil, false
	}
	it, ok := a.factoryCursorItem()
	// `T` IS THE ITEM'S OWN CONVERSATION in every state: it is talk about
	// the item, never a change to it. On the item page it is the manager's
	// chat in the center (factory_host.go), and on the floor, or a page too
	// narrow to host it, the chat surface's own.
	if ok && (k == "T" || k == "shift+t") && a.factory.Has("talk") {
		a.pageMsg = ""
		if a.fp.open && a.factoryCanHost() {
			return a.factoryTalkHere(it), true
		}
		return a.factoryTalk(it), true
	}
	if k == "n" && (!ok || it.State != factory.StateNeedsYou) {
		if a.factory.Has("new") && a.factoryConnected() {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskNew, repo: a.factoryNewRepo(), label: "new work ›", example: "“fix the meter at midnight, $5, plan first”"})
			return nil, true
		}
		return nil, false
	}
	if !ok {
		return nil, false
	}
	// `u` READS THE ITEM AGAIN in every state, and `g` OPENS IT ON ITS FORGE
	// wherever it has a page there and the seam has the door; on an item
	// that lives only on this machine `g` keeps its sync meaning, below.
	if k == "u" && a.factory.Has("refresh") {
		return a.factoryReread(it), true
	}
	if k == "g" && it.URL != "" && it.Origin != factory.OriginTerminal && a.factory.Has("open") {
		a.pageMsg = ""
		return a.factoryOpenForge(it), true
	}
	// THE SETTINGS' KEYS ANSWER ON THE SETTINGS ROW ONLY on the item page:
	// on any other row `s`, `w`, `b` and `1-9` do nothing, and change
	// nothing on the screen, so a letter meant for the manager never opens
	// the add-a-stage row.
	if a.factorySettingsOnlyKey(it, k) && !a.factoryOnSettingsRow(it) {
		return nil, true
	}
	a.pageMsg = ""
	a.fp.said = false
	var cmd tea.Cmd
	took := false
	switch it.State {
	case factory.StateNew, factory.StateDismissed:
		cmd, took = a.factoryNewKey(it, k)
	case factory.StateQueued, factory.StateRunning:
		cmd, took = a.factoryStreamKey(it, k)
	case factory.StateNeedsYou:
		cmd, took = a.factoryNeedsKey(it, k)
	case factory.StateLanded:
		cmd, took = a.factoryLandedKey(it, k)
	}
	// AN ANSWER KEY ON AN ITEM ASKING NOTHING SAYS SO, rather than doing
	// nothing a person could read as a missed key: `#12 is not waiting on
	// you`. Only the keys this item did not take for something of its own
	// (a new item's `a` asks its author, and `n` makes new work above).
	if !took && it.State != factory.StateNeedsYou && a.factory.Has("answer") {
		switch k {
		case "y", "n", "a":
			a.factorySay(factoryNotWaiting(it))
			return nil, true
		}
	}
	return cmd, took
}

// factorySettingsOnlyKey says whether k is one of the settings row's own
// keys for the item: add a stage, set in words, save the stages as the
// recipe and the stage toggles, on a new or dismissed item. `t`, `e` and `c`
// are not among them: the page's knobs answer on every row.
func (a *app) factorySettingsOnlyKey(it factory.Item, k string) bool {
	if it.State != factory.StateNew && it.State != factory.StateDismissed {
		return false
	}
	switch k {
	case keyAddStage, keyInWordsSet, keySaveRecipe, "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return true
	}
	return false
}

// factoryOnSettingsRow says whether the settings' keys answer where the
// person stands: on the floor, which has no rows of the page, and on the
// item page's settings row.
func (a *app) factoryOnSettingsRow(it factory.Item) bool {
	if !a.fp.open {
		return true
	}
	r, ok := a.factoryPageRowAt(it)
	return ok && r.kind == factoryPageSettings
}

// factoryNewKey is a key on a new (or dismissed) item: its card's keys.
func (a *app) factoryNewKey(it factory.Item, k string) (tea.Cmd, bool) {
	seam, id := a.factory, it.ID
	switch k {
	case keyRun:
		// `r` RUNS THE ITEM AS IT STANDS: every stage, holding at each
		// approve step. There is no second run key (owner decision,
		// 2026-10-08).
		// AND NEVER WITHOUT A CHECKOUT: the gate asks to clone first
		// (factory_clone.go).
		if a.factoryCanRun() {
			return a.factoryRunIDs([]int{id}, factoryRunItem), true
		}
	case "L", "shift+l":
		if seam.Has("launch") {
			// MARKS ARE ASKED ABOUT FIRST, with what they would cost in all
			// (factory_run.go's [app.factoryOpenLaunch]); with none, `L`
			// launches the item under the cursor as it stands.
			if ids := a.factoryMarkedIDs(); len(ids) > 0 {
				a.factoryOpenLaunch(ids)
				return nil, true
			}
			return a.factoryRunIDs([]int{id}, factoryRunOne), true
		}
	case "c":
		if seam.Has("setcap") {
			usd := factoryNextCap(it.Cap)
			return a.factoryDo(func(s factory.Seam) error { return s.SetCap(id, usd) }, nil), true
		}
	case "e":
		return a.factoryCycleEffort(it)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if seam.Has("setstage") {
			at := int(k[0] - '1')
			stages := factoryStages(a.fp.snap, it)
			if at >= len(stages) {
				return nil, true
			}
			// A STAGE THE RECIPE FIXES IS NOT SWITCHED OFF: the note line says
			// why, and no door is asked (factory_stagefixed.go).
			if stageLocked(stages[at]) {
				a.factorySay(factoryFixedWords(stages[at].Name))
				return nil, true
			}
			on := !stages[at].On
			return a.factoryDo(func(s factory.Seam) error { return s.SetStage(id, at, on) }, nil), true
		}
	case "s":
		if seam.Has("addstage") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskStage, id: id, label: "+ stage ›", example: "“after review, make it neater”"})
			return nil, true
		}
	case "w":
		if seam.Has("steer") || seam.Has("setcap") || seam.Has("seteffort") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskWords, id: id, label: "in words ›", example: "“$8, stronger”"})
			return nil, true
		}
	case "b":
		if seam.Has("bankstages") {
			repo := factoryRepoShort(it.Repo)
			a.fp.act.doing = "banking…"
			return a.factoryDo(func(s factory.Seam) error { return s.BankStages(id) }, func(err error) {
				if err == nil {
					a.factorySay("these stages are " + repo + "'s recipe now")
				}
			}), true
		}
	case "g":
		if seam.Has("sync") && it.Origin == factory.OriginTerminal {
			on := !it.Synced
			return a.factoryDo(func(s factory.Seam) error { return s.Sync(id, on) }, nil), true
		}
	case "a":
		if seam.Has("askauthor") && len(nonEmpty(it.Triage.Questions)) > 0 {
			return a.factoryDo(func(s factory.Seam) error { return s.AskAuthor(id) }, nil), true
		}
	case "d":
		if seam.Has("dismiss") {
			return a.factoryDo(func(s factory.Seam) error { return s.Dismiss(id) }, nil), true
		}
	}
	return nil, false
}

// factoryStreamKey is a key on an item on a bench or waiting for one.
func (a *app) factoryStreamKey(it factory.Item, k string) (tea.Cmd, bool) {
	seam, id := a.factory, it.ID
	switch k {
	case "x":
		if seam.Has("stop") {
			return a.factoryVerb(id, func(s factory.Seam) error { return s.Stop(id) }, func(it factory.Item) string {
				return it.Ref() + " stopped" + rowSep + "branch kept"
			}), true
		}
	case "e":
		return a.factoryCycleEffort(it)
	}
	return nil, false
}

// factoryNeedsKey is a key on an item waiting on the person.
func (a *app) factoryNeedsKey(it factory.Item, k string) (tea.Cmd, bool) {
	seam, id := a.factory, it.ID
	switch k {
	case "y", "n":
		if seam.Has("answer") {
			yes := k == "y"
			return a.factoryVerb(id, func(s factory.Seam) error { return s.Answer(id, yes, "") }, func(it factory.Item) string {
				if yes {
					return "answered " + it.Ref() + rowSep + "yes"
				}
				return "answered " + it.Ref() + rowSep + "no"
			}), true
		}
	case "a":
		if seam.Has("answer") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskAnswer, id: id, label: "answer ›", example: "“go, but keep the old flag”"})
			return nil, true
		}
	case "x":
		return a.factoryStreamKey(it, k)
	}
	return nil, false
}

// factoryLandedKey is a key on a landed item's proof sheet: `s` signs off,
// `e` signs off with changes (the one a sheet with a row nothing showed takes),
// `B` sends it back in words, `v` checks it again. A FAILED CLAIM MAKES THE
// BLOCKING ACTION THE DEFAULT KEY: `enter` signs off only when every claim and
// policy row was shown, and otherwise opens the send-back row with the first
// row nothing showed already named in it. That `enter` reaches here only from
// the proof of the item page ([app.factoryLayoutKey]); on the floor `enter`
// opens the page.
//
// `s` ASKS THE DOOR WHATEVER THE SHEET SAYS: the runner refuses a plain
// sign-off on a row not shown, in its own sentence (`#9 has a claim not
// shown`), and that sentence is what the note line says.
func (a *app) factoryLandedKey(it factory.Item, k string) (tea.Cmd, bool) {
	seam, id := a.factory, it.ID
	switch k {
	case "enter":
		if failed := factoryFirstFailed(it); failed != "" {
			if seam.Has("sendback") {
				a.factoryOpenAsk(factoryAsk{kind: factoryAskSendBack, id: id, label: wordRequestChanges + " ›", example: "“prove restart survival”", text: "prove " + failed})
				return nil, true
			}
			return nil, false
		}
		if seam.Has("signoff") {
			return a.factorySignOff(it, false), true
		}
	case "s":
		if seam.Has("signoff") {
			return a.factorySignOff(it, false), true
		}
	case "e":
		if seam.Has("signoff") && factoryFirstFailed(it) != "" {
			return a.factorySignOff(it, true), true
		}
	case "B", "shift+b":
		if seam.Has("sendback") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSendBack, id: id, label: wordRequestChanges + " ›", example: "“prove restart survival”"})
			return nil, true
		}
	case "v":
		if seam.Has("reverify") {
			return a.factoryVerb(id, func(s factory.Seam) error { return s.Reverify(id) }, func(it factory.Item) string {
				return it.Ref() + "'s checks are running again"
			}), true
		}
	case "d":
		if it.Diff != "" {
			a.factorySay("the diff is the appendix · " + it.Diff + " · opens in your editor later")
			return nil, true
		}
	}
	return nil, false
}

// factorySignOff ships a landed item and, when the floor says a habit is due,
// draws the offer to bank one.
func (a *app) factorySignOff(it factory.Item, edited bool) tea.Cmd {
	id, repo := it.ID, it.Repo
	due := false
	return a.factoryDo(func(s factory.Seam) error {
		var err error
		due, err = s.SignOff(id, edited)
		return err
	}, func(err error) {
		if err != nil {
			return
		}
		if edited {
			a.factorySay(it.Ref() + " shipped with changes")
		} else {
			a.factorySay(it.Ref() + " shipped")
		}
		if due && a.factory.Has("bank") {
			a.fp.act.habit = repo
		}
	})
}

// factoryHabitYes banks the offered habit on its repo.
func (a *app) factoryHabitYes() tea.Cmd {
	repo := a.fp.act.habit
	a.fp.act.habit = ""
	a.touch()
	if !a.factory.Has("bank") {
		return nil
	}
	short := factoryRepoShort(repo)
	// THE NOTE SAYS WHAT BECAME OF THE LINE (a pull request for the team, a
	// branch, a plain write: recipechange.go's sentences) where the seam can
	// answer it; a seam with only Bank says the line is in.
	var note string
	return a.factoryDo(func(s factory.Seam) error {
		if s.BankNote != nil {
			var err error
			note, err = s.BankNote(repo, factoryHabitSentence)
			return err
		}
		return s.Bank(repo, factoryHabitSentence)
	}, func(err error) {
		if err != nil {
			return
		}
		if strings.TrimSpace(note) != "" {
			a.factorySay(note)
			return
		}
		a.factorySay("banked on " + short)
	})
}

// factoryCycleEffort is `e`: the running stage's effort, or the first stage's
// on an item that has not started, steps "" → cheap → strong → "".
func (a *app) factoryCycleEffort(it factory.Item) (tea.Cmd, bool) {
	if !a.factory.Has("seteffort") {
		return nil, false
	}
	at := factoryEffortStage(a.fp.snap, it)
	stages := factoryStages(a.fp.snap, it)
	if at < 0 || at >= len(stages) {
		return nil, true
	}
	id, next := it.ID, factoryNextEffort(stages[at].Effort)
	return a.factoryDo(func(s factory.Seam) error { return s.SetEffort(id, at, next) }, nil), true
}

// factoryEffortStage is the stage `e` turns: the one behind the running phase,
// matched by name, and otherwise the first stage that is on.
func factoryEffortStage(snap factory.Snapshot, it factory.Item) int {
	stages := factoryStages(snap, it)
	if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) {
		for i, st := range stages {
			if st.Name == s.Phases[s.Cur].Name {
				return i
			}
		}
	}
	for i, st := range stages {
		if st.On {
			return i
		}
	}
	return -1
}

// factoryNextCap and factoryNextEffort are the cap's and the effort's
// ladders, each wrapping to its first rung.
func factoryNextCap(usd float64) float64 {
	for _, c := range factoryCaps {
		if c > usd {
			return c
		}
	}
	return factoryCaps[0]
}

func factoryNextEffort(e string) string {
	for i, x := range factoryEfforts {
		if x == e {
			return factoryEfforts[(i+1)%len(factoryEfforts)]
		}
	}
	return factoryEfforts[0]
}

// factoryFirstFailed is the first claim, then the first policy row, that
// nothing showed, and "" when every row was shown.
func factoryFirstFailed(it factory.Item) string {
	for _, c := range it.Proof {
		if !c.OK {
			return c.Text
		}
	}
	for _, c := range it.Policy {
		if !c.OK {
			return c.Text
		}
	}
	return ""
}

// factoryMarkedIDs is every item the person marked that is still new, in the
// order the floor lists them.
func (a *app) factoryMarkedIDs() []int {
	var out []int
	for _, it := range a.fp.snap.Items {
		if it.State == factory.StateNew && a.factoryMarked(it) {
			out = append(out, it.ID)
		}
	}
	return out
}

// factoryNewRepo is the repo new work lands on: the one the rail is showing,
// and otherwise the floor's first.
func (a *app) factoryNewRepo() string {
	if r := a.fp.repo; r > 0 && r <= len(a.fp.snap.Repos) {
		return a.fp.snap.Repos[r-1].Name
	}
	if len(a.fp.snap.Repos) > 0 {
		return a.fp.snap.Repos[0].Name
	}
	return ""
}

// ── the typing row ──────────────────────────────────────────────────────────

// factoryOpenAsk opens the typing row.
func (a *app) factoryOpenAsk(ask factoryAsk) {
	a.pageMsg = ""
	a.fp.act.ask = &ask
	a.touch()
}

// factoryAskKey is a key while the typing row is open, which has the whole
// keyboard: letters type, `backspace` takes one back, `ctrl+u` clears, `enter`
// submits and `esc` cancels. THE MANAGER'S BOX IS NOT CANCELLED BY `esc`: it
// gives the keys back and keeps its words as the box's draft
// (factory_timeline.go), which `enter` spends.
func (a *app) factoryAskKey(msg tea.KeyPressMsg) tea.Cmd {
	ask := a.fp.act.ask
	switch msg.String() {
	case "esc":
		if ask.kind == factoryAskManager && a.fp.tl.id == ask.id {
			a.fp.tl.draft = ask.text
		}
		a.fp.act.ask = nil
		a.touch()
		return nil
	case "enter":
		if ask.kind == factoryAskManager && a.fp.tl.id == ask.id {
			a.fp.tl.draft = ""
		}
		a.fp.act.ask = nil
		a.touch()
		words := strings.TrimSpace(ask.text)
		if words == "" {
			return nil
		}
		return a.factorySubmit(*ask, words)
	case "backspace":
		if ask.text != "" {
			_, size := utf8.DecodeLastRuneInString(ask.text)
			ask.text = ask.text[:len(ask.text)-size]
			a.touch()
		}
		return nil
	case "ctrl+u":
		ask.text = ""
		a.touch()
		return nil
	case "ctrl+k":
		// THE CARET STANDS AT THE END OF THE WORDS, so the kill to the end has
		// nothing after it to take; the key is answered, and keeps the words.
		return nil
	case "space":
		ask.text += " "
		a.touch()
		return nil
	}
	k := msg.Key()
	if k.Text == "" || k.Mod&(tea.ModAlt|tea.ModCtrl|tea.ModMeta|tea.ModSuper) != 0 {
		return nil
	}
	ask.text += k.Text
	a.touch()
	return nil
}

// factorySubmit hands the typing row's words to its door.
func (a *app) factorySubmit(ask factoryAsk, words string) tea.Cmd {
	if cmd, took := a.factorySettingsSubmit(ask, words); took {
		return cmd
	}
	seam, id := a.factory, ask.id
	switch ask.kind {
	case factoryAskStage:
		return a.factoryDo(func(s factory.Seam) error { return s.AddStage(id, words) }, nil)
	case factoryAskSteer:
		return a.factoryVerb(id, func(s factory.Seam) error { return s.Steer(id, words) }, func(it factory.Item) string {
			return "steered " + it.Ref()
		})
	case factoryAskAnswer:
		// AT AN APPROVE STEP WORDS ARE CONTINUE WITH THEM as the person's
		// note (`n` is the one that sends the run back); elsewhere the yes
		// travels with words as it always did.
		cur, _ := a.factoryItemByID(id)
		yes := cur.QKind == factory.QKindApprove
		return a.factoryVerb(id, func(s factory.Seam) error { return s.Answer(id, yes, words) }, func(it factory.Item) string {
			return "answered " + it.Ref() + " in words"
		})
	case factoryAskSendBack:
		return a.factoryVerb(id, func(s factory.Seam) error { return s.SendBack(id, words) }, func(it factory.Item) string {
			return it.Ref() + " " + wordChangesRequested + rowSep + words
		})
	case factoryAskNew:
		repo, made := ask.repo, 0
		return a.factoryDo(func(s factory.Seam) error {
			var err error
			made, err = s.New(repo, words)
			return err
		}, func(err error) {
			// NEW WORK LANDS ON THE FLOOR, with the cursor on its row and its
			// card in the peek, even when it was typed from an item page: the
			// page was about another item.
			if err == nil {
				a.fp.open = false
				a.factoryFocus(made)
			}
		})
	case factoryAskWords:
		return a.factoryWords(id, words, seam)
	case factoryAskManager:
		return a.factoryTimelineSend(id, words)
	}
	return nil
}

// factoryWords is chips in words on a new item. AN ITEM WITH A STREAM IS
// STEERED, which lifts the chips and folds the rest into the work. AN ITEM
// THAT HAS NEVER RUN HAS NO STREAM TO STEER, so the surface lifts the chips itself with the floor's own reader
// ([factory.LiftChips]) and turns each one through its own door. What no door
// can hold before a launch, a round count and the loose words, is said on the
// note line rather than dropped silently.
func (a *app) factoryWords(id int, words string, seam factory.Seam) tea.Cmd {
	it, ok := a.factoryItemByID(id)
	if ok && it.Stream != nil && seam.Has("steer") {
		return a.factoryDo(func(s factory.Seam) error { return s.Steer(id, words) }, nil)
	}
	usd, rounds, effort, rest := factory.LiftChips(words)
	at := -1
	if ok {
		at = factoryEffortStage(a.fp.snap, it)
	}
	var kept []string
	if rounds != nil {
		kept = append(kept, strconv.Itoa(*rounds)+" rounds")
	}
	if rest != "" {
		kept = append(kept, "“"+rest+"”")
	}
	none := (usd == nil || !seam.Has("setcap")) && (effort == "" || at < 0 || !seam.Has("seteffort"))
	return a.factoryDo(func(s factory.Seam) error {
		if usd != nil && s.SetCap != nil {
			if err := s.SetCap(id, *usd); err != nil {
				return err
			}
		}
		if effort != "" && at >= 0 && s.SetEffort != nil {
			if err := s.SetEffort(id, at, effort); err != nil {
				return err
			}
		}
		return nil
	}, func(err error) {
		switch {
		case err != nil:
		case none:
			a.factorySay("those words set nothing · say a $budget or how hard to think")
		case len(kept) > 0:
			a.factorySay(strings.Join(kept, " and ") + " wait for a stream · S steers it once it runs")
		}
	})
}

// factoryItemByID is the snapshot's item with id.
func (a *app) factoryItemByID(id int) (factory.Item, bool) {
	for _, it := range a.fp.snap.Items {
		if it.ID == id {
			return it, true
		}
	}
	return factory.Item{}, false
}

// ── drawing ─────────────────────────────────────────────────────────────────

// factoryFootRows is what the verbs draw at the bottom of the pane column: the
// habit offer's two lines, then the typing row. Each row is at most measure
// cells and carries no lead; the body puts the pane's lead on it.
func (a *app) factoryFootRows(measure int) []string {
	return append(a.factoryFootRowsWhere(measure, false), a.factoryFootRowsWhere(measure, true)...)
}

// factoryFootOnRows says whether a foot row is about the floor rather than the
// item under the cursor: the `n` typing row, which makes new work, and `U`'s
// question, which reads every item. ON THE FLOOR THOSE STAND AT THE BOTTOM OF
// THE ROWS' COLUMN, the whole of its width (owner ruling, 2026-10-08): under
// the peek they read as being about the item the peek shows.
func (a *app) factoryFootOnRows() bool {
	return a.fp.act.refresh != nil || a.fp.act.launch != nil || a.fp.act.clone != nil || (a.fp.act.ask != nil && a.fp.act.ask.kind == factoryAskNew)
}

// factoryFootRowsWhere is the foot rows of one side: rows false is the
// item's (the habit offer, the settings' offers, every typing row but `n`'s),
// rows true the floor's ([app.factoryFootOnRows]).
func (a *app) factoryFootRowsWhere(measure int, rows bool) []string {
	if measure <= 0 {
		return nil
	}
	pal := a.pal
	var out []string
	if rows {
		if q := a.factoryRefreshRow(measure); q != "" {
			out = append(out, q)
		}
		if q := a.factoryLaunchRow(measure); q != "" {
			out = append(out, q)
		}
		out = append(out, a.factoryCloneRows(measure)...)
		if ask := a.fp.act.ask; ask == nil || ask.kind != factoryAskNew {
			return out
		}
	}
	if repo := a.fp.act.habit; repo != "" && !rows {
		out = append(out,
			pal.ink(fit("habit forming — 3 approvals without edits on "+factoryRepoShort(repo), measure)),
			fit(pal.muted(factoryHabitSentence+"? ")+pal.accent("[y] bank it")+pal.dim(" · [n] not yet"), measure))
	}
	if !rows {
		out = append(out, a.factoryRecipeOfferRows(measure)...)
	}
	if !rows {
		out = append(out, a.factoryOfferRows(measure)...)
	}
	// THE MANAGER'S BOX IS THE TIMELINE'S OWN LAST ROW (factory_timeline.go),
	// so its typing row is drawn there and never a second time down here.
	if ask := a.fp.act.ask; ask != nil && ask.kind != factoryAskManager && (ask.kind == factoryAskNew) == rows {
		out = append(out, a.factoryAskLine(ask, measure))
	}
	return out
}

// factoryAskLine is one typing row as it is drawn: its label in the accent,
// the words in ink with the newest in view, the cursor cell, and the dim
// example while nothing is typed.
func (a *app) factoryAskLine(ask *factoryAsk, measure int) string {
	pal := a.pal
	label := pal.accent(ask.label) + " "
	room := max(measure-ansi.StringWidth(ask.label)-2, 0)
	text := ask.text
	// A TOKEN IS NEVER DRAWN: one mark a character, as every secret
	// typed on this surface is ([keyLine]).
	if factoryAskSecret(ask) {
		text = a.factoryMask(text)
	}
	if w := ansi.StringWidth(text); w > room {
		// The row keeps the newest words in view, as a box does, and its
		// first cell is a `…` so a window that does not start at the
		// beginning says so. The cursor cell is outside the room, so it
		// stays drawn.
		if room > 1 {
			text = "…" + ansi.Cut(text, w-(room-1), w)
		} else {
			text = ansi.Cut(text, w-room, w)
		}
	}
	row := label + pal.ink(text) + pal.cursor(" ", 1)
	if ask.text == "" && ask.example != "" {
		row += " " + pal.dim(ask.example)
	}
	return fit(row, measure)
}

// factoryOpenForge is `g` on an item with a page on its forge: the seam's
// Open door names the page, and the platform's opener (opener.go) opens it,
// both off the loop. The note line says where it went, or why it did not.
func (a *app) factoryOpenForge(it factory.Item) tea.Cmd {
	open, id, ref := a.factory.Open, it.ID, it.Ref()
	return a.offLoop(func() func(bool) tea.Cmd {
		url, err := open(id)
		url = strings.TrimSpace(url)
		if err != nil {
			url = ""
		} else if url != "" {
			err = processOpener(url)
		}
		return func(bool) tea.Cmd {
			switch {
			case url == "":
				a.pageMsg = ref + " has no page on github"
			case err != nil:
				a.pageMsg = "could not open your browser" + rowSep + url
			default:
				a.pageMsg = "opened " + ref + " on github"
			}
			a.touch()
			return nil
		}
	})
}

// factoryCanRun says whether `r run` does anything on this seam: a launch to
// start the item. It is the one predicate the hint line and the peek's key
// line both ask, so the two cannot offer different keys for the same item.
// THE RUN HOLDS AT EACH APPROVE STEP among the item's stages, and the
// stages are what change that.
func (a *app) factoryCanRun() bool {
	return a.factory.Has("launch")
}

// factoryVerbRail is the item's strip: AT MOST [factoryStripMost] clauses,
// the `enter` clause first, then the verbs of the item's state in the order
// a person reaches for them (owner decision, 2026-10-08):
//
//	new      enter open · r run · T chat · space select
//	running  enter open · x stop · T chat · space pause · S steer
//	needs    enter open · y yes · n no · a in words · T chat
//	landed   enter proof · s approve · B request changes · v re-run checks · T chat
//
// `L run selected` stands in the fifth place of a new item's strip only while
// a row is ticked. EVERY OTHER KEY IS ON THE `?` SHEET ([app.factorySheet]):
// the strip is the row's verbs, never the whole keyboard. Only keys that work
// for the item's state on this seam are on it.
func (a *app) factoryVerbRail(it factory.Item) []string {
	open := factoryHintClause(keyOpen, wordOpen)
	if it.State == factory.StateLanded {
		open = factoryHintClause(keyOpen, wordProof)
	}
	out := []string{open}
	for _, r := range a.factoryVerbRows(it) {
		out = append(out, factoryHintClause(r.key, r.word))
	}
	if len(out) > factoryStripMost {
		out = out[:factoryStripMost]
	}
	return out
}

// factoryVerbRows is the item's verbs as key and word, in the strip's order
// and without its `enter` clause or its cut: THE ONE LIST the peek's strip,
// the item page's action line and the item page's verbs on the right
// (factory_verbs.go) all read, so the three cannot name different verbs for
// one item.
func (a *app) factoryVerbRows(it factory.Item) []factorySheetRow {
	seam := a.factory
	var out []factorySheetRow
	add := func(ok bool, key, word string) {
		if ok {
			out = append(out, factorySheetRow{key, word})
		}
	}
	chat := func() { add(seam.Has("talk"), keyChat, wordChat) }
	switch it.State {
	case factory.StateNew, factory.StateDismissed:
		add(a.factoryCanRun(), keyRun, wordRun)
		chat()
		add(it.State == factory.StateNew, keySelect, wordSelect)
		add(seam.Has("launch") && len(a.factoryMarkedIDs()) > 0, keyRunSelected, wordRunSelected)
	case factory.StateQueued, factory.StateRunning:
		add(seam.Has("stop"), keyStop, wordStop)
		chat()
		paused := it.Stream != nil && it.Stream.Paused
		add(seam.Has("pause") && it.State == factory.StateRunning && !paused, keyPause, wordPause)
		add(seam.Has("pause") && it.State == factory.StateRunning && paused, keyPause, wordResume)
		add(a.factorySteerable(it), keySteer, wordSteer)
	case factory.StateNeedsYou:
		add(seam.Has("answer"), keyYes, wordYes)
		add(seam.Has("answer"), keyNo, wordNo)
		add(seam.Has("answer"), keyInWords, wordInWords)
		chat()
	case factory.StateLanded:
		// A CLEAN SHEET IS APPROVED WITH `s`, one with a row nothing showed
		// with `e`, and the key is spelled ONE WAY on every line that names it.
		clean := factoryFirstFailed(it) == ""
		add(clean && seam.Has("signoff"), keyApprove, wordApprove)
		add(!clean && seam.Has("signoff"), keyApproveWithChanges, wordApproveWithChanges)
		add(seam.Has("sendback"), keyRequestChanges, wordRequestChanges)
		add(seam.Has("reverify"), keyRerunChecks, wordRerunChecks)
		chat()
	default:
		chat()
	}
	return out
}

// factoryVerbHint is the item's verbs without the `enter` clause: the item
// page's own action line, where `enter` is the bottom line's to name because
// what it does depends on the row the rail stands on.
func (a *app) factoryVerbHint(it factory.Item) []string {
	return a.factoryVerbRail(it)[1:]
}

// factoryAskHint is the hint line while the typing row is open: its own keys
// and nothing else, because every other key types. The manager's box says
// `esc back to keys`, because its `esc` keeps the words.
func factoryAskHint(ask *factoryAsk) string {
	if ask.kind == factoryAskManager {
		return "type · " + factoryHintClause(keyOpen, wordSend) + " · " + factoryHintClause(keyBack, wordBackToKeys)
	}
	verb := "send"
	switch ask.kind {
	case factoryAskStage:
		verb = "add the stage"
	case factoryAskWords:
		verb = "set them"
	case factoryAskSteer:
		verb = "steer"
	case factoryAskAnswer:
		verb = "answer"
	case factoryAskSendBack:
		verb = "send it back"
	case factoryAskNew:
		verb = "make it"
	default:
		if v := factorySettingsVerb(ask.kind); v != "" {
			verb = v
		}
	}
	return "type · enter " + verb + " · esc cancel"
}
