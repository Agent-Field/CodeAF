package appapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamFrame struct {
	id, kind, data, comment string
}

type streamClient struct {
	t       *testing.T
	scanner *bufio.Scanner
}

func openStream(t *testing.T, s *Server, query string, last ...string) *streamClient {
	t.Helper()
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	r, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/app/v1/events"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header = bearer()
	for _, id := range last {
		r.Header.Add("Last-Event-ID", id)
	}
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream response: %d %v", response.StatusCode, response.Header)
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	return &streamClient{t, scanner}
}

func (client *streamClient) next() streamFrame {
	client.t.Helper()
	var frame streamFrame
	for client.scanner.Scan() {
		line := client.scanner.Text()
		if line == "" {
			return frame
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			frame.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			frame.kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			frame.data = strings.TrimPrefix(line, "data: ")
		case strings.HasPrefix(line, ": "):
			frame.comment = strings.TrimPrefix(line, ": ")
		default:
			client.t.Fatalf("unexpected SSE line: %q", line)
		}
	}
	client.t.Fatalf("stream ended before a frame: %v", client.scanner.Err())
	return frame
}

func publish(t *testing.T, hub *EventHub, kind EventType, payload any) string {
	t.Helper()
	id, err := hub.Publish(kind, payload)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStreamHelloStartsLiveAndSharesTheLaunchEpoch(t *testing.T) {
	s := New(testToken, Options{Version: "test-build", Epoch: "epoch-a"})
	old := publish(t, s.Events(), EventAgentCreated, AgentEvent{Agent: "old"})
	client := openStream(t, s, "")
	hello := client.next()
	if hello.kind != "hello" || hello.id != old || strings.Contains(hello.data, testToken) {
		t.Fatalf("wrong hello: %+v", hello)
	}
	var data streamHello
	if err := json.Unmarshal([]byte(hello.data), &data); err != nil || data != (streamHello{"epoch-a", "test-build", Contract}) {
		t.Fatalf("hello differs from launch: %+v %v", data, err)
	}
	id := publish(t, s.Events(), EventTaskChanged, map[string]any{"agent": "a", "task": "t", "state": "your_call"})
	frame := client.next()
	if frame.kind != "task.changed" || frame.id != id || strings.Contains(frame.data, "old") {
		t.Fatalf("first connection replayed history: %+v", frame)
	}
	if id != "e_epoch-a_2" {
		t.Fatalf("wrong epoch cursor: %s", id)
	}
}

func TestStreamReplaysStrictlyAfterCursorThenContinuesLive(t *testing.T) {
	s := New(testToken, Options{Epoch: "epoch-a"})
	first := publish(t, s.Events(), EventAgentCreated, AgentEvent{Agent: "a"})
	second := publish(t, s.Events(), EventAgentChanged, AgentEvent{Agent: "a", Fields: []string{"name"}})
	third := publish(t, s.Events(), EventTaskChanged, map[string]string{"agent": "a", "task": "t"})
	client := openStream(t, s, "", first)
	if hello := client.next(); hello.kind != "hello" || hello.id != "" {
		t.Fatalf("reconnect hello overwrote replay cursor: %+v", hello)
	}
	for _, id := range []string{second, third} {
		if frame := client.next(); frame.id != id {
			t.Fatalf("replay out of order: %+v want %s", frame, id)
		}
	}
	live := publish(t, s.Events(), EventAgentRemoved, AgentEvent{Agent: "a"})
	if frame := client.next(); frame.id != live || frame.kind != "agent.removed" {
		t.Fatalf("live delivery after replay: %+v", frame)
	}
}

func TestStreamResetForUnknownExpiredAndPriorEpochCursors(t *testing.T) {
	s := New(testToken, Options{Epoch: "epoch-a"})
	for range EventRing + 1 {
		publish(t, s.Events(), EventAgentChanged, AgentEvent{Agent: "a"})
	}
	for _, test := range []struct {
		name, reason string
		cursor       []string
	}{
		{"expired", "expired", []string{s.events.id(1)}},
		{"expired baseline", "expired", []string{s.events.id(0)}},
		{"prior epoch", "epoch_changed", []string{"e_epoch-b_4097"}},
		{"future", "unknown", []string{"e_epoch-a_18446744073709551615"}},
		{"legacy numeric", "unknown", []string{"e_1"}},
		{"malformed", "unknown", []string{"garbage"}},
		{"overflow", "unknown", []string{"e_epoch-a_18446744073709551616"}},
		{"noncanonical", "unknown", []string{"e_epoch-a_01"}},
		{"negative", "unknown", []string{"e_epoch-a_-1"}},
		{"empty", "unknown", []string{""}},
		{"repeated", "unknown", []string{s.events.id(4097), s.events.id(4097)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := openStream(t, s, "", test.cursor...)
			if hello := client.next(); hello.kind != "hello" || hello.id != "" {
				t.Fatalf("missing reconnect hello: %+v", hello)
			}
			frame := client.next()
			var reset streamReset
			if err := json.Unmarshal([]byte(frame.data), &reset); err != nil || frame.kind != "reset" || reset != (streamReset{"epoch-a", test.reason}) {
				t.Fatalf("reset: %+v %+v %v", frame, reset, err)
			}
			// A reset moves to the head, so exactly one reset precedes the next
			// publication instead of replaying an untrustworthy partial window.
			id := publish(t, s.Events(), EventSettingsChanged, map[string]any{"keys": []string{"models.default"}})
			if next := client.next(); next.id != id || next.kind != "settings.changed" {
				t.Fatalf("reset did not return to live: %+v", next)
			}
		})
	}
}

func TestEventRingRetainsExactly4096RecordsAndOldestCursor(t *testing.T) {
	hub := newEventHub("epoch")
	for range EventRing + 1 {
		publish(t, hub, EventAgentChanged, AgentEvent{Agent: "a"})
	}
	batch, head, gap, _, _ := hub.read(2)
	if gap || head != EventRing+1 || len(batch) != EventRing-1 || batch[0].seq != 3 || batch[len(batch)-1].seq != head {
		t.Fatalf("oldest retained cursor cannot replay: head=%d gap=%v records=%d", head, gap, len(batch))
	}
	if _, _, gap, _, _ := hub.read(1); !gap {
		t.Fatal("evicted cursor was still accepted")
	}
	if _, reason := hub.cursor(http.Header{"Last-Event-Id": {hub.id(2)}}); reason != "" {
		t.Fatalf("oldest retained id reset: %s", reason)
	}
	if _, reason := hub.cursor(http.Header{"Last-Event-Id": {hub.id(head)}}); reason != "" {
		t.Fatalf("head cursor reset: %s", reason)
	}
	seen := map[uint64]bool{}
	for _, record := range hub.ring {
		seen[record.seq] = true
	}
	if len(seen) != EventRing || seen[1] || !seen[2] || !seen[head] {
		t.Fatal("ring did not overwrite exactly its oldest record")
	}
}

func TestStreamAgentFiltersApplyEquallyToReplayAndLive(t *testing.T) {
	for _, query := range []string{"", "?agent=a", "?agent=a&agent=b&agent=a"} {
		for _, replay := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/replay=%v", query, replay), func(t *testing.T) {
				s := New(testToken, Options{Epoch: "epoch"})
				var client *streamClient
				var last []string
				if replay {
					last = []string{s.events.id(0)}
				} else {
					client = openStream(t, s, query)
					client.next()
				}
				var want []string
				for _, agent := range []string{"a", "b", "c"} {
					id := publish(t, s.Events(), EventAgentChanged, AgentEvent{Agent: agent})
					selected := strings.Contains(query, "agent="+agent)
					if query == "" || selected {
						want = append(want, id)
					}
					for _, kind := range []string{"text", "thinking"} {
						id = publish(t, s.Events(), EventTurnDelta, TurnDeltaEvent{agent, "turn", kind, "line 1\nline 2"})
						if selected {
							want = append(want, id)
						}
					}
				}
				want = append(want, publish(t, s.Events(), EventSettingsChanged, map[string]any{"keys": []string{"models.default"}}))
				if replay {
					client = openStream(t, s, query, last...)
					client.next()
				}
				for _, id := range want {
					frame := client.next()
					if frame.id != id || !json.Valid([]byte(frame.data)) {
						t.Fatalf("filter %s: %+v want %s", query, frame, id)
					}
				}
				// A sentinel proves that hidden frames do not remain queued and
				// that the next visible frame still arrives after filtering.
				id := publish(t, s.Events(), EventActivity, map[string]string{"verb": "changed"})
				if frame := client.next(); frame.id != id {
					t.Fatalf("hidden events leaked: %+v", frame)
				}
			})
		}
	}
}

