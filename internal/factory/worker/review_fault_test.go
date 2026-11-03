package worker

import (
 "encoding/json"
 "fmt"
 "net/http"
 "os"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestReviewFailedModelIsAcknowledged(t *testing.T) {
 var ids []string
 j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  _ = json.Unmarshal([]byte(r.Header.Get(api.SignalAcksHeader)), &ids)
  _ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
 }))
 path := j.o.Mirror.ItemPath(j.id)
 if err := os.Rename(path, path+".hidden"); err != nil { t.Fatal(err) }
 signals := []api.WorkerSignal{{Verb:"model", Words:"chosen", Delivery:"lease/model"}}
 j.signals(t.Context(), signals)
 if err := os.Rename(path+".hidden", path); err != nil { t.Fatal(err) }
 j.signals(t.Context(), signals)
 j.beat(t.Context())
 it, err := j.o.Mirror.Get(j.id)
 if err != nil { t.Fatal(err) }
 if it.Model != "chosen" {
  t.Fatalf("failed effect never retried: model=%q, next-heartbeat acknowledgements=%v", it.Model, ids)
 }
}

func TestReviewHistoricalAcknowledgementsBreakHeartbeat(t *testing.T) {
 var calls atomic.Int32
 j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  calls.Add(1)
  _ = json.NewEncoder(w).Encode(api.WorkerHeartbeat{})
 }))
 for n:=0; n<25000; n++ {
  j.ackSignal(fmt.Sprintf("lease_%032x/%026d", 1, n))
 }
 // All of these deliveries were already handled; the factory may have removed them.
 j.beat(t.Context())
 ids := make([]string,0,len(j.signalIDs))
 for id := range j.signalIDs { ids=append(ids,id) }
 _, err := j.w.Heartbeat(t.Context(), j.lease, ids)
 raw, _ := json.Marshal(ids)
 req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, j.w.Base()+"/heartbeat", strings.NewReader("{}"))
 req.Header.Set(api.SignalAcksHeader, string(raw))
 resp, wireErr := http.DefaultClient.Do(req)
 if wireErr != nil { t.Fatal(wireErr) }
 defer resp.Body.Close()
 if calls.Load()==0 {
  t.Fatalf("heartbeat cannot reach renewal handler: handled IDs=%d; client error=%v; raw HTTP status=%d",len(ids),err,resp.StatusCode)
 }
}
