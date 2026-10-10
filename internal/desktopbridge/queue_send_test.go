package desktopbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// sendAgent records the sends a queue-send makes. The opening Submit is the
// fixture's running turn and is not a send-now; later Submits and every Steer
// are.
type sendAgent struct {
	queueAgent
	recMu       sync.Mutex
	steers      []string
	submits     []string
	opened      bool
	refuseSteer bool

	// park holds the first UnqueueFollowUp until the test releases it, so a
	// second send can be observed while the take-out is still in progress.
	park     chan struct{}
	parked   chan struct{}
	unqueues atomic.Int32
}

func (a *sendAgent) Steer(text string) (<-chan session.Event, error) {
	a.recMu.Lock()
	a.steers = append(a.steers, text)
	refuse := a.refuseSteer
	a.recMu.Unlock()
	if refuse {
		return nil, session.ErrNothingToSteer
	}
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

// Submit keeps the fixture's first turn open so later messages stay queued.
// A later call is a send-now into an idle conversation.
func (a *sendAgent) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	if !a.opened {
		a.opened = true
		return a.events, nil
	}
	a.submits = append(a.submits, text)
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}

func (a *sendAgent) UnqueueFollowUp(ch <-chan session.Event) bool {
	if a.park != nil && a.unqueues.Add(1) == 1 {
		a.parked <- struct{}{}
		<-a.park
	}
	return a.queueAgent.UnqueueFollowUp(ch)
}

