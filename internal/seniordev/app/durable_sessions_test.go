//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/sessioncore"
	"github.com/Agent-Field/codeaf/internal/seniordev/storage"
)

func TestDurablePromptPersistsAndProjectsBeforeFirstModelCall(t *testing.T) {
	// Session, message, and part records plus their projected views exist
	// before the first provider call, and child-session lineage is durable.
	workspace := t.TempDir()
	type observation struct {
		sessions int
		messages int
		parts    int
		dbRows   [3]int
		err      error
	}
	seen := observation{}
	var runtime *runtimeAdapter
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seen.sessions, seen.err = countStoredJSON(filepath.Join(workspace, ".senior-dev", "storage", "session"))
		if seen.err != nil {
			return nil, seen.err
		}
		seen.messages, seen.err = countStoredJSON(filepath.Join(workspace, ".senior-dev", "storage", "message"))
		if seen.err != nil {
			return nil, seen.err
		}
		seen.parts, seen.err = countStoredJSON(filepath.Join(workspace, ".senior-dev", "storage", "part"))
		if seen.err != nil {
			return nil, seen.err
		}
		for index, table := range []string{"session", "message", "part"} {
			seen.err = runtime.durable.db.QueryRowContext(
				request.Context(), "SELECT COUNT(*) FROM "+table,
			).Scan(&seen.dbRows[index])
			if seen.err != nil {
				return nil, seen.err
			}
		}
		return recordedResponse(
			request, http.StatusOK, "text/event-stream", chatReply("finished", 10),
		), nil
	})}
	runtime = newRuntime(workspace, &openRouterBackend{apiKey: "test", client: client})
	defer runtime.Close()
	rootID, err := runtime.Create(context.Background(), "", "coder")
	if err != nil {
		t.Fatal(err)
	}
	result, err := runTestTurn(t, runtime, testTurn{
		ParentSessionID: rootID, SessionTitle: "durable child",
		Agent: "coder", ProviderID: "openrouter",
		ModelID: "vendor/model", Workspace: workspace, Prompt: "persist me first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen.err != nil {
		t.Fatal(seen.err)
	}
	if seen.sessions != 2 || seen.messages < 2 || seen.parts < 1 ||
		seen.dbRows[0] != 2 || seen.dbRows[1] < 2 || seen.dbRows[2] < 1 {
		t.Fatalf("provider-start persistence = files(%d,%d,%d) db%v",
			seen.sessions, seen.messages, seen.parts, seen.dbRows)
	}
	child, err := runtime.durable.sessions.Get(context.Background(), result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID == nil || *child.ParentID != rootID {
		t.Fatalf("child lineage = %#v, want parent %s", child.ParentID, rootID)
	}
	var projectedParent string
	if err := runtime.durable.db.QueryRow(
		"SELECT parent_id FROM session WHERE id = ?", result.SessionID,
	).Scan(&projectedParent); err != nil {
		t.Fatal(err)
	}
	if projectedParent != rootID {
		t.Fatalf("projected parent = %q, want %q", projectedParent, rootID)
	}
}

func TestDurableStartupReconcilesFlatStorageContract(t *testing.T) {
	// Flat JSON is authoritative and reconstructs a missing database
	// projection, including a complete user turn.
	workspace := t.TempDir()
	runtime := newRuntime(workspace, &capturingBackend{})
	t.Cleanup(runtime.Close)
	rootID, err := runtime.Create(context.Background(), "", "coder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := persistTurnPrompt(context.Background(), runtime.durable, rootID, "msg_replay", turn{
		Agent: "coder", ProviderID: "p", ModelID: "m", Prompt: "replay me",
	}); err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(workspace, ".senior-dev", "senior-dev.db") + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	reopened, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for table, want := range map[string]int{"session": 1, "message": 1, "part": 1} {
		var got int
		if err := reopened.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("replayed %s rows = %d, %v; want %d", table, got, err, want)
		}
	}
}

