package main

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "net/http/httputil"
 "net/url"
  "path/filepath"
 "sync"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 "github.com/Agent-Field/codeaf/internal/factory/client"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
 factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
 factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestAppFactoryLifetimeBudget(t *testing.T) {
 for _, action := range []string{"factory.job_start", "factory.job_run_again", "factory.job_step_rerun", "carry on", "old saved total", "bring back"} {
  t.Run(action, func(t *testing.T) {
   s := newLocalSetup(t)
   t.Setenv("OPENROUTER_API_KEY", "test-key")
   t.Setenv("CODEAF_BASE_URL", newStageModel(t).server.URL)
   s.backend.open = func(ask appOpenAsk) (*appConn, error) {
    return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
   }
   st, err := store.Open(v3FactoryRoot()); if err != nil { t.Fatal(err) }
   if err := st.SetCheckout("acme/api", t.TempDir()); err != nil { t.Fatal(err) }
   release, ok := st.TryWorker(); if !ok { t.Fatal("runner ownership") }; defer release()
   mirror, err := store.Open(filepath.Join(t.TempDir(), "mirror")); if err != nil { t.Fatal(err) }
   transcript, _ := jobConversation(t)
   var calls atomic.Int32
   model := &budgetModel{spent: 5.25}
   model.maker.Script = func(ctx context.Context, c *factoryrun.FakeConversation) {
    if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true}); err != nil { c.Fail(err) }
   }
   chat := factoryrun.NewChatExecutor(model)
   runner := factoryrun.New(factoryrun.Options{Store: mirror, Talk: sessionTalk{}, Exec: map[factory.StageKind]factoryrun.Executor{
    factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, j factoryrun.Job) (factory.StageResult, error) {
     if calls.Add(1) > 1 || (action == "old saved total" || action == "bring back") { model.spent = 1 }
     return chat.Run(ctx, j)
    }),
   }})
   
   status := s.start()
   workerURL, replays := replayBudgetReports(t, status.URL)
   it, err := st.Create(factory.Item{Title: "Lifetime budget", Repo: "acme/api", Num: 99, Kind: factory.KindIssue, State: factory.StateNew, Cap: 6, Autonomy: factory.AutonomyManual, Talk: transcript,
    Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: "done", Max: 1, On: true}}})
   if err != nil { t.Fatal(err) }; t.Cleanup(func() { _ = runner.Stop(it.ID) })
   ctx, cancel := context.WithCancel(context.Background())
						done := make(chan struct{})
						var mu sync.Mutex
						credentials := map[int]string{}
						go func() {
							defer close(done)
							factoryworker.Run(ctx, factoryworker.Options{
								Dial: func(slot int) *client.WorkerClient {
									mu.Lock()
									defer mu.Unlock()
									return client.Open(workerURL, s.backend.local.host.token).Worker(credentials[slot])
								},
								Keep: func(slot int, w *client.WorkerClient) {
									mu.Lock()
									defer mu.Unlock()
									credentials[slot] = w.Credential()
								},
								Attach: api.WorkerAttach{Machine: "budget-worker", Name: "Budget worker", Handle: "owner", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
								Runner: runner, Mirror: mirror, Slots: api.DefaultRunnerSlots, ReportEvery: 10 * time.Millisecond, BeatEvery: 10 * time.Millisecond,
							})
						}()
						t.Cleanup(func() { cancel(); <-done })
   read := func() api.Job { var j api.Job; s.must("factory.job", map[string]any{"factory": status.Factory, "job": it.ID}, &j); return j }
   act := func(method string, extra map[string]any) { j := read(); p := map[string]any{"factory": status.Factory, "job": it.ID, "expect": map[string]any{"version": j.Version}}; for k,v := range extra { p[k]=v }; s.must(method,p,&appwire.FactoryJobActed{}) }
   wait := func(what string, ok func(api.Job) bool) api.Job {
    t.Helper(); deadline := time.Now().Add(5*time.Second)
    for { j:=read(); if ok(j) { return j }; if time.Now().After(deadline) { t.Fatalf("%s: lifetime=%g cost=%g budget=%g questions=%+v calls=%d",what,j.LifetimeCost,j.Cost,j.Budget,j.Questions,calls.Load()) }; time.Sleep(10*time.Millisecond) }
   }
   if (action == "old saved total" || action == "bring back") {
    if err := st.Update(it.ID,func(it *factory.Item) error { it.SpentBefore=6.25; if action=="bring back" { it.State=factory.StateDismissed; it.DoneHow="dropped" }; return nil }); err != nil { t.Fatal(err) }
   } else {
    act("factory.job_start",nil)
    wait("first run spends 5.25 under 6",func(j api.Job) bool { return j.Cost==5.25 && len(j.Steps)==1 && j.Steps[0].State=="done" && j.State!=api.StateRunning })
    // The next app action starts from saved work, as after a stopped run.
    if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateNew; it.Failure=&factory.Failure{Step:"write", Next:factory.FailureStopped}; it.Stream.Ended=time.Now(); return nil }); err != nil { t.Fatal(err) }
    if action == "factory.job_step_rerun" {
     if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateNeedsYou; return nil }); err != nil { t.Fatal(err) }
     act(action,map[string]any{"step":"write"})
    } else if action == "carry on" {
     if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateRunning; it.Failure=nil; it.Stream.Paused=true; it.Stream.Ended=time.Time{}; it.Stream.Phases[0].State=factory.PhaseRunning; it.Stream.Phases[0].Chat=""; return nil }); err != nil { t.Fatal(err) }
     act("factory.job_start",nil)
    } else { act(action,nil) }
   }
   if action=="bring back" { act("factory.job_restore",nil) }; if (action == "old saved total" || action == "bring back") { act("factory.job_start",nil) }
   held := wait("job total 6.25 must ask at 6",func(j api.Job) bool { return j.SpendingLimit!=nil })
   if held.LifetimeCost!=6.25 || held.SpendingLimit.Spent!=6.25 { t.Fatalf("budget must read lifetime: %+v",held) }
   if len(held.Questions)==0 || !strings.Contains(held.Questions[0].Text,"$6.25") || !strings.Contains(held.Questions[0].Text,"$6") { t.Fatalf("missing totals: %+v",held.Questions) }
   if action!="old saved total" && action!="bring back" && replays.Load()==0 { t.Fatal("no paid report replayed") }
   before:=calls.Load()
   if action=="old saved total" || action=="bring back" { if before!=0 { t.Fatalf("exhausted start ran %d calls",before) } }
   j0:=read(); _, code, _ := s.call("factory.job_step_rerun",map[string]any{"factory":status.Factory,"job":it.ID,"step":"write","expect":map[string]any{"version":j0.Version}}); if code=="" || calls.Load()!=before { t.Fatal("exhausted rerun started work") }
   j := read(); _, _, _ = s.call("factory.job_set_budget",map[string]any{"factory":status.Factory,"job":it.ID,"budget":5,"expect":map[string]any{"version":j.Version}})
   if j:=read(); j.SpendingLimit==nil || calls.Load()!=before { t.Fatalf("low raise released work: %+v",j) }
   act("factory.job_set_budget",map[string]any{"budget":10})
   if before>0 {
    wait("raise releases finished step",func(j api.Job) bool { return j.SpendingLimit==nil && j.State!=api.StateRunning })
    if err:=st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateNew; it.Failure=nil; it.Stream.Ended=time.Now(); return nil }); err!=nil { t.Fatal(err) }
    act("factory.job_start",nil)
   }
   wait("raise permits spending that still counts",func(j api.Job) bool { return calls.Load()>before && j.LifetimeCost==7.25 && j.SpendingLimit==nil })
  })
 }
}

