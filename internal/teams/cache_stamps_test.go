package teams

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestTrafficStampSharesReadsResumesAndResets(t *testing.T) {
	for _, limit := range []int{0, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "traffic.jsonl")
			one := `{"id":"000000000001","text":"first"}` + "\n"
			two := `{"id":"000000000002","text":"other"}` + "\n"
			must(t, os.WriteFile(path, []byte(one), 0600))
			var cache trafficMemory
			q := trafficQuery{path: path, limit: limit}
			check := func(want int, reads int) {
				t.Helper()
				var wg sync.WaitGroup
				for range 24 {
					wg.Go(func() {
						got, err := cache.read(q)
						if err != nil || len(got) != want {
							t.Errorf("entries=%d err=%v want %d", len(got), err, want)
						}
					})
				}
				wg.Wait()
				if cache.reads != reads {
					t.Fatalf("reads=%d want %d", cache.reads, reads)
				}
			}
			check(1, 1)
			before := cache.bytes
			appendLedger(t, path, two[:len(two)-1])
			check(2, 2)
			if cache.bytes-before != int64(len(two)) {
				t.Fatalf("append read %d bytes want %d", cache.bytes-before, len(two))
			}
			info, _ := os.Stat(path)
			must(t, os.WriteFile(path+".new", []byte(two+two), 0600))
			must(t, os.Chtimes(path+".new", info.ModTime(), info.ModTime()))
			must(t, os.Rename(path+".new", path))
			check(1, 3)
			must(t, os.WriteFile(path, []byte(one), 0600))
			check(1, 4)
			partial := `{"id":"000000000003"`
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			must(t, err)
			_, err = file.WriteString(partial)
			must(t, err)
			must(t, file.Close())
			check(1, 5)
			file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			must(t, err)
			_, err = file.WriteString("}\n")
			must(t, err)
			must(t, file.Close())
			check(2, 6)
		})
	}
}

func TestTeamLoadSharesParsedFileAndReturnsIndependentMembers(t *testing.T) {
	dir := t.TempDir()
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "crew", Members: []Member{{Key: "/session/a", Handle: "one"}}}}))
	before := storeReads.Load()
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			f, err := Load(dir)
			if err != nil {
				t.Error(err)
				return
			}
			f.Teams[0].Members[0].Handle = "changed"
		})
	}
	wg.Wait()
	if got := storeReads.Load() - before; got != 0 {
		t.Fatalf("team reads=%d want 0: the save left the rows it wrote held", got)
	}
	f, err := Load(dir)
	must(t, err)
	if f.Teams[0].Members[0].Handle != "one" {
		t.Fatal("caller changed cached members")
	}
}

func TestSpendStampMemoizesAndResetsReplacedLedger(t *testing.T) {
	dir := t.TempDir()
	id := "0123456789abcdef"
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "crew", Members: []Member{{Key: filepath.Join("/sessions", id, "transcript.jsonl")}}}}))
	ledger := filepath.Join(dir, "usage.jsonl")
	day := "2026-10-20"
	writeLedger(t, ledger, line(day, 1, id, ""))
	var cache teamSpendMemory
	check := func(want float64, reads int) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				got, err := cache.read(dir, ledger, "aaaaaaaaaaaa", day)
				if err != nil || got.USD != want {
					t.Errorf("spend=%v err=%v want %v", got.USD, err, want)
				}
				for k := range got.ByMember {
					got.ByMember[k] = 99
				}
			})
		}
		wg.Wait()
		if cache.reads != reads {
			t.Fatalf("folds=%d want %d", cache.reads, reads)
		}
	}
	check(1, 1)
	info, _ := os.Stat(ledger)
	writeLedger(t, ledger+".new", line(day, 9, id, ""))
	must(t, os.Chtimes(ledger+".new", info.ModTime(), info.ModTime()))
	must(t, os.Rename(ledger+".new", ledger))
	check(9, 2)
	// A larger replacement must not resume at the old file's offset either.
	writeLedger(t, ledger+".new", line(day, 2, id, ""), line(day, 3, id, ""))
	must(t, os.Rename(ledger+".new", ledger))
	check(5, 3)
	writeLedger(t, ledger, line(day, 4, id, ""))
	check(4, 4)
	appendLedger(t, ledger, line(day, 6, id, ""))
	check(10, 5)
}

func TestTrafficAdvancingCursorReusesTheTailOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.jsonl")
	must(t, os.WriteFile(path, []byte(`{"id":"000000000001"}`+"\n"), 0600))
	var cache trafficMemory
	got, err := cache.read(trafficQuery{path: path, limit: 2})
	must(t, err)
	cursor := got[0].ID
	for i := 2; i < 80; i++ {
		// A new cursor at an unchanged suffix needs no content read.
		before := cache.reads
		got, err = cache.read(trafficQuery{path: path, after: cursor, limit: 2})
		must(t, err)
		if len(got) != 0 || cache.reads != before {
			t.Fatal("new cursor re-read an unchanged suffix")
		}
		row := fmt.Sprintf("{\"id\":\"%012d\"}", i)
		bytes := cache.bytes
		appendLedger(t, path, row)
		got, err = cache.read(trafficQuery{path: path, after: cursor, limit: 2})
		must(t, err)
		if len(got) != 1 || cache.bytes-bytes != int64(len(row)+1) {
			t.Fatalf("cursor read=%v, bytes=%d", got, cache.bytes-bytes)
		}
		cursor = got[0].ID
	}
	if len(cache.held) > 64 {
		t.Fatalf("retained %d cursors", len(cache.held))
	}
}

func TestTrafficForwardBurstKeepsEveryPageReachable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.jsonl")
	must(t, os.WriteFile(path, []byte("{\"id\":\"000000000001\"}\n"), 0600))
	var cache trafficMemory
	q := trafficQuery{path: path, after: "000000000000", limit: 2}
	got, err := cache.read(q)
	must(t, err)
	if len(got) != 1 {
		t.Fatalf("first page: %v", got)
	}
	for i := 2; i <= 4; i++ {
		appendLedger(t, path, fmt.Sprintf("{\"id\":\"%012d\"}", i))
	}
	for _, cursor := range []string{q.after, "000000000002"} {
		q.after = cursor
		got, err = cache.read(q)
		must(t, err)
		if len(got) != 2 {
			t.Fatalf("after %s: got %v, want two entries", cursor, got)
		}
		want := "000000000002"
		if cursor == want {
			want = "000000000004"
		}
		if got[1].ID != want {
			t.Fatalf("after %s: last id %s, want %s", cursor, got[1].ID, want)
		}
	}
}
