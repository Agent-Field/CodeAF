package main

import (
 "context"
 "encoding/json"
 "fmt"
 "math"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
 factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
 "github.com/Agent-Field/codeaf/internal/session"
)

func TestStepCostMatchesJobSpendLedgerAcrossResumedRounds(t *testing.T) {
 t.Setenv("CODEAF_HOME", t.TempDir())
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  fmt.Fprint(w, `{"model":"deepseek/deepseek-v4.1-flash","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"cost":0.01}}`)
 }))
 defer server.Close()
 st, _ := chatSpendFloor(t)
 it, err := st.Create(factory.Item{Repo:"acme/api", Num:7, State:factory.StateRunning, Stages:[]factory.Stage{{Name:"proof",Kind:factory.StageChat,On:true}}, Stream:&factory.Stream{Phases:[]factory.Phase{{Name:"proof",Kind:factory.StageChat,Round:1}}}})
 if err != nil { t.Fatal(err) }
 // Two rounds; round two is interrupted and reopened with a new live meter.
 for _, round := range []int{1,2,2} {
  if err := st.Annotate(it.ID,func(cur *factory.Item) error { cur.Stream.Phases[0].Round=round; return nil }); err != nil { t.Fatal(err) }
  current,_ := st.Get(it.ID)
  b := &stageBudget{floor:st,item:current,stage:"proof",round:round}
  agent,err := session.New(session.Config{Workspace:t.TempDir(),SessionFile:filepath.Join(t.TempDir(),"stage.jsonl"),ProfileDir:t.TempDir(),Model:"deepseek/deepseek-v4.1-flash", APIKey:"fixture",BaseURL:server.URL,JobBudget:b,Unattended:true,DeterministicNames:true})
  if err != nil { t.Fatal(err) }
  _,err=agent.RunErrand(context.Background(),session.ErrandAsk{Purpose:"step_cost_fixture",Role:"brief",System:"Return ok.",Messages:[]session.ErrandMessage{{Role:"user",Text:"Check."}},MaxTokens:100,Deadline:time.Second})
  if err != nil { t.Fatal(err) }
  c:= &stageConversation{agent:agent,budget:b}
  res:=factory.StageResult{Done:true,Spent:c.Spent(),SpendRecorded:true,Chat:fmt.Sprintf("round-%d",round)}
  _=agent.Close()
  // A transport retry writes exactly the same report twice, through its codec.
  report:=api.ToWorkerReport(api.FromRunReport(factory.RunReport{Stages:[]factory.StageReport{{Name:"proof",Round:round,State:factory.PhaseRunning,Result:&res}}}))
  for n:=0;n<2;n++ { if err:=st.Annotate(it.ID,func(cur *factory.Item) error { return factoryrun.ApplyReport(cur,report,time.Now()) });err!=nil {t.Fatal(err)} }
  // Reload the saved job for the next admission.
  raw,_:=json.Marshal(current); if err:=json.Unmarshal(raw,&it);err!=nil {t.Fatal(err)}
 }
 if !session.FlushUsage() {t.Fatal("ledger did not flush")}
 lines,err:=session.ReadUsage(session.UsageLedgerPath(),time.Time{});if err!=nil {t.Fatal(err)}
 ledger:=0.0
 for _,line:=range lines {if line.FactoryJob==factory.LedgerName(it) {ledger+=line.USD}}
 got,_:=st.Get(it.ID)
 cost:=api.FromItem(got).Steps[0].Cost
 if math.Abs(ledger-.03)>1e-9 {t.Fatalf("tagged ledger = %.4f, want .0300",ledger)}
 // This fixture has no manager, title or memory calls: every tagged dollar is a step's.
 if math.Abs(cost-ledger)>1e-9 {t.Fatalf("step cost = %.4f, tagged job ledger = %.4f; non-step spend = 0",cost,ledger)}
}
