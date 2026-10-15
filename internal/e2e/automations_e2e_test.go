//go:build e2e

package e2e

// automations_e2e_test.go is AUTOMATIONS ON A REAL SCREEN: the real binary, a
// real terminal, the real card, the real store and the real clock process
// (docs/design/automations/DESIGN.md).
//
// IT REPLACED THREE SUBTESTS OF [TestTUIE2E] THAT WERE ABOUT STANDING ORDERS,
// and it says which. `ask here` still exists, and an errand that asks for a
// reminder now ends on an automation card (ask_here_end_to_end); a reminder now
// runs from the clock a window keeps, not from a five-minute operating-system
// timer (the_firing_reaches_the_person); and a narrow window still stacks the
// exchange over the list (narrow_window_ask_here). Each of those waited for
// standing words, and each is a subtest here that waits for the words the
// surface says now.
//
// IT NEEDS NO KEY AND SPENDS NOTHING, which is what makes it a gate rather than
// a drive. The one stand-in is the model: an errand's model is scripted to call
// the `automation` tool with exactly one proposal (the [automationBrain] below),
// because no real model can be asked to propose the same reminder twice — and a
// reminder, once saved, needs no model at all: the clock says its line itself.
// Everything between the keystroke and the screen is the product.
//
//	go test -tags e2e -count=1 -run TestAutomationsE2E -v ./internal/e2e/

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/config"
)

// The scripted model's own words. They are the SCENARIO's and not the
// surface's — what an automation is called and what it says are the model's
// to choose, and the brain below chooses these — so they are spelled here and
// not in tuiwords_test.go, whose table is a gate on what internal/tui3 spells.
const (
	automationStubModel = "stub/automations"
	// autoAskWords is what the person types on home, verbatim.
	autoAskWords = "remind me in 10 minutes to drink water"
	// autoAskRow is how the exchange's row on home begins: the ask-here mark
	// and the person's own words (homeexchange.go's exchangeRowLine).
	autoAskRow = "? remind me in 10 minutes"
	// autoTitle and autoSay are the proposal the brain makes: a reminder ten
	// minutes out, so nothing in the ask-here scenario ever comes due.
	autoTitle = "drink water"
	autoSay   = "Time to drink some water."
	// autoReply is the one line the brain says once the card is answered.
	autoReply = "Saved: a reminder in ten minutes."
)

// TestAutomationsE2E is the three scenarios. Each opens its own state root and
// its own repository, and none of them needs a provider key.
func TestAutomationsE2E(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on PATH: this suite drives the real binary in a real terminal")
	}
	t.Run("ask_here_saves_a_reminder", func(t *testing.T) { autosAskHere(t, tuiWide, 45) })
	t.Run("narrow_window_ask_here", func(t *testing.T) { autosAskHere(t, 60, 30) })
	t.Run("the_clock_says_a_reminder_into_its_conversation", autosClockSaysAReminder)
}

// ── ask here, and the card it ends on ─────────────────────────────────────────

