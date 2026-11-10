package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

	"github.com/Agent-Field/codeaf/internal/calllog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

type moneyTransport func(*http.Request) (*http.Response, error)

func (f moneyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func moneyVault(t *testing.T) (*knowledge.Vault, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", testHome(t))
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	ledger := session.UsageLedgerPath()
	t.Cleanup(func() { session.StopUsageWriter(ledger) })
	profile := testHome(t)
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
			ctx := knowledgeCalls(context.Background(), profile, false)
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
			ctx, cancel := context.WithTimeout(knowledgeCalls(context.Background(), profile, authorized), 2*time.Minute)
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
	err := v.Flush(knowledgeCalls(context.Background(), profile, false), "fixture", true)
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
	ctx, cancel := context.WithTimeout(knowledgeCalls(context.Background(), profile, false), 2*time.Minute)
	defer cancel()
	err := v.Flush(ctx, "fixture", true)
	if calls != 1 || err == nil || !strings.Contains(err.Error(), "daily limit reached") {
		t.Fatalf("calls=%d, err=%v; want transport retry refused", calls, err)
	}
}

func TestKnowledgeDailyLimitBlocksEveryRetry(t *testing.T) {
	for _, retry := range []string{"reopen", "automatic backoff", "owner"} {
		t.Run(retry, func(t *testing.T) {
			v, profile := moneyVault(t)
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			var paid, owned atomic.Int32
			http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
				paid.Add(1)
				session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1, Calls: 1, Model: "fixture/concurrent-chat"})
				session.FlushUsage()
				return moneyReply(knowledge.Model, "length", "{", 8000), nil
			})
			ctx := knowledgeCalls(context.Background(), profile, false)
			if retry == "owner" {
				if err := v.Capture(knowledge.Source{ID: "owned", Session: "stage", Job: true, Role: "user", TS: "2026-11-10T12:00:00Z", Text: "Cedar queues jobs."}); err != nil {
					t.Fatal(err)
				}
				v = v.WithCalls(func(ctx context.Context) (context.Context, error) {
					ctx = provider.WithSession(ctx, "stage")
					return provider.WithBilling(ctx, func(provider.Billed) { owned.Add(1) }), nil
				})
			}
			if retry == "automatic backoff" {
				worker, cancel := context.WithTimeout(ctx, 7*time.Second)
				defer cancel()
				if err := v.Run(worker, moneyHistory{}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			} else {
				if err := v.Flush(ctx, "fixture", true); err == nil || !strings.Contains(err.Error(), "length") {
					t.Fatalf("initial paid failure: %v", err)
				}
				if retry == "owner" {
					time.Sleep(2200 * time.Millisecond)
					if err := v.Flush(ctx, "fixture", true); err == nil || !strings.Contains(err.Error(), "daily limit reached") {
						t.Fatalf("owner retry must pause for daily limit: %v", err)
					}
				} else {
					v = knowledge.Open(profile)
				}
				worker, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
				defer cancel()
				if err := v.Run(worker, moneyHistory{}, func() string { return "fixture" }); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			}
			// The day's figure is the whole bill: the concurrent chat's dollar plus the
			// ordinary compiler's unusable answer, which reaches the global ledger. An
			// owner's answer is billed to its owner, so the global day stays at the dollar.
			spent := "$1.01"
			if retry == "owner" {
				spent = "$1"
			}
			state, err := v.Status()
			if err != nil || paid.Load() != 1 || state.Build.Phase != knowledge.BuildFailed || !strings.Contains(state.Build.Error, "daily limit reached · "+spent+" spent of $1 · /budget day changes it") {
				t.Fatalf("%s bought a retry or lost the daily pause: paid=%d state=%+v err=%v", retry, paid.Load(), state.Build, err)
			}
			if retry == "owner" && owned.Load() != 1 {
				t.Fatalf("owned receipts=%d, want the initial paid failure only", owned.Load())
			}
			t.Logf("%s: one initial paid failure; zero paid retries; notice=%q", retry, state.Build.Error)
		})
	}
}

