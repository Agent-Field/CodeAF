package agentfield

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// fakeHub is the control plane's link end: it welcomes, answers every
// up-frame ok, records what came up, and pushes what a test tells it to.
type fakeHub struct {
	t      *testing.T
	mu     sync.Mutex
	conn   *websocket.Conn
	ups    int
	frames []frame
	acks   map[string][]ackBody
	// statusCode is what a status or progress frame is answered.
	statusCode int
}

func (h *fakeHub) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer af-key-1" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := context.Background()
	raw, _ := encodeFrame(frame{T: tWelcome}, welcomeBody{LinkID: "lnk_test", HeartbeatS: 20})
	_ = conn.Write(ctx, websocket.MessageText, raw)
	h.mu.Lock()
	h.conn = conn
	h.ups++
	h.mu.Unlock()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		f, err := decodeFrame(raw)
		if err != nil {
			h.t.Errorf("the node sent a bad frame: %v", err)
			continue
		}
		h.mu.Lock()
		h.frames = append(h.frames, f)
		code := http.StatusOK
		if (f.T == tStatus || f.T == tProgress) && h.statusCode != 0 {
			code = h.statusCode
		}
		if f.T == tAck {
			var a ackBody
			_ = json.Unmarshal(f.Body, &a)
			h.acks[f.ID] = append(h.acks[f.ID], a)
		}
		h.mu.Unlock()
		if f.T != tAck {
			ok, _ := encodeFrame(frame{T: tOK, Seq: f.Seq, Exec: f.Exec}, okBody{Code: code})
			_ = conn.Write(ctx, websocket.MessageText, ok)
		}
	}
}

func (h *fakeHub) deliver(id string, exec string, body deliverBody) {
	h.t.Helper()
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	raw, err := encodeFrame(frame{T: tDeliver, ID: id, Exec: exec}, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, raw); err != nil {
		h.t.Fatal(err)
	}
}

func (h *fakeHub) ack(id string) []ackBody {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]ackBody(nil), h.acks[id]...)
}

func (h *fakeHub) upFrames(t string) []frame {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []frame
	for _, f := range h.frames {
		if f.T == t {
			out = append(out, f)
		}
	}
	return out
}

func (h *fakeHub) drop() {
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	_ = conn.Close(websocket.StatusServiceRestart, "restart")
}

type linkWorld struct {
	*world
	hub       *fakeHub
	cancelled []int
	httpDown  bool
}

// newLinkWorld is a LINK-ONLY node (no base_url) against a fake control
// plane that offers the link.
func newLinkWorld(t *testing.T) *linkWorld {
	t.Helper()
	lw := &linkWorld{world: &world{t: t}, hub: &fakeHub{t: t, acks: map[string][]ackBody{}}}
	lw.plane = newFakePlaneWith(t, "af-key-1", func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/nodes/{id}/link", lw.hub.serve)
	}, func() bool {
		lw.mu.Lock()
		defer lw.mu.Unlock()
		return lw.httpDown
	})
	floor, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lw.floor = floor
	conn := &Connection{Store: floor, Profile: t.TempDir()}
	s, err := Clean(Settings{URL: lw.plane.URL, Trust: factory.TierOwner})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Keep(s, "af-key-1", ""); err != nil {
		t.Fatal(err)
	}
	lw.node = &Node{Conn: conn, Store: floor, Version: NodeVersion, Build: "test",
		Cancel: func(_ context.Context, job int) error {
			lw.mu.Lock()
			lw.cancelled = append(lw.cancelled, job)
			lw.mu.Unlock()
			return nil
		}}
	if err := lw.node.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lw.node.Stop(); lw.node.Forget() })
	eventually(t, "the link up", func() bool { return lw.node.Health().Link == "up" })
	return lw
}

