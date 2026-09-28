package teams

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func note(from, to, text string) Entry {
	return Entry{Kind: KindNote, From: from, To: to, Text: text}
}

func TestTrafficAppendAndRead(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadTraffic(dir, "t1", "", 0); err != nil || len(got) != 0 {
		t.Fatalf("an empty log: %+v %v", got, err)
	}
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	first := Entry{ID: "ignored", At: at, Kind: KindDirective, From: FromManager, To: "fix", Text: "take the login bug",
		Files: []string{"auth/login.go"}, Member: "k1"}
	if err := AppendTraffic(dir, "t1", first); err != nil {
		t.Fatal(err)
	}
	if err := AppendTraffic(dir, "t1", note("fix", ToManager, "on it")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTraffic(dir, "t1", "", 0)
	if err != nil || len(got) != 2 {
		t.Fatalf("read %+v %v", got, err)
	}
	first.ID = "000000000001"
	if got[0].ID != first.ID || !got[0].At.Equal(at) || got[0].Kind != first.Kind || got[0].Text != first.Text ||
		got[0].Files[0] != "auth/login.go" || got[0].Member != "k1" {
		t.Fatalf("first entry came back %+v", got[0])
	}
	if got[1].ID != "000000000002" || got[1].At.IsZero() {
		t.Fatalf("second entry came back %+v", got[1])
	}
	// The line on disk is the contract's shape.
	raw, _ := os.ReadFile(TrafficPath(dir, "t1"))
	line := strings.SplitN(string(raw), "\n", 2)[0]
	for _, key := range []string{`"id":`, `"at":`, `"kind":"directive"`, `"from":"manager"`, `"to":"fix"`, `"text":`, `"files":["auth/login.go"]`, `"member":"k1"`} {
		if !strings.Contains(line, key) {
			t.Fatalf("the line lacks %s: %s", key, line)
		}
	}
	// Two teams, two logs.
	if other, _ := ReadTraffic(dir, "t2", "", 0); len(other) != 0 {
		t.Fatalf("another team's log holds %+v", other)
	}
}

func TestTrafficRefusesBadEntriesAndIDs(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		team string
		e    Entry
	}{
		{"t1", Entry{Kind: "chat", From: "a", To: "b"}},
		{"t1", Entry{Kind: KindNote, To: "b"}},
		{"t1", Entry{Kind: KindNote, From: "a"}},
		{"../escape", note("a", "b", "x")},
		{"", note("a", "b", "x")},
	} {
		if err := AppendTraffic(dir, c.team, c.e); err == nil {
			t.Errorf("appended %+v to %q", c.e, c.team)
		}
	}
	if _, err := ReadTraffic(dir, "a/b", "", 0); err == nil {
		t.Error("read a team id with a slash")
	}
}

// AFTER PAGES FORWARD, AND NO LIMIT SKIPS AN ENTRY; with no after, the limit
// is the tail.
func TestTrafficAfterPaging(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 10; i++ {
		if err := AppendTraffic(dir, "t1", note("a", "b", fmt.Sprint(i))); err != nil {
			t.Fatal(err)
		}
	}
	tail, _ := ReadTraffic(dir, "t1", "", 3)
	if texts(tail) != "8,9,10" {
		t.Fatalf("tail %s", texts(tail))
	}
	// A follower that has seen nothing starts below the first id.
	var pages []string
	after := "000000000000"
	for {
		page, err := ReadTraffic(dir, "t1", after, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		pages = append(pages, texts(page))
		after = page[len(page)-1].ID
	}
	if strings.Join(pages, "|") != "1,2,3,4|5,6,7,8|9,10" {
		t.Fatalf("pages %v", pages)
	}
	if rest, _ := ReadTraffic(dir, "t1", "000000000007", 0); texts(rest) != "8,9,10" {
		t.Fatalf("after 7: %s", texts(rest))
	}
}

// PAST THE SIZE GUARD THE LOG ROTATES, the ids keep counting, and a reader
// still sees both files in order.
func TestTrafficRotates(t *testing.T) {
	dir := t.TempDir()
	defer func(n int64) { trafficRotateBytes = n }(trafficRotateBytes)
	trafficRotateBytes = 600
	for i := 1; i <= 12; i++ {
		if err := AppendTraffic(dir, "t1", note("a", "b", fmt.Sprintf("entry %02d %s", i, strings.Repeat("x", 40)))); err != nil {
			t.Fatal(err)
		}
	}
	path := TrafficPath(dir, "t1")
	info, err := os.Stat(path)
	if err != nil || info.Size() > trafficRotateBytes {
		t.Fatalf("current log: %v %v", info, err)
	}
	if _, err := os.Stat(trafficRotated(path)); err != nil {
		t.Fatalf("no rotated log: %v", err)
	}
	all, _ := ReadTraffic(dir, "t1", "", 0)
	if len(all) == 0 || all[len(all)-1].ID != "000000000012" {
		t.Fatalf("after rotation the last id is %+v", all)
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID <= all[i-1].ID {
			t.Fatalf("out of order: %s then %s", all[i-1].ID, all[i].ID)
		}
	}
	// Only the current and one rotated file are kept, so the oldest are gone.
	if all[0].ID == "000000000001" {
		t.Fatalf("nothing was rotated away: %d entries kept", len(all))
	}
}

// TWO WRITERS AT ONCE: every id is taken once.
func TestTrafficConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if err := AppendTraffic(dir, "t1", note(fmt.Sprintf("w%d", w), "b", fmt.Sprint(i))); err != nil {
					t.Error(err)
				}
			}
		}(w)
	}
	wg.Wait()
	all, _ := ReadTraffic(dir, "t1", "", 0)
	if len(all) != 60 || all[59].ID != "000000000060" {
		t.Fatalf("%d entries, last %+v", len(all), all[len(all)-1])
	}
}

func texts(es []Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, e.Text)
	}
	return strings.Join(out, ",")
}
