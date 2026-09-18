package tui3

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

type replacingQuestionScript struct {
	*questionScript
	replaced []session.Answer
}

func (q *replacingQuestionScript) ReplaceQuestion(_ context.Context, answer session.Answer) (<-chan session.Event, error) {
	q.replaced = append(q.replaced, answer)
	events := make(chan session.Event, 2)
	events <- session.Event{Kind: session.EventTextDelta, Text: "Updated response"}
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	return events, nil
}

func TestQuestionConversationOtherUsesTheReplacementDoor(t *testing.T) {
	lab := newQuestionLab(t)
	agent := &replacingQuestionScript{questionScript: lab.agent}
	lab.a.agent = agent
	lab.raise(consentAsk())
	lab.tick(questionSettle * 2)
	lab.press("o")
	lab.a.input.setText("explain instead")
	lab.press("enter")
	if len(agent.replaced) != 1 || agent.replaced[0].Change != "explain instead" || len(lab.answer) != 0 {
		t.Fatalf("replacement=%+v answers=%+v", agent.replaced, lab.answer)
	}
	if len(lab.a.questions) != 0 {
		t.Fatal("replacement kept the old dialog")
	}
	if !strings.Contains(entriesText(lab.a.entries), "explain instead") {
		t.Fatal("updated request missing from transcript")
	}
}

func TestQuestionConversationTranscriptKeepsIndependentReplies(t *testing.T) {
	lab := newQuestionLab(t)
	a := lab.a
	a.turn = 1
	a.say("Original partial reply")
	originalLive := a.live
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 1, Words: "why?"})
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 2, Event: &session.Event{Kind: session.EventTextDelta, Text: "Because "}})
	a.say(" continues")
	a.settleTurn()
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 3, Event: &session.Event{Kind: session.EventTextDelta, Text: "it matters."}})
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 4, Event: &session.Event{Kind: session.EventTurnDone}})
	if a.entries[originalLive].text != "Original partial reply continues" {
		t.Fatalf("clarification changed original reply: %+v", a.entries)
	}
	text := entriesText(a.entries)
	if !strings.Contains(text, "clarify: why?") || !strings.Contains(text, "Because it matters.") {
		t.Fatal(text)
	}
	a.discussionEvent(&session.QuestionDiscussion{ID: "one", Seq: 3, Event: &session.Event{Kind: session.EventTextDelta, Text: "it matters."}})
	if entriesText(a.entries) != text {
		t.Fatal("replayed clarification duplicated its reply")
	}
}

func TestQuestionConversationNestedInputTakesPriorityAndReturns(t *testing.T) {
	lab := newQuestionLab(t)
	original := consentAsk()
	lab.raise(original)
	nested := consentAsk()
	nested.Ref = "clarify-1/7"
	nested.ClarificationDepth = 1
	nested.Batch = "clarify-1/step"
	lab.raise(nested)
	head, _ := lab.a.questionHead()
	if head.question.Ref != nested.Ref {
		t.Fatal("original stayed ahead of clarification")
	}
	lab.a.closeQuestion(head, session.Answer{Key: "3"})
	head, _ = lab.a.questionHead()
	if head.question.Ref != "" || head.question.ID != original.ID {
		t.Fatal("original did not return")
	}
}

func TestQuestionConversationClarifyingDoesNotMarkTheToolAllowed(t *testing.T) {
	lab := newQuestionLab(t)
	ev := session.Event{Kind: session.EventConsentRequest, ID: 7, Tool: "bash", CallID: "call", Hint: "echo hello"}
	lab.a.askConsent(ev)
	head, _ := lab.a.questionHead()
	lab.spend(lab.a.askBack(head, "", "why?"))
	for _, e := range lab.a.entries {
		if e.kind == entryTool && e.decision != "" {
			t.Fatalf("clarification marked approval: %s", e.decision)
		}
	}
}

func entriesText(entries []entry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e.text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestQuestionConversationReplacementDrainsStoppedToolBeforeNewReply(t *testing.T) {
	lab := newQuestionLab(t)
	agent := &replacingQuestionScript{questionScript: lab.agent}
	lab.a.agent = agent
	lab.a.turn = 1
	lab.a.stream = make(chan session.Event)
	lab.a.ingest(session.Event{Kind: session.EventToolBegin, CallID: "old", Tool: "bash"})
	q := consentAsk()
	q.Subject.CallID = "old"
	lab.raise(q)
	head, _ := lab.a.questionHead()
	lab.spend(lab.a.replaceQuestion(head, "explain instead"))
	if lab.a.questionReplacement == nil {
		t.Fatal("new stream replaced the old stream before its last events")
	}
	lab.a.ingest(session.Event{Kind: session.EventToolFailed, CallID: "old", Tool: "bash", Hint: "ended before an answer"})
	drive(t, lab.a, streamClosedMsg{gen: lab.a.gen})
	if lab.a.questionReplacement != nil {
		t.Fatal("replacement did not start after the old stream closed")
	}
	if lab.a.entries[0].status != toolFailed {
		t.Fatal("the stopped tool still claimed it needed approval")
	}
}
