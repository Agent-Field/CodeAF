package remote

import (
	"io"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CLIENT ID CROSSES THE WIRE ON THE SAME CALL AS ITS WORDS and reaches the
// agent's own door with Said still nil: an id alone makes no words "authored".
func TestSubmitCarriesTheClientIDToTheAgentsDoor(t *testing.T) {
	marked := &markedAgent{fakeAgent: &fakeAgent{model: "m"}}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: marked}, false)}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(argsOf(session.Submission{Text: "hello", ClientID: "c-1"}))}); err != nil {
		t.Fatal(err)
	}
	if len(marked.submissions) != 1 || marked.submissions[0].ClientID != "c-1" || marked.submissions[0].Text != "hello" || marked.submissions[0].Said != nil {
		t.Fatalf("the door was handed %+v", marked.submissions)
	}
	if !s.session.welcomeLocked(s).MessageClientIDs {
		t.Fatal("an engine that keeps the id does not say so in its welcome")
	}
}

// AN ENGINE THAT HAS NOT SAID IT KEEPS THE ID IS NOT SENT ONE: it would run the
// words and drop the id unread.
func TestAnOlderHostIsRefusedBeforeAClientIDCrossesTheWire(t *testing.T) {
	client := &Client{welcome: Welcome{Version: Version, MessageMarks: true, SteerMarks: true, FollowUpMarks: true, ComposedMessages: true, MessageReceipts: true}}
	agent := &Agent{c: client}
	message := session.Submission{Text: "x", ClientID: "c-1"}
	if _, err := agent.SubmitMessage(t.Context(), message); err == nil {
		t.Fatal("SubmitMessage sent an id to an older host")
	}
	if _, _, err := agent.SubmitComposed(t.Context(), message); err == nil {
		t.Fatal("SubmitComposed sent an id to an older host")
	}
	if _, err := agent.SteerMessage(message); err == nil {
		t.Fatal("SteerMessage sent an id to an older host")
	}
	if _, err := agent.FollowUpMessage(message); err == nil {
		t.Fatal("FollowUpMessage sent an id to an older host")
	}
	if client.made.Load() != 0 {
		t.Fatal("a refused message crossed the wire")
	}
	if !(&Agent{c: &Client{welcome: Welcome{MessageClientIDs: true}}}).MessageClientIDsKnown() {
		t.Fatal("the welcome's word is not read")
	}
}