func TestLinkFrameCodec(t *testing.T) {
	raw, err := encodeFrame(frame{T: tAck, ID: "dlv_1", Exec: "exec_1", Seq: 7}, ackBody{Outcome: "done", Result: json.RawMessage(`{"job":3}`)})
	if err != nil {
		t.Fatal(err)
	}
	f, err := decodeFrame(raw)
	if err != nil || f.V != frameVersion || f.T != tAck || f.ID != "dlv_1" || f.Exec != "exec_1" || f.Seq != 7 || f.TS == "" {
		t.Fatalf("round trip: %+v %v", f, err)
	}
	var a ackBody
	if json.Unmarshal(f.Body, &a) != nil || a.Outcome != "done" || string(a.Result) != `{"job":3}` {
		t.Fatalf("body %s", f.Body)
	}
	for _, bad := range []string{`{"v":2,"t":"ack"}`, `{"v":1,"t":"teleport"}`, `nope`} {
		if _, err := decodeFrame([]byte(bad)); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
	if _, err := decodeFrame(make([]byte, maxFrame+1)); err == nil {
		t.Error("a frame over 1 MiB decoded")
	}
	if _, err := encodeFrame(frame{T: "teleport"}, nil); err == nil {
		t.Error("an unknown type encoded")
	}
	if u, _ := LinkURL("https://cp.example/base/", "codeaf-factory"); u != "wss://cp.example/base/api/v1/nodes/codeaf-factory/link" {
		t.Errorf("link url %s", u)
	}
}

// A LINK-ONLY NODE BINDS NOTHING and still takes a dispatch: it comes down
// the link, goes to the same intake route, is acked accepted, and the job's
// reports go back up the link rather than over HTTP.
func TestALinkOnlyNodeBindsNothingAndTakesADispatchDownTheLink(t *testing.T) {
	lw := newLinkWorld(t)
	if h := lw.node.Health(); h.Listening != "" {
		t.Fatalf("a link-only node listens on %q", h.Listening)
	}
	if reg := lw.plane.registrations()[0]; reg.BaseURL != "" {
		t.Fatalf("a link-only node registered base_url %q", reg.BaseURL)
	}
	input := json.RawMessage(dispatch("generic_bearer", map[string]any{"title": "PROTO: add Len"}, "idem-L"))
	lw.hub.deliver("dlv_1", "exec-L", deliverBody{Kind: "dispatch", Reasoner: ReasonerIntake, Input: input, IdempotencyKey: "idem-L", Headers: map[string]string{"X-Execution-ID": "exec-L"}})
	eventually(t, "the ack", func() bool { return len(lw.hub.ack("dlv_1")) == 1 })
	if a := lw.hub.ack("dlv_1")[0]; a.Outcome != "accepted" {
		t.Fatalf("ack %+v", a)
	}
	if h := lw.node.Health(); h.LastEventVia != "link" || !strings.Contains(h.LastEventWhat, "filed job") {
		t.Fatalf("health %+v", h)
	}
	open := lw.node.Open()
	if len(open) != 1 || open[0].ID != "exec-L" {
		t.Fatalf("open %+v", open)
	}
	eventually(t, "a progress frame up the link", func() bool { return len(lw.hub.upFrames(tProgress)) > 0 })
	if len(lw.plane.reportsFor("exec-L")) != 0 {
		t.Fatal("a linked node reported over HTTP")
	}

	// The pull request completes it, up the link.
	const pr = "https://github.com/santoshkumarradha/factory-demo/pull/9"
	job := open[0].Job
	_ = lw.floor.Update(job, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{URL: pr, Number: 9, State: factory.PROpen}
		return nil
	})
	inbox.Notify(context.Background(), lw.floor, job, inbox.Opened(pr))
	eventually(t, "the completion up the link", func() bool { return len(lw.hub.upFrames(tStatus)) > 0 })
	var u ExecutionUpdate
	_ = json.Unmarshal(lw.hub.upFrames(tStatus)[0].Body, &u)
	if u.Status != ExecSucceeded || u.Result["pr_url"] != pr || lw.hub.upFrames(tStatus)[0].Exec != "exec-L" {
		t.Fatalf("completion %+v", u)
	}
}

