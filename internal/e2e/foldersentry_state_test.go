package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/workspace"

	_ "modernc.org/sqlite"
)

// foldersGraph is a production-state reading of collections.db: the folders
// that exist, the memberships, and the organize jobs. Word-matching the TUI
// is not enough for J36–J42.
type foldersGraph struct {
	Exists       bool
	Collections  []foldersCollection
	Memberships  []foldersMembership
	Jobs         []foldersJob
	RootIsARow   bool
	UnfiledChats []string
}

type foldersCollection struct {
	ID, Name string
}

type foldersMembership struct {
	CollectionID, Kind, RefID, SessionID string
}

type foldersJob struct {
	ID, Type, State, CoalesceKey, ChatID string
}

func collectionsDB(home string) string {
	return filepath.Join(home, "v3", "collections.db")
}

func foldersEntryJobPaint(storeState string) string {
	switch storeState {
	case workspace.JobPending:
		return foldersEntryJobQueued
	case workspace.JobLeased:
		return foldersEntryJobRunning
	case workspace.JobDeferred, workspace.JobFailed:
		return foldersEntryJobDelayed
	case workspace.JobCompleted:
		return foldersEntryJobDone
	case workspace.JobCancelled:
		return foldersEntryJobCancel
	default:
		return ""
	}
}