func TestDurableStartupReconciliationPreservesProjectionOnlyRowsContract(t *testing.T) {
	// Startup repairs stale flat-derived rows without deleting the
	// todo/session_message projections that flat replay cannot rebuild.
	workspace := t.TempDir()
	runtime := newRuntime(workspace, &capturingBackend{})
	rootID, err := runtime.Create(context.Background(), "", "coder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := persistTurnPrompt(context.Background(), runtime.durable, rootID, "msg_incremental", turn{
		Agent: "coder", ProviderID: "p", ModelID: "m", Prompt: "keep projections",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.durable.db.Exec(`INSERT INTO todo
		(session_id, content, status, priority, position, time_created, time_updated)
		VALUES (?, 'todo survives', 'pending', 'high', 0, 1, 1)`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.durable.db.Exec(`INSERT INTO session_message
		(id, session_id, type, time_created, time_updated, data)
		VALUES ('projection-only', ?, 'note', 1, 1, '{}')`, rootID); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE session SET title = 'stale title' WHERE id = '` + rootID + `'`,
		`UPDATE message SET data = '{"role":"stale"}' WHERE id = 'msg_incremental'`,
		`UPDATE part SET data = '{"type":"stale"}' WHERE message_id = 'msg_incremental'`,
	} {
		if _, err := runtime.durable.db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	runtime.Close()

	reopened, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var title, messageData, partData string
	if err := reopened.db.QueryRow("SELECT title FROM session WHERE id = ?", rootID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow("SELECT data FROM message WHERE id = 'msg_incremental'").Scan(&messageData); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow("SELECT data FROM part WHERE message_id = 'msg_incremental'").Scan(&partData); err != nil {
		t.Fatal(err)
	}
	if title == "stale title" || strings.Contains(messageData, `"role":"stale"`) ||
		strings.Contains(partData, `"type":"stale"`) {
		t.Fatalf("stale rows remain: title=%q message=%s part=%s", title, messageData, partData)
	}
	for _, table := range []string{"todo", "session_message"} {
		var count int
		if err := reopened.db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE session_id = ?", rootID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("projection-only %s rows = %d, %v; want 1", table, count, err)
		}
	}
}

func TestDurableStartupQuarantinesTruncatedJSONContract(t *testing.T) {
	// A truncated JSON record is moved aside and cannot prevent valid durable
	// sessions from being reconciled at startup.
	workspace := t.TempDir()
	runtime := newRuntime(workspace, &capturingBackend{})
	rootID, err := runtime.Create(context.Background(), "", "coder")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := persistTurnPrompt(context.Background(), runtime.durable, rootID, "msg_valid", turn{Prompt: "valid"}); err != nil {
		t.Fatal(err)
	}
	runtime.Close()
	broken := filepath.Join(workspace, ".senior-dev", "storage", "message", rootID, "msg_truncated.json")
	if err := os.WriteFile(broken, []byte(`{"id":"msg_truncated"`), 0o644); err != nil {
		t.Fatal(err)
	}
	reopened, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatalf("startup was bricked by truncated JSON: %v", err)
	}
	defer reopened.Close()
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatalf("truncated source was not moved aside: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(
		workspace, ".senior-dev", "storage", "quarantine", "message", rootID, "msg_truncated.json.corrupt-*",
	))
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantined files = %v, %v; want one", matches, err)
	}
	var valid int
	if err := reopened.db.QueryRow("SELECT COUNT(*) FROM message WHERE id = 'msg_valid'").Scan(&valid); err != nil || valid != 1 {
		t.Fatalf("valid replay row = %d, %v; want 1", valid, err)
	}
}

type countingProjectionSource struct {
	projectionSource
	reads int
}

func (source *countingProjectionSource) ReadInto(key []string, dst any) error {
	source.reads++
	return source.projectionSource.ReadInto(key, dst)
}

func TestDurableWarmReconciliationReadsOnlyNewRecords(t *testing.T) {
	workspace := t.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	session, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "bounded"})
	if err != nil {
		t.Fatal(err)
	}
	message := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "msg_bounded", SessionID: session.ID},
		Time:        msgmodel.TimeCreated{Created: 1}, Agent: "coder",
	}
	parts := make([]msgmodel.Part, 20)
	for index := range parts {
		parts[index] = msgmodel.TextPart{
			PartBase: msgmodel.PartBase{
				ID: fmt.Sprintf("prt_%02d", index), SessionID: session.ID, MessageID: message.ID,
			},
			Text: "old",
		}
	}
	if err := durable.UpdateMessageWithParts(context.Background(), message, parts...); err != nil {
		t.Fatal(err)
	}
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}

	counter := &countingProjectionSource{projectionSource: durable.store}
	durable.projectionSource = counter
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if counter.reads != 0 {
		t.Fatalf("up-to-date warm reconciliation reads = %d, want 0", counter.reads)
	}

	newPart := msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: "prt_new", SessionID: session.ID, MessageID: message.ID},
		Text:     "new",
	}
	if err := durable.store.Write([]string{"part", session.ID, message.ID, newPart.ID}, newPart); err != nil {
		t.Fatal(err)
	}
	counter.reads = 0
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if counter.reads > 3 {
		t.Fatalf("one-record warm reconciliation reads = %d, want at most 3", counter.reads)
	}
	var projected int
	if err := durable.db.QueryRow("SELECT COUNT(*) FROM part WHERE id = 'prt_new'").Scan(&projected); err != nil || projected != 1 {
		t.Fatalf("new projected part = %d, %v; want 1", projected, err)
	}
	durable.Close()
}

func TestDurableReconciliationCrashBetweenLogAndProjectionMatchesRebuild(t *testing.T) {
	workspace := t.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	session, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "crash"})
	if err != nil {
		t.Fatal(err)
	}
	originalMessage := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "msg_before_crash", SessionID: session.ID},
		Time:        msgmodel.TimeCreated{Created: 1}, Agent: "coder",
	}
	originalPart := msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: "prt_removed_during_crash", SessionID: session.ID, MessageID: originalMessage.ID},
		Text:     "removed before its projection event",
	}
	if err := durable.UpdateMessageWithParts(context.Background(), originalMessage, originalPart); err != nil {
		t.Fatal(err)
	}
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := durable.store.Remove([]string{"part", session.ID, originalMessage.ID, originalPart.ID}); err != nil {
		t.Fatal(err)
	}
	message := msgmodel.User{
		MessageBase: msgmodel.MessageBase{ID: "msg_after_crash", SessionID: session.ID},
		Time:        msgmodel.TimeCreated{Created: 10}, Agent: "coder",
	}
	part := msgmodel.TextPart{
		PartBase: msgmodel.PartBase{ID: "prt_after_crash", SessionID: session.ID, MessageID: message.ID},
		Text:     "durable before projection",
	}
	if err := durable.store.WriteBatch([]storage.WriteItem{
		{Key: []string{"part", session.ID, message.ID, part.ID}, Content: part},
		{Key: []string{"message", session.ID, message.ID}, Content: message},
	}); err != nil {
		t.Fatal(err)
	}
	durable.Close()

	recovered, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	recoveredSnapshot := projectionSnapshotJSON(t, recovered)
	recovered.Close()
	removeProjectionDatabase(t, workspace)
	rebuilt, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	if rebuiltSnapshot := projectionSnapshotJSON(t, rebuilt); !bytes.Equal(recoveredSnapshot, rebuiltSnapshot) {
		t.Fatalf("recovered projection differs from full rebuild\nrecovered=%s\nrebuilt=%s", recoveredSnapshot, rebuiltSnapshot)
	}
}

func TestDurableCorruptAndMissingMarksFallBackToFullRebuild(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *durableSessions)
	}{
		{name: "missing", mutate: func(t *testing.T, durable *durableSessions) {
			_, err := durable.db.Exec("DELETE FROM senior_dev_projection_reconcile")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt", mutate: func(t *testing.T, durable *durableSessions) {
			_, err := durable.db.Exec("UPDATE senior_dev_projection_reconcile SET manifest = '{'")
			if err != nil {
				t.Fatal(err)
			}
		}},
		{name: "older-version", mutate: func(t *testing.T, durable *durableSessions) {
			_, err := durable.db.Exec("UPDATE senior_dev_projection_reconcile SET format_version = 0")
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			durable, err := openDurableSessions(context.Background(), workspace)
			if err != nil {
				t.Fatal(err)
			}
			session, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "authoritative"})
			if err != nil {
				t.Fatal(err)
			}
			if err := durable.reconcileProjection(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := durable.db.Exec("UPDATE session SET title = 'stale' WHERE id = ?", session.ID); err != nil {
				t.Fatal(err)
			}
			test.mutate(t, durable)
			counter := &countingProjectionSource{projectionSource: durable.store}
			durable.projectionSource = counter
			if err := durable.reconcileProjection(context.Background()); err != nil {
				t.Fatal(err)
			}
			if counter.reads == 0 {
				t.Fatal("fallback did not read the authoritative flat store")
			}
			var title string
			if err := durable.db.QueryRow("SELECT title FROM session WHERE id = ?", session.ID).Scan(&title); err != nil || title != "authoritative" {
				t.Fatalf("fallback title = %q, %v", title, err)
			}
			reconciledSnapshot := projectionSnapshotJSON(t, durable)
			durable.Close()
			removeProjectionDatabase(t, workspace)
			rebuilt, err := openDurableSessions(context.Background(), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer rebuilt.Close()
			if got := projectionSnapshotJSON(t, rebuilt); !bytes.Equal(got, reconciledSnapshot) {
				t.Fatalf("fallback differs from full rebuild\nfallback=%s\nrebuilt=%s", reconciledSnapshot, got)
			}
		})
	}
}

func TestDurableManualProjectionDeletionInvalidatesMark(t *testing.T) {
	workspace := t.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	session, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "restore me"})
	if err != nil {
		t.Fatal(err)
	}
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := durable.db.Exec("DELETE FROM session WHERE id = ?", session.ID); err != nil {
		t.Fatal(err)
	}
	counter := &countingProjectionSource{projectionSource: durable.store}
	durable.projectionSource = counter
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if counter.reads == 0 {
		t.Fatal("manual projection deletion did not invalidate the mark")
	}
	var count int
	if err := durable.db.QueryRow("SELECT COUNT(*) FROM session WHERE id = ?", session.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("restored session rows = %d, %v; want 1", count, err)
	}
	durable.Close()
}

func TestDurableQuarantineForcesFullFallbackAndRefreshesMark(t *testing.T) {
	workspace := t.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	session, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "quarantine"})
	if err != nil {
		t.Fatal(err)
	}
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(durable.store.Dir, "message", session.ID, "msg_bad.json")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte(`{"id":"msg_bad"`), 0o644); err != nil {
		t.Fatal(err)
	}
	counter := &countingProjectionSource{projectionSource: durable.store}
	durable.projectionSource = counter
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if counter.reads < 2 {
		t.Fatalf("quarantine reconciliation reads = %d, want incremental probe plus full fallback", counter.reads)
	}
	if _, err := os.Stat(broken); !os.IsNotExist(err) {
		t.Fatalf("corrupt source still exists: %v", err)
	}
	counter.reads = 0
	if err := durable.reconcileProjection(context.Background()); err != nil {
		t.Fatal(err)
	}
	if counter.reads != 0 {
		t.Fatalf("post-quarantine warm reads = %d, want 0", counter.reads)
	}
	durable.Close()
}

// projectionSnapshotJSON dumps every projector table in primary-key order so
// two databases can be compared for identical content.
func projectionSnapshotJSON(t testing.TB, durable *durableSessions) []byte {
	t.Helper()
	snapshot := map[string][]map[string]any{}
	for _, table := range []string{"session", "message", "part", "session_message"} {
		rows, err := durable.db.QueryContext(context.Background(), "SELECT * FROM "+table+" ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		images := []map[string]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for index := range values {
				targets[index] = &values[index]
			}
			if err := rows.Scan(targets...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			image := map[string]any{}
			for index, column := range columns {
				if raw, ok := values[index].([]byte); ok {
					image[column] = string(raw)
				} else {
					image[column] = values[index]
				}
			}
			images = append(images, image)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		snapshot[table] = images
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func removeProjectionDatabase(t testing.TB, workspace string) {
	t.Helper()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(workspace, ".senior-dev", "senior-dev.db") + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

func BenchmarkDurableProjectionReconciliation(b *testing.B) {
	workspace := b.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		b.Fatal(err)
	}
	projectID := durable.projectID
	durable.Close()
	seedSyntheticProjectionStore(b, workspace, projectID, 100, 100, 1)
	durable, err = openDurableSessions(context.Background(), workspace)
	if err != nil {
		b.Fatal(err)
	}
	defer durable.Close()
	counter := &countingProjectionSource{projectionSource: durable.store}
	durable.projectionSource = counter

	b.Run("cold", func(b *testing.B) {
		counter.reads = 0
		b.ResetTimer()
		for range b.N {
			if _, err := durable.db.Exec("DELETE FROM senior_dev_projection_reconcile"); err != nil {
				b.Fatal(err)
			}
			if err := durable.reconcileProjection(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(counter.reads)/float64(b.N), "record-reads/op")
	})
	b.Run("warm", func(b *testing.B) {
		counter.reads = 0
		b.ResetTimer()
		for range b.N {
			if err := durable.reconcileProjection(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(counter.reads)/float64(b.N), "record-reads/op")
	})
}

func seedSyntheticProjectionStore(
	t testing.TB, workspace, projectID string, sessions, messages, parts int,
) {
	t.Helper()
	root := filepath.Join(workspace, ".senior-dev", "storage")
	for sessionIndex := range sessions {
		sessionID := fmt.Sprintf("ses_%03d", sessionIndex)
		info := sessioncore.Info{
			ID: sessionID, Slug: sessionID, ProjectID: projectID, Directory: workspace,
			Title: sessionID, Version: "test", Time: sessioncore.Time{Created: 1, Updated: 1},
		}
		writeSyntheticProjectionRecord(t, filepath.Join(root, "session", sessionID+".json"), info)
		for messageIndex := range messages {
			messageID := fmt.Sprintf("msg_%03d_%03d", sessionIndex, messageIndex)
			message := msgmodel.User{
				MessageBase: msgmodel.MessageBase{ID: messageID, SessionID: sessionID},
				Time:        msgmodel.TimeCreated{Created: uint64(messageIndex + 1)}, Agent: "coder",
			}
			writeSyntheticProjectionRecord(t,
				filepath.Join(root, "message", sessionID, messageID+".json"), message,
			)
			for partIndex := range parts {
				partID := fmt.Sprintf("prt_%03d_%03d_%03d", sessionIndex, messageIndex, partIndex)
				part := msgmodel.TextPart{
					PartBase: msgmodel.PartBase{ID: partID, SessionID: sessionID, MessageID: messageID},
					Text:     "synthetic projection payload",
				}
				writeSyntheticProjectionRecord(t,
					filepath.Join(root, "part", sessionID, messageID, partID+".json"), part,
				)
			}
		}
	}
}

func writeSyntheticProjectionRecord(t testing.TB, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectorFailureAttributedToOriginatingOperationContract(t *testing.T) {
	// A failed projector write is returned by that mutation and cannot leak
	// into the next operation's result.
	workspace := t.TempDir()
	durable, err := openDurableSessions(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer durable.Close()
	root, err := durable.CreateSession(context.Background(), sessioncore.CreateInput{Title: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := durable.db.Exec(`CREATE TRIGGER reject_bad_message BEFORE INSERT ON message
		WHEN NEW.id = 'bad' BEGIN SELECT RAISE(ABORT, 'bad projection'); END`); err != nil {
		t.Fatal(err)
	}
	message := func(id string) msgmodel.User {
		return msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: root.ID},
			Time:        msgmodel.TimeCreated{Created: 1}, Agent: "coder",
		}
	}
	if err := durable.UpdateMessage(context.Background(), message("bad")); err == nil || !strings.Contains(err.Error(), "message bad") {
		t.Fatalf("bad projection error = %v", err)
	}
	if err := durable.UpdateMessage(context.Background(), message("good")); err != nil {
		t.Fatalf("next operation inherited projector error: %v", err)
	}
}

func TestNewPipelineCreatesAFreshRootSession(t *testing.T) {
	// A new pipeline creates a fresh root session even when durable storage
	// already has one from an earlier run.
	workspace := t.TempDir()
	first := newRuntime(workspace, &capturingBackend{})
	t.Cleanup(first.Close)
	oldRoot, err := first.Create(context.Background(), "", "coder")
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	runner := newPipeline(cliArgs{}, workspace, pipelineDeps{Backend: &capturingBackend{}, Events: newEventWriter(io.Discard)})
	defer runner.runtime.Close()
	if runner.sessionID == oldRoot {
		t.Fatal("the new pipeline reused the newest durable root session")
	}
	if err := runner.runtime.ensureRootSession(context.Background(), runner.sessionID, "second run", "coder"); err != nil {
		t.Fatal(err)
	}
	var roots int
	if err := runner.runtime.durable.db.QueryRow("SELECT COUNT(*) FROM session WHERE parent_id IS NULL").Scan(&roots); err != nil || roots != 2 {
		t.Fatalf("root sessions = %d, %v; want 2", roots, err)
	}
}

func TestDurableHistoryPreservesInstructionDedup(t *testing.T) {
	// Instruction dedup reads completed read-tool metadata from the durable
	// transcript after a process restart.
	workspace := t.TempDir()
	nested := filepath.Join(workspace, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(nested, "AGENTS.md")
	target := filepath.Join(nested, "target.txt")
	if err := os.WriteFile(rules, []byte("DURABLE NESTED RULE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	arguments, _ := json.Marshal(map[string]string{"filePath": target})
	firstTransport := &scriptedRoundTripper{replies: []string{
		toolCallReply("read", string(arguments)), chatReply("first done", 10),
	}}
	firstRuntime := newRuntime(workspace, &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: firstTransport},
	})
	t.Cleanup(firstRuntime.Close)
	rootID, err := firstRuntime.Create(context.Background(), "", "coder")
	if err != nil {
		firstRuntime.Close()
		t.Fatal(err)
	}
	first, err := runTestTurn(t, firstRuntime, testTurn{
		ParentSessionID: rootID, SessionTitle: "instruction session",
		Agent: "coder", ProviderID: "openrouter",
		ModelID: "vendor/model", Workspace: workspace, Prompt: "read once",
	})
	if err != nil {
		firstRuntime.Close()
		t.Fatal(err)
	}
	firstRuntime.Close()

	secondTransport := &scriptedRoundTripper{replies: []string{
		toolCallReply("read", string(arguments)), chatReply("second done", 10),
	}}
	secondRuntime := newRuntime(workspace, &openRouterBackend{
		apiKey: "test", client: &http.Client{Transport: secondTransport},
	})
	defer secondRuntime.Close()
	// The second runtime is a fresh process against the same session: that is
	// what makes this a durable-transcript test rather than an in-memory one.
	second, err := runTestTurn(t, secondRuntime, testTurn{
		SessionID: first.SessionID, ParentSessionID: rootID,
		SessionTitle: "instruction session", Agent: "coder", ProviderID: "openrouter",
		ModelID: "vendor/model", Workspace: workspace, Prompt: "read again",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("instruction session = %q, want %q", second.SessionID, first.SessionID)
	}
	messages, err := secondRuntime.durable.Messages(context.Background(), second.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	loaded := []int{}
	for _, message := range messages {
		for _, raw := range message.Parts {
			part, ok := raw.(msgmodel.ToolPart)
			if !ok || part.Tool != "read" {
				continue
			}
			state, ok := part.State.(msgmodel.ToolStateCompleted)
			if !ok {
				continue
			}
			field, _ := state.Metadata.Field("loaded")
			var paths []string
			_ = json.Unmarshal(field, &paths)
			loaded = append(loaded, len(paths))
		}
	}
	if len(loaded) != 2 || loaded[0] != 1 || loaded[1] != 0 {
		t.Fatalf("durable instruction loaded metadata = %v, want [1 0]", loaded)
	}
}

func countStoredJSON(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Ext(path) == ".json" {
			count++
		}
		return nil
	})
	return count, err
}