// Re-deliver the exact acknowledged paid request, including its receipt key.
// The app reads below must still show each dollar once.
func replayBudgetReports(t *testing.T, base string) (string, *atomic.Int32) {
 t.Helper()
 target, err := url.Parse(base)
 if err != nil { t.Fatal(err) }
 var replays atomic.Int32
 proxy := httputil.NewSingleHostReverseProxy(target)
 proxy.Transport = roundTripper(func(r *http.Request) (*http.Response,error) {
  if r.Method!=http.MethodPost || !strings.HasSuffix(r.URL.Path,"/report") { return http.DefaultTransport.RoundTrip(r) }
  body, err := io.ReadAll(r.Body)
  if err!=nil { return nil,err }; _=r.Body.Close()
  r.Body=io.NopCloser(bytes.NewReader(body))
  response, err := http.DefaultTransport.RoundTrip(r)
  if err!=nil { return nil,err }
  var report api.WorkerReport
  if response.StatusCode==http.StatusOK && json.Unmarshal(body,&report)==nil && report.Cost>0 {
   replay:=r.Clone(r.Context()); replay.Body=io.NopCloser(bytes.NewReader(body))
   duplicate, err:=http.DefaultTransport.RoundTrip(replay)
   if err!=nil { t.Errorf("replay paid report: %v",err) } else {
    _,_=io.Copy(io.Discard,duplicate.Body); _=duplicate.Body.Close()
    if duplicate.StatusCode!=http.StatusOK { t.Errorf("replay paid report status=%d",duplicate.StatusCode) } else { replays.Add(1) }
   }
  }
  return response,nil
 })
 server:=httptest.NewServer(proxy)
 t.Cleanup(server.Close)
 return server.URL,&replays
}