// autosAskHere is home's `ask here` asking for a reminder, end to end: the two
// action rows, the exchange's own row and its three tails, the automation card
// and its answers, the saved card read back, and the saved automation on home's
// panel and on the automations place.
//
// THE EXCHANGE IS THE SCREEN WHILE IT HOLDS THE KEYBOARD, AT EVERY WIDTH (home's
// homeStacked), and its row on the list is what `esc` puts back — so every tail
// is read on the list after the keyboard has left the pane, and every word of
// the pane is read while it holds it. At sixty cells the pane is the whole
// screen, which the narrow run checks by the seeded conversation being gone
// while the pane is up.
//
// AND AT SIXTY CELLS THE FOOT KEEPS LESS. The hint under the box is fitted to
// the width and gives clauses up whole (hintFit): the card's answers stay and
// `enter sends a follow-up` and the way back to the list go. So the narrow run
// waits for what the pane itself draws and for the answers, and reads the
// foot's other clauses only where a frame has room for them.
func autosAskHere(t *testing.T, cols, rows int) {
	wide := cols >= tuiPlain
	brain := startAutomationBrain(t, true)
	// THE APPROVAL POSTURE IS PINNED, because newHome copies whoever runs this
	// suite's own profile: a profile that asks before every call would put a
	// permission question in front of the card this scenario is about.
	home := newHome(t, map[string]any{"model.talk": automationStubModel, "tools.approvalMode": "allow"})
	// A conversation elsewhere on the machine is what makes the launch open on
	// home rather than straight into a first conversation.
	seedProject(t, home, "alpha", 0, time.Minute)
	ws := newWorkspace(t, "autoaskws", false)
	r := startWithEnv(t, brain.env(), "afe2e_autos_ask_"+strconv.Itoa(cols), home, ws, cols, rows,
		"chat", "--one-model")
	r.skipSetup(t)
	r.waitFor(25*time.Second, say(t, "placeRestWord"))

	// THE TWO ACTION ROWS, while something is typed: `ask here` directly over
	// `start a new conversation`, and the cursor resting on the second.
	r.lit(autoAskWords)
	typed := r.waitFor(15*time.Second, say(t, "homeAskHereWord")+": ", say(t, "homeStartWord")+": ")
	t.Logf("the action rows while typing:\n%s", typed)

	// ONE ↑ IS THE ASK: the hint under the box says so before enter is pressed,
	// which is how this suite knows the cursor is on the row it means.
	r.keys("Up")
	r.waitFor(10*time.Second, say(t, "homeAskHereHint"))
	r.keys("Enter")

	// THE PANE HOLDS THE KEYBOARD AND THE TURN HAS NOT ANSWERED YET. The brain
	// holds its first answer until the scenario lets it go, so the strip's
	// `thinking ·` is a fact to wait for here rather than a glimpse.
	inPane := []string{say(t, "homeAskHereWord"), say(t, "homeAskThinkWord")}
	if wide {
		inPane = append(inPane, say(t, "exchangeFollowUp"), say(t, "exchangeBack"))
	}
	pane := r.waitFor(25*time.Second, inPane...)
	t.Logf("the exchange took the screen and is thinking:\n%s", pane)
	if !wide && strings.Contains(pane, "Seed Alpha") {
		t.Errorf("the narrow exchange did not take the whole screen — the list is still drawn:\n%s", pane)
	}

	// THE TAILS ARE THE LIST'S. One esc over an empty follow-up box hands the
	// keyboard back, and the errand is a row of its own wearing `working`.
	r.keys("Escape")
	working := waitForRow(t, r, 25*time.Second, autoAskRow, say(t, "homeAskWorkingWord"))
	t.Logf("the exchange's row while its turn is in flight:\n%s", working)

	// THE CARD ARRIVES and the row says it is waiting on somebody.
	brain.release()
	waiting := waitForRow(t, r, 30*time.Second, autoAskRow, say(t, "notifyAskWord"))
	t.Logf("the card is up and the row says so:\n%s", waiting)

	// AN EXCHANGE OUTLIVES THE SCREEN IT WAS ASKED ON. It is the window's and not
	// home's ([app.exchanges]), so closing home with the card unanswered — one
	// esc, the keyboard being on the list — and opening it again finds the row
	// still there and still waiting on somebody.
	r.keys("Escape")
	if !waitUntil(15*time.Second, func() bool { return !strings.Contains(r.capture(), say(t, "placeRestWord")) }) {
		t.Fatalf("esc with the keyboard on the list did not close home:\n%s", r.capture())
	}
	openHome(t, r)
	again := waitForRow(t, r, 20*time.Second, autoAskRow, say(t, "notifyAskWord"))
	t.Logf("home reopened and the exchange is still waiting on somebody:\n%s", again)

	// ENTER ON THE ROW HANDS THE PANE THE KEYBOARD, with the card in it. The
	// hint under the box is the oracle for where the cursor stands — the
	// switcher's rows carry no lead of their own.
	if !walkTo(r, say(t, "homeAnswerHint"), "Down") {
		t.Fatalf("could not put the cursor on the exchange's row:\n%s", r.capture())
	}
	r.keys("Enter")
	// THE ANSWERS ARE READ OFF THE FOOT, which builds them from the question's
	// own options: a reminder offers no `2`, because there is nothing to try.
	// They are the clauses the foot keeps longest, so they are there at sixty
	// cells too.
	onCard := []string{say(t, "autoRemindHead"), say(t, "exchangeAnswerHint")}
	if wide {
		onCard = append(onCard, say(t, "exchangeFollowUp"), say(t, "exchangeBack"))
	}
	card := r.waitFor(20*time.Second, onCard...)
	t.Logf("the automation card, with the pane holding the keyboard:\n%s", card)
	if !strings.Contains(card, autoTitle) {
		t.Errorf("the card does not carry the proposal's own title %q:\n%s", autoTitle, card)
	}

	// A YES HANDS THE KEYBOARD BACK TO THE LIST BY ITSELF, so the row's tail is
	// what says it landed. The digit waits out the question's settle first: a
	// key that reaches a question in its first quarter-second on screen is
	// dropped as aimed at the screen before it (question.go's questionSettle).
	time.Sleep(questionSettlePause)
	r.lit("1")
	saved := waitForRow(t, r, 30*time.Second, autoAskRow, say(t, "homeAskSavedTail"))
	t.Logf("answered `1` and the row says it saved:\n%s", saved)

	// AND THE SAVED CARD IS ONE ENTER AWAY: the answer and its verdict on the
	// card, and the pane's own note saying where the automation now lives.
	r.keys("Enter")
	settled := r.waitFor(20*time.Second, say(t, "homeAskSavedWord"), autoReply)
	t.Logf("the saved exchange, reopened:\n%s", settled)
	if answered := say(t, "autoSaveAnswer") + " · " + say(t, "autoSavedVerdict"); !strings.Contains(settled, answered) {
		t.Errorf("the settled card does not carry %q:\n%s", answered, settled)
	}

	// esc puts the list back with the settled row on it.
	r.keys("Escape")
	waitForRow(t, r, 20*time.Second, autoAskRow, say(t, "homeAskSavedTail"))

	// HOME'S AUTOMATIONS PANEL CARRIES IT where a frame has room for the panel:
	// it is the first to give way on a short or narrow one (homegrid.go's order
	// table), so it is read at the wide frame only. The title is read inside the
	// panel's own columns, because the exchange's row quotes the same words.
	if cols >= tuiWide {
		openHome(t, r)
		if !waitUntil(20*time.Second, func() bool {
			return strings.Contains(panelBlock(r.capture(), say(t, "homePanelNext")), autoTitle)
		}) {
			t.Errorf("home's %q panel does not carry the saved %q:\n%s", say(t, "homePanelNext"), autoTitle, r.capture())
		} else {
			t.Logf("home's automations panel carries the saved reminder:\n%s", panelBlock(r.capture(), say(t, "homePanelNext")))
		}
	}

	// AND THE AUTOMATIONS PLACE LISTS IT, by its title and its kind.
	r.lit("/" + say(t, "homePanelNext"))
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	listed := waitForRow(t, r, 20*time.Second, autoTitle, say(t, "autoKindReminderWord"))
	t.Logf("the automations place lists the reminder:\n%s", listed)
	r.quit()
}

