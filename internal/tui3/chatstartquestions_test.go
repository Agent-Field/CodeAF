package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── QUESTIONS BEHIND THE NEW-CHAT START PAGE ───────────────────────────────
//
// The start page is a message box over a conversation, not another view of
// that conversation's answer lane. These tests hold the rule from both sides:
// every key on the page belongs to the first message, and the question stays
// with the chat behind it until somebody goes back there to answer it.

// startQuestion is one of the four questions that can be waiting in the chat
// under the start page. The closures keep the assertion on what the session
// received and what the surface still holds rather than on one shared internal
// representation the four lanes do not have.
type startQuestion struct {
	name     string
	a        *app
	ev       session.Event
	answered func() bool
	waiting  func() bool
}

// startQuestions builds each question from the same fixtures its own tests use.
func startQuestions(t *testing.T) []startQuestion {
	t.Helper()

	consentAgent, consentApp := wired()
	connectAgent, connectSurface, _ := connectApp(t)
	taskSurface, taskAgent, _ := taskApp(t)
	harnessAgent := &harnessAgent{fakeAgent: &fakeAgent{model: "m"}}
	harnessSurface := newTestApp(harnessAgent)

	return []startQuestion{
		{
			name: "the approval question", a: consentApp,
			ev:       consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`),
			answered: func() bool { return len(consentAgent.answers) > 0 },
			waiting:  consentApp.asking,
		},
		{
			name: "the task proposal", a: taskSurface, ev: proposal(taskSurface, 7, 0),
			answered: func() bool { return len(taskAgent.answered) > 0 },
			waiting:  taskSurface.awaitingTask,
		},
		{
			name: "the connect offer", a: connectSurface,
			ev:       askConnectEvent("c1", "notion", "Notion"),
			answered: func() bool { return len(connectAgent.resolved) > 0 },
			waiting:  connectSurface.asksConnect,
		},
		{
			name: "the harness offer", a: harnessSurface,
			ev: session.Event{
				Kind: session.EventHarnessOffer, ID: 3, Text: "research",
				Hint: "Research a question across sources and write a report",
			},
			answered: func() bool { return len(harnessAgent.answers) > 0 },
			waiting:  harnessSurface.asksHarness,
		},
	}
}

// raise starts the question and gives this window the same new-conversation
// door the start-page tests use. Opening the page itself must spend none of it.
func (q startQuestion) raise(t *testing.T) {
	t.Helper()
	drive(t, q.a, streamOf(q.a, q.ev))
	if !q.waiting() {
		t.Fatal("the fixture did not raise its question")
	}
	made := 0
	startDoor(q.a, &made)
	emptyMachine(q.a)
}

// THE EXACT REPRO. The t in a first sentence used to widen the permission and
// the spaces around it disappeared with every other key the hidden question
// swallowed. The whole sentence belongs to the page, and the session hears no
// answer at all.
func TestTheStartPageBoxGetsEveryCharacterOverAConsentQuestion(t *testing.T) {
	agent, a := wired()
	drive(t, a, streamOf(a,
		consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))
	made := 0
	startDoor(a, &made)
	emptyMachine(a)

	drive(t, a, key(newChatChord))
	typeInto(t, a, "hello there")

	if got := a.input.String(); got != "hello there" {
		t.Fatalf("the start page kept %q, want the whole first sentence", got)
	}
	if !a.asking() {
		t.Fatal("typing on the start page took the question down")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("typing on the start page answered the session: %+v", agent.answers)
	}
}

// EVERY LETTER IS THE PAGE'S. This includes the two silent legacy permission
// keys: a first sentence may contain y, n, a, t or d without answering any kind
// of question in the conversation behind it. Pressing ctrl+t again preserves
// those letters on the same page.
func TestTheStartPageKeepsAnswerLettersOverEveryQuestionBehindIt(t *testing.T) {
	for _, q := range startQuestions(t) {
		t.Run(q.name, func(t *testing.T) {
			q.raise(t)
			drive(t, q.a, key(newChatChord))
			typeInto(t, q.a, "ynatd")
			drive(t, q.a, key(newChatChord), key(newChatChord))

			if got := q.a.input.String(); got != "ynatd" {
				t.Fatalf("the page kept %q, want %q", got, "ynatd")
			}
			if q.answered() {
				t.Fatal("a letter on the start page answered the question behind it")
			}
			if !q.waiting() {
				t.Fatal("the question behind the start page went away")
			}
		})
	}
}

// ESC BELONGS TO THE PAGE TOO. It returns the exact draft of the conversation
// underneath and leaves each kind of question standing and unanswered there.
func TestEscapeFromTheStartPageReturnsToEveryUnansweredQuestion(t *testing.T) {
	for _, q := range startQuestions(t) {
		t.Run(q.name, func(t *testing.T) {
			q.a.input.setText("the chat's own draft")
			q.raise(t)
			drive(t, q.a, key(newChatChord), key("esc"))

			if q.a.startingChat() {
				t.Fatal("esc left the start page open")
			}
			if got := q.a.input.String(); got != "the chat's own draft" {
				t.Fatalf("esc returned the conversation draft as %q", got)
			}
			if q.answered() {
				t.Fatal("esc from the start page answered the question behind it")
			}
			if !q.waiting() {
				t.Fatal("esc from the start page took the question down")
			}
		})
	}
}

// A QUESTION IS DRAWN WHERE IT CAN BE ANSWERED AND NOWHERE ELSE. All four
// cards disappear under the start page, and the three bottom blocks agree with
// the frame about taking no height there.
func TestNoQuestionIsDrawnOnTheStartPage(t *testing.T) {
	for _, q := range startQuestions(t) {
		t.Run(q.name, func(t *testing.T) {
			q.raise(t)
			drive(t, q.a, key(newChatChord))
			got := plain(frame(q.a))
			for _, absent := range []string{
				"allow?", "openaf wants to connect your Notion account",
				`run harness "research"?`, "Fix the nil-map crash",
			} {
				if strings.Contains(got, absent) {
					t.Fatalf("the start page drew %q:\n%s", absent, got)
				}
			}
			if heights := []int{
				q.a.consentHeight(), q.a.connectAskHeight(), q.a.harnessAskHeight(),
			}; heights[0] != 0 || heights[1] != 0 || heights[2] != 0 {
				t.Fatalf("the hidden question heights are %v, want all zero", heights)
			}
		})
	}
}

// A STALE TARGET IS NOT AN ANSWER. The tap spans are first drawn in the
// conversation, then the page covers them; a pointer press in those old cells
// must act on neither approval, connection nor harness offer.
func TestAPressWhereAHiddenQuestionUsedToBeAnswersNothing(t *testing.T) {
	for _, q := range startQuestions(t) {
		if q.name == "the task proposal" {
			continue // Its card already has no question block on this page.
		}
		t.Run(q.name, func(t *testing.T) {
			q.raise(t)
			_ = frame(q.a)
			var x, y int
			switch q.name {
			case "the approval question":
				x = q.a.askTaps[0].span.from
				y = chromeRowY(t, q.a, consentOfferRow)
			case "the connect offer":
				x = q.a.connTaps[0].span.from
				y = connectOfferY(t, q.a)
			case "the harness offer":
				x = q.a.harnessTaps[0].span.from
				y = harnessRowY(t, q.a)
			}

			drive(t, q.a, key(newChatChord))
			_ = frame(q.a)
			drive(t, q.a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if q.answered() {
				t.Fatal("a press where the hidden question used to be answered it")
			}
		})
	}
}

// THE ASKING CHAT STILL SAYS SO. Its tab keeps the question mark under the
// start page; going back restores the offer and its y answers exactly once.
func TestTheAskingChatKeepsItsMarkAndAnswersWhenYouGoBack(t *testing.T) {
	agent, a := wired()
	a.file, a.title = "/tmp/lab/one.jsonl", "Shipping the parser"
	drive(t, a, streamOf(a,
		consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))
	made := 0
	startDoor(a, &made)
	emptyMachine(a)

	drive(t, a, key(newChatChord))
	if got := a.frontSignal(); got != tabNeedsPerson {
		t.Fatalf("the asking chat's tab signal is %v, want the question mark", got)
	}
	if !strings.Contains(plain(frame(a)), "?") {
		t.Fatalf("the start page has no question mark on the asking chat's tab:\n%s", plain(frame(a)))
	}

	drive(t, a, key("esc"))
	if got := plain(frame(a)); !strings.Contains(got, "allow? [y] yes") {
		t.Fatalf("the offer did not return with its chat:\n%s", got)
	}
	drive(t, a, key("y"))
	if len(agent.answers) != 1 {
		t.Fatalf("y sent %d answers, want exactly one", len(agent.answers))
	}
	if got := agent.answers[0]; !got.allow || got.scope != session.ConsentOnce {
		t.Fatalf("y answered %+v, want one-call allow", got)
	}
}

// THE CONTROL. The answer letter still belongs to a visible question inside
// its conversation; the new guard must not disable the ordinary permission
// road.
func TestAVisibleConsentQuestionStillAnswersItsLetter(t *testing.T) {
	agent, a := wired()
	drive(t, a, streamOf(a,
		consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))
	drive(t, a, key("y"))

	if len(agent.answers) != 1 || !agent.answers[0].allow || agent.answers[0].scope != session.ConsentOnce {
		t.Fatalf("the visible question answered as %+v", agent.answers)
	}
}

// AND THE GREETING IS NOT THE START PAGE. The launch screen a window opens on
// draws the same wordmark, and a question raised before the first sentence has
// always stacked above it — there is no other box for those letters to be aimed
// at up there. The guard is the NEW-CHAT page and not the greeting, and this is
// the line between them.
func TestTheGreetingStillStacksAndAnswersAQuestion(t *testing.T) {
	agent, a := wired()
	a.welcome = welcome{open: true}
	drive(t, a, streamOf(a,
		consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))

	if a.startingChat() {
		t.Fatal("the greeting is not the start page")
	}
	if got := plain(frame(a)); !strings.Contains(got, "allow? [y] yes") {
		t.Fatalf("the greeting stopped drawing the question:\n%s", got)
	}
	drive(t, a, key("y"))
	if len(agent.answers) != 1 || !agent.answers[0].allow {
		t.Fatalf("the greeting stopped answering its letter: %+v", agent.answers)
	}
}

// THE CLOCK NEVER ANSWERS FOR ANYBODY. It may run down while its conversation
// is behind the page, but expiry only pauses it; going back finds the same
// unanswered question with that state said on its offer.
func TestAQuestionBehindTheStartPagePausesAtExpiryAndNeverAnswers(t *testing.T) {
	at := time.Now()
	agent, a := wired()
	a.clock, a.askWait = func() time.Time { return at }, 10*time.Second
	drive(t, a, streamOf(a,
		consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))
	made := 0
	startDoor(a, &made)
	emptyMachine(a)
	drive(t, a, key(newChatChord))

	at = at.Add(11 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("expiry answered the hidden question: %+v", agent.answers)
	}
	if !a.asking() || !a.askPaused {
		t.Fatalf("expiry left asking=%v paused=%v, want both true", a.asking(), a.askPaused)
	}

	drive(t, a, key("esc"))
	if got := plain(frame(a)); !strings.Contains(got, "paused") {
		t.Fatalf("the question did not return paused:\n%s", got)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("coming back answered the question: %+v", agent.answers)
	}
}

// ENTER IS THE PAGE'S SEND. Even though every parked lane gives enter a meaning
// of its own, a first message creates a new conversation and goes to that new
// conversation while the original question remains unanswered.
func TestEnterStartsTheFirstMessageOverEveryQuestionBehindThePage(t *testing.T) {
	for _, q := range startQuestions(t) {
		t.Run(q.name, func(t *testing.T) {
			drive(t, q.a, streamOf(q.a, q.ev))
			next := &fakeAgent{model: "m"}
			q.a.start = func(string) (Conversation, error) {
				return Conversation{Agent: next, SessionFile: "/tmp/lab/new.jsonl"}, nil
			}
			emptyMachine(q.a)

			drive(t, q.a, key(newChatChord))
			typeInto(t, q.a, "go")
			drive(t, q.a, key("enter"))

			if q.a.startingChat() {
				t.Fatal("enter left the start page open")
			}
			if len(next.sent) != 1 || next.sent[0] != "go" {
				t.Fatalf("the new conversation received %+v, want the first message", next.sent)
			}
			if q.answered() {
				t.Fatal("the start page's enter answered the parked question")
			}
		})
	}
}

// THE OTHER TWO CHAT CHORDS KEEP THEIR QUESTION-TIME ROADS. The fix is about
// who owns keys after ctrl+t opens the page, not about taking away close or the
// switcher before it does.
func TestCloseAndChatsStillOpenFromAConsentQuestion(t *testing.T) {
	for _, chord := range []string{closeTabChord, "ctrl+k"} {
		t.Run(chord, func(t *testing.T) {
			_, a := wired()
			a.file, a.title = "/tmp/lab/one.jsonl", "Shipping the parser"
			drive(t, a, streamOf(a,
				consentEvent(7, "bash", "bash sleep 300", `bash pattern "sleep *"`)))
			if chord == "ctrl+k" {
				older := &fakeAgent{model: "m"}
				a.stow(Conversation{Agent: older, SessionFile: "/tmp/lab/older.jsonl"},
					&aside{title: "Older chat"})
			}

			drive(t, a, key(chord))
			if chord == closeTabChord && !a.closingTab() {
				t.Fatal("ctrl+w did not open the close card from a question")
			}
			if chord == "ctrl+k" && !a.hopShowing() {
				t.Fatal("ctrl+k did not open Chats from a question")
			}
		})
	}
}
