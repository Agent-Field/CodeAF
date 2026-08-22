package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func drawnAnswerStrip(a *app) string {
	lines, _, _, _ := a.homeFrame(a.width, a.height)
	if len(lines) < 2 {
		return ""
	}
	plain := strings.TrimSpace(ansi.Strip(lines[len(lines)-2]))
	if strings.HasPrefix(plain, "› ") {
		return plain
	}
	return ""
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

func TestAnswerStripFallsBackToThePreview(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "approve schema change"), time.Now())
	lab.a.home.point(lab.a.file)
	strip := drawnAnswerStrip(lab.a)
	if !strings.Contains(strings.ToLower(strip), "porting the resume picker") {
		t.Fatalf("the strip did not fall back to the preview row: %q\n%s", strip, homeText(lab.a))
	}
	if strings.Contains(strip, "1 allow once") {
		t.Fatalf("the preview fallback kept another row's answers: %q", strip)
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
