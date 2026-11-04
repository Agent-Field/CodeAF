package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The same four crews, cap and provider script measure both revisions. Each
// crew completes a tool pair before its held call. The provider has charged
// two held calls two cents already; the last has supplied no usage at all.
func hardCapCrews(t *testing.T, requireStop bool) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	const capUSD = .24
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseCalls := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCalls()
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, capUSD); err != nil {
		t.Fatal(err)
	}
	cross := make(chan struct{})
	agents := make([]*Agent, 4)
	events := make([]<-chan Event, 4)
	cancelled := make(chan int, 4)
	var requests atomic.Int32
	for i := range agents {
		ready := make(chan struct{})
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/chat/completions" {
				http.NotFound(w, r)
				return
			}
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
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}]}\n\n")
			w.(http.Flusher).Flush()
			close(ready)
			if i == 0 {
				<-cross
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
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":4,\"cost\":%.2f}}\n\ndata: [DONE]\n\n", cost)
		}))
		// Close the held requests before the server waits for its handlers.
		t.Cleanup(server.Close)
		client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		a, _ := newTestAgent(t, client, func(c *Config) { c.OneModel = true; c.ProfileDir = profile })
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
		observed := false
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
waiting:
	for stopped < 3 {
		select {
		case <-cancelled:
			stopped++
		case <-deadline.C:
			break waiting
		}
	}
	releaseCalls()
	notices := make([]string, 4)
	dailyStops := make([]bool, 4)
	for i := range agents {
		for _, event := range collect(t, events[i]) {
			notices[i] += event.Text
			dailyStops[i] = dailyStops[i] || errors.Is(event.Err, ErrSpendRail) && IsDailyLimit(event.Err)
		}
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
	t.Logf("crews=4 cap=$%.2f recorded=$%.2f past-cap=$%.2f cancelled=%d/3 unknown=%d", capUSD, spent, spent-capUSD, stopped, unknown)
	if !requireStop {
		return
	}
	if stopped != 3 {
		t.Fatalf("cap crossing cancelled %d/3 running crew calls", stopped)
	}
	if spent < .2899 || spent > .2901 || unknown != 1 {
		t.Fatalf("partial charges or unknown call lost: spent=%v unknown=%d", spent, unknown)
	}
	for i := 1; i < 4; i++ {
		if !dailyStops[i] {
			t.Errorf("crew %d did not report a typed daily stop", i)
		}
		if !strings.Contains(notices[i], "daily limit") || !strings.Contains(notices[i], "/budget day") {
			t.Errorf("crew %d did not explain stop and raise: %q", i, notices[i])
		}
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
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 4; i++ {
		// Public submission reopens only after the old work has finished stopping.
		waitFor(t, "old owned work joins", func() bool { agents[i].mu.Lock(); defer agents[i].mu.Unlock(); return !agents[i].workStopping })
		collect(t, mustSubmit(t, agents[i], "resume"))
	}
	if requests.Load() != 11 {
		t.Fatalf("raise failed to resume chats: requests=%d", requests.Load())
	}
}

func TestHardCapCancelsEveryRunningCrew(t *testing.T) { hardCapCrews(t, true) }
func TestHardCapOvershootMeasurement(t *testing.T)    { hardCapCrews(t, false) }
