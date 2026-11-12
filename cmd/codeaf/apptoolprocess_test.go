package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appProcessJournal writes the same result-presentation seam the real session
// tests exercise. The app reads it through agents.entries, including after its
// live call cache is discarded; no display entries are supplied to the reader.
func appProcessJournal(t *testing.T, call, command string, process *session.ToolProcess) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	args, _ := json.Marshal(map[string]string{"command": command})
	lines := []any{
		map[string]any{"type": "session", "version": 1, "id": "process-history", "cwd": ".", "model": "test/model"},
		map[string]any{"type": "message", "role": "user", "content": "run the command"},
		map[string]any{"type": "message", "role": "assistant", "toolCalls": []ai.ToolCall{{ID: call, Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: string(args)}}}},
		map[string]any{"type": "message", "role": "tool", "toolCallId": call, "content": "still running as job 3; log at logs/3.log", "presentation": struct {
			Audience string               `json:"audience"`
			Invoked  bool                 `json:"invoked"`
			Process  *session.ToolProcess `json:"tool_process,omitempty"`
		}{"tool", true, process}},
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		if err := json.NewEncoder(file).Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// E1, E2 and E4: the app's real turn pump, process list, process.changed and
// disk history reader agree on the session-owned identity for every E2 road.
// toolprocess_test.go in session exercises those producers with real commands;
// this transport fixture deliberately streams only the command's tail, so no
// exported lead is available in the live end row's detail or output file.
func TestAppToolProcessE2LiveAndRereadMatchEveryJobRoadsProcessList(t *testing.T) {
	for _, road := range []string{"background", "clock", "timeout", "steer", "declined_keep", "approval_then_clock"} {
		t.Run(road, func(t *testing.T) {
			base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
			started := base
			if road == "approval_then_clock" {
				started = base.Add(2 * time.Minute)
			}
			const command = "run-the-command"
			job := session.JobNotice{ID: 3, Command: command, Kind: session.JobKindCommand, State: session.JobRunning, Started: started}
			became := &session.ToolProcess{Job: job.ID, Started: job.Started}
			conv := &fakeConversation{jobs: []session.JobNotice{job}, script: []session.Event{
				{Kind: session.EventToolBegin, CallID: "process-call", Tool: "bash", Args: `{"command":"run-the-command"}`},
				{Kind: session.EventToolOutput, CallID: "process-call", Tool: "bash", Text: "only the streamed tail\n"},
				{Kind: session.EventToolFinished, CallID: "process-call", Tool: "bash", Took: time.Millisecond},
				{Kind: session.EventToolEnd, CallID: "process-call", Tool: "bash", ToolInvoked: true, ToolProcess: became},
				{Kind: session.EventTurnDone},
			}}
			var mu sync.Mutex
			var opens, releases int
			rec := newAppRecorder()
			backend := newFakeBackend(rec, fakeOpener(conv, &opens, &releases, &mu))
			backend.now = func() time.Time { return base }
			t.Cleanup(func() { backend.Close() })
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			a, _ := backend.find(agent.ID)
			journal := appProcessJournal(t, "process-call", command, became)
			backend.locked(func() { a.transcript = journal })
			conv.lane(session.Event{Kind: session.EventJobUpdate, Job: &job})
			if _, err := backend.Send(agent.ID, "run the command", ""); err != nil {
				t.Fatal(err)
			}
			begin := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "process-call" && row.Phase == appwire.PhaseBegin })
			end := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == "process-call" && row.Phase == appwire.PhaseEnd })
			rec.waitFor(t, 3*time.Second, appTurnDone)
			list, err := backend.ListProcesses(agent.ID)
			if err != nil || len(list) != 1 {
				t.Fatalf("processes.list = %+v, %v", list, err)
			}
			if end.Process != list[0].ID || end.Process != appProcessID(agent.ID, job) || begin.Process != "" || end.Detail != "" {
				t.Fatalf("begin = %+v; end = %+v; list = %+v", begin, end, list)
			}
			if road == "approval_then_clock" && (end.EndedAt == nil || job.Started.Sub(*end.EndedAt) <= time.Second) {
				t.Fatalf("E4 fixture did not separate the execution clock from approval: end = %+v, job = %+v", end, job)
			}
			rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
				changed, ok := data.(appwire.ProcessChanged)
				return event == appwire.EventProcessChanged && ok && changed.Process.ID == end.Process
			})
			for _, cold := range []bool{false, true} {
				if cold {
					backend.locked(func() { a.calls = nil })
				}
				entries, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries {
					if entry.Kind == appwire.EntryTool && entry.Call == "process-call" {
						found = true
						if entry.Process != end.Process {
							t.Fatalf("cold=%v history = %+v, live = %+v", cold, entry, end)
						}
					}
				}
				if !found {
					t.Fatal("agents.entries omitted the tool call")
				}
			}
		})
	}
}

