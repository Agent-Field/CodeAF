package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeepMergesStateIntoConcurrentWrite(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, true)
	writeNewFile(t, agent, "voice.wav", string(wavFile(1, 8, 4, 4)))
	base := listDeliverables(t, index, DeliverableQuery{}, 10)[0]
	published, resume := make(chan struct{}), make(chan struct{})
	old := keepAfterPublish
	keepAfterPublish = func(string) { close(published); <-resume }
	defer func() { keepAfterPublish = old }()
	dest, home := t.TempDir(), t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, _, err := KeepDeliverable(context.Background(), index, base, []string{workspace}, home, KeepRequest{Dir: dest})
		done <- err
	}()
	<-published
	writeNewFile(t, agent, "voice.wav", string(append(wavFile(1, 8, 4, 4), 0, 0)))
	fresh := listDeliverables(t, index, DeliverableQuery{}, 10)[0]
	close(resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, err := FindDeliverable(index, agent.journalID(), base.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Fact.Version != fresh.Fact.Version || fmt.Sprint(got.Fact.File) != fmt.Sprint(fresh.Fact.File) || got.Artifact.Media.ID != fresh.Artifact.Media.ID {
		t.Fatalf("Keep restored stale content: got %+v media %s; want %+v media %s", got.Fact.File, got.Artifact.Media.ID, fresh.Fact.File, fresh.Artifact.Media.ID)
	}
	if got.Fact.State != DeliverKept || got.Fact.Kept.Path != filepath.Join(canonicalFile(dest), "voice.wav") {
		t.Fatalf("Keep state lost: %+v", got.Fact)
	}
}

func TestConcurrentAliasWritesHaveOneDeliverable(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, false)
	real := filepath.Join(workspace, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	firstLookup, secondReady, resume := make(chan struct{}), make(chan struct{}), make(chan struct{})
	oldLookup, oldWaiting := writtenAfterLookup, writtenWaiting
	defer func() { writtenAfterLookup, writtenWaiting = oldLookup, oldWaiting }()
	var lookups atomic.Int32
	var ready sync.Once
	writtenAfterLookup = func() {
		if lookups.Add(1) == 1 {
			close(firstLookup)
			<-resume
		} else {
			ready.Do(func() { close(secondReady) })
		}
	}
	writtenWaiting = func() { ready.Do(func() { close(secondReady) }) }
	write := beltTool(t, agent, "write")
	done := make(chan error, 2)
	run := func(path string) {
		args, _ := json.Marshal(map[string]string{"path": path, "content": "same bytes"})
		out, bad, err := write.Execute(context.Background(), args)
		if bad && err == nil {
			err = fmt.Errorf("write: %s", out)
		}
		done <- err
	}
	go run("link/new.md")
	<-firstLookup
	go run("real/new.md")
	<-secondReady
	close(resume)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	rows := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 1 {
		t.Fatalf("alias writes minted %d deliverables", len(rows))
	}
	if rows[0].Artifact.Path != filepath.Join(canonicalFile(real), "new.md") {
		t.Fatalf("noncanonical path: %s", rows[0].Artifact.Path)
	}
}

func TestCheckoutRewriteReusesSettledTaskDeliverable(t *testing.T) {
	const branch = "task/landed-document"
	w := newTaskWorld(t, branch)
	w.writeFile(t, "plan.md", "first")
	base := w.byTitle(t)["plan.md"]
	w.land(t, branch)
	if n := w.node.settleDeliverablesIn(context.Background(), mergeMerged); n != 1 {
		t.Fatalf("settled %d", n)
	}
	writeNewFile(t, w.owner, "plan.md", "second")
	args := []byte(`{"path":"plan.md","oldText":"second","newText":"third"}`)
	if out, bad, err := beltTool(t, w.owner, "edit").Execute(context.Background(), args); err != nil || bad {
		t.Fatalf("edit: %v %s", err, out)
	}
	rows := w.byTitleAll(t)
	if len(rows) != 1 || rows[0].ID != base.ID {
		t.Fatalf("checkout rewrites created new identities: %+v", rows)
	}
	if rows[0].Fact.State != DeliverKept || rows[0].Fact.File.SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("third"))) {
		t.Fatalf("stale checkout fact: %+v", rows[0].Fact)
	}
}

func TestCheckoutWritesRemainSeparateFromUnmergedTask(t *testing.T) {
	w := newTaskWorld(t, "task/unmerged-document")
	w.writeFile(t, "plan.md", "branch first")
	branchID := w.byTitle(t)["plan.md"].ID
	writeNewFile(t, w.owner, "plan.md", "checkout")
	w.writeFile(t, "plan.md", "branch second")
	rows := w.byTitleAll(t)
	if len(rows) != 2 {
		t.Fatalf("checkout and branch conflated: %+v", rows)
	}
	found := false
	for _, row := range rows {
		if row.ID == branchID {
			found = true
			if row.Fact.Branch == "" || row.Fact.File.Canonical != "" {
				t.Fatalf("branch claimed checkout identity: %+v", row.Fact)
			}
		}
	}
	if !found {
		t.Fatal("branch identity lost")
	}
}

