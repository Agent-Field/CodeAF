package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// deliverableAgent is an agent with a session file, a folder and an index, so
// its writes reach the index under a real session id.
func deliverableAgent(t *testing.T, owned bool) (*Agent, string, string) {
	t.Helper()
	place := newPlace(t, owned)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ArtifactsIndex = index
		config.Place = place
		config.SessionFile = filepath.Join(place.Dir, "transcript.jsonl")
		if owned {
			config.Workspace = place.Work()
		}
	})
	if owned {
		workspace = place.Work()
	}
	if agent.journalID() == "" {
		t.Fatal("the test agent has no session id")
	}
	return agent, workspace, index
}

func writeNewFile(t *testing.T, agent *Agent, path, content string) {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"path": path, "content": content})
	out, bad, err := beltTool(t, agent, "write").Execute(withRunIdentity(context.Background()), args)
	if err != nil || bad {
		t.Fatalf("write %s: %v %s", path, err, out)
	}
}

func listDeliverables(t *testing.T, index string, q DeliverableQuery, limit int) []DeliverableRow {
	t.Helper()
	var rows []DeliverableRow
	cursor := ""
	for pages := 0; ; pages++ {
		scan, err := ListDeliverables(index, q, cursor, limit, time.Time{}, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, scan.Rows...)
		if scan.Incomplete && scan.Next == "" {
			t.Fatal("an incomplete page with no way to continue")
		}
		if scan.Next == "" {
			if scan.Incomplete {
				t.Fatal("history ended but the page says incomplete")
			}
			return rows
		}
		cursor = scan.Next
		if pages > 100000 {
			t.Fatal("paging does not end")
		}
	}
}

// A NEW supported file is a deliverable; an edit, a log and an unsupported
// kind are not. The check goes write tool -> index -> a fresh reader.
func TestWriteSeamRecordsOnlyGenuineCreations(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, false)
	session := agent.journalID()
	writeNewFile(t, agent, "notes.md", "# one\n")
	writeNewFile(t, agent, "flow.mmd", "graph TD; A-->B\n")
	writeNewFile(t, agent, "build.log", "noise\n")
	writeNewFile(t, agent, "main.go", "package main\n")
	writeNewFile(t, agent, "notes.md", "# one edited\n") // an edit

	rows := listDeliverables(t, index, DeliverableQuery{Sessions: map[string]bool{session: true}}, 50)
	if len(rows) != 2 {
		t.Fatalf("want 2 deliverables, got %d: %+v", len(rows), rows)
	}
	byKind := map[string]DeliverableRow{}
	for _, row := range rows {
		byKind[row.Kind] = row
	}
	md := byKind[DeliverableMarkdown]
	if !md.Recorded || !md.Fact.Authored || md.Fact.Origin != DeliverOriginAgent || md.Fact.State != DeliverKept ||
		md.Fact.Kept == nil || md.Fact.Kept.How != KeptCreatedInRepo || md.Fact.Run == "" {
		t.Fatalf("markdown fact = %+v", md.Fact)
	}
	if md.Fact.Turn != "" || md.Fact.Task != "" || len(md.Fact.From) != 0 {
		t.Fatalf("provenance was invented: %+v", md.Fact)
	}
	if byKind[DeliverableMermaid].ID == "" {
		t.Fatal("no mermaid deliverable")
	}
	if want := filepath.Join(canonicalFile(workspace), "notes.md"); md.Artifact.Path != want {
		t.Fatalf("path %q want %q", md.Artifact.Path, want)
	}
	// An owned session's file is a draft, from the start.
	owned, ownedWork, ownedIndex := deliverableAgent(t, true)
	writeNewFile(t, owned, "plan.md", "# plan\n")
	got := listDeliverables(t, ownedIndex, DeliverableQuery{}, 50)
	if len(got) != 1 || got[0].Fact.State != DeliverDraft || got[0].Fact.Kept != nil {
		t.Fatalf("owned deliverable = %+v", got)
	}
	_ = ownedWork
}

