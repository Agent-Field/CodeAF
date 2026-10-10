package desktopbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// rowsFixture is a places root the test builds and a log of what was published.
type rowsFixture struct {
	t     *testing.T
	root  string
	mu    sync.Mutex
	log   []chatRowsDelta
	clock *manualClock
	rows  *worldRows
}

func newRowsFixture(t *testing.T) *rowsFixture {
	t.Helper()
	f := &rowsFixture{t: t, root: t.TempDir(), clock: &manualClock{}}
	f.rows = newWorldRows(f.root, func(kind string, payload any) {
		if kind != worldRowsKind {
			t.Errorf("kind = %q, want %q", kind, worldRowsKind)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.log = append(f.log, payload.(chatRowsDelta))
	})
	f.rows.newTicker = f.clock.ticker
	return f
}

func (f *rowsFixture) published() []chatRowsDelta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chatRowsDelta(nil), f.log...)
}

// chat writes one conversation folder; state "" writes no presence file.
func (f *rowsFixture) chat(id, title, state string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "bucket", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("{}\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	meta := session.Meta{ID: id, Title: title, Workspace: "/work/repo", LastUserAt: time.Now()}
	if err := session.SaveMeta(dir, meta); err != nil {
		f.t.Fatal(err)
	}
	presence := filepath.Join(dir, "presence.json")
	if state == "" {
		os.Remove(presence)
		return transcript
	}
	raw, _ := json.Marshal(session.SessionPresence{Schema: 1, SessionID: id, UpdatedAt: time.Now(), State: session.PresenceState(state)})
	if err := os.WriteFile(presence, raw, 0o644); err != nil {
		f.t.Fatal(err)
	}
	return transcript
}

func rowByID(rows []WorldChatRow, id string) (WorldChatRow, bool) {
	for _, r := range rows {
		if r.ChatID == id {
			return r, true
		}
	}
	return WorldChatRow{}, false
}

func TestWorldRowsPublishOnlyWhatChanged(t *testing.T) {
	f := newRowsFixture(t)
	f.chat("aaaa", "first", "")
	f.chat("bbbb", "second", "")
	f.rows.Scan()
	first := f.published()
	if len(first) != 1 || len(first[0].Rows) != 2 {
		t.Fatalf("first scan = %+v, want one record with both rows", first)
	}
	f.rows.Scan()
	if got := f.published(); len(got) != 1 {
		t.Fatalf("an unchanged scan published %d records", len(got)-1)
	}
	f.chat("bbbb", "renamed", "")
	// File times can tie inside a test; the size of the title keeps the stamp apart.
	f.rows.Scan()
	got := f.published()
	if len(got) != 2 || len(got[1].Rows) != 1 || got[1].Rows[0].ChatID != "bbbb" || got[1].Rows[0].Title != "renamed" {
		t.Fatalf("delta = %+v, want only bbbb renamed", got)
	}
	if err := os.RemoveAll(filepath.Join(f.root, "bucket", "aaaa")); err != nil {
		t.Fatal(err)
	}
	f.rows.Scan()
	got = f.published()
	if len(got) != 3 || len(got[2].Removed) != 1 || got[2].Removed[0] != "aaaa" || len(got[2].Rows) != 0 {
		t.Fatalf("removal = %+v, want only aaaa removed", got)
	}
	if full := f.rows.Full(); len(full) != 1 || full[0].ChatID != "bbbb" {
		t.Fatalf("Full = %+v, want every remaining row", full)
	}
}

func TestAnAttachedTurnStartingFlipsRunningWithinOneRecord(t *testing.T) {
	f := newRowsFixture(t)
	transcript := f.chat("aaaa", "first", "")
	f.rows.Scan()
	before := len(f.published())
	f.rows.Attached("aaaa", attachedChat{SessionFile: transcript, Running: true}, true)
	got := f.published()
	if len(got) != before+1 {
		t.Fatalf("records = %d, want exactly one more", len(got)-before)
	}
	row := got[before].Rows[0]
	if !row.Running || !row.Attached || row.ChatID != "aaaa" || len(got[before].Rows) != 1 {
		t.Fatalf("row = %+v, want attached and running", got[before])
	}
	if row.Title != "first" {
		t.Fatalf("title = %q, want the disk title kept when the window has none", row.Title)
	}
	f.rows.Attached("aaaa", attachedChat{SessionFile: transcript}, false)
	if last := f.published()[before+1].Rows[0]; last.Running || last.Attached {
		t.Fatalf("after detach = %+v, want the disk's word again", last)
	}
}

func TestNoScanWithoutObservers(t *testing.T) {
	f := newRowsFixture(t)
	f.chat("aaaa", "first", "")
	time.Sleep(20 * time.Millisecond)
	if f.rows.scans != 0 || f.clock.has(f.rows.interval) {
		t.Fatalf("scans = %d with nobody listening", f.rows.scans)
	}
	detach := f.rows.Observe()
	waitFor(t, "first scan", func() bool {
		f.rows.mu.Lock()
		defer f.rows.mu.Unlock()
		return f.rows.scans >= 1
	})
	waitFor(t, "ticker", func() bool { return f.clock.has(f.rows.interval) })
	detach()
	f.rows.mu.Lock()
	scans := f.rows.scans
	f.rows.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	f.rows.mu.Lock()
	defer f.rows.mu.Unlock()
	if f.rows.scans != scans {
		t.Fatalf("scans grew from %d to %d after the last observer left", scans, f.rows.scans)
	}
}

func TestRowsCarryNeedsYouAsThePendingQuestionCount(t *testing.T) {
	f := newRowsFixture(t)
	transcript := f.chat("aaaa", "asks", string(session.PresenceWaiting))
	f.chat("bbbb", "quiet", string(session.PresenceWorking))
	f.rows.Scan()
	rows := f.rows.Full()
	if a, _ := rowByID(rows, "aaaa"); a.NeedsYou != 1 || a.Running {
		t.Fatalf("detached asking row = %+v, want needsYou 1", a)
	}
	if b, _ := rowByID(rows, "bbbb"); b.NeedsYou != 0 || !b.Running {
		t.Fatalf("working row = %+v, want running and needsYou 0", b)
	}
	f.rows.Attached("aaaa", attachedChat{SessionFile: transcript, Questions: 3}, true)
	rows = f.rows.Full()
	if a, _ := rowByID(rows, "aaaa"); a.NeedsYou != 3 {
		t.Fatalf("attached row needsYou = %d, want the three open questions", a.NeedsYou)
	}
	raw, _ := json.Marshal(rows[0])
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)
	if _, ok := wire["needsYou"].(float64); !ok {
		t.Fatalf("needsYou on the wire = %v, want a number", wire["needsYou"])
	}
}
