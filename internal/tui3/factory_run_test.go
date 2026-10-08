package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// ── THE RUN SURFACE'S PROOF ─────────────────────────────────────────────────
//
// What factory_run.go draws and the keys it rewired, against the fake seam
// that records every door (factory_keys_test.go's factoryFake) and the
// fixture's floor, shaped where the fixture does not hold the case.

// factoryShapeItem changes the item with id on every read of the fake floor.
func factoryShapeItem(f *factoryFake, id int, change func(it *factory.Item)) {
	f.shape = func(s *factory.Snapshot) {
		for i := range s.Items {
			if s.Items[i].ID == id {
				change(&s.Items[i])
			}
		}
	}
}

// factoryRowNamed puts the item page's rail cursor on the row whose cell
// reads name, and fails when there is none.
func factoryRowNamed(t *testing.T, a *app, name string) factoryPageRow {
	t.Helper()
	it, _ := a.factoryCursorItem()
	rows := a.factoryItemRows(it)
	for i, r := range rows {
		got := ""
		switch r.kind {
		case factoryPageIssue:
			got = "issue"
		case factoryPageTalk:
			got = "talk"
		case factoryPageProof:
			got = "proof"
		case factoryPageLog:
			got = "log"
		default:
			got = r.view.stage.Name
		}
		if got == name {
			a.factoryStageSelect(i)
			return r
		}
	}
	t.Fatalf("%s has no %q row on its rail", it.Ref(), name)
	return factoryPageRow{}
}

// factoryStageChat is a real conversation on disk, the way a stage's room is:
// a session folder whose meta names a workspace, and its transcript.
func factoryStageChat(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "stage-session")
	if err := session.SaveMeta(dir, session.Meta{ID: "stage-session", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(chat, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return chat
}

// A STAGE IS A ROOM: the rail marks it with the conversation's mark, the
// crumbs go one deeper on it, `enter` opens its conversation and `esc` on the
// empty box comes back to the item page on the same row.
func TestFactoryStageRoomOpensAndEscComesBack(t *testing.T) {
	chat := factoryStageChat(t)
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) { it.Stream.Phases[0].Chat = chat })
	a := factoryVerbLab(t, f)
	var opened string
	a.open = func(where, file string) (Conversation, error) {
		opened = file
		return Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: file, Workspace: where}, nil
	}
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	factoryRowNamed(t, a, "plan")
	body := factoryBodyPlain(a, a.width, 30)
	if !strings.HasPrefix(strings.TrimSpace(body[0]), "Factory › codeaf › #1551 › plan") {
		t.Fatalf("the crumbs do not go one deeper on a room: %q", body[0])
	}
	room := a.icon(tokens.GActionCommunicate)
	planRow, writeRow := "", ""
	for _, row := range body {
		left, _, _ := factorySplitAt(row)
		switch {
		case strings.Contains(left, " plan"):
			planRow = left
		case strings.Contains(left, " write"):
			writeRow = left
		}
	}
	if !strings.Contains(planRow, room) || strings.Contains(writeRow, room) {
		t.Fatalf("the rail marks the wrong stages with a room: plan %q, write %q", planRow, writeRow)
	}
	if hint := (placeFactory{}).hint(a); !strings.Contains(hint, "enter conversation") {
		t.Fatalf("the hint does not name the room: %q", hint)
	}
	drive(t, a, key("enter"))
	if opened != chat || a.pageShowing() {
		t.Fatalf("enter on the room opened %q (page showing %v), want %q", opened, a.pageShowing(), chat)
	}
	drive(t, a, key("esc"))
	if !a.at(pageFactory) || !a.fp.open {
		t.Fatalf("esc in the room did not come back to the item page (page %v, open %v)", a.page, a.fp.open)
	}
	if it, _ := a.factoryCursorItem(); it.ID != 2 {
		t.Fatalf("back on %s, not #1551", it.Ref())
	}
	if got := f.said(); len(got) != 0 {
		t.Fatalf("walking into a room asked the doors %v", got)
	}
}

// A STAGE WITH NO ROOM SAYS WHY ON THE ACTION LINE: one that has not started,
// and a check, which never has one.
func TestFactoryStageWithoutARoomSaysWhy(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.Stream.Phases[2].Kind = factory.StageCheck
	})
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 2)
	drive(t, a, key("enter"))
	for _, c := range []struct{ row, want string }{
		{"neaten", "neaten has not started"},
		{"test", "test is a check · its log is below"},
	} {
		factoryRowNamed(t, a, c.row)
		drive(t, a, key("enter"))
		body := factoryBodyPlain(a, a.width, 30)
		last := ""
		for _, row := range body {
			if _, right, div := factorySplitAt(row); div >= 0 && strings.TrimSpace(right) != "" {
				last = strings.TrimSpace(right)
			}
		}
		if last != c.want {
			t.Fatalf("enter on %s: the action line is %q, want %q", c.row, last, c.want)
		}
		if got := f.said(); len(got) != 0 || a.pageShowing() == false {
			t.Fatalf("enter on %s asked %v or left the page", c.row, got)
		}
	}
}

