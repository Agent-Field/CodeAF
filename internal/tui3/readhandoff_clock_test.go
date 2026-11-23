package tui3

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type readingRunEngine struct{ session.RunEngine }

func (readingRunEngine) Start(context.Context, session.RunSpec) session.RunSummary {
	return session.RunSummary{Outcome: "a reading handoff should use a quick task"}
}

func TestHandedOffReadingRowsCloseBeforeTheChatContinues(t *testing.T) {
	session.RegisterRunEngine(readingRunEngine{})
	t.Cleanup(func() { session.RegisterRunEngine(nil) })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", t.TempDir())
	var chatCalls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"data":[]}`)
			return
		}
		var request struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		chat := len(request.Messages) > 0 && string(request.Messages[0].Content) == `"HANDOFF CHAT"`
		w.Header().Set("Content-Type", "text/event-stream")
		if chat && chatCalls.Add(1) == 1 {
			calls := []map[string]any{}
			for i := 0; i < 3; i++ {
				calls = append(calls, map[string]any{"index": i, "id": fmt.Sprintf("sweep-%d", i), "type": "function", "function": map[string]string{"name": "read", "arguments": fmt.Sprintf(`{"path":"file-%d.go"}`, i)}})
			}
			body, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": calls}, "finish_reason": "tool_calls"}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
			return
		}
		answer := "The files agree."
		if chat {
			answer = "The handoff returned."
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%q}}]}\n\n", answer)
		if chat {
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	defer close(release)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	agent, err := session.New(session.Config{
		Workspace: t.TempDir(), ProfileDir: t.TempDir(), Model: "fixture/reading", APIKey: "fixture", BaseURL: server.URL,
		System: "HANDOFF CHAT", TaskAudit: false, TaskRepairRounds: 0, DailyBudgetResolved: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close(); session.StopUsageWriter(session.UsageLedgerPath()) })
	events, err := agent.Submit(ctx, "Compare three files and report what they share.")
	if err != nil {
		t.Fatal(err)
	}
	a := placeApp(t)
	a.state = stateWorking
	boundaries := map[string][]session.EventKind{}
	for {
		select {
		case ev, open := <-events:
			if !open {
				t.Fatal("chat ended before continuing after the handoff")
			}
			a.feed.ingest(ev)
			if strings.HasPrefix(ev.CallID, "sweep-") {
				boundaries[ev.CallID] = append(boundaries[ev.CallID], ev.Kind)
			}
			if ev.Kind != session.EventTextDelta || !strings.Contains(ev.Text, "handoff returned") {
				continue
			}
			seen := 0
			for i := range a.entries {
				e := &a.entries[i]
				if !strings.HasPrefix(e.callID, "sweep-") {
					continue
				}
				seen++
				if e.status != toolOK || e.ended.IsZero() {
					t.Fatalf("reading row %s remains open after handoff: status=%v events=%v", e.callID, e.status, boundaries[e.callID])
				}
				before, _ := a.phoneClock(e)
				ended, duration := e.ended, e.ran
				at := a.now().Add(time.Hour)
				a.feed.hooks.now = func() time.Time { return at }
				after, _ := a.phoneClock(e)
				if before != after || e.ended != ended || e.ran != duration {
					t.Fatal("finished reading clock kept counting")
				}
			}
			if seen != 3 {
				t.Fatalf("saw %d intercepted rows, want 3", seen)
			}
			return
		case <-ctx.Done():
			t.Fatal("handoff did not return before the deadline")
		}
	}
}
