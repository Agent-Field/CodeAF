package appwire

import (
	"encoding/json"
	"errors"
	"testing"
)

// removerEngine is an Engine that also removes staged attachments, recording
// what it was asked to drop.
type removerEngine struct {
	fakeEngine
	agent, id string
}

func (r *removerEngine) RemoveAttachment(agent, id string) error {
	r.agent, r.id = agent, id
	return nil
}

// ATTACHMENTS.REMOVE reaches an engine that keeps staged attachments, refuses
// an empty id, and is unknown to an engine that does not.
func TestAttachmentsRemoveRoute(t *testing.T) {
	m, ok := methods["attachments.remove"]
	if !ok {
		t.Fatal("attachments.remove is not registered")
	}
	eng := &removerEngine{}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","id":"at2"}`)); err != nil {
		t.Fatal(err)
	}
	if eng.agent != "a1" || eng.id != "at2" {
		t.Fatalf("the engine was asked to drop %q %q", eng.agent, eng.id)
	}
	var wire *Error
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1"}`)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
		t.Fatalf("a missing id answered %v", err)
	}
	if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1","id":"at2"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("an engine without staging answered %v", err)
	}
}
