package remote

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type renderFake struct {
	page    session.RenderPage
	records map[string]session.RenderRecord
	err     error
}

func (f *renderFake) Renders(int, string) (session.RenderPage, error) { return f.page, f.err }
func (f *renderFake) Render(id string) (session.RenderRecord, error) {
	if f.err != nil {
		return session.RenderRecord{}, f.err
	}
	if r, ok := f.records[id]; ok {
		return r, nil
	}
	return session.RenderRecord{}, session.ErrRenderNotFound
}
func (f *renderFake) CancelRender(id string) (session.RenderCancel, error) {
	r, err := f.Render(id)
	return session.RenderCancel{Outcome: session.RenderOutcomeCancelled, Render: r}, err
}

// The three refusals a caller must tell apart survive the wire as themselves,
// and everything else stays an ordinary error.
func TestRenderWireKeepsTypedRefusalsAndRecords(t *testing.T) {
	cost := 0.5
	record := session.RenderRecord{ID: "rnd_1", Kind: "video", State: session.RenderCancelled, Started: time.Unix(1_700_000_000, 0).UTC(), Charge: session.RenderCharged, CostUSD: &cost, Upstream: "unknown"}
	fake := &renderFake{page: session.RenderPage{Renders: []session.RenderRecord{record}, Next: "r2.9", Incomplete: true}, records: map[string]session.RenderRecord{"rnd_1": record}}

	client, engine := newEngine(t)
	call := func(method string, payload any) renderAnswer {
		frame := Frame{Method: method}
		frame.Payload, _ = json.Marshal(payload)
		raw, err := dispatchRender(fake, frame)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		var answer renderAnswer
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatal(err)
		}
		return answer
	}

	engine.answers[MethodRenderList] = call(MethodRenderList, RenderListArgs{})
	engine.answers[MethodRenderGet] = call(MethodRenderGet, RenderIDArgs{ID: "rnd_1"})
	engine.answers[MethodRenderCancel] = call(MethodRenderCancel, RenderIDArgs{ID: "rnd_1"})
	page, err := client.Agent().Renders(10, "")
	if err != nil || !reflect.DeepEqual(page, fake.page) {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if got, err := client.Agent().Render("rnd_1"); err != nil || !reflect.DeepEqual(got, record) {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if got, err := client.Agent().CancelRender("rnd_1"); err != nil || got.Outcome != session.RenderOutcomeCancelled || got.Render.ID != "rnd_1" {
		t.Fatalf("cancel = %+v, %v", got, err)
	}

	for refusal, want := range map[error]error{
		session.ErrRenderNotFound: session.ErrRenderNotFound,
		session.ErrRenderBusy:     session.ErrRenderBusy,
		session.ErrRenderCursor:   session.ErrRenderCursor,
	} {
		fake.err = refusal
		engine.answers[MethodRenderGet] = call(MethodRenderGet, RenderIDArgs{ID: "x"})
		if _, err := client.Agent().Render("x"); !errors.Is(err, want) {
			t.Fatalf("refusal %v came back as %v", refusal, err)
		}
	}
	fake.err = errors.New("disk on fire")
	if _, err := dispatchRender(fake, Frame{Method: MethodRenderList}); err == nil {
		t.Fatal("an ordinary failure was swallowed into an answer")
	}
	if _, err := dispatchRender(struct{}{}, Frame{Method: MethodRenderList}); err == nil {
		t.Fatal("a conversation without renders answered")
	}
}

// The render event carries its whole record across a JSON hop.
func TestRenderEventSurvivesTheWire(t *testing.T) {
	cost := 0.1
	event := session.Event{Kind: session.EventRenderUpdate, Render: &session.RenderRecord{ID: "rnd_2", State: session.RenderFinished, Charge: session.RenderCharged, CostUSD: &cost, Committed: true, Path: "/a.mp4"}}
	raw, err := json.Marshal(WireEvent(event))
	if err != nil {
		t.Fatal(err)
	}
	var wired EventWire
	if err := json.Unmarshal(raw, &wired); err != nil {
		t.Fatal(err)
	}
	back := wired.Unwire()
	if back.Kind != session.EventRenderUpdate || back.Render == nil || !reflect.DeepEqual(*back.Render, *event.Render) {
		t.Fatalf("event = %+v", back)
	}
}

func TestRenderMethodsAreClassifiedAndWatchersMayOnlyRead(t *testing.T) {
	if !watcherMay(MethodRenderList) || !watcherMay(MethodRenderGet) || watcherMay(MethodRenderCancel) {
		t.Fatal("a watcher may read renders and may not cancel one")
	}
	if classify(MethodRenderList) != classGetter || classify(MethodRenderGet) != classGetter || classify(MethodRenderCancel) == classGetter {
		t.Fatal("reads are getters; a cancel is an act")
	}
}