// An update supersedes its base across pages and a removed row is gone; a
// listing over more than the newest-2000 window is complete.
func TestListingIsCompleteAcrossLongHistoryAndSupersession(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 2105; i++ {
		RecordArtifact(index, Artifact{
			Path: "/x/file" + strconv.Itoa(i) + ".png", Session: "0123456789abcdef", Title: "file" + strconv.Itoa(i) + ".png",
			Kind: "export", Created: base.Add(time.Duration(i) * time.Second),
		})
	}
	rows := listDeliverables(t, index, DeliverableQuery{}, 100)
	if len(rows) != 2105 {
		t.Fatalf("listed %d of 2105", len(rows))
	}
	if rows[0].Recorded || rows[0].Fact.State != "" || rows[0].Fact.Turn != "" {
		t.Fatalf("a legacy row was given provenance: %+v", rows[0])
	}
	oldest := rows[len(rows)-1]
	if !AppendDeliverableUpdate(index, oldest, func(f *DeliverableFact) { f.State = DeliverKept }) {
		t.Fatal("update not written")
	}
	rows = listDeliverables(t, index, DeliverableQuery{}, 100)
	if len(rows) != 2105 {
		t.Fatalf("an update must not add a row: %d", len(rows))
	}
	// the oldest row is now listed first (newest row of its id), kept.
	found := 0
	for _, row := range rows {
		if row.ID == oldest.ID {
			found++
			if row.Fact.State != DeliverKept {
				t.Fatalf("latest projection lost: %+v", row.Fact)
			}
		}
	}
	if found != 1 {
		t.Fatalf("id listed %d times", found)
	}
	got, err := FindDeliverable(index, "0123456789abcdef", oldest.ID, time.Time{})
	if err != nil || got.Fact.State != DeliverKept {
		t.Fatalf("find: %v %+v", err, got.Fact)
	}
	state := listDeliverables(t, index, DeliverableQuery{States: map[string]bool{DeliverDraft: true}}, 100)
	if len(state) != 0 {
		t.Fatalf("a stale base matched a state filter: %d", len(state))
	}
	AppendDeliverableUpdate(index, got, func(f *DeliverableFact) { f.Removed = true })
	if rows = listDeliverables(t, index, DeliverableQuery{}, 100); len(rows) != 2104 {
		t.Fatalf("removed row listed: %d", len(rows))
	}
	if _, err := os.Stat(index); err != nil {
		t.Fatal(err)
	}
}

func seedDeliverables(t *testing.T, index string, n int) []string {
	t.Helper()
	var ids []string
	for i := 0; i < n; i++ {
		id := "dlv_" + fmt.Sprintf("%032x", i+1)
		ids = append(ids, id)
		if !RecordArtifact(index, Artifact{
			Path: "/work/f" + strconv.Itoa(i) + ".md", Session: "agent", Title: "f" + strconv.Itoa(i) + ".md", Kind: DeliverableMarkdown,
			Created: time.Unix(int64(i+1), 0), Deliver: &DeliverableFact{ID: id, State: DeliverDraft, Authored: true},
		}) {
			t.Fatal("seed append failed")
		}
	}
	return ids
}

// A Keep of OLD work is an update and must not make it the newest creation.
func TestAnUpdateNeverReordersCreations(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 2)
	old, err := FindDeliverable(index, "agent", ids[0], time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	AppendDeliverableUpdate(index, old, func(f *DeliverableFact) { f.State = DeliverKept })
	scan, err := ListDeliverables(index, DeliverableQuery{}, "", 1, time.Time{}, 0, nil)
	if err != nil || len(scan.Rows) != 1 || scan.Rows[0].ID != ids[1] || scan.Next == "" {
		t.Fatalf("page 1 = %+v %v", scan.Rows, err)
	}
	all := listDeliverables(t, index, DeliverableQuery{}, 1)
	if len(all) != 2 || all[0].ID != ids[1] || all[1].ID != ids[0] || all[1].Fact.State != DeliverKept {
		t.Fatalf("all = %+v", all)
	}
}

