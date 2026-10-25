package session

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestOverwritePrefersAuthoredIdentityOverNewerShare(t *testing.T) {
	w := newShareWorld(t)
	writeNewFile(t, w.agent, "plan.md", "original authored bytes")
	authored := listDeliverables(t, w.index, DeliverableQuery{}, 10)[0]
	result, bad := w.share(t, w.shareCall(t, "share-plan"), "share-plan", "plan.md")
	if bad || len(result.RecordedIDs) != 1 || result.RecordedIDs[0] == authored.ID {
		t.Fatalf("share did not create an independent identity: %+v", result)
	}
	shared, err := FindDeliverable(w.index, w.session, result.RecordedIDs[0], time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	writeNewFile(t, w.agent, "plan.md", "new authored bytes")
	updated, err := FindDeliverable(w.index, w.session, authored.ID, time.Time{})
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("new authored bytes")))
	if err != nil || updated.Fact.File.SHA256 != want || !updated.Fact.Authored || !updated.Artifact.Created.Equal(authored.Artifact.Created) {
		t.Fatalf("authored identity was not refreshed: %+v, %v", updated.Fact, err)
	}
	after, err := FindDeliverable(w.index, w.session, shared.ID, time.Time{})
	if err != nil || !reflect.DeepEqual(after.Fact, shared.Fact) {
		t.Fatalf("overwrite changed independent share: %+v, %v", after.Fact, err)
	}
	if rows := listDeliverables(t, w.index, DeliverableQuery{}, 10); len(rows) != 2 {
		t.Fatalf("overwrite minted another identity: %d rows", len(rows))
	}
}

