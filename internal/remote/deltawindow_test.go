package remote

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// These tests read frames at the surface so coalescing cannot hide a lost event,
// a replay gap or a close sent before the last text.
func deltaSession(t *testing.T, window time.Duration) (*fakeAgent, *Session, *link, uint64, chan session.Event) {
	t.Helper()
	agent := &fakeAgent{}
	engine := engineOn(agent)
	engine.DeltaWindow = window
	sess := NewSession(engine, true)
	t.Cleanup(func() { _ = sess.Close() })
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	return agent, sess, l, ref.Stream, agent.stream(0)
}

// Driver updates describe the connection, not the ordered turn stream. A
// second surface can cause one without adding an event to the replay ring.
func deltaFrame(l *link) Frame {
	for {
		frame := l.recv()
		if frame.Kind != "driver" {
			return frame
		}
	}
}

func deltaEvent(t *testing.T, l *link, id uint64, seq *uint64) session.Event {
	t.Helper()
	frame := deltaFrame(l)
	(*seq)++
	if frame.Kind != "event" || frame.ID != id || frame.Seq != *seq {
		t.Fatalf("frame = %+v, want event on stream %d seq %d", frame, id, *seq)
	}
	return decode[EventWire](t, frame.Payload).Unwire()
}

func deltaClose(t *testing.T, l *link, id, seq uint64) {
	t.Helper()
	if frame := deltaFrame(l); frame.Kind != "closed" || frame.ID != id || frame.Seq != seq {
		t.Fatalf("close = %+v, want stream %d seq %d", frame, id, seq)
	}
}

// Only adjacent text with identical metadata is one observable run. This is
// deliberately independent of the coalescer's comparison helper.
func deltaRuns(events []session.Event) []session.Event {
	var runs []session.Event
	for _, ev := range events {
		if n := len(runs); n > 0 && (ev.Kind == session.EventTextDelta || ev.Kind == session.EventReasoning) {
			prev, next := runs[n-1], ev
			prev.Text, next.Text = "", ""
			if reflect.DeepEqual(prev, next) {
				runs[n-1].Text += ev.Text
				continue
			}
		}
		runs = append(runs, ev)
	}
	return runs
}

func TestDeltaWindowEquivalence(t *testing.T) {
	script := []session.Event{
		{Kind: session.EventTextDelta, Text: "hel"},
		{Kind: session.EventTextDelta, Text: "lo"},
		{Kind: session.EventTextDelta, Text: " there"},
		{Kind: session.EventReasoning, Text: "think"},
		{Kind: session.EventReasoning, Text: "ing"},
		{Kind: session.EventToolBegin, Tool: "read", CallID: "c", Args: "{}"},
		{Kind: session.EventToolOutput, Tool: "read", CallID: "c", Text: "output"},
		{Kind: session.EventToolEnd, Tool: "read", CallID: "c", Text: "done"},
		{Kind: session.EventConsentRequest, ID: 7, Tool: "bash", Hint: "permission"},
		{Kind: session.EventTextDelta, Text: "after"},
		{Kind: session.EventTextDelta, Text: " tool"},
		{Kind: session.EventTextDelta, Text: "addressed", Addressed: true},
		{Kind: session.EventTextDelta, Text: " again", Addressed: true},
		{Kind: session.EventTextDelta, Text: "run", Addressed: true, RunID: "r2"},
		{Kind: session.EventTextDelta, Text: " two", Addressed: true, RunID: "r2"},
		{Kind: session.EventTextDelta, Text: "hint", Hint: "different", Addressed: true, RunID: "r2"},
		{Kind: session.EventTurnDone, Usage: session.Usage{Output: 17}},
	}
	var baseline []session.Event
	for _, window := range []time.Duration{0, 2 * time.Second} {
		agent, _, l, id, stream := deltaSession(t, window)
		for _, ev := range script {
			stream <- ev
		}
		agent.finish(stream)
		var got []session.Event
		var seq uint64
		for {
			frame := deltaFrame(l)
			if frame.Kind == "closed" {
				if frame.ID != id || frame.Seq != seq {
					t.Fatalf("closed = %+v, last seq %d", frame, seq)
				}
				break
			}
			seq++
			if frame.Kind != "event" || frame.ID != id || frame.Seq != seq {
				t.Fatalf("frame = %+v, want seq %d", frame, seq)
			}
			got = append(got, decode[EventWire](t, frame.Payload).Unwire())
		}
		if window == 0 {
			if len(got) != len(script) {
				t.Fatalf("disabled window sent %d frames, want %d", len(got), len(script))
			}
			baseline = deltaRuns(got)
		} else if !reflect.DeepEqual(deltaRuns(got), baseline) {
			t.Fatalf("coalescing changed ordered runs:\ngot  %+v\nwant %+v", deltaRuns(got), baseline)
		}
	}
}

