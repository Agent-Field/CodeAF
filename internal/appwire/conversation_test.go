package appwire

import (
	"encoding/json"
	"testing"
)

type conversationEngine struct {
	fakeEngine
	called string
}

func (e *conversationEngine) ConversationPoints(id string) ([]ConversationPoint, error) {
	e.called = id
	return []ConversationPoint{{Point: 3, Words: "hello"}}, nil
}
func (e *conversationEngine) RewindConversation(id string, p int) (string, error) {
	e.called = id
	return "kept", nil
}
func (e *conversationEngine) UndoConversationRewind(id, token string) error {
	e.called = id
	return nil
}
func (e *conversationEngine) DeleteAgent(id string) error { e.called = id; return nil }
func (e *conversationEngine) DebugAgent(id string) (ConversationDebug, error) {
	e.called = id
	return ConversationDebug{Path: "/record"}, nil
}
func (e *conversationEngine) ManualAgent(id, q, c string) (uint64, error) {
	e.called = id
	return 7, nil
}

func TestConversationMethodsBoundsAndFeatures(t *testing.T) {
	e := &conversationEngine{}
	cases := []struct{ name, params string }{
		{"agents.rewind.points", `{"id":"a"}`},
		{"agents.rewind", `{"id":"a","point":3}`},
		{"agents.rewind.undo", `{"id":"a","token":"kept"}`},
		{"agents.delete", `{"id":"a","confirm":true}`},
		{"agents.debug", `{"id":"a"}`},
		{"agents.manual", `{"id":"a","question":"how?"}`},
	}
	hello := withConversationFeatures(Hello{}, e)
	if len(hello.Features) != len(cases) {
		t.Fatalf("features: %v", hello.Features)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e.called = ""
			if _, err := methods[tc.name].call(e, json.RawMessage(tc.params)); err != nil || e.called != "a" {
				t.Fatalf("call = %q, %v", e.called, err)
			}
			e.called = ""
			if _, err := methods[tc.name].call(e, json.RawMessage(`{}`)); codeOf(err) != CodeBadParams || e.called != "" {
				t.Fatalf("missing id = %v, called %q", err, e.called)
			}
			if _, err := methods[tc.name].call(&fakeEngine{}, json.RawMessage(tc.params)); codeOf(err) != CodeUnknownMethod {
				t.Fatalf("unsupported = %v", err)
			}
		})
	}
	for _, params := range []string{`{"id":"a"}`, `{"id":"a","point":0}`, `{"id":"a","point":-1}`} {
		if _, err := methods["agents.rewind"].call(e, json.RawMessage(params)); codeOf(err) != CodeBadParams {
			t.Fatalf("point %s: %v", params, err)
		}
	}
}

func TestDeleteRequiresConfirmationBeforeProducer(t *testing.T) {
	for _, params := range []string{`{"id":"a"}`, `{"id":"a","confirm":false}`, `{"id":"a","confirm":null}`} {
		e := &conversationEngine{}
		if _, err := methods["agents.delete"].call(e, json.RawMessage(params)); codeOf(err) != CodeBadParams || e.called != "" {
			t.Fatalf("%s: err=%v producer=%q", params, err, e.called)
		}
	}
}
