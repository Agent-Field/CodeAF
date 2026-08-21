package tui3

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
	// AND ONLY WHERE THE CURSOR IS. The band is the card's, and the card is the
	// row under the cursor — a second row's chips would be two questions on
	// screen with one keyboard between them.
	lab.a.home.point(lab.a.file)
	if text := homeText(lab.a); strings.Contains(text, "3 deny") {
		t.Fatalf("the chips followed the cursor off the row they belong to:\n%s", text)
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
	// AND THE BAND SAYS IT IS WAITING, because the other session applies this on
	// its own beat and the chips must not invite a second answer meanwhile.
	if !strings.Contains(text, answerWaitingWord) {
		t.Fatalf("the band did not say the answer is on its way:\n%s", text)
	}
	if strings.Contains(text, "1 allow once") {
		t.Fatalf("the chips are still up on an answered question:\n%s", text)
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

	a.homeKey(key("1"))
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
