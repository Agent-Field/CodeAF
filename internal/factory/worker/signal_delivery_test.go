package worker

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "reflect"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 "github.com/Agent-Field/codeaf/internal/factory/client"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
 "github.com/Agent-Field/codeaf/internal/factory/run"
 "github.com/Agent-Field/codeaf/internal/factory/serve"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

// A heartbeat can finish at the desk without its reply reaching the worker.
// The rig drives real HTTP beats explicitly, without a timer or a model.
func TestEverySignalSurvivesALostHeartbeatReply(t *testing.T) {
 for _, verb := range []string{"hold", "budget", "answer", "steer", "reply", "recheck", "publish", "merge", "stop", "model", "autonomy", "reconcile_results"} {
  t.Run(verb, func(t *testing.T) {
   st, err := store.Open(t.TempDir()); if err != nil { t.Fatal(err) }
   desk, err := lease.Open(st, lease.Options{}); if err != nil { t.Fatal(err) }
   seam := factory.LocalSeam(st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
   handler := serve.New(seam, serve.Config{Store: st, Address: "http://test"})
   defer handler.Close()
   var beats, recoveries atomic.Int32
   replies := make(chan api.WorkerHeartbeat, 8)
   server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.HasPrefix(r.URL.Path,"/v1/workers/recover/"){recoveries.Add(1);w.WriteHeader(422);return}
    if !strings.HasSuffix(r.URL.Path, "/heartbeat") { handler.ServeHTTP(w,r); return }
    n := beats.Add(1)
    // Lose an acknowledgement too, forcing the worker to see the same ID again.
    if n == 3 { r.Header.Del("X-Codeaf-Signal-Acks") ; r.Header.Set("X-Codeaf-Signal-Acks", "[]") }
    captured := httptest.NewRecorder(); handler.ServeHTTP(captured,r)
    var hb api.WorkerHeartbeat
    if err := json.Unmarshal(captured.Body.Bytes(), &hb); err != nil { t.Error(err) }
    replies <- hb
    if n == 1 { panic(http.ErrAbortHandler) }
    for key, values := range captured.Header() { w.Header()[key] = values }
    w.WriteHeader(captured.Code); _, _ = w.Write(captured.Body.Bytes())
   }))
   defer server.Close()
   wc := client.Open(server.URL, "").Worker("")
   attached, err := wc.Attach(t.Context(), api.WorkerAttach{Name:"test", Machine:"test", Handle:"me", TakesUnassigned:map[string]bool{lease.EveryRepo:true}}); if err != nil { t.Fatal(err) }
   it, err := st.Create(factory.Item{Repo:"acme/api", Kind:factory.KindIssue, State:factory.StateNew, Stages:[]factory.Stage{{Name:"check", Kind:factory.StageCheck, On:true, Ask:"true"}}}); if err != nil { t.Fatal(err) }
   if err := lease.NewDoors(desk).Launch(it.ID); err != nil { t.Fatal(err) }
   held, err := desk.Pull(lease.Worker{ID:attached.WorkerID,Handle:"me",TakesUnassigned:map[string]bool{lease.EveryRepo:true}}); if err != nil { t.Fatal(err) }
   mirror, err := store.Open(t.TempDir()); if err != nil { t.Fatal(err) }
   it, _ = st.Get(it.ID); it.State=factory.StateNew; if err := mirror.Save(it); err != nil { t.Fatal(err) }
   entered := make(chan struct{})
   finished := make(chan struct{})
   var writes atomic.Int32
   runner := run.New(run.Options{Publisher:func(context.Context,factory.Item)(run.PublishTarget,error){writes.Add(1);return run.PublishTarget{},errors.New("test forge unavailable")}, Store:mirror, RepoDir:func(string) string{return t.TempDir()}, Exec:map[factory.StageKind]run.Executor{factory.StageCheck:run.ExecutorFunc(func(ctx context.Context, _ run.Job)(factory.StageResult,error){close(entered); defer close(finished); <-ctx.Done(); return factory.StageResult{},ctx.Err()})}})
   if err := runner.Launch(it.ID); err != nil { t.Fatal(err) }
   select { case <-entered: case <-time.After(3*time.Second): t.Fatal("runner did not start") }
   defer func(){ _ = runner.Stop(it.ID); select {case <-finished: case <-time.After(3*time.Second): t.Error("runner did not stop") }; waitSignalRunner(t,runner,it.ID) }()
   var turns atomic.Int32
   replied := make(chan struct{}, 4)
   j := &job{o:Options{Runner:runner, Mirror:mirror, Reply:func(context.Context,factory.Item,string)error{ turns.Add(1); replied<-struct{}{}; return nil }},w:wc,lease:held.ID,id:it.ID,accepted:map[string]bool{},acknowledged:map[string]bool{}}
   signal := lease.Signal{Verb:verb,Words:"chosen",Budget:8,Yes:true}
   if verb=="publish"{signal.Words="Factory-Approval: evt_12345678 @me"}
   if verb=="reconcile_results" {
    _=runner.Stop(it.ID)
    waitSignalRunner(t,runner,it.ID)
    cur,_:=mirror.Get(it.ID)
    p,err:=j.prepareDelivery(cur,factory.RunReport{Cost:.25});if err!=nil{t.Fatal(err)}
    p.Reason="HTTP 422";j.pending=p;j.blocked="results not saved"
   }

   if verb == "answer" { if err := mirror.Update(it.ID,func(it *factory.Item)error{it.Asking=&factory.Asked{With:factory.AskedYou,Stage:"check",Question:"which?"};return nil}); err != nil {t.Fatal(err)} }
   if verb == "hold" { if err := lease.NewDoors(desk).Hold(it.ID,true); err != nil {t.Fatal(err)} } else if verb == "reply" {
    if err := st.Update(it.ID,func(it *factory.Item)error{it.Thread=append(it.Thread,factory.Message{ID:"message",State:"pending",Text:"chosen"});return nil}); err != nil {t.Fatal(err)}
   } else if ok,err := desk.SignalIfHeld(it.ID,signal); !ok || err != nil { t.Fatalf("queue: %t %v",ok,err) }
   j.beat(t.Context()); first:= <-replies
   j.beat(t.Context()); second:= <-replies
   if verb == "publish" || verb == "merge" {
    if len(first.Signals)!=0 || len(second.Signals)!=0 {t.Fatal("publish/merge crossed the live heartbeat")}
    _=runner.Stop(it.ID);waitSignalRunner(t,runner,it.ID)
    if err:=desk.Apply(held.ID,held.Worker,true,func(it *factory.Item)error{if verb=="merge"{it.Merging=&factory.MergeRun{State:factory.MergeQueued,Commit:signal.Words}};return nil});err!=nil{t.Fatal(err)}
    pulled,ok,err:=wc.Pull(t.Context());if !ok || err!=nil || len(pulled.Signals)!=1 || pulled.Signals[0].Verb!=verb{t.Fatalf("lost deferred pull: %+v %t %v",pulled,ok,err)}
    cur:=api.ToItem(pulled.Job);start:=stand(&cur,pulled.Signals)
    if err:=mirror.Save(cur);err!=nil{t.Fatal(err)}
    if err:=start(runner,it.ID);err!=nil{t.Fatal(err)}
    waitSignalRunner(t,runner,it.ID)
    j.lease=pulled.Lease.ID
    for range 2{j.beat(t.Context());hb:= <-replies;if len(hb.Signals)!=0{t.Fatalf("deferred signal repeated live: %+v",hb.Signals)}}
    if writes.Load()!=1{t.Fatalf("forge operation attempted %d times",writes.Load())}
    return
   }
   if len(first.Signals)!=1 || len(second.Signals)!=1 || second.Signals[0].Verb!=verb {t.Fatalf("signal lost after heartbeat reply: first=%+v next=%+v",first.Signals,second.Signals)}
   if !reflect.DeepEqual(first.Signals,second.Signals) {t.Fatalf("retry changed signal: first=%+v next=%+v",first.Signals,second.Signals)}
   if verb=="reply" {select{case <-replied: case <-time.After(3*time.Second):t.Fatal("reply did not run")}; for deadline:=time.Now().Add(3*time.Second);j.replyingNow();time.Sleep(time.Millisecond){if time.Now().After(deadline){t.Fatal("reply stayed in flight")}} }
   before,_:=mirror.Get(it.ID)
   // A replay must not overwrite a later choice or repeat a manager turn/note.
   if err:=mirror.Update(it.ID,func(it *factory.Item)error{it.Cap=19;it.Model="later";it.Autonomy="later";return nil});err!=nil{t.Fatal(err)}
   j.beat(t.Context()); third:= <-replies
   if !reflect.DeepEqual(second.Signals,third.Signals) {t.Fatalf("lost ack did not replay the signal: %+v",third.Signals)}
   for deadline:=time.Now().Add(3*time.Second);j.replyingNow();time.Sleep(time.Millisecond){if time.Now().After(deadline){t.Fatal("replayed reply stayed in flight")}}
   after,_:=mirror.Get(it.ID)
   if after.Cap!=19 || after.Model!="later" || after.Autonomy!="later" || !reflect.DeepEqual(before.Notes,after.Notes) || turns.Load()>1 {t.Fatalf("worker acted twice: before=%+v after=%+v turns=%d",before,after,turns.Load())}
   j.beat(t.Context()); fourth:= <-replies
   if len(fourth.Signals)!=0 {t.Fatalf("acknowledged signal repeated: %+v",fourth.Signals)}
   switch verb {case "budget": if before.Cap!=8 {t.Fatal("budget was not applied")}; case "model":if before.Model!="chosen"{t.Fatal("model was not applied")};case "autonomy":if before.Autonomy!="chosen"{t.Fatal("autonomy was not applied")};case "steer","answer":if len(before.Notes)!=1{t.Fatalf("expected one note, got %v",before.Notes)};case "hold":if before.Stream==nil || !before.Stream.Paused {t.Fatal("hold was not applied")};case "stop":if before.State!=factory.StateNew{t.Fatal("stop was not applied")};case "reply":if turns.Load()!=1{t.Fatal("reply was not applied")};case "reconcile_results":if recoveries.Load()!=1{t.Fatalf("reconciliation attempts=%d",recoveries.Load())}}
  })
 }
}

