package tui3

// `c change` on a receipt: a decision made again.
//
// The receipt above the box has offered `c change` on every reversible decision
// since it was written (question.go's [app.questionReceiptLine]) and nothing has
// ever taken the key — a sentence on the screen naming a key that does nothing.
// What it takes now is the SAME question, put back on the block with its answers
// exactly as they were read the first time, and the answer it takes carries one
// bit saying the decision is being changed ([session.Answer.Revises]).
//
// IT IS NOT A SECOND ANSWERING PATH. The block draws it, the pointer walks it
// and [app.answerQuestion] sends it, all unchanged; the only difference between
// a question and a question being changed is that bit and the receipt that came
// off the screen to make room for it.
//
// THE KEY IS ONLY TAKEN WHERE THE SENTENCE IS ON THE SCREEN AND THE BOX IS
// EMPTY. A receipt is drawn above the box in the conversation for a short while
// after an answer ([app.questionRecordsShown]); outside that, `c` is a letter
// somebody is typing, and a surface that took it would be answering a question
// with the first word of a sentence.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// questionReceiptKey is `c` or `u` on the newest receipt that still takes one.
func (a *app) questionReceiptKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key != questionCommentKey && key != questionUndoKey {
		return nil, false
	}
	if !a.input.empty() {
		return nil, false
	}
	if a.questionOffFrame() || a.pageShowing() {
		// WHAT IS NOT ON THE SCREEN TAKES NO KEYS, which is [app.questionKey]'s
		// own guard about the block said about the row under it.
		return nil, false
	}
	if key == questionUndoKey {
		record, ok := a.undoableReceipt()
		if !ok {
			return nil, false
		}
		return a.undoGrant(record), true
	}
	record, ok := a.changeableReceipt()
	if !ok {
		return nil, false
	}
	a.changeAnswer(record)
	return nil, true
}

// changeableReceipt is the newest receipt whose decision can still be walked
// back and whose question this surface kept.
//
// IT IS THE NEWEST AND NOT THE FIRST ONE THAT MATCHES, because `c` means the
// thing the person is looking at: two receipts are drawn at once and the one
// they have just made is the lower and the newer of them.
func (a *app) changeableReceipt() (questionRecord, bool) {
	shown := a.questionRecordsShown()
	for at := len(shown) - 1; at >= 0; at-- {
		if record := shown[at]; questionCanChange(record) {
			return record, true
		}
	}
	return questionRecord{}, false
}

// questionCanChange is THE ONE READING of whether a receipt may be answered
// again, and the receipt line and the key both ask it — so the sentence on the
// screen can never offer what the door would refuse.
//
// [session.Question.Revisable] is the engine's half of it, keyed on what the
// answer DID rather than on which lane asked. What this adds is what the SURFACE
// needs to put the question back: the answers it was read with. A decision with
// none was answered in words, or was learnt from another window and never drawn
// here.
func questionCanChange(record questionRecord) bool {
	if record.withdrawn != "" || !record.reversible {
		return false
	}
	return record.question.Revisable() && len(record.question.Options) > 0
}

// undoableReceipt is the newest receipt that GRANTED something — a permission
// answered `always`, whose standing half can be handed back whole.
//
// `u undo` AND `c change` ARE NOT THE SAME ACT, which is why they are two keys.
// Changing an answer puts the question back and asks it again; undoing one takes
// back what it gave and asks nothing, because there is nothing to decide — a
// person who says "undo that" about a permission has already said what they
// want. So `u` is offered where a yes bought something standing, and `c`
// wherever a question can be put again.
func (a *app) undoableReceipt() (questionRecord, bool) {
	shown := a.questionRecordsShown()
	for at := len(shown) - 1; at >= 0; at-- {
		if record := shown[at]; questionCanUndo(record) {
			return record, true
		}
	}
	return questionRecord{}, false
}

// questionCanUndo is that reading, asked by the key and by the receipt line.
func questionCanUndo(record questionRecord) bool {
	if record.withdrawn != "" || !record.reversible {
		return false
	}
	if record.question.Kind != session.QuestionConsent || !record.question.Revisable() {
		return false
	}
	picked := ""
	if len(record.record.Picked) > 0 {
		picked = record.record.Picked[0]
	}
	action, ok := session.AnswerFromKey(session.QuestionConsent, picked)
	// ONLY A YES IS UNDONE. A denial gave nothing away, and an `allow once` is
	// already over — what is left to take back is the standing half.
	return ok && action.Allow && action.Scope == session.ConsentToolSession
}

