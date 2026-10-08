package tui3

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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
		SendBack:   func(id int, words string) error { return f.rec("SendBack", id, words) },
		Reverify:   func(id int) error { return f.rec("Reverify", id) },
		Bank:       func(repo, sentence string) error { return f.rec("Bank", repo, sentence) },
		New:        func(repo, words string) (int, error) { return f.made, f.rec("New", repo, words) },
		Sync:       func(id int, on bool) error { return f.rec("Sync", id, on) },
		AskAuthor:  func(id int) error { return f.rec("AskAuthor", id) },
		SetStage:   func(id, index int, on bool) error { return f.rec("SetStage", id, index, on) },
		AddStage:   func(id int, words string) error { return f.rec("AddStage", id, words) },
		BankStages: func(id int) error { return f.rec("BankStages", id) },
		SetGate:    func(id int, g factory.Gate) error { return f.rec("SetGate", id, g) },
		SetCap:     func(id int, usd float64) error { return f.rec("SetCap", id, usd) },
		SetEffort:  func(id, stage int, effort string) error { return f.rec("SetEffort", id, stage, effort) },
		Sleep:      func(d time.Duration) error { return f.rec("Sleep", d) },
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
// row opens the page and launches nothing.
func TestFactoryVerbsAskTheRightDoors(t *testing.T) {
	for _, c := range []struct {
		id   int
		keys []string
		want string
	}{
		// A new item, made in the terminal: the card's keys.
		{8, []string{"enter"}, ""},
		{8, []string{"p"}, "SetGate(8,plan) Launch(8)"},
		{8, []string{"r"}, "SetGate(8,ship) Launch(8)"},
		{8, []string{"t"}, "SetGate(8,none)"},
		{8, []string{"c"}, "SetCap(8,8)"},
		{8, []string{"e"}, "SetEffort(8,0,cheap)"},
		{8, []string{"3"}, "SetStage(8,2,false)"},
		{8, []string{"b"}, "BankStages(8)"},
		{8, []string{"g"}, "Sync(8,true)"},
		{8, []string{"d"}, "Dismiss(8)"},
		{8, []string{"L"}, "Launch(8)"},
		{8, []string{"S"}, "Sleep(8h0m0s)"},
		// A thin item asks its author; one from github does not sync.
		{6, []string{"a"}, "AskAuthor(6)"},
		{6, []string{"g"}, ""},
		// A stream.
		{2, []string{"p"}, "Pause(2)"},
		{2, []string{"x"}, "Stop(2)"},
		{2, []string{"e"}, "SetEffort(2,3,cheap)"},
		{2, []string{"enter"}, ""},
		// A queued item cannot pause.
		{3, []string{"p"}, ""},
		// An item waiting on the person.
		{1, []string{"y"}, "Answer(1,true,)"},
		{1, []string{"n"}, "Answer(1,false,)"},
		{1, []string{"x"}, "Stop(1)"},
		// A landed item with a claim nothing showed.
		{9, []string{"a"}, "SignOff(9,true)"},
		{9, []string{"o"}, "Reverify(9)"},
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
	if got := strings.Join(f.said(), " "); got != "SetGate(8,plan) SetCap(8,8) SetEffort(8,0,strong)" {
		t.Fatalf("chips in words asked %q", got)
	}

	// A steer on a stream, an answer in words, a send back.
	factoryOn(t, a, 2)
	drive(t, a, key("s"))
	factoryType(t, a, "redo it stronger")
	drive(t, a, key("enter"))
	factoryOn(t, a, 1)
	drive(t, a, key("a"))
	factoryType(t, a, "keep the flag")
	drive(t, a, key("enter"))
	factoryOn(t, a, 9)
	drive(t, a, key("c"))
	factoryType(t, a, "prove it twice")
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "Steer(2,redo it stronger) Answer(1,false,keep the flag) SendBack(9,prove it twice)" {
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

// ENTER ON A LANDED ITEM WITH A FAILED CLAIM OPENS SEND BACK, NOT SHIP, with
// the first claim nothing showed already named; enter again sends it. On the
// floor `enter` opens the item page on its proof, which is the sheet, and the
// sheet's `enter` is the one that sends back.
func TestFactoryEnterOnAFailedClaimSendsBack(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 9)
	if hint := (placeFactory{}).hint(a); !strings.HasPrefix(hint, "enter open · a ship anyway · c send back · o check again · d diff") {
		t.Fatalf("the landed hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if !a.fp.open || f.said() != nil {
		t.Fatal("enter on the landed row did not open its page, or asked a door")
	}
	if hint := (placeFactory{}).hint(a); !strings.HasPrefix(hint, "↑↓ stages · enter send back · a ship anyway · c send back · o check again · d diff") {
		t.Fatalf("the proof page's hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if a.fp.act.ask == nil || a.fp.act.ask.text != "prove survives a codeaf restart" {
		t.Fatalf("enter did not open send back prefilled: %+v", a.fp.act.ask)
	}
	if got := f.said(); len(got) != 0 {
		t.Fatalf("opening send back asked %v", got)
	}
	if text := factoryFrameText(a); !strings.Contains(text, "send back › prove survives a codeaf restart") {
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
	factoryOn(t, a, 9)
	if hint := (placeFactory{}).hint(a); !strings.HasPrefix(hint, "enter open · c send back") {
		t.Fatalf("the clean landed hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if hint := (placeFactory{}).hint(a); !strings.HasPrefix(hint, "↑↓ stages · enter ship · c send back") {
		t.Fatalf("the clean proof page's hint is %q", hint)
	}
	drive(t, a, key("enter"))
	if got := strings.Join(f.said(), " "); got != "SignOff(9,false)" {
		t.Fatalf("enter on a clean sheet asked %q", got)
	}
	text := factoryFrameText(a)
	for _, want := range []string{"habit forming — 3 sign-offs without edits on codeaf", "factory PRs from your own issues self-ship when the proof is green? [y] bank it · [n] not yet"} {
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
	drive(t, a, key("a"))
	f.said()
	if a.fp.act.habit == "" {
		t.Fatal("the second sign-off drew no offer")
	}
	drive(t, a, key("n"))
	if got := f.said(); len(got) != 0 || a.fp.act.habit != "" {
		t.Fatalf("n on the offer asked %v or left it drawn", got)
	}
}

// A DOOR THAT DOES NOT EXIST IS A KEY THAT DOES NOTHING AND IS NAMED NOWHERE.
func TestFactoryNilDoorDrawsNoKeyAndIgnoresThePress(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.factory.Dismiss, a.factory.SetGate, a.factory.Sleep, a.factory.New = nil, nil, nil, nil
	factoryOn(t, a, 8)
	hint := (placeFactory{}).hint(a)
	for _, gone := range []string{"d hide", "p plan first", "r run", "S sleep", "n new", "t c e"} {
		if strings.Contains(hint, gone) {
			t.Fatalf("the hint names %q with no door behind it: %q", gone, hint)
		}
	}
	if !strings.Contains(hint, "L launch marked") || !strings.Contains(hint, "c e chips") {
		t.Fatalf("the hint lost the keys that do work: %q", hint)
	}
	for _, k := range []string{"d", "p", "r", "t", "S", "n"} {
		drive(t, a, key(k))
	}
	if got := f.said(); len(got) != 0 || a.fp.act.ask != nil {
		t.Fatalf("keys with no door asked %v or opened a row", got)
	}
}

// THE HINT NAMES THE KEYS FOR THE STATE, and the rail's keys go first when the
// line is too long.
func TestFactoryHintByState(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.width = 400
	for _, c := range []struct {
		id   int
		want string
	}{
		{8, "enter open · r run · p plan first · space mark · L launch marked · 1-9 stages · s stage · t c e chips · d hide · n new · / filter · [ ] repo · A backlog · z density · S sleep 8h · E recipe · esc back"},
		{2, "enter open · s steer · p pause · x stop · e effort · n new · / filter · [ ] repo · A backlog · z density · S sleep 8h · E recipe · esc back"},
		{1, "enter open · y n answer · a in words · s steer · x stop · / filter · [ ] repo · A backlog · z density · S sleep 8h · E recipe · esc back"},
	} {
		factoryOn(t, a, c.id)
		if got := (placeFactory{}).hint(a); got != c.want {
			t.Errorf("item %d: the hint is\n%q\nwant\n%q", c.id, got, c.want)
		}
	}
	a.width = 150
	factoryOn(t, a, 8)
	got := (placeFactory{}).hint(a)
	if strings.Contains(got, "S sleep") || !strings.HasPrefix(got, "enter open · r run · p plan first") || !strings.HasSuffix(got, "esc back") {
		t.Fatalf("at 150 columns the hint is %q", got)
	}

	// THE ITEM PAGE'S LINE IS THE PAGE'S: the stage walk, the item's verbs,
	// and the way back to the floor, with no rail keys.
	a.width = 400
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	if got, want := (placeFactory{}).hint(a), "↑↓ stages · s steer · p pause · x stop · e effort · n new · S sleep 8h · esc floor"; got != want {
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

// A STAGE'S CONVERSATION DOES NOT OPEN YET, AND SAYS SO on the note line:
// `enter` on a stream's row opens its page, and `enter` on a stage says where
// the conversation will be, asking no door. `esc` puts the floor back.
func TestFactoryEnterOnAStreamSaysWhereTheRoomWillBe(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	for _, id := range []int{2, 10} {
		factoryOn(t, a, id)
		drive(t, a, key("enter"))
		if !a.fp.open {
			t.Fatalf("enter on item %d did not open its page", id)
		}
		drive(t, a, key("enter"))
		if text := factoryFrameText(a); !strings.Contains(text, factoryStageNoteWords) || !a.fp.said {
			t.Fatalf("enter on a stage of item %d does not say where the conversation will be:\n%s", id, text)
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

// LIFTCHIPS READS THE SAME WORDS THE MOCK'S PARSER DOES.
func TestFactoryLiftChips(t *testing.T) {
	gate, usd, rounds, effort, rest := factory.LiftChips("fix the meter, $8, plan first, two review rounds, stronger")
	if gate == nil || *gate != factory.GatePlan || usd == nil || *usd != 8 || rounds == nil || *rounds != 2 || effort != "strong" || rest != "fix the meter" {
		t.Fatalf("LiftChips = %v %v %v %q %q", gate, usd, rounds, effort, rest)
	}
	gate, usd, rounds, effort, rest = factory.LiftChips("just words")
	if gate != nil || usd != nil || rounds != nil || effort != "" || rest != "just words" {
		t.Fatalf("plain words lifted chips: %v %v %v %q %q", gate, usd, rounds, effort, rest)
	}
}
