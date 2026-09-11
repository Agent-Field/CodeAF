package session

// THE QUICK TASK AS ONE OBJECT: ONE DOOR, ONE RECORD, ONE HOLD.
//
// task_quick_test.go drives the quick task's road; this page pins what the node
// IS on every side of that road — the same shape whichever road admitted it,
// the same body after the file has been written and read back, a hold on what
// it wrote, and one row, not eight, in another window's account of it
// (docs/design/quick-task/DESIGN.md, the 2026-09-11 section).

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// quickArgs is one `quick_task` call's arguments, as the model sends them.
func quickArgs(t *testing.T, parsed quickArguments) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// ── (1) the record carries the body, and a restart reads it back ────────────

// A QUICK NODE SURVIVES THE FILE, TICKS AND ALL, AND THE CLOSE SETTLES IT.
//
// Before the body was on the record, a checkpoint holding a quick node could not
// even be read (its empty done-condition refused the whole document), and when
// it could, the node came back as a label: no list, no ticks, nothing telling
// the runner it was quick. This is the whole road through the real store: the
// tool admits one quick node that runs and a second that waits behind it on a
// shared file, the first writes and ticks, and a fresh graph reads the file.
func TestAQuickNodeSurvivesItsCheckpointWithItsTicksAndSettlesOnTheClose(t *testing.T) {
	session := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = session
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	items := []string{"read the schema", "read the migration", "say which disagrees"}
	said, failed, err := agent.startQuickTask(context.Background(), quickArgs(t, quickArguments{
		Line: "compare the schema with the migration", Items: items, Files: []string{"notes.md"},
	}))
	if err != nil || failed {
		t.Fatalf("the first quick task was refused: %q (%v)", said, err)
	}
	working := ran.await(t)
	said, failed, err = agent.startQuickTask(context.Background(), quickArgs(t, quickArguments{
		Line: "tidy the notes afterwards", Files: []string{"notes.md"},
	}))
	if err != nil || failed {
		t.Fatalf("the second quick task was refused: %q (%v)", said, err)
	}
	if !strings.Contains(said, fmt.Sprintf("waits for task %d", working.id)) {
		t.Fatalf("the second quick task did not wait on the first: %q", said)
	}

	// The worker writes a file and ticks the first item. The tick is a
	// transition, and it reaches the disk as one.
	working.noteWrote("notes.md")
	working.quickItemChange(1, nil)

	document, found := loadTaskCheckpoint(taskCheckpointPath(session))
	if !found {
		t.Fatal("the checkpoint holding two quick nodes could not be read back")
	}
	var first taskRecord
	for _, record := range document.Nodes {
		if record.ID == working.id {
			first = record
		}
	}
	want := &quickRecord{Line: "compare the schema with the migration", Items: items,
		Done: []bool{true, false, false}, Files: []string{"notes.md"}}
	if !reflect.DeepEqual(first.Quick, want) {
		t.Fatalf("the record carries the body %+v, want %+v", first.Quick, want)
	}
	if first.Acceptance != "" {
		t.Fatalf("the record invented a done-condition for a quick node: %q", first.Acceptance)
	}

	// A FRESH PROCESS READS IT. Both quick nodes settle — the one that was
	// working and the one that was waiting — and neither goes back on the
	// frontier as work.
	fresh := newTaskGraph()
	fresh.run = func(node *TaskNode) { t.Errorf("task %d was run again after the close", node.id) }
	recovery := fresh.rehydrate(document, workspace, TaskSettleAsk)
	if recovery.quick != 2 || recovery.interrupted != 0 || recovery.waiting != 0 {
		t.Fatalf("recovery counted %+v, want both quick nodes under their own clause", recovery)
	}
	note := recovery.note()
	if !strings.Contains(note, "2 quick tasks did not finish") {
		t.Fatalf("the recovery line does not say what happened to them: %q", note)
	}
	if strings.Contains(note, "interrupted") || strings.Contains(note, "no branch kept") {
		t.Fatalf("the recovery line promises a resume or a branch a quick task never had: %q", note)
	}
	fresh.runFrontier()

	back := fresh.node(working.id)
	if back.stateNow() != TaskFailed {
		t.Fatalf("the working quick node came back %q", back.stateNow())
	}
	if back.spec.quick == nil || back.kind != TaskKindQuick {
		t.Fatal("the node came back without its body, so nothing tells the runner it is quick")
	}
	report, changed, _, _ := back.leavings()
	for _, line := range []string{quickInterruptedReport, "ticked 1 of 3: read the schema",
		"not ticked: read the migration · say which disagrees"} {
		if !strings.Contains(report, line) {
			t.Fatalf("the report does not say %q:\n%s", line, report)
		}
	}
	if !reflect.DeepEqual(changed, []string{"notes.md"}) {
		t.Fatalf("the settled quick node lists %q as changed, want what it wrote", changed)
	}
	// AND THE NEXT CHECKPOINT WRITES THE BODY BACK, because a node read back
	// without it would lose the list at the first transition of the new life.
	fresh.mu.Lock()
	again := back.recordLocked()
	fresh.mu.Unlock()
	if !reflect.DeepEqual(again.Quick, want) {
		t.Fatalf("the restored node writes the body back as %+v, want %+v", again.Quick, want)
	}

	waiting := fresh.node(working.id + 1)
	if waiting.stateNow() != TaskFailed {
		t.Fatalf("the waiting quick node came back %q — it would start on its own in the next session", waiting.stateNow())
	}
	if report, _, _, _ := waiting.leavings(); report != quickNeverStartedReport {
		t.Fatalf("the waiting quick node settled saying %q, want %q", report, quickNeverStartedReport)
	}
}

