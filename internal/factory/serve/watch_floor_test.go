package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A RENEWAL WAKES NO PULL. Every held job's runner rewrites the lease document
// every two seconds, and each rewrite woke every open pull on the machine to
// look at a floor on which nothing had changed: the seventh live A+B run's idle
// engine spent a fifth of a core on it with two holds, fifteen slots and 86
// jobs. The watch answers the question once for every pull: a rewrite whose
// only difference is when the holds were heard wakes none, and neither do a
// runner's own folders or the event log; a hold that comes or goes, or a job
// that changes, wakes them all. No goroutine runs here: each change is
// published by hand, with its file's time set, so nothing waits on a clock.
func TestARenewalWakesNoPullAndARealChangeWakesThemAll(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	step := 0
	write := func(name string, data []byte) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		step++
		stamp := at.Add(time.Duration(step) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if dir := filepath.Dir(path); dir != root {
			if err := os.Chtimes(dir, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
	leases := func(heard time.Time, jobs ...int) []byte {
		var held []map[string]any
		for _, job := range jobs {
			held = append(held, map[string]any{"id": "lease_" + string(rune('a'+job)), "job": job, "worker": "w" + string(rune('a'+job)), "handle": "sam", "heartbeat_at": heard, "expires_at": heard.Add(time.Minute)})
		}
		data, err := json.Marshal(map[string]any{"schema": 1, "leases": held})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	write("1.json", []byte(`{"schema":1,"revision":1,"item":{"ID":1}}`))
	write("leases.json", leases(at, 1))
	w := &rootWatch{root: root, hub: newHub(), floor: newHub(), snap: snapshot(root), leases: readFloorLeases(root)}
	pulls, pages := w.floor.join(), w.hub.join()
	woke := func() bool {
		select {
		case <-pulls:
			return true
		default:
			return false
		}
	}
	paged := func() bool {
		select {
		case <-pages:
			return true
		default:
			return false
		}
	}

	for beat := 1; beat <= 30; beat++ {
		write("leases.json", leases(at.Add(time.Duration(2*beat)*time.Second), 1))
		w.publish()
		if woke() {
			t.Fatalf("renewal %d woke the open pulls", beat)
		}
		if !paged() {
			t.Fatalf("renewal %d was not told to the floor's pages", beat)
		}
	}
	write("worker-reports/1-abc.json", []byte(`{}`))
	write("events.jsonl", []byte("{}\n"))
	write("runners.json", []byte(`{}`))
	w.publish()
	if woke() {
		t.Fatal("a runner's report folder, the event log or the registry woke the open pulls")
	}

	write("leases.json", leases(at.Add(time.Hour), 1, 2))
	w.publish()
	if !woke() {
		t.Fatal("a new hold did not wake the open pulls")
	}
	write("leases.json", leases(at.Add(time.Hour+2*time.Second), 1, 2))
	w.publish()
	if woke() {
		t.Fatal("the renewal after a new hold woke the open pulls")
	}
	write("1.json", []byte(`{"schema":1,"revision":2,"item":{"ID":1,"State":"queued"}}`))
	w.publish()
	if !woke() {
		t.Fatal("a changed job did not wake the open pulls")
	}
	write("leases.json", []byte(`not json`))
	w.publish()
	if !woke() {
		t.Fatal("an unreadable lease document did not wake the open pulls")
	}
}
