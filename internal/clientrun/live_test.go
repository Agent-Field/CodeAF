package clientrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/directory"
)

func (e *engine) Entries(id, before string, limit int) ([]appwire.Entry, bool, error) {
	rows := make([]appwire.Entry, 60)
	for i := range rows {
		text := e.entryPrefix + strings.Repeat("x", e.entryBytes) + e.entrySuffix
		rows[i] = appwire.Entry{ID: fmt.Sprint(i), Agent: id, Kind: appwire.EntryAnswer, Text: text, At: time.Unix(int64(i+1), 0)}
	}
	start := max(0, len(rows)-limit)
	out := rows[start:]
	e.returnedEntries = out
	return out, start > 0, nil
}

func (g *rig) slot(t *testing.T, method string) []byte {
	t.Helper()
	p := g.readBoxSlot(t, method, g.s.dev.ID(), 0)
	return p.Payload
}

// TestEntrySnapshotsKeepNewestWholeEntriesWithinSlot verifies that entry
// snapshots keep the newest whole entries when multiple entries exceed the
// 64 KiB slot limit, and keep the newest entry's tail with a truncation marker
// when a single oversized entry exceeds the slot limit so the entry list is
// never left empty (contract L5).
func TestEntrySnapshotsKeepNewestWholeEntriesWithinSlot(t *testing.T) {
	for _, size := range []int{10, 4000, 70000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			g := newRig(t)
			g.e.entryBytes = size
			if size == 70000 {
				g.e.entryPrefix = "HEAD-MARKER-"
				g.e.entrySuffix = "-TAIL-KEEP"
			}
			g.enroll(t, []string{"agents.entries"})
			if err := g.r.Step(context.Background()); err != nil {
				t.Fatal(err)
			}
			for i, orig := range g.e.returnedEntries {
				wantLen := len(g.e.entryPrefix) + size + len(g.e.entrySuffix)
				if len(orig.Text) != wantLen {
					t.Fatalf("engine-returned entry %d was mutated in place: len=%d, want %d", i, len(orig.Text), wantLen)
				}
			}
			var got []struct {
				Agent  string
				Result appwire.EntriesResult
			}
			if err := json.Unmarshal(g.slot(t, "agents.entries"), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Agent != chat {
				t.Fatalf("scope: %+v", got)
			}
			entries := got[0].Result.Entries
			if !got[0].Result.More {
				t.Fatal("missing older-entry marker")
			}
			if size == 10 && len(entries) != 40 {
				t.Fatalf("got %d entries, want 40", len(entries))
			}
			if size == 4000 && (len(entries) == 0 || len(entries) >= 40) {
				t.Fatalf("did not trim to slot: %d", len(entries))
			}
			if size == 70000 {
				if len(entries) != 1 {
					t.Fatalf("oversized single entry produced %d entries, want 1", len(entries))
				}
				row := entries[0]
				if row.ID != "59" {
					t.Fatalf("kept entry ID = %q, want newest entry 59", row.ID)
				}
				if !strings.HasPrefix(row.Text, TruncatedEntryMarker) {
					t.Fatalf("truncated oversized entry missing marker prefix %q", TruncatedEntryMarker)
				}
				if !strings.HasSuffix(row.Text, "-TAIL-KEEP") {
					t.Fatalf("truncated oversized entry lost its tail")
				}
				if strings.Contains(row.Text, "HEAD-MARKER-") {
					t.Fatalf("truncated oversized entry kept head instead of tail")
				}
				return
			}
			for i, row := range entries {
				if row.ID != fmt.Sprint(60-len(entries)+i) || len(row.Text) != size {
					t.Fatalf("not a whole newest suffix: %s (%d bytes)", row.ID, len(row.Text))
				}
			}
		})
	}
}

func (e *engine) ListTasks(agent string) ([]appwire.Task, error) {
	return []appwire.Task{{ID: chat + ":1", Agent: chat, Irreversible: e.taskIrreversible}, {ID: otherChat + ":1", Agent: otherChat}}, nil
}

// TestTaskSnapshotsPartitionRowsByHeldChat verifies that tasks.list snapshots
// only include tasks belonging to chats leased by this computer.
func TestTaskSnapshotsPartitionRowsByHeldChat(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"tasks.list"})
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []struct {
		Agent  string
		Result appwire.TasksResult
	}
	if err := json.Unmarshal(g.slot(t, "tasks.list"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != chat || len(got[0].Result.Tasks) != 1 || got[0].Result.Tasks[0].ID != chat+":1" {
		t.Fatalf("unleased task leaked: %+v", got)
	}
}

// TestSnapshotsIncludeRealEngineChatClasses verifies that non-archived cells of
// every chat class leased by this computer are included in snapshots.
func TestSnapshotsIncludeRealEngineChatClasses(t *testing.T) {
	g := newRig(t)
	if _, err := g.dir.Create(context.Background(), otherChat, directory.CellInit{Head: strings.Repeat("2", 64), Class: "host-bound"}); err != nil {
		t.Fatal(err)
	}
	g.enroll(t, []string{"asks.list"}, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var got []struct{ Agent string }
	if err := json.Unmarshal(g.slot(t, "asks.list"), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Agent != otherChat {
		t.Fatalf("real engine chat missing: %+v", got)
	}
}