// E2: a kept service uses its published service Process id. An app with no
// supervisor publishing that row has no such process to promise.
func TestAppToolProcessE2KeptServiceNamesOnlyItsPublishedProcess(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	became := &session.ToolProcess{Service: row.ID}
	a, _ := w.backend.find(w.agent.ID)
	journal := appProcessJournal(t, "service-call", row.Command, became)
	w.backend.locked(func() { a.transcript = journal })
	conv := w.backend.liveOf(a).conn.Conv.(*fakeConversation)
	conv.mu.Lock()
	conv.script = []session.Event{
		{Kind: session.EventToolBegin, CallID: "service-call", Tool: "bash"},
		{Kind: session.EventToolEnd, CallID: "service-call", Tool: "bash", ToolInvoked: true, ToolProcess: became},
		{Kind: session.EventTurnDone},
	}
	conv.mu.Unlock()
	if _, err := w.backend.Send(w.agent.ID, "keep the command", ""); err != nil {
		t.Fatal(err)
	}
	end := toolRow(w.rec, t, func(row appwire.TurnTool) bool { return row.Call == "service-call" && row.Phase == appwire.PhaseEnd })
	w.rec.waitFor(t, 3*time.Second, appTurnDone)
	list, err := w.backend.ListProcesses(w.agent.ID)
	if err != nil || len(list) != 1 || list[0].ID != end.Process || end.Process != w.agent.ID+":"+row.ID {
		t.Fatalf("service end = %+v, list = %+v, %v", end, list, err)
	}
	entries, _, err := w.backend.Entries(w.agent.ID, "", appwire.MaxEntries)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Kind == appwire.EntryTool {
			found = true
			if entry.Process != end.Process {
				t.Fatalf("service history = %+v, live = %+v", entry, end)
			}
		}
	}
	if !found {
		t.Fatal("the service's tool entry is missing")
	}
	w.rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		changed, ok := data.(appwire.ProcessChanged)
		return event == appwire.EventProcessChanged && ok && changed.Process.ID == end.Process
	})
	other, _, _ := workBackend(t, &fakeConversation{})
	if got := other.appToolProcessID(w.agent.ID, became); got != "" {
		t.Fatalf("an unpublished service acquired process %q", got)
	}
}

// E3 and E5: even an exact exported lead plus a matching process store row
// cannot manufacture the missing typed fact on live or older history records.
func TestAppToolProcessE3NonJobsAndE5OlderRecordsOmitProcess(t *testing.T) {
	for _, kind := range []string{"foreground", "failed", "refused", "rejected", "interrupted"} {
		t.Run(kind, func(t *testing.T) {
			end := session.Event{Kind: session.EventToolEnd, CallID: "ordinary", Tool: "bash", Output: "still running as job 3; log at logs/3.log", ToolInvoked: true}
			if kind != "foreground" {
				end.Kind = session.EventToolFailed
			}
			if kind == "refused" {
				end.HarnessMade, end.Refused = true, true
			}
			if kind == "rejected" {
				end.RejectedBeforeRun = true
			}
			job := session.JobNotice{ID: 3, Command: "run-the-command", Kind: session.JobKindCommand, State: session.JobRunning, Started: time.Now()}
			if kind != "foreground" {
				// A failed end cannot claim a process even if a peer supplies one.
				end.ToolProcess = &session.ToolProcess{Job: job.ID, Started: job.Started}
			}
			conv := &fakeConversation{jobs: []session.JobNotice{job}, script: []session.Event{
				{Kind: session.EventToolBegin, CallID: end.CallID, Tool: "bash", Args: `{"command":"run-the-command"}`}, end, {Kind: session.EventTurnDone},
			}}
			backend, rec, agent := workBackend(t, conv)
			a, _ := backend.find(agent.ID)
			journal := appProcessJournal(t, end.CallID, job.Command, nil)
			backend.locked(func() { a.transcript = journal })
			if _, err := backend.Send(agent.ID, "run it", ""); err != nil {
				t.Fatal(err)
			}
			row := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Call == end.CallID && row.Phase == appwire.PhaseEnd })
			rec.waitFor(t, 3*time.Second, appTurnDone)
			if row.Process != "" {
				t.Fatalf("a non-job end named a process: %+v", row)
			}
			entries, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Process != "" {
					t.Fatalf("older history invented a process: %+v", entry)
				}
			}
		})
	}
}

