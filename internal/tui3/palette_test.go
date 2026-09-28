package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The picker's list under test is the one the door hands over, and never the
// machine's: CODEAF_HOME is moved to a temporary directory so ~/.codeaf/v3/
// models.json — a real file on a developer's laptop — cannot decide what these
// tests see.
func pickerApp(t *testing.T, agent *fakeAgent, models []Model) *app {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newTestApp(agent)
	a.models = func() []Model { return models }
	return a
}

func pickerIDs(a *app) []string {
	out := make([]string, 0, len(a.pick.hits))
	for _, at := range a.pick.hits {
		// THE DOOR IS NOT A MODEL: the add-provider row rides the list's end
		// and is chosen, not compared — these helpers read the models.
		if a.pick.all[at].AddProvider {
			continue
		}
		out = append(out, a.pick.all[at].ID)
	}
	return out
}

func typeInto(t *testing.T, a *app, text string) {
	t.Helper()
	for _, r := range text {
		drive(t, a, key(string(r)))
	}
}

var pickerCatalog = []Model{
	{ID: "anthropic/claude-gpt-echo", ContextLength: 200_000},
	{ID: "openai/gpt-4.1-mini", ContextLength: 1_000_000},
	{ID: "gpt-5-classic", ContextLength: 400_000},
	{ID: "moonshotai/kimi-k3"},
}

func TestBareModelOpensThePickerAndASlugDoesNot(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := pickerApp(t, agent, pickerCatalog)

	typeLine(t, a, "/model")
	if !a.pick.open {
		t.Fatal("/model with no argument has to open the picker")
	}
	// The cursor opens on the model in use, so enter confirms rather than moves.
	if chosen, _ := a.pick.choice(); chosen.ID != "openai/gpt-4.1-mini" {
		t.Fatalf("the picker opened on %q, want the model in use", chosen.ID)
	}
	drive(t, a, key("esc"))

	typeLine(t, a, "/model gpt-5-classic")
	if a.pick.open {
		t.Fatal("/model <slug> must switch directly, with no overlay")
	}
	if agent.model != "gpt-5-classic" {
		t.Fatalf("model is %q, want gpt-5-classic", agent.model)
	}
	if agent.window != 400_000 {
		t.Fatalf("context window is %d, want the catalog's 400000", agent.window)
	}
	if got := plain(frame(a)); !strings.Contains(got, "model · gpt-5-classic") {
		t.Fatalf("the switch has to be said out loud:\n%s", got)
	}
}

func TestTheFilterRanksAPrefixAboveASubstring(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "moonshotai/kimi-k3"}, pickerCatalog)
	typeLine(t, a, "/model")

	typeInto(t, a, "gpt")
	want := []string{"gpt-5-classic", "openai/gpt-4.1-mini", "anthropic/claude-gpt-echo"}
	if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered to %v, want %v — a prefix outranks a substring", got, want)
	}

	// The same query in capitals is the same query.
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "GPT")
	if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("uppercase filtered to %v, want %v", got, want)
	}

	// A query nothing carries empties the list and says so, rather than
	// leaving the last list on screen for enter to act on.
	drive(t, a, key("ctrl+u"))
	typeInto(t, a, "zzz")
	if len(a.pick.hits) != 0 {
		t.Fatalf("filtered to %v, want nothing", pickerIDs(a))
	}
	if got := plain(frame(a)); !strings.Contains(got, "no model matches") {
		t.Fatalf("an empty list has to say so:\n%s", got)
	}
	if _, ok := a.pick.choice(); ok {
		t.Fatal("enter on an empty list must choose nothing")
	}
}

