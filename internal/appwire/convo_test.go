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

// readerEngine is an Engine that reads files, recording what it was asked.
type readerEngine struct {
	fakeEngine
	agent, path string
	offset      int64
	limit       int
}

func (r *readerEngine) ReadFile(agent, path string, offset int64, limit int) (FileRead, error) {
	r.agent, r.path, r.offset, r.limit = agent, path, offset, limit
	return FileRead{Path: path, Kind: ReadText}, nil
}

// FILES.READ reaches an engine that reads, clamps the page to FilePageMax,
// refuses a missing path or a negative offset, and is unknown elsewhere.
func TestFilesReadRoute(t *testing.T) {
	m, ok := methods["files.read"]
	if !ok {
		t.Fatal("files.read is not registered")
	}
	eng := &readerEngine{}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","path":"-x.go","offset":4,"limit":999999}`)); err != nil {
		t.Fatal(err)
	}
	if eng.agent != "a1" || eng.path != "-x.go" || eng.offset != 4 || eng.limit != FilePageMax {
		t.Fatalf("the engine was asked %+v", eng)
	}
	var wire *Error
	for _, bad := range []string{`{"agent":"a1"}`, `{"agent":"a1","path":"x","offset":-1}`, `{"agent":"a1","path":"x","limit":-1}`} {
		if _, err := m.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("%s answered %v", bad, err)
		}
	}
	if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1","path":"x"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("an engine without reading answered %v", err)
	}
}