func TestPublicationOwnsItsPayloadAndRejectsInvalidFrames(t *testing.T) {
	hub := newEventHub("epoch")
	payload := map[string]any{"agent": "a", "fields": []string{"name"}}
	publish(t, hub, EventAgentChanged, payload)
	payload["agent"] = "b"
	payload["fields"].([]string)[0] = "model"
	batch, _, _, _, _ := hub.read(0)
	if len(batch) != 1 || batch[0].agent != "a" || string(batch[0].data) != `{"agent":"a","fields":["name"]}` {
		t.Fatalf("caller mutation changed replay: %+v", batch)
	}
	for _, test := range []struct {
		kind EventType
		data any
	}{
		{"hello", map[string]string{}}, {"reset", map[string]string{}},
		{"unknown", map[string]string{}}, {"agent.changed\nevent: reset", map[string]string{}},
		{EventProcessOutput, map[string]string{"process": "p", "text": "log"}},
		{EventAgentChanged, nil}, {EventAgentChanged, []string{"a"}},
		{EventAgentChanged, "string"}, {EventAgentChanged, map[string]int{"agent": 4}},
		{EventAgentChanged, map[string]any{"value": make(chan int)}},
		{EventTurnDelta, TurnDeltaEvent{"", "t", "text", ""}},
		{EventTurnDelta, TurnDeltaEvent{"a", "", "text", ""}},
		{EventTurnDelta, TurnDeltaEvent{"a", "t", "other", ""}},
	} {
		if id, err := hub.Publish(test.kind, test.data); err == nil || id != "" {
			t.Fatalf("invalid publication accepted: %q %v", test.kind, test.data)
		}
	}
	if hub.seq != 1 {
		t.Fatalf("invalid publications advanced the cursor: %d", hub.seq)
	}
}