// PHASE STATES AND ROUNDS DRAW: each state's mark, a round as `2/2` only where
// the stage may run more than one, the spinner on the running row, and a
// failed or waiting phase's note dim under the peek's strip.
func TestFactoryPhaseStatesAndRoundsDraw(t *testing.T) {
	for _, c := range []struct {
		round, most int
		want        string
	}{{0, 2, ""}, {1, 1, ""}, {1, 2, "1/2"}, {2, 2, "2/2"}, {3, 2, "3/3"}, {2, 0, "2/2"}} {
		if got := factoryRoundWords(c.round, c.most); got != c.want {
			t.Errorf("round %d of %d draws %q, want %q", c.round, c.most, got, c.want)
		}
	}
	if got := factoryPhaseWords(factory.Phase{Name: "plan", State: factory.PhaseDone, Round: 1}, 1); got != "plan" {
		t.Errorf("a one-round stage draws its round: %q", got)
	}

	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		ph := it.Stream.Phases
		ph[3].Round, ph[3].Note = 2, "3 findings"
		ph[2].State, ph[2].Note = factory.PhaseFailed, "test did not finish"
	})
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 2)
	it, _ := a.factoryCursorItem()
	marks := map[factory.PhaseState]string{}
	for _, st := range []factory.PhaseState{factory.PhaseDone, factory.PhaseFailed, factory.PhasePending} {
		marks[st], _ = a.factoryPhaseMark(st)
	}
	strip := ansi.Strip(a.factoryPeekStrip(it, 120))
	for _, want := range []string{marks[factory.PhaseDone] + " plan", marks[factory.PhaseFailed] + " test", "review 2/2", marks[factory.PhasePending] + " neaten"} {
		if !strings.Contains(strip, want) {
			t.Fatalf("the strip %q lacks %q", strip, want)
		}
	}
	stages := ansi.Strip(strings.Join(a.factoryPeekStages(it, 120), "\n"))
	if !strings.Contains(stages, "test · test did not finish") {
		t.Fatalf("the failed phase's note is not under the strip:\n%s", stages)
	}
	drive(t, a, key("enter"))
	body := strings.Join(factoryBodyPlain(a, a.width, 30), "\n")
	if !strings.Contains(body, a.factorySpin()+" review 2/2") {
		t.Fatalf("the running row does not wear the spinner and its round:\n%s", body)
	}
}

// THE LOG PANE'S THREE VOICES: a thought dim, the person's `steer:` line ink,
// everything else muted; each with its time at the margin, the newest last.
func TestFactoryLogPaneThreeKinds(t *testing.T) {
	f := &factoryFake{}
	factoryShapeItem(f, 2, func(it *factory.Item) {
		it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: factoryTestNow, Tone: "said", Text: "steer: keep the old flag"})
	})
	a := factoryVerbLab(t, f)
	if a.pal.dim("x") == a.pal.ink("x") || a.pal.ink("x") == a.pal.muted("x") {
		t.Skip("the lab's palette paints no colour")
	}
	factoryOn(t, a, 2)
	it, _ := a.factoryCursorItem()
	thought, steer, said := it.Stream.Log[0], it.Stream.Log[len(it.Stream.Log)-1], it.Stream.Log[1]
	if row := a.factoryLogRow(thought, 120); !strings.Contains(row, a.pal.dim(thought.Text)) {
		t.Errorf("a thought is not dim: %q", row)
	}
	if row := a.factoryLogRow(steer, 120); !strings.Contains(row, a.pal.ink(steer.Text)) {
		t.Errorf("a steer is not ink: %q", row)
	}
	if row := a.factoryLogRow(said, 120); !strings.Contains(row, a.pal.muted(said.Text)) {
		t.Errorf("a said line is not muted: %q", row)
	}
	drive(t, a, key("enter"))
	factoryRowNamed(t, a, "log")
	var pane []string
	for _, row := range factoryBodyPlain(a, a.width, 30) {
		if _, right, div := factorySplitAt(row); div >= 0 && strings.TrimSpace(right) != "" {
			pane = append(pane, strings.TrimSpace(right))
		}
	}
	if len(pane) < 2 || !strings.HasPrefix(pane[0], thought.At.Format("15:04")) || !strings.Contains(pane[len(pane)-2], "steer: keep the old flag") {
		t.Fatalf("the log pane is not oldest first, newest last, stamped:\n%s", strings.Join(pane, "\n"))
	}
}

