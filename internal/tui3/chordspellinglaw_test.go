package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ON A MAC, NOTHING THIS SURFACE DRAWS MAY SAY `alt+`.
//
// chords.go's whole argument is that a chord has ONE spelling per keyboard and
// that [chordSpelling.say] is the one door every person-facing sentence about
// one goes through: the constants are authored `alt+`, the manual quotes them
// that way, and a Mac's `opt+` is substituted in once, at the moment of drawing.
// The comment there says a table of chords would rot the first time a lane added
// one without touching it — and what actually rotted was the other half, the
// SENTENCES. Three lines were built straight out of their constants and never
// passed through the door:
//
//   - home's rule, which said `alt+w folder · alt+o model` on a machine whose
//     memory place said `opt+s` and whose map said `opt+1…opt+7` (homedraft.go);
//   - the question chip on the status line, `alt+y` (question.go);
//   - the switcher's own door on the legend, the day it moved onto this
//     modifier (render.go's [hopDoorWord]).
//
// So a person moving between two screens one keystroke apart was shown one
// modifier under two names and had no way to know it was one key. That is the
// question this law was written from, asked by the owner looking at the two
// spellings side by side.
//
// IT IS A FRAME TEST AND NOT A SOURCE TEST ON PURPOSE. What is wrong is never
// the constant — every one of them is SUPPOSED to say `alt+` — it is the path
// from the constant to the paint, and the only thing that can see the whole of
// that path is the paint itself. So this renders the surfaces on a Mac's
// spelling and reads what came out.
func TestNothingDrawnOnAMacSpellsTheModifierAsAlt(t *testing.T) {
	seen := 0
	check := func(where, frame string) {
		t.Helper()
		frame = ansi.Strip(frame)
		if strings.Contains(frame, chordMetaWord) {
			seen++
		}
		if !strings.Contains(frame, chordAltWord) {
			return
		}
		for _, line := range strings.Split(frame, "\n") {
			if strings.Contains(line, chordAltWord) {
				t.Errorf("%s draws %q where a Mac spells it %q:\n  %s",
					where, chordAltWord, chordMetaWord, strings.TrimSpace(line))
			}
		}
	}

	// THE CONVERSATION, with the switcher's door on the legend — which needs
	// somewhere to go, or the clause is rightly absent ([app.hopAvailable]).
	a := macApp(t)
	a.hopKnown = 3
	check("the conversation", frame(a))

	// THE KEY SHEET, which is the one screen whose whole job is naming chords.
	check("the key sheet", helpText("", chordSpelling{meta: chordMetaWord}))

	// AND EVERY PLACE, each with its own foot, plus the map over one — the line
	// whose job is to say what the keys are (pages.go's [app.placeHintSaid]).
	for _, at := range placeOrder {
		a = macApp(t)
		a.hopKnown = 3
		runCmd(a.showPage(at))
		check("the "+at.word()+" place", frame(a))
	}
	a = macApp(t)
	a.hopKnown = 3
	runCmd(a.showPage(pageTasks))
	a.mapShowing = true
	check("the key map", frame(a))

	// AND THE LAW PROVES ITS OWN EYES. A frame that contains NEITHER spelling is
	// a frame this test cannot speak about, and a run where that were true of
	// every surface would be green for the wrong reason forever.
	if seen == 0 {
		t.Fatal("no surface drew a chord at all; this law has stopped seeing the thing it reads")
	}
}

// macApp is the suite's app on a Mac's chord spelling, which is the one fact
// this law changes about it. Everything else — the pinned terminal, palette and
// glyph tier — is [newTestApp]'s, because a law about spelling must not also be
// a law about a font.
func macApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 120, 30
	a.chords = chordSpelling{meta: chordMetaWord, terminal: "Terminal", setting: "Profiles → Keyboard"}
	return a
}
