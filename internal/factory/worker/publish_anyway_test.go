package worker_test

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestPublishAnywayPersonDecisionReachesHeldWorker(t *testing.T) {
 checkout:=t.TempDir()
 git(t,"","init","-q","-b","main",checkout)
 if err:=os.WriteFile(filepath.Join(checkout,"README.md"),[]byte("initial\n"),0600);err!=nil {t.Fatal(err)}
 git(t,checkout,"add","README.md")
 git(t,checkout,"commit","-qm","initial")
 src:=&pullSource{}
 g:=newRigWith(t,nil,func(o *run.Options){
  o.RepoDir=func(string)string{return checkout}
  o.Exec[factory.StageChat]=run.ExecutorFunc(func(_ context.Context,j run.Job)(factory.StageResult,error){
   err:=os.WriteFile(filepath.Join(j.Dir,"docs.md"),[]byte("docs only\n"),0600)
   return factory.StageResult{Done:err==nil},err
  })
  o.Exec[factory.StagePost]=run.NewPostExecutor(func(string)factory.Source{return src},func(string)[]string{return []string{"tests pass before anything posts"}})
 })
 id:=g.add(t,factory.Stage{Name:"write",Kind:factory.StageChat,On:true},factory.Stage{Name:"inspect",Kind:factory.StageCheck,Ask:"true",When:"touches auth",On:true},factory.Stage{Name:"post",Kind:factory.StagePost,Ask:"pr",On:true})
 if err:=g.st.Update(id,func(it *factory.Item)error{it.Assignee="me";return nil});err!=nil {t.Fatal(err)}
 if err:=g.floor.Launch(id);err!=nil {t.Fatal(err)}
 waiting:=g.waitFor(t,id,factory.StateNeedsYou)
 if !factory.OffersPublishAnyway(waiting) {t.Fatalf("worker did not offer choice: %+v",waiting)}
 if err:=g.st.Update(id,func(it *factory.Item)error{return factory.DecidePublishAnyway(it,"me",time.Now())});err!=nil {t.Fatal(err)}
 if err:=g.doors.Answer(id,true,factory.ChoiceTryOnceMore);err!=nil {t.Fatal(err)}
 g.waitFor(t,id,factory.StateLanded)
 writes:=src.writes()
 if len(writes)!=1 || !strings.Contains(writes[0].Body,"Published anyway by @me") {t.Fatalf("held worker lost person decision: %+v",writes)}
}
