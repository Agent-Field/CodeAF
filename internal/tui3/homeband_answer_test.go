package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ANSWERING FROM HOME: the chips, the keys, the pointer, and the two windows
// this must never answer — a stale one, and a question that offered no answers.

// sentAnswer is one answer this window left on another session's doorstep, as
// the seam saw it.
type sentAnswer struct {
	dir  string
	kind session.QuestionKind
	id   uint64
	key  string
}

// answerLab is a projects root with one conversation stopped on a question, and
// a surface pointed at it with the seam recorded rather than written.
type answerLab struct {
	*homeLab
	a    *app
	sent *[]sentAnswer
	dir  string
	row  string
}

// asking writes one session's presence with a question on it — the file a live
// session's heartbeat would have written (session's taskpresence.go).
func (l *homeLab) asking(bucket, id string, question session.PresenceQuestion, at time.Time) string {
	l.t.Helper()
	dir := filepath.Join(l.project(bucket), id)
	raw, err := json.Marshal(session.SessionPresence{
		Schema:    1,
		SessionID: id,
		Workspace: "/tmp/alpha",
		PID:       4242,
		UpdatedAt: at,
		State:     session.PresenceWaiting,
		Reason:    question.Text,
		Question:  question,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "presence.json"), append(raw, '\n'), 0o600); err != nil {
		l.t.Fatal(err)
	}
	return dir
}

// consentQuestion is the gate's question as another window reads it, with the
// engine's own words for the chips.
func consentQuestion(id uint64, text string) session.PresenceQuestion {
	return session.PresenceQuestion{
		Kind:    session.QuestionConsent,
		ID:      id,
		Text:    text,
		Options: session.AnswerOptions(session.QuestionConsent),
		Asked:   time.Now(),
	}
}

// automationQuestion is an automation card as another window reads it, with
// the engine's own answers for that automation ([session.AutomationOptions]) —
// which is what makes the chips home draws the chips that session will
// actually take.
func automationQuestion(id uint64, item automation.Automation, text string) session.PresenceQuestion {
	return session.PresenceQuestion{
		Kind:    session.QuestionAutomation,
		ID:      id,
		Text:    text,
		Options: session.AutomationOptions(item),
		Asked:   time.Now(),
	}
}

// newAnswerLab builds the whole situation: this window is sitting in one
// conversation, and ANOTHER one, in another project, is stopped on a question.
func newAnswerLab(t *testing.T, question session.PresenceQuestion, refreshed time.Time) *answerLab {
	t.Helper()
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now.Add(-2*time.Minute))
	row := lab.session("-tmp-beta", "bbbb000000000001", "pricing research", "/tmp/beta", now.Add(-3*time.Minute))
	dir := lab.asking("-tmp-beta", "bbbb000000000001", question, refreshed)

	sent := []sentAnswer{}
	a := lab.app(mine)
	a.width, a.height = 120, 30
	a.leaveAnswer = func(dir string, kind session.QuestionKind, id uint64, key string) error {
		sent = append(sent, sentAnswer{dir: dir, kind: kind, id: id, key: key})
		return nil
	}
	a.openHome()
	a.home.point(row)
	return &answerLab{homeLab: lab, a: a, sent: &sent, dir: dir, row: row}
}

// THE BAND IS THE ANSWERS. The question itself is the state band's line, one
// row above; this is the row of chips under it.
func TestHomeDrawsTheAnswersToAnotherWindowsQuestion(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	text := homeText(lab.a)
	if !strings.Contains(text, "needs your ok to run bash") {
		t.Fatalf("the card does not say what it is stopped on:\n%s", text)
	}
	for _, chip := range []string{"1 allow once", "2 always", "3 deny"} {
		if !strings.Contains(text, chip) {
			t.Fatalf("the card does not offer %q:\n%s", chip, text)
		}
	}
	// AND ONCE, ON THE ROW, and only while the row is being read. The grid
	// draws a question's answers on its `needs you` row under the pointer or
	// the cursor (owner, 2026-09-17), never a second time at the foot — and a
	// digit answers the top one from anywhere whether or not its chips are on
	// the screen (homegrid.go's [app.homeGridAnswer]).
	if n := strings.Count(text, "3 deny"); n != 1 {
		t.Fatalf("the chips are drawn %d times with the cursor on their row:\n%s", n, text)
	}
	lab.a.home.point(lab.a.file)
	if n := strings.Count(homeText(lab.a), "3 deny"); n != 0 {
		t.Fatalf("the chips are drawn %d times with the cursor elsewhere:\n%s", n, homeText(lab.a))
	}
	lab.a.homeKey(key("3"))
	if len(*lab.sent) != 1 || (*lab.sent)[0].key != "3" {
		t.Fatalf("the digit did not answer the top question with its chips off the screen: %+v", *lab.sent)
	}
}

