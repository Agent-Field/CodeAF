package session

// THE BEHAVIORAL CONTRACT FOR RECORDED TASK CLOCKS LIVES HERE. These tests
// cross checkpoints, graph transitions, rows, notices, and ground overlap; the
// source-reading law is separate so the fast law gate never runs this file's
// live graph completion.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// C1 — A landed node's landing time survives the process. The JSON crossing is
// deliberate: copying a taskRecord would prove only that two Go structs happen
// to agree, not that the checkpoint a later process reads carries both facts.
func TestACheckpointCarriesTheLandingTimeAcrossARestore(t *testing.T) {
	graph := newTaskGraph()
	started := time.Now().Add(-2 * time.Minute)
	node := &TaskNode{
		graph:   graph,
		id:      1,
		done:    make(chan struct{}),
		state:   TaskRunning,
		started: started,
		spec: taskSpec{
			title: "Rotate the staging certificate", brief: "rotate it", acceptance: "the certificate is current",
		},
	}
	graph.nodes[node.id] = node
	graph.order = append(graph.order, node.id)
	graph.seq = node.id
	graph.complete(node, TaskDone)

	graph.mu.Lock()
	landed := node.ended
	graph.mu.Unlock()
	if landed.IsZero() {
		t.Fatal("the live landing recorded no landing time")
	}

	raw, err := json.Marshal(graph.document())
	if err != nil {
		t.Fatalf("encode the checkpoint: %v", err)
	}
	document, err := decodeTasks(raw)
	if err != nil {
		t.Fatalf("decode the checkpoint: %v", err)
	}
	restored := restoreNode(newTaskGraph(), document.Nodes[0])
	if !restored.started.Equal(started) {
		t.Fatalf("restored start = %s, want %s", restored.started, started)
	}
	if !restored.ended.Equal(landed) {
		t.Fatalf("restored landing = %s, want %s", restored.ended, landed)
	}
}

// C1 — A settled node's recorded age stays frozen across a restore, including
// the sub-millisecond run whose elapsed_ms rounded to zero. Reading the same
// restored record again must not turn the time since its old start into work.
func TestARestoredSettledNodesZeroAgeStaysFrozen(t *testing.T) {
	started := time.Now().Add(-time.Hour)
	node := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, StartedAt: started, EndedAt: started.Add(500 * time.Microsecond), ElapsedMS: 0,
	})

	read := func() (time.Duration, int64, int64) {
		node.graph.mu.Lock()
		defer node.graph.mu.Unlock()
		notice := node.noticeLocked(0)
		entry := node.indexEntryLocked("aaaa1111aaaa1111")
		record := node.recordLocked()
		return notice.Elapsed, entry.DurationMS, record.ElapsedMS
	}
	assertZero := func(label string) {
		t.Helper()
		noticeAge, indexAge, recordAge := read()
		if noticeAge != 0 || indexAge != 0 || recordAge != 0 {
			t.Fatalf("%s read grew the settled age: notice %s, index %dms, record %dms", label, noticeAge, indexAge, recordAge)
		}
	}

	assertZero("first")
	time.Sleep(time.Millisecond)
	assertZero("later")
}

// C2 — A checkpoint written before stamps existed still resumes at version 1.
// Its elapsed age remains data, while absent instants remain honest zero times
// and cost no new bytes when this build writes the record again.
func TestACheckpointWrittenBeforeStampsExistedStillResumes(t *testing.T) {
	old := []byte(`{"type":"tasks","version":1,"seq":10,"nodes":[{"id":1,"title":"Rotate the staging certificate","brief":"…","acceptance":"…","state":"unverified","elapsed_ms":720000}],"runs":[]}`)
	document, err := decodeTasks(old)
	if err != nil {
		t.Fatalf("a version 1 checkpoint was refused: %v", err)
	}
	if document.Version != taskFileVersion || taskFileVersion != 1 {
		t.Fatalf("version = %d and taskFileVersion = %d, want both to remain 1", document.Version, taskFileVersion)
	}
	if len(document.Nodes) != 1 || document.Nodes[0].ElapsedMS != 720000 {
		t.Fatalf("the old elapsed age did not survive: %+v", document.Nodes)
	}

	restored := restoreNode(newTaskGraph(), document.Nodes[0])
	if !restored.started.IsZero() || !restored.ended.IsZero() {
		t.Fatalf("an old record invented stamps: started %s, ended %s", restored.started, restored.ended)
	}
	raw, err := json.Marshal(document.Nodes[0])
	if err != nil {
		t.Fatalf("re-encode the old record: %v", err)
	}
	if strings.Contains(string(raw), `"startedAt"`) || strings.Contains(string(raw), `"endedAt"`) {
		t.Fatalf("zero stamps took space in an old record: %s", raw)
	}
}