func readFoldersGraph(t *testing.T, home, ws string) foldersGraph {
	t.Helper()
	out := foldersGraph{UnfiledChats: listConversationIDs(t, home)}
	_ = ws
	path := collectionsDB(home)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return out
		}
		t.Fatalf("stat collections.db: %v", err)
	}
	out.Exists = true
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open collections.db: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, name FROM collections`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var row foldersCollection
			if err := rows.Scan(&row.ID, &row.Name); err != nil {
				t.Fatalf("scan collection: %v", err)
			}
			if strings.EqualFold(row.Name, "Root") || strings.EqualFold(row.ID, "root") {
				out.RootIsARow = true
			}
			out.Collections = append(out.Collections, row)
		}
	}
	mems, err := db.Query(`SELECT collection_id, kind, ref_id, session_id FROM memberships`)
	if err == nil {
		defer mems.Close()
		for mems.Next() {
			var row foldersMembership
			if err := mems.Scan(&row.CollectionID, &row.Kind, &row.RefID, &row.SessionID); err != nil {
				t.Fatalf("scan membership: %v", err)
			}
			out.Memberships = append(out.Memberships, row)
		}
	}
	jobs, err := db.Query(`SELECT id, type, state, coalesce_key, chat_id FROM jobs`)
	if err == nil {
		defer jobs.Close()
		for jobs.Next() {
			var row foldersJob
			if err := jobs.Scan(&row.ID, &row.Type, &row.State, &row.CoalesceKey, &row.ChatID); err != nil {
				t.Fatalf("scan job: %v", err)
			}
			out.Jobs = append(out.Jobs, row)
		}
	}
	return out
}

func listConversationIDs(t *testing.T, home string) []string {
	t.Helper()
	root := filepath.Join(home, "v3", "projects")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read projects: %v", err)
	}
	var ids []string
	for _, project := range entries {
		if !project.IsDir() {
			continue
		}
		chats, err := os.ReadDir(filepath.Join(root, project.Name()))
		if err != nil {
			continue
		}
		for _, chat := range chats {
			if !chat.IsDir() || len(chat.Name()) != 16 {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, project.Name(), chat.Name(), "transcript.jsonl")); err != nil {
				continue
			}
			ids = append(ids, chat.Name())
		}
	}
	return ids
}

func (g foldersGraph) filed(conversationID string) bool {
	for _, m := range g.Memberships {
		if m.Kind == "conversation" && (m.RefID == conversationID || m.SessionID == conversationID) {
			return true
		}
	}
	return false
}

func (g foldersGraph) organizeJobs() []foldersJob {
	var out []foldersJob
	for _, job := range g.Jobs {
		if job.Type == foldersEntryJobType {
			out = append(out, job)
		}
	}
	return out
}

func (g foldersGraph) explicitOrganize() []foldersJob {
	var out []foldersJob
	for _, job := range g.organizeJobs() {
		if job.CoalesceKey == foldersEntryCoalesceKey {
			out = append(out, job)
		}
	}
	return out
}

func folderNamed(g foldersGraph, name string) bool {
	for _, c := range g.Collections {
		if c.Name == name {
			return true
		}
	}
	return false
}

func seedUnfiledChat(t *testing.T, home, ws, id, title, body string) {
	t.Helper()
	if len(id) != 16 {
		t.Fatalf("conversation id %q is not the 16-hex session id the product uses", id)
	}
	dir := filepath.Join(home, "v3", "projects", strings.ReplaceAll(ws, "/", "-"), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed chat dir: %v", err)
	}
	line, err := json.Marshal(map[string]string{
		"type":    "message",
		"role":    "user",
		"content": body,
	})
	if err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), append(line, '\n'), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	meta, err := json.MarshalIndent(map[string]any{
		"id":         id,
		"title":      title,
		"workspace":  ws,
		"lastUserAt": time.Now().UTC().Format(time.RFC3339Nano),
	}, "", " ")
	if err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), append(meta, '\n'), 0o644); err != nil {
		t.Fatalf("write meta: %v", err)
	}
}

func openGraphStore(t *testing.T) *workspace.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestFoldersEntryFreshStoreHasNoGeneratedFolders(t *testing.T) {
	store := openGraphStore(t)
	got, err := store.Collections(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a fresh collections.db already holds folders: %+v", got)
	}
}

func TestFoldersEntryOrganizeJobCoalescesOnTheExplicitKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Create is the store's only door onto a blank file. Organize existing chats
	// still reuses this same jobs table; the coalescing law is independent of
	// whether the graph already holds a folder.
	if _, err := store.Create(ctx, "Billing"); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := store.EnqueueJob(ctx, workspace.Job{
		Type:        workspace.JobOrganize,
		CoalesceKey: foldersEntryCoalesceKey,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if first.Type != foldersEntryJobType {
		t.Fatalf("job type is %q, want %q — there is no second scheduler", first.Type, foldersEntryJobType)
	}
	if first.State != workspace.JobPending {
		t.Fatalf("store state is %q, want pending (person sees queued)", first.State)
	}
	second, err := store.EnqueueJob(ctx, workspace.Job{
		Type:        workspace.JobOrganize,
		CoalesceKey: foldersEntryCoalesceKey,
	})
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("a second Organize existing chats minted %q after %q; J42 is idempotent while queued", second.ID, first.ID)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	again, err := reopened.EnqueueJob(ctx, workspace.Job{
		Type:        workspace.JobOrganize,
		CoalesceKey: foldersEntryCoalesceKey,
	})
	if err != nil {
		t.Fatalf("restart enqueue: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("restart minted %q after %q; J42 resumes the durable job", again.ID, first.ID)
	}
}

func TestFoldersEntryPersonFacingJobWordsNeverPaintStoreStates(t *testing.T) {
	for _, store := range []string{
		workspace.JobPending, workspace.JobLeased, workspace.JobDeferred,
		workspace.JobFailed, workspace.JobCompleted, workspace.JobCancelled,
	} {
		paint := foldersEntryJobPaint(store)
		if paint == "" {
			t.Errorf("store state %q has no person-facing word", store)
			continue
		}
		if paint == foldersEntryChecked {
			t.Errorf("store state %q mapped to banned %q", store, foldersEntryChecked)
		}
		for _, banned := range foldersEntryStoreWords {
			if paint == banned {
				t.Errorf("store state %q is painted as store word %q", store, paint)
			}
		}
	}
	if foldersEntryJobPaint(workspace.JobPending) != foldersEntryJobQueued {
		t.Errorf("pending must paint %q", foldersEntryJobQueued)
	}
	if foldersEntryJobPaint(workspace.JobLeased) != foldersEntryJobRunning {
		t.Errorf("leased must paint %q", foldersEntryJobRunning)
	}
	if foldersEntryJobPaint(workspace.JobDeferred) != foldersEntryJobDelayed {
		t.Errorf("deferred must paint %q", foldersEntryJobDelayed)
	}
	if foldersEntryJobPaint(workspace.JobFailed) != foldersEntryJobDelayed {
		t.Errorf("failed must paint %q", foldersEntryJobDelayed)
	}
	if foldersEntryJobPaint(workspace.JobCompleted) != foldersEntryJobDone {
		t.Errorf("completed must paint %q", foldersEntryJobDone)
	}
	if foldersEntryJobPaint(workspace.JobCancelled) != foldersEntryJobCancel {
		t.Errorf("cancelled must paint %q", foldersEntryJobCancel)
	}
}

func TestFoldersEntryRootIsNeverACollectionsRow(t *testing.T) {
	home := t.TempDir()
	path := collectionsDB(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Create(context.Background(), "Billing"); err != nil {
		_ = store.Close()
		t.Fatalf("create: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	graph := readFoldersGraph(t, home, t.TempDir())
	if graph.RootIsARow {
		t.Fatal("Root was persisted as a collections row; Root is virtual")
	}
	if !folderNamed(graph, "Billing") {
		t.Fatal("the created folder is missing from collections.db")
	}
}
