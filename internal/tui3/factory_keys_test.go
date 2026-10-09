package tui3

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryFake is a seam whose every door records how it was asked and whose
// read hands back a fresh copy of the fixture, shaped by `shape` when a test
// needs a floor the fixture does not hold. A door named in `fail` refuses with
// that error.
type factoryFake struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error
	shape func(*factory.Snapshot)
	habit bool
	note  string // what BankNote answers; "" means the seam has only Bank
	made  int
}

func (f *factoryFake) rec(door string, args ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := make([]string, len(args))
	for i, x := range args {
		parts[i] = fmt.Sprint(x)
	}
	f.calls = append(f.calls, door+"("+strings.Join(parts, ",")+")")
	return f.fail[door]
}

// said is every door asked since the last call, and forgets them.
func (f *factoryFake) said() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *factoryFake) seam() factory.Seam {
	return factory.Seam{
		Load: func() (factory.Snapshot, error) {
			snap := factory.Fixture(factoryTestNow)
			if f.shape != nil {
				f.shape(&snap)
			}
			return snap, nil
		},
		Launch:  func(id int) error { return f.rec("Launch", id) },
		Stop:    func(id int) error { return f.rec("Stop", id) },
		Pause:   func(id int) error { return f.rec("Pause", id) },
		Dismiss: func(id int) error { return f.rec("Dismiss", id) },
		Answer:  func(id int, yes bool, words string) error { return f.rec("Answer", id, yes, words) },
		Steer:   func(id int, words string) error { return f.rec("Steer", id, words) },
		SignOff: func(id int, edited bool) (bool, error) {
			return f.habit, f.rec("SignOff", id, edited)
		},
		SendBack: func(id int, words string) error { return f.rec("SendBack", id, words) },
		Reverify: func(id int) error { return f.rec("Reverify", id) },
		Bank:     func(repo, sentence string) error { return f.rec("Bank", repo, sentence) },
		BankNote: func(repo, sentence string) (string, error) {
			if f.note == "" {
				return "", f.rec("Bank", repo, sentence)
			}
			return f.note, f.rec("BankNote", repo, sentence)
		},
		New:        func(repo, words string) (int, error) { return f.made, f.rec("New", repo, words) },
		Sync:       func(id int, on bool) error { return f.rec("Sync", id, on) },
		AskAuthor:  func(id int) error { return f.rec("AskAuthor", id) },
		SetStage:   func(id, index int, on bool) error { return f.rec("SetStage", id, index, on) },
		AddStage:   func(id int, words string) error { return f.rec("AddStage", id, words) },
		BankStages: func(id int) error { return f.rec("BankStages", id) },
		SetCap:     func(id int, usd float64) error { return f.rec("SetCap", id, usd) },
		SetEffort:  func(id, stage int, effort string) error { return f.rec("SetEffort", id, stage, effort) },
	}
}

// factoryVerbLab is the factory place over the fake seam, opened through the
// router, on a frame wide enough for every hint clause.
func factoryVerbLab(t *testing.T, f *factoryFake) *app {
	t.Helper()
	a := placeApp(t)
	a.factory = f.seam()
	a.width, a.height = 150, 44
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) || !a.fp.loaded {
		t.Fatal("the factory place did not open over the fake seam")
	}
	f.said()
	return a
}

// factoryOn puts the cursor on the item with id.
func factoryOn(t *testing.T, a *app, id int) {
	t.Helper()
	a.factoryFocus(id)
	if it, ok := a.factoryCursorItem(); !ok || it.ID != id {
		t.Fatalf("item %d is not on the rail", id)
	}
}

// factoryType presses each character of words as its own key.
func factoryType(t *testing.T, a *app, words string) {
	t.Helper()
	for _, r := range words {
		k := key(string(r))
		if r == ' ' {
			k = key(" ")
		}
		drive(t, a, k)
	}
}