func TestNewSignalWorkerStillUsesAnOldFactory(t *testing.T) {
 var beats atomic.Int32
 j,_:=deliveryJob(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  if err:=decoder.Decode(&struct{}{});err!=nil{t.Error("old factory refuses new heartbeat body",err);w.WriteHeader(422);return}
  n:=beats.Add(1)
  _=json.NewEncoder(w).Encode(api.WorkerHeartbeat{Signals:[]api.WorkerSignal{{Verb:"budget",Budget:float64(n+2)}}})
 }))
 for _,want:=range []float64{3,4}{
  if !j.beat(t.Context()){t.Fatal("old factory heartbeat failed")}
  it,err:=j.o.Mirror.Get(j.id);if err!=nil || it.Cap!=want{t.Fatalf("old signal without delivery ID was suppressed: cap=%v err=%v",it.Cap,err)}
 }
}

func TestRepeatedBudgetDeliveriesCannotUndoALaterRaise(t *testing.T) {
 j,_:=deliveryJob(t,http.HandlerFunc(func(http.ResponseWriter,*http.Request){}))
 var signals []api.WorkerSignal
 if err:=json.Unmarshal([]byte(`[{"verb":"budget","budget":8,"delivery":"lease:a"},{"verb":"budget","budget":12,"delivery":"lease:b"}]`),&signals);err!=nil{t.Fatal(err)}
 j.signals(t.Context(),signals)
 it,_:=j.o.Mirror.Get(j.id);if it.Cap!=12{t.Fatalf("budget order lost: %v",it.Cap)}
 j.signals(t.Context(),signals[:1])
 it,_=j.o.Mirror.Get(j.id);if it.Cap!=12{t.Fatalf("replayed budget undid the later raise: %v",it.Cap)}
}

