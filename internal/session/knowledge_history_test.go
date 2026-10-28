package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

type knowledgeHTTP func(*http.Request) (*http.Response, error)

func (f knowledgeHTTP) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func knowledgeProviderFixture(t *testing.T, started chan<- struct{}, release <-chan struct{}) *atomic.Int32 {
	t.Helper()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := &atomic.Int32{}
	http.DefaultTransport = knowledgeHTTP(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if release != nil {
			select {
			case <-release:
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}
		var body struct {
			Model    string
			Messages []struct{ Content string }
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			return nil, err
		}
		if body.Model != "deepseek/deepseek-v4.1-flash" {
			t.Errorf("unexpected model %q", body.Model)
		}
		var payload struct{ Messages []knowledge.Source }
		if err := json.Unmarshal([]byte(body.Messages[1].Content), &payload); err != nil {
			return nil, err
		}
		result := map[string]any{"subjects": []any{}}
		if len(payload.Messages) > 0 {
			claims := []any{}
			for _, source := range payload.Messages {
				claims = append(claims, map[string]any{"local_id": source.ID, "text": source.Text, "scope": "Cedar", "topic": "Queues", "kind": "fact", "evidence": []any{map[string]any{"source": source.ID, "quote": source.Text}}})
			}
			result = map[string]any{"claims": claims}
		}
		content, _ := json.Marshal(result)
		raw, _ := json.Marshal(map[string]any{"model": body.Model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
	})
	return calls
}
func writeKnowledgeChat(t *testing.T, root, bucket, id string, entries ...map[string]any) string {
	t.Helper()
	dir := filepath.Join(root, bucket, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := Place{Dir: dir}.Transcript()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	if err = encoder.Encode(map[string]any{"type": "session", "version": 1, "id": id, "cwd": "/" + bucket}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if err = encoder.Encode(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func knowledgeMessage(text string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": text, "timestamp": "2026-10-25T12:00:00Z"}
}

func TestKnowledgeHistoryIncludesTaskReportsWithoutInjectedBriefs(t *testing.T) {
	root := t.TempDir()
	chat := writeKnowledgeChat(t, root, "archive", "parent", knowledgeMessage("Original question."))
	place := Place{Dir: filepath.Dir(chat)}
	for _, name := range []string{"20261025-120000.000000_1.jsonl", "20261025-120001.000000_1.jsonl", "20261025-120002.000000_1-audit-check.jsonl"} {
		path := touchJournal(t, place, name)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range []map[string]any{
			knowledgeMessage("Injected brief says the task is complete."),
			{"type": "message", "role": "assistant", "content": "Cedar retains completed task findings.", "presentation": map[string]any{"audience": "operational"}},
			{"type": "message", "role": "assistant", "content": "Interrupted narration", "presentation": map[string]any{"audience": "operational", "interrupted": true}},
			{"type": "message", "role": "assistant", "content": "Synthetic note", "note": true},
			{"type": "message", "role": "tool", "content": "Tool transport"},
			{"type": "compaction", "window": 1},
			{"type": "message", "role": "assistant", "content": "Replay copy"},
		} {
			if err = json.NewEncoder(file).Encode(entry); err != nil {
				t.Fatal(err)
			}
		}
		if err = file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	history := KnowledgeHistory(root)
	journals, err := history.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(journals) != 4 {
		t.Fatalf("saved task runs missing: got %d journals, want 4", len(journals))
	}
	seen := map[string]bool{}
	for _, journal := range journals {
		batch, err := history.Read(context.Background(), journal, knowledge.JournalCursor{})
		if err != nil || len(batch.Sources) != 1 {
			t.Fatalf("task eligibility lost reports or imported injected context: sources=%d error=%v", len(batch.Sources), err)
		}
		source := batch.Sources[0]
		if journal.ID != chat && (source.Role != "assistant" || source.Text != "Cedar retains completed task findings.") {
			t.Fatalf("task report did not remain assistant evidence: %+v", source)
		}
		if seen[source.ID] {
			t.Fatal("task runs share source identity")
		}
		seen[source.ID] = true
		replay, err := history.Read(context.Background(), journal, batch.Cursor)
		if err != nil || len(replay.Sources) != 0 {
			t.Fatalf("completed cursor replayed sources: %d %v", len(replay.Sources), err)
		}
	}
}

func TestKnowledgeHistoryIncludesLooseAndAdaptiveReportsAndHonorsTombstones(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	root := PlacesRoot()
	chat := writeKnowledgeChat(t, root, "project", "parent", knowledgeMessage("Original question."))
	deletedChat := writeKnowledgeChat(t, root, "archive", "deleted", knowledgeMessage("Deleted question."))
	if err := os.WriteFile(filepath.Join(filepath.Dir(deletedChat), conversationDeletedFile), []byte("deleted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(chat), taskDeletedFile), []byte(`{"2":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	deletedNode := filepath.Join(RunsRoot(), "parent", "run-example", "node-b.jsonl")
	cleanup, err := json.Marshal(map[string]taskCleanup{"2": {IDs: map[string]bool{"2": true}, Rows: []TaskIndexEntry{{ID: "2", SessionID: "parent", TranscriptURI: taskURI(deletedNode)}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(chat), taskCleanupFile), cleanup, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(LooseTasksRoot(), "parent", "20261025-120000.000000_1.jsonl"),
		filepath.Join(LooseTasksRoot(), "parent", "20261025-120000.000000_2.jsonl"),
		filepath.Join(LooseTasksRoot(), "deleted", "20261025-120000.000000_1.jsonl"),
		filepath.Join(RunsRoot(), "parent", "run-example", "node-a.jsonl"),
		deletedNode,
		filepath.Join(RunsRoot(), "deleted", "run-example", "node-a.jsonl"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{\"type\":\"session\",\"version\":1,\"id\":\"shared-header\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Cedar reports durable task work.\"}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	history := KnowledgeHistory(root)
	journals, err := history.Inventory(context.Background())
	if err != nil || len(journals) != 3 {
		t.Fatalf("loose/adaptive reports or tombstones incorrect: journals=%d error=%v", len(journals), err)
	}
	seen := map[string]bool{}
	for _, journal := range journals {
		batch, err := history.Read(context.Background(), journal, knowledge.JournalCursor{})
		if err != nil || len(batch.Sources) != 1 {
			t.Fatalf("durable report missing: sources=%d error=%v", len(batch.Sources), err)
		}
		if seen[batch.Sources[0].ID] {
			t.Fatal("loose and adaptive journals share source identity")
		}
		seen[batch.Sources[0].ID] = true
	}
}

func TestKnowledgeBuildPublishesSavedTaskReportAsAssistantEvidence(t *testing.T) {
	root := t.TempDir()
	chat := writeKnowledgeChat(t, root, "archive", "parent")
	path := touchJournal(t, Place{Dir: filepath.Dir(chat)}, "20261025-120000.000000_1.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(file).Encode(map[string]any{"type": "message", "role": "assistant", "content": "Cedar retains durable queue findings.", "timestamp": "2026-10-25T12:00:00Z"})
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	v := knowledge.Open(t.TempDir())
	knowledgeProviderFixture(t, nil, nil)
	if err = v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	runKnowledgeWorker(t, v, root)
	state := waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildReady && len(s.Pages) > 0
	})
	if state.Build.Sources != 1 {
		t.Fatalf("saved task report was not imported: %+v", state.Build)
	}
	hits, err := v.Search("durable", 3)
	if err != nil || len(hits) != 1 || len(hits[0].Page.Evidence) != 1 || hits[0].Page.Evidence[0].Role != "assistant" {
		t.Fatalf("task report lost its citation or assistant attribution: hits=%d error=%v", len(hits), err)
	}
}
func waitKnowledgeState(t *testing.T, v *knowledge.Vault, condition func(knowledge.Snapshot) bool) knowledge.Snapshot {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var state knowledge.Snapshot
	var err error
	for time.Now().Before(deadline) {
		state, err = v.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if condition(state) {
			return state
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("knowledge state did not settle: %+v", state)
	return state
}
func runKnowledgeWorker(t *testing.T, v *knowledge.Vault, root string) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- v.Run(ctx, KnowledgeHistory(root), func() string { return "fixture" }) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("knowledge worker did not stop")
		}
	})
	return cancel, done
}
func TestKnowledgeInitialBuildImportsAllProjectsAndAutopopulates(t *testing.T) {
	root := t.TempDir()
	profile := t.TempDir()
	v := knowledge.Open(profile)
	writeKnowledgeChat(t, root, "project-a", "chat-a", knowledgeMessage("Cedar queues jobs."), map[string]any{"type": "message", "role": "user", "content": "Harness-only note", "note": true, "timestamp": "2026-10-25T12:00:00Z"}, map[string]any{"type": "compaction", "window": 2}, knowledgeMessage("Synthetic summary"), knowledgeMessage("Synthetic replay"))
	path := writeKnowledgeChat(t, root, "project-b", "chat-b", knowledgeMessage("Cedar retries failed work."))
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	calls := knowledgeProviderFixture(t, started, release)
	runKnowledgeWorker(t, v, root)
	state := waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool { return s.Build.Phase == knowledge.BuildUnbuilt })
	if state.Enabled || state.Build.Chats != 2 || calls.Load() != 0 {
		t.Fatalf("history imported before requested: %+v", state)
	}
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("background compiler did not start")
	}
	state, err := v.Status()
	if err != nil {
		t.Fatal(err)
	}
	if state.Build.Phase != knowledge.BuildRunning {
		t.Fatalf("missing working state: %+v", state)
	}
	if err = v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	close(release)
	state = waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool { return s.Build.Phase == knowledge.BuildReady && len(s.Pages) > 0 })
	if state.Build.Sources != 2 {
		t.Fatalf("captured notes or replay copies: %+v", state)
	}
	if len(state.Pages) != 1 || len(state.Pages[0].Evidence) != 2 {
		t.Fatalf("cross-project evidence missing: %+v", state.Pages)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = json.NewEncoder(file).Encode(knowledgeMessage("Cedar drains completed jobs."))
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	state = waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildReady && s.Build.Sources == 3 && s.Pending == 0
	})
	hits, err := knowledge.Open(profile).Search("drains", 3)
	if err != nil || len(hits) != 1 {
		t.Fatalf("new saved chat not populated: %+v %v", hits, err)
	}
	if len(state.Pages[0].Evidence) != 3 {
		t.Fatal("automatic update lost earlier citations")
	}
}
func TestKnowledgeWithoutHistoryWaitsAndLearnsFromFirstSavedChat(t *testing.T) {
	root := t.TempDir()
	v := knowledge.Open(t.TempDir())
	calls := knowledgeProviderFixture(t, nil, nil)
	runKnowledgeWorker(t, v, root)
	state := waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool { return s.Build.Phase == knowledge.BuildWaiting })
	if !state.Enabled || calls.Load() != 0 || len(state.Pages) != 0 {
		t.Fatalf("empty history called model: %+v", state)
	}
	writeKnowledgeChat(t, root, "project", "first", knowledgeMessage("Cedar queues new work."))
	state = waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool { return s.Build.Phase == knowledge.BuildReady && len(s.Pages) > 0 })
	if state.Build.Sources != 1 {
		t.Fatalf("first chat not automatically captured: %+v", state)
	}
}

func TestKnowledgeBuildContinuesPastUnreadableChat(t *testing.T) {
	root := t.TempDir()
	v := knowledge.Open(t.TempDir())
	broken := writeKnowledgeChat(t, root, "project-a", "broken", knowledgeMessage("Discard invalid chat."))
	if err := os.Mkdir(filepath.Join(filepath.Dir(broken), "meta.json"), 0700); err != nil {
		t.Fatal(err)
	}
	writeKnowledgeChat(t, root, "project-b", "healthy", knowledgeMessage("Cedar keeps healthy chats searchable."))
	knowledgeProviderFixture(t, nil, nil)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	runKnowledgeWorker(t, v, root)
	state := waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildPartial && len(s.Pages) > 0
	})
	if state.Build.Problems != 1 {
		t.Fatalf("missing unreadable-chat warning: %+v", state.Build)
	}
	hits, err := v.Search("healthy", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("unreadable chat blocked healthy knowledge: %v %+v", err, hits)
	}
}

func TestKnowledgeBuildIsolatesUnreadableProjectAndRetries(t *testing.T) {
	root := t.TempDir()
	writeKnowledgeChat(t, root, "project-a", "healthy-a", knowledgeMessage("Cedar keeps earlier healthy history searchable."))
	writeKnowledgeChat(t, root, "project-b", "recoverable", knowledgeMessage("Cedar recovers restored project history."))
	writeKnowledgeChat(t, root, "project-c", "healthy-c", knowledgeMessage("Cedar keeps later healthy history searchable."))
	blocked := filepath.Join(root, "project-b")
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(blocked, 0700); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.ReadDir(blocked); !os.IsPermission(err) {
		t.Skip("environment cannot simulate an unreadable project directory")
	}
	v := knowledge.Open(t.TempDir())
	knowledgeProviderFixture(t, nil, nil)
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	runKnowledgeWorker(t, v, root)
	state := waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildPartial && len(s.Pages) > 0
	})
	if state.Build.Problems != 1 || state.Build.Sources != 2 {
		t.Fatalf("unreadable project prevented healthy history or lost its warning: %+v", state.Build)
	}
	hits, err := v.Search("healthy history", 5)
	if err != nil || len(hits) != 1 || len(hits[0].Page.Evidence) != 2 {
		t.Fatalf("healthy history on both sides of unreadable project missing: %+v %v", hits, err)
	}
	if err = os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildReady && s.Build.Sources == 3
	})
	hits, err = v.Search("restored", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("restored project was not retried: %+v %v", hits, err)
	}
}