// EVERY VERB ASKS THE RIGHT DOOR WITH THE RIGHT ARGUMENTS, by the state of the
// item under the cursor, AND THE SAME ON THE ITEM PAGE: every case is pressed
// once on the floor and once with the item's page open over it. `enter` on a
// row opens the page and launches nothing. THE SETTINGS' OWN KEYS (`1-9`,
// `b`) are pressed on the page's settings row, the one row they answer on.
func TestFactoryVerbsAskTheRightDoors(t *testing.T) {
	for _, c := range []struct {
		id   int
		keys []string
		want string
	}{
		// A new item, made in the terminal: the card's keys.
		{8, []string{"enter"}, ""},
		// `r` RUNS THE ITEM AS IT STANDS; `p` is no verb any more, and `t`
		// is none either: where the run holds is an approve step.
		{8, []string{"p"}, ""},
		{8, []string{"r"}, "Launch(8)"},
		{8, []string{"t"}, ""},
		{8, []string{"c"}, "SetCap(8,8)"},
		{8, []string{"e"}, "SetEffort(8,0,cheap)"},
		{8, []string{"3"}, "SetStage(8,2,false)"},
		{8, []string{"b"}, "BankStages(8)"},
		{8, []string{"g"}, "Sync(8,true)"},
		{8, []string{"d"}, "Dismiss(8)"},
		{8, []string{"L"}, "Launch(8)"},
		{8, []string{"S"}, ""},
		// A thin item asks its author; one from github does not sync.
		{6, []string{"a"}, "AskAuthor(6)"},
		{6, []string{"g"}, ""},
		// A stream.
		{2, []string{" "}, "Pause(2)"},
		{2, []string{"p"}, ""},
		{2, []string{"x"}, "Stop(2)"},
		{2, []string{"e"}, "SetEffort(2,4,cheap)"},
		{2, []string{"enter"}, ""},
		// A queued item cannot pause.
		{3, []string{" "}, ""},
		// An item waiting on the person.
		{1, []string{"y"}, "Answer(1,true,)"},
		{1, []string{"n"}, "Answer(1,false,)"},
		{1, []string{"x"}, "Stop(1)"},
		// A landed item with a claim nothing showed.
		{9, []string{"e"}, "SignOff(9,true)"},
		{9, []string{"s"}, "SignOff(9,false)"},
		{9, []string{"v"}, "Reverify(9)"},
		{9, []string{"a"}, ""},
		{9, []string{"o"}, ""},
		{9, []string{"d"}, ""},
		{9, []string{"enter"}, ""},
	} {
		for _, page := range []bool{false, true} {
			f := &factoryFake{}
			a := factoryVerbLab(t, f)
			factoryOn(t, a, c.id)
			if page {
				drive(t, a, key("enter"))
				if !a.fp.open {
					t.Fatalf("enter on item %d did not open its page", c.id)
				}
				if it, _ := a.factoryCursorItem(); a.factorySettingsOnlyKey(it, c.keys[0]) {
					factoryRowNamed(t, a, wordFacetSettings)
				}
			}
			for _, k := range c.keys {
				drive(t, a, key(k))
			}
			if got := strings.Join(f.said(), " "); got != c.want {
				t.Errorf("item %d, keys %v, item page %v: asked %q, want %q", c.id, c.keys, page, got, c.want)
			}
		}
	}
}

// THE CURSOR STAYS ON A LAUNCHED ITEM, which is now a stream, and `L` launches
// every marked item in the order the floor lists them and spends the marks.
func TestFactoryLaunchKeepsTheCursorAndSpendsTheMarks(t *testing.T) {
	f := &factoryFake{}
	f.shape = func(s *factory.Snapshot) {}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	// The next read has #1540 on a bench.
	f.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == 8 {
				s.Items[i].State = factory.StateRunning
			}
		}
	}
	drive(t, a, key("r"))
	if it, _ := a.factoryCursorItem(); it.ID != 8 || it.State != factory.StateRunning {
		t.Fatalf("after the launch the cursor is on %s (%s)", it.Ref(), it.State)
	}

	f2 := &factoryFake{}
	b := factoryVerbLab(t, f2)
	factoryOn(t, b, 7)
	drive(t, b, key(" "))
	factoryOn(t, b, 8)
	drive(t, b, key(" "))
	drive(t, b, key("L"))
	if got := f2.said(); len(got) != 0 || b.fp.act.launch == nil {
		t.Fatalf("L with marks asked %v before its question was answered", got)
	}
	drive(t, b, key("y"))
	if got := strings.Join(f2.said(), " "); got != "Launch(7) Launch(8)" {
		t.Fatalf("L asked %q", got)
	}
	if len(b.factoryMarkedIDs()) != 0 {
		t.Fatal("L left the marks on")
	}
}