func TestDeltaWindowFirstDeltaIsImmediate(t *testing.T) {
	agent, _, l, id, stream := deltaSession(t, 2*time.Second)
	var seq uint64
	for _, text := range []string{"first", "after tool"} {
		start := time.Now()
		stream <- session.Event{Kind: session.EventTextDelta, Text: text}
		if ev := deltaEvent(t, l, id, &seq); ev.Text != text {
			t.Fatalf("delta = %+v", ev)
		}
		if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
			t.Fatalf("first delta waited %v", elapsed)
		}
		stream <- session.Event{Kind: session.EventToolBegin, Tool: "read"}
		if ev := deltaEvent(t, l, id, &seq); ev.Kind != session.EventToolBegin {
			t.Fatalf("tool = %+v", ev)
		}
	}
	agent.finish(stream)
	deltaClose(t, l, id, seq)
}

// The scripted stop emits the same done event as a cancelled turn, through the
// real interrupt method, so this exercises the stop door as well as the pump.
type deltaStoppedAgent struct{ *fakeAgent }

func (a *deltaStoppedAgent) Interrupt() { a.InterruptFor(session.StopByPerson) }
func (a *deltaStoppedAgent) InterruptFor(door session.StopDoor) {
	a.fakeAgent.InterruptFor(door)
	if stream := a.stream(0); door == session.StopByPerson && stream != nil {
		stream <- session.Event{Kind: session.EventTurnDone}
	}
}

func deltaStop(t *testing.T, l *link, agent *fakeAgent) {
	t.Helper()
	l.ok(100, MethodInterrupt, InterruptArgs{Door: string(session.StopByPerson)})
	agent.mu.Lock()
	door := agent.stopDoor
	agent.mu.Unlock()
	if door != session.StopByPerson {
		t.Fatalf("stop door = %q", door)
	}
}

func deltaInterruptSession(t *testing.T, window time.Duration) (*fakeAgent, *link, uint64, chan session.Event) {
	t.Helper()
	agent := &fakeAgent{}
	engine := engineOn(agent)
	engine.Agent = &deltaStoppedAgent{agent}
	engine.DeltaWindow = window
	sess := NewSession(engine, true)
	t.Cleanup(func() { _ = sess.Close() })
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})
	ref := decode[StreamRef](t, l.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
	return agent, l, ref.Stream, agent.stream(0)
}

func TestDeltaWindowNonTextFlushesAndClosesWindow(t *testing.T) {
	agent, l, id, stream := deltaInterruptSession(t, 2*time.Second)
	var seq uint64
	for _, kind := range []session.EventKind{session.EventToolBegin, session.EventConsentRequest, session.EventTurnDone} {
		stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
		if ev := deltaEvent(t, l, id, &seq); ev.Text != "a" {
			t.Fatalf("leading delta = %+v", ev)
		}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "b"}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "c"}
		start := time.Now()
		if kind == session.EventTurnDone {
			deltaStop(t, l, agent)
		} else {
			stream <- session.Event{Kind: kind, ID: 7, Tool: "read"}
		}
		if ev := deltaEvent(t, l, id, &seq); ev.Kind != session.EventTextDelta || ev.Text != "bc" {
			t.Fatalf("pending delta = %+v", ev)
		}
		if ev := deltaEvent(t, l, id, &seq); ev.Kind != kind {
			t.Fatalf("barrier = %+v, want %v", ev, kind)
		}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "after"}
		if ev := deltaEvent(t, l, id, &seq); ev.Text != "after" {
			t.Fatalf("after barrier = %+v", ev)
		}
		if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
			t.Fatalf("barrier waited %v", elapsed)
		}
		stream <- session.Event{Kind: session.EventToolEnd}
		deltaEvent(t, l, id, &seq)
	}
	agent.finish(stream)
	deltaClose(t, l, id, seq)
}

