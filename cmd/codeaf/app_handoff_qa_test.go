package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// Observe the same row a second desktop uses to arm Move here, with a real
// background process and its owed follow-up on the first desktop.
func TestQAMoveWaitsForBackgroundJobAndItsReply(t *testing.T) {
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	const output = "handoff-job-complete"
	args, _ := json.Marshal(map[string]any{"command": "printf 'started\\n'; while [ ! -f release-job ]; do sleep 0.02; done; printf '" + output + "\\n'", "background": true, "notify": true})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools    []json.RawMessage `json:"tools"`
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		called, ended := false, false
		for _, m := range request.Messages {
			called = called || m.Role == "tool"
			raw, _ := json.Marshal(m.Content)
			ended = ended || strings.Contains(string(raw), "job 1 exited")
		}
		content, finish := `"content":"The full output will land here when it finishes."`, "stop"
		if len(request.Tools) > 0 && !called {
			content, finish = `"content":"Starting background work.","tool_calls":[{"index":0,"id":"handoff-job","type":"function","function":{"name":"bash","arguments":`+strconv.Quote(string(args))+`}}]`, "tool_calls"
		} else if ended {
			content = `"content":"The job finished: ` + output + `."`
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant",`+content+`},"finish_reason":"`+finish+`"}]}`+"\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Background job QA"},"finish_reason":"stop"}]}`)
		}
	}))
	defer model.Close()
	t.Setenv("CODEAF_BASE_URL", model.URL)
	_, _, _ = heldChatRelay(t, "fixture")
	dirA, dirB := outageHomes(t, os.Getenv(syncsetup.URLVar))
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "start a job and report its output")
	row := workingChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.CanTake && r.Title != "" && r.Seen.Head != "" && r.Working })
	processes := machineCall[appwire.ProcessesResult](t, a.w, "processes.list", map[string]any{"agent": created.ID}).Processes
	if len(processes) != 1 || processes[0].State != "running" {
		t.Fatalf("the fixture did not start its background job: %+v", processes)
	}
	sourceLog := machineCall[appwire.ProcessLogTargetResult](t, a.w, "processes.log_target", map[string]any{"id": processes[0].ID})
	if sourceLog.Path == "" || !strings.HasPrefix(sourceLog.Path, dirA+string(filepath.Separator)) {
		t.Fatal("the fixture's source log is not in its temporary home")
	}

	defer os.WriteFile(filepath.Join(workspace, "release-job"), nil, 0600)
	// Wait beyond final capture and the tail quiet window. A turn-only flag
	// drops here, even though the process and the promised reply are still out.
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		rows := machineCall[appwire.ChatsResult](t, b.w, "chats.list", struct{}{}).Chats
		for _, r := range rows {
			if r.Cell == row.Cell && !r.Working {
				t.Fatalf("Move here became ready while the background job was running; source processes: %+v", machineCall[appwire.ProcessesResult](t, a.w, "processes.list", map[string]any{"agent": created.ID}).Processes)
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(workspace, "release-job"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	a.wait(t, func(f e2Frame) bool {
		var e appwire.EntryAdded
		return f.Event == appwire.EventEntryAdded && json.Unmarshal(f.Data, &e) == nil && e.Entry.Kind == appwire.EntryAnswer && strings.Contains(e.Entry.Text, "The job finished:")
	})
	row = workingChatRow(t, b, row.Cell, func(r appwire.ChatRow) bool { return !r.Working && r.CanTake })
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	entries := machineCall[struct {
		Entries []appwire.Entry `json:"entries"`
	}](t, b.w, "agents.entries", map[string]any{"id": taken.ID}).Entries
	found := false
	for _, e := range entries {
		found = found || e.Kind == appwire.EntryAnswer && strings.Contains(e.Text, "The job finished: "+output)
	}
	if !found {
		t.Fatal("the moved chat lost the job's follow-up answer")
	}
	list := machineCall[appwire.ProcessesResult](t, b.w, "processes.list", map[string]any{"agent": taken.ID})
	if len(list.Processes) != 1 || list.Processes[0].State != "ended" || list.Processes[0].ExitCode == nil || *list.Processes[0].ExitCode != 0 {
		t.Fatalf("moved job = %+v", list.Processes)
	}
	process := list.Processes[0]
	// The two processes share this test machine. Hide the sender's file so a
	// stale absolute path cannot accidentally satisfy the receiver's read.
	if err := os.Rename(sourceLog.Path, sourceLog.Path+".sender-only"); err != nil {
		t.Fatal(err)
	}
	if process.EndedAt == nil || process.EndedAt.Sub(process.StartedAt) < time.Second {
		t.Fatal("the moved job lost its real elapsed time")
	}
	log := machineCall[appwire.LinesNextLineResult](t, b.w, "processes.output", map[string]any{"id": process.ID, "from_line": 0})
	if !strings.Contains(strings.Join(log.Lines, "\n"), output) {
		t.Fatal("the moved job lost its output log")
	}
	page := machineCall[appwire.ProcessPageResult](t, b.w, "processes.page", appwire.ProcessPageAsk{Agent: taken.ID})
	if len(page.Processes) != 1 || page.Processes[0].ID != process.ID {
		t.Fatalf("the moved process is missing from its paged history: %+v", page.Processes)
	}
	strict := machineCall[appwire.ProcessLogResult](t, b.w, "processes.log", appwire.ProcessLogAsk{ID: process.ID})
	if strict.LogGone || !strings.Contains(strict.Text, output) {
		t.Fatal("the moved managed output log is not readable")
	}

	workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return !r.Here && r.Seen.Device != row.Seen.Device })
	// This projection includes tool records too, using their saved facts,
	// without consulting the old computer's call-log cache.
	remote := machineCall[struct {
		Entries []appwire.Entry `json:"entries"`
	}](t, a.w, "agents.entries", map[string]any{"id": created.ID}).Entries
	tool := false
	for _, e := range remote {
		tool = tool || e.Kind == appwire.EntryTool && e.Name == "bash"
	}
	if !tool {
		t.Fatal("read-only following lost the saved command entry")
	}

}

