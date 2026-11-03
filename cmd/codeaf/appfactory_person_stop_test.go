package main

import (
factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
"github.com/Agent-Field/codeaf/internal/factory/intake"
"github.com/Agent-Field/codeaf/internal/factory/pipeline"
"github.com/Agent-Field/codeaf/internal/factory/record"
 forge "github.com/Agent-Field/codeaf/internal/praf/github"
"context"
"net/http"
"net/http/httptest"
"sync"
"sync/atomic"
"github.com/Agent-Field/codeaf/internal/session"
"github.com/Agent-Field/codeaf/internal/factory/store"
"encoding/json"
"errors"
"fmt"
"os"
"path/filepath"
"time"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// Every authority converges on the same launch door. A name or fresh process
// supplies no permission to undo the person's Stop.
func TestFactoryPersonStopLaunchAuthorities(t *testing.T) {
	for _, authority := range []string{"manager", "other_job_manager", "crew", "standing_item", "scheduler", "github_poll", "new_commits", "restart"} {
		t.Run(authority, func(t *testing.T) {
			s, status := newPauseSetup(t)
			host := s.backend.local.host
			it, err := host.floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNeedsYou, Starter: "owner", Talk: filepath.Join(t.TempDir(),"manager.jsonl"), Question: "which command?", QKind: "step"})
			if err != nil {
				t.Fatal(err)
			}
			var before api.Job
			s.must("factory.job", map[string]any{"factory": status.Factory, "job": it.ID}, &before)
			s.must("factory.job_stop", map[string]any{"factory": status.Factory, "job": it.ID, "expect": map[string]any{"version": before.Version}}, &appwire.FactoryJobActed{})
			desk, err := lease.Open(host.floor, lease.Options{})
			if err != nil {
				t.Fatal(err)
			}
            // The shared launch seam has no caller privilege to borrow. Exercise
            // manager tools, queue admission and a fresh desk as well as Launch.
            switch authority {
            case "manager", "standing_item":
                _,err=runDoor(host.floor,"",s.backend.localProfile()).(session.RunStarter).StartRun(context.Background(),it.Talk)
                if !errors.Is(err,factory.ErrPersonStopped){t.Fatalf("manager tool admitted %s: %v",authority,err)}
            case "scheduler", "restart":
                if authority=="restart" {
                    reopened,openErr:=store.Open(host.floor.Root());if openErr!=nil{t.Fatal(openErr)}
                    desk,err=lease.Open(reopened,lease.Options{});if err!=nil{t.Fatal(err)}
                }
                _,err=desk.Pull(lease.Worker{ID:authority,Handle:"owner",Solo:true})
                if !errors.Is(err,lease.ErrNoJob){t.Fatalf("queue admitted %s: %v",authority,err)}
            case "github_poll", "new_commits":
                pollStoppedJob(t,host.floor,it.ID)
                _,err=desk.Pull(lease.Worker{ID:authority,Handle:"owner",Solo:true})
                if !errors.Is(err,lease.ErrNoJob){t.Fatalf("GitHub admitted %s: %v",authority,err)}
            case "other_job_manager", "crew":
                // A different conversation cannot borrow this manager's identity.
                _,err=runDoor(host.floor,"",s.backend.localProfile()).(session.RunStarter).StartRun(context.Background(),filepath.Join(t.TempDir(),"other-manager.jsonl"))
                if !errors.Is(err,factory.ErrNotManager){t.Fatalf("foreign manager identity accepted: %v",err)}
                err=factoryrun.New(factoryrun.Options{Store:host.floor}).Launch(it.ID)
                if !errors.Is(err,factory.ErrPersonStopped){t.Fatalf("runner admitted %s: %v",authority,err)}
            }
            if err:=lease.NewDoors(desk).Launch(it.ID);!errors.Is(err,factory.ErrPersonStopped){t.Fatalf("lowest queue door admitted %s: %v",authority,err)}
			var after api.Job
			s.must("factory.job", map[string]any{"factory": status.Factory, "job": it.ID}, &after)
			if after.State != api.StateNeedsYou {
				t.Fatalf("stopped job reads as %s", after.State)
			}
			if after.Can.Answer || after.Can.Approve || after.Can.PRReady || after.Can.Stop {
				t.Fatalf("stopped job offers effectful controls: %+v", after.Can)
			}
			if !after.Can.StartNow {
				t.Fatal("person cannot Start stopped job")
			}
			saved, err := host.floor.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := factory.ReadinessGate(saved); err == nil || !strings.Contains(err.Error(), "person stopped") {
				t.Fatalf("readiness ignored Stop: %v", err)
			}
			if _, err := lease.NewDoors(desk).AnswerPending(it.ID, saved.Talk, "late-question", "yes"); err == nil || !strings.Contains(err.Error(), "person stopped") {
				t.Fatalf("manager answer ignored Stop: %v", err)
			}
			if factory.Automatic(saved) {
				t.Fatal("stopped job remains automatic")
			}
		})
	}
}