func TestHubPublicationClosesCapturedWakeupAndSerialisesConcurrentWriters(t *testing.T) {
	hub := newEventHub("epoch")
	_, _, _, changed, _ := hub.read(0)
	publish(t, hub, EventAgentChanged, AgentEvent{Agent: "a"})
	select {
	case <-changed:
	default:
		t.Fatal("publication after read lost its wakeup")
	}
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			for range 100 {
				if _, err := hub.Publish(EventAgentChanged, AgentEvent{Agent: "a"}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	writers.Wait()
	batch, head, gap, _, _ := hub.read(0)
	if gap || len(batch) != 801 || head != 801 {
		t.Fatalf("concurrent publications lost: %d %d %v", len(batch), head, gap)
	}
	for i, record := range batch {
		if record.seq != uint64(i+1) {
			t.Fatal("concurrent event ids are not monotonic")
		}
	}
}

// frameWriter reports flushes so tests advance clocks and writes at known
// stream boundaries, never by guessing how long the handler needs to run.
type frameWriter struct {
	*httptest.ResponseRecorder
	frames  chan string
	fail    bool
	onFlush func()
}

func (w *frameWriter) Write(data []byte) (int, error) {
	if w.fail {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(data)
}

func (w *frameWriter) Flush() {
	w.frames <- w.Body.String()
	w.Body.Reset()
	w.ResponseRecorder.Flush()
	if w.onFlush != nil {
		w.onFlush()
	}
}

func awaitFrame(t *testing.T, frames <-chan string) string {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not flush its next frame")
		return ""
	}
}

func TestStreamKeepaliveAndCancellationUseAnInjectedClock(t *testing.T) {
	s := New(testToken, Options{Epoch: "epoch"})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &frameWriter{ResponseRecorder: httptest.NewRecorder(), frames: make(chan string, 4)}
	r := httptest.NewRequest("GET", "/app/v1/events", nil).WithContext(ctx)
	beat := make(chan time.Time)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(done)
		s.streamWithClock(w, r, func() (<-chan time.Time, func()) { return beat, func() { close(stopped) } })
	}()
	if hello := awaitFrame(t, w.frames); !strings.Contains(hello, "event: hello\n") {
		t.Fatalf("missing first hello: %s", hello)
	}
	beat <- time.Time{}
	if frame := awaitFrame(t, w.frames); frame != ": keepalive\n\n" {
		t.Fatalf("keepalive became a data event: %s", frame)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled stream did not end")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("stream did not release its ticker")
	}
	if eventKeepalive != 20*time.Second {
		t.Fatalf("keepalive interval changed: %s", eventKeepalive)
	}
}

func TestShutdownEndsIdleStreamsAndRefusesFurtherPublications(t *testing.T) {
	s := New(testToken, Options{Epoch: "epoch"})
	client := openStream(t, s, "")
	client.next()
	w := request(t, s, "POST", "/app/v1/shutdown", `{}`, bearer(), "127.0.0.1:4000")
	if w.Code != 200 || client.scanner.Scan() || client.scanner.Err() != nil {
		t.Fatalf("shutdown left an idle stream open: %v", client.scanner.Err())
	}
	s.Events().Close()
	if _, err := s.Events().Publish(EventAgentChanged, AgentEvent{Agent: "a"}); err == nil {
		t.Fatal("closed hub accepted a publication")
	}
}

type noFlushWriter struct{ http.ResponseWriter }

func TestStreamRefusesUnsupportedWritersInvalidFiltersAndEndsOnWriteError(t *testing.T) {
	s := New(testToken, Options{Epoch: "epoch"})
	r := httptest.NewRequest("GET", "/app/v1/events", nil)
	w := httptest.NewRecorder()
	s.stream(noFlushWriter{w}, r)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "cannot stream") {
		t.Fatalf("unsupported stream: %d %s", w.Code, w.Body)
	}
	w = request(t, s, "GET", "/app/v1/events?agent=", "", bearer(), "127.0.0.1:4000")
	if w.Code != 422 {
		t.Fatalf("empty agent filter accepted: %d", w.Code)
	}
	s.streamWithClock(&frameWriter{ResponseRecorder: httptest.NewRecorder(), fail: true}, r, func() (<-chan time.Time, func()) {
		t.Fatal("failed hello started a ticker")
		return nil, func() {}
	})
}

