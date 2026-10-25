package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

func writeIndexRows(t *testing.T, path string, from, to int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	defer func() {
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}()
	for i := from; i < to; i++ {
		row := fmt.Sprintf(`{"id":"%d","sessionId":"s%d","name":"task-%d","label":"task %d","title":"task number %d, groomed","status":"done","endedAt":%q}`+"\n",
			i, i%40, i, i, i, time.Unix(1_700_000_000+int64(i), 0).UTC().Format(time.RFC3339))
		if _, err := file.WriteString(row); err != nil {
			t.Fatal(err)
		}
	}
}

// THE PROJECT'S INDEX IS SHARED BY EVERY CONVERSATION IN IT, so listing the
// tasks of a project's hundred conversations read and decoded the same file a
// hundred times. An index nobody has appended to since it was read is answered
// from that reading; a row appended after it is read.
func TestAnUnchangedTaskIndexIsDecodedOnce(t *testing.T) {
	// Move only the cache clock; changing mtime cannot age the inode's ctime.
	taskIndexParsed.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { taskIndexParsed.now = nil })
	path := filepath.Join(t.TempDir(), taskIndexName)
	writeIndexRows(t, path, 0, 1500)
	want := ReadTaskIndex(path)
	if len(want) != 1500 {
		t.Fatalf("read %d rows, want 1500", len(want))
	}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 40 {
		// Whoever gets a reading may reorder it; the next must not see that.
		got := ReadTaskIndex(path)
		got[0], got[len(got)-1] = got[len(got)-1], got[0]
	}
	runtime.ReadMemStats(&after)
	t.Logf("40 readings of a 1,500-row index allocated %d bytes", after.TotalAlloc-before.TotalAlloc)
	const budget = 64 << 20
	if spent := after.TotalAlloc - before.TotalAlloc; spent > budget {
		t.Fatalf("40 readings of an unchanged index allocated %d bytes, want under %d", spent, budget)
	}
	if got := ReadTaskIndex(path); !reflect.DeepEqual(got, want) {
		t.Fatal("a reading changed what the next one saw")
	}

	writeIndexRows(t, path, 1500, 1501)
	if got := ReadTaskIndex(path); len(got) != 1501 {
		t.Fatalf("after an append the index has %d rows, want 1501", len(got))
	}
}

func TestTaskIndexEvictionClearsRetainedRows(t *testing.T) {
	cache := taskIndexCache{now: func() time.Time { return time.Now().Add(time.Hour) }, kept: make([]taskIndexKept, 0, 8)}
	paths := make([]string, 8)
	for i := range paths {
		paths[i] = filepath.Join(t.TempDir(), taskIndexName)
		writeIndexRows(t, paths[i], i, i+1)
	}
	for _, path := range paths[:4] {
		cache.read(path)
	}
	backing := cache.kept[:cap(cache.kept)]
	for _, path := range paths[4:] {
		cache.read(path)
	}
	for _, entry := range backing[:4] {
		if entry.rows != nil {
			t.Fatal("evicted index still reachable through discarded backing array")
		}
	}
}

func TestTaskIndexRejectsUnsettledChangeTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), taskIndexName)
	writeIndexRows(t, path, 0, 1)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stamp, ok := recordStampOf(info)
	if !ok {
		t.Skip("file identity is unavailable")
	}
	now := time.Unix(0, stamp.ctime).Add(time.Second)
	cache := taskIndexCache{now: func() time.Time { return now }}
	for range 2 {
		rows, ok := cache.read(path)
		if !ok || len(rows) != 1 {
			t.Fatalf("unsettled file was not read: %+v %v", rows, ok)
		}
		if len(cache.kept) != 0 {
			t.Fatal("index with old mtime but unsettled ctime was cached")
		}
	}
	now = now.Add(2 * time.Second)
	cache.read(path)
	if len(cache.kept) != 1 {
		t.Fatal("settled index did not enter cache")
	}
}

func TestTaskIndexReadOwnsNestedFields(t *testing.T) {
	taskIndexParsed.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { taskIndexParsed.now = nil })
	path := filepath.Join(t.TempDir(), taskIndexName)
	entry := TaskIndexEntry{ID: "1", Title: "Isolated result", Files: []string{"original.go"},
		Crew: &crewroute.Decision{Why: "original", Crew: []crewroute.Pick{{Model: "original", Price: &crewroute.RoutePrice{Prompt: 1}}},
			Retried: []crewroute.Retry{{Why: "original"}}, Rungs: []crewroute.Retry{{Why: "original"}}, Warnings: []crewroute.SeatWarning{{Model: "original"}}},
		VerdictBasis: &plandb.VerdictBasis{Kind: "run", Runs: []plandb.VerdictRun{{Command: "go test", ExitCode: 0}}, Unobserved: []string{"original"}},
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	// Admission and subsequent hits must both return independently owned fields.
	for range 2 {
		got := ReadTaskIndex(path)
		if len(got) != 1 {
			t.Fatalf("read %d rows, want one", len(got))
		}
		got[0].Files[0] = "changed.go"
		got[0].Crew.Why = "changed"
		got[0].Crew.Crew[0].Model = "changed"
		got[0].Crew.Crew[0].Price.Prompt = 2
		got[0].Crew.Retried[0].Why = "changed"
		got[0].Crew.Rungs[0].Why = "changed"
		got[0].Crew.Warnings[0].Model = "changed"
		got[0].VerdictBasis.Kind = "reading"
		got[0].VerdictBasis.Runs[0].ExitCode = 1
		got[0].VerdictBasis.Unobserved[0] = "changed"
		if next := ReadTaskIndex(path); !reflect.DeepEqual(next, []TaskIndexEntry{entry}) {
			t.Fatalf("nested mutation contaminated the next read: %+v", next)
		}
	}
}

func TestTaskIndexReadCapacityCannotReachCachedRows(t *testing.T) {
	taskIndexParsed.now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { taskIndexParsed.now = nil })
	for _, scenario := range []string{"unlimited", "limited", "repeated-node", "tombstones"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), taskIndexName)
			for i := 1; i <= 3; i++ {
				id := fmt.Sprint(i)
				if scenario == "repeated-node" && i == 1 {
					id = "2"
				}
				appendTaskIndex(path, TaskIndexEntry{ID: id, SessionID: "chat", Title: "Isolated result",
					Files:        []string{"original.go"},
					Crew:         &crewroute.Decision{Why: "original"},
					VerdictBasis: &plandb.VerdictBasis{Kind: "run"},
				})
			}
			if scenario == "tombstones" {
				dir := filepath.Join(filepath.Dir(path), "chat")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, taskDeletedFile), []byte(`{"1":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want := ReadTaskIndex(path)
			got := ReadTaskIndex(path)
			if scenario == "limited" {
				got = readTaskIndex(path, 1)
			}
			if len(got) == 0 {
				t.Fatal("read no rows, want mutable results")
			}
			if cap(got) != len(got) {
				t.Errorf("returned %d rows with capacity %d; reslicing can expose hidden rows", len(got), cap(got))
			}
			for _, row := range got[:cap(got)] {
				if len(row.Files) > 0 {
					row.Files[0] = "changed.go"
				}
				if row.Crew != nil {
					row.Crew.Why = "changed"
				}
				if row.VerdictBasis != nil {
					row.VerdictBasis.Kind = "reading"
				}
			}
			if next := ReadTaskIndex(path); !reflect.DeepEqual(next, want) {
				t.Fatal("mutating rows reachable through capacity contaminated the next ordinary read")
			}
		})
	}
}
