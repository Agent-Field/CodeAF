package appapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type turnEngine struct {
	*fakeAgentEngine
	lock        sync.Mutex
	entries     []session.DisplayEntry
	usage       session.Usage
	streams     []chan session.Event
	steers      []string
	questions   []session.Question
	history     session.EarlierHistory
	startErr    error
	submitErr   error
	brief       string
	interrupted int
	files       []remote.WireFile
}

func (e *turnEngine) Submit(_ context.Context, text string) (<-chan session.Event, error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	if e.submitErr != nil {
		return nil, e.submitErr
	}
	ch := make(chan session.Event, 16)
	e.streams = append(e.streams, ch)
	e.entries = append(e.entries, session.DisplayEntry{Role: "user", Text: text})
	return ch, nil
}
func (e *turnEngine) SubmitFiles(ctx context.Context, text string, files []remote.WireFile, _ []session.Image) (<-chan session.Event, error) {
	e.files = files
	return e.Submit(ctx, text)
}
func (e *turnEngine) Steer(text string) (<-chan session.Event, error) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.steers = append(e.steers, text)
	ch := make(chan session.Event)
	close(ch)
	return ch, nil
}
func (e *turnEngine) Transcript() []session.DisplayEntry {
	e.lock.Lock()
	defer e.lock.Unlock()
	return append([]session.DisplayEntry{}, e.entries...)
}
func (e *turnEngine) Usage() session.Usage                   { return e.usage }
func (e *turnEngine) OpenQuestions() []session.Question      { return e.questions }
func (e *turnEngine) EarlierHistory() session.EarlierHistory { return e.history }
func (e *turnEngine) Interrupt()                             { e.interrupted++ }
func (e *turnEngine) StartTask(_ context.Context, brief string, solo bool) (uint64, string, string, error) {
	if !solo {
		return 0, "", "", errors.New("expected one task")
	}
	e.brief = brief
	if e.startErr != nil {
		return 0, "", "", e.startErr
	}
	return 17, "Continued work", "", nil
}
func (e *turnEngine) stream(i int) chan session.Event {
	e.lock.Lock()
	defer e.lock.Unlock()
	return e.streams[i]
}

type turnFixture struct {
	*agentFixture
	engine *turnEngine
	id     string
	timers []func()
}

func newTurnFixture(t *testing.T) *turnFixture {
	t.Helper()
	base := newAgentFixture(t)
	agent := readAgent(t, base.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	engine := &turnEngine{fakeAgentEngine: base.engines[0]}
	f := &turnFixture{agentFixture: base, engine: engine, id: agent.ID}
	base.registry.live[agent.ID].conn.Engine = engine
	base.registry.after = func(_ time.Duration, callback func()) *time.Timer { f.timers = append(f.timers, callback); return nil }
	return f
}
func (f *turnFixture) action(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, method, "/app/v1/agents/"+f.id+path, body)
}
func (f *turnFixture) snapshot(t *testing.T) AgentResponse {
	t.Helper()
	w := f.action(t, "GET", "", "")
	if w.Code != 200 {
		t.Fatalf("snapshot %d: %s", w.Code, w.Body)
	}
	var response AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
func turnStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("want %d got %d: %s", status, w.Code, w.Body)
	}
}
func turnWait(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-deadline.C:
			t.Fatal("turn did not settle")
		case <-tick.C:
		}
	}
}
func (f *turnFixture) waitState(t *testing.T, state string) {
	t.Helper()
	turnWait(t, func() bool { return f.snapshot(t).State == state })
}

