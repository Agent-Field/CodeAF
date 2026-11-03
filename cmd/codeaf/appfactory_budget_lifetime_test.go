package main

import (
 "context"
 "fmt"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestAppFactoryLifetimeBudget(t *testing.T) {
 for _, action := range []string{"factory.job_start", "factory.job_run_again", "factory.job_step_rerun", "carry on", "old saved total"} {
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
   transcript, _ := jobConversation(t)
   var calls atomic.Int32
   model := &budgetModel{spent: 5.25}
   model.maker.Script = func(ctx context.Context, c *factoryrun.FakeConversation) {
    if err := c.Spec.Stage.Report(ctx, factory.StageResult{Done: true}); err != nil { c.Fail(err) }
   }
   chat := factoryrun.NewChatExecutor(model)
   runner := factoryrun.New(factoryrun.Options{Store: st, Talk: sessionTalk{}, Exec: map[factory.StageKind]factoryrun.Executor{
    factory.StageChat: factoryrun.ExecutorFunc(func(ctx context.Context, j factoryrun.Job) (factory.StageResult, error) {
     if calls.Add(1) > 1 || action == "old saved total" { model.spent = 1 }
     return chat.Run(ctx, j)
    }),
   }})
   hook := s.backend.local.seamHook
   s.backend.local.seamHook = func(seam factory.Seam) factory.Seam { seam = hook(seam); seam.Launch, seam.Answer = runner.Launch, runner.Answer; return seam }
   status := s.start()
   it, err := st.Create(factory.Item{Title: "Lifetime budget", Repo: "acme/api", Num: 99, Kind: factory.KindIssue, State: factory.StateNew, Cap: 6, Autonomy: factory.AutonomyManual, Talk: transcript,
    Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, Ask: "write", Until: "done", Max: 1, On: true}}})
   if err != nil { t.Fatal(err) }; t.Cleanup(func() { _ = runner.Stop(it.ID) })
   read := func() api.Job { var j api.Job; s.must("factory.job", map[string]any{"factory": status.Factory, "job": it.ID}, &j); return j }
   act := func(method string, extra map[string]any) { j := read(); p := map[string]any{"factory": status.Factory, "job": it.ID, "expect": map[string]any{"version": j.Version}}; for k,v := range extra { p[k]=v }; s.must(method,p,&appwire.FactoryJobActed{}) }
   wait := func(what string, ok func(api.Job) bool) api.Job {
    t.Helper(); deadline := time.Now().Add(5*time.Second)
    for { j:=read(); if ok(j) { return j }; if time.Now().After(deadline) { t.Fatalf("%s: lifetime=%g cost=%g budget=%g questions=%+v calls=%d",what,j.LifetimeCost,j.Cost,j.Budget,j.Questions,calls.Load()) }; time.Sleep(10*time.Millisecond) }
   }
   if action == "old saved total" {
    if err := st.Update(it.ID,func(it *factory.Item) error { it.ChatSpent=6.25; return nil }); err != nil { t.Fatal(err) }
   } else {
    act("factory.job_start",nil)
    wait("first run spends 5.25 under 6",func(j api.Job) bool { return j.Cost==5.25 && len(j.Steps)==1 && j.Steps[0].State=="done" })
    // The next app action starts from saved work, as after a stopped run.
    if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateNew; it.Failure=&factory.Failure{Step:"write", Next:factory.FailureStopped}; it.Stream.Ended=time.Now(); return nil }); err != nil { t.Fatal(err) }
    if action == "factory.job_step_rerun" {
     if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateNeedsYou; return nil }); err != nil { t.Fatal(err) }
     act(action,map[string]any{"step":"write"})
    } else if action == "carry on" {
     if err := st.Update(it.ID,func(it *factory.Item) error { it.State=factory.StateRunning; it.Stream.Paused=true; it.Stream.Ended=time.Time{}; it.Stream.Phases[0].State=factory.PhaseRunning; it.Stream.Phases[0].Chat=""; return nil }); err != nil { t.Fatal(err) }
     act("factory.job_start",nil)
    } else { act(action,nil) }
   }
   if action == "old saved total" { act("factory.job_start",nil) }
   held := wait("job total 6.25 must ask at 6",func(j api.Job) bool { return j.SpendingLimit!=nil })
   if held.LifetimeCost!=6.25 || held.SpendingLimit.Spent!=6.25 { t.Fatalf("budget must read lifetime: %+v",held) }
   if len(held.Questions)==0 || !strings.Contains(held.Questions[0].Text,"$6.25") || !strings.Contains(held.Questions[0].Text,"$6") { t.Fatalf("missing totals: %+v",held.Questions) }
   before:=calls.Load()
   j := read(); _, _, _ = s.call("factory.job_set_budget",map[string]any{"factory":status.Factory,"job":it.ID,"budget":5,"expect":map[string]any{"version":j.Version}})
   if j:=read(); j.SpendingLimit==nil || calls.Load()!=before { t.Fatalf("low raise released work: %+v",j) }
   act("factory.job_set_budget",map[string]any{"budget":10})
   wait("raise resumes paid work",func(j api.Job) bool { return calls.Load()>before && j.LifetimeCost>6.25 && j.SpendingLimit==nil })
   t.Logf("%s: job total $6.25 of $6 held; raise resumes",fmt.Sprint(action))
  })
 }
}