func stoppedAppJob(t *testing.T, initial factory.State) (*localSetup, appwire.FactoryLocalStatus, factory.Item) {
 t.Helper()
 s, status := newPauseSetup(t)
 floor := s.backend.local.host.floor
 if err := os.WriteFile(filepath.Join(s.backend.localProfile(),"config.json"), []byte(`{"models.tasks.one":"test/model"}`),0600); err != nil { t.Fatal(err) }
 workspace := t.TempDir()
 gitCheckout(t, workspace, "acme/api")
 if err := floor.SetCheckout("acme/api",workspace); err != nil { t.Fatal(err) }
 it, err := floor.Create(factory.Item{Repo:"acme/api", Kind:factory.KindIssue, Title:"Stop is a person decision", Starter:"owner", State:initial, Autonomy:factory.AutonomyAuto, Cap:10,
  Question:"Which test command?", QKind:"step", Asking:&factory.Asked{Stage:"test",With:factory.AskedYou,Question:"Which test command?"},
  Stages:[]factory.Stage{{Name:"test",Kind:factory.StageCheck,Ask:"true",On:true,Until:factory.UntilDone}},
  Stream:&factory.Stream{Phases:[]factory.Phase{{Name:"test",Kind:factory.StageCheck,State:factory.PhaseWaiting,Round:1}}},
 })
 if err != nil { t.Fatal(err) }
 var before api.Job
 s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&before)
 s.must("factory.job_stop",map[string]any{"factory":status.Factory,"job":it.ID,"expect":map[string]any{"version":before.Version}},&appwire.FactoryJobActed{})
 saved, err := floor.Get(it.ID)
 if err != nil { t.Fatal(err) }
 return s,status,saved
}

func TestFactoryPersonStopPersonControls(t *testing.T) {
 for _, method := range []string{"factory.job_start","factory.job_resume","factory.job_run_again","factory.job_step_rerun"} {
  t.Run(method,func(t *testing.T) {
   s,status,it := stoppedAppJob(t,factory.StateNeedsYou)
   var stopped api.Job
   s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&stopped)
   args := map[string]any{"factory":status.Factory,"job":it.ID,"expect":map[string]any{"version":stopped.Version}}
   if method == "factory.job_step_rerun" { args["step"]="test" }
   s.must(method,args,&appwire.FactoryJobActed{})
   saved,err := s.backend.local.host.floor.Get(it.ID)
   if err != nil { t.Fatal(err) }
   if factory.StoppedByPerson(saved) || saved.State != factory.StateQueued { t.Fatalf("person's %s failed to run: state=%s stopped=%v",method,saved.State,factory.StoppedByPerson(saved)) }
   desk,err := lease.Open(s.backend.local.host.floor,lease.Options{})
   if err != nil { t.Fatal(err) }
   held,err := desk.Pull(lease.Worker{ID:"person-start",Handle:"owner",Solo:true})
   if err != nil || held.Job != it.ID || held.ThreadOnly { t.Fatalf("person's start not admitted: %+v %v",held,err) }
   mirror,err:=store.Open(t.TempDir());if err!=nil{t.Fatal(err)}
   saved.State=factory.StateRunning;saved.Stream.Paused=true
   if err:=mirror.Save(saved);err!=nil{t.Fatal(err)}
   ran:=make(chan struct{},1)
   runner:=factoryrun.New(factoryrun.Options{Store:mirror,Exec:map[factory.StageKind]factoryrun.Executor{factory.StageCheck:factoryrun.ExecutorFunc(func(context.Context,factoryrun.Job)(factory.StageResult,error){select{case ran<-struct{}{}:default:};return factory.StageResult{Done:true},nil})}})
   t.Cleanup(func(){runner.Withdraw(saved.ID)})
   if err:=runner.Hold(saved.ID,false);err!=nil{t.Fatal(err)}
   select{case <-ran:case <-time.After(3*time.Second):t.Fatal("person control admitted a lease but did not execute its step")}

  })
 }
}