// ACK DEDUPE: the same delivery again, a new delivery for the same execution
// and a new one with the same idempotency key are each acked duplicate and
// file nothing more.
func TestARedeliveryIsAckedDuplicate(t *testing.T) {
	lw := newLinkWorld(t)
	input := json.RawMessage(dispatch("generic_bearer", map[string]any{"title": "PROTO: dedupe"}, "idem-D"))
	body := deliverBody{Kind: "dispatch", Reasoner: ReasonerIntake, Input: input, IdempotencyKey: "idem-D", Headers: map[string]string{"X-Execution-ID": "exec-D"}}
	lw.hub.deliver("dlv_a", "exec-D", body)
	eventually(t, "the first ack", func() bool { return len(lw.hub.ack("dlv_a")) == 1 })
	lw.hub.deliver("dlv_a", "exec-D", body) // a redelivery
	other := body
	other.Headers = map[string]string{"X-Execution-ID": "exec-D2"}
	lw.hub.deliver("dlv_b", "exec-D2", other) // same idempotency key
	eventually(t, "the duplicates' acks", func() bool { return len(lw.hub.ack("dlv_a")) == 2 && len(lw.hub.ack("dlv_b")) == 1 })
	if a := lw.hub.ack("dlv_a"); a[0].Outcome != "accepted" || a[1].Outcome != "duplicate" {
		t.Fatalf("acks of dlv_a %+v", a)
	}
	if b := lw.hub.ack("dlv_b"); b[0].Outcome != "duplicate" {
		t.Fatalf("ack of dlv_b %+v", b)
	}
	if items, _ := lw.floor.ListSaved(); len(items) != 1 {
		t.Fatalf("dedupe filed %d jobs", len(items))
	}
}

// RECONNECT: the control plane drops the link (a restart); the node dials
// again with backoff and, on the new link, tells the completion that could
// not land while it was down.
func TestTheLinkReconnectsAndTheCompletionStillArrives(t *testing.T) {
	lw := newLinkWorld(t)
	input := json.RawMessage(dispatch("generic_bearer", map[string]any{"title": "PROTO: reconnect"}, "idem-R"))
	lw.hub.deliver("dlv_r", "exec-R", deliverBody{Kind: "dispatch", Reasoner: ReasonerIntake, Input: input, Headers: map[string]string{"X-Execution-ID": "exec-R"}})
	eventually(t, "the ack", func() bool { return len(lw.hub.ack("dlv_r")) == 1 })
	job := lw.node.Open()[0].Job

	// The control plane goes away: the link drops and HTTP fails too.
	lw.mu.Lock()
	lw.httpDown = true
	lw.mu.Unlock()
	lw.hub.drop()
	eventually(t, "the link down", func() bool { return lw.node.Health().Link != "up" })
	const pr = "https://github.com/santoshkumarradha/factory-demo/pull/10"
	_ = lw.floor.Update(job, func(it *factory.Item) error {
		it.PullRequest = &factory.PullRequest{URL: pr, Number: 10, State: factory.PROpen}
		return nil
	})
	inbox.Notify(context.Background(), lw.floor, job, inbox.Opened(pr))
	time.Sleep(50 * time.Millisecond)
	if len(lw.node.Open()) != 1 {
		t.Fatal("a completion that did not land closed the execution")
	}
	// It comes back.
	lw.mu.Lock()
	lw.httpDown = false
	lw.mu.Unlock()
	eventually(t, "the link back up", func() bool { h := lw.node.Health(); return h.Link == "up" && h.LinkUps == 2 })
	eventually(t, "the completion after the reconnect", func() bool {
		for _, f := range lw.hub.upFrames(tStatus) {
			var u ExecutionUpdate
			if json.Unmarshal(f.Body, &u) == nil && f.Exec == "exec-R" && u.Status == ExecSucceeded && u.Result["pr_url"] == pr {
				return true
			}
		}
		return false
	})
	eventually(t, "the execution closed", func() bool { return len(lw.node.Open()) == 0 })
}

