package remote

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestATruncatedReplayIsSignalledAndNeverLoopsTheResume verifies W7: when a
// running stream's ring evicts old events (by byte bound or event count) and a
// resuming surface presents a cursor with seq+1 < ring.first, the server sends
// exactly one "gap" frame with {stream, have, first} ahead of the retained
// events; a second resume from the updated cursor sends no gap; a cursor at or
// after first-1 receives no gap; the ring never holds more than ringBytes plus
// the newest event; and an older reader that ignores unknown frame kinds still
// decodes the retained events.
func TestATruncatedReplayIsSignalledAndNeverLoopsTheResume(t *testing.T) {
	savedBackoff := firstBackoff
	firstBackoff = 5 * time.Millisecond
	t.Cleanup(func() { firstBackoff = savedBackoff })

	savedByteLimit := ringByteLimit
	savedEventLimit := ringEventLimit
	ringByteLimit = 240
	t.Cleanup(func() {
		ringByteLimit = savedByteLimit
		ringEventLimit = savedEventLimit
	})

	// 1. Unit invariant on ring: never holds more than ringBytes plus the newest
	// event, always keeps the newest event even when larger than ringBytes, and
	// keeps first exact.
	t.Run("ring byte bound keeps newest event and exact first", func(t *testing.T) {
		r := &ring{first: 1}
		for i := 1; i <= 10; i++ {
			payload := mustJSON(WireEvent(session.Event{
				Kind: session.EventTextDelta,
				Text: strings.Repeat("x", 60),
			}))
			seq := r.add(payload)
			if seq != uint64(i) {
				t.Fatalf("seq = %d, want %d", seq, i)
			}
			newest := len(payload)
			if r.bytes > ringByteLimit+newest {
				t.Fatalf("ring bytes %d exceeded cap %d + newest %d", r.bytes, ringByteLimit, newest)
			}
			if len(r.events) > 1 && r.bytes > ringByteLimit {
				t.Fatalf("ring with %d events held %d bytes > cap %d", len(r.events), r.bytes, ringByteLimit)
			}
			if wantFirst := r.last - uint64(len(r.events)) + 1; r.first != wantFirst {
				t.Fatalf("ring.first = %d, want %d", r.first, wantFirst)
			}
		}
		if r.first <= 1 {
			t.Fatalf("ring did not evict under byte cap: first=%d", r.first)
		}

		// A single oversized event larger than ringByteLimit is still kept as the sole entry.
		huge := mustJSON(WireEvent(session.Event{
			Kind: session.EventTextDelta,
			Text: strings.Repeat("H", ringByteLimit*3),
		}))
		r.add(huge)
		if len(r.events) != 1 || r.first != r.last {
			t.Fatalf("oversized newest event not kept alone: len=%d first=%d last=%d", len(r.events), r.first, r.last)
		}
	})

	// 2. End-to-end roaming client + real Session: gap frame on first resume, no
	// gap on second resume from the new cursor, and no gap for cursor >= first-1.
	t.Run("roaming resume receives one gap frame then retained events and second resume gets no gap", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		sess := NewSession(engineOn(agent), true)

		var mu sync.Mutex
		var surfaces []io.ReadWriteCloser
		dialer := func() (io.ReadWriteCloser, error) {
			surface, engine := Pipe()
			guard.Go("remote/test-w7-serve", func() {
				_ = ServeAttach(engine, engine, AttachOptions{
					Open: func(Hello) (*Session, error) { return sess, nil },
				})
				_ = engine.Close()
			})
			mu.Lock()
			surfaces = append(surfaces, surface)
			mu.Unlock()
			return surface, nil
		}

		var gapsMu sync.Mutex
		var gaps []Gap
		client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{
			Dial:   dialer,
			Window: time.Second,
			OnGap: func(g Gap) {
				gapsMu.Lock()
				gaps = append(gaps, g)
				gapsMu.Unlock()
			},
		})
		if err != nil {
			t.Fatalf("roam: %v", err)
		}
		t.Cleanup(func() { _ = client.Close() })

		events, err := client.Agent().Submit(nil, "stream a long reply")
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		waitFor(t, "turn to open", func() bool { return agent.stream(0) != nil })
		turn := agent.stream(0)

		// Deliver seq 1 and seq 2 to the connected client.
		turn <- session.Event{Kind: session.EventTextDelta, Text: "e1"}
		turn <- session.Event{Kind: session.EventTextDelta, Text: "e2"}
		for _, want := range []string{"e1", "e2"} {
			if ev, ok := nextEvent(t, events); !ok || ev.Text != want {
				t.Fatalf("got %q (ok=%v), want %q", ev.Text, ok, want)
			}
		}

		// Cut link 0 so the client is offline with cursor Have=2.
		// Hold a dial gate so we can overflow the ring while the client is disconnected.
		mu.Lock()
		firstConn := surfaces[0]
		mu.Unlock()

		// Overflow the ring on the server while no surface is reading.
		// Emit 6 events of ~80 bytes each so under ringByteLimit=240 the oldest
		// events (seqs 1..6) are evicted and only the tail remains.
		sess.mu.Lock()
		delete(sess.surfaces, sess.driver)
		sess.mu.Unlock()

		for _, tok := range []string{
			"e3-" + strings.Repeat("a", 50),
			"e4-" + strings.Repeat("b", 50),
			"e5-" + strings.Repeat("c", 50),
			"e6-" + strings.Repeat("d", 50),
			"e7-" + strings.Repeat("e", 50),
			"e8-" + strings.Repeat("f", 50),
		} {
			turn <- session.Event{Kind: session.EventTextDelta, Text: tok}
		}
		waitFor(t, "ring to reach seq 8 and evict older events", func() bool {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			r := sess.rings[1]
			return r != nil && r.last == 8 && r.first > 3
		})
		sess.mu.Lock()
		wantFirst := sess.rings[1].first
		sess.mu.Unlock()

		// Verify directly at wire level that cursor at first-1 and cursor at first get NO gap frame.
		sess.mu.Lock()
		atBoundary := sess.replayLocked([]StreamCursor{{Stream: 1, Seq: wantFirst - 1}})
		afterBoundary := sess.replayLocked([]StreamCursor{{Stream: 1, Seq: wantFirst}})
		sess.mu.Unlock()
		for _, f := range append(atBoundary, afterBoundary...) {
			if f.Kind == "gap" {
				t.Fatalf("cursor at/after first-1 (first=%d) received unexpected gap frame: %+v", wantFirst, f)
			}
		}

		// Now close the client's first pipe to trigger redial from Have=2 (< wantFirst-1).
		_ = firstConn.Close()

		// Read the retained events from seq=wantFirst..8 off the resumed turn.
		wantCount := int(8 - wantFirst + 1)
		var received []string
		for i := 0; i < wantCount; i++ {
			ev, ok := nextEvent(t, events)
			if !ok {
				t.Fatalf("stream closed early after %d events", i)
			}
			received = append(received, ev.Text[:2])
		}
		if len(received) != wantCount || received[len(received)-1] != "e8" {
			t.Fatalf("retained replay delivered %v (want %d events ending in e8)", received, wantCount)
		}

		gapsMu.Lock()
		gotGaps := append([]Gap(nil), gaps...)
		gapsMu.Unlock()
		if !reflect.DeepEqual(gotGaps, []Gap{{Stream: 1, Have: 2, First: wantFirst}}) {
			t.Fatalf("OnGap received %+v, want [{Stream:1 Have:2 First:%d}]", gotGaps, wantFirst)
		}

		// Cut the second pipe and let the client redial a SECOND time from its
		// updated cursor (seq=8). No second gap frame may be sent.
		waitFor(t, "second connection to settle", func() bool {
			mu.Lock()
			n := len(surfaces)
			mu.Unlock()
			return n >= 2 && client.LinkNote() == ""
		})
		mu.Lock()
		secondConn := surfaces[1]
		mu.Unlock()
		_ = secondConn.Close()

		waitFor(t, "third connection to settle", func() bool {
			mu.Lock()
			n := len(surfaces)
			mu.Unlock()
			return n >= 3 && client.LinkNote() == ""
		})

		turn <- session.Event{Kind: session.EventTextDelta, Text: "e9"}
		agent.finish(turn)
		if ev, ok := nextEvent(t, events); !ok || ev.Text != "e9" {
			t.Fatalf("after second resume got %q (ok=%v), want e9", ev.Text, ok)
		}
		for range events {
		}

		gapsMu.Lock()
		finalGaps := append([]Gap(nil), gaps...)
		gapsMu.Unlock()
		if len(finalGaps) != 1 {
			t.Fatalf("second resume produced extra gap frames: %+v", finalGaps)
		}
	})

	// 3. An old-style client (nil OnGap / Dial reader that ignores unknown frame
	// kinds) still decodes all retained events after a gap frame.
	t.Run("old reader ignoring gap frame decodes the retained events", func(t *testing.T) {
		agent := &fakeAgent{model: "a/b"}
		sess := NewSession(engineOn(agent), true)

		firstViewer := dialSession(t, sess)
		if frame := firstViewer.hello(Hello{Version: Version}); frame.Kind != "welcome" {
			t.Fatalf("hello: %s", frame.Error)
		}
		ref := decode[StreamRef](t, firstViewer.ok(1, MethodSubmit, SubmitArgs{Text: "go"}).Payload)
		waitFor(t, "turn to open", func() bool { return agent.stream(0) != nil })
		turn := agent.stream(0)
		_ = firstViewer.toward.Close()

		for _, tok := range []string{
			"m1-" + strings.Repeat("a", 80),
			"m2-" + strings.Repeat("b", 80),
			"m3-" + strings.Repeat("c", 80),
			"m4-" + strings.Repeat("d", 80),
		} {
			turn <- session.Event{Kind: session.EventTextDelta, Text: tok}
		}
		waitFor(t, "ring to evict m1", func() bool {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			r := sess.rings[ref.Stream]
			return r != nil && r.last == 4 && r.first > 1
		})

		// Connect a standard Dial client (no Roaming / no OnGap) resuming from seq 0.
		surface, engine := Pipe()
		guard.Go("remote/test-w7-old-client", func() {
			_ = ServeAttach(engine, engine, AttachOptions{
				Open: func(Hello) (*Session, error) { return sess, nil },
			})
			_ = engine.Close()
		})
		oldClient, err := Dial(surface, "devbox", Hello{
			Resume: []StreamCursor{{Stream: ref.Stream, Seq: 0}},
		})
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		t.Cleanup(func() { _ = oldClient.Close() })

		_, liveEvents := oldClient.Live()
		if liveEvents == nil {
			t.Fatal("expected live stream on welcome")
		}
		agent.finish(turn)

		var got []string
		for ev := range liveEvents {
			if ev.Kind == session.EventTextDelta {
				got = append(got, ev.Text[:2])
			}
		}
		if len(got) == 0 || got[len(got)-1] != "m4" {
			t.Fatalf("old client decoded %v, want retained events ending in m4", got)
		}
	})
}

var _ = json.Unmarshal
