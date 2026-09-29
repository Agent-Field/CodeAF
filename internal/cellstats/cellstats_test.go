package cellstats_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cellstats"
	"github.com/Agent-Field/codeaf/internal/cellsync"
)

type fakeMeter struct{ c cellstats.Counts }

func (f *fakeMeter) Snapshot() cellstats.Counts { return f.c }

const golden = `{"V":1,"turn":"t1","turns":2,"frames":1,"objects":14,"bytes_up":20480,"puts":1,"gets":0,"has":0,"bytes_down":0}` + "\n"

func TestStatsLineShape(t *testing.T) {
	m := &fakeMeter{cellstats.Counts{Puts: 1, BytesUp: 20480}}
	r := cellstats.NewRecorder(t.TempDir(), "c1", m)
	r.OnFlush(cellsync.Flush{Head: "t1", Turns: 2, Frames: 1, Objects: 14, Bytes: 20480})
	got, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != golden {
		t.Fatalf("stats line = %q, want %q", got, golden)
	}
}

func TestStatsAreDeltasAndAppendOnly(t *testing.T) {
	m := &fakeMeter{}
	home := t.TempDir()
	r := cellstats.NewRecorder(home, "c1", m)
	m.c = cellstats.Counts{Puts: 1, BytesUp: 100}
	r.OnFlush(cellsync.Flush{Head: "a"})
	first, _ := os.ReadFile(r.Path)
	m.c = cellstats.Counts{Puts: 3, Gets: 2, BytesUp: 250, BytesDown: 40}
	r.OnFlush(cellsync.Flush{Head: "b"})
	all, _ := os.ReadFile(r.Path)
	if !strings.HasPrefix(string(all), string(first)) {
		t.Fatal("a later flush changed an earlier line")
	}
	lines, err := cellstats.Read(home, "c1")
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines = %v, err = %v", lines, err)
	}
	if l := lines[1]; l.Puts != 2 || l.Gets != 2 || l.BytesUp != 150 || l.BytesDown != 40 {
		t.Fatalf("second line = %+v, want the growth since the first", l)
	}
	if tot := cellstats.Total(lines); tot.Puts != 3 || tot.BytesUp != 250 {
		t.Fatalf("total = %+v", tot)
	}
}

func TestStatsWriteFailureNeverFailsTheFlush(t *testing.T) {
	blocker := t.TempDir() + "/file"
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r := cellstats.NewRecorder(blocker, "c1", nil) // a file where a directory must be
	r.OnFlush(cellsync.Flush{Head: "a"})
	if r.Err() == nil {
		t.Fatal("an unwritable stats file must be reported through Err")
	}
}

func TestReadOfACellThatNeverFlushedIsEmpty(t *testing.T) {
	lines, err := cellstats.Read(t.TempDir(), "c1")
	if err != nil || len(lines) != 0 {
		t.Fatalf("lines = %v, err = %v", lines, err)
	}
}

func TestStatsCarryNoPaths(t *testing.T) {
	const secretName, secretTitle = "secret-plan-2026.txt", "Quarterly layoffs memo"
	s := newSession(t, secretTitle)
	s.sealAndFlush(t, map[string]string{"dir/" + secretName: "the body is also secret"})
	raw, err := os.ReadFile(cellstats.Path(s.home, cellID))
	if err != nil || len(raw) == 0 {
		t.Fatalf("no stats written: %v", err)
	}
	for _, leak := range []string{"secret", "layoffs", "Quarterly", "dir/", "body", s.cell.Root, s.home} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("stats file leaks %q: %s", leak, raw)
		}
	}
}

// The counters of a session equal the server's own count for the same identity.
func TestCountersMatchRelayStats(t *testing.T) {
	s := newSession(t, "t")
	ctx := context.Background()
	if _, err := s.store.Has(ctx, []string{strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Get(ctx, strings.Repeat("b", 64)); err == nil { // a miss is still a request
		t.Fatal("expected not found")
	}
	s.sealAndFlush(t, map[string]string{"a": "one", "b": "two"})
	lines, err := cellstats.Read(s.home, cellID)
	if err != nil || len(lines) == 0 {
		t.Fatalf("lines = %v, err = %v", lines, err)
	}
	want, err := s.client.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := cellstats.Total(lines)
	if got.Puts != want.Puts || got.Gets != want.Gets || got.Has != want.Has ||
		got.BytesUp != want.BytesIn || got.BytesDown != want.BytesOut {
		t.Fatalf("session counters %+v differ from the store's %+v", got, want)
	}
	if got.Puts == 0 || got.Has != 1 || got.Gets != 1 {
		t.Fatalf("the comparison is empty: %+v", got)
	}
}

func TestStatsSettleCarriesWhatNoFlushDid(t *testing.T) {
	m := &fakeMeter{}
	home := t.TempDir()
	r := cellstats.NewRecorder(home, "c1", m)
	r.Settle()
	if _, err := os.Stat(r.Path); err == nil {
		t.Fatal("settling with nothing counted wrote a line")
	}
	m.c = cellstats.Counts{Gets: 5, BytesDown: 900}
	r.Settle()
	r.Settle() // the second settle has nothing new to say
	lines, err := cellstats.Read(home, "c1")
	if err != nil || len(lines) != 1 {
		t.Fatalf("lines = %v, err = %v; want one", lines, err)
	}
	if l := lines[0]; l.Turn != "" || l.Gets != 5 || l.BytesDown != 900 {
		t.Fatalf("settled line = %+v", l)
	}
}

func TestStatsReportShowsTheVaultAsItsOwnRow(t *testing.T) {
	cell := []cellstats.Line{{V: 1, Turn: "abcdefghijklmnop", Puts: 2, BytesUp: 100}}
	vault := []cellstats.Line{{V: 1, Puts: 1, BytesUp: 30}, {V: 1, Gets: 1, BytesDown: 7}}
	var out strings.Builder
	cellstats.RenderScopes(&out, cell, vault)
	rows := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(rows) != 4 || !strings.HasPrefix(rows[2], "total") || !strings.HasPrefix(rows[3], "vault") {
		t.Fatalf("report rows = %q; want a flush row, total, then vault", rows)
	}
	if !strings.Contains(rows[2], " 100 ") || !strings.Contains(rows[3], " 30 ") || !strings.Contains(rows[3], " 7") {
		t.Fatalf("the vault row is not kept out of the cell's total:\n%s", out.String())
	}
	out.Reset()
	cellstats.RenderScopes(&out, nil, nil)
	if out.Len() != 0 {
		t.Fatalf("an empty report printed %q", out.String())
	}
}