// THE FILE THE OWNER'S LAPTOP ACTUALLY WROTE DECODES.
//
// Four quick nodes with an empty done-condition beside an ordinary node: the
// shape of a real tasks.json that `ignoring corrupt task checkpoint` threw away
// whole, taking the ordinary node with it. An ordinary node with no
// done-condition is still refused — the exemption is the kind's, not a hole.
func TestACheckpointHoldingQuickNodesBesideATaskDecodes(t *testing.T) {
	var nodes []string
	for id := 1; id <= 4; id++ {
		nodes = append(nodes, fmt.Sprintf(`{"id":%d,"title":"read part %d","brief":"read part %d","acceptance":"","state":"done","kind":"quick","depth":1,"where":"in place","groundMode":"in place"}`, id, id, id))
	}
	nodes = append(nodes, `{"id":5,"title":"fix the loader","brief":"fix the loader","acceptance":"the loader test passes","state":"queued","depends_on":[1,2,3,4]}`)
	file := fmt.Sprintf(`{"type":%q,"version":%d,"seq":5,"nodes":[%s]}`, taskDocumentType, taskFileVersion, strings.Join(nodes, ","))

	document, err := decodeTasks([]byte(file))
	if err != nil {
		t.Fatalf("a checkpoint with quick nodes in it was refused: %v", err)
	}
	if len(document.Nodes) != 5 {
		t.Fatalf("decoded %d nodes, want all five", len(document.Nodes))
	}

	ordinary := strings.Replace(file, `"acceptance":"the loader test passes"`, `"acceptance":""`, 1)
	if _, err := decodeTasks([]byte(ordinary)); err == nil {
		t.Fatal("an ordinary node with no done-condition was accepted")
	}
}

