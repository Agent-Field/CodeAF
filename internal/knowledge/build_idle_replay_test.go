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

// TWELVE CHATS COST TWELVE COMPILES. Replayed on a scripted clock in the
// shape of live lane E6b's run, the loop as it was made 46 Library calls (one
// per pass here, where every pass finds nothing to learn), compiling each chat
// in slices while it grew; held until each chat goes quiet it makes one per
// chat (build_idle.go).
func TestAScriptedSessionCompilesEachChatOnceAfterItGoesQuiet(t *testing.T) {
	for _, idle := range []time.Duration{2 * time.Minute} {
		t.Run(fmt.Sprint("idle=", idle), func(t *testing.T) {
			var clock atomic.Int64
			start := time.Date(2026, 11, 16, 22, 29, 0, 0, time.UTC)
			clock.Store(start.Add(-time.Hour).UnixNano())
			now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
			knowledge.SetBuildTimingForTest(t, 2*time.Millisecond, 20*time.Millisecond, idle, now)
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
			for step := 0; step <= (28*60)/2; step++ {
				clock.Store(start.Add(time.Duration(step) * 2 * time.Second).UnixNano())
				time.Sleep(4 * time.Millisecond)
			}
			time.Sleep(200 * time.Millisecond)
			cancel()
			<-done
			if n := calls.Load(); n == 0 || n > 12 {
				t.Fatalf("twelve chats made %d Library calls; want one compile per chat, after it goes quiet", n)
			}
		})
	}
}