func TestDeltaWindowTurnEndFlushes(t *testing.T) {
	for _, done := range []bool{false, true} {
		agent, _, l, id, stream := deltaSession(t, 2*time.Second)
		stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
		var seq uint64
		deltaEvent(t, l, id, &seq)
		stream <- session.Event{Kind: session.EventTextDelta, Text: "b"}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "c"}
		start := time.Now()
		if done {
			stream <- session.Event{Kind: session.EventTurnDone}
		}
		agent.finish(stream)
		if ev := deltaEvent(t, l, id, &seq); ev.Text != "bc" {
			t.Fatalf("tail = %+v", ev)
		}
		if done {
			if ev := deltaEvent(t, l, id, &seq); ev.Kind != session.EventTurnDone {
				t.Fatalf("done = %+v", ev)
			}
		}
		deltaClose(t, l, id, seq)
		if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
			t.Fatalf("close waited %v", elapsed)
		}
	}
}

func TestDeltaWindowCoalescesOnlyWhenEnabled(t *testing.T) {
	for _, window := range []time.Duration{0, time.Second} {
		agent, _, l, id, stream := deltaSession(t, window)
		for range 50 {
			stream <- session.Event{Kind: session.EventTextDelta, Text: "x"}
		}
		agent.finish(stream)
		var seq uint64
		var text string
		for {
			frame := deltaFrame(l)
			if frame.Kind == "closed" {
				if frame.ID != id || frame.Seq != seq {
					t.Fatalf("close = %+v, last seq %d", frame, seq)
				}
				break
			}
			seq++
			if frame.Kind != "event" || frame.ID != id || frame.Seq != seq {
				t.Fatalf("frame = %+v", frame)
			}
			ev := decode[EventWire](t, frame.Payload).Unwire()
			if ev.Kind != session.EventTextDelta {
				t.Fatalf("delta = %+v", ev)
			}
			text += ev.Text
		}
		if text != strings.Repeat("x", 50) {
			t.Fatalf("text = %q", text)
		}
		if window == 0 && seq != 50 || window > 0 && seq > 3 {
			t.Fatalf("window %v sent %d frames", window, seq)
		}
	}
}

func TestDeltaWindowByteCapSendsImmediately(t *testing.T) {
	agent, _, l, id, stream := deltaSession(t, 2*time.Second)
	var seq uint64
	stream <- session.Event{Kind: session.EventTextDelta, Text: "first"}
	deltaEvent(t, l, id, &seq)
	start := time.Now()
	stream <- session.Event{Kind: session.EventTextDelta, Text: strings.Repeat("x", DeltaBytes-1)}
	stream <- session.Event{Kind: session.EventTextDelta, Text: "y"}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != strings.Repeat("x", DeltaBytes-1)+"y" {
		t.Fatalf("capped delta = %d bytes", len(ev.Text))
	}
	// A large delta is its own frame, even when a small run is already pending.
	stream <- session.Event{Kind: session.EventTextDelta, Text: "small"}
	stream <- session.Event{Kind: session.EventTextDelta, Text: strings.Repeat("z", DeltaBytes+1)}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != "small" {
		t.Fatalf("pending before large delta = %d bytes", len(ev.Text))
	}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != strings.Repeat("z", DeltaBytes+1) {
		t.Fatalf("large delta = %d bytes", len(ev.Text))
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("byte cap waited %v", elapsed)
	}
	agent.finish(stream)
	deltaClose(t, l, id, seq)
}