func TestQAReadOnlyChatFollowsRemoteSavesWithoutTaking(t *testing.T) {
	celltest.RequireEngine(t)
	model, workspace := wakeEnvironment(t, e2TempDir(t))
	_ = model
	_, _, _ = heldChatRelay(t, "fixture")
	dirA, dirB := outageHomes(t, os.Getenv(syncsetup.URLVar))
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "first computer's words")
	e2Quit(t, a)
	row := workingChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.CanTake && r.Title != "" && !r.Working })
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Workspace == "" || taken.Workspace == os.Getenv("HOME") || taken.WorkspaceOrigin != workspace {
		t.Fatalf("received working folder = %q; origin = %q", taken.Workspace, taken.WorkspaceOrigin)
	}
	if _, err := os.Stat(taken.Workspace); err != nil {
		t.Fatal(err)
	}
	a = e2StartApp(t, dirA)
	old := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return !r.Here && r.Seen.Device != row.Seen.Device })
	// Reading the remote saved history must not replace the journal/tools root
	// that an eventual Move here still needs to reconcile.
	local := takeRootForAt(dirA, row.Cell)
	before, err := os.ReadFile(filepath.Join(local, ".cell", "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	e2Turn(t, b, taken.ID, "new words from the other computer")
	newest := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != old.Seen.Head && !r.Working })
	entries := machineCall[struct {
		Entries []appwire.Entry `json:"entries"`
	}](t, a.w, "agents.entries", map[string]any{"id": created.ID}).Entries
	found := false
	for _, e := range entries {
		found = found || e.Kind == appwire.EntryUser && e.Text == "new words from the other computer"
	}
	if !found {
		t.Fatal("the waiting computer still shows only its old local messages")
	}
	after, err := os.ReadFile(filepath.Join(local, ".cell", "transcript.jsonl"))
	if err != nil || string(before) != string(after) {
		t.Fatal("read-only following changed the local journal")
	}
	final := workingChatRow(t, a, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head == newest.Seen.Head })
	if final.Here || final.Seen.Device != newest.Seen.Device || final.Seen.Fence != newest.Seen.Fence {
		t.Fatal("reading took the chat away from its holder")
	}
}
