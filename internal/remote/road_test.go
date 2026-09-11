package remote

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// heldGetter is a fake engine whose Transcript holds the read loop's old
// shape: one slow call in flight. After [classify], that call runs off the
// reader, which is what the test below is here to prove.
type heldGetter struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	heard   []session.Answer
}

func (h *heldGetter) Transcript() []session.DisplayEntry {
	h.once.Do(func() { close(h.entered) })
	<-h.release
	return h.fakeAgent.Transcript()
}

func (h *heldGetter) ResolveQuestion(answer session.Answer) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.heard = append(h.heard, answer)
	return nil
}

// TestASecondCallIsNotRefusedWhileASlowGetterIsStillHeld is the road half of
// the measured defect: six copies of the ordinary question e2e, and one of
// them spent exactly [callDeadline] before the surface was told the
// connection was gone, while the engine had already applied the answer. A
// getter held the reader's invoke; the keystroke sat behind it. This drives
// that shape on the real protocol, with a fake agent whose Transcript does
// not return, and asserts the second call — the keystroke — comes back
// without waiting out the deadline and without the gone sentence.
func TestASecondCallIsNotRefusedWhileASlowGetterIsStillHeld(t *testing.T) {
	far := &heldGetter{
		fakeAgent: &fakeAgent{model: "m"},
		entered:   make(chan struct{}),
		release:   make(chan struct{}),
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() {
		select {
		case <-far.release:
		default:
			close(far.release)
		}
		_ = loop.Close()
	})

	done := make(chan struct{})
	go func() {
		_ = loop.Client.Agent().Transcript()
		close(done)
	}()
	select {
	case <-far.entered:
	case <-time.After(time.Second):
		t.Fatal("the getter never reached the engine")
	}

	answered := make(chan error, 1)
	go func() {
		answered <- loop.Client.Agent().ResolveQuestion(session.Answer{Key: "1", Picked: []string{"1"}})
	}()
	select {
	case err := <-answered:
		if err != nil {
			t.Fatalf("the second call was refused: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second call did not return; it is waiting behind the getter")
	}

	far.mu.Lock()
	heard := len(far.heard)
	far.mu.Unlock()
	if heard != 1 {
		t.Fatalf("the engine heard %d answers, want the keystroke that was not queued", heard)
	}
	if err := loop.Client.Err(); err != nil {
		t.Fatalf("the connection was buried under a live getter: %v", err)
	}

	close(far.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the getter never finished after it was released")
	}
}

// TestADeadlineOnALiveConnectionDoesNotSayTheConnectionIsGone is the other
// half of that defect's clock: a call that has waited its window out used to
// come back as [Client.gone], which is the sentence for a dead link. The
// scripted engine answers nothing; the deadline is a few milliseconds so this
// does not wait out [callDeadline].
func TestADeadlineOnALiveConnectionDoesNotSayTheConnectionIsGone(t *testing.T) {
	client, e := newEngine(t)
	e.silent[MethodPing] = true

	_, answered, err := client.callAnswered(nil, MethodPing, nil, 20*time.Millisecond)
	if answered {
		t.Fatal("a silent engine was treated as having answered")
	}
	if err == nil {
		t.Fatal("a deadline that ran out reported success")
	}
	if strings.Contains(err.Error(), "gone") {
		t.Fatalf("a live connection was declared gone: %v", err)
	}
	if !strings.Contains(err.Error(), strings.TrimSpace(lateCallTail)) {
		t.Fatalf("the late call did not say so: %v", err)
	}
	if client.Err() != nil {
		t.Fatalf("the connection was buried: %v", client.Err())
	}

	// A LATER CALL ON A DIFFERENT METHOD still works, which is the whole of
	// not burying. Ping stays silent so this does not race the scripted
	// engine's map; Transcript is a getter the harness answers.
	if _, err := client.call(nil, MethodTranscript, nil); err != nil {
		t.Fatalf("a later call on the same connection failed: %v", err)
	}
}

func TestTheCallClassKeepsAKeystrokeOffTheReader(t *testing.T) {
	if staysOnReader(MethodQuestionResolve) || staysOnReader(MethodTranscript) || staysOnReader(MethodPing) {
		t.Fatal("a getter or a small act stayed on the reader")
	}
	if !staysOnReader(MethodSubmit) || !staysOnReader(MethodCompact) || !staysOnReader(MethodSetModel) {
		t.Fatal("a stream or a shape change left the reader")
	}
	// AND AN ACT IS NOT A CLASS OF ITS OWN ON THE CLOCK, which is a law with a
	// measurement behind it (callclass.go): a longer window for a keystroke buys
	// a terminal that stops drawing, because the question block asks its door
	// from the update loop. Both classes leave the reader; neither waits longer.
	if classify(MethodQuestionResolve) != classAct || classify(MethodTranscript) != classGetter {
		t.Fatal("the two off-reader classes are no longer told apart")
	}
}