func TestTurnSendSteerQueueEditRemoveAndDrain(t *testing.T) {
	f := newTurnFixture(t)
	send := f.action(t, "POST", "/turn", `{"text":"Investigate billing","mode":"send"}`)
	turnStatus(t, send, 200)
	var first TurnResponse
	json.Unmarshal(send.Body.Bytes(), &first)
	if !first.Accepted || first.TurnID == "" {
		t.Fatal("missing turn receipt")
	}
	snapshot := f.snapshot(t)
	if snapshot.Name != "Investigate billing" || snapshot.State != "running" {
		t.Fatalf("first send: %+v", snapshot.Agent)
	}
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"cannot send beside reply","mode":"send"}`), 409)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"Use the billing tests","mode":"steer"}`), 200)
	if len(f.engine.steers) != 1 {
		t.Fatal("steer did not reach engine")
	}
	for _, text := range []string{"follow up", "remove this"} {
		turnStatus(t, f.action(t, "POST", "/turn", fmt.Sprintf(`{"text":%q,"mode":"queue"}`, text)), 200)
	}
	queue := f.snapshot(t).Snapshot.Queue
	if len(queue) != 2 {
		t.Fatalf("queue %+v", queue)
	}
	turnStatus(t, f.action(t, "POST", "/queue/"+queue[0].ID, `{"text":"edited follow up"}`), 200)
	turnStatus(t, f.action(t, "DELETE", "/queue/"+queue[1].ID, `{}`), 200)
	f.engine.stream(0) <- session.Event{Kind: session.EventTurnDone}
	close(f.engine.stream(0))
	turnWait(t, func() bool { return len(f.snapshot(t).Snapshot.Queue) == 0 })
	entries := f.engine.Transcript()
	if len(entries) != 2 || entries[1].Text != "edited follow up" {
		t.Fatalf("drain: %+v", entries)
	}
	turnStatus(t, f.action(t, "POST", "/queue/"+queue[0].ID, `{"text":"too late"}`), 409)
	turnStatus(t, f.action(t, "DELETE", "/queue/"+queue[0].ID, `{}`), 409)
	f.engine.stream(1) <- session.Event{Kind: session.EventTurnDone}
	close(f.engine.stream(1))
	f.waitState(t, "idle")
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"too late","mode":"steer"}`), 409)
}

func TestTurnStopSuppressesLateEventsAndQueueAndKeepsReceipt(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"work","mode":"send"}`), 200)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"next","mode":"queue"}`), 200)
	turnStatus(t, f.action(t, "POST", "/stop", `{}`), 200)
	got := f.snapshot(t)
	if got.State != "idle" || len(got.Snapshot.Queue) != 0 || got.Snapshot.Entries[len(got.Snapshot.Entries)-1].Text != "Stopped by you" || f.engine.stop != 1 {
		t.Fatalf("stop: %+v", got)
	}
	f.timers[0]()
	f.engine.stream(0) <- session.Event{Kind: session.EventError, Err: errors.New("late")}
	close(f.engine.stream(0))
	// A new turn has a different generation, so even a late error cannot fail it.
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"fresh","mode":"send"}`), 200)
	if f.snapshot(t).State != "running" {
		t.Fatal("late completion replaced fresh turn")
	}
}

func TestTurnFailureRetryAndSwitchModelPreserveLastRequest(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/retry", `{}`), 409)
	turnStatus(t, f.action(t, "POST", "/switch-model", `{"model":"test/new"}`), 409)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"work","mode":"send"}`), 200)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"queued","mode":"queue"}`), 200)
	f.engine.stream(0) <- session.Event{Kind: session.EventError, Err: errors.New("provider 529")}
	close(f.engine.stream(0))
	f.waitState(t, "failed")
	if len(f.snapshot(t).Snapshot.Queue) != 1 {
		t.Fatal("failure lost pending message")
	}
	turnStatus(t, f.action(t, "POST", "/switch-model", `{"model":"test/new"}`), 200)
	if f.snapshot(t).Model != "test/new" {
		t.Fatal("switch did not reach engine")
	}
	turnStatus(t, f.action(t, "POST", "/retry", `{"expect":{"state":"failed"}}`), 200)
	entries := f.engine.Transcript()
	if len(entries) != 2 || entries[0].Text != entries[1].Text {
		t.Fatalf("retry changed request: %+v", entries)
	}
}

func TestTurnLongOnceAndMoveReplyToCanonicalTask(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"Finish billing migration","mode":"send"}`), 200)
	f.timers[0]()
	w := f.action(t, "POST", "/move-reply-to-task", `{}`)
	turnStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"task_id":"17"`) || !strings.Contains(f.engine.brief, "Finish billing migration") || f.engine.interrupted != 1 || f.engine.stop != 0 {
		t.Fatalf("move: %s, brief %s", w.Body, f.engine.brief)
	}
	got := f.snapshot(t)
	if got.State != "idle" || got.Receipt != "Moved to a task · the chat is free again" {
		t.Fatalf("moved: %+v", got)
	}
	turnStatus(t, f.action(t, "POST", "/move-reply-to-task", `{}`), 409)
	f.timers[0]()
}