// THE TYPING ROW SUBMITS ITS WORDS TO ITS DOOR, edits with backspace, and
// `esc` cancels it without asking anything.
func TestFactoryTypingRowSubmitsAndCancels(t *testing.T) {
	f := &factoryFake{made: 7}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)

	drive(t, a, key("s"))
	if a.fp.act.ask == nil {
		t.Fatal("s did not open the stage row")
	}
	text := factoryFrameText(a)
	if !strings.Contains(text, "+ stage ›") || !strings.Contains(text, "“after review, make it neater”") {
		t.Fatalf("the stage row is not drawn with its example:\n%s", text)
	}
	if got := (placeFactory{}).hint(a); got != "type · enter add the stage · esc cancel" {
		t.Fatalf("the hint with the row open is %q", got)
	}
	factoryType(t, a, "after review, make it neaterx")
	drive(t, a, key("backspace"))
	if text := factoryFrameText(a); !strings.Contains(text, "+ stage › after review, make it neater") {
		t.Fatalf("the typed words are not on the row:\n%s", text)
	}
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "AddStage(8,after review, make it neater)" {
		t.Fatalf("the stage row asked %q", got)
	}
	if a.fp.act.ask != nil {
		t.Fatal("enter left the row open")
	}

	// esc cancels, and a letter typed into the row is not a verb.
	drive(t, a, key("s"))
	factoryType(t, a, "d")
	drive(t, a, key("esc"))
	if got := f.said(); len(got) != 0 || a.fp.act.ask != nil || !a.at(pageFactory) {
		t.Fatalf("esc on the row asked %v, left it open, or left the page", got)
	}

	// Chips in words on an item that has never run turn each chip's door.
	drive(t, a, key("w"))
	factoryType(t, a, "$8 plan first stronger")
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SetCap(8,8) SetEffort(8,0,strong)" {
		t.Fatalf("chips in words asked %q", got)
	}

	// A steer on a stream, an answer in words, a send back.
	factoryOn(t, a, 2)
	drive(t, a, key("S"))
	factoryType(t, a, "redo it stronger")
	drive(t, a, key("enter"))
	factoryOn(t, a, 1)
	drive(t, a, key("a"))
	factoryType(t, a, "keep the flag")
	drive(t, a, key("enter"))
	factoryOn(t, a, 9)
	drive(t, a, key("B"))
	factoryType(t, a, "prove it twice")
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "Steer(2,redo it stronger) Answer(1,true,keep the flag) SendBack(9,prove it twice)" {
		t.Fatalf("the rows asked %q", got)
	}

	// New work lands on the repo and the cursor goes to it.
	factoryOn(t, a, 2)
	drive(t, a, key("n"))
	if text := factoryFrameText(a); !strings.Contains(text, "new work ›") {
		t.Fatalf("n did not open the new-work row:\n%s", text)
	}
	factoryType(t, a, "fix the meter")
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "New(agentfield/codeaf,fix the meter)" {
		t.Fatalf("new work asked %q", got)
	}
	if it, _ := a.factoryCursorItem(); it.ID != 7 {
		t.Fatalf("after new work the cursor is on %s, not the item the door made", it.Ref())
	}
}

// ENTER ON A LANDED ITEM'S PROOF STAGE WITH A FAILED CLAIM OPENS SEND BACK,
// NOT SHIP, with the first claim nothing showed already named; enter again
// sends it. On the floor `enter` opens the item page on its result, whose
// `enter` does nothing; the proof stage under `run` is the sheet, and its
// `enter` is the one that sends back.
func TestFactoryEnterOnAFailedClaimSendsBack(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 9)
	if strip := factoryStripOf(t, a); !strings.HasPrefix(strip, "enter proof · e approve with changes · B request changes · v re-run checks") {
		t.Fatalf("the landed strip is %q", strip)
	}
	drive(t, a, key("enter"))
	if !a.fp.open || f.said() != nil {
		t.Fatal("enter on the landed row did not open its page, or asked a door")
	}
	if it, _ := a.factoryCursorItem(); a.factoryItemRows(it)[a.fp.stage].kind != factoryPageResult {
		t.Fatalf("the landed page opened on row %d, not its result", a.fp.stage)
	}
	if hint := (placeFactory{}).hint(a); hint != "↑↓ rows · esc floor · ? keys" {
		t.Fatalf("the result's hint names an enter: %q", hint)
	}
	factoryRowNamed(t, a, "proof")
	if hint := (placeFactory{}).hint(a); hint != "↑↓ rows · enter request changes · esc floor · ? keys" {
		t.Fatalf("the proof page's hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if a.fp.act.ask == nil || a.fp.act.ask.text != "prove survives a codeaf restart" {
		t.Fatalf("enter did not open send back prefilled: %+v", a.fp.act.ask)
	}
	if got := f.said(); len(got) != 0 {
		t.Fatalf("opening send back asked %v", got)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "request changes › prove survives a codeaf restart") {
		t.Fatalf("the send-back row is not drawn:\n%s", text)
	}
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SendBack(9,prove survives a codeaf restart)" {
		t.Fatalf("enter on the row asked %q", got)
	}
}