func TestInPlaceWorkerOverwritePublishesResolvableMedia(t *testing.T) {
	w := newTaskWorld(t, "task/media-writer")
	data := wavFile(1, 8, 4, 4)
	writeNewFile(t, w.owner, "voice.wav", string(data))
	base := w.byTitle(t)["voice.wav"]
	w.owner.mu.Lock()
	sink := w.owner.deliverSinkLocked(w.checkout, w.node)
	w.owner.mu.Unlock()
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.Workspace = w.checkout; c.InTask = true; c.deliverSink = sink })
	data[len(data)-1] = 1
	writeNewFile(t, worker, "voice.wav", string(data))
	rows := w.byTitleAll(t)
	if len(rows) != 1 || rows[0].ID != base.ID {
		t.Fatalf("worker changed identity: %+v", rows)
	}
	preview := rows[0].Artifact
	preview.Deliver = nil
	id, ok := MediaSourceOf(preview)
	if !ok {
		t.Fatal("no preview advertised")
	}
	media, err := FindMedia(w.index, w.sessionID, id, time.Time{})
	if err != nil || media.Fact.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("advertised preview is missing or stale: %+v %v", media.Fact, err)
	}
	if media.Fact.ID == base.Artifact.Media.ID {
		t.Fatal("worker reused stale preview")
	}
	if live := ResolveMedia(media, []string{w.checkout}); live.State != MediaAvailable {
		t.Fatalf("worker preview is %s", live.State)
	}
}

func TestFirstWritesReadHistoryOnce(t *testing.T) {
	agent, _, index := deliverableAgent(t, false)
	for i := 0; i < artifactRows+1; i++ {
		if !RecordArtifact(index, Artifact{Path: "/work/other.md", Session: "other", Title: "other.md", Created: time.Unix(1, 0)}) {
			t.Fatal("seed failed")
		}
	}
	old := writtenRead
	defer func() { writtenRead = old }()
	read := 0
	writtenRead = func(n int) { read += n }
	for i := range 20 {
		writeNewFile(t, agent, fmt.Sprintf("file%d.md", i), "content")
	}
	info, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	if int64(read) > 2*info.Size() {
		t.Fatalf("new paths rescanned history: read %d bytes for %d index bytes", read, info.Size())
	}
	rows := listDeliverables(t, index, DeliverableQuery{Sessions: map[string]bool{agent.journalID(): true}}, 50)
	if len(rows) != 20 {
		t.Fatalf("recorded %d of 20 first writes", len(rows))
	}
}

func TestExpiredWrittenLookupStillRecords(t *testing.T) {
	agent, _, index := deliverableAgent(t, false)
	for i := range 100 {
		if !RecordArtifact(index, Artifact{Path: fmt.Sprintf("/work/other%d.md", i), Session: "other", Title: "other.md", Created: time.Unix(1, 0)}) {
			t.Fatal("seed failed")
		}
	}
	old, oldBudget := writtenDeadline, writtenSyncBudget
	attempts := 0
	writtenSyncBudget = 127
	writtenDeadline = func() time.Time { attempts++; return time.Unix(1, 0) }
	defer func() { writtenDeadline, writtenSyncBudget = old, oldBudget }()
	writeNewFile(t, agent, "late.md", "still recorded")
	rows := listDeliverables(t, index, DeliverableQuery{Sessions: map[string]bool{agent.journalID(): true}}, 10)
	if len(rows) != 1 {
		t.Fatalf("successful write lost its recording after lookup deadline: %d rows", len(rows))
	}
	if attempts < 2 {
		t.Fatalf("fixture did not cross lookup attempts: %d", attempts)
	}
}

// The path index is a cache of the ledger, not an authority that can hide rows
// appended by another writer or survive a replacement of that ledger.
func TestWrittenPathIndexFollowsLedgerChanges(t *testing.T) {
	agent, _, index := deliverableAgent(t, true)
	writeNewFile(t, agent, "plan.md", "first")
	base := listDeliverables(t, index, DeliverableQuery{}, 10)[0]
	// Warm the lookup, then append outside the recording helper.
	writeNewFile(t, agent, "plan.md", "second")
	external := base.Artifact
	fact := base.Fact
	fact.Update, fact.State = true, DeliverKept
	fact.Kept = &DeliverableKept{Path: base.Artifact.Path, How: KeptCreatedInRepo}
	external.Deliver = &fact
	line, err := json.Marshal(external)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(index, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	writeNewFile(t, agent, "plan.md", "third")
	rows := listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 1 || rows[0].ID != base.ID || rows[0].Fact.State != DeliverKept {
		t.Fatalf("external update ignored: %+v", rows)
	}
	replacement := filepath.Join(t.TempDir(), "replacement.jsonl")
	fact.ID, fact.Update, fact.State, fact.Kept = NewDeliverableID(), false, DeliverDraft, nil
	if !RecordArtifact(replacement, external) {
		t.Fatal("replacement seed failed")
	}
	if err := os.Rename(replacement, index); err != nil {
		t.Fatal(err)
	}
	writeNewFile(t, agent, "plan.md", "fourth")
	rows = listDeliverables(t, index, DeliverableQuery{}, 10)
	if len(rows) != 1 || rows[0].ID != fact.ID || rows[0].Fact.State != DeliverDraft {
		t.Fatalf("replaced ledger ignored: %+v", rows)
	}
}