// THE MATCHED LETTERS CARRY THE EMPHASIS, AND NOTHING ELSE ON THE ROW DOES.
// A filter that answered should say WHICH letters answered: the bytes the
// query matched draw in bold over the row's own ink — the one emphasis this
// surface already owns — while the rest of the name keeps the ink it had.
// No new colour, no ground: it is a reading aid for the scan, which is what
// "subtle" asked for, and not a louder row.
func TestTheFilterCarriesTheMatchedLettersInBold(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "moonshotai/kimi-k3"}, pickerCatalog)
	typeLine(t, a, "/model")
	typeInto(t, a, "gpt")
	// THE ROW THAT MATCHED MID-NAME, unselected — the prefix row above it took
	// the cursor, and this test is about the resting rows a person scans:
	// exactly "gpt" after the author's slash is bold, and not a letter more —
	// the whole name is in the one string, so a bold run that bled past the
	// match, or one that skipped a matched byte, does not contain it.
	want := a.pal.dim("openai/") + a.pal.bold(a.pal.dim("gpt")) + a.pal.dim("-4.1-mini")
	if !strings.Contains(frame(a), want) {
		t.Fatalf("the openai row did not draw its matched letters in bold; want %q among:\n%s", want, plain(frame(a)))
	}
	// THE ROW THE CURSOR IS ON IS BOLD WHOLE ALREADY, and the span rides that
	// bold quietly: the label paints as it always has, one wrap, no bold
	// opened inside a bold that a close could cut short.
	if !strings.Contains(frame(a), a.pal.bold(a.pal.ink("gpt-5-classic"))) {
		t.Fatalf("the selected prefix row is no longer painted as itself:\n%s", plain(frame(a)))
	}
}

// ENTER APPLIES AND THE LIST STAYS UP. It used to close on the press, which
// made every choice final and every comparison a round trip; the list is a
// table now, and esc is the way out ([app.pickerKey]).
func TestEnterAppliesTheChoiceAndLeavesTheListOpen(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, pickerCatalog)
	typeLine(t, a, "/model")

	typeInto(t, a, "gpt")
	drive(t, a, key("down")) // gpt-5-classic → openai/gpt-4.1-mini
	drive(t, a, key("enter"))

	if !a.pick.open {
		t.Fatal("enter has to leave the picker open")
	}
	// AND THE MARK FOLLOWS THE CHOICE, because the row it used to sit on is no
	// longer the model in use ([picker.restate]).
	if a.pick.current != "openai/gpt-4.1-mini" {
		t.Fatalf("the list still marks %q", a.pick.current)
	}
	drive(t, a, key("esc"))
	if a.pick.open {
		t.Fatal("esc has to close the picker")
	}
	if agent.model != "openai/gpt-4.1-mini" {
		t.Fatalf("model is %q, want openai/gpt-4.1-mini", agent.model)
	}
	if agent.window != 1_000_000 {
		t.Fatalf("context window is %d, want the row's 1000000", agent.window)
	}
	if a.model != "openai/gpt-4.1-mini" {
		t.Fatal("the status line still shows the old model")
	}
	got := plain(frame(a))
	if !strings.Contains(got, "model · openai/gpt-4.1-mini") {
		t.Fatalf("the switch has to be said out loud:\n%s", got)
	}
	if strings.Contains(got, rowAll(pickerHintFieldsBare)) {
		t.Fatalf("the overlay is still on screen:\n%s", got)
	}
}

func TestEscClosesThePickerAndChangesNothing(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, pickerCatalog)

	// A half-typed sentence is suspended, not eaten: the picker's filter box is
	// its own editor, and the draft comes back whole.
	typeInto(t, a, "half a thought")
	a.openPicker()
	typeInto(t, a, "gpt")
	drive(t, a, key("down"))
	drive(t, a, key("esc"))

	if a.pick.open {
		t.Fatal("esc has to close the picker")
	}
	if agent.model != "moonshotai/kimi-k3" || agent.window != 0 {
		t.Fatalf("esc changed the session: model %q, window %d", agent.model, agent.window)
	}
	if a.input.String() != "half a thought" {
		t.Fatalf("the draft came back as %q", a.input.String())
	}
	if got := plain(frame(a)); !strings.Contains(got, "› half a thought") {
		t.Fatalf("the input line did not come back:\n%s", got)
	}
}

