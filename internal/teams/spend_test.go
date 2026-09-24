package teams

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// A TEAM'S DAY IS ITS SUBTREE'S LEDGER LINES, EACH ONCE. harbor holds one
// conversation, dock under it another; a third is in neither. A task's line
// counts through its root; a line on another day, or of an outsider, does not.
func TestTeamSpendSumsTheSubtreeFromTheLedger(t *testing.T) {
	dir := t.TempDir()
	a, b, x := "0123456789abcdef", "fedcba9876543210", "1111111111111111"
	key := func(id string) string { return filepath.Join("/srv/sessions", id, "transcript.jsonl") }
	must(t, Save(dir, []Team{
		{ID: "aaaaaaaaaaaa", Name: "harbor", Members: []Member{{Key: key(a)}}},
		{ID: "bbbbbbbbbbbb", Name: "dock", Parent: "aaaaaaaaaaaa", Members: []Member{{Key: key(b)}, {Key: key(a)}}},
	}))
	ledger := filepath.Join(dir, "usage.jsonl")
	lines := []string{
		line("2026-09-24", 1.00, a, ""),
		line("2026-09-24", 0.25, "9999999999999999", b), // a task b started
		line("2026-09-24", 5.00, x, ""),                 // somebody else
		line("2026-09-23", 7.00, a, ""),                 // yesterday
		line("2026-09-24", 0.50, b, ""),
	}
	writeLedger(t, ledger, lines...)

	got, err := TeamSpendIn(dir, ledger, "aaaaaaaaaaaa", "2026-09-24")
	if err != nil || fmt.Sprintf("%.2f", got.USD) != "1.75" || got.Calls != 3 {
		t.Fatalf("harbor's day: %+v, %v", got, err)
	}
	if fmt.Sprintf("%.2f", got.ByMember[key(b)]) != "0.75" {
		t.Fatalf("by member: %+v", got.ByMember)
	}
	dock, _ := TeamSpendIn(dir, ledger, "bbbbbbbbbbbb", "2026-09-24")
	if fmt.Sprintf("%.2f", dock.USD) != "1.75" {
		t.Fatalf("dock (which holds a too) spent %v", dock.USD)
	}
	if none, _ := TeamSpendIn(dir, ledger, "cccccccccccc", "2026-09-24"); none.USD != 0 {
		t.Fatal("an unknown team spent something")
	}

	// Quiet: no bytes read. Grown: only the appended line.
	before := spendCache.bytes()
	_, _ = TeamSpendIn(dir, ledger, "aaaaaaaaaaaa", "2026-09-24")
	if spendCache.bytes() != before {
		t.Fatal("a quiet ledger was read again")
	}
	extra := line("2026-09-24", 2.00, a, "")
	appendLedger(t, ledger, extra)
	again, _ := TeamSpendIn(dir, ledger, "aaaaaaaaaaaa", "2026-09-24")
	if fmt.Sprintf("%.2f", again.USD) != "3.75" || spendCache.bytes()-before != int64(len(extra)+1) {
		t.Fatalf("after one more line: %v, read %d bytes", again.USD, spendCache.bytes()-before)
	}
}

func line(day string, usd float64, session, root string) string {
	return fmt.Sprintf(`{"at":"2026-09-24T10:00:00Z","day":%q,"usd":%v,"calls":1,"session":%q,"root":%q}`, day, usd, session, root)
}

func writeLedger(t *testing.T, path string, lines ...string) {
	t.Helper()
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	must(t, os.WriteFile(path, []byte(body), 0o600))
}

func appendLedger(t *testing.T, path, l string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	_, err = f.WriteString(l + "\n")
	must(t, err)
	must(t, f.Close())
}

func (m *spendMemory) bytes() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads
}
