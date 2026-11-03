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
 for _, tc := range []struct {name string; usage, noID, noReceipt, direct bool}{
 {name:"usage", usage:true}, {name:"before-usage"},
 {name:"interim-fallback", usage:true, noReceipt:true},
 {name:"unpriced", noReceipt:true},
 {name:"no-id", usage:true, noID:true},
 {name:"direct", usage:true, direct:true},
 } {
  t.Run(tc.name, func(t *testing.T) {
   read := loggingTo(t)
   ctx, cancel := context.WithCancel(context.Background())
   defer cancel()
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == "GET" {
     if tc.noReceipt { w.WriteHeader(404); return }
     fmt.Fprint(w, `{"data":{"total_cost":0.07,"tokens_prompt":11,"tokens_completion":3}}`)
     return
    }
    w.Header().Set("Content-Type", "text/event-stream")
    u := ""
    if tc.usage { u = `,"usage":{"prompt_tokens":11,"completion_tokens":3,"cost":0.07}` }
    id := "cancel-generation"
    if tc.noID { id = "" }
    fmt.Fprintf(w, "data: {\"id\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]%s}\n\n", id, u)
    w.(http.Flusher).Flush()
    <-r.Context().Done()
   }))
   defer server.Close()
   client := receiptTestClient(t, server)
   client.config.Direct = tc.direct
   results := make(chan Reconciled, 3)
   ctx = WithCallTag(WithReconcile(ctx, func(r Reconciled) { results <- r }), "caption")
   ctx = WithStreamObserver(ctx, func(e StreamEvent) { if e.Kind == StreamDelta { cancel() } })
   _, err := client.CompleteWithMessages(ctx, []ai.Message{{Role:"user", Content:[]ai.ContentPart{{Type:"text", Text:"hello"}}}})
   if err == nil { t.Fatal("cancelled completion succeeded") }
   select {
   case r := <-results:
    wantFound := tc.usage || !tc.noReceipt
    wantCost := 0.07
    if !wantFound { wantCost = 0 }
    if r.Found != wantFound || r.Cost != wantCost { t.Errorf("settlement=%+v", r) }
    data, _ := json.Marshal(r)
    var settlement map[string]any
    json.Unmarshal(data, &settlement)
    if tc.usage && (tc.noReceipt || tc.noID || tc.direct) && settlement["Interim"] != true { t.Errorf("interim usage not identified: %+v", r) }
   case <-time.After(time.Second): t.Error("paid cancellation has no settlement")
   }
   for _, row := range ended(read()) {
    data, _ := json.Marshal(row)
    var fields map[string]any
    json.Unmarshal(data, &fields)
    if !tc.noID && fields["generation_id"] != "cancel-generation" { t.Errorf("helper generation id missing: %s", data) }
   }
   select { case extra := <-results: t.Errorf("double booking: %+v", extra); default: }
  })
 }
}