// A CLEAN SIGN-OFF THAT MAKES A HABIT DUE DRAWS THE OFFER, AND `y` BANKS IT.
func TestFactoryHabitOfferBanksOnY(t *testing.T) {
	f := &factoryFake{habit: true}
	f.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			for j := range s.Items[i].Proof {
				s.Items[i].Proof[j].OK = true
			}
		}
	}
	a := factoryVerbLab(t, f)
	// THE OFFER'S SENTENCE IS LONG; the page is wide enough that the pane
	// beside the verbs' column holds it whole.
	a.width = 200
	factoryOn(t, a, 9)
	if strip := factoryStripOf(t, a); !strings.HasPrefix(strip, "enter proof · s approve · B request changes") {
		t.Fatalf("the clean landed strip is %q", strip)
	}
	drive(t, a, key("enter"))
	factoryRowNamed(t, a, "proof")
	if hint := (placeFactory{}).hint(a); hint != "↑↓ rows · enter approve · esc floor · ? keys" {
		t.Fatalf("the clean proof page's hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SignOff(9,false)" {
		t.Fatalf("enter on a clean sheet asked %q", got)
	}
	text := factoryFrameText(a)
	for _, want := range []string{"habit forming — 3 approvals without edits on codeaf", "factory PRs from your own issues self-ship when the proof is green? [y] bank it · [n] not yet"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the offer is missing %q:\n%s", want, text)
		}
	}
	drive(t, a, key("y"))
	if got := strings.Join(f.said(), " "); got != "Bank(agentfield/codeaf,"+factoryHabitSentence+")" {
		t.Fatalf("y on the offer asked %q", got)
	}
	if a.fp.act.habit != "" {
		t.Fatal("y left the offer drawn")
	}

	// `n` puts the offer away and asks nothing.
	drive(t, a, key("s"))
	f.said()
	if a.fp.act.habit == "" {
		t.Fatal("the second sign-off drew no offer")
	}
	drive(t, a, key("n"))
	if got := f.said(); len(got) != 0 || a.fp.act.habit != "" {
		t.Fatalf("n on the offer asked %v or left it drawn", got)
	}
	// A SEAM THAT ANSWERS WHAT BECAME OF THE LINE has its note on the note
	// line instead of `banked on`.
	f.note = "written · pull request #7 opened for the team"
	a.fp.act.habit = "agentfield/codeaf"
	drive(t, a, key("y"))
	if a.pageMsg != f.note {
		t.Fatalf("the note line says %q, not the bank's note", a.pageMsg)
	}
}

// A DOOR THAT DOES NOT EXIST IS A KEY THAT DOES NOTHING AND IS NAMED NOWHERE.
func TestFactoryNilDoorDrawsNoKeyAndIgnoresThePress(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.factory.Dismiss, a.factory.New = nil, nil
	factoryOn(t, a, 8)
	hint := (placeFactory{}).hint(a) + " · " + factoryStripOf(t, a) + " · " + factorySheetText(a)
	for _, gone := range []string{"d dismiss", "n new", "ask me at"} {
		if strings.Contains(hint, gone) {
			t.Fatalf("the hint, strip or sheet names %q with no door behind it: %q", gone, hint)
		}
	}
	if !strings.Contains(hint, "L run selected") || !strings.Contains(hint, "c budget · e thinking") {
		t.Fatalf("the sheet lost the keys that do work: %q", hint)
	}
	for _, k := range []string{"d", "p", "t", "S", "n"} {
		drive(t, a, key(k))
	}
	if got := f.said(); len(got) != 0 || a.fp.act.ask != nil {
		t.Fatalf("keys with no door asked %v or opened a row", got)
	}
}