func TestANarrowAnswerBandKeepsEveryChip(t *testing.T) {
	question := consentQuestion(7, "needs your ok to run bash")
	lab := newAnswerLab(t, question, time.Now())
	rows := lab.a.answerChipLines(question, 30, lab.a.pal)
	assertNarrowRows(t, "answer", rows, 30, "3 deny")
	if len(rows) < 2 {
		t.Fatalf("narrow chips stayed on one row: %q", plain(strings.Join(rows, "\n")))
	}
}

// A WINDOW WITH NOWHERE TO LEAVE AN ANSWER OFFERS NONE. The absence law: chips
// that did nothing would be worse than the walk they promised to save.
func TestHomeOffersNoAnswerWithNoSeamToLeaveOneOn(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	lab.a.leaveAnswer = nil
	if text := homeText(lab.a); strings.Contains(text, "1 allow once") {
		t.Fatalf("a window that cannot answer drew chips:\n%s", text)
	}
}

func TestADigitOnHomeLeavesTheAnswerOnTheOtherSessionsDoorstep(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	lab.a.homeKey(key("3"))

	if len(*lab.sent) != 1 {
		t.Fatalf("the digit sent %d answers, want 1", len(*lab.sent))
	}
	answer := (*lab.sent)[0]
	if answer.dir != lab.dir || answer.kind != session.QuestionConsent || answer.id != 7 || answer.key != "3" {
		t.Fatalf("the answer reads %+v, want the beta session's question 7 answered 3", answer)
	}
	// NOTHING WAS TYPED. A digit that answered must not also land in the box,
	// where the next enter would send it to a model as a message.
	if typed := lab.a.home.box.String(); typed != "" {
		t.Fatalf("the digit also typed %q into the box", typed)
	}
	text := homeText(lab.a)
	if !strings.Contains(text, answerSentWord+"deny") {
		t.Fatalf("home did not say what it just answered:\n%s", text)
	}
	if strings.Contains(text, "1 allow once") {
		t.Fatalf("the chips are still up on an answered question:\n%s", text)
	}
	// AND THE BAND SAYS IT IS WAITING, because the other session applies this on
	// its own beat and the chips must not invite a second answer meanwhile.
	//
	// THE BAND IS ON THE CARD AND THE CARD ONLY EXISTS PAST [homeCardMin] (SCREEN
	// 1d). At this lab's ordinary width home is the flat list, the chips are the
	// strip's at the foot of the frame, and their going is the whole of what a
	// person sees there. The SENTENCE that says why they went is the answer
	// band's, so it is asked for at a width where a card is drawn — and wide
	// enough that a sentence longer than [homeCardCol] is not clipped.
	lab.a.width, lab.a.height = homeCardWidest, 40
	lab.a.home.build()
	if card := strings.Join(homeCardFor(t, lab.a, lab.row), "\n"); !strings.Contains(card, answerWaitingWord) {
		t.Fatalf("the card did not say the answer is on its way:\n%s", card)
	}
	// A SECOND PRESS IS NOT A SECOND ANSWER.
	lab.a.homeKey(key("1"))
	if len(*lab.sent) != 1 {
		t.Fatalf("a second press sent another answer: %+v", *lab.sent)
	}
}

// A KEY THE QUESTION DOES NOT TAKE IS STILL A CHARACTER. Home's box is a search
// and a new conversation at once, and `4` on a row that offers three answers is
// somebody typing.
func TestADigitTheQuestionDoesNotTakeIsTyped(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	lab.a.homeKey(key("4"))
	if len(*lab.sent) != 0 {
		t.Fatalf("a key nothing offered was answered: %+v", *lab.sent)
	}
	if typed := lab.a.home.box.String(); typed != "4" {
		t.Fatalf("the box holds %q, want the character that was typed", typed)
	}
}

// NEVER A STALE WINDOW. A terminal killed with a card on screen leaves a file
// claiming a question nobody is waiting for; answering it would be a keystroke
// that quietly did nothing.
func TestHomeWillNotAnswerAWindowThatStoppedRefreshing(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now().Add(-2*time.Minute))
	if text := homeText(lab.a); strings.Contains(text, "1 allow once") {
		t.Fatalf("a stale window was offered answers:\n%s", text)
	}
	lab.a.homeKey(key("1"))
	if len(*lab.sent) != 0 {
		t.Fatalf("a stale window was answered: %+v", *lab.sent)
	}
	if typed := lab.a.home.box.String(); typed != "1" {
		t.Fatalf("the box holds %q, want the character that was typed", typed)
	}
}