// ── the clock ───────────────────────────────────────────────────────────────

// autosClockSaysAReminder is a reminder run by the clock a window keeps, end
// to end: typed with the exact grammar (no model in between), saved on its
// card, run by `codeaf clock` while the window is open, and said as one line in
// the conversation that made it, as a desktop notification, and as the row's
// last result on the automations place. Then the window closes and the clock
// leaves on its own.
//
// IT IS THE ONE SCENARIO THAT LETS THE CLOCK RUN, so it names the guard's
// switch itself (`-u CODEAF_NO_AUTOMATIONS`), and it waits for the clock to be
// gone before the state root it runs against is removed.
//
// AND IT IS THE PROOF THAT A LAUNCH SCHEDULES NOTHING ON THE MACHINE. Every
// start of this binary takes the old timers off its login, and the guard's
// login holds none, so the stand-ins for launchctl and systemctl must be asked
// nothing at all — and the developer's own old-timer files are hashed around
// the whole run.
func autosClockSaysAReminder(t *testing.T) {
	requireMachineTimerUntouched(t)
	// NO MODEL IS ASKED ANYTHING HERE. The endpoint is there so that what a
	// launch fetches — the catalog — reaches this test and not the network.
	brain := startAutomationBrain(t, false)
	home := newHome(t, map[string]any{"model.talk": automationStubModel})
	ws := newWorkspace(t, "autoclockws", false)
	root := filepath.Join(home, "v3", "automations")
	// THE CLOCK IS DETACHED AND OUTLIVES THE TERMINAL by its grace, so it is
	// waited out before the state root it runs against is removed. Registered
	// here, before the rig, so it runs after the rig's own cleanup has closed
	// the window — on a failure as much as on a pass.
	t.Cleanup(func() {
		if !waitUntil(90*time.Second, func() bool { return !automation.Held(root) }) {
			t.Logf("the clock still held %s when the test's state root was removed", root)
		}
	})
	env := append([]string{"-u", noClockEnv}, brain.env()...)
	r := startWithEnv(t, env, "afe2e_autos_clock", home, ws, tuiWide, 40, "chat", "--one-model")
	statesPastTheDoor(t, r)

	// THE WINDOW KEEPS A CLOCK. Presence is taken as the surface starts, and a
	// clock is started at once if none holds the lock.
	if !waitUntil(30*time.Second, func() bool { return automation.Held(root) }) {
		t.Fatalf("no clock holds %s while a window is open:\n%s", filepath.Join(root, automation.LockName), r.capture())
	}

	// THE TYPED DOOR: the exact grammar, read with no model in between, ending
	// on the same card a conversation's proposal ends on.
	const title, said = "water", "drink water"
	r.lit(`/` + say(t, "homePanelNext") + ` add ` + title + ` in 30s say "` + said + `"`)
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	card := r.waitFor(20*time.Second, say(t, "autoTypedHead"), said)
	t.Logf("the typed automation's card:\n%s", card)
	time.Sleep(questionSettlePause)
	r.lit("1")
	receipt := r.waitFor(20*time.Second, say(t, "autoTypedSavedWord")+title)
	t.Logf("saved:\n%s", receipt)

	// THE CLOCK SAYS IT. A reminder calls no model: the run records its own
	// line, and the window that holds the conversation it was made in reads the
	// run off the store and draws it as one dim line — the title, what it came
	// to, and what it said.
	line := title + " · " + say(t, "autoRunDoneWord") + " · " + said
	ran := r.waitFor(90*time.Second, line)
	t.Logf("the reminder's line in the conversation that made it:\n%s", ran)

	// AND THE PERSON IS TOLD, through the operating system's own notifier —
	// which the guard stands in for, so the banner is a line in its log and not
	// a banner on the screen of whoever runs this suite.
	notifier := "osascript"
	if runtime.GOOS == "linux" {
		notifier = "notify-send"
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		raised := ""
		if !waitUntil(20*time.Second, func() bool { raised = hostCall(t, r.host, notifier, title); return raised != "" }) {
			t.Errorf("no %s notification named %q; the stand-ins were asked %v", notifier, title, r.host.calls(t))
		} else {
			t.Logf("the notification went to the guard's %s: %s", notifier, raised)
		}
	}

	// THE PLACE SAYS HOW IT LAST WENT.
	r.lit("/" + say(t, "homePanelNext"))
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	listed := waitForRow(t, r, 20*time.Second, title, say(t, "autoKindReminderWord"), say(t, "autoLastWord")+say(t, "autoRunDoneWord"))
	t.Logf("the automations place after the run:\n%s", listed)

	// NOTHING WAS SCHEDULED ON THE MACHINE: not by the window, not by the
	// clock, not by the old-timer removal every start runs.
	for _, call := range r.host.calls(t) {
		for _, scheduler := range []string{"launchctl ", "systemctl ", "crontab "} {
			if strings.HasPrefix(call, scheduler) {
				t.Errorf("a launch asked the machine's scheduler %q", call)
			}
		}
	}

	// AND THE CLOCK LEAVES WHEN THE LAST WINDOW HAS BEEN SHUT FOR ITS GRACE.
	// It is detached, so killing the terminal does not kill it; it counts the
	// windows, finds none for thirty seconds, and goes.
	r.quit()
	if !waitUntil(90*time.Second, func() bool { return !automation.Held(root) }) {
		t.Errorf("the clock was still holding %s ninety seconds after the last window closed", root)
	}
}