// undoGrant hands back a permission the person has just given: the engine's memo
// and the account capability go through the one door that knows every store a
// yes was written into ([session.Agent.undoGrant], reached by a revision), and
// the saved rule in their own settings goes here, because the surface is what
// wrote it.
func (a *app) undoGrant(record questionRecord) tea.Cmd {
	q := record.question
	if a.priorAnswers == nil {
		a.priorAnswers = make(map[string]session.DecisionRecord, 1)
	}
	a.priorAnswers[questionTokenOf(q)] = record.record
	a.dropReceipt(record)
	tool := strings.TrimSpace(q.Subject.Name)
	if said := a.forgetAlways(tool); said != "" {
		a.note(said)
	}
	// THE REVISION IS AN ANSWER LIKE ANY OTHER and goes through the one door
	// (questionchange.go's own law): the same [session.Answer] a key press
	// makes, with `Revises` on it and the refusal spelled by the engine.
	return a.answerQuestion(questionShown{question: q, revising: true}, session.Answer{
		Key:     questionDenyKey,
		Revises: true,
	})
}

// questionDenyKey is the consent lane's `deny`, which is what taking a
// permission back MEANS — spelled from the lane's own answers rather than as a
// literal, so a renumbered row cannot silently undo the wrong thing.
var questionDenyKey = questionConsentDenyKey()

func questionConsentDenyKey() string {
	for _, option := range session.AnswerOptions(session.QuestionConsent) {
		if action, ok := session.AnswerFromKey(session.QuestionConsent, option.Key); ok && !action.Allow {
			return option.Key
		}
	}
	return ""
}

// forgetAlways takes one tool's standing approval out of the person's own
// settings — the inverse of [app.rememberAlways], through the same door the
// permissions panel drops a line with ([app.dropPermission]) — and answers what
// to say about it.
//
// THE SHELL RULE IS NAMED AND NOT GUESSED. A bash `always` writes a rule about a
// COMMAND SHAPE, and the receipt carries the tool's name and not the shape it
// was banked under; dropping "the bash rule" would be dropping whichever one a
// list reached first. So the memo for this session is taken back either way (the
// engine's half) and the person is told where the standing line still is.
func (a *app) forgetAlways(tool string) string {
	if tool == "" {
		return ""
	}
	if tool == consentBash {
		return undoneBashWord
	}
	if err := a.dropPermission(permRule{kind: permTool, name: tool}); err != nil {
		// THE SESSION'S OWN MEMO IS STILL GONE, which is what the revision did
		// before this ran; what could not be written is the standing row, and
		// saying so is this surface's own honesty law ([app.approvalsReloaded]).
		return undoneWord + " · " + err.Error()
	}
	return undoneWord
}

// The two sentences an undo says, spelled once because the manual quotes them.
const (
	undoneWord     = "taken back · this tool asks again"
	undoneBashWord = "taken back for this conversation · the saved command rule is in /permissions"
)

// changeAnswer puts one settled question back on the block.
//
// THE CLOCK DOES NOT COME BACK WITH IT. Whatever the policy was the first time,
// this question is now in front of somebody who has just pressed a key about it,
// and a countdown that started again would be this surface taking the decision
// away from the person who came back to make it.
//
// AND THE RECEIPT COMES OFF. One decision is one row: the question is open
// again, and a receipt under it saying what was decided is the old answer drawn
// as though it still stood. The new one is written when the new answer is given
// ([app.recordQuestion]).
func (a *app) changeAnswer(record questionRecord) {
	q := record.question
	q.Withdrawn = nil
	q.Deadline = time.Time{}
	q.Policy = session.Policy{Kind: session.PolicyAsk}
	// AND NOTHING IS WAITING ON IT. The turn that asked has long since been
	// answered and moved on — that is what the receipt was — so a row that still
	// said `waiting` would be telling somebody their work had stopped for a
	// decision they are only revisiting. Seen on the Spark, 2026-09-11.
	q.Blocking = session.Blocking{}
	q.Asked = a.now()
	if a.priorAnswers == nil {
		a.priorAnswers = make(map[string]session.DecisionRecord, 1)
	}
	a.priorAnswers[questionTokenOf(q)] = record.record
	a.dropReceipt(record)
	shown := questionShown{question: q, revising: true}
	a.raiseQuestion(shown)
	a.markQuestionShown(shown.token())
	a.touch()
}

// dropReceipt takes one receipt off the screen, by the question it was about.
func (a *app) dropReceipt(record questionRecord) {
	token := questionTokenOf(record.question)
	kept := a.questionRecords[:0]
	for _, held := range a.questionRecords {
		if held.withdrawn == "" && questionTokenOf(held.question) == token {
			continue
		}
		kept = append(kept, held)
	}
	a.questionRecords = kept
}
