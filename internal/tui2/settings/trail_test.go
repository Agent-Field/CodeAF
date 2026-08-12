package settings

import (
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The back grammar, and the defect it answers: a reader who reached this sheet
// through the palette had NO way back. Esc threw the whole flow away, clicking
// did nothing, and no key returned to the list they came from.
//
// Everything below is the palette's own grammar (internal/tui2/palette/trail.go)
// asked of this surface: the trail is where you are and the way back, the
// ancestors are doors and the current step is not, backspace on an empty query
// walks one rung, and esc still closes everything from any depth.

// drilled is a sheet reached through one ancestor, with the back door recorded
// rather than performed — this package holds words and never learns what they
// open, so the test asserts the ASK and not an outcome it does not own.
func drilled(t *testing.T, trail ...string) (*Model, *[]int) {
	t.Helper()
	asked := &[]int{}
	m := New(Options{Registry: config.NewSettings(config.SettingsOptions{})})
	m.SetTrail(trail, func(depth int) tea.Cmd {
		*asked = append(*asked, depth)
		return nil
	})
	return m, asked
}

func press(m *Model, name string) tea.Cmd {
	return m.Key(tea.KeyPressMsg{Code: keyCodeOf(name), Text: textOf(name)})
}

// keyCodeOf and textOf spell the two keys these tests press. They are here
// rather than in a shared helper because two keys is not a keyboard.
func keyCodeOf(name string) rune {
	switch name {
	case "backspace":
		return tea.KeyBackspace
	case "esc":
		return tea.KeyEscape
	}
	return []rune(name)[0]
}

func textOf(name string) string {
	switch name {
	case "backspace", "esc":
		return ""
	}
	return name
}

// A sheet nobody drilled into draws the header it has always drawn, records no
// click targets, and answers backspace exactly as it did: the back grammar is a
// property of how you GOT here, and alt+, is not a path.
func TestAStandaloneSheetWearsNoTrail(t *testing.T) {
	t.Parallel()
	m := New(Options{Registry: config.NewSettings(config.SettingsOptions{})})
	header := strings.Split(m.Render(80, 20), "\n")[headerLine]
	if !strings.Contains(header, tokens.GlyphScopeUp+" "+TrailWord) {
		t.Fatalf("the standalone header is %q, want it to open with the scope line", header)
	}
	if len(m.steps) != 0 {
		t.Errorf("a standalone sheet recorded %d click targets, want none", len(m.steps))
	}
	if cmd := press(m, "backspace"); cmd != nil {
		t.Error("backspace on a standalone sheet asked to go somewhere")
	}
}

// The drilled header IS the path: every ancestor a door, the current step a
// label, in the palette's own mark so the two surfaces draw one path.
func TestADrilledSheetDrawsThePathAndOnlyTheAncestorIsADoor(t *testing.T) {
	t.Parallel()
	m, asked := drilled(t, "all")
	header := strings.Split(m.Render(80, 20), "\n")[headerLine]

	want := trailLead + "all " + trailLead + TrailWord
	if !strings.Contains(header, want) {
		t.Fatalf("the trail reads %q, want it to contain %q", header, want)
	}
	if len(m.steps) != 1 {
		t.Fatalf("the trail recorded %d click targets, want 1", len(m.steps))
	}
	if at, w := blocks.Width(trailLead), blocks.Width("all"); m.steps[0].at != at || m.steps[0].w != w {
		t.Errorf("the ancestor is recorded at %d+%d, want %d+%d", m.steps[0].at, m.steps[0].w, at, w)
	}

	// The step the reader is standing on opens nothing: clicking where you
	// already are must do nothing rather than rebuild the screen under the
	// pointer.
	current := blocks.Width(trailLead + "all " + trailLead)
	for x := current; x < current+blocks.Width(TrailWord); x++ {
		if _, hit := stepAt(m.steps, x); hit {
			t.Fatalf("the current step is a click target at column %d", x)
		}
	}
	if len(*asked) != 0 {
		t.Errorf("rendering the trail asked to go back: %v", *asked)
	}
}

// The keyboard half of the round trip. Backspace on an empty query is the back
// key; with text in the field it is still an erase, because a key that
// sometimes deleted a letter and sometimes threw away the screen would make
// typing feel dangerous.
func TestBackspaceOnAnEmptyQueryLeavesTheSheet(t *testing.T) {
	t.Parallel()
	m, asked := drilled(t, "all")

	m.setQuery("budget")
	press(m, "backspace")
	if m.Query() != "budge" {
		t.Fatalf("backspace with a query typed left %q, want it to erase a letter", m.Query())
	}
	if len(*asked) != 0 {
		t.Fatalf("backspace threw the screen away mid-search: %v", *asked)
	}

	m.setQuery("")
	press(m, "backspace")
	if len(*asked) != 1 || (*asked)[0] != 0 {
		t.Fatalf("backspace on an empty query asked for %v, want one step back to depth 0", *asked)
	}
}

// The pointer half. A click on the ancestor word is the same door the key is —
// the whole way out of a drilled sheet for a reader with a mouse in their hand.
func TestClickingATrailWordLeavesTheSheet(t *testing.T) {
	t.Parallel()
	m, asked := drilled(t, "all")
	m.Render(80, 20)

	at := m.steps[0].at
	m.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(at, headerLine))
	if len(*asked) != 1 || (*asked)[0] != 0 {
		t.Fatalf("clicking the ancestor asked for %v, want one step back to depth 0", *asked)
	}

	// Chrome on the same line is still chrome: a click past the path does
	// nothing, rather than something arbitrary.
	m.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(70, headerLine))
	if len(*asked) != 1 {
		t.Fatalf("a click on header chrome asked to go back: %v", *asked)
	}
}