func TestStreamCatchesPublicationDuringHelloAndResetsOnReaderOverrun(t *testing.T) {
	for _, count := range []int{1, EventRing, EventRing + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := New(testToken, Options{Epoch: "epoch"})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			w := &frameWriter{ResponseRecorder: httptest.NewRecorder(), frames: make(chan string, EventRing+3)}
			first := true
			w.onFlush = func() {
				if first {
					first = false
					for range count {
						publish(t, s.Events(), EventAgentChanged, AgentEvent{Agent: "a"})
					}
				} else {
					cancel()
				}
			}
			s.streamWithClock(w, httptest.NewRequest("GET", "/app/v1/events", nil).WithContext(ctx), func() (<-chan time.Time, func()) { return nil, func() {} })
			if hello := awaitFrame(t, w.frames); !strings.Contains(hello, "id: e_epoch_0\n") {
				t.Fatalf("wrong baseline: %s", hello)
			}
			frame := awaitFrame(t, w.frames)
			if count <= EventRing {
				if !strings.Contains(frame, "id: e_epoch_1\nevent: agent.changed\n") {
					t.Fatalf("publication during hello was lost: %s", frame)
				}
			} else if !strings.Contains(frame, "event: reset\n") || !strings.Contains(frame, `"reason":"expired"`) || !strings.Contains(frame, "id: e_epoch_4097\n") {
				t.Fatalf("reader overrun did not reset at head: %s", frame)
			}
		})
	}
}

func TestEmptyEpochIsFreshAndZeroCursorReplaysTheEntireRetainedWindow(t *testing.T) {
	s, other := New(testToken, Options{}), New(testToken, Options{})
	if s.options.Epoch == "" || s.options.Epoch == other.options.Epoch || s.options.Epoch == testToken {
		t.Fatal("server without an epoch reused an identity or credential")
	}
	for range EventRing {
		publish(t, s.Events(), EventAgentChanged, AgentEvent{Agent: "a"})
	}
	if _, reason := s.events.cursor(http.Header{"Last-Event-Id": {s.events.id(0)}}); reason != "" {
		t.Fatalf("complete replay window rejected its baseline: %s", reason)
	}
	client := openStream(t, s, "", s.events.id(0))
	client.next()
	for seq := uint64(1); seq <= EventRing; seq++ {
		if frame := client.next(); frame.id != s.events.id(seq) || frame.kind != "agent.changed" {
			t.Fatalf("full ring replay at %d: %+v", seq, frame)
		}
	}
}