func TestFactoryPersonStopProofAndDraftPR(t *testing.T) {
 for _, draft := range []bool{false,true} {
  t.Run(fmt.Sprint("draft=",draft),func(t *testing.T) {
   s,status := newPauseSetup(t)
   floor := s.backend.local.host.floor
   it := factory.Item{Repo:"acme/api",Kind:factory.KindIssue,State:factory.StateLanded,Starter:"owner",Autonomy:factory.AutonomyAuto,Cap:10,
    HeadCommit:strings.Repeat("a",40),Proof:[]factory.Claim{{Text:"works",OK:true}},Stream:&factory.Stream{Phases:[]factory.Phase{{Name:"proof",State:factory.PhaseDone}}},
   }
   if draft { it.PullRequest=&factory.PullRequest{Number:12,State:factory.PROpen,Draft:true,HeadSHA:it.HeadCommit,ChecksRead:true,ReviewsRead:true,Mergeable:factory.MergeableYes} }
   saved,err := floor.Create(it)
   if err != nil { t.Fatal(err) }
   var before api.Job
   s.must("factory.job",map[string]any{"factory":status.Factory,"job":saved.ID},&before)
   s.must("factory.job_stop",map[string]any{"factory":status.Factory,"job":saved.ID,"expect":map[string]any{"version":before.Version}},&appwire.FactoryJobActed{})
   after,err := floor.Get(saved.ID)
   if err != nil { t.Fatal(err) }
   if !factory.StoppedByPerson(after) || after.StopUndo == nil { t.Fatal("proof-only Stop has no durable record") }
   if err := factoryrun.ApprovalRefused(&after,false); !errors.Is(err,factory.ErrPersonStopped) { t.Fatalf("proof approval after Stop: %v",err) }
   if err := factory.ReadinessGate(after); !errors.Is(err,factory.ErrPersonStopped) { t.Fatalf("readiness after Stop: %v",err) }
   if factory.QueueCIFollowUp(&after,time.Now()) || factory.Automatic(after) { t.Fatal("draft PR resumed automatically") }
   var job api.Job
   s.must("factory.job",map[string]any{"factory":status.Factory,"job":saved.ID},&job)
   if job.State != api.StateNeedsYou || job.Can.PRReady || job.Can.Approve || job.Desk == nil || (job.Desk.PersonStopped == nil || !*job.Desk.PersonStopped) { t.Fatalf("wire lost Stop: %+v",job) }
  })
 }
}

func TestFactoryPersonStopLegacyFixtures(t *testing.T) {
 for _, name := range []string{"stop-undo","no-stop-evidence","stopped-failure","stopped-phase"} {
  t.Run(name,func(t *testing.T) {
   s,status := newPauseSetup(t)
   data,err := os.ReadFile(filepath.Join("testdata","person-stop",name+".json"))
   if err != nil { t.Fatal(err) }
   var legacy factory.Item
   if err := json.Unmarshal(data,&legacy); err != nil { t.Fatal(err) }
   it,err := s.backend.local.host.floor.Create(legacy)
   if err != nil { t.Fatal(err) }
   desk,err := lease.Open(s.backend.local.host.floor,lease.Options{})
   if err != nil { t.Fatal(err) }
   err = lease.NewDoors(desk).Launch(it.ID)
   if name == "no-stop-evidence" { if err != nil { t.Fatalf("guessed a Stop without evidence: %v",err) }; return }
   if !errors.Is(err,factory.ErrPersonStopped) { t.Fatalf("legacy Stop was ignored: %v",err) }
   var job api.Job
   s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&job)
   if job.State != api.StateNeedsYou || job.Desk == nil || (job.Desk.PersonStopped == nil || !*job.Desk.PersonStopped) { t.Fatalf("legacy wire lost Stop: %+v",job) }
  })
 }
}

