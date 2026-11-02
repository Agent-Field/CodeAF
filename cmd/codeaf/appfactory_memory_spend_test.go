package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The provider's own counter is independent of the knowledge receipts and the
// Spending ledger. Both compilation and reading use their exported interfaces.
func TestMemorySpendAndLifecycle(t *testing.T) {
	s := newLocalSetup(t)
	_ = s
	var calls atomic.Int64
	model := knowledge.Model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		result := `{"subjects":[]}`
		var payload map[string]json.RawMessage
		if len(body.Messages) > 1 {
			_ = json.Unmarshal([]byte(body.Messages[1].Content), &payload)
		}
		if payload["messages"] != nil {
			result = `{"claims":[{"local_id":"c","text":"Cedar queues jobs.","topic":"Queues","scope":"Cedar","kind":"fact","evidence":[{"source":"source","quote":"Cedar queues jobs."}]}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": result}}}, "usage": map[string]any{"prompt_tokens": 4400, "completion_tokens": 100, "cost": 0.01}})
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = roundTripper(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		copy.URL.Scheme = "http"
		copy.URL.Host = server.Listener.Addr().String()
		return original.RoundTrip(copy)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	vault := knowledge.Open(filepath.Join(s.home, "profile"))
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(knowledge.Source{ID: "source", Session: "job-chat", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := vault.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	hits, err := vault.Search("queues", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("memory output lost: %v %v", hits, err)
	}
	before := calls.Load()
	if err := vault.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Errorf("unchanged input: calls %d -> %d", before, calls.Load())
	}
	if !session.FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledgerCalls int
	var usd float64
	for _, line := range lines {
		if line.Role == "memory" {
			ledgerCalls += line.Calls
			usd += line.USD
		}
	}
	t.Logf("provider: calls=%d USD=%.4f; memory ledger: calls=%d USD=%.4f", calls.Load(), float64(calls.Load())*.01, ledgerCalls, usd)
	if int64(ledgerCalls) != calls.Load() || fmt.Sprintf("%.4f", usd) != fmt.Sprintf("%.4f", float64(calls.Load())*.01) {
		t.Errorf("memory spend missing: provider calls=%d USD=%.4f; ledger calls=%d USD=%.4f", calls.Load(), float64(calls.Load())*.01, ledgerCalls, usd)
	}
}