func TestTurnFailedMoveRecordsTheStoppedReply(t *testing.T) {
	f := newTurnFixture(t)
	f.engine.startErr = errors.New("task could not start")
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"work","mode":"send"}`), 200)
	turnStatus(t, f.action(t, "POST", "/move-reply-to-task", `{}`), 409)
	got := f.snapshot(t)
	if got.State != "failed" || !strings.Contains(got.Receipt, "moving it to a task failed") {
		t.Fatalf("failed move: %+v", got)
	}
}

func TestTurnSnapshotCodecEntriesPaginationAndToolBoundary(t *testing.T) {
	f := newTurnFixture(t)
	for i := 0; i < 130; i++ {
		f.engine.entries = append(f.engine.entries, session.DisplayEntry{Role: "assistant", Text: fmt.Sprintf("entry %d", i)})
	}
	f.engine.entries = append(f.engine.entries, session.DisplayEntry{Role: "tool", Tool: "read", CallID: "call1", Output: "recorded output", Answered: true})
	f.engine.usage = session.Usage{Input: 17, Output: 8, CostUSD: 0.42, Turns: 3, Calls: 4}
	f.engine.tasks = []session.PlanTaskRow{{ID: "task1", Title: "done", Status: "done", Steps: 3}, {ID: "task2", Status: "claimed", Parent: "task1"}}
	f.engine.questions = []session.Question{{ID: 7, Kind: session.QuestionKind("consent"), Ask: session.AskPermission, Head: "Allow command?", Options: []session.AnswerOption{{Key: "1", Label: "Allow once"}}}}
	got := f.snapshot(t).Snapshot
	if len(got.Entries) != 100 || got.Usage.In != 17 || got.Usage.Cost.USD != "0.42" || got.Tasks.Progress.Done != 1 || got.Tasks.Progress.Total != 2 || len(got.OpenAsks) != 1 || got.OpenAsks[0].Kind != "permission" {
		t.Fatalf("snapshot: %+v", got)
	}
	w := f.action(t, "GET", "/entries?limit=25", "")
	turnStatus(t, w, 200)
	var page EntriesResponse
	json.Unmarshal(w.Body.Bytes(), &page)
	if !page.HasMore || len(page.Entries) != 25 || page.Before == "" {
		t.Fatalf("page: %+v", page)
	}
	older := f.action(t, "GET", "/entries?limit=25&before="+page.Before, "")
	turnStatus(t, older, 200)
	var previous EntriesResponse
	json.Unmarshal(older.Body.Bytes(), &previous)
	if previous.Entries[24].Text == page.Entries[0].Text {
		t.Fatal("pagination repeated boundary")
	}
	turnStatus(t, f.action(t, "GET", "/tools/call1", ""), 200)
	turnStatus(t, f.action(t, "GET", "/tools/unknown", ""), 404)
	for _, query := range []string{"limit=0", "limit=501", "limit=no", "before=entry_999", "before=forged"} {
		turnStatus(t, f.action(t, "GET", "/entries?"+query, ""), 422)
	}
	dir := filepath.Dir(f.registry.live[f.id].conn.SessionFile)
	root := filepath.Join(dir, "logs", "stubs")
	if e := os.MkdirAll(root, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "0123456789abcdef.txt")
	if e := os.WriteFile(path, []byte("full saved output"), 0600); e != nil {
		t.Fatal(e)
	}
	f.engine.entries[130].Output = "[tool: read · full: " + path + "]"
	w = f.action(t, "GET", "/tools/call1", "")
	turnStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"full":true`) || !strings.Contains(w.Body.String(), "full saved output") {
		t.Fatal(w.Body)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	f.engine.entries[130].Output = "[tool: read · full: " + outside + "]"
	turnStatus(t, f.action(t, "GET", "/tools/call1", ""), 409)
}