// Two ancestors, two doors, each one landing on its own rung — the trail is N
// targets and not one back button, which is the whole reason it is a trail.
func TestEachTrailWordLandsOnItsOwnRung(t *testing.T) {
	t.Parallel()
	m, asked := drilled(t, "all", "somewhere")
	m.Render(80, 20)
	if len(m.steps) != 2 {
		t.Fatalf("the trail recorded %d targets, want 2", len(m.steps))
	}
	for i, step := range m.steps {
		m.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, image.Pt(step.at, headerLine))
		if len(*asked) != i+1 || (*asked)[i] != i {
			t.Fatalf("clicking step %d asked for %v", i, *asked)
		}
	}
}

// ESC IS NOT PART OF THE LADDER. It closes the whole flow from any depth —
// 8.2.21 is that esc acts on what you are watching, and what a reader watching a
// drilled sheet wants gone is the sheet. Two keys with one meaning between them
// is how "press esc until something happens" gets learned.
func TestEscClosesTheWholeFlowFromADrilledSheet(t *testing.T) {
	t.Parallel()
	closed, back := 0, 0
	m := New(Options{
		Registry: config.NewSettings(config.SettingsOptions{}),
		OnClose:  func() tea.Cmd { closed++; return nil },
	})
	m.SetTrail([]string{"all"}, func(int) tea.Cmd { back++; return nil })

	press(m, "esc")
	if closed != 1 || back != 0 {
		t.Fatalf("esc closed %d times and went back %d, want 1 and 0", closed, back)
	}
}

// The mark this surface draws the path with is the palette's own. Two surfaces
// drawing one path with two different marks would be two paths.
func TestTheSheetsTrailMarkIsThePalettesOwn(t *testing.T) {
	t.Parallel()
	if want := tokens.GlyphPromptChat + " "; trailLead != want {
		t.Fatalf("the sheet's trail lead is %q, want the palette's %q", trailLead, want)
	}
}

// The seam the palette's settings rows carried — "the sheet has no
// select-this-key door" — closed. Picking a setting takes you TO it.
func TestSelectPutsTheBandOnTheRowThatWasAskedFor(t *testing.T) {
	t.Parallel()
	m := New(Options{Registry: config.NewSettings(config.SettingsOptions{})})
	if len(m.visible) < 2 {
		t.Skip("this registry has too few rows to select between")
	}
	want := m.rows[m.visible[len(m.visible)-1]].setting.Key

	if !m.Select(want) {
		t.Fatalf("the sheet refused to select %q", want)
	}
	got, ok := m.Selected()
	if !ok || got != want {
		t.Fatalf("the band is on %q (ok=%v), want %q", got, ok, want)
	}
	if m.Select("no.such.key") {
		t.Error("the sheet claimed to select a key it does not have")
	}
	if got, _ := m.Selected(); got != want {
		t.Errorf("a refused select moved the band to %q", got)
	}
}
