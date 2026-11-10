package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

type moneyTransport func(*http.Request) (*http.Response, error)

func (f moneyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func moneyVault(t *testing.T) (*knowledge.Vault, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 100); err != nil {
		t.Fatal(err)
	}
	v := knowledge.Open(profile)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := v.Capture(knowledge.Source{ID: "source", Session: "chat", Role: "user", TS: "2026-11-10T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	return v, profile
}

func moneyReply(model, finish, content string, output int) *http.Response {
	raw, _ := json.Marshal(map[string]any{"id": "money-fixture", "model": model, "choices": []any{map[string]any{"finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": output, "total_tokens": 10 + output, "cost": 0.0123}})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}
}

func TestKnowledgePaidAnswersReachLedgerAndReceipts(t *testing.T) {
	for _, shape := range []string{"valid", "invalid", "empty ceiling", "length capped", "substituted"} {
		t.Run(shape, func(t *testing.T) {
			v, profile := moneyVault(t)
			model := "fixture/money-" + strings.ReplaceAll(shape, " ", "-")
			if err := os.WriteFile(filepath.Join(profile, "config.json"), []byte(fmt.Sprintf(`{"knowledge.model":%q,"daily_budget_usd":100}`, model)), 0600); err != nil {
				t.Fatal(err)
			}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			calls := 0
			var bodies [][]byte
			http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					return nil, readErr
				}
				bodies = append(bodies, body)
				switch shape {
				case "substituted":
					return moneyReply("fixture/actually-served", "stop", "not JSON", 3), nil
				case "invalid":
					return moneyReply(model, "stop", "not JSON", 3), nil
				case "length capped":
					return moneyReply(model, "length", "{", 8000), nil
				case "empty ceiling":
					return moneyReply(model, "length", "", 8000), nil
				default:
					return moneyReply(model, "stop", `{"claims":[],"notes":[],"relations":[],"aliases":[],"subjects":[]}`, 3), nil
				}
			})
			ctx := knowledgeCalls(context.Background(), session.Config{ProfileDir: profile})
			err := v.Flush(ctx, "fixture", true)
			if shape == "valid" && err != nil {
				t.Fatal(err)
			}
			if shape != "valid" && err == nil {
				t.Fatal("unusable result accepted")
			}
			session.FlushUsage()
			lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if calls == 0 || len(lines) != calls {
				t.Fatalf("paid calls=%d; ledger rows=%d, want one per call", calls, len(lines))
			}
			expectedModel := model
			if shape == "substituted" {
				expectedModel = "fixture/actually-served"
			}
			if shape == "empty ceiling" && len(bodies) > 1 && bytes.Equal(bodies[0], bodies[1]) {
				t.Fatal("empty-at-ceiling request was immediately resent identically")
			}
			for _, line := range lines {
				if line.Role != "knowledge" || line.Model != expectedModel || line.USD != 0.0123 || line.Calls != 1 {
					t.Fatalf("wrong ledger row: %+v", line)
				}
			}
			state, err := v.Status()
			if err != nil || math.Abs(state.CostUSD-float64(calls)*0.0123) > 1e-9 {
				t.Fatalf("receipts lost spend: calls=%d, cost=%g, err=%v", calls, state.CostUSD, err)
			}
			before := calls
			_ = v.Flush(ctx, "fixture", true)
			if calls != before {
				t.Fatalf("identical request immediately repeated: before=%d after=%d", before, calls)
			}
		})
	}
}

func TestKnowledgeDailyLimitPausesAndResumesAndPreauthorizationLiftsIt(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(fmt.Sprint(authorized), func(t *testing.T) {
			v, profile := moneyVault(t)
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1, Calls: 1, Model: "fixture/earlier"})
			session.FlushUsage()
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			var calls atomic.Int32
			http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return moneyReply(knowledge.Model, "stop", "not JSON", 3), nil
			})
			ctx, cancel := context.WithTimeout(knowledgeCalls(context.Background(), session.Config{ProfileDir: profile, SpendPreauthorized: authorized}), 2*time.Minute)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- v.Run(ctx, moneyHistory{}, func() string { return "fixture" }) }()
			defer func() { cancel(); <-done }()
			if !authorized {
				for {
					if calls.Load() != 0 {
						t.Fatalf("blocked compiler made %d provider requests", calls.Load())
					}
					state, err := v.Status()
					if err == nil && strings.Contains(state.Build.Error, "daily limit reached · $1 spent of $1 · /budget day changes it") {
						break
					}
					if ctx.Err() != nil {
						t.Fatal("job line never named the daily limit")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if calls.Load() != 0 {
					t.Fatalf("blocked compiler made %d provider requests", calls.Load())
				}
				if err := config.WriteDailyBudgetUSD(profile, 2); err != nil {
					t.Fatal(err)
				}
			}
			for calls.Load() == 0 {
				if ctx.Err() != nil {
					t.Fatal("compiler did not resume automatically")
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

type moneyHistory struct{}

func (moneyHistory) Inventory(context.Context) ([]knowledge.Journal, error) { return nil, nil }
func (moneyHistory) Read(context.Context, knowledge.Journal, knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	return knowledge.JournalBatch{}, nil
}

func TestKnowledgeChecksDailyLimitBetweenCompilerCalls(t *testing.T) {
	v, profile := moneyVault(t)
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1, Calls: 1, Model: "fixture/concurrent-chat"})
			session.FlushUsage()
			return moneyReply(knowledge.Model, "stop", `{"claims":[{"local_id":"c","text":"Cedar queues jobs.","topic":"Queues","scope":"Cedar","kind":"fact","evidence":[{"source":"source","quote":"Cedar queues jobs."}]}],"notes":[],"relations":[],"aliases":[]}`, 3), nil
		}
		return moneyReply(knowledge.Model, "stop", "not JSON", 3), nil
	})
	err := v.Flush(knowledgeCalls(context.Background(), session.Config{ProfileDir: profile}), "fixture", true)
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "daily limit reached") {
		t.Fatalf("calls=%d, err=%v; want the next compiler call refused", calls, err)
	}
}

func TestKnowledgeDailyLimitBlocksTransportRetry(t *testing.T) {
	v, profile := moneyVault(t)
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	calls := 0
	http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1, Calls: 1, Model: "fixture/concurrent-chat"})
			session.FlushUsage()
			return &http.Response{StatusCode: 500, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary provider failure"}}`))}, nil
		}
		return moneyReply(knowledge.Model, "stop", "not JSON", 3), nil
	})
	ctx, cancel := context.WithTimeout(knowledgeCalls(context.Background(), session.Config{ProfileDir: profile}), 2*time.Minute)
	defer cancel()
	err := v.Flush(ctx, "fixture", true)
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "daily limit reached") {
		t.Fatalf("calls=%d, err=%v; want transport retry refused", calls, err)
	}
}