// THE STRIP NAMES THE ROW'S VERBS FOR ITS STATE, at most five, and the bottom
// bar is the floor's navigation whatever row the cursor is on (owner
// decision, 2026-10-08). `n new` is not on it while the row needs you, where
// `n` answers no.
func TestFactoryHintByState(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.width = 400
	for _, c := range []struct {
		id          int
		strip, hint string
	}{
		{8, "enter open · r run · space select", "n new · / filter · esc back · ? keys"},
		{2, "enter open · x stop · space pause · S steer", "n new · / filter · esc back · ? keys"},
		{1, "enter open · y continue · n send back · a in words", "/ filter · esc back · ? keys"},
	} {
		factoryOn(t, a, c.id)
		if got := factoryStripOf(t, a); got != c.strip {
			t.Errorf("item %d: the strip is\n%q\nwant\n%q", c.id, got, c.strip)
		}
		if got := (placeFactory{}).hint(a); got != c.hint {
			t.Errorf("item %d: the hint is\n%q\nwant\n%q", c.id, got, c.hint)
		}
	}

	// THE ITEM PAGE'S LINE IS THE PAGE'S: the walk down its rows, what enter
	// does on the row, the way back to the floor and the sheet, nothing else.
	a.width = 400
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	if got, want := (placeFactory{}).hint(a), "↑↓ rows · esc floor · ? keys"; got != want {
		t.Fatalf("on the item page the hint is\n%q\nwant\n%q", got, want)
	}
}

// A DOOR'S REFUSAL REACHES THE NOTE LINE IN ITS OWN WORDS, and the page stays.
func TestFactoryRefusalReachesTheNoteLine(t *testing.T) {
	f := &factoryFake{fail: map[string]error{"Dismiss": errors.New("#1540 is on a bench; stop it first")}}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	drive(t, a, key("d"))
	if !a.at(pageFactory) {
		t.Fatal("a refusal left the page")
	}
	if text := factoryFrameText(a); !strings.Contains(text, "#1540 is on a bench; stop it first") {
		t.Fatalf("the refusal is not on the note line:\n%s", text)
	}
	// And the next key clears it.
	drive(t, a, key("c"))
	if strings.Contains(factoryFrameText(a), "stop it first") {
		t.Fatal("the refusal outlived the next key")
	}
}

// A STAGE WITH NO CONVERSATION SAYS WHY on the pane's action line: `enter` on
// a stream's row opens its page, and `enter` on a stage that has no room names
// the reason, asking no door. `esc` puts the floor back.
func TestFactoryEnterOnAStageWithNoRoomSaysWhy(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	for _, id := range []int{2, 10} {
		factoryOn(t, a, id)
		drive(t, a, key("enter"))
		if !a.fp.open {
			t.Fatalf("enter on item %d did not open its page", id)
		}
		// A shipped item opens on its issue; the first stage is under `run`.
		for i := 0; i < 3; i++ {
			if it, _ := a.factoryCursorItem(); a.factoryItemRows(it)[a.fp.stage].kind != factoryPageStage {
				drive(t, a, key("down"))
			}
		}
		drive(t, a, key("enter"))
		it, _ := a.factoryCursorItem()
		want := factoryNoRoomWords(a.factoryItemRows(it)[a.fp.stage].view)
		if text := factoryFrameText(a); !strings.Contains(text, want) || !a.fp.said {
			t.Fatalf("enter on a stage of item %d does not say %q:\n%s", id, want, text)
		}
		if got := f.said(); len(got) != 0 {
			t.Fatalf("enter on a stage of item %d asked %v", id, got)
		}
		drive(t, a, key("esc"))
		if a.fp.open || !a.at(pageFactory) {
			t.Fatalf("esc from item %d's page did not put the floor back", id)
		}
	}
}

// LIFTCHIPS READS THE SAME WORDS THE MOCK'S PARSER DOES, and leaves the gate
// words in the rest: an item's approve steps are its stages, never a chip.
func TestFactoryLiftChips(t *testing.T) {
	usd, rounds, effort, rest := factory.LiftChips("fix the meter, $8, plan first, two review rounds, stronger")
	if usd == nil || *usd != 8 || rounds == nil || *rounds != 2 || effort != "strong" || rest != "fix the meter plan first" {
		t.Fatalf("LiftChips = %v %v %q %q", usd, rounds, effort, rest)
	}
	usd, rounds, effort, rest = factory.LiftChips("just words")
	if usd != nil || rounds != nil || effort != "" || rest != "just words" {
		t.Fatalf("plain words lifted chips: %v %v %q %q", usd, rounds, effort, rest)
	}
}

