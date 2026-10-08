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
// off, send back, the chips and the stages. place_factory.go routes a key here
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

// factoryRoomWords is what `s` says on a landed item, whose stream's room
// would open: the stream's conversation is a later lane, so the key says where
// the room will be rather than opening nothing silently. `ENTER` IS NOT A VERB
// HERE: on a floor row it opens the item page (factory_item.go), and on that
// page it is the stage's own, except on a landed item's proof, which calls
// [app.factoryLandedKey] with it.
const factoryRoomWords = "the room opens here once streams are conversations"

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
	factoryAskWords                          // chips in words on a new item
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
	case "S", "shift+s":
		if a.factory.Has("sleep") {
			a.pageMsg = ""
			return a.factoryDo(func(s factory.Seam) error { return s.Sleep(8 * time.Hour) }, nil), true
		}
		return nil, false
	}
	it, ok := a.factoryCursorItem()
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
	a.pageMsg = ""
	a.fp.said = false
	switch it.State {
	case factory.StateNew, factory.StateDismissed:
		return a.factoryNewKey(it, k)
	case factory.StateQueued, factory.StateRunning:
		return a.factoryStreamKey(it, k)
	case factory.StateNeedsYou:
		return a.factoryNeedsKey(it, k)
	case factory.StateLanded:
		return a.factoryLandedKey(it, k)
	}
	return nil, false
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
			return a.factoryDo(func(s factory.Seam) error {
				if err := s.SetGate(id, g); err != nil {
					return err
				}
				return s.Launch(id)
			}, nil), true
		}
	case "L", "shift+l":
		if seam.Has("launch") {
			ids := a.factoryMarkedIDs()
			if len(ids) == 0 {
				ids = []int{id}
			}
			// THE MARKS ARE SPENT BY THE LAUNCH: an item that is now a stream
			// has nothing left for a mark to mean.
			for _, m := range ids {
				delete(a.fp.marked, m)
			}
			return a.factoryDo(func(s factory.Seam) error {
				for _, m := range ids {
					if err := s.Launch(m); err != nil {
						return err
					}
				}
				return nil
			}, nil), true
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
	case "s":
		if seam.Has("steer") && it.Stream != nil {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSteer, id: id, label: "steer ›", example: "“redo it stronger, keep the old flag”"})
			return nil, true
		}
	case "p":
		if seam.Has("pause") && it.State == factory.StateRunning {
			return a.factoryDo(func(s factory.Seam) error { return s.Pause(id) }, nil), true
		}
	case "x":
		if seam.Has("stop") {
			return a.factoryDo(func(s factory.Seam) error { return s.Stop(id) }, nil), true
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
			return a.factoryDo(func(s factory.Seam) error { return s.Answer(id, yes, "") }, nil), true
		}
	case "a":
		if seam.Has("answer") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskAnswer, id: id, label: "answer ›", example: "“go, but keep the old flag”"})
			return nil, true
		}
	case "s", "x":
		return a.factoryStreamKey(it, k)
	}
	return nil, false
}

