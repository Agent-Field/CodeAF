package provider

import (
 "context"
 "encoding/json"
 "fmt"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The public completion and reconciliation doors must retain paid partial work.
func TestBilledCancellation(t *testing.T) {
 for _, usage := range []bool{true, false} {
  t.Run(fmt.Sprint(usage), func(t *testing.T) {
   read := loggingTo(t)
   ctx, cancel := context.WithCancel(context.Background())
   defer cancel()
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == "GET" {
     fmt.Fprint(w, `{"data":{"total_cost":0.07,"tokens_prompt":11,"tokens_completion":3}}`)
     return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    u := ""
    if usage { u = `,"usage":{"prompt_tokens":11,"completion_tokens":3,"cost":0.07}` }
    fmt.Fprintf(w, "data: {\"id\":\"cancel-generation\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]%s}\n\n", u)
    w.(http.Flusher).Flush()
    <-r.Context().Done()
   }))
   defer server.Close()
   client := receiptTestClient(t, server)
   results := make(chan Reconciled, 3)
   ctx = WithCallTag(WithReconcile(ctx, func(r Reconciled) { results <- r }), "caption")
   ctx = WithStreamObserver(ctx, func(e StreamEvent) { if e.Kind == StreamDelta { cancel() } })
   _, err := client.CompleteWithMessages(ctx, []ai.Message{{Role:"user", Content:[]ai.ContentPart{{Type:"text", Text:"hello"}}}})
   if err == nil { t.Fatal("cancelled completion succeeded") }
   select {
   case r := <-results:
    if !r.Found || r.Cost != 0.07 || r.Ref != "cancel-generation" { t.Errorf("settlement=%+v", r) }
   case <-time.After(time.Second): t.Error("paid cancellation has no settlement")
   }
   for _, row := range ended(read()) {
    data, _ := json.Marshal(row)
    var fields map[string]any
    json.Unmarshal(data, &fields)
    if fields["generation_id"] != "cancel-generation" { t.Errorf("helper generation id missing: %s", data) }
   }
   select { case extra := <-results: t.Errorf("double booking: %+v", extra); default: }
  })
 }
}