// ── reading the screen ──────────────────────────────────────────────────────

// questionSettlePause is how long a scenario lets a question stand on the
// screen before it presses an answer. internal/tui3 DROPS a key that reaches a
// question in its first quarter-second on screen (question.go's
// questionSettle), as a key aimed at the screen that was there before it; a
// wait that saw the card arrive can be inside that quarter-second, so the press
// waits a whole second more. It is a floor under a fact, not a guess at a
// duration: nothing is waited out but the product's own stated guard.
const questionSettlePause = time.Second

// waitForRow polls until ONE LINE of the screen carries every part, and
// answers that line. A screen-wide search is satisfied by a title on one row
// and a tail on another — `saved` is on the pane's note as well as on the
// row — so a claim about one row is read off one row.
func waitForRow(t *testing.T, r *rig, within time.Duration, parts ...string) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		screen := r.capture()
		for _, line := range strings.Split(screen, "\n") {
			whole := true
			for _, part := range parts {
				if !strings.Contains(line, part) {
					whole = false
					break
				}
			}
			if whole {
				return strings.TrimSpace(line)
			}
		}
		if time.Now().After(deadline) {
			t.Errorf("waited %s for one line carrying %q and saw none. the screen was:\n%s", within, parts, screen)
			return ""
		}
		time.Sleep(pollEvery)
	}
}