func TestKeepMergesOntoConcurrentWrittenRefresh(t *testing.T) {
	agent, workspace, index := deliverableAgent(t, true)
	original := wavFile(1, 8, 4, 4)
	writeNewFile(t, agent, "voice.wav", string(original))
	base := listDeliverables(t, index, DeliverableQuery{}, 10)[0]
	published, resume := make(chan struct{}), make(chan struct{})
	old := keepAfterPublish
	keepAfterPublish = func(string) { close(published); <-resume }
	defer func() { keepAfterPublish = old }()
	type result struct {
		row  DeliverableRow
		path string
		err  error
	}
	done := make(chan result, 1)
	dest, home := t.TempDir(), t.TempDir()
	go func() {
		row, path, err := KeepDeliverable(t.Context(), index, base, []string{workspace}, home, KeepRequest{Dir: dest})
		done <- result{row, path, err}
	}()
	select {
	case <-published:
	case early := <-done:
		t.Fatalf("Keep stopped before publishing: %v", early.err)
	}
	// Keep has copied A and is paused outside the ledger lock. Record B fully
	// before letting Keep update the same id, without scheduling assumptions.
	current := wavFile(1, 8, 8, 8)
	writeNewFile(t, agent, "voice.wav", string(current))
	refreshed, err := FindDeliverable(index, agent.journalID(), base.ID, time.Time{})
	close(resume)
	kept := <-done
	if err != nil || kept.err != nil {
		t.Fatalf("refresh: %v, Keep: %v", err, kept.err)
	}
	latest, err := FindDeliverable(index, agent.journalID(), base.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []DeliverableRow{kept.row, latest} {
		if !reflect.DeepEqual(row.Fact.File, refreshed.Fact.File) || row.Fact.Version != refreshed.Fact.Version || !reflect.DeepEqual(row.Artifact.Media, refreshed.Artifact.Media) || row.Fact.Run != refreshed.Fact.Run {
			t.Fatalf("Keep restored stale source metadata: got %+v, want %+v", row.Fact, refreshed.Fact)
		}
		wantCopy := fmt.Sprintf("%x", sha256.Sum256(original))
		if row.Fact.State != DeliverKept || row.Fact.Kept == nil || row.Fact.Kept.SHA256 != wantCopy || row.Fact.Kept.Path != kept.path {
			t.Fatalf("Keep lost its own copy metadata: %+v", row.Fact)
		}
	}
	if copied, err := os.ReadFile(kept.path); err != nil || string(copied) != string(original) {
		t.Fatalf("kept copy changed: %q, %v", copied, err)
	}
	preview := latest.Artifact
	preview.Deliver = nil
	id, ok := MediaSourceOf(preview)
	if !ok {
		t.Fatal("refreshed source has no preview")
	}
	media, err := FindMedia(index, agent.journalID(), id, time.Time{})
	want := fmt.Sprintf("%x", sha256.Sum256(current))
	if err != nil || media.Fact.SHA256 != want {
		t.Fatalf("stale preview: %+v, %v", media.Fact, err)
	}
	listed, err := ListMedia(index, agent.journalID(), "", 10, time.Time{})
	if err != nil || len(listed.Rows) != 2 {
		t.Fatalf("Keep duplicated a media source: %d rows, %v", len(listed.Rows), err)
	}
}

func TestConcurrentAliasWritesHaveOneDeliverable(t *testing.T) {
	for _, relative := range []string{"new.md", filepath.Join("newdir", "new.md")} {
		t.Run(relative, func(t *testing.T) {
			agent, workspace, index := deliverableAgent(t, false)
			real := filepath.Join(workspace, "real")
			if err := os.Mkdir(real, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, filepath.Join(workspace, "link")); err != nil {
				t.Fatal(err)
			}
			old, registry := writtenBeforeRecord, deliverRegistry
			defer func() { writtenBeforeRecord, deliverRegistry = old, registry }()
			write := beltTool(t, agent, "write")
			synctest.Test(t, func(t *testing.T) {
				// Registry notifications and tracker channels must stay in the bubble.
				deliverRegistry = &trackerRegistry{live: map[string]*deliverTracker{}, freed: make(chan struct{})}
				defer forgetTracker(index)
				firstWritten, resume := make(chan struct{}), make(chan struct{})
				var records atomic.Int32
				writtenBeforeRecord = func() {
					if records.Add(1) == 1 {
						close(firstWritten)
						<-resume
					}
				}
				done := make(chan error, 2)
				run := func(path, content string) {
					args, _ := json.Marshal(map[string]string{"path": path, "content": content})
					out, bad, err := write.Execute(context.Background(), args)
					if bad && err == nil {
						err = fmt.Errorf("write: %s", out)
					}
					done <- err
				}
				// The alias is absent when its queue key is chosen. Pause after its
				// disk write, BEFORE the ledger lock, so a separate queue could let
				// the second writer both overwrite and record ahead of the first.
				go run(filepath.Join("link", relative), "first writer")
				<-firstWritten
				go run(filepath.Join("real", relative), "second writer has different bytes")
				synctest.Wait()
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
				path := filepath.Join(canonicalFile(real), relative)
				if rows[0].Artifact.Path != path {
					t.Fatalf("noncanonical path: %s", rows[0].Artifact.Path)
				}
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("%x", sha256.Sum256(content))
				if string(content) != "second writer has different bytes" || rows[0].Fact.File.SHA256 != want {
					t.Fatalf("ledger has stale alias bytes: disk %q, recorded hash %s, want %s", content, rows[0].Fact.File.SHA256, want)
				}
				if live := ResolveDeliverable(rows[0], []string{workspace}); live.State != MediaAvailable {
					t.Fatalf("ledger has stale alias identity: %s", live.State)
				}
			})
		})
	}
}

func TestWrittenGenericFilesSniffMIMEAndPreserveKnownTypes(t *testing.T) {
	for _, test := range []struct {
		name    string
		shared  bool
		content string
		want    string
	}{
		{name: "new PDF", content: "%PDF-1.7\nnew document\n%%EOF\n"},
		{name: "shared PDF overwrite", shared: true, content: "%PDF-1.7\nrevised document\n%%EOF\n"},
		{name: "shared PDF generic binary", shared: true, content: "\x00\x01\x02\x03"},
		{name: "shared PDF generic text", shared: true, content: "unrecognized header"},
		{name: "shared PDF replaced by HTML", shared: true, content: "<!DOCTYPE HTML><html>new document</html>", want: "text/html; charset=utf-8"},
		{name: "new generic binary", content: "\x00\x01\x02\x03", want: "application/octet-stream"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newShareWorld(t)
			var id string
			if test.shared {
				writeNewFile(t, w.agent, "report.pdf", "%PDF-1.7\noriginal\n%%EOF\n")
				id = listDeliverables(t, w.index, DeliverableQuery{}, 10)[0].ID
				result, bad := w.share(t, w.shareCall(t, "share-pdf"), "share-pdf", "report.pdf")
				if bad || len(result.RecordedIDs) != 1 {
					t.Fatalf("share PDF: %+v", result)
				}
			}
			writeNewFile(t, w.agent, "report.pdf", test.content)
			rows := listDeliverables(t, w.index, DeliverableQuery{}, 10)
			wantRows := 1
			if test.shared {
				wantRows = 2
			}
			if len(rows) != wantRows {
				t.Fatalf("PDF rewrite changed identity: %+v", rows)
			}
			row := rows[0]
			if id != "" {
				var err error
				row, err = FindDeliverable(w.index, w.session, id, time.Time{})
				if err != nil {
					t.Fatal(err)
				}
			}
			wantMIME := test.want
			if wantMIME == "" {
				wantMIME = "application/pdf"
			}
			if row.Kind != shareKindFile || row.Fact.File.MIME != wantMIME {
				t.Fatalf("PDF MIME lost: kind %s, MIME %q", row.Kind, row.Fact.File.MIME)
			}
			want := fmt.Sprintf("%x", sha256.Sum256([]byte(test.content)))
			if row.Fact.File.SHA256 != want {
				t.Fatalf("MIME preservation restored old bytes: %+v", row.Fact.File)
			}
		})
	}
}