// EVERY KEY ASKS ITS DOOR AND SAYS WHAT BECAME OF THE ITEM, and a door that
// refuses says its own sentence verbatim.
func TestFactoryRunKeysSayTheirSentence(t *testing.T) {
	for _, c := range []struct {
		id         int
		keys       []string
		call, note string
	}{
		{8, []string{"r"}, "Launch(8)", "#1540 is running"},
		{8, []string{"L"}, "Launch(8)", "#1540 is running"},
		{2, []string{"x"}, "Stop(2)", "#1551 stopped · branch kept"},
		{2, []string{" "}, "Pause(2)", "#1551 paused"},
		{1, []string{"y"}, "Answer(1,true,)", "answered #1538 · yes"},
		{1, []string{"n"}, "Answer(1,false,)", "answered #1538 · no"},
		{9, []string{"s"}, "SignOff(9,false)", "#1661 shipped"},
		{9, []string{"e"}, "SignOff(9,true)", "#1661 shipped with changes"},
		{9, []string{"v"}, "Reverify(9)", "#1661's checks are running again"},
		{9, append([]string{"B"}, strings.Split("prove it", "")...), "", ""},
		{2, append([]string{"S"}, strings.Split("stronger", "")...), "", ""},
	} {
		f := &factoryFake{}
		a := factoryVerbLab(t, f)
		factoryOn(t, a, c.id)
		for _, k := range c.keys {
			drive(t, a, key(k))
		}
		if c.call == "" {
			// A key that types opens its row and asks nothing until enter.
			if a.fp.act.ask == nil || len(f.said()) != 0 {
				t.Fatalf("item %d %v did not open a row", c.id, c.keys[0])
			}
			drive(t, a, key("enter"))
			c.call = map[string]string{"B": "SendBack(9,prove it)", "S": "Steer(2,stronger)"}[c.keys[0]]
			c.note = map[string]string{"B": "#1661 changes requested · prove it", "S": "steered #1551"}[c.keys[0]]
		}
		if got := strings.Join(f.said(), " "); got != c.call {
			t.Errorf("item %d %v asked %q, want %q", c.id, c.keys, got, c.call)
		}
		if a.pageMsg != c.note {
			t.Errorf("item %d %v said %q, want %q", c.id, c.keys, a.pageMsg, c.note)
		}
	}

	f := &factoryFake{fail: map[string]error{"Stop": errors.New("#1551 is not running")}}
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 2)
	drive(t, a, key("x"))
	if !strings.Contains(factoryFrameText(a), "#1551 is not running") || strings.Contains(a.pageMsg, "stopped") {
		t.Fatalf("a refusal did not reach the note line verbatim: %q", a.pageMsg)
	}
}

// THE PROOF SHEET BOTH WAYS: a row nothing showed offers `e` and `B`; a clean
// sheet offers `s`. Each row wears ✓ or ✕ and its medium as a chip.
func TestFactoryProofSheetBothWays(t *testing.T) {
	for _, clean := range []bool{false, true} {
		f := &factoryFake{}
		if clean {
			factoryShapeItem(f, 9, func(it *factory.Item) {
				for i := range it.Proof {
					it.Proof[i].OK = true
				}
			})
		}
		a := factoryVerbLab(t, f)
		factoryOn(t, a, 9)
		drive(t, a, key("enter"))
		text := strings.Join(factoryBodyPlain(a, a.width, 30), "\n")
		want := "1 of 6 not shown · e approve with changes · B request changes"
		if clean {
			want = "all 6 shown · s approve"
		}
		if !strings.Contains(text, want) {
			t.Fatalf("clean %v: the sheet does not end %q:\n%s", clean, want, text)
		}
		ok, bad := a.icon(tokens.GSettled), a.icon(tokens.GFailed)
		if !strings.Contains(text, ok+" fires on first true, never again") || (!clean && !strings.Contains(text, bad+" survives a codeaf restart")) {
			t.Fatalf("clean %v: the sheet's marks are wrong:\n%s", clean, text)
		}
	}

	// With no sign-off or send-back door the line keeps its count and no key.
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.factory.SignOff, a.factory.SendBack = nil, nil
	it := *factoryPaneItem(t, a, 9)
	if got := ansi.Strip(a.factorySignOffLine(it, 120)); got != "1 of 6 not shown" {
		t.Fatalf("with no doors the sign-off line is %q", got)
	}
}

