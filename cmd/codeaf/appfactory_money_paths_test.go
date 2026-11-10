package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/control"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Both arms reach their final price before either can return. This exercises
// completed losers, rather than depending on a cancelled arm's later receipt.
type moneyBarrier struct {
	priced atomic.Int64
	ready  chan struct{}
}

func (b *moneyBarrier) BeforeAttempt(context.Context, provider.ActualAttemptInfo) (provider.ActualAttemptToken, error) {
	return b, nil
}
func (*moneyBarrier) EndTransport(provider.TransportEnd) {}
func (b *moneyBarrier) ObserveFinancial(f provider.FinancialObservation) {
	if f.State != provider.FinancialKnown {
		return
	}
	if b.priced.Add(1) == 2 {
		close(b.ready)
	}
	select {
	case <-b.ready:
	case <-time.After(5 * time.Second):
	}
}

type moneyDoor struct {
	*session.Agent
	barrier *moneyBarrier
}

func (d *moneyDoor) RunErrand(ctx context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	if d.barrier != nil {
		var err error
		ctx, err = provider.WithActualAttemptObserver(ctx, d.barrier)
		if err != nil {
			return session.ErrandAnswer{}, err
		}
	}
	return d.Agent.RunErrand(ctx, ask)
}

// Only waiting policy is scripted: the real transport must start a rescue.
type moneyWait struct {
	control.Controller
	deadline time.Time
	fired    bool
}

func (w *moneyWait) Quiet(time.Time) control.Act {
	if w.fired {
		return control.Act{}
	}
	w.fired = true
	return control.Act{Kind: control.Report, Reason: control.CeilingReason}
}
func (w *moneyWait) Note(control.Reading) control.Act { return control.Act{} }
func (w *moneyWait) Deadline() time.Time {
	if w.fired {
		return time.Time{}
	}
	return w.deadline
}

func TestAppFactoryCompletedHedgeLoserMoney(t *testing.T) {
	s := newLocalSetup(t)
	var calls atomic.Int64
	server := moneyProvider(t, &calls)
	lanes.HeardPrefsCarried(server.URL)
	old := lanes.SetController(func(plan control.Plan) control.Controller {
		return &moneyWait{Controller: control.New(plan), deadline: time.Now().Add(20 * time.Millisecond)}
	})
	t.Cleanup(func() { lanes.SetController(old) })
	st := s.start()
	floor := s.backend.local.host.floor
	barrier := &moneyBarrier{ready: make(chan struct{})}
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		agent := moneyAgent(t, s, ask, server.URL)
		return &appConn{Conv: &moneyDoor{Agent: agent, barrier: barrier}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
	}
	it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 7, Title: "Hedge accounting", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1})
	if err != nil {
		t.Fatal(err)
	}
	makeJobManager(t, s, it.ID)
	var chats appwire.FactoryJobChats
	s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
	var answer appwire.Errand
	s.must("agents.errand", map[string]any{"agent": chats.Agent, "purpose": "hedge_money", "role": "brief", "system": "Answer briefly.", "messages": []appwire.ErrandMessage{{Role: "user", Text: "Check accounting."}}, "max_tokens": 100, "deadline_ms": 10000}, &answer)
	if answer.Reason != "" {
		t.Fatalf("errand: %+v", answer)
	}
	if calls.Load() != 2 || barrier.priced.Load() != 2 {
		t.Fatalf("completed arms=%d priced=%d, want two", calls.Load(), barrier.priced.Load())
	}
	moneyTotals(t, s, st.Factory, it, &calls, .02)
}

func moneyProvider(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		calls.Add(1)
		var request struct {
			Stream bool `json:"stream"`
			Tools  []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		message := map[string]any{"content": briefDraft}
		choice := map[string]any{"finish_reason": "stop"}
		if request.Stream {
			choice["delta"] = message
		} else {
			choice["message"] = message
		}
		for _, tool := range request.Tools {
			if tool.Function.Name == "stage_result" {
				message = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "repair-report", "type": "function", "function": map[string]any{"name": "stage_result", "arguments": `{"done":false,"output":"Repair needs a decision.","repair":{"decision":"needs_you","reason":"Choose the retry policy."}}`}}}}
				choice["finish_reason"] = "tool_calls"
				if request.Stream {
					choice["delta"] = message
				} else {
					choice["message"] = message
				}
				break
			}
		}
		raw, _ := json.Marshal(map[string]any{"model": "deepseek/deepseek-v4.1-flash", "choices": []any{choice}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
		} else {
			w.Write(raw)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func moneyAgent(t *testing.T, s *localSetup, ask appOpenAsk, base string) *session.Agent {
	t.Helper()
	agent, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: filepath.Join(s.home, "money-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: base, Routing: provider.RoutingLatency, JobChat: jobChatDoorOf(s.backend.local.host.floor), Unattended: true, DeterministicNames: true})
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func moneyTotals(t *testing.T, s *localSetup, factoryID string, it factory.Item, calls *atomic.Int64, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !session.FlushUsage() {
			t.Fatal("ledger did not flush")
		}
		lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		var ledger float64
		var count int
		for _, line := range lines {
			if line.FactoryJob == factory.LedgerName(it) {
				ledger += line.USD
				count += line.Calls
			}
		}
		var header api.Job
		s.must("factory.job", map[string]any{"factory": factoryID, "job": it.ID}, &header)
		providerUSD := float64(calls.Load()) * .01
		if math.Abs(providerUSD-want) < 1e-9 && math.Abs(ledger-want) < 1e-9 && math.Abs(header.Cost-want) < 1e-9 && int64(count) == calls.Load() {
			t.Logf("provider=%.4f ledger=%.4f header=%.4f calls=%d", providerUSD, ledger, header.Cost, count)
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider=%.4f ledger=%.4f header=%.4f calls=%d; want %.4f once per call", providerUSD, ledger, header.Cost, count, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
