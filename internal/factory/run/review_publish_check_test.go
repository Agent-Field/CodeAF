package run

import (
 "context"
 "net/http"
 "net/http/httptest"
 "sync/atomic"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/client"
)

func TestReviewPublishCheckNoStop(t *testing.T) {
 for _, scenario := range []string{"healthy", "old_factory_404", "transient_503"} {
  t.Run(scenario, func(t *testing.T) {
   var checks atomic.Int32
   srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    n := checks.Add(1)
    if scenario == "old_factory_404" { http.NotFound(w, r); return }
    if scenario == "transient_503" && n == 1 { http.Error(w, "temporary outage", http.StatusServiceUnavailable); return }
    w.WriteHeader(http.StatusOK)
   }))
   defer srv.Close()
   g := newPublishRig(t, factory.Item{Num:12, Kind:factory.KindIssue, Repo:"acme/api", Title:"Publish without Stop"})
   worker := client.Open(srv.URL, "").Worker("credential")
   g.r.SetPublishCheck(g.id, func(ctx context.Context) error { return worker.CheckPublish(ctx, "valid-lease") })
   if err := g.r.Publish(g.id, testTrailer); err != nil { t.Fatal(err) }
   it := g.settle(t)
   if it.State != factory.StateShipped || it.PullRequest == nil {
    t.Fatalf("no Stop: publish refused; state=%s status=%+v checks=%d requests=%d error=%q", it.State, it.PublishStatus, checks.Load(), len(g.src.got), it.PublishError)
   }
  })
 }
}
