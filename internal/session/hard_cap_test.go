package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The same four crews, cap and provider script measure both revisions. Each
// crew completes a tool pair before its held call. Two calls are replying;
// the last daily-capped call waits for its first token and has a zero bill.
// Job caps retain their separate immediate-cancellation rule.
func hardCapCrews(t *testing.T, requireStop bool) { hardCapScope(t, requireStop, false, false) }

func hardCapScope(t *testing.T, requireStop, jobCap, interim bool) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	const capUSD = .24
	budget := &streamJobBudget{namedBudgetStub: namedBudgetStub{liveBudgetStub: liveBudgetStub{cap: capUSD}, job: "fixture/job#1"}}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCalls := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCalls()
	profile := t.TempDir()
	dayCap := capUSD
	if jobCap {
		dayCap = 0
	}
	if err := config.WriteDailyBudgetUSD(profile, dayCap); err != nil {
		t.Fatal(err)
	}
	cross := make(chan struct{})
	agents := make([]*Agent, 4)
	events := make([]<-chan Event, 4)
	cancelled := make(chan int, 4)
	var requests atomic.Int32
	settled := make(chan struct{})
	var completed atomic.Int32
	for i := range agents {
		ready := make(chan struct{})
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				http.NotFound(w, r)
				return
			}
			var request ai.Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode provider request: %v", err)
				http.Error(w, "bad request", 400)
				return
			}
			validateToolPairs(t, request.Messages)
			requests.Add(1)
			n := calls.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			if n == 1 {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"paired\",\"type\":\"function\",\"function\":{\"name\":\"touch\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"cost\":0}}\n\ndata: [DONE]\n\n")
				return
			}
			if n > 2 {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"resumed\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				return
			}
			if i == 1 || i == 2 {
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.02}}\n\n")
			}
			if jobCap || interim || i != 3 {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}]}\n\n")
			}
			w.(http.Flusher).Flush()
			close(ready)
			if i == 0 {
				select {
				case <-cross:
				case <-r.Context().Done():
					return
				}
			} else {
				select {
				case <-r.Context().Done():
					cancelled <- i
					return
				case <-release:
				}
			}
			cost := .10
			if i == 0 {
				cost = .25
			}
			if i == 0 && interim {
				fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":4,\"cost\":0.25}}\n\n")
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					cancelled <- i
					return
				case <-release:
				}
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":4,\"cost\":%.2f}}\n\ndata: [DONE]\n\n", cost)
		}))
		// Close the held requests before the server waits for its handlers.
		t.Cleanup(server.Close)
		client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		var completer Completer = client
		if interim {
			completer = &settledStreamBarrier{Completer: client, arrived: &completed, ready: settled}
		}
		a, _ := newTestAgent(t, completer, func(c *Config) {
			c.OneModel = true
			c.ProfileDir = profile
			if jobCap {
				c.JobBudget = budget
			}
		})
		a.tools = append(a.tools, countingTool("touch", make(chan string, 1), nil))
		agents[i] = a
		events[i] = mustSubmit(t, a, "work")
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			t.Fatal("crew did not hold its call open")
		}
		// Seeing text emitted after usage proves the client decoded the interim
		// frame before another crew crosses the cap.
		observed := !jobCap && !interim && i == 3
		for !observed {
			select {
			case event, open := <-events[i]:
				if !open {
					t.Fatal("held crew ended before crossing")
				}
				observed = event.Kind == EventTextDelta && strings.Contains(event.Text, "working")
			case <-time.After(10 * time.Second):
				t.Fatal("interim usage was not followed by streamed text")
			}
		}
	}
	close(cross)
	stopped := 0
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	wantCancelled := 1
	if jobCap {
		wantCancelled = 3
	}
	if interim {
		wantCancelled = 4
	}
