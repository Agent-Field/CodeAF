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

type differEngine struct {
	fakeEngine
	agent, path, base string
}

func (d *differEngine) DiffFile(agent, path, base string) (FileDiff, error) {
	d.agent, d.path, d.base = agent, path, base
	return FileDiff{Path: path, State: DiffUnchanged}, nil
}

// FILES.DIFF reaches an engine that diffs with an optional base, refuses a
// missing path and a base git could read as an option or a range, and is
// unknown elsewhere.
func TestFilesDiffRoute(t *testing.T) {
	m, ok := methods["files.diff"]
	if !ok {
		t.Fatal("files.diff is not registered")
	}
	eng := &differEngine{}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","path":"-x.go"}`)); err != nil || eng.base != "" || eng.path != "-x.go" {
		t.Fatalf("no base: %v, %+v", err, eng)
	}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","path":"x","base":"main"}`)); err != nil || eng.base != "main" {
		t.Fatalf("a base: %v, %+v", err, eng)
	}
	var wire *Error
	for _, bad := range []string{`{"agent":"a1"}`, `{"agent":"a1","path":"x","base":"--output=y"}`, `{"agent":"a1","path":"x","base":"a..b"}`} {
		if _, err := m.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("%s answered %v", bad, err)
		}
	}
	if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1","path":"x"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("an engine without diffing answered %v", err)
	}
}

// completerEngine is an Engine that completes paths, recording what it was
// asked and answering with no entries at all.
type completerEngine struct {
	fakeEngine
	agent, path string
	limit       int
}

func (c *completerEngine) CompletePath(agent, path string, limit int) (PathCompletion, error) {
	c.agent, c.path, c.limit = agent, path, limit
	return PathCompletion{Dir: "/x"}, nil
}

// FILES.COMPLETE reaches an engine that completes paths with or without an
// agent, defaults and clamps the limit, answers entries as a list even when
// there are none, refuses a negative limit or a path over two lines, and is
// unknown to an engine that does not complete.
func TestFilesCompleteRoute(t *testing.T) {
	m, ok := methods["files.complete"]
	if !ok {
		t.Fatal("files.complete is not registered")
	}
	eng := &completerEngine{}
	got, err := m.call(eng, json.RawMessage(`{"path":"~/code/"}`))
	if err != nil {
		t.Fatal(err)
	}
	if eng.agent != "" || eng.path != "~/code/" || eng.limit != PathCompleteLimit {
		t.Fatalf("no agent, no limit: the engine was asked %+v", eng)
	}
	raw, _ := json.Marshal(got)
	if string(raw) != `{"dir":"/x","entries":[]}` {
		t.Fatalf("an empty answer is %s", raw)
	}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","path":"./in","limit":99999}`)); err != nil || eng.agent != "a1" || eng.limit != PathCompleteMax {
		t.Fatalf("a big limit: %v, %+v", err, eng)
	}
	var wire *Error
	for _, bad := range []string{`{"path":"x","limit":-1}`, `{"path":"a\nb"}`} {
		if _, err := m.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("%s answered %v", bad, err)
		}
	}
	if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"path":"x"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("an engine without completion answered %v", err)
	}
}

// checkoutEngine is an Engine that reads a checkout, recording the query.
type checkoutEngine struct {
	fakeEngine
	q CheckoutQuery
}

func (c *checkoutEngine) CheckoutStatus(q CheckoutQuery) (CheckoutStatus, error) {
	c.q = q
	return CheckoutStatus{Repo: true, Land: CheckoutLand{Available: true}}, nil
}

// CHECKOUT.STATUS bounds its page, refuses what is malformed, is unknown on an
// engine that cannot read a checkout, and ALWAYS says there is no land: an
// engine cannot claim a landing this protocol has no door for.
func TestCheckoutStatusRoute(t *testing.T) {
	m, ok := methods["checkout.status"]
	if !ok {
		t.Fatal("checkout.status is not registered")
	}
	eng := &checkoutEngine{}
	got, err := m.call(eng, json.RawMessage(`{"agent":"a1"}`))
	if err != nil || eng.q.Limit != CheckoutPageDefault {
		t.Fatalf("default limit: %v %+v", err, eng.q)
	}
	status := got.(*CheckoutStatus)
	if status.Land.Available || status.Land.Reason != CheckoutLandNoProducer || status.Entries == nil {
		t.Fatalf("answer %+v", status)
	}
	if _, err := m.call(eng, json.RawMessage(`{"agent":"a1","limit":100000,"cursor":"c"}`)); err != nil || eng.q.Limit != CheckoutPageMax || eng.q.Cursor != "c" {
		t.Fatalf("max limit: %v %+v", err, eng.q)
	}
	var wire *Error
	for _, bad := range []string{`{}`, `{"agent":"  "}`, `{"agent":"a1","limit":-1}`} {
		if _, err := m.call(eng, json.RawMessage(bad)); !errors.As(err, &wire) || wire.Code != CodeBadParams {
			t.Fatalf("%s answered %v", bad, err)
		}
	}
	if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"agent":"a1"}`)); !errors.As(err, &wire) || wire.Code != CodeUnknownMethod {
		t.Fatalf("an engine without a checkout answered %v", err)
	}
}
