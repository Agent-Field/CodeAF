package remote

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
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
// with no such door refuses the whole marked message.
func TestSubmitCarriesTheMessagesMarkToTheAgentsDoor(t *testing.T) {
	said := ""
	pasteBytes, pasteLines := int64(12), 2
	attachments := []session.MessageAttachment{{ID: "at1", Kind: "file", Name: "notes.md", Path: "notes.md"}, {ID: "at2", Kind: "paste", Name: "Pasted text", Bytes: &pasteBytes, Lines: &pasteLines}}
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

	// Reads alone are a mark: they reach the door whole, every count intact.
	reads := []session.MessageRead{{
		Source: session.MessageReference{Kind: "collection", ID: "c1", Name: "Specs"},
		Count:  200, Missing: 1, Unavailable: 2, Unread: 3, More: true,
		Warnings: []string{"two could not be asked"},
		Items:    []session.MessageReference{{Kind: "file", ID: "r1", Name: "a.md", Owner: "fs", Path: "docs/a.md", URL: "https://x/a"}},
	}}
	s.pending = nil
	if _, err := s.invoke(Frame{Kind: "call", ID: 3, Method: MethodSubmit, Payload: mustJSON(SubmitArgs{Text: "read these", Reads: reads})}); err != nil {
		t.Fatal(err)
	}
	if len(marked.submissions) != 2 || marked.submissions[1].Said != nil || !reflect.DeepEqual(marked.submissions[1].Reads, reads) {
		t.Fatalf("the marked door was handed %+v", marked.submissions)
	}

	plain := &fakeAgent{model: "m"}
	s = &server{out: io.Discard, session: NewSession(&Engine{Agent: plain}, false)}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(SubmitArgs{Text: "Attached file: notes.md", Said: &said, Attachments: attachments})}); err == nil {
		t.Fatal("an engine without the marked door accepted a marked message")
	}
	if len(plain.sent) != 0 {
		t.Fatalf("an agent with no door for the mark was sent %q", plain.sent)
	}
}

func TestAnOlderHostIsRefusedBeforeAMarkedMessageCrossesTheWire(t *testing.T) {
	client := &Client{welcome: Welcome{Version: Version}}
	agent := &Agent{c: client}
	reads := []session.MessageRead{{Source: session.MessageReference{Kind: "collection", ID: "c1"}, Count: 1}}
	if _, err := agent.SubmitMessage(t.Context(), session.Submission{Text: "read these", Reads: reads}); err == nil {
		t.Fatal("an older host was sent a marked message")
	}
	if client.made.Load() != 0 {
		t.Fatal("a refused message crossed the wire")
	}
	if !messageMarksKnown(&markedAgent{fakeAgent: &fakeAgent{}}) || messageMarksKnown(&fakeAgent{}) {
		t.Fatal("the welcome does not distinguish engines that keep a mark")
	}
}

// THE CREATION CONTEXT CROSSES THE WIRE AS PRESENCE: a team id, an explicit
// none ({}) and absence are three different things on the wire and at the
// door, and absence is never turned into none.
func TestTheCreationContextCrossesTheWireOnSubmitAsPresence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		given *session.CreationContext
		wire  string
	}{
		{"team", &session.CreationContext{Team: "aaaaaaaaaaaa"}, `"creation":{"team":"aaaaaaaaaaaa"}`},
		{"none", &session.CreationContext{}, `"creation":{}`},
		{"absent", nil, ""},
	} {
		raw := mustJSON(argsOf(session.Submission{Text: "hi", Creation: tc.given}))
		if tc.wire == "" && strings.Contains(string(raw), "creation") || tc.wire != "" && !strings.Contains(string(raw), tc.wire) {
			t.Fatalf("%s: the wire said %s", tc.name, raw)
		}
		marked := &markedAgent{fakeAgent: &fakeAgent{model: "m"}}
		s := &server{out: io.Discard, session: NewSession(&Engine{Agent: marked}, false)}
		if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: raw}); err != nil {
			t.Fatal(err)
		}
		if tc.given == nil {
			// Words alone reach the plain door, and nothing states a context.
			if len(marked.submissions) != 0 {
				t.Fatalf("absent: the mark door was handed %+v", marked.submissions)
			}
			continue
		}
		got := marked.submissions
		if len(got) != 1 || got[0].Creation == nil || got[0].Creation.Team != tc.given.Team || got[0].Creation == tc.given {
			t.Fatalf("%s: the door was handed %+v", tc.name, got)
		}
	}
	// An older engine's JSON — no field — decodes as absent.
	var old SubmitArgs
	if err := json.Unmarshal([]byte(`{"text":"hi"}`), &old); err != nil || old.Creation != nil || old.submission().Creation != nil {
		t.Fatalf("an older client's call decoded with a context: %+v %v", old, err)
	}
}