func TestDeltaWindowReplayWhileTextIsPending(t *testing.T) {
	for _, cursor := range []bool{true, false} {
		agent, sess, first, id, stream := deltaSession(t, 2*time.Second)
		stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
		var seq uint64
		seen := deltaEvent(t, first, id, &seq).Text
		stream <- session.Event{Kind: session.EventTextDelta, Text: "b"}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "c"}
		// Once the pump has taken both chunks, at least the earlier one is pending.
		waitForEngine(t, func() bool { return len(stream) == 0 })
		if cursor {
			if err := first.end(); err != nil {
				t.Fatal(err)
			}
		}
		// Replaying a frame emitted while nobody watched tests both the ring and the
		// pending tail. The metadata change flushes "bc" and leaves "d" pending.
		stream <- session.Event{Kind: session.EventTextDelta, Text: "d", Addressed: true}
		stream <- session.Event{Kind: session.EventTextDelta, Text: "e", Addressed: true}
		waitForEngine(t, func() bool { return len(stream) == 0 })
		if !cursor {
			// The original surface stays attached in the no-cursor case; both
			// surfaces must see each emitted frame once, including the pending tail.
			seen += deltaEvent(t, first, id, &seq).Text
		}
		second := dialSession(t, sess)
		hello := Hello{Version: Version, Watch: !cursor}
		if cursor {
			hello.Resume = []StreamCursor{{Stream: id, Seq: seq}}
		} else {
			seq, seen = 0, ""
		}
		second.hello(hello)
		stream <- session.Event{Kind: session.EventTextDelta, Text: "f", Addressed: true}
		agent.finish(stream)
		for {
			frame := deltaFrame(second)
			if frame.Kind == "closed" {
				if frame.ID != id || frame.Seq != seq {
					t.Fatalf("closed = %+v, last seq %d", frame, seq)
				}
				break
			}
			seq++
			if frame.Kind != "event" || frame.ID != id || frame.Seq != seq {
				t.Fatalf("replay = %+v, want seq %d", frame, seq)
			}
			seen += decode[EventWire](t, frame.Payload).Unwire().Text
		}
		if seen != "abcdef" {
			t.Fatalf("cursor %v received %q", cursor, seen)
		}
		if !cursor {
			firstSeq := uint64(2)
			if ev := deltaEvent(t, first, id, &firstSeq); ev.Text != "def" {
				t.Fatalf("original surface tail = %q", ev.Text)
			}
			deltaClose(t, first, id, firstSeq)
		}
	}
}

func TestDeltaWindowNonTextLatency(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind session.EventKind
	}{
		{"stop", session.EventTurnDone},
		{"consent", session.EventConsentRequest},
		{"tool", session.EventToolBegin},
		{"error", session.EventError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := tc.kind
			agent, l, id, stream := deltaInterruptSession(t, DefaultDeltaWindow)
			var seq uint64
			var samples []time.Duration
			for range 20 {
				stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
				deltaEvent(t, l, id, &seq)
				stream <- session.Event{Kind: session.EventTextDelta, Text: "b"}
				start := time.Now()
				if kind == session.EventTurnDone {
					deltaStop(t, l, agent)
				} else {
					ev := session.Event{Kind: kind, ID: 7, Tool: "read"}
					if kind == session.EventError {
						ev.Err = errors.New("scripted failure")
					}
					stream <- ev
				}
				if ev := deltaEvent(t, l, id, &seq); ev.Kind != session.EventTextDelta || ev.Text != "b" {
					t.Fatalf("pending = %+v", ev)
				}
				if ev := deltaEvent(t, l, id, &seq); ev.Kind != kind {
					t.Fatalf("barrier = %+v", ev)
				}
				samples = append(samples, time.Since(start))
			}
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			median, maximum := samples[len(samples)/2], samples[len(samples)-1]
			t.Logf("non-text push-to-receipt: median %v, max %v (window %v)", median, maximum, DefaultDeltaWindow)
			if median >= DefaultDeltaWindow/2 || maximum >= DefaultDeltaWindow {
				t.Fatalf("non-text waited: median %v, max %v", median, maximum)
			}
			agent.finish(stream)
			deltaClose(t, l, id, seq)
		})
	}
}