waiting:
	for stopped < wantCancelled {
		select {
		case <-cancelled:
			stopped++
		case <-deadline.C:
			break waiting
		}
	}
	if interim {
		if !FlushUsage() {
			t.Fatal("flush before settlement")
		}
		spent, err := DailySpendUSD(UsageLedgerPath(), time.Now())
		if err != nil || spent != 0 {
			t.Fatalf("open streams recorded=%g err=%v", spent, err)
		}
		for i, a := range agents {
			a.mu.Lock()
			stopped := a.workStopped
			a.mu.Unlock()
			if stopped {
				t.Fatalf("crew %d stopped before settlement", i)
			}
		}
	}
	releaseCalls()
	notices := make([]string, 4)
	dailyStops := make([]bool, 4)
	for i := range agents {
		for _, event := range collect(t, events[i]) {
			notices[i] += event.Text
			dailyStops[i] = dailyStops[i] || errors.Is(event.Err, ErrSpendRail) && IsDailyLimit(event.Err) != jobCap
		}
		if err := agents[i].WaitReceipts(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if stopped == 3 {
		waitFor(t, "cancelled calls settle their measured usage", func() bool {
			FlushUsage()
			spent, err := DailySpendUSD(UsageLedgerPath(), time.Now())
			rows, readErr := ReadUsage(UsageLedgerPath(), time.Time{})
			unknown := 0
			for _, row := range rows {
				if row.Unbilled {
					unknown++
				}
			}
			return err == nil && readErr == nil && spent >= .2899 && unknown >= 1
		})
	}
	if !FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	rows, err := ReadUsage(UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var spent float64
	unknown := 0
	for _, row := range rows {
		spent += row.USD
		if row.Unbilled {
			unknown++
		}
	}
	t.Logf("crews=4 cap=$%.2f recorded=$%.2f past-cap=$%.2f cancelled=%d/4 unknown=%d", capUSD, spent, spent-capUSD, stopped, unknown)
	if interim {
		if stopped != 0 || spent < .5499 || spent > .5501 {
			t.Fatalf("known interim limit changed: spent=%g cancelled=%d/4", spent, stopped)
		}
	}
	if !requireStop {
		return
	}
	wantStops, wantSpend := 1, .45
	if jobCap {
		wantStops, wantSpend = 3, .29
	}
	if stopped != wantStops {
		t.Fatalf("cap crossing cancelled %d/4 running crew calls, want %d waiting calls", stopped, wantStops)
	}
	if spent < wantSpend-.0001 || spent > wantSpend+.0001 || unknown != 1 {
		t.Fatalf("partial charges or unknown call lost: spent=%v unknown=%d", spent, unknown)
	}
	if !strings.Contains(notices[0], " done") {
		t.Errorf("settled crossing call lost its answer: %q", notices[0])
	}
	for i := 1; i < 4; i++ {
		validateToolPairs(t, agents[i].snapshot())
		if !jobCap && i < 3 && !strings.Contains(notices[i], " done") {
			t.Errorf("replying crew %d lost its completed answer: %q", i, notices[i])
		}
		if (jobCap || i == 3) && !dailyStops[i] {
			t.Errorf("crew %d did not report a typed daily stop", i)
		}
		word, raise := "daily limit", "/budget day"
		if jobCap {
			word, raise = "job budget", "raise"
		}
		waitFor(t, "limit notice persisted after drain", func() bool {
			for _, entry := range agents[i].Transcript() {
				if strings.Contains(entry.Text, word) && strings.Contains(entry.Text, raise) {
					return true
				}
			}
			return false
		})
		if jobCap && (!strings.Contains(notices[i], word) || !strings.Contains(notices[i], raise)) {
			t.Errorf("crew %d did not explain stop and raise: %q", i, notices[i])
		}
	}
	// The crossing call has already completed, so it reports the cap through
	// the next turn's admission rather than a stop of work that is now idle.
	refused := collect(t, mustSubmit(t, agents[0], "still over cap"))
	if len(refused) != 1 || !errors.Is(refused[0].Err, ErrSpendRail) || IsDailyLimit(refused[0].Err) == jobCap {
		t.Fatalf("completed crossing call reopened: %v", refused)
	}
	if _, err := agents[0].RunOrchestrate(context.Background(), "new crew", "", 5); err == nil {
		t.Error("completed crossing call admitted a new run")
	}
	for i := 1; i < 4; i++ {
		waitFor(t, "budget stop joins", func() bool { agents[i].mu.Lock(); defer agents[i].mu.Unlock(); return !agents[i].workStopping })
		if _, err := agents[i].Submit(context.Background(), "still over cap"); !errors.Is(err, ErrSpendRail) {
			t.Errorf("crew %d reopened below the required raise: %v", i, err)
		}
		if _, err := agents[i].RunOrchestrate(context.Background(), "new crew", "", 5); err == nil {
			t.Errorf("crew %d admitted a new run after stopping", i)
		}
	}
	if requests.Load() != 8 {
		t.Fatalf("retry or auxiliary call escaped: requests=%d", requests.Load())
	}
	if jobCap {
		budget.mu.Lock()
		budget.cap = 1
		budget.mu.Unlock()
	}
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		// Public submission reopens only after the old work has finished stopping.
		waitFor(t, "old owned work joins", func() bool { agents[i].mu.Lock(); defer agents[i].mu.Unlock(); return !agents[i].workStopping })
		collect(t, mustSubmit(t, agents[i], "resume"))
	}
	if requests.Load() != 12 {
		t.Fatalf("raise failed to resume chats: requests=%d", requests.Load())
	}
}

func TestHardCapCancelsEveryRunningCrew(t *testing.T)   { hardCapCrews(t, true) }
func TestHardCapConcurrentCallMeasurement(t *testing.T) { hardCapCrews(t, false) }

// Hold returned final answers until every released stream has decoded its final
// usage. Otherwise one settlement can cancel a peer before its final chunk is read.
type settledStreamBarrier struct {
	Completer
	arrived *atomic.Int32
	ready   chan struct{}
}

func (b *settledStreamBarrier) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	response, err := b.Completer.CompleteWithMessages(ctx, messages, opts...)
	if err == nil && response != nil && response.Usage != nil && response.Usage.Cost != nil && *response.Usage.Cost > 0 {
		if b.arrived.Add(1) == 4 {
			close(b.ready)
		}
		select {
		case <-b.ready:
		case <-ctx.Done():
		}
	}
	return response, err
}

// Interim usage still closes no gate until settlement. Once the cap crosses,
// all four calls here are already replying and must keep their final usage.
func TestInterimUsageStreamOvershootIsTheKnownLimit(t *testing.T) {
	hardCapScope(t, false, false, true)
}

func validateToolPairs(t *testing.T, messages []ai.Message) {
	t.Helper()
	pending := map[string]bool{}
	for _, message := range messages {
		if message.Role == "tool" {
			if !pending[message.ToolCallID] {
				t.Errorf("orphan or duplicate tool result %q", message.ToolCallID)
			}
			delete(pending, message.ToolCallID)
		} else {
			if len(pending) != 0 {
				t.Errorf("unpaired calls before %s: %v", message.Role, pending)
			}
			for _, call := range message.ToolCalls {
				pending[call.ID] = true
			}
		}
	}
	if len(pending) != 0 {
		t.Errorf("provider received unpaired tool calls: %v", pending)
	}
}

type streamJobBudget struct{ namedBudgetStub }

func (b *streamJobBudget) Refusal() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.spent >= b.cap {
		return "job budget reached · raise the job budget to continue"
	}
	return ""
}
func TestJobHardCapStopsEveryRunningCall(t *testing.T) { hardCapScope(t, true, true, false) }
