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

	"github.com/Agent-Field/codeaf/internal/standing"
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
	Meta         foldersGraphMeta
}

type foldersCollection struct {
	ID, Name string
}

type foldersMembership struct {
	CollectionID, Kind, RefID, SessionID string
}

type foldersJob struct {
	ID, Type, State, CoalesceKey, ChatID string
	EnqueuedAt, StartedAt, CommittedAt   string
	Cursor                               string
}

// foldersGraphMeta records which optional runtime columns the jobs table
// actually has. Missing timing or cursor columns fail F09/F10 rather than
// skipping as a pass — table-exists is not the journey.
type foldersGraphMeta struct {
	HasEnqueuedAt, HasStartedAt, HasCommittedAt, HasCursor bool
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
	cols := jobColumnSet(t, db)
	out.Meta = foldersGraphMeta{
		HasEnqueuedAt:  cols["enqueued_at"],
		HasStartedAt:   cols["started_at"],
		HasCommittedAt: cols["committed_at"],
		HasCursor:      cols["cursor"],
	}
	selectSQL := `SELECT id, type, state, coalesce_key, chat_id`
	if out.Meta.HasEnqueuedAt {
		selectSQL += `, enqueued_at`
	}
	if out.Meta.HasStartedAt {
		selectSQL += `, started_at`
	}
	if out.Meta.HasCommittedAt {
		selectSQL += `, committed_at`
	}
	if out.Meta.HasCursor {
		selectSQL += `, cursor`
	}
	selectSQL += ` FROM jobs`
	jobs, err := db.Query(selectSQL)
	if err == nil {
		defer jobs.Close()
		for jobs.Next() {
			var row foldersJob
			dest := []any{&row.ID, &row.Type, &row.State, &row.CoalesceKey, &row.ChatID}
			if out.Meta.HasEnqueuedAt {
				dest = append(dest, &row.EnqueuedAt)
			}
			if out.Meta.HasStartedAt {
				dest = append(dest, &row.StartedAt)
			}
			if out.Meta.HasCommittedAt {
				dest = append(dest, &row.CommittedAt)
			}
			if out.Meta.HasCursor {
				dest = append(dest, &row.Cursor)
			}
			if err := jobs.Scan(dest...); err != nil {
				t.Fatalf("scan job: %v", err)
			}
			out.Jobs = append(out.Jobs, row)
		}
	}
	return out
}

func jobColumnSet(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("pragma jobs: %v", err)
		}
		out[strings.ToLower(name)] = true
	}
	return out
}

// foldersJobInstants is enqueue/start/commit/visible for one organize job.
// Scheduler delay is start−enqueue. Model latency is commit−start. Visible is
// when the Folders beat draws the new membership — not a store column.
type foldersJobInstants struct {
	Enqueue, Start, Commit, Visible time.Time
	SchedulerDelay, ModelLatency    time.Duration
	Missing                         string
}