// factoryLandedKey is a key on a landed item's proof sheet. A FAILED CLAIM
// MAKES THE BLOCKING ACTION THE DEFAULT KEY: `enter` ships only when every
// claim and policy row was shown, and otherwise opens the send-back row with
// the first row nothing showed already named in it. That `enter` reaches here
// only from the proof stage of the item page ([app.factoryLayoutKey]); on the
// floor `enter` opens the page.
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
	case "a":
		if seam.Has("signoff") {
			return a.factorySignOff(it, true), true
		}
	case "c":
		if seam.Has("sendback") {
			a.factoryOpenAsk(factoryAsk{kind: factoryAskSendBack, id: id, label: "send back ›", example: "“prove restart survival”"})
			return nil, true
		}
	case "o":
		if seam.Has("reverify") {
			return a.factoryDo(func(s factory.Seam) error { return s.Reverify(id) }, nil), true
		}
	case "d":
		if it.Diff != "" {
			a.factorySay("the diff is the appendix · " + it.Diff + " · opens in your editor later")
			return nil, true
		}
	case "s":
		a.factorySay(factoryRoomWords)
		return nil, true
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
		if err == nil && due && a.factory.Has("bank") {
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

// factoryNextGate, factoryNextCap and factoryNextEffort are the three chips'
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
		return a.factoryDo(func(s factory.Seam) error { return s.Steer(id, words) }, nil)
	case factoryAskAnswer:
		return a.factoryDo(func(s factory.Seam) error { return s.Answer(id, false, words) }, nil)
	case factoryAskSendBack:
		return a.factoryDo(func(s factory.Seam) error { return s.SendBack(id, words) }, nil)
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
			a.factorySay("no chip in those words · a gate, a $cap or an effort word turns one")
		case len(kept) > 0:
			a.factorySay(strings.Join(kept, " and ") + " wait for a stream · s steers it once it runs")
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
	if measure <= 0 {
		return nil
	}
	pal := a.pal
	var out []string
	if repo := a.fp.act.habit; repo != "" {
		out = append(out,
			pal.ink(fit("habit forming — 3 sign-offs without edits on "+factoryRepoShort(repo), measure)),
			fit(pal.muted(factoryHabitSentence+"? ")+pal.accent("[y] bank it")+pal.dim(" · [n] not yet"), measure))
	}
	out = append(out, a.factoryOfferRows(measure)...)
	if ask := a.fp.act.ask; ask != nil {
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

// factoryCanRun says whether `r run` and `p plan first` do anything on this
// seam: a launch to start the item and a gate to set first. It is the one
// predicate the hint line and the peek's key line both ask, so the two cannot
// offer different keys for the same item.
func (a *app) factoryCanRun() bool {
	return a.factory.Has("launch") && a.factory.Has("setgate")
}

// factoryVerbHint is the hint line's clauses for the item under the cursor:
// only keys that work for its state on this seam, in the order the card, the
// stream and the sheet draw them. `space mark` sits among them on a new item.
func (a *app) factoryVerbHint(it factory.Item) []string {
	seam := a.factory
	var out []string
	add := func(ok bool, words string) {
		if ok {
			out = append(out, words)
		}
	}
	switch it.State {
	case factory.StateNew, factory.StateDismissed:
		add(a.factoryCanRun(), "r run")
		add(a.factoryCanRun(), "p plan first")
		add(it.State == factory.StateNew, "space mark")
		add(seam.Has("launch"), "L launch marked")
		add(seam.Has("setstage") && len(factoryStages(a.fp.snap, it)) > 0, "1-9 stages")
		add(seam.Has("addstage"), "s stage")
		var chips []string
		for _, c := range []struct{ door, key string }{{"setgate", "t"}, {"setcap", "c"}, {"seteffort", "e"}} {
			if seam.Has(c.door) {
				chips = append(chips, c.key)
			}
		}
		add(len(chips) > 0, strings.Join(chips, " ")+" chips")
		add(seam.Has("dismiss"), "d hide")
	case factory.StateQueued, factory.StateRunning:
		add(seam.Has("steer") && it.Stream != nil, "s steer")
		paused := it.Stream != nil && it.Stream.Paused
		add(seam.Has("pause") && it.State == factory.StateRunning && !paused, "p pause")
		add(seam.Has("pause") && it.State == factory.StateRunning && paused, "p resume")
		add(seam.Has("stop"), "x stop")
		add(seam.Has("seteffort"), "e effort")
	case factory.StateNeedsYou:
		add(seam.Has("answer"), "y n answer")
		add(seam.Has("answer"), "a in words")
		add(seam.Has("steer") && it.Stream != nil, "s steer")
		add(seam.Has("stop"), "x stop")
	case factory.StateLanded:
		// THE SHEET'S `enter` IS ON THE ITEM PAGE'S PROOF ([app.factoryItemHint]
		// names it there); on the floor `enter` opens the page.
		if factoryFirstFailed(it) != "" {
			add(seam.Has("signoff"), "a ship anyway")
		}
		add(seam.Has("sendback"), "c send back")
		add(seam.Has("reverify"), "o check again")
		add(it.Diff != "", "d diff")
	}
	add(it.State != factory.StateNeedsYou && seam.Has("new"), "n new")
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
		verb = "turn the chips"
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
