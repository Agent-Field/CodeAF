package main

import (
 "encoding/json"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"

 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/verification"
)

func TestReviewLegacySuccessfulOutput(t *testing.T) {
 for _, ending := range []string{"Command aborted", "Command cancelled", "Command timed out after 1 seconds"} {
 t.Run(ending, func(t *testing.T) {
 root:=t.TempDir()
 bin:=filepath.Join(root,"bin")
 if err:=os.Mkdir(bin,0700);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(bin,"go"),[]byte("#!/bin/sh\nprintf 'PASS\\nok  example 1s\\n'\n"),0700);err!=nil {t.Fatal(err)}
 command:=`go test ./...; printf '\n` + ending + `\n'`
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
 })
 }
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


func TestBackgroundReceiptWaitsForMeasuredExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		measured bool
		failed bool
		promoted bool
	}{
		{name: "launch without exit"},
		{name: "promoted without exit", promoted: true},
		{name: "measured pass", measured: true},
		{name: "measured failure", measured: true, failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, file, run := repairCheckFixture(t)
			command := "cargo test --test cli regression"
			if tc.failed { command = "CHECK_EXIT=7 " + command }
			run(command)
			if !tc.measured {
				if err := os.RemoveAll(file + ".checks"); err != nil { t.Fatal(err) }
			}
			raw, err := os.ReadFile(file)
			if err != nil { t.Fatal(err) }
			var journal strings.Builder
			enc := json.NewEncoder(&journal)
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil { t.Fatal(err) }
				if row["role"] == "assistant" && !tc.promoted {
					call := row["toolCalls"].([]any)[0].(map[string]any)["function"].(map[string]any)
					var args map[string]any
					if err := json.Unmarshal([]byte(call["arguments"].(string)), &args); err != nil { t.Fatal(err) }
					args["background"] = true
					argJSON, err := json.Marshal(args)
					if err != nil { t.Fatal(err) }
					call["arguments"] = string(argJSON)
				}
				if row["role"] == "tool" {
					row["content"] = "PASS\n"
					row["presentation"] = map[string]any{"invoked": true, "outcome": "ok", "tool_process": map[string]any{"job": 1}}
				}
				if err := enc.Encode(row); err != nil { t.Fatal(err) }
			}
			if err := os.WriteFile(file, []byte(journal.String()), 0600); err != nil { t.Fatal(err) }
			notes, _ := (&stageConversation{file: file, dir: root}).ExecutionNotes()
			found := false
			for _, note := range notes {
				if strings.HasPrefix(note, "Command receipt: ") {
					found = true
					if !strings.Contains(note, `"outcome":"started"`) { t.Errorf("launch must stay started: %s", note) }
				}
			}
			if !found { t.Fatal("missing launch receipt") }
			door := &receiptStageDoor{StageDoor: &repairReportDoor{}, file: file, dir: root, requireFresh: true, requireAnyCheck: true}
			t.Cleanup(door.closeVerification)
			err = door.Report(t.Context(), factory.StageResult{Done: true})
			if wantHeld := !tc.measured || tc.failed; (err != nil) != wantHeld { t.Errorf("held=%v want %v: %v", err != nil, wantHeld, err) }
			body := factory.PublishBody(factory.Item{Title: "background validation", Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Result: &factory.StageResult{Notes: notes}}}}}, "")
			if published := strings.Contains(body, "Passed locally:"); published && (!tc.measured || tc.failed) { t.Errorf("published pass=%v: %s", published, body) }
		})
	}
}