func parseJobStamp(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

func jobInstants(job foldersJob, visibleAt time.Time) foldersJobInstants {
	out := foldersJobInstants{Visible: visibleAt}
	var missing []string
	if ts, ok := parseJobStamp(job.EnqueuedAt); ok {
		out.Enqueue = ts
	} else {
		missing = append(missing, "enqueue")
	}
	if ts, ok := parseJobStamp(job.StartedAt); ok {
		out.Start = ts
	} else {
		missing = append(missing, "start")
	}
	if ts, ok := parseJobStamp(job.CommittedAt); ok {
		out.Commit = ts
	} else {
		missing = append(missing, "commit")
	}
	if !out.Enqueue.IsZero() && !out.Start.IsZero() {
		out.SchedulerDelay = out.Start.Sub(out.Enqueue)
	}
	if !out.Start.IsZero() && !out.Commit.IsZero() {
		out.ModelLatency = out.Commit.Sub(out.Start)
	}
	out.Missing = strings.Join(missing, ",")
	return out
}

func (g foldersGraph) collectionID(name string) string {
	for _, c := range g.Collections {
		if c.Name == name {
			return c.ID
		}
	}
	return ""
}

func (g foldersGraph) parentsOf(kind, refID string) []string {
	var out []string
	for _, m := range g.Memberships {
		if m.Kind == kind && m.RefID == refID {
			out = append(out, m.CollectionID)
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

func TestFoldersColumnsTimingsDistinguishSchedulerFromModel(t *testing.T) {
	enqueue := time.Date(2026, 9, 19, 17, 0, 0, 0, time.UTC)
	start := enqueue.Add(800 * time.Millisecond)
	commit := start.Add(12 * time.Second)
	visible := commit.Add(200 * time.Millisecond)
	got := jobInstants(foldersJob{
		EnqueuedAt:  enqueue.Format(time.RFC3339Nano),
		StartedAt:   start.Format(time.RFC3339Nano),
		CommittedAt: commit.Format(time.RFC3339Nano),
	}, visible)
	if got.Missing != "" {
		t.Fatalf("synthetic stamps should parse: missing %s", got.Missing)
	}
	if got.SchedulerDelay != 800*time.Millisecond {
		t.Fatalf("scheduler delay is %s, want 800ms (start−enqueue, not wall-clock)", got.SchedulerDelay)
	}
	if got.ModelLatency != 12*time.Second {
		t.Fatalf("model latency is %s, want 12s (commit−start)", got.ModelLatency)
	}
	if got.SchedulerDelay >= standing.Interval {
		t.Fatal("a sub-second wakeup must not be classified as the five-minute tick")
	}
	tick := jobInstants(foldersJob{
		EnqueuedAt:  enqueue.Format(time.RFC3339),
		StartedAt:   enqueue.Add(standing.Interval).Format(time.RFC3339),
		CommittedAt: enqueue.Add(standing.Interval + time.Second).Format(time.RFC3339),
	}, time.Time{})
	if tick.SchedulerDelay < standing.Interval {
		t.Fatalf("tick-shaped delay %s should be at least standing.Interval", tick.SchedulerDelay)
	}
	blank := jobInstants(foldersJob{}, time.Time{})
	if blank.Missing != "enqueue,start,commit" {
		t.Fatalf("missing instants should be named, got %q — F10 must not pass on an empty jobs table", blank.Missing)
	}
}

func TestFoldersColumnsSharedObjectIsOneIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3", "collections.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store, err := workspace.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	billing, err := store.Create(ctx, "Billing")
	if err != nil {
		t.Fatalf("billing: %v", err)
	}
	receipts, err := store.Create(ctx, "Receipts")
	if err != nil {
		t.Fatalf("receipts: %v", err)
	}
	security, err := store.Create(ctx, "Security")
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	if err := store.Add(ctx, billing.ID, workspace.Ref{Kind: workspace.CollectionKind, ID: receipts.ID}); err != nil {
		t.Fatalf("nest billing: %v", err)
	}
	if err := store.Add(ctx, security.ID, workspace.Ref{Kind: workspace.CollectionKind, ID: receipts.ID}); err != nil {
		t.Fatalf("nest security: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	home := filepath.Dir(filepath.Dir(path))
	graph := readFoldersGraph(t, home, t.TempDir())
	id := graph.collectionID("Receipts")
	if id == "" || id != receipts.ID {
		t.Fatalf("Receipts must be one collections row, got %q want %q", id, receipts.ID)
	}
	parents := graph.parentsOf("collection", receipts.ID)
	if len(parents) != 2 {
		t.Fatalf("J45 two paths need two memberships of the same id, got %v", parents)
	}
	if folderNamed(graph, "Root") || graph.RootIsARow {
		t.Fatal("Root is virtual and must not become a duplicated collections row")
	}
}

func TestFoldersColumnsSurveyCapIsASliceNotAWall(t *testing.T) {
	if foldersEntrySurveyCap != 8 {
		t.Fatalf("F09 survey cap drifted to %d; CONTRACTS.md freezes organizeSurveyCap=8 as a per-lease slice", foldersEntrySurveyCap)
	}
}
