package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// drawnAnswerStrip is the strip as the FRAME drew it: the row between the box
// and the hint, and the empty string when that row is not a strip at all.
//
// IT HAS TO TELL THE STRIP FROM THE BOX ROW NOW, AND IT DID NOT HAVE TO BEFORE.
// Home's box used to say [homeFootWord] — `type to search or start something
// new …` — so `› ` at the head of that row could only ever be the strip's own
// lead. The design's box row is the same sentence on every place, home included
// (SCREEN 2b, FIDELITY.md item 3), so home's box now says `› say what you want
// done` and a bare prefix test answers true on a home with nothing to answer.
// The row is therefore told apart by the box's own resting sentence, which is
// the one thing that can stand in that slot and is not a strip
// (pages.go's [app.placeRestWord] draws it, [app.placeStrip] draws this).
func drawnAnswerStrip(a *app) string {
	lines, _, _, _ := a.homeFrame(a.width, a.height)
	if len(lines) < 2 {
		return ""
	}
	plain := strings.TrimSpace(ansi.Strip(lines[len(lines)-2]))
	if !strings.HasPrefix(plain, "› ") || strings.Contains(plain, placeRestWord) {
		return ""
	}
	return plain
}

func TestAnswerStripShowsTheCursorRowsAnswerableCard(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "approve schema change — alter users table, add sso columns"), time.Now())
	strip := drawnAnswerStrip(lab.a)
	if strip == "" {
		t.Fatalf("home drew no answer strip:\n%s", homeText(lab.a))
	}
	for _, want := range []string{"approve schema change", "1 allow once", "2 always", "3 deny"} {
		if !strings.Contains(strip, want) {
			t.Fatalf("the answer strip lost %q: %q", want, strip)
		}
	}
}

// THE STRIP IS AN ANSWER, NOT A MIRROR: a cursor row with nothing to answer
// draws no strip at all. An always-on preview row moved home's geometry on
// every frame and broke the pad and gutter laws for a line the list already
// said.
func TestAnswerStripDrawsNothingWithoutAnAnswer(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "approve schema change"), time.Now())
	lab.a.home.point(lab.a.file)
	if strip := drawnAnswerStrip(lab.a); strip != "" {
		t.Fatalf("a row with nothing to answer drew a strip: %q\n%s", strip, homeText(lab.a))
	}
}

func TestAnswerStripDrawsNothingWhenHomeIsIdle(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "approve schema change"), time.Now())
	lab.a.home.cursor, lab.a.home.hover = -1, -1
	if strip := drawnAnswerStrip(lab.a); strip != "" {
		t.Fatalf("idle home drew an answer strip: %q\n%s", strip, homeText(lab.a))
	}
}

func TestAnswerStripDigitsUseTheExistingAnswerRoad(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "approve schema change"), time.Now())
	lab.a.homeKey(key("2"))
	if len(*lab.sent) != 1 || (*lab.sent)[0].key != "2" || (*lab.sent)[0].dir != lab.dir {
		t.Fatalf("the strip's digit left answers %+v, want key 2 on the cursor row", *lab.sent)
	}
	if typed := lab.a.home.box.String(); typed != "" {
		t.Fatalf("the answered digit also typed %q", typed)
	}
}