// waitUntil polls a fact until it is true or the time is up.
func waitUntil(within time.Duration, fact func() bool) bool {
	deadline := time.Now().Add(within)
	for {
		if fact() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollEvery)
	}
}

// hostCall is the first thing a stand-in was asked that names what, or "".
func hostCall(t *testing.T, g hostGuard, standIn, what string) string {
	t.Helper()
	for _, call := range g.calls(t) {
		if strings.HasPrefix(call, standIn+" ") && strings.Contains(call, what) {
			return call
		}
	}
	return ""
}

// ── the scripted model ──────────────────────────────────────────────────────

// automationBrain stands in for every model call a scenario makes. A request
// that offers the `automation` tool and has no tool result in it yet is
// answered with ONE proposal; the same conversation's next request, carrying
// the card's answer as the tool's result, is answered with one line; anything
// else — a title, a summary — is answered with a word.
type automationBrain struct {
	server *httptest.Server
	// hold, when the scenario asked for one, keeps the proposal back until
	// [automationBrain.release], so a turn that has not answered yet is a state
	// the screen can be read in rather than a moment that may already be over.
	hold chan struct{}
	once sync.Once
}

func startAutomationBrain(t *testing.T, hold bool) *automationBrain {
	t.Helper()
	brain := &automationBrain{}
	if hold {
		brain.hold = make(chan struct{})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/models", brain.serveCatalog)
	mux.HandleFunc("POST /api/v1/chat/completions", brain.serveCompletion)
	brain.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		brain.release()
		brain.server.Close()
	})
	return brain
}

