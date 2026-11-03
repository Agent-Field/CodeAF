package main

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
)

func TestReviewLegacySuccessfulOutput(t *testing.T) {
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(bin,"go"),[]byte("#!/bin/sh\nprintf 'PASS\\nok  example 1s\\n'\n"),0700);err!=nil {t.Fatal(err)}
 command:=`go test ./...; printf '\nCommand aborted\n'`
 run:=exec.Command("bash","-c",command)
 run.Env=append(os.Environ(),"PATH="+bin+":"+os.Getenv("PATH"))
 output,err:=run.CombinedOutput()
 if err!=nil {t.Fatal(err)}
 file:=filepath.Join(root,"journal.jsonl")
 f,err:=os.Create(file);if err!=nil {t.Fatal(err)}
 enc:=json.NewEncoder(f)
 args,_:=json.Marshal(map[string]string{"command":command})
 enc.Encode(map[string]any{"role":"assistant","toolCalls":[]any{map[string]any{"id":"a","function":map[string]string{"name":"bash","arguments":string(args)}}}})
 enc.Encode(map[string]any{"role":"tool","toolCallId":"a","content":string(output),"presentation":map[string]any{"audience":"tool","invoked":true}})
 f.Close()
 notes,_:=(&stageConversation{file:file,dir:root}).ExecutionNotes()
 for _,note:=range notes {
  if strings.HasPrefix(note,"Command receipt: ") && !strings.Contains(note,`"outcome":"passed"`) {t.Errorf("exit=0 old journal: want passed, got %s",note)}
 }
 door:=&receiptStageDoor{StageDoor:&repairReportDoor{},file:file,dir:root,requireFresh:true}
 defer door.closeVerification()
 if err:=door.Report(t.Context(),factory.StageResult{Done:true});err!=nil {t.Errorf("successful old journal held: %v",err)}
}

func TestReviewBackgroundValidation(t *testing.T) {
 root,file,run:=repairCheckFixture(t)
 command:="cargo test --test cli regression && false"
 run(command)
 raw,err:=os.ReadFile(file);if err!=nil{t.Fatal(err)}
 var rows []map[string]any
 for _,line:=range strings.Split(strings.TrimSpace(string(raw)),"\n") {
  var row map[string]any;if err:=json.Unmarshal([]byte(line),&row);err!=nil{t.Fatal(err)}
  if row["role"]=="tool" {row["content"]="job 1 started; log at /tmp/job.log";row["presentation"]=map[string]any{"invoked":true,"outcome":"ok"}}
  rows=append(rows,row)
 }
 f,err:=os.Create(file);if err!=nil{t.Fatal(err)};enc:=json.NewEncoder(f);for _,row:=range rows{enc.Encode(row)};f.Close()
 notes,_:=(&stageConversation{file:file,dir:root}).ExecutionNotes()
 blocked:=false;for _,note:=range notes{blocked=blocked||strings.HasPrefix(note,verification.FailurePrefix)}
 if !blocked{t.Errorf("real background compound exit=1 did not reach guard: %v",notes)}
 body:=factory.PublishBody(factory.Item{Title:"review",Stream:&factory.Stream{Phases:[]factory.Phase{{Name:"proof",Result:&factory.StageResult{Notes:notes}}}}},"")
 if strings.Contains(body,"Passed locally:"){t.Errorf("background exit=1 published as passed: %s",body)}
}

