package worker_test

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/run"
)

type stoppedPublisher struct {
 *pullSource
 sent chan struct{}
 answer chan struct{}
}

func (s *stoppedPublisher) Write(ctx context.Context,a factory.Action)(factory.Receipt,error){
 if a.BeforeWrite!=nil {if err:=a.BeforeWrite(ctx);err!=nil{return factory.Receipt{},err}}
 close(s.sent)
 <-s.answer
 return s.pullSource.Write(context.Background(),a)
}

func TestStopPublishWorkerAfterRequestSent(t *testing.T){
 checkout:=filepath.Join(t.TempDir(),"api")
 git(t,"","init","-q","-b","main",checkout)
 if err:=os.WriteFile(filepath.Join(checkout,"ledger.go"),[]byte("package ledger\n"),0600);err!=nil{t.Fatal(err)}
 git(t,checkout,"add","ledger.go");git(t,checkout,"commit","-qm","initial")
 dirs:=run.NewWorkdirs(filepath.Join(t.TempDir(),"work"),run.ExecGit{})
 src:=&stoppedPublisher{pullSource:&pullSource{},sent:make(chan struct{}),answer:make(chan struct{})}
 var once sync.Once
 release:=func(){once.Do(func(){close(src.answer)})}
 defer release()
 g:=newRigWith(t,nil,func(o *run.Options){
  o.Source=func(string)factory.Source{return src}
  o.Workdir=func(it factory.Item)(string,error){return dirs.For(it,checkout)}
  o.Push=func(string,string)error{return nil}
 })
 write:=check("printf 'package ledger\nconst fixed = true\n' > ledger.go")
 write.Name="proof"
 id:=g.add(t,write)
 if err:=g.st.Update(id,func(it *factory.Item)error{it.Autonomy=factory.AutonomyAuto;it.Starter="me";it.Cap=10;return nil});err!=nil{t.Fatal(err)}
 opened:=make(chan factory.Item,1)
 g.st.ObserveWrites(func(before,after factory.Item){if after.PullRequest!=nil&&after.State==factory.StateShipped{select{case opened<-after:default:}}})
 if err:=g.floor.Launch(id);err!=nil{t.Fatal(err)}
 select{case <-src.sent:case <-time.After(8*time.Second):t.Fatal("automatic proof did not send PR request")}
 if err:=g.floor.Stop(id);err!=nil{t.Fatal(err)}
 release()
 select{
 case it:=<-opened:
  if it.PullRequest.URL!="https://github.com/acme/api/pull/7"{t.Fatalf("wrong PR: %+v",it.PullRequest)}
  var words string;for _,l:=range it.Stream.Log{words+=l.Text}
  if !strings.Contains(words,"not closed"){t.Fatal("person was not told the PR remains open")}
 case <-time.After(8*time.Second):
  it,_:=g.st.Get(id);t.Fatalf("floor lost the successful PR receipt: state=%s pr=%+v",it.State,it.PullRequest)
 }
 for _,a:=range src.writes(){if a.Verb=="close"{t.Fatal("closed automatically")}}
}
