package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A HELD JOB WHOSE MIRROR HAS NOT CHANGED IS NOT READ AGAIN. The flush runs
// twice a second for every held job; once one found nothing to send, the next
// ones only look at the mirror's file and the manager's journal, until either
// changes, a minute passes, or a send moves what was sent.
func TestAHeldJobWhoseMirrorIsUnchangedIsNotReadAgain(t *testing.T) {
	var reports atomic.Int64
	var cost atomic.Value
	j, now := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report api.WorkerReport
		_ = json.NewDecoder(r.Body).Decode(&report)
		reports.Add(1)
		cost.Store(report.Cost)
		w.WriteHeader(200)
	}))
	talk := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := j.o.Mirror.Update(j.id, func(it *factory.Item) error { it.Talk = talk; return nil }); err != nil {
		t.Fatal(err)
	}
	j.sent, _ = j.o.Mirror.Get(j.id)
	tick := func() {
		t.Helper()
		*now = now.Add(500 * time.Millisecond)
		if !j.flush(context.Background()) {
			t.Fatal("the flush lost the lease")
		}
	}
	full := func() int64 { return fullFlushes.Load() }

	before := full()
	tick()
	if got := full() - before; got != 1 || reports.Load() != 0 {
		t.Fatalf("the first flush read %d times and sent %d, want 1 and 0", got, reports.Load())
	}
	before = full()
	for range 20 {
		tick()
	}
	if got := full() - before; got != 0 || reports.Load() != 0 {
		t.Fatalf("20 flushes of an unchanged job read it %d times and sent %d, want 0 and 0", got, reports.Load())
	}

	// Another process edits the mirror in place: the next flush reads and sends it.
	path := j.o.Mirror.ItemPath(j.id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d map[string]any
	_ = json.Unmarshal(raw, &d)
	d["item"].(map[string]any)["Stream"].(map[string]any)["Spent"] = 0.25
	raw, _ = json.Marshal(d)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	before = full()
	tick()
	if got := full() - before; got != 1 || reports.Load() != 1 || cost.Load() != 0.25 {
		t.Fatalf("an edited mirror: read %d, sent %d at %v; want 1, 1 at 0.25", got, reports.Load(), cost.Load())
	}
	// The send forgot the quiet, so the next flush reads once and is quiet again.
	before = full()
	tick()
	tick()
	if got := full() - before; got != 1 || reports.Load() != 1 {
		t.Fatalf("after a send: read %d, sent %d; want 1 and 1", got, reports.Load())
	}

	// The manager's journal changes: the job is read again.
	if err := os.WriteFile(talk, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before = full()
	tick()
	if got := full() - before; got != 1 {
		t.Fatalf("a changed journal read the job %d times, want 1", got)
	}

	// A quiet is trusted for a minute and no longer.
	before = full()
	for range int(quietTrust/(500*time.Millisecond)) - 1 {
		tick()
	}
	if got := full() - before; got != 0 {
		t.Fatalf("within a minute an unchanged job was read %d times, want 0", got)
	}
	tick()
	if got := full() - before; got != 1 {
		t.Fatalf("a minute on an unchanged job was read %d times, want 1", got)
	}

	// A reply the factory accepted and has not acknowledged is reported at once.
	j.mu.Lock()
	j.accepted["reply-1"] = true
	j.mu.Unlock()
	before = full()
	tick()
	if got := full() - before; got != 1 || reports.Load() != 2 {
		t.Fatalf("an owed delivery: read %d, sent %d; want 1 and 2", got, reports.Load())
	}
}