func TestFactoryPersonStopInFlightWakeAndPersonChat(t *testing.T) {
 for _,mode:=range []string{"late_tools","retry_503","queued_person"} { t.Run(mode,func(t *testing.T){
 s,status,it := stoppedAppJob(t,factory.StateNeedsYou)
 var stopped api.Job
 s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&stopped)
 s.must("factory.job_resume",map[string]any{"factory":status.Factory,"job":it.ID,"expect":map[string]any{"version":stopped.Version}},&appwire.FactoryJobActed{})
 floor := s.backend.local.host.floor
 if _,err := s.backend.local.host.seam.Talk(context.Background(),it.ID); err != nil { t.Fatal(err) }
 it,err := floor.Get(it.ID)
 if err != nil { t.Fatal(err) }
 entered,release := make(chan struct{}),make(chan struct{})
 personEntered,personRelease := make(chan struct{}),make(chan struct{})
 var personOnce sync.Once
 unblockPerson:=func(){personOnce.Do(func(){close(personRelease)})}
 var once sync.Once
 unblock := func(){once.Do(func(){close(release)})}
 var calls atomic.Int32
 model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method != http.MethodPost || r.URL.Path == "" { http.Error(w,"no catalog",http.StatusServiceUnavailable); return }
  var req recordedRequest
  if err := json.NewDecoder(r.Body).Decode(&req); err != nil { t.Error(err); return }
  n:=calls.Add(1)
  firstPerson:=int32(2);if mode=="queued_person"{firstPerson=3}
  if n==firstPerson { close(personEntered); <-personRelease }
  delta,finish := map[string]any{"content":"this job is stopped; a person presses Start"},"stop"
  if n==1 {
   close(entered)
   <-release
   if mode=="retry_503"{http.Error(w,"temporary upstream failure",http.StatusServiceUnavailable);return}
   delta=map[string]any{"tool_calls":[]any{map[string]any{"index":0,"id":"late-start","type":"function","function":map[string]string{"name":"factory_start","arguments":"{}"}}}}
   finish="tool_calls"
  }
  chunk,_:=json.Marshal(map[string]any{"choices":[]any{map[string]any{"index":0,"delta":delta}}})
  w.Header().Set("Content-Type","text/event-stream")
  fmt.Fprintf(w,"data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n",chunk,finish)
 }))
 t.Cleanup(model.Close)
 t.Cleanup(unblock)
 t.Cleanup(unblockPerson)
 agent,err:=session.New(session.Config{Workspace:mustCheckout(t,floor),SessionFile:it.Talk,ProfileDir:s.backend.localProfile(),Model:"test/model",APIKey:"test-key",BaseURL:model.URL,OneModel:true,DeterministicNames:true,FactoryRun:runDoor(floor,mustCheckout(t,floor),s.backend.localProfile())})
 if err != nil { t.Fatal(err) }
 t.Cleanup(func(){unblock(); unblockPerson(); _ = agent.Close()})
 wakes,stopWatching:=agent.WatchWakes()
 defer stopWatching()
 events,err:=agent.SubmitRunnerNoteThrough(context.Background(),"The team question arrived; continue the job.",runDoor(floor,mustCheckout(t,floor),s.backend.localProfile()))
 if err != nil { t.Fatal(err) }
 select { case <-entered: case <-time.After(10*time.Second): t.Fatal("fake model did not enter the manager wake") }
 var before api.Job
 s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&before)
 s.must("factory.job_stop",map[string]any{"factory":status.Factory,"job":it.ID,"expect":map[string]any{"version":before.Version}},&appwire.FactoryJobActed{})
 var queued <-chan session.Event
 if mode=="queued_person" { queued,err=agent.Submit(context.Background(),"Why did this stop? Answer without doing any work.");if err!=nil{t.Fatal(err)} }
 unblock()
 var words strings.Builder
 for event:=range events { words.WriteString(event.Text) }
 if mode=="queued_person" { for range queued{};select{case answer:=<-wakes:for range answer{};case <-time.After(10*time.Second):t.Fatal("person message queued during automatic Stop was unanswered")} }
 wantCalls:=int32(1);if mode=="queued_person"{wantCalls=2}
 if calls.Load()!=wantCalls { t.Errorf("in-flight wake bought %d calls; want %d including person answers",calls.Load(),wantCalls) }
 if !strings.Contains(words.String(),"this job is stopped") || !strings.Contains(words.String(),"a person presses Start") { t.Errorf("manager did not explain Stop: %q",words.String()) }
 saved,err:=floor.Get(it.ID)
 if err != nil { t.Fatal(err) }
 if !factory.StoppedByPerson(saved) || saved.State!=factory.StateNew { t.Fatalf("late tool restarted job: %s",saved.State) }
 beforeCalls:=calls.Load()
 reply,err:=agent.Submit(context.Background(),"Explain what you need from me, without doing any work.")
 if err != nil { t.Fatal(err) }
 select {case <-personEntered: case <-time.After(10*time.Second): t.Fatal("person chat did not enter fake model") }
 steered,err:=agent.Submit(context.Background(),"Also explain which Start button I should press; do not start anything.")
 if err!=nil{t.Fatal(err)}
 unblockPerson()
 words.Reset()
 for event:=range reply { words.WriteString(event.Text) }
 if calls.Load()<beforeCalls+1 || !strings.Contains(words.String(),"this job is stopped") { t.Fatalf("person chat did not reach manager: calls=%d text=%q",calls.Load()-beforeCalls,words.String()) }
 for range steered {}
 select { case answer:=<-wakes: for event:=range answer { words.WriteString(event.Text) }; case <-time.After(10*time.Second): t.Fatal("person message during a response was left unanswered") }
 if calls.Load()!=beforeCalls+2 {t.Fatalf("two person messages bought %d calls; want two",calls.Load()-beforeCalls)}
 saved,err=floor.Get(it.ID)
 if err!=nil || !factory.StoppedByPerson(saved) || saved.State!=factory.StateNew { t.Fatalf("person chat changed execution: %s %v",saved.State,err) }
 }) }
}

