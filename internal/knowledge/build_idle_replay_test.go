package knowledge_test

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// replayHistory grows twelve chats on the scripted clock: each is written at
// every turn, four turns thirty seconds apart, the next chat starting when the
// last ends — the shape of live lane E6b's run.
type replayHistory struct {
	now   func() time.Time
	start time.Time
}

func (h replayHistory) Inventory(context.Context) ([]knowledge.Journal, error) {
	var out []knowledge.Journal
	elapsed := h.now().Sub(h.start)
	for chat := 0; chat < 12; chat++ {
		begin := time.Duration(chat) * 2 * time.Minute
		if elapsed < begin {
			break
		}
		turns := int((elapsed-begin)/(30*time.Second)) + 1
		if turns > 4 {
			turns = 4
		}
		modified := h.start.Add(begin + time.Duration(turns-1)*30*time.Second)
		out = append(out, knowledge.Journal{ID: fmt.Sprintf("chat-%d", chat), Session: fmt.Sprintf("s-%d", chat), Size: int64(turns), Modified: modified})
	}
	return out, nil
}

func (h replayHistory) Read(_ context.Context, j knowledge.Journal, c knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	var sources []knowledge.Source
	for turn := c.Offset; turn < j.Size; turn++ {
		sources = append(sources, knowledge.Source{ID: fmt.Sprintf("%s-%d", j.ID, turn), Session: j.Session, Role: "user", TS: "2026-11-16T22:29:00Z", Text: fmt.Sprintf("Cedar queues job %s %d.", j.ID, turn)})
	}
	return knowledge.JournalBatch{Sources: sources, Cursor: knowledge.JournalCursor{Offset: j.Size}}, nil
}

// TWELVE CHATS COST A FEW BATCHED COMPILES, AND NOTHING AFTER THE LAST.
// Replayed on a scripted clock in the shape of live lane E6b's run, the loop
// as it first was made 46 Library calls (one per pass here, where every pass
// finds nothing to learn), compiling each chat in slices while it grew. Held
// until each chat went quiet it made one per chat, twelve; held until the
// sitting goes quiet (no chat waits more than buildBatchMax) it makes a few,
// and once the last chat is compiled the loop calls nothing more however long
// the engine stays up (build_idle.go).
func TestAScriptedSessionCompilesItsChatsInBatchesAndNothingAfterIdle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		batch time.Duration
		max   int64
	}{
		{"each chat on its own", 0, 12},
		{"one sitting", 10 * time.Minute, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var clock atomic.Int64
			start := time.Date(2026, 11, 16, 22, 29, 0, 0, time.UTC)
			clock.Store(start.Add(-time.Hour).UnixNano())
			now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			knowledge.SetBuildTimingForTest(t, 2*time.Millisecond, 20*time.Millisecond, 2*time.Minute, now)
			knowledge.SetBuildBatchForTest(tc.batch)
			v := knowledge.Open(libraryAccountingProfile(t))
			if err := v.StartBuild(); err != nil {
				t.Fatal(err)
			}
			withKnowledgeModel(t, func(p map[string]any) map[string]any {
				return map[string]any{"subjects": []any{}, "claims": []any{}}
			})
			fixture := http.DefaultTransport
			var calls atomic.Int64
			http.DefaultTransport = knowledgeTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return fixture.RoundTrip(r)
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- v.Run(ctx, replayHistory{now: now, start: start}, func() string { return "fixture" }) }()
			waitPhase(t, v, knowledge.BuildWaiting)
			// 24 minutes of chats plus 4 quiet ones, a scripted second per 2s of loop.
			step := 0
			for ; step <= (28*60)/2; step++ {
				clock.Store(start.Add(time.Duration(step) * 2 * time.Second).UnixNano())
				time.Sleep(4 * time.Millisecond)
			}
			time.Sleep(200 * time.Millisecond)
			atIdle := calls.Load()
			// An hour of an idle engine after the sitting.
			for end := step + 3600/2; step <= end; step++ {
				clock.Store(start.Add(time.Duration(step) * 2 * time.Second).UnixNano())
				time.Sleep(time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)
			cancel()
			<-done
			t.Logf("twelve chats: %d Library calls by idle, %d after", atIdle, calls.Load()-atIdle)
			if atIdle == 0 || atIdle > tc.max {
				t.Fatalf("twelve chats made %d Library calls; want 1..%d", atIdle, tc.max)
			}
			if after := calls.Load() - atIdle; after != 0 {
				t.Fatalf("an idle hour after the sitting made %d Library calls; want none", after)
			}
		})
	}
}
