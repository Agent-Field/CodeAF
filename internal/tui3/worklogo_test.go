package tui3

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
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

func workLogoRowTexts(rows []row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

func workingActivityRows(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.activity {
			out = append(out, i)
		}
	}
	return out
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
	first := a.activityRows(a.workActivity, "", 90)
	a.clock = func() time.Time { return began.Add(700 * time.Millisecond) }
	second := a.activityRows(a.workActivity, "", 90)
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
	rows, _ := a.deckRows(a.conversation(), 90)
	found := 0
	for _, r := range rows {
		if r.activity && strings.Contains(ansi.Strip(r.text), a.workActivity.Caption()) {
			found++
		}
	}
	if found != 1 {
		t.Fatal("question does not have one activity line")
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
		func(a *app) { a.height = 16 },
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
	for _, r := range a.activityRows(a.workActivity, "", 48) {
		if r.entry != -1 || r.hit != 0 {
			t.Fatal("animation entered the selectable transcript")
		}
		if strings.Contains(r.text, "38;2;") {
			t.Fatal("256-color terminal received truecolor")
		}
	}
}

// C1 says a copy snapshot is the exact page without the transient working row
// or the blank that row alone introduced, and thawing restores live movement.
func TestCopyModeFreezesThePageWithoutTheWorkingLogo(t *testing.T) {
	a := workLogoApp(t)
	width := a.bodyWidth()
	if got := workingActivityRows(a.visible(width)); len(got) != 1 {
		t.Fatalf("live page has activity rows %v, want exactly one", got)
	}
	caption := a.workActivity.Caption()

	wantApp := workLogoApp(t)
	wantApp.workActivity = tokens.WorkActivity{}
	want := workLogoRowTexts(wantApp.layout(width))

	a.enterCopy()
	if !a.copy.on {
		t.Fatal("copy mode did not open")
	}
	plainCopy := ansi.Strip(strings.Join(a.copy.rows, "\n"))
	if strings.Contains(plainCopy, caption) {
		t.Fatalf("copy snapshot retained the activity caption %q", caption)
	}
	if strings.ContainsAny(plainCopy, "●•·˙") {
		t.Fatalf("copy snapshot retained a working-logo mark:\n%s", plainCopy)
	}
	if !slices.Equal(a.copy.rows, want) {
		t.Fatalf("copy snapshot differs from the page before activity:\n got %q\nwant %q", a.copy.rows, want)
	}

	a.exitCopy()
	if got := workingActivityRows(a.visible(width)); len(got) != 1 {
		t.Fatalf("thawed page has activity rows %v, want exactly one", got)
	}
}

// C2 gives task and adaptive-run pages the same copy law through their shared
// room freeze door.
func TestCopyModeFreezesWorkPagesWithoutTheWorkingLogo(t *testing.T) {
	for _, tc := range []struct {
		name string
		app  func(*testing.T) *app
	}{
		{name: "task", app: func(t *testing.T) *app {
			a, _ := roomModelApp(t, "task-model")
			a.width, a.height = 100, 40
			a.pal = newPalette(tokens.TrueColor, false)
			a.room.entries = []entry{{kind: entryUser, text: "Ship the parser fix", turn: 1}}
			a.room.turn, a.room.readingRestored, a.room.dirty = 1, true, true
			return a
		}},
		{name: "adaptive run", app: func(t *testing.T) *app {
			a, _ := orchApp(t, orchestrate.Snapshot{})
			a.width, a.height = 100, 40
			a.pal = newPalette(tokens.TrueColor, false)
			a.orchOf().known = true
			a.room.dirty = true
			return a
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.app(t)
			width := a.bodyWidth()
			activity := a.room.workActivity
			a.room.workActivity = tokens.WorkActivity{}
			a.room.dirty = true
			want := workLogoRowTexts(a.roomRows(width))
			a.room.workActivity = activity
			a.room.dirty = true
			if got := workingActivityRows(a.roomRows(width)); len(got) != 1 {
				t.Fatalf("live page has activity rows %v, want exactly one", got)
			}

			a.freezeRoom()
			if !a.copy.on {
				t.Fatal("copy mode did not open")
			}
			if !slices.Equal(a.copy.rows, want) {
				t.Fatalf("copy snapshot differs from the page before activity:\n got %q\nwant %q", a.copy.rows, want)
			}

			a.exitCopy()
			if got := workingActivityRows(a.roomRows(width)); len(got) != 1 {
				t.Fatalf("thawed page has activity rows %v, want exactly one", got)
			}
		})
	}
}

// C3 keeps adopted or self-started work on the compact waiting treatment until
// its running turn has a question of its own to anchor.
func TestAWorkingTurnWithoutItsOwnQuestionKeepsTheWaitingText(t *testing.T) {
	a := workLogoApp(t)
	a.entries = []entry{
		{kind: entryUser, text: "The answered question", turn: 1},
		{kind: entryAssistant, text: "The earlier answer", turn: 1},
	}
	a.turn = 2
	a.turnBegan = time.Time{}
	a.workActivity = tokens.WorkActivity{}
	a.startClock()
	began := a.turnBegan
	a.clock = func() time.Time { return began.Add(11 * time.Second) }

	if a.workLogoVisible() {
		t.Fatal("a turn without its own question borrowed the previous turn's logo anchor")
	}
	rows, _ := a.deckRows(a.conversation(), 90)
	if got := workingActivityRows(rows); len(got) != 0 {
		t.Fatalf("a turn without its own question drew activity rows %v", got)
	}
	if tail, inline := a.compactWaitSuffix("answer", 90, a.conversation()); !inline || !strings.Contains(ansi.Strip(tail), "still working · 11s") {
		t.Fatalf("compact waiting was suppressed: inline=%v tail=%q", inline, ansi.Strip(tail))
	}

	a.entries = append(a.entries, entry{kind: entryUser, text: "The running turn's question", turn: 2})
	if !a.workLogoVisible() {
		t.Fatal("the running turn's own question did not admit the logo")
	}
	rows, _ = a.deckRows(a.conversation(), 90)
	activity := workingActivityRows(rows)
	if len(activity) != 1 {
		t.Fatalf("running turn has activity rows %v, want exactly one", activity)
	}
	lastQuestion := -1
	for i, r := range rows {
		if r.entry == len(a.entries)-1 {
			lastQuestion = i
		}
	}
	if lastQuestion < 0 || activity[0] != lastQuestion+2 || strings.TrimSpace(ansi.Strip(rows[lastQuestion+1].text)) != "" {
		t.Fatalf("activity row %d did not follow the question row %d and its gap", activity[0], lastQuestion)
	}
}

// C4 keeps one row under the latest correction when the running turn has both
// its original question and a later steer.
func TestTheWorkingLogoFollowsTheCurrentTurnsLatestSteer(t *testing.T) {
	a := workLogoApp(t)
	a.turn = 2
	a.entries = []entry{
		{kind: entryUser, text: "Inspect the parser", turn: 2},
		{kind: entrySteer, turn: 2, steer: &steerElbow{words: "Also inspect its tests", consumed: true}},
	}
	rows, _ := a.deckRows(a.conversation(), 90)
	activity := workingActivityRows(rows)
	if len(activity) != 1 {
		t.Fatalf("steered turn has activity rows %v, want exactly one", activity)
	}
	lastSteer := -1
	for i, r := range rows {
		if r.entry == 1 {
			lastSteer = i
		}
	}
	if lastSteer < 0 || activity[0] != lastSteer+2 {
		t.Fatalf("activity row %d did not follow the steer row %d and its gap", activity[0], lastSteer)
	}
}

// C5 keeps a task's original request as its anchor across later retry turn
// numbers, because that request remains the work the page is carrying out.
func TestATaskRetryKeepsTheWorkingLogoUnderItsRequest(t *testing.T) {
	a, _ := roomModelApp(t, "task-model")
	a.width, a.height = 100, 40
	a.pal = newPalette(tokens.TrueColor, false)
	a.room.entries = []entry{{kind: entryUser, text: "Ship the parser fix", turn: 1}}
	a.room.turn, a.room.readingRestored, a.room.dirty = 3, true, true

	_, anchor, visible := a.questionActivity(a.room.deck())
	if !visible || anchor != 0 {
		t.Fatalf("retry activity anchor = %d, visible=%v; want the turn-1 request", anchor, visible)
	}
	rows, _ := a.deckRows(a.room.deck(), 90)
	activity := workingActivityRows(rows)
	lastRequest := -1
	for i, r := range rows {
		if r.entry == 0 {
			lastRequest = i
		}
	}
	if len(activity) != 1 || lastRequest < 0 || activity[0] != lastRequest+2 {
		t.Fatalf("retry activity rows %v did not follow request row %d and its gap", activity, lastRequest)
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
	if len(a.activityRows(a.room.workActivity, "", 80)) != tokens.WorkLogoHeight {
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

func TestWorkingLogoQuestionAnchorReservesColumnsAcrossEveryPose(t *testing.T) {
	a := workLogoApp(t)
	a.entries[0].text = strings.Repeat("A long question that wraps. ", 6)
	began := a.now()
	anchor := -1
	chromeHeight := a.chromeBaseHeight()
	for style := 0; style < tokens.WorkLogoCount; style++ {
		a.workActivity.Start(began, style)
		for i := 0; i < 28; i++ {
			a.clock = func() time.Time { return began.Add(time.Duration(i) * 100 * time.Millisecond) }
			rows, _ := a.deckRows(a.conversation(), 60)
			at := -1
			lastQuestion := -1
			for j, r := range rows {
				if r.entry == 0 {
					lastQuestion = j
				}
				if r.activity {
					at = j
				}
			}
			if at < 0 || at != lastQuestion+2 {
				t.Fatal("indicator is not after the complete wrapped question")
			}
			if anchor < 0 {
				anchor = at
			}
			if at != anchor || a.chromeBaseHeight() != chromeHeight {
				t.Fatal("motion moved its anchor or added input chrome")
			}
			text := ansi.Strip(rows[at].text)
			prefix := strings.SplitN(text, tokens.DecodeWorkCaption(a.workActivity.Caption(), a.workActivity.Elapsed(a.now())), 2)
			if len(prefix) != 2 || ansi.StringWidth(prefix[0]) != activityLabelColumn || strings.TrimSpace(prefix[1]) != "" {
				t.Fatalf("unstable label: %q", text)
			}
		}
	}
	a.entries = append(a.entries, entry{kind: entryAssistant, text: strings.Repeat("The answer grows. ", 50), turn: 1})
	rows, _ := a.deckRows(a.conversation(), 60)
	if !rows[anchor].activity {
		t.Fatal("streaming moved the indicator")
	}
	a.state = stateIdle
	_, _, visible := a.questionActivity(a.conversation())
	if visible {
		t.Fatal("completed question still animates")
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

func TestWorkingLogoAdaptiveRunUsesItsOwnState(t *testing.T) {
	a, _ := orchApp(t, orchestrate.Snapshot{})
	a.width, a.height = 100, 40
	a.pal = newPalette(tokens.TrueColor, false)
	run := a.orchOf()
	run.known = true
	if !a.roomWorkLogoVisible() {
		t.Fatal("active run has no indicator")
	}
	rows := a.orchRows(90)
	if len(rows) == 0 || !rows[0].activity {
		t.Fatal("run indicator is not below the goal header")
	}
	run.snap.Paused = true
	if a.roomWorkLogoVisible() {
		t.Fatal("paused run animates")
	}
	run.snap.Paused = false
	run.snap.Done = true
	if a.roomWorkLogoVisible() {
		t.Fatal("finished run animates")
	}
}

func TestWorkingCaptionDecodeKeepsFollowingContentStill(t *testing.T) {
	a := workLogoApp(t)
	start := a.now()
	caption := a.workActivity.Caption()
	frames := map[string]bool{}
	for i := 0; i < 240; i++ {
		elapsed := time.Duration(i) * 50 * time.Millisecond
		a.clock = func() time.Time { return start.Add(elapsed) }
		stripped := ansi.Strip(a.activityRows(a.workActivity, "next", 90)[0].text)
		if at := strings.Index(stripped, "next"); at < 0 || ansi.StringWidth(stripped[:at]) != activityContentColumn {
			t.Fatal("following content moved")
		}
		decoded := tokens.DecodeWorkCaption(a.workActivity.CaptionAt(a.now()), elapsed%tokens.WorkCaptionPeriod)
		if !strings.Contains(stripped, decoded) {
			t.Fatal("shared decoding frame missing")
		}
		frames[decoded] = true
		if a.workActivity.Caption() != caption {
			t.Fatal("operation caption changed")
		}
	}
	if len(frames) < 2 {
		t.Fatal("caption never decodes")
	}
	a.pal.ascii = true
	a.clock = func() time.Time { return start.Add(1800 * time.Millisecond) }
	if !strings.Contains(ansi.Strip(a.activityRows(a.workActivity, "", 90)[0].text), caption) {
		t.Fatal("accessible caption must stay readable")
	}
}

// The ball's gold must stay legible on whichever page the ladder is written
// for, including a light ladder put there by a theme setting.
func TestTheWorkingBallIsDeeperGoldOnALightPage(t *testing.T) {
	dark := newPalette(tokens.TrueColor, false)
	light := newThemedPalette(tokens.TrueColor, false, themeLight, func(string) string { return "" })
	ball := tokens.WorkLogoCell{Glyph: '●', Gold: true}
	if got := dark.workLogoCell(ball); got != dark.paint("●", hueWorkGold) {
		t.Fatalf("dark page ball = %q, want the bright gold", got)
	}
	if got := light.workLogoCell(ball); got != light.paint("●", lightWorkGold) {
		t.Fatalf("light page ball = %q, want the deeper gold", got)
	}
	white := luminanceOf(255, 255, 255)
	if r := contrastOn(lightWorkGold, white); r < 4.5 {
		t.Fatalf("light gold contrast on white = %.2f, want at least 4.5", r)
	}
}