func TestThePickerIsBottomAnchoredAndMarksTheCurrentModel(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")

	painted, caretX, caretY := a.frame()
	lines := strings.Split(plain(painted), "\n")
	if len(lines) != a.height {
		t.Fatalf("the frame is %d rows, want %d", len(lines), a.height)
	}
	// The list sits at the foot of the frame with the filter box directly above
	// it — that is what "bottom-anchored" means here, and it is where the caret
	// has to be. The only thing below it is the status line, which is the last
	// row of every frame as of the status-down wave (view.go).
	// THE DOOR IS A ROW OF THE LIST ([app.modelPickerList]), so the drawn list
	// carries one row more than the catalog: the window is read one row further
	// up, and the door is the row the loop below does not walk.
	tail := lines[len(lines)-2-len(pickerCatalog) : len(lines)-1]
	// THE ROWS ARE IN THE LIST'S OWN ORDER, which is its first column — the name,
	// ascending — because every table on this surface opens sorted (pickersort.go).
	// This test is about WHERE the list sits and not what order it is in, so it
	// asks the picker for the order rather than assuming the catalog's.
	for i, id := range pickerIDs(a) {
		if !strings.Contains(tail[i], id) {
			t.Fatalf("row %d is %q, want %s", i, tail[i], id)
		}
	}
	// The foot keeps no blank under the box (view.go's [app.footClearance]), so
	// the filter box is the row directly above the list.
	// THE DOOR IS A ROW OF THE LIST TOO ([app.modelPickerList]), so the box is
	// one row further up than the catalog alone would put it.
	box := lines[len(lines)-len(pickerCatalog)-3]
	if !strings.Contains(box, rowAll(pickerHintFieldsBare)) {
		t.Fatalf("the filter box is %q, want the hint", box)
	}
	// THE CARET SITS AFTER THE TACK, which is the one thing the sticky `/model`
	// chip costs the text ([draftBlockTacked]): the chip is not editable, so the
	// first character a person types goes to its right.
	wantX := len(inputPad) + 2 + ansi.StringWidth(slashPickerTack) + 1
	if caretY != a.height-2-len(pickerCatalog)-1 || caretX != wantX {
		t.Fatalf("the caret is at %d,%d — it belongs in the filter box after the tack (x=%d)",
			caretX, caretY, wantX)
	}
	// Windows are shown where they are known and nowhere else. The rows are looked
	// up by NAME rather than by index, because what order the list is in is the
	// sort's business (pickersort.go) and this assertion is about the cells.
	rowOf := func(id string) string {
		for at, drawn := range pickerIDs(a) {
			if drawn == id {
				return tail[at]
			}
		}
		t.Fatalf("%s is not on the list at all", id)
		return ""
	}
	if !strings.Contains(rowOf("openai/gpt-4.1-mini"), "1M") ||
		!strings.Contains(rowOf("anthropic/claude-gpt-echo"), "200k") {
		t.Fatalf("context lengths are missing:\n%s", strings.Join(tail, "\n"))
	}
	if silent := rowOf("moonshotai/kimi-k3"); strings.Contains(silent, "0") {
		t.Fatalf("a model with no published window must show none: %q", silent)
	}

	// The model in use is accent, wherever the cursor happens to be.
	rows := a.pick.rows(a.width, a.overlayHeight(), a.pal, -1, a.reasoningFor)
	marked := false
	for _, row := range rows {
		if strings.Contains(row, a.pal.accent("openai/gpt-4.1-mini")) {
			marked = true
		}
	}
	if !marked {
		t.Fatalf("the current model is not marked:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTheModelListFallsBackToTheCacheThenTheBuiltins(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newTestApp(&fakeAgent{model: "m"})

	// Nothing from the door, nothing on disk: the built-ins are the floor, and
	// the picker still opens onto a list.
	if got := a.modelList(); len(got) != len(BuiltinModels()) || got[0].ID != BuiltinModels()[0].ID {
		t.Fatalf("with no source the list is %v, want the built-ins", got)
	}

	cached := []Model{{ID: "cached/one", ContextLength: 32_000}, {ID: "cached/two"}}
	if err := WriteModelCache(cached); err != nil {
		t.Fatalf("WriteModelCache: %v", err)
	}
	// AND THE SURFACE IS TOLD. The frame reads a memo of this file and never the
	// file (learned.go); a cache written behind a running window reaches it on
	// the pulse's beat, which is what this stands in for.
	a.refreshLearning()
	if got := a.modelList(); len(got) != 2 || got[0].ID != "cached/one" || got[0].ContextLength != 32_000 {
		t.Fatalf("the disk cache is not being read: %v", got)
	}

	// The door's list, when it has one, beats the cache.
	a.models = func() []Model { return pickerCatalog }
	if got := a.modelList(); got[0].ID != pickerCatalog[0].ID {
		t.Fatalf("the door's list has to win: %v", got)
	}
	// An empty answer from a still-warming catalog is not an answer.
	a.models = func() []Model { return nil }
	if got := a.modelList(); got[0].ID != "cached/one" {
		t.Fatalf("an empty catalog must fall through to the cache: %v", got)
	}
}

func TestAnEmptyCacheIsNeverWritten(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	if err := WriteModelCache([]Model{{ID: "  "}}); err != nil {
		t.Fatalf("WriteModelCache: %v", err)
	}
	if got := CachedModels(); got != nil {
		t.Fatalf("a bad fetch must not erase a good cache: %v", got)
	}
}

func TestContextWord(t *testing.T) {
	for _, c := range []struct {
		tokens int
		want   string
	}{{0, ""}, {-1, ""}, {512, "512"}, {164_000, "164k"}, {1_048_576, "1M"}} {
		if got := contextWord(c.tokens); got != c.want {
			t.Fatalf("contextWord(%d) = %q, want %q", c.tokens, got, c.want)
		}
	}
}

// A ROW IS NEVER WIDER THAN THE FRAME, AND A NOTE IS THE HALF THAT USED TO
// BREAK IT. [overlayRowTinted] fitted the label to the room the note left and
// clamped the gap at one cell, but never cut the NOTE — so a value longer than
// the terminal was appended whole to a label squeezed to nothing, and the row
// ran past the edge by the note's own length. Settings' `tool exceptions` found
// it at 60 columns with ten tools named in one value.
func TestAnOverlayRowNeverOutgrowsItsWidth(t *testing.T) {
	long := "• propose_task:allow, tasks:allow, read:allow, ls:allow, " +
		"services:allow, track:allow, edit:allow, write:allow, " +
		"commit:allow, web_search:allow"
	pal := newPalette(tokens.ANSI256, false)
	for _, width := range []int{4, 12, 24, 44, 60, 80, 120} {
		for _, note := range []string{long, "short", ""} {
			for _, oncursor := range []bool{false, true} {
				line := overlayRowTinted("tool exceptions", note, nil,
					oncursor, markNone, false, width, pal)
				if got := ansi.StringWidth(ansi.Strip(line)); got > width {
					t.Fatalf("at %d a row with a %d-cell note drew %d cells: %q",
						width, len([]rune(note)), got, ansi.Strip(line))
				}
			}
		}
	}
}

// A SWITCH MADE WHILE WORK IS RUNNING SAYS WHAT IT DID NOT TOUCH. A task's
// model is frozen at admission, so the person who runs /model mid-task and
// watches the task keep its old voice has been told nothing — unless the note
// says so at the moment they acted. On 2026-08-31 somebody did exactly this
// and could not tell a working feature from a broken one.
func TestSwitchingModelsMidTaskSaysRunningWorkKeepsItsModel(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := pickerApp(t, agent, pickerCatalog)
	a.taskUpdate(update(7, "long triage", session.TaskRunning, session.TaskNotice{}))

	typeLine(t, a, "/model gpt-5-classic")
	// The frame wraps the note to its width, so the sentence is read back with
	// the wrap taken out rather than matched against one lucky layout.
	got := strings.Join(strings.Fields(plain(frame(a))), " ")
	if !strings.Contains(got, "tasks already running keep the model they started on") {
		t.Fatalf("the switch did not say what it left alone:\n%s", got)
	}
}

// And with nothing running, the line is just the model — a clause about tasks
// on an idle session is noise about work that does not exist.
func TestSwitchingModelsOnAnIdleSessionSaysOnlyTheModel(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	a := pickerApp(t, agent, pickerCatalog)

	typeLine(t, a, "/model gpt-5-classic")
	got := plain(frame(a))
	if strings.Contains(got, "already running") {
		t.Fatalf("an idle switch talked about running tasks:\n%s", got)
	}
	if !strings.Contains(got, "model · gpt-5-classic") {
		t.Fatalf("the switch was not said out loud:\n%s", got)
	}
}