// A QUESTION WITH NO ANSWERS ON IT IS NOT ANSWERABLE. The stuck-turn question
// travels this way — it is about a turn and has three answers of its own — so a
// presence that names no options gets the row's `▲` and nothing more.
func TestHomeOffersNothingForAQuestionWithNoOptions(t *testing.T) {
	lab := newAnswerLab(t, session.PresenceQuestion{
		Kind: session.QuestionConsent, ID: 7, Text: "it has tried that three times",
	}, time.Now())
	text := homeText(lab.a)
	if !strings.Contains(text, "it has tried that three times") {
		t.Fatalf("the line it is stopped on is missing:\n%s", text)
	}
	if strings.Contains(text, "1 allow once") {
		t.Fatalf("a question that offered nothing was given chips:\n%s", text)
	}
}

// THE POINTER ANSWERS WHAT IT IS OVER, which is the whole reason the chips are
// laid out rather than merely printed.
func TestAClickOnAChipAnswersThatChip(t *testing.T) {
	lab := newAnswerLab(t, consentQuestion(7, "needs your ok to run bash"), time.Now())
	width, height := lab.a.size()
	lines, _, _, _ := lab.a.homeFrame(width, height)
	found := false
	for y, line := range lines {
		plain := ansi.Strip(line)
		at := strings.Index(plain, "2 always")
		if at < 0 {
			continue
		}
		found = true
		// The column the chip starts in, which is what a press carries — the
		// left column above it holds glyphs that are one cell and three bytes.
		lab.a.homePress(ansi.StringWidth(plain[:at])+1, y)
		break
	}
	if !found {
		t.Fatal("the chips were never drawn, so this proves nothing")
	}
	if len(*lab.sent) != 1 || (*lab.sent)[0].key != "2" {
		t.Fatalf("the click sent %+v, want the always chip", *lab.sent)
	}
	// AND A CLICK BESIDE THEM IS NOT AN ANSWER: it falls through to the list, as
	// every other press on this screen does.
	before := len(*lab.sent)
	lab.a.homePress(width-1, height-1)
	if len(*lab.sent) != before {
		t.Fatalf("a press off the chips answered something: %+v", *lab.sent)
	}
}

// THIS WINDOW'S OWN QUESTION IS ANSWERED IN THIS WINDOW'S OWN HANDS. Writing
// into the folder this process is holding open would be a session talking to
// itself through the disk.
func TestHomeAnswersItsOwnWindowThroughItsOwnResolver(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "porting the resume picker", "/tmp/alpha", now)
	lab.asking("-tmp-alpha", "aaaa000000000001", consentQuestion(7, "needs your ok to run bash"), now)

	agent := &wiredAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.width, a.height = 120, 30
	a.homeRoot = lab.root
	a.file = mine
	sent := 0
	a.leaveAnswer = func(string, session.QuestionKind, uint64, string) error { sent++; return nil }
	// The card this window is holding, with the same id the presence file
	// carries — which is what makes the two the same question.
	a.askConsent(consentEvent(7, "bash", "rm -rf build/", "bash always asks"))
	a.openHome()
	a.home.point(mine)

	// The answer travels on the command the key hands back (offloop.go).
	spend(t, a, a.homeKey(key("1")))
	if sent != 0 {
		t.Fatal("this window left an answer on its own doorstep instead of answering it")
	}
	if len(agent.answers) != 1 {
		t.Fatalf("the resolver was called %d times, want once", len(agent.answers))
	}
	if got := agent.answers[0]; got.id != 7 || !got.allow || got.scope != session.ConsentOnce {
		t.Fatalf("the answer reads %+v, want question 7 allowed once", got)
	}
	// AND THE CARD UNDERNEATH IS SETTLED, so the window it belongs to is not
	// still asking a question that has been answered.
	if a.asking() {
		t.Fatal("the card in this window is still up after being answered from home")
	}
}

// SAYING NO FROM HOME, IN ONE KEYSTROKE.
//
// The automation card's no lives on a key home cannot spare otherwise: `esc`
// on home closes home. The engine's list carries the card's own no, and this
// band draws it like any other chip. A card met at home can be answered every
// way without walking to the window it is in.
func TestHomeCanSayNoToAnAutomationCard(t *testing.T) {
	watch := automation.Automation{Title: "ci on main", Words: "tell me when ci goes red",
		Schedule: automation.Schedule{Every: "15m"}, Look: &automation.Look{Command: "gh run list", Condition: "the latest run failed"},
		Action: automation.Action{Say: "CI is red"}}
	lab := newAnswerLab(t, automationQuestion(9, watch, "wants to watch for something: ci on main"), time.Now())
	text := homeText(lab.a)
	for _, chip := range []string{"1 Save", "2 Save and check it now", "0 Don't save"} {
		if !strings.Contains(text, chip) {
			t.Fatalf("the card does not offer %q:\n%s", chip, text)
		}
	}

	lab.a.homeKey(key(session.AutomationNoKey))
	if len(*lab.sent) != 1 {
		t.Fatalf("the decline sent %d answers, want 1", len(*lab.sent))
	}
	answer := (*lab.sent)[0]
	if answer.dir != lab.dir || answer.kind != session.QuestionAutomation || answer.id != 9 || answer.key != session.AutomationNoKey {
		t.Fatalf("the answer reads %+v, want the automation card declined", answer)
	}
	// NOTHING WAS TYPED, which is the whole trade a digit key makes on a screen
	// whose box is a search and a new conversation at once.
	if typed := lab.a.home.box.String(); typed != "" {
		t.Fatalf("the decline also typed %q into the box", typed)
	}
	if !strings.Contains(homeText(lab.a), answerSentWord+"Don't save") {
		t.Fatalf("home did not say what it just answered:\n%s", homeText(lab.a))
	}
}