// A CANCEL DOWN THE LINK STOPS THE JOB and closes the execution here.
func TestACancelDownTheLinkStopsTheJob(t *testing.T) {
	lw := newLinkWorld(t)
	input := json.RawMessage(dispatch("generic_bearer", map[string]any{"title": "PROTO: cancel"}, "idem-C"))
	lw.hub.deliver("dlv_c", "exec-C", deliverBody{Kind: "dispatch", Reasoner: ReasonerIntake, Input: input, Headers: map[string]string{"X-Execution-ID": "exec-C"}})
	eventually(t, "the ack", func() bool { return len(lw.hub.ack("dlv_c")) == 1 })
	job := lw.node.Open()[0].Job
	lw.hub.deliver("dlv_x", "exec-C", deliverBody{Kind: "cancel", Reason: "a person cancelled it"})
	eventually(t, "the cancel's ack", func() bool { return len(lw.hub.ack("dlv_x")) == 1 })
	lw.mu.Lock()
	cancelled := append([]int(nil), lw.cancelled...)
	lw.mu.Unlock()
	if len(cancelled) != 1 || cancelled[0] != job {
		t.Fatalf("stopped %v, want job %d", cancelled, job)
	}
	if len(lw.node.Open()) != 0 {
		t.Fatal("a cancelled execution is still open")
	}
	if h := lw.node.Health(); !strings.Contains(h.LastEventWhat, "cancelled execution exec-C") {
		t.Fatalf("health %+v", h)
	}
}

// THE HEARTBEAT DOWN THE LINK CARRIES INFLIGHT and replaces the HTTP lease.
func TestTheLinkHeartbeatCarriesInflight(t *testing.T) {
	lw := newLinkWorld(t)
	input := json.RawMessage(dispatch("generic_bearer", map[string]any{"title": "PROTO: inflight"}, "idem-I"))
	lw.hub.deliver("dlv_i", "exec-I", deliverBody{Kind: "dispatch", Reasoner: ReasonerIntake, Input: input, Headers: map[string]string{"X-Execution-ID": "exec-I"}})
	eventually(t, "the ack", func() bool { return len(lw.hub.ack("dlv_i")) == 1 })
	lw.node.mu.Lock()
	ls := lw.node.running.link
	lw.node.mu.Unlock()
	if _, err := ls.call(context.Background(), frame{T: tHeartbeat}, heartbeat{Inflight: lw.node.inflight()}); err != nil {
		t.Fatal(err)
	}
	hbs := lw.hub.upFrames(tHeartbeat)
	var hb heartbeat
	_ = json.Unmarshal(hbs[len(hbs)-1].Body, &hb)
	if len(hb.Inflight) != 1 || hb.Inflight[0] != "exec-I" {
		t.Fatalf("heartbeat inflight %v", hb.Inflight)
	}
	// The hello listed it too, on a reconnect.
	lw.hub.drop()
	eventually(t, "the link back", func() bool { return lw.node.Health().LinkUps == 2 && lw.node.Health().Link == "up" })
	hellos := lw.hub.upFrames(tHello)
	var hello helloBody
	_ = json.Unmarshal(hellos[len(hellos)-1].Body, &hello)
	if len(hello.Inflight) != 1 || hello.Inflight[0] != "exec-I" {
		t.Fatalf("hello inflight %v", hello.Inflight)
	}
}

// A WRONG KEY IS NOT A LINK: the node says so and keeps HTTP leasing.
func TestALinkWithAWrongKeyIsRefused(t *testing.T) {
	hub := &fakeHub{t: t, acks: map[string][]ackBody{}}
	srv := httptest.NewServer(http.HandlerFunc(hub.serve))
	defer srv.Close()
	n := &Node{Conn: &Connection{}}
	err := n.runLink(context.Background(), &running{secrets: Secrets{Key: "wrong"}, settings: Settings{NodeID: "x"}}, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if err == nil || !strings.Contains(err.Error(), "did not accept that API key") {
		t.Fatalf("a wrong key: %v", err)
	}
}
