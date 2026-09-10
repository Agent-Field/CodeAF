package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// ── A TOOL CALL'S ARGUMENTS ARE THE ANSWER ARRIVING ─────────────────────────

// readingRecorder is the shipped controller with every reading it was handed
// written down, so a test can ask what the read loop called a fragment.
type readingRecorder struct {
	control.Controller
	mu       *sync.Mutex
	readings *[]control.Reading
}

func (r readingRecorder) Note(reading control.Reading) control.Act {
	r.mu.Lock()
	*r.readings = append(*r.readings, reading)
	r.mu.Unlock()
	return r.Controller.Note(reading)
}

// TestAToolCallFragmentIsAVisibleReading is law one at the seam that decides
// it. A fragment of a call being assembled is billed and streamed exactly as
// answer text is, so the controller is told it is VISIBLE, and its phase is
// writing — never a run of thought judged against how long this model thinks,
// which is how a ten-minute `write` was taken for a pathological thought and
// raced by a rescue whose eighteen thousand tokens nobody kept.
func TestAToolCallFragmentIsAVisibleReading(t *testing.T) {
	var mu sync.Mutex
	var readings []control.Reading
	var built atomic.Pointer[control.Controller]
	before := lanes.SetController(func(plan control.Plan) control.Controller {
		made := control.New(plan)
		built.Store(&made)
		return readingRecorder{Controller: made, mu: &mu, readings: &readings}
	})
	t.Cleanup(func() { lanes.SetController(before) })

	piece := strings.Repeat("a", 40)
	server := httptest.NewServer(writeCallStream("steady", piece, time.Millisecond, 20*time.Millisecond))
	defer server.Close()
	client := steadyClient(t, server.URL)
	ctx := WithStreamObserver(talking(), func(StreamEvent) {})
	if _, err := client.CompleteWithMessages(ctx, userMessages("write the page")); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	visible, hidden := 0, 0
	for _, reading := range readings {
		visible += reading.Visible
		hidden += reading.Hidden
	}
	if hidden != 0 {
		t.Fatalf("the controller was told %d hidden tokens for a stream that carried nothing but a call", hidden)
	}
	if visible < 10 {
		t.Fatalf("the controller was told %d visible tokens, want the call's arguments counted as the answer", visible)
	}
	controller := built.Load()
	if controller == nil {
		t.Fatal("no controller was built for the call")
	}
	if phase := (*controller).Phase(); phase != control.PhaseWriting {
		t.Fatalf("phase = %v, want writing: a call being assembled is the answer arriving", phase)
	}
}

// TestARescuesCallIsHeldWhileTheSpeakersCallIsOnTheScreen is the one-voice rule
// after law one. A call's arguments now count as visible progress, so the first
// fragment of a RESCUE'S call asks for the voice — and it must not get it while
// the person is watching the speaker's own call form. A forming call on the
// screen is the answer being read.
func TestARescuesCallIsHeldWhileTheSpeakersCallIsOnTheScreen(t *testing.T) {
	var mu sync.Mutex
	var heard []int
	race := &hedgeRace{winner: -1, held: map[int][]StreamEvent{}}
	race.observer = func(event StreamEvent) {
		mu.Lock()
		defer mu.Unlock()
		heard = append(heard, event.Index)
	}
	race.arms = []*hedgeArm{{index: 0, lane: "A"}, {index: 1, lane: "B"}}
	race.arms[0].watch = &streamWatch{race: race, arm: 0, tokens: 40}
	race.arms[1].watch = &streamWatch{race: race, arm: 1, tokens: 10}

	race.emit(0, StreamEvent{Kind: StreamToolCallForming, Index: 0})
	race.voice(1)
	race.emit(1, StreamEvent{Kind: StreamToolCallForming, Index: 1})

	if race.speaker != 0 {
		t.Fatalf("speaker = %d: the rescue's first call fragment took the voice from a call on the screen", race.speaker)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 1 || heard[0] != 0 {
		t.Fatalf("the person heard %v, want only the speaker's call", heard)
	}
	if len(race.held[1]) != 1 {
		t.Fatalf("the rescue's call was not held: %v", race.held)
	}
}

// writeCallStream serves one `write` call whose arguments arrive `piece` at a
// time, one fragment every `every`, naming `lane` on every chunk. With a
// positive `lasting` the call closes after that long and the stream finishes
// cleanly; with zero it runs until the request is cancelled.
func writeCallStream(lane, piece string, every, lasting time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush := func(frame string) bool {
			if _, err := fmt.Fprint(w, "data: "+frame+"\n\n"); err != nil {
				return false
			}
			w.(http.Flusher).Flush()
			return true
		}
		flush(`{"id":"one","provider":"` + lane + `","choices":[{"index":0,"delta":{"tool_calls":` +
			`[{"index":0,"id":"call_1","type":"function","function":{"name":"write","arguments":"{\"content\":\""}}]}}]}`)
		fragment := `{"id":"one","provider":"` + lane + `","choices":[{"index":0,"delta":{"tool_calls":` +
			`[{"index":0,"function":{"arguments":"` + piece + `"}}]}}]}`
		began := time.Now()
		for lasting <= 0 || time.Since(began) < lasting {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(every):
			}
			if !flush(fragment) {
				return
			}
		}
		flush(`{"id":"one","provider":"` + lane + `","choices":[{"index":0,"delta":{"tool_calls":` +
			`[{"index":0,"function":{"arguments":"\"}"}}]},"finish_reason":"tool_calls"}]}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
}

// steadyClient is a client on its own ledger whose lane `steady` of sim/model is
// measured at four hundred tokens a second and has finished nothing yet — so
// its wall, where a test shortens it, is the floor [shortenWall] set.
func steadyClient(t *testing.T, base string) *Client {
	t.Helper()
	client, err := NewClient(Config{APIKey: "k", BaseURL: base, Model: "sim/model"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.velocity = newVelocityLedger()
	client.velocity.brisk("sim/model", "steady")
	return client
}