func TestTurnSnapshotShowsComposedRepliesNotRawToolResults(t *testing.T) {
	f := newTurnFixture(t)
	f.engine.entries = []session.DisplayEntry{
		{Role: "user", Text: "plot the stars"},
		{Role: "assistant", Text: ""},
		{Role: "tool", Tool: "bash", CallID: "call1", Output: "/bin/bash: gh: command not found", Answered: true},
		{Role: "tool", Text: "/bin/bash: gh: command not found\n\nCommand exited with code 1"},
		{Role: "assistant", Text: "  \n"},
		{Role: "aside", Text: "taking stock"},
		{Role: "assistant", Text: "Here is the chart."},
	}
	got := f.snapshot(t).Snapshot.Entries
	want := []struct{ kind, role, text string }{
		{"message", "user", "plot the stars"},
		{"tool", "tool", ""},
		{"notice", "aside", "taking stock"},
		{"message", "assistant", "Here is the chart."},
	}
	if len(got) != len(want) {
		t.Fatalf("entries: %+v", got)
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Role != w.role || got[i].Text != w.text || got[i].ID != fmt.Sprintf("entry_%d", i) {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestTurnValidationExpectationsAndEveryRouteGuard(t *testing.T) {
	f := newTurnFixture(t)
	for _, body := range []string{`{}`, `null`, `[]`, `{} {}`, `{"mode":"unknown","text":"x"}`, `{"mode":"send","text":null}`, `{"mode":"send","text":""}`, `{"mode":"send","text":"x","unexpected":1}`, `{"mode":"send","text":"x","expect":{"unknown":1}}`, `{"mode":"send","text":"x","context":[{"path":"relative","kind":"repo"}]}`, `{"mode":"send","text":"x","files":[{"path":"relative"}]}`} {
		turnStatus(t, f.action(t, "POST", "/turn", body), 422)
	}
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"x","expect":{"version":0}}`), 409)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"x","expect":{"state":"running"}}`), 409)
	for _, route := range turnContractRoutes {
		path := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(route.Path, "{id}", f.id), "{qid}", "missing"), "{call_id}", "missing")
		t.Run(route.Method+route.Path, func(t *testing.T) {
			denied := request(t, f.server, route.Method, path, `{}`, nil, "127.0.0.1:4000")
			turnStatus(t, denied, 401)
			wrongMethod := "PUT"
			if route.Method == wrongMethod {
				wrongMethod = "POST"
			}
			wrong := request(t, f.server, wrongMethod, path, `{}`, bearer(), "127.0.0.1:4000")
			turnStatus(t, wrong, 405)
			missingPath := strings.Replace(path, f.id, "missing", 1)
			missing := request(t, f.server, route.Method, missingPath, `{"mode":"send","text":"x","model":"test/model"}`, bearer(), "127.0.0.1:4000")
			if route.Request == "ExpectRequest" {
				missing = request(t, f.server, route.Method, missingPath, `{}`, bearer(), "127.0.0.1:4000")
			}
			if route.Request == "TextRequest" {
				missing = request(t, f.server, route.Method, missingPath, `{"text":"x"}`, bearer(), "127.0.0.1:4000")
			}
			if route.Request == "TurnRequest" {
				missing = request(t, f.server, route.Method, missingPath, `{"mode":"send","text":"x"}`, bearer(), "127.0.0.1:4000")
			}
			if route.Request == "ReorderQueueRequest" {
				missing = request(t, f.server, route.Method, missingPath, `{"order":[]}`, bearer(), "127.0.0.1:4000")
			}
			if route.Request == "ModelRequest" {
				missing = request(t, f.server, route.Method, missingPath, `{"model":"test/model"}`, bearer(), "127.0.0.1:4000")
			}
			turnStatus(t, missing, 404)
		})
	}
	turnStatus(t, f.action(t, "POST", "/switch-model", `{"model":""}`), 422)
	turnStatus(t, f.action(t, "POST", "/queue/missing", `{"text":""}`), 422)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"steer","text":"x","files":[{"path":"/tmp/file"}]}`), 409)
}

func TestTurnPOSTIdempotencyAndQueueSurviveRegistryRestart(t *testing.T) {
	f := newTurnFixture(t)
	body := `{"mode":"send","text":"work"}`
	h := bearer()
	h.Set("Idempotency-Key", "same")
	first := request(t, f.server, "POST", "/app/v1/agents/"+f.id+"/turn", body, h, "127.0.0.1:4000")
	again := request(t, f.server, "POST", "/app/v1/agents/"+f.id+"/turn", body, h, "127.0.0.1:4000")
	if first.Code != 200 || first.Body.String() != again.Body.String() || len(f.engine.Transcript()) != 1 {
		t.Fatalf("replayed send: %s %s", first.Body, again.Body)
	}
	turnStatus(t, request(t, f.server, "POST", "/app/v1/agents/"+f.id+"/turn", `{"mode":"send","text":"changed"}`, h, "127.0.0.1:4000"), 409)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"queue","text":"pending"}`), 200)
	f.registry.Close()
	restarted := NewAgentRegistry(f.home, nil)
	t.Cleanup(restarted.Close)
	s := New(testToken, Options{Agents: restarted})
	w := request(t, s, "GET", "/app/v1/agents/"+f.id, "", bearer(), "127.0.0.1:4000")
	turnStatus(t, w, 200)
	var got AgentResponse
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.State != "idle" || len(got.Snapshot.Queue) != 1 || got.Snapshot.Queue[0].Text != "pending" {
		t.Fatalf("restart: %+v", got)
	}
}