// env is what a rig needs to talk to this endpoint and nothing else. The key
// is a string and not a secret: config.Load refuses to build a session with
// none at all, and nothing behind this endpoint checks it.
func (b *automationBrain) env() []string {
	return []string{config.APIKeyEnv + "=stub-key", "CODEAF_BASE_URL=" + b.server.URL + "/api/v1", "CODEAF_PROFILE_DIR="}
}

// release lets a held proposal through. It is idempotent.
func (b *automationBrain) release() {
	b.once.Do(func() {
		if b.hold != nil {
			close(b.hold)
		}
	})
}

func (b *automationBrain) serveCatalog(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"data":[{"id":%q,"canonical_slug":%q,"name":"Scripted automations stub",
		"context_length":200000,
		"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
		"pricing":{"prompt":"0","completion":"0","request":"0","input_cache_read":"0"},
		"supported_parameters":["tools","tool_choice","max_tokens"]}]}`, automationStubModel, automationStubModel)
}

func (b *automationBrain) serveCompletion(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(raw, &body)
	answered := false
	for _, message := range body.Messages {
		if strings.EqualFold(message.Role, "tool") {
			answered = true
		}
	}
	offered := false
	for _, tool := range body.Tools {
		if tool.Function.Name == "automation" {
			offered = true
		}
	}
	send, ok := automationStream(w)
	if !ok {
		return
	}
	switch {
	case offered && !answered:
		// A HELD ANSWER STILL KEEPS THE LINE ALIVE. A stream that says nothing
		// at all for ten seconds is read as a dead path and asked again
		// (internal/provider's armwatch.go), so the hold writes the same comment
		// a real router writes while it is busy, once a second.
		if b.hold != nil {
			beat := time.NewTicker(time.Second)
			defer beat.Stop()
			stop := time.After(45 * time.Second)
		held:
			for {
				select {
				case <-b.hold:
					break held
				case <-stop:
					break held
				case <-r.Context().Done():
					return
				case <-beat.C:
					keepalive(w)
				}
			}
		}
		args, _ := json.Marshal(map[string]any{
			"op": "propose", "title": autoTitle, "words": autoAskWords,
			"when": map[string]string{"in": "10m"}, "say": autoSay,
		})
		quoted, _ := json.Marshal(string(args))
		send(`{"tool_calls":[{"index":0,"id":"call_auto","type":"function","function":{"name":"automation","arguments":`+string(quoted)+`}}]}`, "")
		send(`{}`, "tool_calls")
	case offered:
		quoted, _ := json.Marshal(autoReply)
		send(`{"content":`+string(quoted)+`}`, "")
		send(`{}`, "stop")
	default:
		send(`{"content":"ok"}`, "")
		send(`{}`, "stop")
	}
	send("", "[DONE]")
}

// keepalive writes one SSE comment, the line a router sends while it is still
// working on an answer (internal/provider's sse.go reads it as the stream being
// alive and as nothing else).
func keepalive(w http.ResponseWriter) {
	_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// automationStream writes a stream's head and hands back its frame writer —
// internal/provider's sse.go read backwards, as [checkerBrain.open] does.
func automationStream(w http.ResponseWriter) (func(delta, reason string), bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "the script needs a flushable writer", http.StatusInternalServerError)
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	id := fmt.Sprintf("auto-%d", time.Now().UnixNano())
	send := func(delta, reason string) {
		if reason == "[DONE]" {
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		finish := "null"
		if reason != "" {
			finish = strconv.Quote(reason)
		}
		_, _ = fmt.Fprintf(w, "data: {\"id\":%q,\"object\":\"chat.completion.chunk\",\"created\":%d,"+
			"\"model\":%q,\"provider\":\"stub\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":%s}],"+
			"\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2,\"cost\":0}}\n\n",
			id, time.Now().Unix(), automationStubModel, delta, finish)
		flusher.Flush()
	}
	send(`{"role":"assistant","content":""}`, "")
	return send, true
}