func sendFixture(t *testing.T) (*Bridge, *sendAgent, string) {
	t.Helper()
	a := &sendAgent{queueAgent: queueAgent{fakeAgent: fakeAgent{events: make(chan session.Event, 8), model: Model}}}
	b := New(testToken, func(string) (Connection, error) {
		return Connection{Agent: a, Welcome: remote.Welcome{SessionFile: "session.jsonl", Workspace: "/project", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}, nil
	})
	t.Cleanup(b.Close)
	w := request(b, "POST", "/api/engine/sessions", "{}")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	path := "/api/engine/sessions/" + snapshot.ID
	if w := request(b, "POST", path+"/turn", `{"text":"first"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	return b, a, path
}

func queueWords(t *testing.T, b *Bridge, path string, words ...string) []QueuedWire {
	t.Helper()
	for _, word := range words {
		if w := request(b, "POST", path+"/turn", `{"text":"`+word+`","mode":"queue"}`); w.Code != 200 {
			t.Fatalf("queue %q: %s", word, w.Body.String())
		}
	}
	return snapshotQueue(t, b, path)
}

func conversationOn(b *Bridge, path string) *conversation {
	id := strings.TrimPrefix(path, "/api/engine/sessions/")
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

func postQueueSend(s *conversation, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/engine/sessions/"+s.id+"/queue-send", strings.NewReader(`{"id":`+strconv.Quote(id)+`}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.queueSend(w, r)
	return w
}

func requireAccepted(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var ack struct {
		Accepted bool `json:"accepted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ack); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if w.Code != 200 || !ack.Accepted {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func requireAlreadySent(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	if w.Code != 409 || body.Error != "that message has already been sent" {
		t.Fatalf("status %d error %q", w.Code, body.Error)
	}
}

func (a *sendAgent) sent() (steers, submits []string) {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	return append([]string(nil), a.steers...), append([]string(nil), a.submits...)
}

func waitIdle(t *testing.T, b *Bridge, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var snap Snapshot
		if err := json.Unmarshal(request(b, "GET", path, "").Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		if !snap.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("conversation stayed running")
}

// A send while work is running steers that message into the turn and leaves
// the others queued, in both the snapshot and the engine.
func TestQueueSendNowThroughBridgeRoute(t *testing.T) {
	b, a, path := sendFixture(t)
	queued := queueWords(t, b, path, "send this now")
	body := `{"id":` + strconv.Quote(queued[0].ID) + `}`
	requireAccepted(t, request(b, http.MethodPost, path+"/queue-send", body))
	steers, submits := a.sent()
	if len(steers) != 1 || steers[0] != "send this now" || len(submits) != 0 {
		t.Fatalf("steers %q, submits %q", steers, submits)
	}
	if queue := snapshotQueue(t, b, path); len(queue) != 0 {
		t.Fatalf("queue after send: %+v", queue)
	}
	requireAlreadySent(t, request(b, http.MethodPost, path+"/queue-send", body))
}

func TestQueueSendNowSteersWhileRunning(t *testing.T) {
	b, a, path := sendFixture(t)
	q := queueWords(t, b, path, "one", "two", "three")
	s := conversationOn(b, path)
	s.mu.Lock()
	before := s.seq
	s.mu.Unlock()
	w := postQueueSend(s, q[1].ID)
	requireAccepted(t, w)
	steers, submits := a.sent()
	if strings.Join(steers, ",") != "two" || len(submits) != 0 {
		t.Fatalf("steers %v submits %v", steers, submits)
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "one,three" {
		t.Fatalf("snapshot queue = %s", got)
	}
	if got := strings.Join(a.order(), ","); got != "one,three" {
		t.Fatalf("engine queue = %s", got)
	}
	s.mu.Lock()
	seq := s.seq
	var snap *SnapshotTail
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].Snapshot != nil {
			snap = s.records[i].Snapshot
			break
		}
	}
	s.mu.Unlock()
	if seq <= before || snap == nil || queueTexts(snap.Header.Queue) != "one,three" {
		t.Fatalf("snapshot not published: seq %d→%d queue %v", before, seq, snap)
	}
}

// A send after the turn has finished submits the message as the next turn.
func TestQueueSendNowSubmitsWhileIdle(t *testing.T) {
	b, a, path := sendFixture(t)
	q := queueWords(t, b, path, "one", "two")
	close(a.events)
	waitIdle(t, b, path)
	w := postQueueSend(conversationOn(b, path), q[0].ID)
	requireAccepted(t, w)
	steers, submits := a.sent()
	if len(steers) != 0 || strings.Join(submits, ",") != "one" {
		t.Fatalf("steers %v submits %v", steers, submits)
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
		t.Fatalf("snapshot queue = %s", got)
	}
	if got := strings.Join(a.order(), ","); got != "two" {
		t.Fatalf("engine queue = %s", got)
	}
}

// The turn can end between the claim and the steer. The words are already out
// of the queue, so they are submitted rather than dropped.
func TestQueueSendNowSubmitsWhenTheTurnEndsBeforeTheSteer(t *testing.T) {
	b, a, path := sendFixture(t)
	q := queueWords(t, b, path, "one", "two")
	a.recMu.Lock()
	a.refuseSteer = true
	a.recMu.Unlock()
	w := postQueueSend(conversationOn(b, path), q[0].ID)
	requireAccepted(t, w)
	steers, submits := a.sent()
	if strings.Join(steers, ",") != "one" || strings.Join(submits, ",") != "one" {
		t.Fatalf("steers %v submits %v", steers, submits)
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
		t.Fatalf("snapshot queue = %s", got)
	}
}

// An id the queue does not hold, and one the engine has already taken, are
// both 409 with the same sentence. Neither sends.
func TestQueueSendNowRefusesWhenTheMessageIsGone(t *testing.T) {
	t.Run("unknown id", func(t *testing.T) {
		b, a, path := sendFixture(t)
		queueWords(t, b, path, "one", "two")
		w := postQueueSend(conversationOn(b, path), "nope")
		requireAlreadySent(t, w)
		steers, submits := a.sent()
		if len(steers) != 0 || len(submits) != 0 {
			t.Fatalf("steers %v submits %v", steers, submits)
		}
		if got := queueTexts(snapshotQueue(t, b, path)); got != "one,two" {
			t.Fatalf("queue = %s", got)
		}
	})
	t.Run("engine already took it", func(t *testing.T) {
		b, a, path := sendFixture(t)
		q := queueWords(t, b, path, "one", "two")
		a.mu.Lock()
		a.list = a.list[1:]
		a.mu.Unlock()
		w := postQueueSend(conversationOn(b, path), q[0].ID)
		requireAlreadySent(t, w)
		steers, submits := a.sent()
		if len(steers) != 0 || len(submits) != 0 {
			t.Fatalf("steers %v submits %v", steers, submits)
		}
		if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
			t.Fatalf("queue = %s", got)
		}
	})
}

// Two sends of one id, overlapping the take-out, produce one steer.
func TestQueueSendNowSendsOnceUnderTwoCallers(t *testing.T) {
	b, a, path := sendFixture(t)
	q := queueWords(t, b, path, "one", "two")
	a.park = make(chan struct{})
	a.parked = make(chan struct{})
	s := conversationOn(b, path)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([]string, 2)
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w := postQueueSend(s, q[0].ID)
			codes[i] = w.Code
			bodies[i] = w.Body.String()
		}(i)
	}
	close(start)
	select {
	case <-a.parked:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not reach UnqueueFollowUp")
	}
	if s.mu.TryLock() {
		s.mu.Unlock()
		close(a.park)
		wg.Wait()
		t.Fatal("queue-send released the conversation during UnqueueFollowUp")
	}
	if n := a.unqueues.Load(); n != 1 {
		close(a.park)
		wg.Wait()
		t.Fatalf("unqueues while the first send is inside the take-out = %d", n)
	}
	close(a.park)
	wg.Wait()
	ok, gone := 0, 0
	for i := range codes {
		switch codes[i] {
		case 200:
			ok++
		case 409:
			gone++
			var body struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(bodies[i]), &body); err != nil || body.Error != "that message has already been sent" {
				t.Fatalf("refusal %d body %s", codes[i], bodies[i])
			}
		default:
			t.Fatalf("status %d body %s", codes[i], bodies[i])
		}
	}
	if ok != 1 || gone != 1 {
		t.Fatalf("codes %v", codes)
	}
	steers, submits := a.sent()
	if strings.Join(steers, ",") != "one" || len(submits) != 0 {
		t.Fatalf("steers %v submits %v", steers, submits)
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
		t.Fatalf("snapshot queue = %s", got)
	}
	if got := strings.Join(a.order(), ","); got != "two" {
		t.Fatalf("engine queue = %s", got)
	}
}

// An engine without the queue door says so instead of pretending to send.
func TestQueueSendNowRefusesAnEngineThatCannotEditTheQueue(t *testing.T) {
	b, _, id := fixture(t)
	w := postQueueSend(conversationOn(b, "/api/engine/sessions/"+id), "q1")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "cannot change queued messages") {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}