// An update that lands BETWEEN pages loses no deliverable: a listing is a
// snapshot, so every page agrees, and a fresh listing sees the update.
func TestAnUpdateBetweenPagesLosesNoRow(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 10)
	first, err := ListDeliverables(index, DeliverableQuery{}, "", 5, time.Time{}, 0, nil)
	if err != nil || len(first.Rows) != 5 || first.Next == "" {
		t.Fatalf("first %+v %v", first, err)
	}
	oldest, _ := FindDeliverable(index, "agent", ids[0], time.Time{})
	AppendDeliverableUpdate(index, oldest, func(f *DeliverableFact) { f.State = DeliverKept })
	second, err := ListDeliverables(index, DeliverableQuery{}, first.Next, 5, time.Time{}, 0, nil)
	if err != nil || second.Next != "" {
		t.Fatalf("second %+v %v", second, err)
	}
	seen := map[string]DeliverableRow{}
	for _, row := range append(first.Rows, second.Rows...) {
		seen[row.ID] = row
	}
	if len(seen) != 10 {
		t.Fatalf("walk saw %d of 10", len(seen))
	}
	if seen[ids[0]].Fact.State != DeliverDraft {
		t.Fatal("the snapshot saw an update appended after it began")
	}
	fresh := listDeliverables(t, index, DeliverableQuery{}, 5)
	if len(fresh) != 10 || fresh[9].Fact.State != DeliverKept {
		t.Fatalf("a fresh listing missed the update: %d", len(fresh))
	}
}

// Past the tracker's bound the answer is a typed refusal, never rows that might
// be superseded or removed.
func TestTrackerBoundIsATypedRefusalNotAWrongList(t *testing.T) {
	old := deliverTrackerMax
	resetTrackers(t)
	deliverTrackerMax = 3
	defer func() { deliverTrackerMax = old; resetTrackers(t) }()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 6)
	for _, id := range ids {
		row, err := FindDeliverable(index, "agent", id, time.Time{})
		if err != nil && !errors.Is(err, ErrDeliverIndexTooLarge) {
			t.Fatal(err)
		}
		if err == nil {
			AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.Removed = true })
		}
	}
	scan, err := ListDeliverables(index, DeliverableQuery{}, "", 100, time.Time{}, 0, nil)
	if !errors.Is(err, ErrDeliverIndexTooLarge) || len(scan.Rows) != 0 {
		t.Fatalf("rows=%d err=%v", len(scan.Rows), err)
	}
	if _, err := FindDeliverable(index, "agent", ids[0], time.Time{}); !errors.Is(err, ErrDeliverIndexTooLarge) {
		t.Fatalf("find: %v", err)
	}
}

// A permitted-size update is read: a Removed row with a full From stays removed.
func TestAMaximumSizeUpdateIsStillSeen(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	var from []string
	for i := 0; i < 40; i++ {
		from = append(from, strings.Repeat("x", deliverFromBytes))
	}
	if got := boundedFrom(from); len(got) != deliverFromMax {
		t.Fatalf("bounded from = %d", len(got))
	}
	if got := boundedFrom([]string{strings.Repeat("x", deliverFromBytes+1)}); len(got) != 0 {
		t.Fatal("an over-long input was cut instead of dropped")
	}
	ids := seedDeliverables(t, index, 1)
	row, _ := FindDeliverable(index, "agent", ids[0], time.Time{})
	big := boundedFrom(from)
	if !AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.From = big; f.Removed = true }) {
		t.Fatal("a maximum-size update was refused")
	}
	if rows := listDeliverables(t, index, DeliverableQuery{}, 10); len(rows) != 0 {
		t.Fatalf("a removed row listed: %d", len(rows))
	}
	// An update past the bound is refused, not written where readers skip it.
	if AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.From = []string{strings.Repeat("q", deliverRowMax)} }) {
		t.Fatal("an oversize update was written")
	}
	// An oversize creation degrades to the plain legacy row: unknown, not false.
	plain := Artifact{Path: "/w/big.md", Session: "agent", Title: "big.md", Kind: DeliverableMarkdown, Created: time.Now(),
		Deliver: &DeliverableFact{ID: NewDeliverableID(), State: DeliverKept, From: []string{strings.Repeat("q", deliverRowMax)}}}
	if !RecordArtifact(index, plain) {
		t.Fatal("creation lost")
	}
	rows := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 1 || rows[0].Recorded || rows[0].Fact.State != "" {
		t.Fatalf("degraded row = %+v", rows)
	}
}

