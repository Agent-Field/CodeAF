package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// failureBucket is one project bucket with one conversation folder and a real tasks.jsonl.
func failureBucket(t *testing.T, rows string) (dir string) {
	t.Helper()
	bucket := t.TempDir()
	dir = filepath.Join(bucket, "0123456789abcdef")
	seedMetaLockConversation(t, dir)
	if err := os.WriteFile(filepath.Join(bucket, taskIndexName), []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const failureRows = `{"id":"1","title":"a","label":"a","status":"failed","sessionId":"0123456789abcdef","endedAt":"2026-10-01T10:00:00Z"}` + "\n" +
	`{"id":"2","title":"b","label":"b","status":"done","sessionId":"0123456789abcdef","endedAt":"2026-10-01T11:00:00Z"}` + "\n" +
	`{"id":"3","title":"c","label":"c","status":"failed","sessionId":"0123456789abcdef","endedAt":"2026-10-02T10:00:00.5Z"}` + "\n" +
	`{"id":"9","title":"other","label":"other","status":"failed","sessionId":"someone-else","endedAt":"2026-10-03T10:00:00Z"}` + "\n"

func at(s string) time.Time {
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestMarkingAFailureSeenIsMonotonicIdempotentAndNeverCoversALaterOne(t *testing.T) {
	dir := failureBucket(t, failureRows)
	first, err := MarkFailureSeen(dir, "0123456789abcdef", "1", at("2026-10-01T10:00:00Z"))
	if err != nil || !first.Changed || first.Unseen != 1 {
		t.Fatalf("first failure: %+v, %v (the later failure must stay unseen)", first, err)
	}
	again, err := MarkFailureSeen(dir, "0123456789abcdef", "1", at("2026-10-01T10:00:00Z"))
	if err != nil || again.Changed || again.Unseen != 1 || !again.Through.Equal(first.Through) {
		t.Fatalf("a repeat changed something: %+v, %v", again, err)
	}
	last, err := MarkFailureSeen(dir, "0123456789abcdef", "", at("2026-10-02T10:00:00.5Z"))
	if err != nil || !last.Changed || last.Unseen != 0 {
		t.Fatalf("newest failure: %+v, %v", last, err)
	}
	older, err := MarkFailureSeen(dir, "0123456789abcdef", "1", at("2026-10-01T10:00:00Z"))
	if err != nil || older.Changed || !older.Through.Equal(at("2026-10-02T10:00:00.5Z")) {
		t.Fatalf("an older mark moved the watermark backwards: %+v, %v", older, err)
	}
	meta, _ := LoadMeta(dir)
	if !meta.FailuresSeen.Equal(at("2026-10-02T10:00:00.5Z")) || meta.Title != "previous identity" || meta.Model != "test/model" {
		t.Fatalf("the mark disturbed the rest of the identity: %+v", meta)
	}
}

func TestAFailureThatLandsAfterTheMarkIsUnseenAgain(t *testing.T) {
	dir := failureBucket(t, failureRows)
	if _, err := MarkFailureSeen(dir, "0123456789abcdef", "", at("2026-10-02T10:00:00.5Z")); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(filepath.Dir(dir), taskIndexName), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"4","title":"d","label":"d","status":"failed","sessionId":"0123456789abcdef","endedAt":"2026-10-04T10:00:00Z"}` + "\n")
	f.Close()
	meta, _ := LoadMeta(dir)
	row := SessionRow{ID: meta.ID, FailuresSeen: meta.FailuresSeen, Tasks: TaskRollup{Rows: ReadTaskIndex(filepath.Join(filepath.Dir(dir), taskIndexName))}}
	var mine []TaskIndexEntry
	for _, e := range row.Tasks.Rows {
		if e.SessionID == meta.ID {
			mine = append(mine, e)
		}
	}
	row.Tasks.Rows = mine
	if got := row.UnseenFailures(); got != 1 {
		t.Fatalf("the new failure was covered by the old mark: unseen %d", got)
	}
	if newest, ok := row.NewestFailure(); !ok || newest.ID != "4" {
		t.Fatalf("newest failure = %+v %v", newest, ok)
	}
}

func TestOnlyARealFailureOfThisConversationCanBeMarked(t *testing.T) {
	dir := failureBucket(t, failureRows)
	for name, c := range map[string]struct {
		session, task string
		when          time.Time
	}{
		"a time between failures":           {"0123456789abcdef", "", at("2026-10-01T10:30:00Z")},
		"the future":                        {"0123456789abcdef", "", at("2030-01-01T00:00:00Z")},
		"a done task's landing instant":     {"0123456789abcdef", "", at("2026-10-01T11:00:00Z")},
		"another conversation's failure":    {"0123456789abcdef", "", at("2026-10-03T10:00:00Z")},
		"the right instant, the wrong task": {"0123456789abcdef", "3", at("2026-10-01T10:00:00Z")},
	} {
		if _, err := MarkFailureSeen(dir, c.session, c.task, c.when); !errors.Is(err, ErrNoSuchFailure) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
	if _, err := MarkFailureSeen(dir, "wrong-session", "", at("2026-10-01T10:00:00Z")); err == nil || errors.Is(err, ErrNoSuchFailure) {
		t.Fatalf("a different conversation's folder accepted a mark: %v", err)
	}
	if meta, _ := LoadMeta(dir); !meta.FailuresSeen.IsZero() {
		t.Fatalf("a refused mark wrote %v", meta.FailuresSeen)
	}
}

func TestAFailureWithNoLandingInstantIsNeverCountedUnseen(t *testing.T) {
	row := SessionRow{Tasks: TaskRollup{Rows: []TaskIndexEntry{{ID: "1", Status: "failed"}, {ID: "2", Status: "running"}}}}
	if row.UnseenFailures() != 0 {
		t.Fatal("a failed row with no version was counted")
	}
	if _, ok := row.NewestFailure(); ok {
		t.Fatal("a failed row with no version was offered as the failure to mark")
	}
}

func TestMarkingFailuresSeenSurvivesConcurrentIdentityWritersAndWindows(t *testing.T) {
	dir := failureBucket(t, failureRows)
	a := metaLockAgent(t, dir)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := MarkFailureSeen(dir, "0123456789abcdef", "", at("2026-10-02T10:00:00.5Z")); err != nil {
				t.Error(err)
			}
		}()
		go func(i int) {
			defer wg.Done()
			a.updateMeta(dir, a.metaSnapshot(), func(m *Meta) { m.SpentUSD = float64(i) / 10 })
		}(i)
	}
	wg.Wait()
	meta, _ := LoadMeta(dir)
	if !meta.FailuresSeen.Equal(at("2026-10-02T10:00:00.5Z")) {
		t.Fatalf("an engine metadata write erased the seen mark: %+v", meta)
	}
	if strings.TrimSpace(meta.ID) == "" {
		t.Fatal("identity lost")
	}
}