// A BRIEF'S EXPECTATIONS SURVIVE THE FILE, AND THE PREFLIGHT IS OWED ONCE.
//
// They were never written, so a node read back lost the section its brief
// carries them in, and a node that had not yet started lost the preflight it
// still owed. Now both come back — the preflight only onto a node that never
// ran, because one that did has answered it in a working copy it has since
// changed.
func TestABriefsExpectationsSurviveTheFileAndThePreflightIsOwedOnce(t *testing.T) {
	expects := []Expectation{{Path: "internal/config/load.go", Holds: "func Load"}}
	waiting := restoreNode(newTaskGraph(), taskRecord{ID: 1, Title: "fix the loader", Brief: "b", Acceptance: "a",
		State: TaskQueued, Expects: expects})
	if !reflect.DeepEqual(waiting.spec.expects, expects) || !reflect.DeepEqual(waiting.Expects, expects) {
		t.Fatalf("a node that never ran came back with %+v in its brief and %+v owed, want both", waiting.spec.expects, waiting.Expects)
	}
	resumed := restoreNode(newTaskGraph(), taskRecord{ID: 2, Title: "fix the loader", Brief: "b", Acceptance: "a",
		State: TaskQueued, Interrupted: true, StartedAt: time.Now(), Expects: expects})
	if !reflect.DeepEqual(resumed.spec.expects, expects) {
		t.Fatal("a resumed node lost the expectations its brief carries")
	}
	if len(resumed.Expects) != 0 {
		t.Fatal("a node that already ran is asked the preflight again on a copy it has changed")
	}
	waiting.graph.mu.Lock()
	written := waiting.recordLocked()
	waiting.graph.mu.Unlock()
	if !reflect.DeepEqual(written.Expects, expects) {
		t.Fatalf("the record writes %+v, want the expectations back", written.Expects)
	}
}

// ── (2) one door ────────────────────────────────────────────────────────────

// THE CEILING'S QUICK NODE IS THE TOOL'S QUICK NODE.
//
// The two roads used to build two objects: the ceiling's came through the route
// judge's launcher with an invented done-condition, a working copy's mode, a
// naming call, no `where`, no family and no look at the claims. Both now come
// through [Agent.admitQuick], and every field the kind decides is compared here.
func TestTheCeilingsQuickNodeHasTheToolsShape(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	completer := &scriptedCompleter{steps: grindingSteps(checkpointMarkAt(1)+6, checkpointSplitSketch, dowry)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	ceiling := ran.await(t)
	if ceiling.spec.quick == nil {
		t.Fatal("the write-free turn was not handed to a quick node")
	}

	said, failed, err := agent.startQuickTask(context.Background(), quickArgs(t, quickArguments{
		Line: asked, Items: ceiling.spec.quick.items,
	}))
	if err != nil || failed {
		t.Fatalf("the tool refused the same ask: %q (%v)", said, err)
	}
	tool := ran.await(t)

	shape := func(node *TaskNode) map[string]any {
		node.graph.mu.Lock()
		defer node.graph.mu.Unlock()
		return map[string]any{
			"title":       node.spec.title,
			"summary":     node.spec.summary,
			"brief":       node.spec.brief,
			"acceptance":  node.spec.acceptance,
			"deliverable": node.spec.deliverable,
			"checks":      len(node.spec.checks),
			"named":       node.spec.named,
			"ahead":       node.spec.ahead != nil,
			"where":       node.spec.where,
			"ground":      node.Ground,
			"mode":        node.Mode,
			"parent":      node.parent,
			"depth":       node.depth,
			"owner":       node.spec.owner != nil,
			"model":       node.spec.model,
			"request":     node.spec.request,
			"wide":        node.spec.wide,
			"armed":       node.spec.armed,
			"drawn":       node.spec.drawn.proposes(),
			"kind":        node.kind,
			"line":        node.spec.quick.line,
			"items":       strings.Join(node.spec.quick.items, "|"),
			"files":       len(node.spec.quick.files),
		}
	}
	got, want := shape(ceiling), shape(tool)
	for field, value := range want {
		if !reflect.DeepEqual(got[field], value) {
			t.Errorf("the ceiling's quick node has %s = %v, the tool's has %v", field, got[field], value)
		}
	}
	// AND THE VALUES ARE THE KIND'S, not merely equal to each other.
	if got["acceptance"] != "" || got["named"] != true || got["mode"] != TaskModeInPlace ||
		got["where"] != "in place" || got["depth"] != 1 || got["ahead"] != false {
		t.Fatalf("the quick node's shape is not the kind's: %+v", got)
	}
}

// ── (3) the continue verb ───────────────────────────────────────────────────

// A QUICK TASK CANNOT BE CONTINUED, AND IS TOLD SO IN THE DESIGN'S WORDS.
//
// A restored quick node used to be re-queued by the continue verb and fall
// through the runner to an ordinary worker — a copy of the folder, a branch and
// a check for work whose promise was none. It is refused now by the same kind
// switch that refuses a design and a saved shape's run, in the same sentence.
func TestContinuingARestoredQuickTaskIsRefusedLikeADesign(t *testing.T) {
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 2, Nodes: []taskRecord{
		{ID: 1, Title: "compare the configs", Brief: "compare the configs", State: TaskFailed, Kind: TaskKindQuick,
			Quick: &quickRecord{Line: "compare the configs", Items: []string{"a", "b"}, Done: []bool{true, false}}},
		{ID: 2, Title: "make a shape", Brief: "make a shape", Acceptance: "a page", State: TaskFailed, Kind: TaskKindHarness},
	}}
	graph := newTaskGraph()
	graph.rehydrate(document, t.TempDir(), TaskSettleAsk)

	shape := regexp.MustCompile(`^task (\d+) is (.+), not a run that can be continued$`)
	quick := graph.reopen(graph.node(1), "")
	design := graph.reopen(graph.node(2), "")
	if quick == nil {
		t.Fatal("a restored quick task was put back on the frontier")
	}
	for _, refusal := range []error{quick, design} {
		if refusal == nil || !shape.MatchString(refusal.Error()) {
			t.Fatalf("the refusal %v is not the one sentence every such kind gets", refusal)
		}
	}
	if got := shape.FindStringSubmatch(quick.Error())[2]; got != TaskKindWord(TaskKindQuick) {
		t.Fatalf("the refusal names the kind %q, want %q", got, TaskKindWord(TaskKindQuick))
	}
	if state := graph.node(1).stateNow(); state != TaskFailed {
		t.Fatalf("the refused quick task is now %q", state)
	}
}

