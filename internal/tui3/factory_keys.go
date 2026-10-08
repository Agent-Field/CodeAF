package tui3

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FACTORY'S VERBS ─────────────────────────────────────────────────────
//
// The keys that change the floor: launch, stop, pause, answer, steer, sign
// off, send back, the gate, the cap, the effort and the stages. place_factory.go routes a key here
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
// read nobody pressed for (the three-second re-read, the mock clock's beat) is
// asked beside it ([app.besideLine]); a launch and the steer typed after it are
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

// factoryGates is the order `t` cycles the gate in.
var factoryGates = []factory.Gate{factory.GatePlan, factory.GateShip, factory.GateNone}

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
// THE PAGE'S OTHER FIELDS ARE THE RAIL'S AND THE READ'S; these are the three
// things only a verb leaves behind: an open typing row, a habit offer waiting
// for `y` or `n`, and the mock clock's beat.
type factoryActs struct {
	// ask is the open typing row, and nil when none is open.
	ask *factoryAsk
	// habit is the repo a habit offer is about, and "" when none is drawn.
	habit string
	// beatGen is the generation of the mock clock's beat: a beat from an
	// earlier opening of the page finds a newer generation and stops.
	beatGen int
	// ticking is true while a Tick is out, so a slow tick is never stacked.
	ticking bool
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
}

// ── the clock ───────────────────────────────────────────────────────────────

// factoryBeatMsg is one beat of the mock floor's clock, carrying the
// generation that armed it.
type factoryBeatMsg struct{ gen int }

// factoryBeatAfter is the next beat, [factoryMockBeat] from now.
func factoryBeatAfter(gen int) tea.Cmd {
	return surfaceTick(factoryMockBeat, func(time.Time) tea.Msg { return factoryBeatMsg{gen: gen} })
}

// factoryArmBeat starts the mock clock for a page that just opened. A SEAM
// WITH NO CLOCK GETS NO BEAT: a real engine keeps its own time, and the page
// draws no speed for it either ([factorySpeedWord]).
func (a *app) factoryArmBeat() tea.Cmd {
	if !a.factory.Has("tick") {
		return nil
	}
	a.fp.act.beatGen++
	return factoryBeatAfter(a.fp.act.beatGen)
}

// factoryBeat is one beat arriving. The clock moves by the snapshot's own
// speed every [factoryMockBeat], WHICH IS THE ONE FIGURE THE HANDOVER'S `150×`
// IS DERIVED FROM, so the speed drawn and the speed run cannot disagree. The
// beat stops for good when the page is not showing; opening the page again
// arms a new one.
func (a *app) factoryBeat(gen int) tea.Cmd {
	if gen != a.fp.act.beatGen || !a.at(pageFactory) || !a.factory.Has("tick") {
		return nil
	}
	next := factoryBeatAfter(gen)
	speed := a.fp.snap.Speed
	if a.fp.act.ticking || speed <= 0 {
		return next
	}
	tick, load := a.factory.Tick, a.factory.Load
	a.fp.act.ticking = true
	return tea.Batch(next, a.besideLine(func() func(bool) tea.Cmd {
		err := tick(speed)
		var snap factory.Snapshot
		if err == nil && load != nil {
			snap, err = load()
		}
		return func(bool) tea.Cmd {
			a.fp.act.ticking = false
			if err != nil {
				a.fp.err = err
				return nil
			}
			if load != nil {
				a.factoryFold(snap)
			}
			return nil
		}
	}))
}

// ── asking a door ───────────────────────────────────────────────────────────

// factoryDo asks one gesture's doors off the loop, in the order pressed, then
// reads the floor in the same ask and folds it in. `then` runs on the loop
// after the fold with what the doors said, for the verbs that move the cursor
// or open an offer. A refusal's own sentence goes on the note line.
func (a *app) factoryDo(act func(s factory.Seam) error, then func(err error)) tea.Cmd {
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
			if then != nil {
				then(err)
			}
			a.touch()
			return nil
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
	return nil, false
}

