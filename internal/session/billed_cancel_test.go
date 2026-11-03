package session

import (
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "sync/atomic"
 "testing"
 "time"

 "github.com/Agent-Field/agentfield/sdk/go/ai"
 "github.com/Agent-Field/codeaf/internal/provider"
)

func TestCancelledHelperRetryBooksEachAttemptOnce(t *testing.T) {
 var attempts atomic.Int32
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Content-Type", "text/event-stream")
  n := attempts.Add(1)
  finish := ""
  if n > 1 { finish = `,"finish_reason":"stop"` }
  fmt.Fprintf(w, "data: {\"id\":\"helper-%d\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}%s}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":3,\"cost\":0.07}}\n\n", n, finish)
  w.(http.Flusher).Flush()
  if n == 1 { <-r.Context().Done() } else { fmt.Fprint(w, "data: [DONE]\n\n") }
 }))
 defer server.Close()
 client, err := provider.NewClient(provider.Config{APIKey:"fixture", BaseURL:server.URL, Model:"cancel/model", Direct:true})
 if err != nil { t.Fatal(err) }
 ledger := filepath.Join(t.TempDir(), UsageLedgerName)
 agent, _ := newTestAgent(t, client, func(c *Config) { c.usageLedger = ledger })
 ctx, cancel := context.WithCancel(context.Background())
 defer cancel()
 ctx = provider.WithReconcile(ctx, agent.reconciled)
 ctx = provider.WithCallTag(ctx, "caption")
 ctx = provider.WithStreamObserver(ctx, func(e provider.StreamEvent) { if e.Kind == provider.StreamDelta { cancel() } })
 messages := []ai.Message{textMessage("user", "name this")}
 if _, err := client.CompleteWithMessages(ctx, messages); err == nil { t.Fatal("cancel succeeded") }
 response, err := client.CompleteWithMessages(context.Background(), messages)
 if err != nil { t.Fatal(err) }
 agent.addAuxiliaryUsageAs(response, "cancel/model", 1, "caption")
 FlushUsage()
 rows, err := ReadUsage(ledger, time.Time{})
 if err != nil { t.Fatal(err) }
 if len(rows) != 2 || agent.Usage().Calls != 2 || agent.Usage().CostUSD != 0.14 { t.Fatalf("retry bookings: rows=%+v usage=%+v", rows, agent.Usage()) }
 for i, row := range rows {
  if row.USD != 0.07 || row.Calls != 1 || row.Role != "caption" { t.Errorf("row %d=%+v", i, row) }
 }
}