func TestKnowledgeMixedPaidAnswersMatchCallLogLedgerAndReceipts(t *testing.T) {
	v, profile := moneyVault(t)
	callPath := filepath.Join(os.Getenv("CODEAF_HOME"), "logs", "calls.jsonl")
	t.Setenv(calllog.EnvVar, callPath)
	calllog.Open("")
	t.Cleanup(func() {
		calllog.Close()
		os.Setenv(calllog.EnvVar, calllog.OffValue)
		calllog.Open("")
	})
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	paid := 0
	http.DefaultTransport = moneyTransport(func(r *http.Request) (*http.Response, error) {
		paid++
		switch paid {
		case 1:
			return moneyReply(knowledge.Model, "stop", "", 3), nil
		case 2:
			return moneyReply(knowledge.Model, "length", "{", 8000), nil
		case 3:
			return moneyReply("fixture/substituted", "stop", `{"claims":[]}`, 3), nil
		case 4:
			return moneyReply(knowledge.Model, "stop", "not JSON", 3), nil
		default:
			return moneyReply(knowledge.Model, "stop", `{"claims":[],"notes":[],"relations":[],"aliases":[],"subjects":[]}`, 3), nil
		}
	})
	ctx := knowledgeCalls(context.Background(), profile, false)
	for attempt := 1; attempt <= 5; attempt++ {
		// Reopening represents the engine's next ownership of the same profile.
		v = knowledge.Open(profile)
		err := v.Flush(ctx, "fixture", true)
		if attempt < 5 && err == nil {
			t.Fatalf("unusable paid answer %d accepted", attempt)
		}
		if attempt == 5 && err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Flush(ctx, "fixture", true); err != nil {
		t.Fatal(err)
	}
	if !session.FlushUsage() {
		t.Fatal("usage ledger did not flush")
	}
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	ledgerRows, ledgerCost := 0, 0.0
	for _, line := range lines {
		if line.Role == "knowledge" {
			ledgerRows++
			ledgerCost += line.USD
			if line.Calls != 1 {
				t.Fatalf("ledger bill counted more than once: %+v", line)
			}
		}
	}
	raw, err := os.ReadFile(callPath)
	if err != nil {
		t.Fatal(err)
	}
	logRows, logCost := 0, 0.0
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		var row calllog.Record
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		// A start row describes an in-flight request; its completed row is the answer.
		if row.Tag == "memory" && row.Phase != calllog.PhaseStart {
			logRows++
			logCost += row.Cost
		}
	}
	uri := sqliteuri.File(filepath.Join(v.Directory(), "knowledge.sqlite"))
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var receipts, failures, usable int
	var receiptCost float64
	if err := db.QueryRow(`SELECT count(*), coalesce(sum(cost),0), count(*) FILTER (WHERE json_extract(payload,'$.result') IS NULL), count(*) FILTER (WHERE json_extract(payload,'$.result') IS NOT NULL) FROM receipts`).Scan(&receipts, &receiptCost, &failures, &usable); err != nil {
		t.Fatal(err)
	}
	if paid != 5 || logRows != paid || ledgerRows != paid || receipts != paid || failures != 4 || usable != 1 {
		t.Fatalf("paid=%d memory answers=%d knowledge ledger=%d receipts=%d failures=%d usable=%d", paid, logRows, ledgerRows, receipts, failures, usable)
	}
	for _, cost := range []float64{logCost, ledgerCost, receiptCost} {
		if math.Abs(cost-float64(paid)*.0123) > 1e-9 {
			t.Fatalf("cost mismatch: call log=%g ledger=%g receipts=%g paid=%d", logCost, ledgerCost, receiptCost, paid)
		}
	}
	t.Logf("paid=%d memory answers=%d knowledge ledger=%d receipts=%d failed=%d usable=%d cost=%g", paid, logRows, ledgerRows, receipts, failures, usable, ledgerCost)
}