func mustCheckout(t *testing.T, floor *store.Store) string {
 t.Helper()
 dirs,err:=floor.Checkouts()
 if err!=nil {t.Fatal(err)}
 return dirs["acme/api"]
}

func TestFactoryPersonStopGitHubSyncAndIntake(t *testing.T) {
 s,status,it:=stoppedAppJob(t,factory.StateNeedsYou)
 floor:=s.backend.local.host.floor
 pollStoppedJob(t,floor,it.ID)
 var job api.Job
 s.must("factory.job",map[string]any{"factory":status.Factory,"job":it.ID},&job)
 if job.State!=api.StateNeedsYou||job.Desk==nil||(job.Desk.PersonStopped == nil || !*job.Desk.PersonStopped)||!job.Can.StartNow||job.Can.Answer||job.Can.Approve||job.Can.PRReady{t.Fatalf("GitHub sync erased or hid Stop: %+v",job)}
 history,err:=record.Open(floor,record.Options{})
 if err!=nil{t.Fatal(err)}
 file,err:=pipeline.Parse("default_budget: 10\nstart:\n  labels: [agent-ready]\n")
 if err!=nil{t.Fatal(err)}
 var calls atomic.Int32
 picker:=intake.New(floor,intake.Options{Record:history,Load:func(string)(pipeline.File,error){calls.Add(1);return file,nil},Launch:func(int)error{calls.Add(1);return nil}})
 if err:=picker.Once(context.Background());err!=nil{t.Fatal(err)}
 if calls.Load()!=0{t.Fatalf("GitHub sync restarted stopped intake: %d calls",calls.Load())}
 desk,err:=lease.Open(floor,lease.Options{})
 if err!=nil{t.Fatal(err)}
 if _,err:=desk.Pull(lease.Worker{ID:"poll",Handle:"owner",Solo:true});!errors.Is(err,lease.ErrNoJob){t.Fatalf("GitHub poll made stopped job runnable: %v",err)}
}