// A REMINDER HAS NO `run it now` AND STILL HAS A NO. The `2` is the only chip
// that is ever missing ([session.AutomationOptions]); the decline answers every
// automation card there is.
func TestHomeCanSayNoToAReminderThatOffersNoRunNow(t *testing.T) {
	reminder := automation.Automation{Title: "leave", Words: "remind me at 6 to leave",
		Schedule: automation.Schedule{At: time.Now().Add(time.Hour)}, Action: automation.Action{Say: "leave"}}
	lab := newAnswerLab(t, automationQuestion(9, reminder, "wants to remind you: leave"), time.Now())
	text := homeText(lab.a)
	if strings.Contains(text, "run it now") || strings.Contains(text, "check it now") {
		t.Fatalf("a reminder was offered running it now from home:\n%s", text)
	}
	if !strings.Contains(text, "0 Don't save") {
		t.Fatalf("a reminder was offered no way to say no:\n%s", text)
	}
	lab.a.homeKey(key(session.AutomationNoKey))
	if len(*lab.sent) != 1 || (*lab.sent)[0].key != session.AutomationNoKey {
		t.Fatalf("the decline sent %+v", *lab.sent)
	}
}

// askQuestion is a question the MODEL raised, as another window reads it: the
// whole object beside the four fields every older reader knows, and answers that
// are the model's own words rather than a lane's fixed table.
func askQuestion(id uint64, head string) session.PresenceQuestion {
	whole := session.Question{
		ID: id, Kind: session.QuestionAsk, Ask: session.AskPermission,
		Form: session.FormLine, Asker: session.Asker{Kind: session.AskerModel},
		Head: head, Reason: "the draft has not been read by anybody else",
		Options: []session.AnswerOption{
			{Key: "1", Label: "publish it"},
			{Key: "2", Label: "hold it", Safe: true},
		},
		Stakes: session.StakesReversible, Asked: time.Now(),
	}
	return session.PresenceQuestion{
		Kind: whole.Kind, ID: whole.ID, Text: whole.Head,
		Options: whole.Options, Asked: whole.Asked, Full: &whole,
	}
}

// EVERY LANE'S ANSWERS REACH THE ROW, AND THE DOORSTEP TAKES THEM.
//
// This is the whole path a person walks when a conversation in another window
// stops on a question the model raised: the chips come off the question's own
// options, the digit is checked against those options, and the answer is left in
// the folder that session drains. The seam here is the REAL one
// ([session.WriteAnswer]) and not the lab's recorder, because the defect was
// entirely on the far side of it — the door refused every lane whose keys the
// kind's own table does not fix, so the chips drew, the key landed, and home
// said `could not leave that answer` about a question it had just offered to
// answer.
func TestHomeAnswersAQuestionTheModelRaisedThroughTheRealDoorstep(t *testing.T) {
	lab := newAnswerLab(t, askQuestion(7, "publish the draft?"), time.Now())
	lab.a.leaveAnswer = session.WriteAnswer

	text := homeText(lab.a)
	for _, chip := range []string{"1 publish it", "2 hold it"} {
		if !strings.Contains(text, chip) {
			t.Fatalf("home does not offer %q for the model's own question:\n%s", chip, text)
		}
	}

	lab.a.homeKey(key("1"))
	if said := homeText(lab.a); strings.Contains(said, answerFailedWord) {
		t.Fatalf("the doorstep refused an answer home had just offered:\n%s", said)
	}
	raw, err := os.ReadFile(session.AnswersPath(lab.dir))
	if err != nil {
		t.Fatalf("nothing reached the doorstep: %v", err)
	}
	var answer session.Answer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("the doorstep holds %q: %v", raw, err)
	}
	if answer.Kind != session.QuestionAsk || answer.ID != 7 || answer.FirstKey() != "1" {
		t.Fatalf("the answer reads %+v, want the ask lane's question 7 answered 1", answer)
	}
	if said := homeText(lab.a); !strings.Contains(said, answerSentWord+"publish it") {
		t.Fatalf("home did not say what it just answered:\n%s", said)
	}
}