// R2, E2 and E5: the real pump must start fresh when a later turn reuses an
// ended provider id. Repeated begins of the still-running call keep its tail.
func TestAppToolProcessReusedIDStartsFreshAcrossPumpTurns(t *testing.T) {
	for _, nextJob := range []bool{false, true} {
		name := "foreground"
		if nextJob {
			name = "new_job"
		}
		t.Run(name, func(t *testing.T) {
			base := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
			first := &session.ToolProcess{Job: 3, Started: base}
			conv := &fakeConversation{script: []session.Event{
				{Kind: session.EventToolBegin, CallID: "same", Tool: "bash", Args: `{"command":"first-command"}`},
				{Kind: session.EventToolOutput, CallID: "same", Text: "first output\n"},
				{Kind: session.EventToolEnd, CallID: "same", Tool: "bash", ToolInvoked: true, ToolProcess: first},
				{Kind: session.EventTurnDone},
			}}
			backend, rec, agent := workBackend(t, conv)
			backend.now = func() time.Time { return base }
			turn, err := backend.Send(agent.ID, "first", "")
			if err != nil {
				t.Fatal(err)
			}
			appTurnDoneFor(t, rec, turn)
			end := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Turn == turn && row.Phase == appwire.PhaseEnd })
			if end.Process != appProcessID(agent.ID, session.JobNotice{ID: first.Job, Started: first.Started}) {
				t.Fatalf("the first turn never became its job: %+v", end)
			}
			var second *session.ToolProcess
			if nextJob {
				second = &session.ToolProcess{Job: 4, Started: base.Add(time.Minute)}
			}
			conv.mu.Lock()
			conv.script = []session.Event{
				{Kind: session.EventToolBegin, CallID: "same", Tool: "bash", Args: `{"command":"second-command"}`},
				{Kind: session.EventToolOutput, CallID: "same", Text: "second output\n"},
				{Kind: session.EventToolBegin, CallID: "same", Tool: "bash", Args: `{"command":"second-command"}`},
				{Kind: session.EventToolEnd, CallID: "same", Tool: "bash", ToolInvoked: true, ToolProcess: second},
				{Kind: session.EventTurnDone},
			}
			conv.mu.Unlock()
			turn = appSendWithWhenIdle(t, backend, agent.ID, "second", "", nil)
			begin := toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Turn == turn && row.Call == "same" })
			if begin.Phase != appwire.PhaseBegin || begin.Process != "" || begin.OK != nil || begin.EndedAt != nil || begin.ExitCode != nil || len(begin.Tail) != 0 || begin.Lines != nil {
				t.Fatalf("the second turn inherited the ended call: %+v", begin)
			}
			appTurnDoneFor(t, rec, turn)
			end = toolRow(rec, t, func(row appwire.TurnTool) bool { return row.Turn == turn && row.Phase == appwire.PhaseEnd })
			want := ""
			if second != nil {
				want = appProcessID(agent.ID, session.JobNotice{ID: second.Job, Started: second.Started})
			}
			if end.Process != want || len(end.Tail) != 1 || end.Tail[0] != "second output" || end.Lines == nil || *end.Lines != 1 {
				t.Fatalf("the second end did not keep only its own facts: %+v, want process %q", end, want)
			}
			repeated := toolRow(rec, t, func(row appwire.TurnTool) bool {
				return row.Turn == turn && row.Phase == appwire.PhaseBegin && len(row.Tail) == 1
			})
			if repeated.Process != "" || repeated.Tail[0] != "second output" || repeated.StartedAt == nil || begin.StartedAt == nil || !repeated.StartedAt.Equal(*begin.StartedAt) {
				t.Fatalf("a repeated live begin discarded its own call: %+v", repeated)
			}
			a, _ := backend.find(agent.ID)
			log := backend.callLog(a)
			log.locked(func() {
				if len(log.order) != 1 || log.order[0] != "same" {
					t.Errorf("reusing a call duplicated its retention slot: %v", log.order)
				}
			})
		})
	}
}