// The catch-up gives up at its byte budget with progress kept, and a caller
// waiting behind another honors its own deadline.
func TestTrackerCatchUpIsResumableAndHonorsDeadlines(t *testing.T) {
	oldBudget, oldChunk := deliverSyncBudget, deliverSyncChunk
	deliverSyncBudget, deliverSyncChunk = 2048, 512
	defer func() { deliverSyncBudget, deliverSyncChunk = oldBudget, oldChunk }()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 40)
	for _, id := range ids[:8] {
		row, _ := FindDeliverableAfter(t, index, id)
		AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.Removed = true })
	}
	forgetTracker(index) // a fresh process: nothing is known yet
	var scan DeliverableScan
	var err error
	busy := 0
	for i := 0; i < 200; i++ {
		scan, err = ListDeliverables(index, DeliverableQuery{}, "", 100, time.Time{}, 0, nil)
		if !errors.Is(err, ErrMediaBusy) {
			break
		}
		busy++
	}
	if err != nil || busy == 0 || len(scan.Rows) != 32 {
		t.Fatalf("busy=%d rows=%d err=%v", busy, len(scan.Rows), err)
	}
	// someone else holds the catch-up: a short deadline answers busy promptly
	tracker, release, err := leaseTracker(index, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	tracker.sem <- struct{}{}
	started := time.Now()
	_, err = ListDeliverables(index, DeliverableQuery{}, "", 10, time.Now().Add(30*time.Millisecond), 0, nil)
	<-tracker.sem
	release()
	if !errors.Is(err, ErrMediaBusy) || time.Since(started) > 2*time.Second {
		t.Fatalf("waiter: %v after %v", err, time.Since(started))
	}
}

// FindDeliverableAfter finds with retries over a deliberately tiny budget.
func FindDeliverableAfter(t *testing.T, index, id string) (DeliverableRow, error) {
	t.Helper()
	for i := 0; i < 500; i++ {
		row, err := FindDeliverable(index, "agent", id, time.Time{})
		if !errors.Is(err, ErrMediaBusy) {
			return row, err
		}
	}
	t.Fatal("never caught up")
	return DeliverableRow{}, nil
}

// Two writers racing to create ONE path make ONE authored deliverable: the
// exclusive create inside the write tool's queue decides who created it.
func TestConcurrentWritersToOnePathRecordOneCreation(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, false)
	session := agent.journalID()
	const files = 40
	var wg sync.WaitGroup
	for i := 0; i < files; i++ {
		for _, body := range []string{"# first writer\n", "# second writer, longer\n"} {
			wg.Add(1)
			go func(i int, body string) {
				defer wg.Done()
				args, _ := json.Marshal(map[string]any{"path": "doc" + strconv.Itoa(i) + ".md", "content": body})
				beltTool(t, agent, "write").Execute(withRunIdentity(context.Background()), args)
			}(i, body)
		}
	}
	wg.Wait()
	rows := listDeliverables(t, index, DeliverableQuery{Sessions: map[string]bool{session: true}}, 100)
	if len(rows) != files {
		t.Fatalf("%d creations for %d files", len(rows), files)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row.Artifact.Path] {
			t.Fatalf("%s recorded twice", row.Artifact.Path)
		}
		seen[row.Artifact.Path] = true
		sum := row.Fact.File
		if sum == nil || sum.SHA256 == "" || row.Fact.Version != "sha256:"+sum.SHA256 {
			t.Fatalf("fact %+v", row.Fact)
		}
	}
	_ = workspace
}

