package remote

import (
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// markedAgent is the scripted agent with the door a mark is recorded through.
type markedAgent struct {
	*fakeAgent
	submissions []session.Submission
}

func (m *markedAgent) SubmitMessage(ctx context.Context, message session.Submission) (<-chan session.Event, error) {
	m.mu.Lock()
	m.submissions = append(m.submissions, message)
	m.mu.Unlock()
	return m.fakeAgent.Submit(ctx, message.Text)
}

// A MESSAGE'S MARK CROSSES THE WIRE ON THE SAME CALL AS ITS WORDS. Submit with
// the person's words and attachments reaches the agent's own door for them,
// whole; Submit without them is the plain Submit it always was; and an agent
// with no such door — an engine from before the mark — runs the words alone.
func TestSubmitCarriesTheMessagesMarkToTheAgentsDoor(t *testing.T) {
	said := ""
	attachments := []session.MessageAttachment{{ID: "at1", Kind: "file", Name: "notes.md", Path: "notes.md"}, {ID: "at2", Kind: "paste", Name: "Pasted text", Bytes: 12, Lines: 2}}
	marked := &markedAgent{fakeAgent: &fakeAgent{model: "m"}}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: marked}, false)}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(SubmitArgs{Text: "Attached file: notes.md", Said: &said, Attachments: attachments})}); err != nil {
		t.Fatal(err)
	}
	s.pending = nil
	if _, err := s.invoke(Frame{Kind: "call", ID: 2, Method: MethodSubmit, Payload: mustJSON(SubmitArgs{Text: "plain words"})}); err != nil {
		t.Fatal(err)
	}
	if len(marked.submissions) != 1 || marked.submissions[0].Text != "Attached file: notes.md" || marked.submissions[0].Said == nil || *marked.submissions[0].Said != "" ||
		!reflect.DeepEqual(marked.submissions[0].Attachments, attachments) {
		t.Fatalf("the marked door was handed %+v", marked.submissions)
	}
	if !reflect.DeepEqual(marked.sent, []string{"Attached file: notes.md", "plain words"}) {
		t.Fatalf("the agent was sent %q", marked.sent)
	}

	plain := &fakeAgent{model: "m"}
	s = &server{out: io.Discard, session: NewSession(&Engine{Agent: plain}, false)}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(SubmitArgs{Text: "Attached file: notes.md", Said: &said, Attachments: attachments})}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plain.sent, []string{"Attached file: notes.md"}) {
		t.Fatalf("an agent with no door for the mark was sent %q", plain.sent)
	}
}
