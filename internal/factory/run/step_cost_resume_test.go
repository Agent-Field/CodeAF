package run

import (
 "context"
 "math"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
)

func TestStepCostFallbackResumeChargesOnlyNewJobSpend(t *testing.T) {
 calls:=0
 g:=newRig(t,map[factory.StageKind]Executor{factory.StageChat:ExecutorFunc(func(_ context.Context,job Job)(factory.StageResult,error) {
  calls++
  if calls==1 {return factory.StageResult{Spent:1,Chat:"fallback",JobBudgetStopped:true,Output:(SpendStop{Job:true,Said:"budget"}).Error()},nil}
  if job.Resume!="fallback" || job.Round!=1 {t.Errorf("resume %q round %d",job.Resume,job.Round)}
  return factory.StageResult{Done:true,Spent:1.25,Chat:"fallback"},nil
 })},nil)
 id:=g.add("fallback resume",chat("write"))
 if err:=g.st.Update(id,func(it *factory.Item)error {it.Cap=1;return nil});err!=nil {t.Fatal(err)}
 if err:=g.r.Launch(id);err!=nil {t.Fatal(err)}
 g.asked(id,"budget of $1 reached · $1 spent · raise it to $5, or stop?")
 if err:=g.r.Answer(id,true,"");err!=nil {t.Fatal(err)}
 got:=g.waitState(id,factory.StateLanded)
 if math.Abs(factory.JobSpent(got)-1.25)>1e-9 || math.Abs(got.Stream.Phases[0].StepCost()-1.25)>1e-9 {t.Fatalf("job spend %.4f step cost %.4f, want 1.2500",factory.JobSpent(got),got.Stream.Phases[0].StepCost())}
}
