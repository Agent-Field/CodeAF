package tui3

import (
	"strings"
	"testing"
)

// ── #677: THE START PAGE KEEPS ITS KEYBOARD ─────────────────────────────────
//
// WHAT WAS MEASURED, on dev@5ffb15b18. A shell command was waiting for
// approval. `ctrl+t` for a new chat, then `hello there` typed into the start
// page's box — and the box held `here`. The `t` in "there" had granted the tool
// for the whole session and `sleep 300` ran. The same page's `esc`, which its
// own legend offers as "keeps the chat you were in", denied a task proposal
// behind it.
//
// THE RULE IS THE ONE HOME ALREADY KEEPS: a question is answered where it is
// drawn, and nowhere else. The start page takes the frame whole, so the block is
// not on the screen while it is up — and a question that is not on the screen
// takes no keys ([app.questionOffFrame]).

func TestTheStartPageKeepsEveryLetterTypedIntoIt(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	drive(t, a, streamOf(a, consentEvent(7, "bash", "sleep 300", `bash pattern "sleep *"`)))
	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did not open the start page over the question")
	}
	typeInto(t, a, "hello there")
	if got := a.input.String(); got != "hello there" {
		t.Fatalf("the start page's box holds %q — the question behind it ate the rest", got)
	}
	if a.asking() != true {
		t.Fatal("the question behind the page was settled by the letters typed into it")
	}
	// AND NOTHING OF IT IS DRAWN ON THE PAGE, which is the other half of the
	// rule: a row offering keys the page cannot honour is a row that lies.
	if frame := plain(frame(a)); strings.Contains(frame, "allow once") {
		t.Fatalf("the approval block is drawn on the start page:\n%s", frame)
	}
}

// AND `esc` ON THE START PAGE IS THE START PAGE'S OWN. Its legend says it keeps
// the chat you were in; before this it denied the task proposal behind it.
func TestEscOnTheStartPageClosesThePageAndAnswersNothing(t *testing.T) {
	a, agent, _ := taskApp(t)
	made := 0
	startDoor(a, &made)
	emptyMachine(a)
	drive(t, a, streamOf(a, proposal(a, 7, 0)))
	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did not open the start page over the proposal")
	}
	drive(t, a, key("esc"))
	if len(agent.answered) != 0 {
		t.Fatalf("esc on the start page answered the proposal behind it: %+v", agent.answered)
	}
	if a.startingChat() {
		t.Fatal("esc did not close the page it was pressed on")
	}
}

// AND THE ANSWER KEYS COME BACK WITH THE CONVERSATION. The question was never
// cancelled — it was behind a page — so it is still there to answer.
func TestTheQuestionIsAnsweredAgainOnceTheStartPageIsClosed(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	drive(t, a, streamOf(a, consentEvent(7, "bash", "sleep 300", `bash pattern "sleep *"`)))
	drive(t, a, key(newChatChord))
	typeInto(t, a, "hello there")
	drive(t, a, key("esc"))
	if a.startingChat() {
		t.Fatal("esc did not close the start page")
	}
	if !a.asking() {
		t.Fatal("the question did not come back with the conversation")
	}
	settleAsk(a)
	if row := plain(strings.Join(a.questionRows(a.width), "\n")); !strings.Contains(row, "allow once") {
		t.Fatalf("the question is not back on the block:\n%s", row)
	}
	// AND ITS KEYS ARE ITS OWN AGAIN. (What the answer then reaches is the
	// engine's door, which this lab's agent does not have — the answer road has
	// its own tests; what is being held here is ownership of the keyboard.)
	if _, taken := a.questionKey(questionPressOf("1")); !taken {
		t.Fatal("the question would not take its own key once the page was gone")
	}
}

// AND THE PAGE IS NOT REFUSED OVER THE ROWS IT WAS ABOUT TO RECLAIM.
//
// WHAT WAS MEASURED, on a merge of this branch with lane P's. A permission was
// waiting on a twenty-row terminal, and `ctrl+t` did nothing at all — no page,
// no word. [app.openChatStart] asks whether there is a body region to stand the
// page in, and it was asking [app.viewHeight], which is charged for the question
// block; a consent drawn as a card rather than a line took the body to zero and
// the page refused itself. But the block COMES DOWN when the page goes up
// ([app.questionRows] answers nothing while [app.startingChat]), so those rows
// were the page's own. The guard now measures [app.startPageBody].
//
// The frame here is ten rows for exactly that reason: it is a frame with no body
// region left while the question stands, and a body region once it does not.
func TestTheStartPageOpensOverAQuestionThatHasEatenTheFrame(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.resized(60, 10)
	drive(t, a, streamOf(a, consentEvent(7, "bash", "sleep 300", `bash pattern "sleep *"`)))
	if a.viewHeight() != 0 {
		t.Fatalf("the fixture no longer measures what it is for: the question leaves %d rows of body, so the old guard would have opened the page anyway", a.viewHeight())
	}
	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t did nothing and said nothing: the page was refused over the rows the question block was holding, and the question block comes down with the page")
	}
}