// A TYPING ROW WIDER THAN THE PANE SCROLLS, AND SAYS SO: the window that does
// not start at the first word begins with `…`, and the newest words stay.
func TestFactoryTypingRowMarksAScrolledStart(t *testing.T) {
	f := &factoryFake{made: 7}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 8)
	drive(t, a, key("s"))
	factoryType(t, a, "alpha bravo "+strings.Repeat("charlie delta ", 20)+"zulu")
	var row string
	for _, line := range strings.Split(factoryFrameText(a), "\n") {
		if strings.Contains(line, "+ stage ›") {
			row = line
		}
	}
	if !strings.Contains(row, "+ stage › …") || !strings.HasSuffix(strings.TrimRight(row, " "), "zulu") || strings.Contains(row, "alpha") {
		t.Fatalf("the scrolled row is not marked or lost its end: %q", row)
	}
}

// A LAUNCH SAYS QUEUED ONLY WHEN THE NEXT READ STILL SAYS SO. The door writes
// queued and a bench takes the item a moment later, so the read the door's own
// ask makes sees queued: the note waits for the floor's next read, and says
// running when the item started, queued when it did not. On 2026-10-08 `r`
// said `#1 is queued · a bench frees it` while #1 started that second.
func TestFactoryLaunchNoteWaitsForTheNextRead(t *testing.T) {
	for _, c := range []struct {
		next factory.State
		key  string
		want string
	}{
		{factory.StateRunning, "r", "#1540 is running"},
		{factory.StateQueued, "r", "#1540 is queued · a bench frees it"},
		{factory.StateNeedsYou, "L", "#1540 is waiting on you"},
	} {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		factoryOn(t, a, 8)
		state := factory.StateQueued
		f.shape = func(s *factory.Snapshot) {
			for i := range s.Items {
				if s.Items[i].ID == 8 {
					s.Items[i].State = state
				}
			}
		}
		drive(t, a, key(c.key))
		if a.pageMsg != "" {
			t.Fatalf("%s said %q before the floor's next read", c.key, a.pageMsg)
		}
		state = c.next
		snap, _ := a.factory.Load()
		a.factoryFold(snap)
		if a.pageMsg != c.want {
			t.Errorf("%s then %s said %q, want %q", c.key, c.next, a.pageMsg, c.want)
		}
		snap, _ = a.factory.Load()
		a.pageMsg = ""
		a.factoryFold(snap)
		if a.pageMsg != "" {
			t.Errorf("a second read said the launch again: %q", a.pageMsg)
		}
	}
}

// THE SETTINGS' KEYS ANSWER ON THE SETTINGS ROW ONLY: on the manager row
// `s`, `w`, `b` and `1` ask nothing, open no row and change nothing on the
// screen; on the settings row `s` opens the add-a-stage row.
func TestFactorySettingsKeysOnlyOnTheSettingsRow(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbsLab(t, f, 150)
	factoryVerbsOpen(t, a, 8)
	for _, row := range []string{wordFacetManager, wordFacetIssue, wordFacetSteps} {
		factoryRowNamed(t, a, row)
		// The manager row makes its chat when the cursor rests on it (the
		// Talk door); that is the row's own ask, not a key's.
		drive(t, a, key("left"))
		before := frame(a)
		f.said()
		for _, k := range []string{keyAddStage, keyInWordsSet, keySaveRecipe, "1"} {
			drive(t, a, key(k))
			if got := f.said(); len(got) != 0 || a.fp.act.ask != nil {
				t.Fatalf("%s on the %s row asked %v or opened %+v", k, row, got, a.fp.act.ask)
			}
			if after := frame(a); after != before {
				t.Fatalf("%s on the %s row changed the screen", k, row)
			}
		}
	}
	factoryRowNamed(t, a, wordFacetSettings)
	drive(t, a, key(keyAddStage))
	if ask := a.fp.act.ask; ask == nil || ask.kind != factoryAskStage {
		t.Fatalf("s on the settings row opened %+v, not the add-a-stage row", ask)
	}
}