// A file that already exists - made by anyone, before the call - is an edit.
func TestAnOverwriteOfAPrecreatedFileRecordsNothing(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, false)
	if err := os.WriteFile(filepath.Join(workspace, "mine.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeNewFile(t, agent, "mine.md", "# written over\n")
	if rows := listDeliverables(t, index, DeliverableQuery{}, 10); len(rows) != 0 {
		t.Fatalf("an overwrite of someone's empty file became a deliverable: %+v", rows)
	}
}

// forgetTracker drops an index's tracker as a process restart would, giving its
// share of the process-wide bound back.
func forgetTracker(index string) {
	r := deliverRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	if t := r.live[index]; t != nil && t.leases == 0 {
		r.evictLocked(t)
	}
}

// peekTracker is index's registered tracker, without a lease, for a test to
// look into; nil when none is registered.
func peekTracker(index string) *deliverTracker {
	r := deliverRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.live[index]
}

// The bound is the process's, across indexes: two small indexes together pass a
// limit neither passes alone.
func TestTrackerBoundIsAggregateAcrossIndexes(t *testing.T) {
	old := deliverTrackerMax
	resetTrackers(t)
	deliverTrackerMax = 4
	defer func() { deliverTrackerMax = old; resetTrackers(t) }()
	for n := 0; n < 2; n++ {
		index := filepath.Join(t.TempDir(), "artifacts.jsonl")
		ids := seedDeliverables(t, index, 3)
		for _, id := range ids {
			row, err := FindDeliverable(index, "agent", id, time.Time{})
			if err == nil {
				AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.State = DeliverKept })
			}
		}
		_, err := ListDeliverables(index, DeliverableQuery{}, "", 10, time.Time{}, 0, nil)
		if n == 0 && err != nil {
			t.Fatalf("first index within the bound: %v", err)
		}
		if n == 1 && !errors.Is(err, ErrDeliverIndexTooLarge) {
			t.Fatalf("second index passed the process-wide bound: %v", err)
		}
	}
}

// A row that spans three or more catch-up chunks keeps its true offset.
func TestARowSpanningManyChunksKeepsItsOffset(t *testing.T) {
	oldChunk := deliverSyncChunk
	deliverSyncChunk = 64
	defer func() { deliverSyncChunk = oldChunk }()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 3)
	row, _ := FindDeliverableAfter(t, index, ids[0])
	if !AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.State = DeliverKept; f.From = []string{strings.Repeat("p", 500)} }) {
		t.Fatal("update not written")
	}
	forgetTracker(index)
	rows := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if r.ID == ids[0] && r.Fact.State != DeliverKept {
			t.Fatalf("the multi-chunk update was lost: %+v", r.Fact)
		}
	}
}

