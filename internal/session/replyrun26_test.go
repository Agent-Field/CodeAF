package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the run 26 replay ───────────────────────────────────────────────────────
//
// testdata/run26-replies/cafe-site-t2.json is café-site t2 of the
// connected-canvas qloop's run 26 (deepseek-v4.1-flash, engine b3a842964,
// which carries #2848), from the person's message to the reply the turn first
// ended on, exactly as the session journal kept them, with the harness's
// instructions after the person's words left out. Its .check.json beside it
// holds what came next in the same journal: the continuation the check sent,
// the reader's two lines rebuilt from it under their kinds, and the reply the
// turn ended on after it.
//
// The run's report said the check never fired: "its reply said more than it
// checked this turn" was in no screenshot and no kept engine file. It fired.
// The journal holds `reply-contradicts-look` sent-back on this very turn, and
// on 16 of the run's 28 turns in all; the model then rendered and looked at
// the four pages at phone width before it replied again. What never happened
// is the line reaching anyone: the app dropped every check's line by its words
// (codeaf app's pump), and the engine writes a notice into no record.

type run26Check struct {
	ReaderAnswer string `json:"reader_answer"`
	Continuation string `json:"continuation"`
	SecondReply  string `json:"second_reply"`
}

func run26Turn(t *testing.T, name string) ([]ai.Message, run26Check) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "run26-replies", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []ai.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) < 2 || messages[0].Role != "user" || messages[len(messages)-1].Role != "assistant" {
		t.Fatalf("%s is not one turn from the person's message to a reply", name)
	}
	data, err = os.ReadFile(filepath.Join("testdata", "run26-replies", name+".check.json"))
	if err != nil {
		t.Fatal(err)
	}
	var check run26Check
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatal(err)
	}
	return messages, check
}

// THE CHECK SENDS CAFÉ T2 BACK AS IT DID IN THE RUN, AND ITS LINE IS TYPED.
// Replayed through [Agent.replyLooksBack] with the reader answering what run
// 26's reader answered, the turn is sent back with the same continuation the
// journal kept, and the one line said while it works carries
// [NoticeKindSentBack] — the fact the app reads to keep it, since its words
// alone are the same words the app drops for the completion check's carry-on
// and the nudges.
func TestRun26CafeSentBackWithATypedLine(t *testing.T) {
	messages, check := run26Turn(t, "cafe-site-t2")
	claim := "Checked all four pages on desktop and at phone width"
	agent, asked, _ := replayAgent(t, messages, func(page string) string {
		if strings.Contains(page, claim) && strings.Contains(page, "it stacks very cleanly") {
			return check.ReaderAnswer
		}
		return "NONE"
	})
	var events []Event
	hub := &eventHub{observe: func(event Event) { events = append(events, event) }}
	said := strings.TrimSpace(partsText(messages[len(messages)-1]))
	if !strings.Contains(said, claim) {
		t.Fatalf("the replayed reply does not say %q", claim)
	}
	if !agent.replyLooksBack(context.Background(), hub, &checkpointMeter{}, said) {
		t.Fatal("café t2 was not sent back")
	}
	if asked.Load() != 1 {
		t.Fatalf("the reader was asked %d times; want 1", asked.Load())
	}
	snapshot := agent.snapshot()
	if lead := messageText(snapshot[len(snapshot)-1]); lead != check.Continuation {
		t.Fatalf("the continuation differs from the one run 26 kept:\n got %q\nwant %q", lead, check.Continuation)
	}
	var lines []Event
	for _, event := range events {
		if event.Kind == EventNotice {
			lines = append(lines, event)
		}
	}
	if len(lines) != 1 || lines[0].Text != replyCheckNote || lines[0].NoticeKind != NoticeKindSentBack {
		t.Fatalf("the check said %+v; want one %q line typed %q", lines, replyCheckNote, NoticeKindSentBack)
	}
}

// EVERY CHECK ON A FINISHED REPLY TYPES ITS LINE, because all of them come
// through [Agent.sendBack]; the nudges and the completion check's carry-on,
// which the app still leaves out, stay untyped.
func TestEverySendBackLineIsTypedAndNoNudgeIs(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, note := range []string{replyStaleLookNote, replyCheckNote, replyUnshownNote, failureCheckNote, handoffCheckNote, handoffPromiseNote} {
		var events []Event
		hub := &eventHub{observe: func(event Event) { events = append(events, event) }}
		agent.sendBack(hub, "probe", note, "[carry on] probe")
		if len(events) != 1 || events[0].NoticeKind != NoticeKindSentBack || !IsCarryOnNotice(events[0].Text) {
			t.Fatalf("%q went out as %+v", note, events)
		}
	}
}