// `L` WITH MARKS ASKS FIRST, with what the marked items are estimated to cost
// in all; `n` asks nothing and `y` launches them.
func TestFactoryLaunchMarkedAsksWithTheEstimate(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	for _, id := range []int{7, 8} {
		factoryOn(t, a, id)
		drive(t, a, key(" "))
	}
	drive(t, a, key("L"))
	if text := factoryFrameText(a); !strings.Contains(text, "run 2 selected · ~$8? [y] go · [n] not now") {
		t.Fatalf("the L question is not drawn:\n%s", text)
	}
	if got := (placeFactory{}).hint(a); got != "y go · n not now" {
		t.Fatalf("the hint under the question is %q", got)
	}
	drive(t, a, key("n"))
	if got := f.said(); len(got) != 0 || a.fp.act.launch != nil || len(a.factoryMarkedIDs()) != 2 {
		t.Fatalf("n asked %v, left the question, or spent the marks", got)
	}
	drive(t, a, key("L"), key("y"))
	if got := strings.Join(f.said(), " "); got != "Launch(7) Launch(8)" || a.pageMsg != "launched 2" {
		t.Fatalf("y asked %q and said %q", got, a.pageMsg)
	}
}

// ABSENT DOORS DRAW NO KEY: with no pause, steer, stop, answer, sign-off,
// send-back or re-check door, neither the hint nor the peek names them, and
// pressing them asks nothing.
func TestFactoryRunKeysAbsentWithoutDoors(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	s := &a.factory
	s.Pause, s.Steer, s.Stop, s.Answer, s.SignOff, s.SendBack, s.Reverify = nil, nil, nil, nil, nil, nil, nil
	for _, c := range []struct {
		id   int
		keys []string
	}{{2, []string{" ", "S", "x"}}, {1, []string{"y", "n", "a"}}, {9, []string{"s", "e", "B", "v"}}} {
		factoryOn(t, a, c.id)
		it, _ := a.factoryCursorItem()
		hint := (placeFactory{}).hint(a) + " | " + a.factoryActionWords(it)
		for _, gone := range []string{"space pause", "S steer", "x stop", "y yes", "a in words", "s approve", "e approve", "B request changes", "v re-run checks"} {
			if strings.Contains(hint, gone) {
				t.Fatalf("item %d names %q with no door: %q", c.id, gone, hint)
			}
		}
		for _, k := range c.keys {
			drive(t, a, key(k))
		}
		if got := f.said(); len(got) != 0 || a.fp.act.ask != nil {
			t.Fatalf("item %d: keys with no door asked %v or opened a row", c.id, got)
		}
	}
}

// THE QUESTION STANDS IN THE ITEM PAGE'S HEAD, the runner's own sentence, its
// mark amber and its words ink, where the chips stand on every other item.
func TestFactoryQuestionInTheItemPageHead(t *testing.T) {
	q := "review is not clean after 2 rounds: 3 findings · one more round, or go on as is?"
	f := &factoryFake{}
	factoryShapeItem(f, 1, func(it *factory.Item) { it.Question = q })
	a := factoryVerbLab(t, f)
	factoryOn(t, a, 1)
	drive(t, a, key("enter"))
	it, _ := a.factoryCursorItem()
	row := a.factoryItemQuestion(it, 140)
	if !strings.Contains(row, a.pal.ask(a.icon(tokens.GNeedsHuman))) || !strings.Contains(row, a.pal.ink(q)) {
		t.Fatalf("the question's paint is wrong: %q", row)
	}
	body := factoryBodyPlain(a, a.width, 30)
	if !strings.Contains(body[1], q) || !strings.Contains(body[1], factoryAnswerKeys) {
		t.Fatalf("the head's second row is not the question with its answers: %q", body[1])
	}
}

// THE FRAMES ARE EXACT AT 160, 120 AND 100 on a running item's log, a parked
// item's question and a landed item's sheet: no row wider than the frame.
func TestFactoryRunFramesAtThreeWidths(t *testing.T) {
	for _, width := range factoryAlignWidths {
		for _, c := range []struct {
			id  int
			row string
		}{{2, "log"}, {1, "plan"}, {9, "proof"}} {
			f := &factoryFake{}
			a := factoryVerbLab(t, f)
			a.width = width
			factoryOn(t, a, c.id)
			drive(t, a, key("enter"))
			factoryRowNamed(t, a, c.row)
			rows := a.factoryBody(width, 30)
			if len(rows) != 30 {
				t.Fatalf("at %d item %d drew %d rows", width, c.id, len(rows))
			}
			for i, r := range rows {
				if w := ansi.StringWidth(r.text); w != width {
					t.Fatalf("at %d item %d's %s row %d is %d cells", width, c.id, c.row, i, w)
				}
			}
		}
	}
}
