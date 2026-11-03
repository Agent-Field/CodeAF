package run

import (
 "strings"
 "testing"
 "time"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/api"
)

func publishAnywayJob() factory.Item {
 return factory.Item{ID:3, Repo:"r", Title:"Docs", Assignee:"alice", State:factory.StateNeedsYou, QKind:factory.QKindOutOfAttempts, HeadCommit:strings.Repeat("a",40), Change:&factory.Change{Files:[]factory.ChangedFile{{Path:"docs.md"}}},
 Stages:[]factory.Stage{{Name:"inspect",Kind:factory.StageCheck,When:"touches auth"},{Name:"post",Kind:factory.StagePost,Ask:"pr"}},
 Stream:&factory.Stream{Cur:1,Phases:[]factory.Phase{{Name:"inspect",Kind:factory.StageCheck,State:factory.PhaseSkipped,SkipReason:"when touches auth does not match the change files"},{Name:"post",Kind:factory.StagePost,State:factory.PhaseWaiting,Result:&factory.StageResult{Claims:[]factory.Claim{{Medium:"policy",Evidence:"tests pass before anything posts"}}}}}}}
}

func TestPublishAnywayPostRequiresCurrentPersonDecision(t *testing.T) {
 at:=time.Date(2026,11,3,15,0,0,0,time.UTC)
 policy:=[]string{"tests pass before anything posts"}
 for _, tc:=range []struct{name string; change func(*factory.Item); want bool}{
  {"person",func(*factory.Item){},true},
  {"automatic approval",func(it *factory.Item){it.PublishAnyway.Automatic=true},false},
  {"files changed",func(it *factory.Item){it.Change.Files[0].Path="other.md"},false},
  {"head changed",func(it *factory.Item){it.HeadCommit=strings.Repeat("b",40)},false},
  {"skips changed",func(it *factory.Item){it.Stream.Phases[0].SkipReason="different reason"},false},
 } {t.Run(tc.name,func(t *testing.T){
  it:=publishAnywayJob()
  if !api.CanFor(it,"alice").PublishAnyway {t.Fatal("eligible job has no offer")}
  if err:=factory.DecidePublishAnyway(&it,"alice",at);err!=nil {t.Fatal(err)}
  tc.change(&it)
  src:=&fakeSource{}
  res,err,_:=runPost(t,src,policy,it,"pr",nil)
  if err!=nil || res.Done!=tc.want || (len(src.got)==1)!=tc.want {t.Fatalf("published=%v writes=%d err=%v",res.Done,len(src.got),err)}
  if tc.want {
   body:=src.got[0].Body
   for _, want:=range []string{"inspect: skipped", "when touches auth does not match the change files", "Published anyway by @alice at 2026-11-03T15:00:00Z; skipped checks remain unmeasured."} {if !strings.Contains(body,want){t.Errorf("PR missing %q: %s",want,body)}}
   if strings.Contains(body,"Passed locally") || it.Stream.Phases[0].Result!=nil || len(it.Proof)!=0 {t.Fatal("skip gained measured proof")}
  }
 })}
}

func TestPublishAnywayPostRefusesOtherUnmetChecks(t *testing.T) {
 for _, state:=range []factory.PhaseState{factory.PhaseFailed,factory.PhasePending,factory.PhaseRunning,factory.PhaseDone} {t.Run(string(state),func(t *testing.T){
  it:=publishAnywayJob()
  it.Stages=append(it.Stages,factory.Stage{Name:"unit",Kind:factory.StageCheck})
  it.Stream.Phases=append(it.Stream.Phases,factory.Phase{Name:"unit",Kind:factory.StageCheck,State:state})
  if api.CanFor(it,"alice").PublishAnyway {t.Fatal("unmet check offered override")}
  if err:=factory.DecidePublishAnyway(&it,"alice",time.Now());err==nil {t.Fatal("unmet check accepted press")}
  src:=&fakeSource{}
  res,err,_:=runPost(t,src,[]string{"tests pass before anything posts"},it,"pr",nil)
  if err!=nil || res.Done || len(src.got)!=0 || strings.Contains(res.Output,"Publish anyway") {t.Fatalf("unmet check escaped: %+v %v",res,err)}
 })}
}

func TestPublishAnywayNeverAutomaticOrOneGreen(t *testing.T) {
 it:=publishAnywayJob()
 it.Autonomy=factory.AutonomyAuto
 it.Approval=&factory.Approval{Automatic:true,Kind:"proof",State:"approved"}
 src:=&fakeSource{}
 res,err,_:=runPost(t,src,[]string{"tests pass before anything posts"},it,"pr",nil)
 if err!=nil || res.Done || len(src.got)!=0 {t.Fatal("automatic approval published a skipped check")}
 it.Stream.Phases[1].Result.Claims[0].Evidence="no pull request without a green check"
 if api.CanFor(it,"alice").PublishAnyway {t.Fatal("one-green policy offered override")}
 res,err,_=runPost(t,src,[]string{"no pull request without a green check"},it,"pr",nil)
 if err!=nil || res.Done || len(src.got)!=0 {t.Fatal("one-green published only skips")}
}