func TestReplyDeliveryRetriesFailureButNeverRepeatsAnAcceptedTurn(t *testing.T) {
 j,_:=deliveryJob(t,http.HandlerFunc(func(http.ResponseWriter,*http.Request){}))
 var attempts atomic.Int32
 finished:=make(chan struct{},4)
 j.o.Reply=func(context.Context,factory.Item,string)error{
  defer func(){finished<-struct{}{}}()
  if attempts.Add(1)==1{return context.DeadlineExceeded};return nil
 }
 var signals []api.WorkerSignal
 if err:=json.Unmarshal([]byte(`[{"verb":"reply","id":"message","words":"chosen","delivery":"lease:a"}]`),&signals);err!=nil{t.Fatal(err)}
 for n:=0;n<2;n++{
  j.signals(t.Context(),signals)
  select{case <-finished:case <-time.After(3*time.Second):t.Fatal("reply never completed")}
  for deadline:=time.Now().Add(3*time.Second);j.replyingNow();time.Sleep(time.Millisecond){if time.Now().After(deadline){t.Fatal("reply stayed in flight")}}
 }
 j.signals(t.Context(),signals)
 for deadline:=time.Now().Add(3*time.Second);j.replyingNow();time.Sleep(time.Millisecond){if time.Now().After(deadline){t.Fatal("replayed reply stayed in flight")}}
 if attempts.Load()!=2{t.Fatalf("accepted reply ran again: %d",attempts.Load())}
}

func TestRepeatedReconcileDeliveryDoesNotRestartManualRecovery(t *testing.T) {
 var attempts atomic.Int32
 j,_:=deliveryJob(t,http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if strings.HasPrefix(r.URL.Path,"/v1/workers/recover/"){attempts.Add(1);w.WriteHeader(422);return}
  var report api.WorkerReport
  _=json.NewDecoder(r.Body).Decode(&report)
  if report.Question!=nil{w.WriteHeader(200);return}
  w.WriteHeader(422)
 }))
 setDeliverySpend(t,j,.25)
 j.flush(t.Context())
 if j.blocked=="" || j.pending==nil{t.Fatal("fixture did not retain rejected results")}
 var signals []api.WorkerSignal
 if err:=json.Unmarshal([]byte(`[{"verb":"reconcile_results","delivery":"lease:a"}]`),&signals);err!=nil{t.Fatal(err)}
 j.signals(t.Context(),signals)
 first:=attempts.Load()
 j.signals(t.Context(),signals)
 if first!=1 || attempts.Load()!=first{t.Fatalf("one reconcile delivery started %d recovery attempts",attempts.Load())}
}

func waitSignalRunner(t *testing.T,r *run.Runner,id int){
 t.Helper()
 for deadline:=time.Now().Add(3*time.Second);r.Running(id);time.Sleep(time.Millisecond){if time.Now().After(deadline){t.Fatal("runner stayed in flight")}}
}
