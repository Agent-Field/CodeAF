package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

func workLogoApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "test-model"})
	a.width, a.height = 100, 40
	a.pal = newPalette(tokens.TrueColor, false)
	a.clock = func() time.Time { return time.Unix(1000, 0) }
	a.state = stateWorking
	a.startClock()
	a.entries = []entry{{kind: entryUser, text: "Please inspect this project", turn: 1}}
	a.turn = 1
	return a
}

func TestWorkingLogoFollowsChatAndKeepsItsChoice(t *testing.T) {
	a := workLogoApp(t)
	// Pick a known moving interval; other studies deliberately hold at contact.
	a.workActivity.Start(a.now(), tokens.WorkLogoRally)
	if !a.workLogoVisible() {
		t.Fatal("submitted turn has no logo")
	}
	style := a.workActivity.Style()
	began := a.turnBegan
	first := a.workLogoRows(90, "")
	a.clock = func() time.Time { return began.Add(700 * time.Millisecond) }
	second := a.workLogoRows(90, "")
	if first[0].text == second[0].text {
		t.Fatal("working frame did not advance")
	}
	a.startClock()
	if a.workActivity.Style() != style {
		t.Fatal("steering reselected the logo")
	}
	a.live = 1
	a.entries = append(a.entries, entry{kind: entryAssistant, text: "Here is the answer", turn: 1})
	a.lastDelta = a.now()
	if !a.workLogoVisible() {
		t.Fatal("streaming reply lost the logo")
	}
	rows, marks, _, _ := a.chrome(90)
	found := 0
	for i, r := range rows {
		if marks[i].kind == chromeActivity && strings.Contains(ansi.Strip(r), "Working") {
			found++
		}
		if ansi.StringWidth(r) > 90 {
			t.Fatal("dock overflows")
		}
	}
	if found != 1 {
		t.Fatal("chrome did not emit one activity dock")
	}
	a.state = stateIdle
	if a.workLogoVisible() {
		t.Fatal("finished turn still animates")
	}
	a.turnBegan = time.Time{}
	a.state = stateWorking
	a.startClock()
	if a.workActivity.Style() == style {
		t.Fatal("new turn repeated the prior study")
	}
}

func TestWorkingLogoFallbacksAndTransientRows(t *testing.T) {
	for _, change := range []func(*app){
		func(a *app) { a.linear = true }, func(a *app) { a.pal.ascii = true },
		func(a *app) { a.pal = newPalette(tokens.NoColor, false) }, func(a *app) { a.width = 40 },
		func(a *app) { a.height = 16 }, func(a *app) { a.copy.on = true },
		func(a *app) { a.state = stateInterrupted }, func(a *app) { a.page = pageHome },
	} {
		a := workLogoApp(t)
		change(a)
		if a.workLogoVisible() {
			t.Fatal("unsupported or inactive view animates")
		}
	}
	a := workLogoApp(t)
	a.pal = newPalette(tokens.ANSI256, false)
	for _, r := range a.workLogoRows(48, "") {
		if r.entry != -1 || r.hit != 0 {
			t.Fatal("animation entered the selectable transcript")
		}
		if strings.Contains(r.text, "38;2;") {
			t.Fatal("256-color terminal received truecolor")
		}
	}
}

func TestWorkingLogoUsesTasksOwnState(t *testing.T) {
	a, _ := roomModelApp(t, "task-model")
	a.width, a.height = 100, 40
	a.pal = newPalette(tokens.TrueColor, false)
	a.state = stateIdle
	if !a.roomWorkLogoVisible() {
		t.Fatal("running task depends on parent chat's state")
	}
	choice := a.room.workActivity.Style()
	a.state = stateWorking
	a.startClock()
	if a.room.workActivity.Style() != choice {
		t.Fatal("chat turn changed task animation")
	}
	if len(a.roomWorkLogoRows(80)) != tokens.WorkLogoHeight {
		t.Fatal("task does not use shared layout")
	}
	node := a.roomNode()
	node.paused = true
	if a.roomWorkLogoVisible() {
		t.Fatal("fuel-paused task animates")
	}
	node.paused = false
	for _, state := range []session.TaskState{session.TaskQueued, session.TaskDone, session.TaskFailed, session.TaskUnverified} {
		node.state = state
		if a.roomWorkLogoVisible() {
			t.Fatalf("%s task animates", state)
		}
	}
	node.state = session.TaskRunning
	a.room.readFailed = true
	if a.roomWorkLogoVisible() {
		t.Fatal("disconnected task claims progress")
	}
}

func TestWorkingLogoStopsForQuestions(t *testing.T) {
	a := workLogoApp(t)
	a.questions = append(a.questions, questionShown{question: session.Question{ID: 12, Head: "Which branch?"}})
	if a.workLogoVisible() {
		t.Fatal("chat question still advertises progress")
	}
	r, _ := roomModelApp(t, "task-model")
	r.width, r.height = 100, 40
	r.pal = newPalette(tokens.TrueColor, false)
	r.questions = append(r.questions, questionShown{question: session.Question{ID: 13, Head: "May I continue?"}})
	if r.roomWorkLogoVisible() {
		t.Fatal("task question still advertises progress")
	}
}

func TestWorkingLogoDockReservesGeometryAcrossEveryPose(t *testing.T) {
	a := workLogoApp(t)
	began := a.now()
	dockAt, caretAt := -1, -1
	for style := 0; style < tokens.WorkLogoCount; style++ {
		a.workActivity.Start(began, style)
		for i := 0; i < 28; i++ {
			a.clock = func() time.Time { return began.Add(time.Duration(i) * 100 * time.Millisecond) }
			rows, marks, _, caret := a.chrome(100)
			at := -1
			for j, mark := range marks {
				if mark.kind == chromeActivity {
					at = j
				}
			}
			if at < 0 {
				t.Fatal("missing pinned slot")
			}
			if dockAt < 0 {
				dockAt, caretAt = at, caret
			}
			if at != dockAt || caret != caretAt {
				t.Fatal("motion moved the dock or input")
			}
			text := ansi.Strip(rows[at])
			prefix := strings.SplitN(text, "Working", 2)
			if len(prefix) != 2 || ansi.StringWidth(prefix[0]) != activityLabelColumn || prefix[1] != "" {
				t.Fatalf("unstable label: %q", text)
			}
			if got := ansi.StringWidth(a.activityMark(a.workActivity)); got != tokens.WorkLogoWidth {
				t.Fatalf("slot is %d columns", got)
			}
		}
	}
	before := a.chromeBaseHeight()
	a.state = stateIdle
	rows, marks, _, caret := a.chrome(100)
	if a.chromeBaseHeight() != before || caret != caretAt || marks[dockAt].kind != chromeActivity || rows[dockAt] != "" {
		t.Fatal("finishing changed reserved geometry")
	}
}

func TestWorkingLogoHeaderHasStaticBrandMark(t *testing.T) {
	p := newPalette(tokens.TrueColor, false)
	if got := ansi.Strip(p.wordmark(80)); got != ">● codeaf" {
		t.Fatalf("wordmark: %q", got)
	}
	if ansi.StringWidth(p.wordmark(80)) != 9 {
		t.Fatal("wordmark shifts header geometry")
	}
	p.ascii = true
	if got := ansi.Strip(p.wordmark(80)); got != product {
		t.Fatalf("ASCII fallback: %q", got)
	}
}