// ── (4) the hold on what it wrote ───────────────────────────────────────────

// A FILE A RUNNING QUICK TASK HAS WRITTEN IS ITS FILE UNTIL IT LANDS.
//
// A quick task writes in its caller's folder and deliberately holds no tree, and
// until this changed that left it holding nothing: the chat could overwrite the
// half-written file under it while the manual said the first writer owned it.
// Driven through the real road — the chat calls `quick_task`, a real worker
// writes notes.md and is held mid-work — and asked of the real guard.
func TestTheChatCannotWriteAFileARunningQuickTaskHasWritten(t *testing.T) {
	release := make(chan struct{})
	completer := newQuickLanes([]step{
		quickCall("q1", "draft the note", "draft the NOTE-HOLDER note", nil, nil),
		finalText("it is out"),
	})
	completer.lane("NOTE-HOLDER",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("w1", "write", `{"path":"notes.md","content":"half a draft\n"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			<-release
			return textResponse("the note is drafted"), nil
		},
	)
	agent, graph, workspace := quickAgent(t, completer)
	events := mustSubmit(t, agent, "draft the note")
	waitFor(t, "the quick task to write notes.md", func() bool {
		node := graph.node(1)
		return node != nil && node.stateNow() == TaskRunning && len(node.rememberedWrites()) > 0
	})

	guard := treeClaimGuard{agent: agent}
	_, result, ok := guard.PreAction(context.Background(), nil, nil, scopedCall("write", filepath.Join(workspace, "notes.md")))
	if ok {
		t.Fatal("the chat wrote over a file a running quick task had written")
	}
	if want := fileHeldRefusal(treeClaim{id: 1, title: "draft the note"}, "notes.md"); result.text != want {
		t.Fatalf("the refusal reads %q, want %q", result.text, want)
	}
	// THE FILE, NOT THE FOLDER. Everything else in it is still the person's.
	if _, _, ok := guard.PreAction(context.Background(), nil, nil, scopedCall("write", filepath.Join(workspace, "other.md"))); !ok {
		t.Fatal("a running quick task held a file it never wrote")
	}

	close(release)
	collect(t, events)
	waitDoneNode(t, graph.node(1))
	if _, _, ok := guard.PreAction(context.Background(), nil, nil, scopedCall("write", filepath.Join(workspace, "notes.md"))); !ok {
		t.Fatal("the file was still held after the quick task landed")
	}
}

// ── (5) another window's account of a family ────────────────────────────────

// ANOTHER WINDOW'S QUICK PARTS READ AS ONE PIECE OF WORK.
//
// The presence row now carries the kind and the parent, and the block folds a
// family onto its head: a task with three quick parts running is one line
// saying so, not four unrelated jobs a model might start again. A quick task
// with no parent says `quick`, because it writes in that window's folder.
func TestAnotherWindowsQuickPartsFoldUnderTheirTask(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := newTaskGraph()
	started := time.Now()
	add := func(node *TaskNode) {
		node.graph, node.state, node.started = graph, TaskRunning, started
		node.done = make(chan struct{})
		graph.nodes[node.id] = node
		graph.order = append(graph.order, node.id)
	}
	add(&TaskNode{id: 4, spec: taskSpec{title: "Survey the config loaders"}, wrote: []string{"internal/config/load.go"}})
	for id := uint64(5); id <= 7; id++ {
		body := newQuickTaskSpec(fmt.Sprintf("read loader %d", id), []string{"read it", "say what it sets"}, nil)
		body.done[0] = true
		add(&TaskNode{id: id, parent: 4, kind: TaskKindQuick, spec: taskSpec{title: fmt.Sprintf("loader %d", id), quick: body},
			wrote: []string{fmt.Sprintf("notes/%d.md", id)}})
	}
	add(&TaskNode{id: 9, kind: TaskKindQuick, spec: taskSpec{title: "Compare the two lockfiles",
		quick: newQuickTaskSpec("compare the two lockfiles", nil, nil)}})
	agent.config.tasker = graph

	rows := agent.presenceTasks()
	if len(rows) != 5 {
		t.Fatalf("presence carries %d rows, want five", len(rows))
	}
	part := rows[1]
	if part.Kind != TaskKindQuick || part.Parent != "4" || part.Done != 1 || part.Total != 2 {
		t.Fatalf("a quick part's presence row is %+v, want its kind, its parent and its list's count", part)
	}
	if rows[0].Kind != "" || rows[0].Parent != "" || rows[0].Total != 0 {
		t.Fatalf("an ordinary task's presence row grew facts it does not have: %+v", rows[0])
	}

	live := make([]ElsewhereTask, 0, len(rows))
	for _, row := range rows {
		live = append(live, ElsewhereTask{SessionID: "theirs", Session: "docs pass", Task: row})
	}
	block := renderElsewhereBlock(nil, live)
	for _, want := range []string{
		`- Survey the config loaders · window "docs pass" · 3 quick parts running · internal/config/load.go, notes/5.md, notes/6.md, notes/7.md`,
		`- Compare the two lockfiles · quick · window "docs pass"`,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("the block is missing %q:\n%s", want, block)
		}
	}
	if strings.Count(block, "\n- ") != 2 {
		t.Fatalf("the block draws the family as separate jobs:\n%s", block)
	}
	// NO TICK COUNT IN IT: a number that moved with every item would churn the
	// block every turn the header promises it does not churn.
	if strings.Contains(block, "1/2") || strings.Contains(block, "1 of 2") {
		t.Fatalf("the block carries a list's progress:\n%s", block)
	}
}

// A PART WHOSE PARENT IS NOT IN THE READING STANDS ON ITS OWN, and ids are only
// a family inside one window: the same number in two windows is two nodes.
func TestAPartWithNoParentInTheReadingStandsAlone(t *testing.T) {
	live := []ElsewhereTask{
		{SessionID: "one", Session: "a", Task: PresenceTask{ID: "5", Title: "orphan part", Parent: "4", Kind: TaskKindQuick}},
		{SessionID: "two", Session: "b", Task: PresenceTask{ID: "4", Title: "a different four"}},
	}
	families := foldElsewhere(live)
	if len(families) != 2 || len(families[0].parts)+len(families[1].parts) != 0 {
		t.Fatalf("a part was folded under a node of another window: %+v", families)
	}
}