func TestDeltaWindowTimerFlushesAndIdleStartsImmediately(t *testing.T) {
	const window = 400 * time.Millisecond
	agent, _, l, id, stream := deltaSession(t, window)
	var seq uint64
	stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
	deltaEvent(t, l, id, &seq)
	for _, text := range []string{"bc", "de"} {
		start := time.Now()
		for _, char := range text {
			stream <- session.Event{Kind: session.EventTextDelta, Text: string(char)}
		}
		if ev := deltaEvent(t, l, id, &seq); ev.Text != text {
			t.Fatalf("timed tail = %q, want %q", ev.Text, text)
		}
		if elapsed := time.Since(start); elapsed < window/2 || elapsed > window+500*time.Millisecond {
			t.Fatalf("tail arrived after %v", elapsed)
		}
	}
	// An empty window needs no timer wakeup. Let it expire before the next run.
	time.Sleep(window + 100*time.Millisecond)
	start := time.Now()
	stream <- session.Event{Kind: session.EventTextDelta, Text: "f"}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != "f" {
		t.Fatalf("idle first delta = %q", ev.Text)
	}
	if elapsed := time.Since(start); elapsed >= window/4 {
		t.Fatalf("idle first delta waited %v", elapsed)
	}
	agent.finish(stream)
	deltaClose(t, l, id, seq)
}

func TestDeltaWindowMetadataFlushRestartsTimer(t *testing.T) {
	const window = time.Second
	agent, _, l, id, stream := deltaSession(t, window)
	var seq uint64
	stream <- session.Event{Kind: session.EventTextDelta, Text: "a"}
	deltaEvent(t, l, id, &seq)
	stream <- session.Event{Kind: session.EventTextDelta, Text: "b"}
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	stream <- session.Event{Kind: session.EventTextDelta, Text: "c", RunID: "next"}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != "b" {
		t.Fatalf("old run = %q", ev.Text)
	}
	if ev := deltaEvent(t, l, id, &seq); ev.Text != "c" || ev.RunID != "next" {
		t.Fatalf("new run = %+v", ev)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond || elapsed > window+500*time.Millisecond {
		t.Fatalf("restarted window lasted %v", elapsed)
	}
	agent.finish(stream)
	deltaClose(t, l, id, seq)
}

func TestDeltaWindowStreamsAreIndependent(t *testing.T) {
	agent, _, l, firstID, first := deltaSession(t, 2*time.Second)
	var firstSeq uint64
	first <- session.Event{Kind: session.EventTextDelta, Text: "a"}
	deltaEvent(t, l, firstID, &firstSeq)
	first <- session.Event{Kind: session.EventTextDelta, Text: "b"}
	secondID := decode[StreamRef](t, l.ok(2, MethodSubmit, SubmitArgs{Text: "another turn"}).Payload).Stream
	second := agent.stream(1)
	var secondSeq uint64
	start := time.Now()
	second <- session.Event{Kind: session.EventReasoning, Text: "thinking"}
	if ev := deltaEvent(t, l, secondID, &secondSeq); ev.Kind != session.EventReasoning || ev.Text != "thinking" {
		t.Fatalf("second stream = %+v", ev)
	}
	if elapsed := time.Since(start); elapsed >= 500*time.Millisecond {
		t.Fatalf("second stream inherited the first's window: %v", elapsed)
	}
	agent.finish(second)
	deltaClose(t, l, secondID, secondSeq)
	agent.finish(first)
	if ev := deltaEvent(t, l, firstID, &firstSeq); ev.Text != "b" {
		t.Fatalf("first stream tail = %q", ev.Text)
	}
	deltaClose(t, l, firstID, firstSeq)
}
