package tui3

import (
	"strings"
	"testing"
)

// ── alt+1…7 IS THE ONE CLASS THAT BELONGS TO NO PLACE ───────────────────────
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
	drive(t, a, key("esc"))
	if a.pageShowing() {
		t.Fatalf("esc left the %s place standing over the conversation", a.page.word())
	}
	return a
}

// THE NUMBERS OPEN THEIR ROOM FROM THE CONVERSATION. Every place that opens on
// this machine is reachable by its own digit without first going somewhere else
// to press it.
func TestTheNumbersJumpFromTheConversation(t *testing.T) {
	for at, id := range pages() {
		t.Run(id.word(), func(t *testing.T) {
			a := conversationApp(t)
			ready := a.pageReady(id)
			drive(t, a, key("alt+"+string(rune('1'+at))))
			if ready {
				if !a.pageShowing() || a.page != id {
					t.Fatalf("alt+%d on the conversation drew nothing: the router says %q, showing=%v",
						at+1, a.page.word(), a.pageShowing())
				}
				return
			}
			// A ROOM WITH NOTHING IN IT SAYS SO, in a sentence — the refusal is
			// the router's whole answer to a door that will not open, and a
			// silent one is what the owner met.
			if a.pageShowing() {
				t.Fatalf("alt+%d opened the %s place, which has nothing to open onto", at+1, id.word())
			}
			if !pageRefusalSaid(a) {
				t.Fatalf("alt+%d on the conversation refused the %s place without saying why", at+1, id.word())
			}
		})
	}
}

// pageRefusalSaid is whether the surface said anything back. A refusal on the
// conversation's road is a note in the transcript, which is where a person is
// looking; on a place's road it is the router's own line (pages.go's
// [app.refusePage] holds both halves).
func pageRefusalSaid(a *app) bool {
	if strings.TrimSpace(a.pageMsg) != "" {
		return true
	}
	for _, written := range a.entries {
		if written.kind == entryNote && strings.TrimSpace(written.text) != "" {
			return true
		}
	}
	return false
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
			drive(t, a, key("right"))
			if a.page != place.id {
				t.Fatalf("`→` on the %s place moved to %q", place.id.word(), a.page.word())
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
