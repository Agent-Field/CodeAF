package desktopbridge

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// queueAgent is a fake engine whose follow-up queue behaves the way the real
// one does: a message is editable and movable only while it is listed, and
// delivering it (the turn starting) takes it off the list under the same lock.
type queueAgent struct {
	fakeAgent
	mu    sync.Mutex
	list  []chan session.Event
	texts map[<-chan session.Event]string
}

func (a *queueAgent) FollowUp(text string) (<-chan session.Event, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ch := make(chan session.Event, 4)
	a.list = append(a.list, ch)
	if a.texts == nil {
		a.texts = map[<-chan session.Event]string{}
	}
	a.texts[ch] = text
	return ch, nil
}

func (a *queueAgent) at(ch <-chan session.Event) int {
	for i, c := range a.list {
		if (<-chan session.Event)(c) == ch {
			return i
		}
	}
	return -1
}

func (a *queueAgent) EditFollowUp(ch <-chan session.Event, text string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message")
	}
	if a.at(ch) < 0 {
		return session.ErrFollowUpGone
	}
	a.texts[ch] = strings.TrimSpace(text)
	return nil
}

func (a *queueAgent) UnqueueFollowUp(ch <-chan session.Event) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.at(ch)
	if i < 0 {
		return false
	}
	close(a.list[i])
	a.list = append(a.list[:i], a.list[i+1:]...)
	return true
}

func (a *queueAgent) MoveFollowUp(ch, before <-chan session.Event) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	i := a.at(ch)
	if i < 0 {
		return false
	}
	item := a.list[i]
	a.list = append(a.list[:i], a.list[i+1:]...)
	to := len(a.list)
	if j := a.at(before); before != nil && j >= 0 {
		to = j
	}
	a.list = append(a.list[:to], append([]chan session.Event{item}, a.list[to:]...)...)
	return true
}

// deliver is the turn starting for the first message in the queue.
func (a *queueAgent) deliver() {
	a.mu.Lock()
	defer a.mu.Unlock()
	first := a.list[0]
	a.list = a.list[1:]
	first <- session.Event{Kind: session.EventTextDelta, Text: "reply", Addressed: true}
}

func (a *queueAgent) order() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, ch := range a.list {
		out = append(out, a.texts[ch])
	}
	return out
}

func queueFixture(t *testing.T) (*Bridge, *queueAgent, string) {
	t.Helper()
	a := &queueAgent{fakeAgent: fakeAgent{events: make(chan session.Event, 8), model: Model}}
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

func snapshotQueue(t *testing.T, b *Bridge, path string) []QueuedWire {
	t.Helper()
	var s Snapshot
	if err := json.Unmarshal(request(b, "GET", path, "").Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s.Queue
}

func queueTexts(q []QueuedWire) string {
	var out []string
	for _, item := range q {
		out = append(out, item.Text)
	}
	return strings.Join(out, ",")
}

func TestQueuedMessagesAreEditedMovedAndRemovedInTheEngine(t *testing.T) {
	b, a, path := queueFixture(t)
	for _, word := range []string{"one", "two", "three"} {
		if w := request(b, "POST", path+"/turn", `{"text":"`+word+`","mode":"queue"}`); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	q := snapshotQueue(t, b, path)
	if queueTexts(q) != "one,two,three" {
		t.Fatalf("queue = %v", q)
	}
	if w := request(b, "POST", path+"/queue-edit", `{"id":"`+q[1].ID+`","text":" 2nd "}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(b, "POST", path+"/queue-move", `{"id":"`+q[2].ID+`","to":0}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "three,one,2nd" {
		t.Fatalf("snapshot order = %s", got)
	}
	if got := strings.Join(a.order(), ","); got != "three,one,2nd" {
		t.Fatalf("engine order = %s", got)
	}
	if w := request(b, "POST", path+"/queue-remove", `{"id":"`+q[0].ID+`"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "three,2nd" {
		t.Fatalf("after remove = %s", got)
	}
	if w := request(b, "POST", path+"/queue-edit", `{"id":"`+q[1].ID+`","text":"   "}`); w.Code != 400 {
		t.Fatalf("empty edit status %d", w.Code)
	}
}

// A MESSAGE WHOSE TURN HAS STARTED CANNOT BE CHANGED: edit, move and remove are
// 409 and the row leaves the snapshot.
func TestDeliveredQueuedMessageRefusesEditMoveAndRemove(t *testing.T) {
	for _, action := range []string{"queue-edit", "queue-move", "queue-remove"} {
		t.Run(action, func(t *testing.T) {
			b, a, path := queueFixture(t)
			for _, word := range []string{"one", "two"} {
				request(b, "POST", path+"/turn", `{"text":"`+word+`","mode":"queue"}`)
			}
			q := snapshotQueue(t, b, path)
			// The engine starts the first message's turn before the call arrives;
			// the bridge has not yet seen its first event.
			a.mu.Lock()
			a.list = a.list[1:]
			a.mu.Unlock()
			w := request(b, "POST", path+"/"+action, `{"id":"`+q[0].ID+`","text":"late","to":1}`)
			if w.Code != 409 || !strings.Contains(w.Body.String(), "already been sent") {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
				t.Fatalf("queue after refusal = %s", got)
			}
		})
	}
}

// A queued row leaves the snapshot when its turn starts, and an unknown id is a
// 409, never a success.
func TestQueuedRowLeavesTheSnapshotWhenItsTurnStarts(t *testing.T) {
	b, a, path := queueFixture(t)
	request(b, "POST", path+"/turn", `{"text":"one","mode":"queue"}`)
	request(b, "POST", path+"/turn", `{"text":"two","mode":"queue"}`)
	a.deliver()
	deadline := time.Now().Add(2 * time.Second)
	for queueTexts(snapshotQueue(t, b, path)) != "two" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := queueTexts(snapshotQueue(t, b, path)); got != "two" {
		t.Fatalf("queue = %s", got)
	}
	if w := request(b, "POST", path+"/queue-remove", `{"id":"nope"}`); w.Code != 409 {
		t.Fatalf("unknown id status %d", w.Code)
	}
}

// An engine that cannot reorder says so instead of pretending.
func TestEngineWithoutAQueueDoorRefusesQueueChanges(t *testing.T) {
	b, _, id := fixture(t)
	w := request(b, "POST", "/api/engine/sessions/"+id+"/queue-edit", `{"id":"q1","text":"x"}`)
	if w.Code != 409 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}
