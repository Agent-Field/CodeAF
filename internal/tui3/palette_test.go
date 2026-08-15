package tui3

import (
	"strings"
	"testing"
)

// The picker's list under test is the one the door hands over, and never the
// machine's: AFORGE_HOME is moved to a temporary directory so ~/.aforge/v3/
// models.json — a real file on a developer's laptop — cannot decide what these
// tests see.
func pickerApp(t *testing.T, agent *fakeAgent, models []Model) *app {
	t.Helper()
	t.Setenv("AFORGE_HOME", t.TempDir())
	a := newTestApp(agent)
	a.models = func() []Model { return models }
	return a
}

func pickerIDs(a *app) []string {
	out := make([]string, 0, len(a.pick.hits))
	for _, at := range a.pick.hits {
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

func TestEnterAppliesTheChoiceAndClosesThePicker(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, pickerCatalog)
	typeLine(t, a, "/model")

	typeInto(t, a, "gpt")
	drive(t, a, key("down")) // gpt-5-classic → openai/gpt-4.1-mini
	drive(t, a, key("enter"))

	if a.pick.open {
		t.Fatal("enter has to close the picker")
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
	if strings.Contains(got, pickerHint) {
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
	// The list is the tail of the frame and the filter box sits directly above
	// it — that is what "bottom-anchored" means here, and it is where the caret
	// has to be.
	tail := lines[len(lines)-len(pickerCatalog):]
	for i, model := range pickerCatalog {
		if !strings.Contains(tail[i], model.ID) {
			t.Fatalf("row %d is %q, want %s", i, tail[i], model.ID)
		}
	}
	box := lines[len(lines)-len(pickerCatalog)-1]
	if !strings.Contains(box, pickerHint) {
		t.Fatalf("the filter box is %q, want the hint", box)
	}
	if caretY != a.height-1-len(pickerCatalog) || caretX != 2 {
		t.Fatalf("the caret is at %d,%d — it belongs in the filter box", caretX, caretY)
	}
	// Windows are shown where they are known and nowhere else.
	if !strings.Contains(tail[1], "1M") || !strings.Contains(tail[0], "200k") {
		t.Fatalf("context lengths are missing:\n%s", strings.Join(tail, "\n"))
	}
	if strings.Contains(tail[3], "0") {
		t.Fatalf("a model with no published window must show none: %q", tail[3])
	}

	// The model in use is accent, wherever the cursor happens to be.
	rows := a.pick.rows(a.width, a.overlayHeight(), a.pal)
	if !strings.Contains(rows[1], a.pal.accent("openai/gpt-4.1-mini")) {
		t.Fatalf("the current model is not marked:\n%s", rows[1])
	}
}

func TestTheModelListFallsBackToTheCacheThenTheBuiltins(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
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
	t.Setenv("AFORGE_HOME", t.TempDir())
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