// C3 — A rebuilt row takes the record's stamp. Its equality to the recorded
// instant and its distance from now prove the reading clock did not replace it.
func TestARestoredNodesRowKeepsTheRecordedLandingTime(t *testing.T) {
	landed := time.Now().Add(-20 * time.Minute)
	node := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, EndedAt: landed,
	})
	node.graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	node.graph.mu.Unlock()

	if !entry.EndedAt.Equal(landed) {
		t.Fatalf("rebuilt row landed at %s, want the recorded %s", entry.EndedAt, landed)
	}
	if time.Since(entry.EndedAt) < time.Minute {
		t.Fatalf("the restored row was dated at the reading instead of twenty minutes ago: %s", entry.EndedAt)
	}
}

// C4 — A row with no recorded landing time is undated, and is still a row.
// This is the emptiness law: the surface files it at the reading for inclusion,
// but draws nothing where its age would go.
func TestANodeWithNoRecordedLandingTimeDrawsNoStamp(t *testing.T) {
	node := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, ElapsedMS: 720000,
	})
	node.graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	node.graph.mu.Unlock()
	if !entry.EndedAt.IsZero() {
		t.Fatalf("an undated record was stamped at %s", entry.EndedAt)
	}
	if entry.DurationMS != 720000 {
		t.Fatalf("duration = %dms, want the recorded 720000ms", entry.DurationMS)
	}
	if entry.ID != "1" || entry.Status != string(TaskDone) {
		t.Fatalf("the undated node stopped being a row: %+v", entry)
	}
}

// C5 — The live landing still stamps the live instant. This drives the graph's
// real report hook and reads the row it appends, so both the transition and the
// project's durable index are covered by one observation.
func TestALiveLandingStampsTheMomentItLanded(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = func(node *TaskNode) {
		node.finish("the certificate is current", nil, "", mergeInPlace)
		node.graph.complete(node, TaskDone)
	}
	graph.mu.Unlock()

	before := time.Now()
	id := graph.reserve()
	graph.admit(id, taskSpec{
		title: "Rotate the staging certificate", brief: "rotate it", acceptance: "the certificate is current",
	})
	waitDoneNode(t, graph.node(id))
	after := time.Now()

	rows := ReadTaskIndex(TaskIndexPath(journal))
	if len(rows) != 1 {
		t.Fatalf("the live landing appended %d rows, want one: %+v", len(rows), rows)
	}
	if rows[0].EndedAt.IsZero() || rows[0].EndedAt.Before(before) || rows[0].EndedAt.After(after) {
		t.Fatalf("the live row landed at %s, outside %s through %s", rows[0].EndedAt, before, after)
	}
}

// C6 — The notice carries the facts. A surface receives the record's exact
// instants, and receives zero rather than a guess when the record had neither.
func TestANoticeCarriesTheRecordedStamps(t *testing.T) {
	started := time.Now().Add(-25 * time.Minute)
	landed := started.Add(5 * time.Minute)
	restored := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, StartedAt: started, EndedAt: landed,
	})
	notice := restored.notice()
	if !notice.StartedAt.Equal(started) || !notice.EndedAt.Equal(landed) {
		t.Fatalf("notice stamps = %s through %s, want %s through %s", notice.StartedAt, notice.EndedAt, started, landed)
	}

	unknown := restoreNode(newTaskGraph(), taskRecord{
		ID: 2, Title: "An older task", Brief: "read it", Acceptance: "it is read", State: TaskDone,
	}).notice()
	if !unknown.StartedAt.IsZero() || !unknown.EndedAt.IsZero() {
		t.Fatalf("a notice invented absent stamps: started %s, ended %s", unknown.StartedAt, unknown.EndedAt)
	}
}

// C7 — The ground overlap reads the record. A restored start outranks the
// elapsed-age fallback, even when that fallback would imply a different clock.
func TestTheGroundOverlapReadsTheRestoredStart(t *testing.T) {
	started := time.Now().Add(-2 * time.Hour)
	node := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, StartedAt: started, ElapsedMS: int64((20 * time.Minute) / time.Millisecond),
	})
	if got := node.runStart(); !got.Equal(started) {
		t.Fatalf("the restored run starts at %s, want the recorded %s", got, started)
	}
}
