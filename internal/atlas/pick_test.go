package atlas

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE PICKER (pick.go). Bare `codeaf atlas` shows it, so two things have to
// hold: every registered map is on the list with its description, in the
// registry's order, and enter answers the map the cursor is on.
func TestPickerListsEveryMapAndEnterOpens(t *testing.T) {
	p := NewPicker()
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	frame := p.frame()
	for _, mp := range Maps {
		for _, want := range []string{mp.Name, mp.Description} {
			if !strings.Contains(frame, want) {
				t.Fatalf("the picker is missing %q of map %q:\n%s", want, mp.Name, frame)
			}
		}
	}
	if len(Maps) > 1 {
		if first, second := strings.Index(frame, Maps[0].Name), strings.Index(frame, Maps[1].Name); second < first {
			t.Fatalf("the picker is not in the registry's order:\n%s", frame)
		}
	}

	// Movement, then enter: the map under the cursor comes back.
	next, _ := p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	picker, ok := next.(*Picker)
	if !ok {
		t.Fatal("Update answered something other than the picker")
	}
	if len(Maps) > 1 && picker.cursor != 1 {
		t.Fatalf("down moved the cursor to %d, want 1", picker.cursor)
	}
	want := Maps[min(picker.cursor, len(Maps)-1)]
	next, _ = picker.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	picker = next.(*Picker)
	if picker.Chosen() != want {
		t.Fatalf("enter chose %v, want %v", picker.Chosen(), want)
	}
	if picker.Left() {
		t.Fatal("enter left the picker")
	}
}

// esc LEAVES WITHOUT OPENING ANYTHING — the same road out the map itself
// takes, and the shell's exit 0 depends on it.
func TestPickerEscLeavesWithoutOpening(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyEscape},
		{Code: 0x71, Text: "q"},
	} {
		p := NewPicker()
		next, _ := p.Update(k)
		picker, ok := next.(*Picker)
		if !ok {
			t.Fatal("Update answered something other than the picker")
		}
		if !picker.Left() || picker.Chosen() != nil {
			t.Fatalf("%s did not leave the picker whole", k.String())
		}
	}
}

// NoMap IS THE ONE SENTENCE BOTH ENTRY POINTS SAY: what was asked, and what
// the registry holds instead.
func TestNoMapNamesWhatWasAskedAndWhatThereIs(t *testing.T) {
	said := NoMap("nope")
	if !strings.Contains(said, `no map "nope"`) {
		t.Fatalf("the sentence does not name what was asked: %q", said)
	}
	for _, mp := range Maps {
		if !strings.Contains(said, mp.Name) {
			t.Fatalf("the sentence does not name %q: %q", mp.Name, said)
		}
	}
}