// factoryKey is one key on the floor or the item page that the layout did not
// take, read against the item under the cursor. It answers false for a key that means nothing
// here, so the rail's own arms still see it.
func (a *app) factoryKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	// THE ANYWHERE KEYS come first: new work and the mock's sleep, which need
	// no item under the cursor.
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
		// words; on every other item it is the mock clock's sleep.
		if it, ok := a.factoryCursorItem(); ok && a.factorySteerable(it) {
			a.pageMsg = ""
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSteer, id: it.ID, label: "steer ›", example: "“redo it stronger, keep the old flag”"})
			return nil, true
		}
		if a.factory.Has("sleep") {
			a.pageMsg = ""
			return a.factoryDo(func(s factory.Seam) error { return s.Sleep(8 * time.Hour) }, nil), true
		}
		return nil, false
	}
	it, ok := a.factoryCursorItem()
	// `T` IS THE ITEM'S OWN CONVERSATION in every state, on the floor and on
	// the item page alike: it is talk about the item, never a change to it.
	if ok && (k == "T" || k == "shift+t") && a.factory.Has("talk") {
		a.pageMsg = ""
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

// factoryNewKey is a key on a new (or dismissed) item: its card's keys.
func (a *app) factoryNewKey(it factory.Item, k string) (tea.Cmd, bool) {
	seam, id := a.factory, it.ID
	switch k {
	case "p", "r":
		if seam.Has("launch") && seam.Has("setgate") {
			g := factory.GatePlan
			if k == "r" {
				g = factory.GateShip
			}
			return a.factoryVerb(id, func(s factory.Seam) error {
				if err := s.SetGate(id, g); err != nil {
					return err
				}
				return s.Launch(id)
			}, func(it factory.Item) string {
				w := factoryLaunchedWords(it)
				if g == factory.GatePlan {
					w += rowSep + "plan first"
				}
				return w
			}), true
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
			return a.factoryVerb(id, func(s factory.Seam) error { return s.Launch(id) }, factoryLaunchedWords), true
		}
	case "t":
		if seam.Has("setgate") {
			g := factoryNextGate(it.Gate)
			return a.factoryDo(func(s factory.Seam) error { return s.SetGate(id, g) }, nil), true
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
			on := !stages[at].On
			return a.factoryDo(func(s factory.Seam) error { return s.SetStage(id, at, on) }, nil), true
		}
	case "s":
		if seam.Has("addstage") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskStage, id: id, label: "+ stage ›", example: "“after review, make it neater”"})
			return nil, true
		}
	case "w":
		if seam.Has("steer") || seam.Has("setgate") || seam.Has("setcap") || seam.Has("seteffort") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskWords, id: id, label: "in words ›", example: "“$8, plan first, stronger”"})
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
				a.factoryOpenAsk(factoryAsk{kind: factoryAskSendBack, id: id, label: "send back ›", example: "“prove restart survival”", text: "prove " + failed})
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
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSendBack, id: id, label: "send back ›", example: "“prove restart survival”"})
			return nil, true
		}
	case "v":
		if seam.Has("reverify") {
			return a.factoryVerb(id, func(s factory.Seam) error { return s.Reverify(id) }, func(it factory.Item) string {
				return it.Ref() + " is being checked again"
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
	return a.factoryDo(func(s factory.Seam) error { return s.Bank(repo, factoryHabitSentence) }, func(err error) {
		if err == nil {
			a.factorySay("banked on " + short)
		}
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

// factoryNextGate, factoryNextCap and factoryNextEffort are the gate's, the cap's and the effort's
// ladders, each wrapping to its first rung.
func factoryNextGate(g factory.Gate) factory.Gate {
	for i, x := range factoryGates {
		if x == g {
			return factoryGates[(i+1)%len(factoryGates)]
		}
	}
	return factoryGates[0]
}

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
// submits and `esc` cancels.
func (a *app) factoryAskKey(msg tea.KeyPressMsg) tea.Cmd {
	ask := a.fp.act.ask
	switch msg.String() {
	case "esc":
		a.fp.act.ask = nil
		a.touch()
		return nil
	case "enter":
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
		return a.factoryVerb(id, func(s factory.Seam) error { return s.Answer(id, false, words) }, func(it factory.Item) string {
			return "answered " + it.Ref() + " in words"
		})
	case factoryAskSendBack:
		return a.factoryVerb(id, func(s factory.Seam) error { return s.SendBack(id, words) }, func(it factory.Item) string {
			return it.Ref() + " sent back" + rowSep + words
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
	}
	return nil
}

// factoryWords is chips in words on a new item. AN ITEM WITH A STREAM IS
// STEERED, which lifts the chips and folds the rest into the work. AN ITEM
// THAT HAS NEVER RUN HAS NO STREAM TO STEER (the mock refuses it in so many
// words), so the surface lifts the chips itself with the floor's own reader
// ([factory.LiftChips]) and turns each one through its own door. What no door
// can hold before a launch, a round count and the loose words, is said on the
// note line rather than dropped silently.
func (a *app) factoryWords(id int, words string, seam factory.Seam) tea.Cmd {
	it, ok := a.factoryItemByID(id)
	if ok && it.Stream != nil && seam.Has("steer") {
		return a.factoryDo(func(s factory.Seam) error { return s.Steer(id, words) }, nil)
	}
	gate, usd, rounds, effort, rest := factory.LiftChips(words)
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
	none := (gate == nil || !seam.Has("setgate")) && (usd == nil || !seam.Has("setcap")) && (effort == "" || at < 0 || !seam.Has("seteffort"))
	return a.factoryDo(func(s factory.Seam) error {
		if gate != nil && s.SetGate != nil {
			if err := s.SetGate(id, *gate); err != nil {
				return err
			}
		}
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
			a.factorySay("those words set nothing · say a gate, a $cap or an effort")
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
	return a.fp.act.refresh != nil || a.fp.act.launch != nil || (a.fp.act.ask != nil && a.fp.act.ask.kind == factoryAskNew)
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
		if ask := a.fp.act.ask; ask == nil || ask.kind != factoryAskNew {
			return out
		}
	}
	if repo := a.fp.act.habit; repo != "" && !rows {
		out = append(out,
			pal.ink(fit("habit forming — 3 sign-offs without edits on "+factoryRepoShort(repo), measure)),
			fit(pal.muted(factoryHabitSentence+"? ")+pal.accent("[y] bank it")+pal.dim(" · [n] not yet"), measure))
	}
	if !rows {
		out = append(out, a.factoryOfferRows(measure)...)
	}
	if ask := a.fp.act.ask; ask != nil && (ask.kind == factoryAskNew) == rows {
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
		out = append(out, fit(row, measure))
	}
	return out
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

// factoryCanRun says whether `r run` and `p plan first` do anything on this
// seam: a launch to start the item and a gate to set first. It is the one
// predicate the hint line and the peek's key line both ask, so the two cannot
// offer different keys for the same item.
func (a *app) factoryCanRun() bool {
	return a.factory.Has("launch") && a.factory.Has("setgate")
}

// factoryVerbHint is the item's keys in ONE ORDER, the order every key line
// on the floor draws and drops them in: the peek's last row and the bottom
// key line both build from it ([app.factoryVerbRail] puts `enter open` in
// front), and both drop from its right end as they narrow, so a narrow peek
// and a narrow foot lose the same keys in the same order. Only keys that work
// for the item's state on this seam are in it.
//
// THE ORDER IS WHAT A PERSON REACHES FOR FIRST: the verbs of the item's state
// (run it, answer it, steer it, sign it off); then `T talk`, the item's own
// conversation, which every state has and which used to stand last and be the
// first thing a line lost; then what shapes a run before it starts (stages,
// gate, cap, effort); then hiding it, opening it on its forge, reading it
// again, and new work, which is not about this item at all.
func (a *app) factoryVerbHint(it factory.Item) []string {
	seam := a.factory
	var out []string
	add := func(ok bool, words string) {
		if ok {
			out = append(out, words)
		}
	}
	talk := func() { add(seam.Has("talk"), "T talk") }
	switch it.State {
	case factory.StateNew, factory.StateDismissed:
		add(a.factoryCanRun(), "r run")
		add(a.factoryCanRun(), "p plan first")
		add(it.State == factory.StateNew, "space mark")
		talk()
		add(seam.Has("launch"), "L launch marked")
		add(seam.Has("setstage") && len(factoryStages(a.fp.snap, it)) > 0, "1-9 stages")
		add(seam.Has("addstage"), "s stage")
		// THE GATE, THE CAP AND THE EFFORT ARE NAMED BY WHAT EACH TURNS
		// (owner ruling, 2026-10-08), never by the shape the screen draws
		// them in.
		add(seam.Has("setgate"), "t gate")
		add(seam.Has("setcap"), "c cap")
		add(seam.Has("seteffort"), "e effort")
		add(seam.Has("dismiss"), "d hide")
	case factory.StateQueued, factory.StateRunning:
		add(a.factorySteerable(it), "S steer")
		paused := it.Stream != nil && it.Stream.Paused
		add(seam.Has("pause") && it.State == factory.StateRunning && !paused, "space pause")
		add(seam.Has("pause") && it.State == factory.StateRunning && paused, "space resume")
		add(seam.Has("stop"), "x stop")
		talk()
		add(seam.Has("seteffort"), "e effort")
	case factory.StateNeedsYou:
		add(seam.Has("answer"), "y n answer")
		add(seam.Has("answer"), "a in words")
		add(a.factorySteerable(it), "S steer")
		add(seam.Has("stop"), "x stop")
		talk()
	case factory.StateLanded:
		// THE SHEET'S `enter` IS ON THE ITEM PAGE'S PROOF ([app.factoryItemHint]
		// names it there); on the floor `enter` opens the page. A clean sheet
		// signs off with `s`, one with a row nothing showed with `e`, and the
		// key is spelled ONE WAY on every line that names it.
		clean := factoryFirstFailed(it) == ""
		add(clean && seam.Has("signoff"), "s sign off")
		add(!clean && seam.Has("signoff"), "e sign off with changes")
		add(seam.Has("sendback"), "B send back")
		add(seam.Has("reverify"), "v check again")
		talk()
		add(it.Diff != "", "d diff")
	default:
		talk()
	}
	add(it.URL != "" && it.Origin != factory.OriginTerminal && seam.Has("open"), "g github")
	add(seam.Has("refresh"), "u read again")
	add(it.State != factory.StateNeedsYou && seam.Has("new"), "n new item")
	return out
}

// factoryAskHint is the hint line while the typing row is open: its own keys
// and nothing else, because every other key types.
func factoryAskHint(ask *factoryAsk) string {
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