func TestWorkerDeliverablesRetainCreationOnlyScope(t *testing.T) {
	w := newTaskWorld(t, "task/creation-only-scope")
	w.writeFile(t, "plan.md", "original authored bytes")
	base := w.byTitle(t)["plan.md"]
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Workspace, c.InTask, c.deliverSink = w.worktree, true, w.sink
	})
	mutate := func(tool string, args map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(args)
		if out, bad, err := beltTool(t, worker, tool).Execute(withRunIdentity(t.Context()), raw); err != nil || bad {
			t.Fatalf("worker %s: %v %s", tool, err, out)
		}
	}
	mutate("edit", map[string]any{"path": "plan.md", "oldText": "original", "newText": "edited"})
	mutate("write", map[string]any{"path": "plan.md", "content": "overwritten bytes"})
	mutate("write", map[string]any{"path": "plan.md", "content": "appended bytes", "append": true})
	mutate("write", map[string]any{"path": "source.go", "content": "package main"})
	mutate("write", map[string]any{"path": "voice.wav", "content": string(wavFile(1, 8, 4, 4))})
	rows := w.byTitleAll(t)
	if len(rows) != 1 || rows[0].ID != base.ID || rows[0].Fact.File.SHA256 != base.Fact.File.SHA256 || rows[0].Fact.File.Canonical != "" || rows[0].Artifact.Media != nil {
		t.Fatalf("worker recording changed from creation-only behavior: %+v", rows)
	}
	w.land(t, "task/creation-only-scope")
	if n := w.node.settleDeliverablesIn(t.Context(), mergeMerged); n != 0 {
		t.Fatalf("settled %d worker files after their creation bytes changed", n)
	}
	writeNewFile(t, w.owner, "plan.md", "conversation's own bytes")
	rows = w.byTitleAll(t)
	if len(rows) != 2 {
		t.Fatalf("conversation write consumed a task-owned row: %+v", rows)
	}
	for _, row := range rows {
		if row.ID == base.ID && (row.Fact.State != "" || row.Fact.File.SHA256 != base.Fact.File.SHA256) {
			t.Fatalf("conversation write changed the task row: %+v", row.Fact)
		}
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
