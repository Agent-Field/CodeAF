package tui3

import (
	"strings"
	"testing"
)

// ── alt+1…8 IS THE ONE CLASS THAT BELONGS TO NO PLACE ───────────────────────
//
// placeeveryone_test.go asks the numbers of all seven ROOMS. Nothing asked them
// of the surface a person spends most of their time on: the conversation. And
// the manual promises them there in as many words — home.md says `alt+1` "goes
// straight there from anywhere", places.md tables the seven as "jump straight to
// a place" — so a chord that did nothing at all on the conversation was the
// program contradicting its own account of itself.

// conversationApp is a surface with the places available and NOTHING drawn over
// the transcript. There is no page id for that state — [app.page] is a label on
// whichever place was last standing — so what says it is [app.pageShowing],
// which asks the places' own `open` flags.
func conversationApp(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	a.closeHome()
	if a.pageShowing() {
		t.Fatalf("esc left the %s place standing over the conversation", a.page.word())
	}
	return a
}

// THE NUMBERS OPEN THEIR ROOM FROM THE CONVERSATION, and EVERY room opens.
//
// This used to ask a `pageReady` first and accept a refusal for the three places
// that could give one. Nothing refuses any more (pages.go's [app.showPage]), so
// the assertion is the whole of the law: seven digits, seven rooms, from the
// surface a person is most often on.
func TestTheNumbersJumpFromTheConversation(t *testing.T) {
	for _, id := range pages() {
		at := placeDigitOf(id) - 1
		t.Run(id.word(), func(t *testing.T) {
			a := conversationApp(t)
			drive(t, a, key("alt+"+string(rune('1'+at))))
			if !a.pageShowing() || a.page != id {
				t.Fatalf("alt+%d on the conversation drew nothing: the router says %q, showing=%v",
					at+1, a.page.word(), a.pageShowing())
			}
			// AND NOTHING IS WRITTEN INTO THE CONVERSATION ON THE WAY. A refusal
			// used to land there; a place that opens has nothing to say about it.
			if strings.TrimSpace(a.pageMsg) != "" {
				t.Fatalf("alt+%d opened the %s place and said %q anyway", at+1, id.word(), a.pageMsg)
			}
		})
	}
}

// ── `→` AND `←`, ON ALL SEVEN ───────────────────────────────────────────────

// `→` OPENS THE ROW'S VERBS AND `←` PUTS THEM AWAY (SCREEN 3c). The owner tried
// the plain arrows for the tab bar and reported that "only shift moves tabs",
// which is worth auditing rather than assuming: the answer the design gives is
// that neither arrow is a tab key, and what `→` is instead is the strip.
//
// A place whose cursor is on a row with no verbs is not a failure of this law —
// SCREEN 3c is explicit that the verbs are the row's own, so a row that can be
// acted on in no way has no strip — which is why a place that offers none is
// only required not to invent one.
func TestTheRightArrowOpensTheRowsVerbsOnEveryPlace(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			placeFrameText(a)
			if a.strip.open {
				t.Fatalf("the %s place opened with a verb strip already up", place.id.word())
			}
			from := a.home.columnOf(a.home.cursor)
			drive(t, a, key("right"))
			if a.page != place.id {
				t.Fatalf("`→` on the %s place moved to %q", place.id.word(), a.page.word())
			}
			// ON HOME'S GRID OF COLUMNS `→` IS THE NEXT COLUMN where one has a row
			// to stand on, and the strip is `→` where none does (homegrid.go's
			// [app.homeGridCross]; pages_test.go pins that end).
			if place.id == pageHome && a.home.gridOn() && a.home.columnOf(a.home.cursor) == from+1 {
				if a.strip.open {
					t.Fatal("`→` crossed home's columns and opened a strip as well")
				}
				return
			}
			if !a.strip.open {
				if len(a.rowVerbs()) > 0 {
					t.Fatalf("the row under the cursor has %d verbs and `→` drew no strip", len(a.rowVerbs()))
				}
				return
			}
			// AND `←` IS THE WAY BACK OUT OF IT, leaving the cursor exactly where
			// the strip found it.
			cursor := place.cursor(a)
			drive(t, a, key("left"))
			if a.strip.open {
				t.Fatalf("`←` left the %s place's verb strip standing", place.id.word())
			}
			if got := place.cursor(a); got != cursor {
				t.Fatalf("closing the strip moved the %s place's cursor from %d to %d",
					place.id.word(), cursor, got)
			}
			if a.page != place.id {
				t.Fatalf("`←` on the %s place moved to %q", place.id.word(), a.page.word())
			}
		})
	}
}

// AND NEITHER ARROW IS A TAB KEY, ON ANY OF THE SEVEN. This is the audit the
// owner's "left right does not seem to move tabs" asks for, written as the law
// it protects: `tab`/`shift+tab` walk the rooms, `shift+←→↑↓` are the time
// window (SCREEN 3d), and the plain arrows keep whatever they already meant on
// the place a person is standing on.
func TestThePlainArrowsSwitchNoPlaceAnywhere(t *testing.T) {
	for _, place := range everyPlaceTable() {
		t.Run(place.id.word(), func(t *testing.T) {
			a := place.open(t)
			for _, k := range []string{"left", "right", "up", "down"} {
				drive(t, a, key(k))
				if a.page != place.id {
					t.Fatalf("%s moved from the %s place to %q", k, place.id.word(), a.page.word())
				}
			}
		})
	}
}