func TestKnowledgeSealedHistoryPublishesBeforeDamagedLaterRecords(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "project", "sealed")
	if err := os.MkdirAll(filepath.Join(dir, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create((Place{Dir: dir}).Transcript())
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	if err = encoder.Encode(map[string]any{"type": "session", "version": 1, "id": "sealed"}); err != nil {
		t.Fatal(err)
	}
	if err = encoder.Encode(knowledgeMessage("Cedar retains readable earlier history.")); err != nil {
		t.Fatal(err)
	}
	for range 256 {
		if err = encoder.Encode(map[string]any{"type": "message", "note": true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = io.WriteString(file, strings.Repeat("x", 16*1024*1024+1)+"\n"); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	v := knowledge.Open(t.TempDir())
	knowledgeProviderFixture(t, nil, nil)
	if err = v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	runKnowledgeWorker(t, v, root)
	waitKnowledgeState(t, v, func(s knowledge.Snapshot) bool {
		return s.Build.Phase == knowledge.BuildPartial && len(s.Pages) > 0
	})
	hits, err := v.Search("readable earlier", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("damaged later record prevented earlier knowledge: %+v %v", hits, err)
	}
}

func TestKnowledgeHistoryExcludesReservedContinuationContext(t *testing.T) {
	root := t.TempDir()
	writeKnowledgeChat(t, root, "project", "chat", knowledgeMessage("[carry on] Please continue my work."), knowledgeMessage(checkpointCarryOnLead+"Internal checkpoint detail."), knowledgeMessage(legacyCheckpointCarryOnLead+"Older internal detail."), knowledgeMessage(volatileNoteOpening+"Transient control context"), map[string]any{"type": "message", "role": "assistant", "content": NoChangeReply}, map[string]any{"type": "message", "role": "user", "content": "Internal control", "presentation": map[string]any{"audience": "operational"}}, knowledgeMessage("Cedar uses bounded queues."))
	h := KnowledgeHistory(root)
	js, err := h.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Read(context.Background(), js[0], knowledge.JournalCursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Sources) != 2 || b.Sources[0].Text != "[carry on] Please continue my work." || b.Sources[1].Text != "Cedar uses bounded queues." {
		t.Fatalf("internal controls entered knowledge or person text was dropped: %+v", b.Sources)
	}
}