// pollStoppedJob drives the real GitHub source and PR follow-up reconciliation.
func pollStoppedJob(t *testing.T,floor *store.Store,id int){
 t.Helper()
 oldHead,newHead:=strings.Repeat("a",40),strings.Repeat("b",40)
 if err:=floor.SetRepos([]string{"acme/api"});err!=nil{t.Fatal(err)}
 if err:=floor.Update(id,func(cur *factory.Item)error{
  cur.Origin=factory.OriginForge;cur.Num=128;cur.URL="https://github.com/acme/api/issues/128"
  cur.Publish="pr";cur.HeadCommit=oldHead
  cur.PullRequest=&factory.PullRequest{Number:12,State:factory.PROpen,Draft:true,HeadSHA:oldHead}
  cur.FollowUp=&factory.PRFollowUp{Kind:"pr",State:"watching",HeadSHA:oldHead}
  return nil
 });err!=nil{t.Fatal(err)}
 var pullReads atomic.Int32
 github:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.Method!=http.MethodGet {t.Errorf("stopped poll wrote %s %s",r.Method,r.URL.Path);w.WriteHeader(403);return}
  switch {
  case r.URL.Path=="/user":fmt.Fprint(w,`{"login":"owner"}`)
  case r.URL.Path=="/repos/acme/api/issues":fmt.Fprint(w,`[{"id":100128,"number":128,"state":"open","title":"Changed on GitHub","labels":[{"name":"agent-ready"},{"name":"size:S"}],"user":{"login":"owner"}}]`)
  case r.URL.Path=="/repos/acme/api/issues/128":fmt.Fprint(w,`{"id":100128,"number":128,"state":"open"}`)
  case strings.HasSuffix(r.URL.Path,"/permission"):fmt.Fprint(w,`{"permission":"admin"}`)
  case r.URL.Path=="/repos/acme/api/pulls/12":pullReads.Add(1);fmt.Fprintf(w,`{"number":12,"state":"open","draft":true,"mergeable":true,"head":{"sha":%q},"base":{"ref":"main"}}`,newHead)
  case strings.Contains(r.URL.Path,"/check-runs"):fmt.Fprint(w,`{"total_count":1,"check_runs":[{"name":"test","status":"completed","conclusion":"failure"}]}`)
  case strings.HasSuffix(r.URL.Path,"/status"):fmt.Fprint(w,`{"state":"failure","statuses":[]}`)
  case strings.Contains(r.URL.Path,"/protection"):w.WriteHeader(404)
  default:fmt.Fprint(w,`[]`)
  }
 }))
 defer github.Close()
 source:=factorygithub.New(forge.NewClient("test-key").WithBaseURL(github.URL),[]string{"acme/api"},time.Now)
 if _,err:=factorygithub.PollOnce(context.Background(),source,floor,"",time.Now);err!=nil{t.Fatal(err)}
 saved,err:=floor.Get(id)
 if err!=nil||pullReads.Load()==0||saved.PullRequest==nil||saved.PullRequest.HeadSHA!=newHead{t.Fatalf("GitHub fixture did not sync new commits: reads=%d job=%+v err=%v",pullReads.Load(),saved.PullRequest,err)}
 if !factory.StoppedByPerson(saved)||saved.State!=factory.StateNew||saved.FollowUp.State=="queued"{t.Fatalf("new commits revived Stop: state=%s follow=%+v",saved.State,saved.FollowUp)}
}