func TestTurnAttachmentsAndCompactedHistoryUseCanonicalDoors(t *testing.T) {
	f := newTurnFixture(t)
	file := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(file, []byte("attachment bytes"), 0600)
	turnStatus(t, f.action(t, "POST", "/turn", fmt.Sprintf(`{"mode":"send","files":[{"path":%q}]}`, file)), 200)
	if len(f.engine.files) != 1 || string(f.engine.files[0].Bytes) != "attachment bytes" || f.engine.files[0].Name != "notes.txt" {
		t.Fatalf("files: %+v", f.engine.files)
	}
	turnStatus(t, f.action(t, "POST", "/stop", `{}`), 200)
	f.engine.history = session.EarlierHistory{Entries: []session.DisplayEntry{{Role: "user", Text: "before compaction"}}, Floor: 1}
	w := f.action(t, "GET", "/entries", "")
	turnStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), "before compaction") {
		t.Fatal("earlier history omitted")
	}
}

func TestTurnEventsHaveAppPayloadsWithoutSteerDuplicates(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"work"}`), 200)
	f.engine.stream(0) <- session.Event{Kind: session.EventTextDelta, Text: "answer"}
	f.engine.stream(0) <- session.Event{Kind: session.EventReasoning, Text: "reasoning"}
	f.engine.stream(0) <- session.Event{Kind: session.EventToolEnd, Tool: "read", CallID: "call", Output: "result"}
	f.engine.stream(0) <- session.Event{Kind: session.EventTurnDone}
	close(f.engine.stream(0))
	f.waitState(t, "idle")
	records, _, _, _, _ := f.server.events.read(0)
	counts := map[EventType]int{}
	for _, record := range records {
		counts[record.kind]++
		if record.kind == EventTurnDelta {
			var delta TurnDeltaEvent
			if err := json.Unmarshal(record.data, &delta); err != nil || delta.Agent != f.id || delta.Turn == "" || delta.Text == "" {
				t.Fatalf("delta: %s %v", record.data, err)
			}
		}
		if record.kind == EventTurnEntry {
			var entry TurnEntryEvent
			json.Unmarshal(record.data, &entry)
			if entry.Entry.CallID != "call" || entry.Entry.Output != "result" || !entry.Entry.Answered {
				t.Fatalf("tool: %s", record.data)
			}
		}
	}
	if counts[EventTurnStarted] != 1 || counts[EventTurnEnded] != 1 || counts[EventTurnDelta] != 2 || counts[EventTurnEntry] != 1 {
		t.Fatalf("event counts: %v", counts)
	}
}

func TestTurnLongTimerIsOneShotAndReceiptStaysBetweenTurns(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"first"}`), 200)
	f.timers[0]()
	f.timers[0]()
	turnStatus(t, f.action(t, "POST", "/stop", `{}`), 200)
	turnStatus(t, f.action(t, "POST", "/turn", `{"mode":"send","text":"second"}`), 200)
	f.timers[0]()
	entries := f.snapshot(t).Snapshot.Entries
	if len(entries) != 3 || entries[0].Text != "first" || entries[1].Text != "Stopped by you" || entries[2].Text != "second" {
		t.Fatalf("receipt moved to wrong turn: %+v", entries)
	}
	records, _, _, _, _ := f.server.events.read(0)
	long := 0
	for _, record := range records {
		if record.kind == EventTurnLong {
			long++
			var event TurnLongEvent
			json.Unmarshal(record.data, &event)
			if event.AfterS != 180 || event.Agent != f.id {
				t.Fatalf("long: %s", record.data)
			}
		}
	}
	if long != 1 {
		t.Fatalf("long emitted %d times", long)
	}
}

