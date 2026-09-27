package session

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
)

var retryUsageModelSequence atomic.Int32

// Every paid answer survives the adapter's hidden retry, including a failed retry.
func TestCeilingRetryBanksEveryPaidAttempt(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		failed, detached, billed bool
	}{
		{name: "inline"}, {name: "inline-error", failed: true}, {name: "detached", detached: true}, {name: "detached-error", detached: true, failed: true}, {name: "full-billing", billed: true}, {name: "full-billing-error", billed: true, failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failed := tc.failed
			model := fmt.Sprintf("retry-test/%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), retryUsageModelSequence.Add(1))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
					w.WriteHeader(404)
					return
				}
				n := requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if n == 1 {
					fmt.Fprintf(w, `{"model":%q,"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":718,"completion_tokens":320,"total_tokens":1038,"prompt_tokens_details":{"cached_tokens":100},"cache_creation_input_tokens":20,"completion_tokens_details":{"reasoning_tokens":320},"cost":0.0005994}}`, model)
					return
				}
				if failed {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"test unauthorized"}}`)
					return
				}
				fmt.Fprintf(w, `{"model":%q,"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"final answer"}}],"usage":{"prompt_tokens":718,"completion_tokens":789,"total_tokens":1507,"prompt_tokens_details":{"cached_tokens":640},"cost":0.00097404}}`, model)
			}))
			defer server.Close()
			ledger := filepath.Join(t.TempDir(), "usage.jsonl")
			a, err := New(Config{usageLedger: ledger, Workspace: t.TempDir(), Model: model, APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			a.turnSpend = Usage{CostUSD: 0.5, Calls: 1}
			a.config.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }
			a.crewDayOnce.Do(func() { a.crewDayHeld = NewSpendDay(0) })
			helperCrew := &taskCrew{guard: &SpendGuard{TaskCap: 1, Task: &SpendTask{}}}
			ctx := provider.WithConfiguredReasoningEffort(context.Background(), provider.EffortOff)
			guard := &SpendGuard{Day: NewSpendDay(0), Price: kimiPrice, TaskCap: 1, SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 1}}
			wrapped := SeatCompleter(crewroute.Checker, guard.Wrap(model, a.routedCompleter()))
			ctx = withCrewTask(withPurpose(ctx, callPurpose("worker")), helperCrew)
			if tc.detached {
				ctx = withDetachedUsage(ctx)
			}
			var fullCost float64
			var fullCalls int
			if tc.billed {
				ctx = provider.WithBilling(ctx, func(b provider.Billed) { fullCost += b.Cost; fullCalls++ })
			}
			response, err := wrapped.CompleteWithMessages(ctx, []ai.Message{textMessage("user", "summarize")}, ai.WithModel(model), ai.WithMaxTokens(320))
			called := model
			if failed {
				if err == nil {
					t.Fatal("retry should retain its error")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if response.Text() != "final answer" || response.Usage.TotalTokens != 1507 {
					t.Fatalf("final response changed: %+v", response)
				}
				if !tc.billed {
					a.addUsageAs(response, called, 1, "worker", true, false, tc.detached)
				}
			}
			if requests.Load() != 2 {
				t.Fatalf("requests=%d want2", requests.Load())
			}
			want := 0.0005994
			calls := 1
			in, out, cache := 718, 320, 100
			if !failed {
				want += 0.00097404
				calls++
				in += 718
				out += 789
				cache += 640
			}
			for name, spent := range map[string]float64{"day": guard.Day.Total(), "task": guard.tally().Total(), "seat": guard.seatTally(crewroute.Checker).Total(), "helper-day": a.crewDay().Total(), "helper-task": helperCrew.guard.tally().Total(), "helperUSD": helperCrew.helperUSD} {
				if math.Abs(spent-want) > 1e-12 {
					t.Errorf("%s spend %.8f want %.8f", name, spent, want)
				}
			}
			if guard.Day.held != 0 || guard.tally().held != 0 || guard.seatTally(crewroute.Checker).held != 0 {
				t.Fatal("reservation not released exactly once")
			}
			wantTurn := 0.5
			if !tc.detached && !tc.billed {
				wantTurn += want
			}
			if math.Abs(a.turnSpend.CostUSD-wantTurn) > 1e-12 {
				t.Fatalf("turn cost%.8f want%.8f detached%v", a.turnSpend.CostUSD, wantTurn, tc.detached)
			}
			if tc.billed {
				if math.Abs(fullCost-want) > 1e-12 || fullCalls != calls || a.Usage().CostUSD != 0 || a.Usage().Calls != 0 {
					t.Fatalf("billing owner cost%.8f calls%d session%+v", fullCost, fullCalls, a.Usage())
				}
				return
			}
			used := a.Usage()
			if math.Abs(used.CostUSD-want) > 1e-12 || used.Calls != calls || used.Input != in || used.Output != out || used.CacheRead != cache || used.CacheWrite != 20 {
				t.Fatalf("usage=%+v want cost %.8f calls%d input%d output%d cache%d write20", used, want, calls, in, out, cache)
			}
			if !FlushUsage() {
				t.Fatal("ledger did not flush")
			}
			data, err := os.ReadFile(ledger)
			if err != nil {
				t.Fatal(err)
			}
			var total float64
			var ledgerCalls int
			for _, line := range strings.Split(string(data), "\n") {
				var row UsageLine
				if json.Unmarshal([]byte(line), &row) == nil {
					total += row.USD
					ledgerCalls += row.Calls
					if row.Role != "worker" {
						t.Fatalf("lost role: %+v", row)
					}
				}
			}
			if math.Abs(total-want) > 1e-12 || ledgerCalls != calls {
				t.Fatalf("ledger $%.8f/%d calls want $%.8f/%d", total, ledgerCalls, want, calls)
			}
		})
	}
}
