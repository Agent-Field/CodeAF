package main

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
 "strings"
 "github.com/Agent-Field/codeaf/internal/appwire"
 "github.com/Agent-Field/codeaf/internal/factory/record"
)

func TestPersonPublishesAnywayThroughJobAction(t *testing.T) {
	f := startActionFactory(t)
	alice := signedIn(t, f, 1, "alice")
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "alice", HeadCommit: actionHead, Question: "Publishing waits", QKind: factory.QKindOutOfAttempts,
		Change: &factory.Change{Files: []factory.ChangedFile{{Path: "docs.md"}}},
		Stages: []factory.Stage{{Name: "inspect", Kind: factory.StageCheck, When: "touches auth"}, {Name: "post", Kind: factory.StagePost, Ask: "pr"}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "inspect", Kind: factory.StageCheck, State: factory.PhaseSkipped, SkipReason: "when touches auth does not match the change files"}, {Name: "post", Kind: factory.StagePost, State: factory.PhaseWaiting, Result: &factory.StageResult{Claims: []factory.Claim{{Medium: "policy", Evidence: "tests pass before anything posts"}}}}}}})
	if !alice.job(t,it.ID).Can.PublishAnyway { t.Fatal("eligible skip has no can action") }
 hello:=string(alice.w.lines[0]); if !strings.Contains(hello,appwire.FeatureFactoryPublishAnyway) { t.Fatal("hello missing feature") }
 stale:=alice.params(t,it.ID,nil)
 if err:=f.st.Update(it.ID,func(cur *factory.Item) error {cur.Title="revised";return nil});err!=nil {t.Fatal(err)}
 alice.refused(t,"factory.job_publish_anyway",stale,appwire.CodeStale)
 out := alice.act(t, "factory.job_publish_anyway", it.ID, nil)
	if out.Current == nil || out.Current.PublishAnyway == nil || out.Current.PublishAnyway.DecidedBy != "alice" || out.Current.PublishAnyway.At.IsZero() || f.count(record.Approved)!=1 {
		t.Fatal("publish anyway did not return the job")
	}
 nohead:=it
 nohead.ID=0
 nohead.HeadCommit=""
 nohead.Change=&factory.Change{Patch:"complete docs patch",Files:[]factory.ChangedFile{{Path:"docs.md"}}}
 nohead=f.create(t,nohead)
 second:=alice.act(t,"factory.job_publish_anyway",nohead.ID,nil)
 if second.Current==nil || second.Current.PublishAnyway==nil || strings.HasSuffix(second.Current.PublishAnyway.Words," ") {t.Fatal("files-bound approval has an invalid receipt")}

}

func TestPublishAnywayRefusesOtherPerson(t *testing.T) {
	f := startActionFactory(t)
	_ = signedIn(t, f, 1, "alice")
	bob := signedIn(t, f, 2, "bob")
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "alice"})
	bob.refused(t, "factory.job_publish_anyway", bob.params(t, it.ID, nil), "forbidden")
}

func TestPublishAnywayJobActionRefusesFailedAndUnmeasured(t *testing.T) {
 f:=startActionFactory(t)
 alice:=signedIn(t,f,1,"alice")
 for _, state:=range []factory.PhaseState{factory.PhaseFailed,factory.PhasePending} {
  it:=f.create(t,factory.Item{State:factory.StateNeedsYou,Assignee:"alice",HeadCommit:actionHead,QKind:factory.QKindOutOfAttempts,Change:&factory.Change{Files:[]factory.ChangedFile{{Path:"docs.md"}}},Stages:[]factory.Stage{{Name:"inspect",Kind:factory.StageCheck,When:"touches auth"},{Name:"unit",Kind:factory.StageCheck},{Name:"post",Kind:factory.StagePost}},Stream:&factory.Stream{Cur:2,Phases:[]factory.Phase{{Name:"inspect",Kind:factory.StageCheck,State:factory.PhaseSkipped,SkipReason:"no matching files"},{Name:"unit",Kind:factory.StageCheck,State:state},{Name:"post",Kind:factory.StagePost,State:factory.PhaseWaiting,Result:&factory.StageResult{Claims:[]factory.Claim{{Medium:"policy",Evidence:"tests pass before anything posts"}}}}}}})
  if alice.job(t,it.ID).Can.PublishAnyway {t.Fatal("unmet check offered publish anyway")}
  words,_:=alice.refused(t,"factory.job_publish_anyway",alice.params(t,it.ID,nil),appwire.CodeConflict)
  if !strings.Contains(words,"check failed, is pending, was not measured") {t.Fatal(words)}
 }
}