// E4: the cache's last provider id never lends its process to another result
// occurrence, whether both occurrences are on one page or paged separately.
func TestAppToolProcessE4ReusedCallIDsDoNotLendProcessesAcrossHistory(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	a, _ := backend.find(agent.ID)
	job := &session.ToolProcess{Job: 3, Started: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	backend.callEnded(a, agent.ID, 1, session.Event{Kind: session.EventToolEnd, CallID: "same", Tool: "bash", ToolProcess: job}, "run", "", "", true)
	record := session.Record{Entries: []session.DisplayEntry{
		{Role: "user", Text: "first"},
		{Role: "tool", Tool: "bash", CallID: "same", Answered: true, ToolProcess: job},
		{Role: "user", Text: "second"},
		{Role: "tool", Tool: "bash", CallID: "same", Answered: true},
	}}
	for _, limit := range []int{appwire.MaxEntries, 2} {
		before := ""
		var tools []appwire.Entry
		for {
			page, more, err := appEntryPage(agent.ID, record, time.Time{}, nil, before, limit, func(item session.DisplayEntry) appwire.ToolFacts { return backend.appEntryFacts(a, item) })
			if err != nil {
				t.Fatal(err)
			}
			var calls []appwire.Entry
			for _, entry := range page {
				if entry.Kind == appwire.EntryTool {
					calls = append(calls, entry)
				}
			}
			tools = append(calls, tools...)
			if !more {
				break
			}
			before = page[0].ID
		}
		if len(tools) != 2 || tools[0].Process == "" || tools[1].Process != "" {
			t.Fatalf("limit=%d same-id facts = %+v", limit, tools)
		}
	}
}

// R3 and E5: paging keeps every existing fact from the last same-id item;
// only Process is overlaid from each row's own source occurrence.
func TestAppToolProcessPagingPreservesLegacyFactsForReusedIDs(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	a, _ := backend.find(agent.ID)
	job := &session.ToolProcess{Job: 3, Started: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)}
	for _, linked := range []bool{false, true} {
		name := "legacy"
		if linked {
			name = "with_process"
		}
		t.Run(name, func(t *testing.T) {
			record := session.Record{Entries: []session.DisplayEntry{
				{Role: "user", Text: "first"},
				{Role: "tool", Tool: "bash", CallID: "same", Answered: true, Args: `{"command":"first-command","timeout":12}`, Output: "first output\nCommand exited with code 1", ToolResult: session.ToolResultInvoked},
				{Role: "user", Text: "second"},
				{Role: "tool", Tool: "bash", CallID: "same", Answered: true, Args: `{"command":"last-command","timeout":34}`, Output: "last output\nCommand exited with code 7", ToolResult: session.ToolResultRejectedBeforeRun | session.ToolResultHarnessMade | session.ToolResultRefused | session.ToolResultMediaAttempt},
			}}
			if linked {
				record.Entries[1].ToolProcess = job
			}
			legacy := backend.appEntryFacts(a, record.Entries[3])
			if legacy.ExitCode == nil || *legacy.ExitCode != 7 || legacy.Input == nil || legacy.Input.Text != "last-command" || len(legacy.Tail) != 2 || legacy.Tail[0] != "last output" || legacy.Invoked || !legacy.RejectedBeforeRun || !legacy.HarnessMade || !legacy.Refused || !legacy.MediaAttempt {
				t.Fatalf("the legacy source does not distinguish the occurrences: %+v", legacy)
			}
			for _, limit := range []int{appwire.MaxEntries, 2} {
				var got []appwire.Entry
				before := ""
				for {
					count := 0
					facts := func(item session.DisplayEntry) appwire.ToolFacts {
						count++
						return backend.appEntryFacts(a, item)
					}
					page, more, err := appEntryPage(agent.ID, record, time.Time{}, nil, before, limit, facts)
					if err != nil {
						t.Fatal(err)
					}
					tools := 0
					for _, entry := range page {
						if entry.Kind == appwire.EntryTool {
							tools++
							kept := entry.ToolFacts
							kept.Process = ""
							if !reflect.DeepEqual(kept, legacy) {
								t.Fatalf("limit=%d existing facts changed: got %+v, want %+v", limit, kept, legacy)
							}
						}
					}
					if count != tools {
						t.Fatalf("read %d facts for %d visible calls", count, tools)
					}
					got = append(page, got...)
					if !more {
						break
					}
					before = page[0].ID
				}
				want := appEntriesOf(agent.ID, record.Entries, time.Time{}, nil)
				want[1].ToolFacts, want[3].ToolFacts = legacy, legacy
				if linked {
					want[1].Process = appProcessID(agent.ID, session.JobNotice{ID: job.Job, Started: job.Started})
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("limit=%d paging changed legacy rows or borrowed a process: got %+v, want %+v", limit, got, want)
				}
			}
		})
	}
}

// E1 and E5: adding the optional field changes no no-job JSON bytes.
func TestAppToolProcessE5AbsentFieldPreservesExistingJSON(t *testing.T) {
	for _, c := range []struct {
		facts appwire.ToolFacts
		want  string
	}{{appwire.ToolFacts{}, `{}`}, {appwire.ToolFacts{Kind: appwire.ToolRun}, `{"tool_kind":"run"}`}} {
		got, err := json.Marshal(c.facts)
		if err != nil || string(got) != c.want {
			t.Fatalf("E5 JSON = %s, %v; want %s", got, err, c.want)
		}
	}
	for _, value := range []any{appwire.TurnTool{Phase: appwire.PhaseBegin}, appwire.Entry{Kind: appwire.EntryTool}} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), `"process"`) {
			t.Fatalf("E5 no-job row changed: %s, %v", raw, err)
		}
	}
	raw, err := json.Marshal(appwire.ToolFacts{Process: "a:p3.abc"})
	if err != nil || string(raw) != `{"process":"a:p3.abc"}` {
		t.Fatalf("E1 process field = %s, %v", raw, err)
	}
}
