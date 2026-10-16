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

// AN ENGINE THAT HAS NOT SAID IT KEEPS AN ATTACHMENT'S SOURCE IS NOT SENT ONE:
// it would run the words and drop the recorded range unread.
func TestAnOlderHostIsRefusedBeforeAnAttachmentSourceCrossesTheWire(t *testing.T) {
	from, to := int64(0), int64(1000)
	ranged := session.MessageAttachment{ID: "at1", Kind: "file", Path: "/o/a.range-0-1000.wav", Source: &session.MediaReference{SourceID: "src_1", Version: "sha256:v", FromMS: &from, ToMS: &to}}
	message := session.Submission{Text: "x", Attachments: []session.MessageAttachment{ranged}}
	old := &Client{welcome: Welcome{Version: Version, MessageMarks: true, SteerMarks: true, FollowUpMarks: true, ComposedMessages: true, MessageReceipts: true, MessageClientIDs: true}}
	agent := &Agent{c: old}
	if _, err := agent.SubmitMessage(t.Context(), message); err == nil {
		t.Fatal("SubmitMessage sent a source to an older host")
	}
	if _, _, err := agent.SubmitComposed(t.Context(), message); err == nil {
		t.Fatal("SubmitComposed sent a source to an older host")
	}
	if _, err := agent.SteerMessage(message); err == nil {
		t.Fatal("SteerMessage sent a source to an older host")
	}
	if _, err := agent.FollowUpMessage(message); err == nil {
		t.Fatal("FollowUpMessage sent a source to an older host")
	}
	if old.made.Load() != 0 {
		t.Fatal("a refused message crossed the wire")
	}
	// An ordinary attachment is never held back by this word.
	plain := session.Submission{Text: "x", Attachments: []session.MessageAttachment{{ID: "at1", Kind: "file", Path: "a.txt"}}}
	if err := agent.refuseUnkeptClientID(plain); err != nil {
		t.Fatalf("an ordinary attachment was refused: %v", err)
	}
	if !(&Agent{c: &Client{welcome: Welcome{AttachmentSources: true}}}).AttachmentSourcesKnown() {
		t.Fatal("the welcome's word is not read")
	}
}

func TestAnAttachmentSourceCrossesTheWireAndIsAdvertisedByAHostThatKeepsIt(t *testing.T) {
	marked := &markedAgent{fakeAgent: &fakeAgent{model: "m"}}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: marked}, false)}
	from, to := int64(10), int64(20)
	message := session.Submission{Text: "hello", Attachments: []session.MessageAttachment{{ID: "at1", Kind: "file", Path: "/o/a.wav", Source: &session.MediaReference{SourceID: "src_1", Path: "/w/a.wav", Version: "sha256:v", FromMS: &from, ToMS: &to}}}}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(argsOf(message))}); err != nil {
		t.Fatal(err)
	}
	got := marked.submissions
	if len(got) != 1 || got[0].Attachments[0].Source == nil || *got[0].Attachments[0].Source.FromMS != 10 || *got[0].Attachments[0].Source.ToMS != 20 || got[0].Attachments[0].Source.Version != "sha256:v" {
		t.Fatalf("the door was handed %+v", got)
	}
	if !s.session.welcomeLocked(s).AttachmentSources {
		t.Fatal("an engine that keeps the source does not say so in its welcome")
	}
}