func TestTurnToolOutputBoundsAndSymlinkEscapes(t *testing.T) {
	f := newTurnFixture(t)
	f.engine.entries = []session.DisplayEntry{{Role: "tool", Tool: "read", CallID: "bounded", Answered: true, Output: strings.Repeat("é", (1<<20)/2+1)}}
	w := f.action(t, "GET", "/tools/bounded", "")
	turnStatus(t, w, 200)
	var output ToolResponse
	json.Unmarshal(w.Body.Bytes(), &output)
	if len(output.Output) > 1<<20 || output.Full {
		t.Fatalf("result exceeds cap: %d full %v", len(output.Output), output.Full)
	}
	root := filepath.Join(filepath.Dir(f.registry.live[f.id].conn.SessionFile), "logs", "stubs")
	os.MkdirAll(root, 0700)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	os.WriteFile(outside, []byte("not allowed"), 0600)
	link := filepath.Join(root, "0123456789abcdef.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	f.engine.entries[0].Output = "[tool: read · full: " + link + "]"
	turnStatus(t, f.action(t, "GET", "/tools/bounded", ""), 409)
}

func TestTurnQueueReorderPersistsPublishesAndDrainsInOrder(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"first","mode":"send"}`), 200)
	for _, text := range []string{"second", "third", "fourth"} {
		turnStatus(t, f.action(t, "POST", "/turn", fmt.Sprintf(`{"text":%q,"mode":"queue","context":[{"kind":"file","path":"/tmp/%s.go"}]}`, text, text)), 200)
	}
	before := f.snapshot(t)
	queue := before.Snapshot.Queue
	order := []string{queue[2].ID, queue[0].ID, queue[1].ID}
	body, _ := json.Marshal(ReorderQueueRequest{Order: order, Expect: &AgentExpect{Version: &before.Version}})
	turnStatus(t, f.action(t, "PUT", "/queue", string(body)), 200)
	got := f.snapshot(t)
	if got.Version != before.Version+1 || got.Snapshot.Queue[0].Text != "fourth" || got.Snapshot.Queue[0].Context[0].Path != "/tmp/fourth.go" {
		t.Fatalf("reordered snapshot: %+v", got)
	}
	f.registry.mu.Lock()
	err := f.registry.disk(func(d *agentDisk) error {
		if d.Records[f.id].Queue[0].ID != order[0] {
			t.Error("reorder was not saved")
		}
		return nil
	}, false)
	f.registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	events, _, _, _, _ := f.server.events.read(0)
	var changed QueueChangedEvent
	for _, event := range events {
		if event.kind == EventQueueChanged {
			if err := json.Unmarshal(event.data, &changed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(changed.Queue) != 3 || changed.Queue[0].ID != order[0] {
		t.Fatalf("queue event: %+v", changed)
	}
	turnStatus(t, f.action(t, "PUT", "/queue", string(body)), 409)
	for i, text := range []string{"fourth", "second", "third"} {
		f.engine.stream(i) <- session.Event{Kind: session.EventTurnDone}
		close(f.engine.stream(i))
		turnWait(t, func() bool { return len(f.snapshot(t).Snapshot.Queue) == 2-i })
		entries := f.engine.Transcript()
		if entries[len(entries)-1].Text != contextMessage(text, []AgentContext{{Kind: "file", Path: "/tmp/" + text + ".go"}}) {
			t.Fatalf("admitted wrong message: %+v", entries)
		}
	}
	turnStatus(t, f.action(t, "PUT", "/queue", `{"order":[]}`), 200)
}

func TestTurnQueueReorderRejectsInvalidAndStaleOrdersAtomically(t *testing.T) {
	f := newTurnFixture(t)
	turnStatus(t, f.action(t, "POST", "/turn", `{"text":"first","mode":"send"}`), 200)
	for _, text := range []string{"second", "third"} {
		turnStatus(t, f.action(t, "POST", "/turn", fmt.Sprintf(`{"text":%q,"mode":"queue"}`, text)), 200)
	}
	before := f.snapshot(t)
	queue := before.Snapshot.Queue
	for _, body := range []string{`{}`, `{"order":null}`, `{"order":"bad"}`, `{"order":[""]}`, fmt.Sprintf(`{"order":[%q,%q]}`, queue[0].ID, queue[0].ID), `{"order":[],"text":"no"}`, `{"order":[]} {}`} {
		turnStatus(t, f.action(t, "PUT", "/queue", body), 422)
	}
	for _, body := range []string{`{"order":[]}`, fmt.Sprintf(`{"order":[%q]}`, queue[0].ID), fmt.Sprintf(`{"order":[%q,"already-sent"]}`, queue[1].ID)} {
		turnStatus(t, f.action(t, "PUT", "/queue", body), 409)
	}
	after := f.snapshot(t)
	if after.Version != before.Version || after.Snapshot.Queue[0].ID != queue[0].ID || after.Snapshot.Queue[1].ID != queue[1].ID {
		t.Fatalf("refused reorder mutated queue: %+v", after)
	}
	turnStatus(t, f.action(t, "POST", "/queue", `{"order":[]}`), 405)
}
