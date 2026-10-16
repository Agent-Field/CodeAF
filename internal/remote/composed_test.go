package remote

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// composedAgent is the scripted agent with the door pictures are sent through.
type composedAgent struct {
	*fakeAgent
	submissions []session.Submission
}

func (c *composedAgent) SubmitComposed(ctx context.Context, message session.Submission) (session.SubmissionReceipt, <-chan session.Event, error) {
	c.mu.Lock()
	c.submissions = append(c.submissions, message)
	c.mu.Unlock()
	events, err := c.fakeAgent.Submit(ctx, message.Text)
	return session.SubmissionReceipt{MessageID: "msg_1", Delivery: session.DeliveryImage, Media: []session.MessageMedia{{ID: "at1", Width: 4}}}, events, err
}

// PICTURES AND THEIR MARKS CROSS THE WIRE ON THE SAME CALL AS THE WORDS, and the
// engine's receipt comes back on the answer that names the stream.
func TestSubmitCarriesPicturesAndAnnotationAndAnswersTheReceipt(t *testing.T) {
	agent := &composedAgent{fakeAgent: &fakeAgent{model: "m"}}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: agent}, false)}
	want := session.Submission{
		Text:  "redo this",
		Media: []session.SubmissionMedia{{ID: "at1", Path: "/w/a.png"}},
		Annotation: &session.Annotation{
			Original: "at1", ChangeOnlyMarked: true,
			Marks: []session.AnnotationMark{{Kind: "pin", At: &session.Point{X: 1, Y: 2}}},
		},
	}
	out, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(argsOf(want))})
	if err != nil {
		t.Fatal(err)
	}
	var ref StreamRef
	if err := json.Unmarshal(out, &ref); err != nil || ref.Stream == 0 || ref.Receipt == nil || ref.Receipt.MessageID != "msg_1" || len(ref.Receipt.Media) != 1 {
		t.Fatalf("answer = %s (%v)", out, err)
	}
	if len(agent.submissions) != 1 || !reflect.DeepEqual(agent.submissions[0].Media, want.Media) || !reflect.DeepEqual(agent.submissions[0].Annotation, want.Annotation) {
		t.Fatalf("the door was handed %+v", agent.submissions)
	}
	if !composedKnown(agent) || composedKnown(&fakeAgent{}) {
		t.Fatal("the welcome does not distinguish engines that take pictures")
	}
}

func TestAnEngineWithoutTheDoorRefusesPicturesAndRunsNothing(t *testing.T) {
	plain := &markedAgent{fakeAgent: &fakeAgent{model: "m"}}
	s := &server{out: io.Discard, session: NewSession(&Engine{Agent: plain}, false)}
	args := SubmitArgs{Text: "x", Media: []session.SubmissionMedia{{ID: "at1", Path: "/w/a.png"}}}
	if _, err := s.invoke(Frame{Kind: "call", ID: 1, Method: MethodSubmit, Payload: mustJSON(args)}); err == nil {
		t.Fatal("an engine without the door took a message with pictures")
	}
	if len(plain.sent) != 0 || len(plain.submissions) != 0 {
		t.Fatal("pictures were run as words alone")
	}
	// A correction and a queued message carry no pictures, whatever the engine.
	for _, method := range []string{MethodSteer, MethodFollowUp} {
		if _, err := s.invoke(Frame{Kind: "call", ID: 2, Method: method, Payload: mustJSON(args)}); err == nil {
			t.Fatalf("%s accepted pictures", method)
		}
	}
}

func TestAnOlderHostIsRefusedBeforeAComposedMessageCrossesTheWire(t *testing.T) {
	client := &Client{welcome: Welcome{Version: Version, MessageMarks: true}}
	agent := &Agent{c: client}
	message := session.Submission{Text: "x", Media: []session.SubmissionMedia{{ID: "at1", Path: "/w/a.png"}}}
	if _, _, err := agent.SubmitComposed(t.Context(), message); err == nil {
		t.Fatal("an older host was sent pictures")
	}
	if _, err := agent.SubmitMessage(t.Context(), message); err == nil {
		t.Fatal("SubmitMessage dropped pictures for an older host")
	}
	if _, err := agent.SteerMessage(message); err == nil {
		t.Fatal("a correction carried pictures")
	}
	if client.made.Load() != 0 {
		t.Fatal("a refused message crossed the wire")
	}
}

var _ composedDoor = (*session.Agent)(nil)