// A newline-free run longer than one call's budget is passed a budget at a
// time: the skip is remembered between calls, so the catch-up converges in a
// bounded number of busy answers and the removal after the run is honoured.
func TestARunLongerThanOneCallsBudgetIsPassedNotRestarted(t *testing.T) {
	oldBudget := deliverSyncBudget
	deliverSyncBudget = 1 << 20
	defer func() { deliverSyncBudget = oldBudget }()
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	f, err := os.Create(index)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(3 << 20)
	f.WriteAt([]byte("\n"), 3<<20)
	f.Close()
	id := "dlv_" + strings.Repeat("a", 32)
	base := Artifact{Path: "/work/result.md", Session: "agent", Title: "result.md", Kind: DeliverableMarkdown, Created: time.Now(), Deliver: &DeliverableFact{ID: id, State: DeliverDraft}}
	RecordArtifact(index, base)
	gone := *base.Deliver
	gone.Update, gone.Removed = true, true
	base.Deliver = &gone
	RecordArtifact(index, base)
	busy := 0
	for {
		page, err := ListDeliverables(index, DeliverableQuery{}, "", 100, time.Now().Add(time.Second), 4<<20, nil)
		if errors.Is(err, ErrMediaBusy) {
			if busy++; busy > 4 {
				t.Fatalf("still busy after %d calls over a 3 MiB run with a 1 MiB budget", busy)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range page.Rows {
			if r.ID == id {
				t.Fatal("the removed row came back")
			}
		}
		return
	}
}

// A row still being written when a listing's snapshot was taken is not read as
// though the file ended there: it waits, and is read whole once its newline
// lands, so neither half is mistaken for a row of its own.
func TestARowHalfWrittenAtTheSnapshotIsReadWhenItEnds(t *testing.T) {
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, index, 2)
	row, _ := FindDeliverableAfter(t, index, ids[0])
	scratch := filepath.Join(t.TempDir(), "artifacts.jsonl")
	RecordArtifact(scratch, Artifact{Path: row.Artifact.Path, Session: row.Artifact.Session, Title: row.Artifact.Title, Kind: row.Artifact.Kind, Created: row.Artifact.Created, Deliver: &DeliverableFact{ID: row.ID, Update: true, State: DeliverKept}})
	line, err := os.ReadFile(scratch)
	if err != nil || len(line) < 10 {
		t.Fatalf("scratch row: %v", err)
	}
	f, err := os.OpenFile(index, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	half := len(line) / 2
	f.Write(line[:half])
	listDeliverables(t, index, DeliverableQuery{}, 10)
	f.Write(line[half:])
	for _, r := range listDeliverables(t, index, DeliverableQuery{}, 10) {
		if r.ID == ids[0] && r.Fact.State != DeliverKept {
			t.Fatalf("the update finished after a snapshot was lost: %+v", r.Fact)
		}
	}
}

// Two indexes under one process-wide bound: a batch the bound refuses changes
// nothing, so when the other index lets go the same chunk is read once more and
// every id's offsets stay strictly ascending with no row counted twice.
func TestARefusedBatchLeavesTheTrackerAsItWas(t *testing.T) {
	old := deliverTrackerMax
	resetTrackers(t)
	deliverTrackerMax = 4
	defer func() { deliverTrackerMax = old; resetTrackers(t) }()
	first := filepath.Join(t.TempDir(), "artifacts.jsonl")
	for _, id := range seedDeliverables(t, first, 2) {
		row, _ := FindDeliverableAfter(t, first, id)
		AppendDeliverableUpdate(first, row, func(f *DeliverableFact) { f.State = DeliverKept })
	}
	if _, err := ListDeliverables(first, DeliverableQuery{}, "", 10, time.Time{}, 0, nil); err != nil {
		t.Fatalf("first index within the bound: %v", err)
	}
	second := filepath.Join(t.TempDir(), "artifacts.jsonl")
	ids := seedDeliverables(t, second, 2)
	for i, id := range []string{ids[0], ids[1], ids[0]} {
		row, err := FindDeliverableAfter(t, second, id)
		if err != nil {
			t.Fatal(err)
		}
		state := DeliverKept
		if i == 2 {
			state = DeliverDraft
		}
		AppendDeliverableUpdate(second, row, func(f *DeliverableFact) { f.State = state })
		forgetTracker(second)
	}
	if _, err := ListDeliverables(second, DeliverableQuery{}, "", 10, time.Time{}, 0, nil); !errors.Is(err, ErrDeliverIndexTooLarge) {
		t.Fatalf("second index passed the process-wide bound: %v", err)
	}
	tracker := peekTracker(second)
	tracker.mu.RLock()
	count, pos := tracker.count, tracker.scan.pos
	tracker.mu.RUnlock()
	if count != 0 || pos != 0 || deliverTracked.Load() != 2 {
		t.Fatalf("a refused batch changed the tracker: count %d pos %d process %d", count, pos, deliverTracked.Load())
	}
	forgetTracker(first)
	rows := listDeliverables(t, second, DeliverableQuery{}, 10)
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	if tracker.count != 3 || deliverTracked.Load() != 3 || len(tracker.updates[ids[0]]) != 2 {
		t.Fatalf("count %d process %d updates of the twice-changed row %d", tracker.count, deliverTracked.Load(), len(tracker.updates[ids[0]]))
	}
	if list := tracker.updates[ids[0]]; list[0].off >= list[1].off {
		t.Fatalf("offsets out of order: %+v", list)
	}
	for _, r := range rows {
		if r.ID == ids[0] && r.Fact.State != DeliverDraft {
			t.Fatalf("the latest update is not the one answered: %+v", r.Fact)
		}
	}
}

// appendRawRows writes index lines exactly as given, as a damaged or foreign
// writer could, without any of RecordArtifact's checks.
func appendRawRows(t *testing.T, index string, rows ...Artifact) {
	t.Helper()
	f, err := os.OpenFile(index, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, row := range rows {
		line, _ := json.Marshal(row)
		f.Write(append(line, '\n'))
	}
}

// An id of no shape this engine issues is refused alike in a creation and in
// an update: a long one costs the tracker nothing, never counts against the
// process bound and never supersedes a real row, and the writer will not write
// one. Two indexes full of them still share the bound only through their real
// updates.
func TestAMalformedIDIsRefusedAlikeInBaseAndUpdate(t *testing.T) {
	old := deliverTrackerMax
	resetTrackers(t)
	deliverTrackerMax = 6
	defer func() { deliverTrackerMax = old; resetTrackers(t) }()
	for n := 0; n < 2; n++ {
		index := filepath.Join(t.TempDir(), "artifacts.jsonl")
		ids := seedDeliverables(t, index, 3)
		for _, id := range ids {
			row, _ := FindDeliverableAfter(t, index, id)
			AppendDeliverableUpdate(index, row, func(f *DeliverableFact) { f.State = DeliverKept })
		}
		forgetTracker(index)
		long := "dlv_" + strings.Repeat("f", 60<<10)
		base := Artifact{Path: "/work/odd.md", Session: "agent", Title: "odd.md", Kind: DeliverableMarkdown, Created: time.Unix(5, 0), Deliver: &DeliverableFact{ID: long, State: DeliverDraft}}
		appendRawRows(t, index, base)
		for k := 0; k < 50; k++ {
			gone := Artifact{Path: "/work/x.md", Session: "agent", Title: "x.md", Kind: DeliverableMarkdown, Deliver: &DeliverableFact{ID: ids[0] + strings.Repeat("0", 1000+k), Update: true, Removed: true}}
			appendRawRows(t, index, gone)
		}
		rows := listDeliverables(t, index, DeliverableQuery{}, 10)
		if len(rows) != 3 {
			t.Fatalf("index %d: %d rows, want the 3 real ones", n, len(rows))
		}
		for _, r := range rows {
			if len(r.ID) > 36 || r.Fact.State != DeliverKept {
				t.Fatalf("index %d: a malformed row was shown or superseded a real one: %s %+v", n, r.ID[:min(len(r.ID), 40)], r.Fact)
			}
		}
		tracker := peekTracker(index)
		tracker.mu.RLock()
		count, keys := tracker.count, len(tracker.updates)
		tracker.mu.RUnlock()
		if count != 3 || keys != 3 {
			t.Fatalf("index %d: tracker holds %d updates under %d ids, want 3 under 3", n, count, keys)
		}
		if _, ok := recordArtifactRow(index, Artifact{Path: "/work/y.md", Session: "agent", Title: "y.md", Kind: DeliverableMarkdown, Deliver: &DeliverableFact{ID: long, Update: true}}); ok {
			t.Fatal("an update under a malformed id was written")
		}
		if written, ok := recordArtifactRow(index, base); !ok || written.Deliver != nil {
			t.Fatalf("a creation under a malformed id kept its fact: %v %+v", ok, written.Deliver)
		}
	}
	if got := deliverTracked.Load(); got != 6 {
		t.Fatalf("the process bound counts %d updates across two indexes, want 6", got)
	}
}
