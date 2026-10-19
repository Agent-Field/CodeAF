package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// C3: replaying a killed tool batch must be legal for the provider. The
// session's existing repair drops the unfinished batch while retaining all
// finished steps; the new private wake tells the model to check their effects.
func TestResumeAfterCloseC3RepairsUnfinishedToolBatch(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "no result", true: "partial results"}[partial], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			journal := `{"type":"session","version":1,"id":"closed"}` + "\n" +
				`{"type":"message","role":"user","content":"finish the changes"}` + "\n" +
				`{"type":"message","role":"assistant","toolCalls":[{"id":"finished","type":"function","function":{"name":"read","arguments":"{\"path\":\"finished\"}"}}]}` + "\n" +
				`{"type":"message","role":"tool","toolCallId":"finished","content":"the finished step's result"}` + "\n" +
				`{"type":"message","role":"assistant","content":"Finished the first step."}` + "\n" +
				`{"type":"message","role":"assistant","toolCalls":[{"id":"cut1","type":"function","function":{"name":"read","arguments":"{\"path\":\"one\"}"}},{"id":"cut2","type":"function","function":{"name":"read","arguments":"{\"path\":\"two\"}"}}]}` + "\n"
			if partial {
				journal += `{"type":"message","role":"tool","toolCallId":"cut1","content":"one result"}` + "\n"
			}
			if err := os.WriteFile(path, []byte(journal), 0600); err != nil {
				t.Fatal(err)
			}
			called := make(chan []ai.Message, 1)
			completer := &scriptedCompleter{steps: []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
				called <- append([]ai.Message(nil), messages...)
				return textResponse("Finished the rest."), nil
			}}}
			agent, _ := newTestAgent(t, completer, func(c *Config) { c.SessionFile = path })
			wakes, stop := agent.WatchWakes()
			defer stop()
			if !agent.ResumeAfterClose() {
				t.Fatal("resume was not queued")
			}
			events := <-wakes
			collect(t, events)
			messages := <-called
			finished, note := false, false
			calls, results := 0, 0
			for _, m := range messages {
				for _, call := range m.ToolCalls {
					if call.ID != "finished" {
						t.Fatalf("unfinished call reached provider: %+v", messages)
					}
					calls++
				}
				if m.Role == "tool" {
					if m.ToolCallID != "finished" {
						t.Fatalf("unpaired result reached provider: %+v", messages)
					}
					results++
				}
				text := messageContentText(m)
				finished = finished || strings.Contains(text, "Finished the first step.")
				note = note || strings.Contains(text, ResumeAfterCloseNote)
			}
			if calls != 1 || results != 1 {
				t.Fatalf("finished tool batch was lost or unpaired: calls=%d results=%d", calls, results)
			}
			if !finished || !note {
				t.Fatalf("finished step=%v, close instruction=%v", finished, note)
			}
			if resumesUnanswered(StopByClosing) {
				t.Fatal("close was added to the takeover resume door")
			}
		})
	}
}

// C8: the forensic reader retains stale evidence without changing the live
// reader's staleness-only law, and both refuse unknown presence schemas.
func TestResumeAfterCloseC8AnyAgeDoesNotLoosenFreshReader(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 19, 16, 30, 0, 0, time.UTC)
	for _, schema := range []int{1, 2} {
		p := SessionPresence{Schema: schema, PID: 987654, UpdatedAt: now.Add(-time.Hour), State: PresenceWorking}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "presence.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		read, ok := ReadSessionPresenceAnyAge(dir)
		if ok != (schema == 1) || (ok && (!read.UpdatedAt.Equal(p.UpdatedAt) || read.Dir != dir)) {
			t.Fatalf("any-age=%+v,%v", read, ok)
		}
		if _, ok := ReadSessionPresence(dir, now); ok {
			t.Fatal("the live reader believed stale evidence")
		}
	}
}

// C3, C13 (G1): a buffered failed wake carries no credit for a subsequently
// queued close note whose own wake is refused by the spending rail. Only the
// later turn that actually consumes that note reports its matching key.
func TestResumeAfterCloseKeyBelongsOnlyToConsumingTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return nil, fmt.Errorf("unrelated wake failed")
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("carried on"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(c *Config) { c.SpendRailUSD = 1 })
	wakes, stop := agent.WatchWakes()
	defer stop()
	if !agent.enqueueNote(wakeNote("unrelated news")) {
		t.Fatal("fixture wake refused")
	}
	first := collect(t, <-wakes)
	spend(agent, 2)
	if !agent.ResumeAfterCloseKey("owed-key") {
		t.Fatal("close note was not queued")
	}
	select {
	case <-wakes:
		t.Fatal("spending rail allowed the close wake")
	default:
	}
	for _, e := range first {
		if e.Kind == EventCloseContinued {
			t.Fatal("unrelated buffered turn credited close note")
		}
	}
	spend(agent, 0)
	agent.mu.Lock()
	started := agent.wakeLocked()
	agent.mu.Unlock()
	if !started {
		t.Fatal("own continuation did not start")
	}
	events := collect(t, <-wakes)
	seen := 0
	for _, e := range events {
		if e.Kind == EventCloseContinued {
			seen++
			if e.Text != "owed-key" {
				t.Fatalf("key=%q", e.Text)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("consumption acknowledgements=%d", seen)
	}
}

// C3, C13 (G1): recovery refuses to attach a new close promise to a turn that
// was already answering before that promise was offered.
func TestResumeAfterCloseKeyRefusesAlreadyStartedTurn(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		close(entered)
		<-release
		return textResponse("done"), nil
	}}}
	agent, _ := newTestAgent(t, completer, nil)
	events := mustSubmit(t, agent, "newer request")
	<-entered
	accepted := agent.ResumeAfterCloseKey("old-promise")
	close(release)
	collected := collect(t, events)
	if accepted {
		t.Fatal("close note joined a preexisting turn")
	}
	for _, e := range collected {
		if e.Kind == EventCloseContinued {
			t.Fatal("old turn completed new pending marker")
		}
	}
}
