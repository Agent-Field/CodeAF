package live_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/guard"
)

func outcomeEvent(job int) live.Event {
	return live.New(live.OutcomeKind, job, live.Outcome{Kind: "finished", Job: job})
}

func readOutcomes(t *testing.T, root string, cursor int64, limit int) live.OutcomePage {
	t.Helper()
	got, err := live.ReadOutcomes(root, &cursor, limit)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// E2, E3: independent writers share one sequence, retained without a reader.
func TestOutcomesAcrossWritersAndBoundedReads(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		wg.Add(1)
		guard.Go("outcome test writer", func() {
			defer wg.Done()
			for i := 0; i < 12; i++ {
				if err := live.Append(root, outcomeEvent(1)); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	var cursor int64
	for {
		got := readOutcomes(t, root, cursor, 7)
		if got.Reset || len(got.Events) > 7 {
			t.Fatalf("read: %+v", got)
		}
		for _, e := range got.Events {
			cursor++
			if e.Seq != cursor {
				t.Fatalf("seq %d, want %d", e.Seq, cursor)
			}
		}
		if got.Cursor != cursor {
			t.Fatalf("cursor %d, want %d", got.Cursor, cursor)
		}
		if !got.More {
			break
		}
	}
	if cursor != 48 {
		t.Fatalf("received %d outcomes", cursor)
	}
}

// E4, E5: trimming raises the floor to the last discarded sequence.
func TestOutcomesRetentionAndReset(t *testing.T) {
	root := t.TempDir()
	events := make([]live.Event, live.OutcomeLimit+3)
	for i := range events {
		events[i] = outcomeEvent(1)
	}
	if err := live.Append(root, events...); err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []int64{0, 2, live.OutcomeLimit + 4} {
		got := readOutcomes(t, root, cursor, 100)
		if !got.Reset || got.Cursor != live.OutcomeLimit+3 || len(got.Events) != 0 {
			t.Fatalf("cursor %d: %+v", cursor, got)
		}
	}
	got := readOutcomes(t, root, 3, live.OutcomeLimit)
	if got.Reset || len(got.Events) != live.OutcomeLimit || got.Events[0].Seq != 4 {
		t.Fatalf("retention: %+v", got)
	}
	var disk struct {
		Floor  int64          `json:"floor"`
		Events []live.Outcome `json:"events"`
	}
	data, err := os.ReadFile(filepath.Join(root, live.OutcomeFile))
	if err != nil || json.Unmarshal(data, &disk) != nil || disk.Floor != 3 || len(disk.Events) != live.OutcomeLimit {
		t.Fatalf("record %s: %v", data, err)
	}
}

// E5, E6: absent differs from zero; other notifications leave outcomes alone.
func TestOutcomesInitialCursorAndLiveOnlyWrites(t *testing.T) {
	root := t.TempDir()
	got, err := live.ReadOutcomes(root, nil, 100)
	if err != nil || !got.Reset || got.Cursor != 0 || len(got.Events) != 0 {
		t.Fatalf("initial read: %+v %v", got, err)
	}
	if got := readOutcomes(t, root, 0, 100); got.Reset || got.Cursor != 0 {
		t.Fatalf("zero cursor: %+v", got)
	}
	if err := live.Append(root, outcomeEvent(1)); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, live.OutcomeFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"activity", "machine"} {
		if err := live.Append(root, live.New(kind, 0, live.Payload{})); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(filepath.Join(root, live.OutcomeFile))
	if err != nil || string(before) != string(after) {
		t.Fatalf("live-only write rewrote outcomes: %v", err)
	}
	if got := readOutcomes(t, root, 1, 100); got.Reset || got.Cursor != 1 || len(got.Events) != 0 {
		t.Fatalf("caught up: %+v", got)
	}
}

// E5: an older writer invalidates a caught-up cursor before and after a commit.
func TestOutcomesOlderWriterConsumesAGapSequence(t *testing.T) {
	root := t.TempDir()
	if err := live.Append(root, outcomeEvent(1), live.New("taken", 1, live.Payload{})); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"next":2,"events":[]}`)
	if err := os.WriteFile(filepath.Join(root, live.File), data, 0600); err != nil {
		t.Fatal(err)
	}
	if got := readOutcomes(t, root, 1, 100); !got.Reset || got.Cursor != 1 || len(got.Events) != 0 {
		t.Fatalf("pending gap: %+v", got)
	}
	if err := live.Append(root, outcomeEvent(2)); err != nil {
		t.Fatal(err)
	}
	if got := readOutcomes(t, root, 1, 100); !got.Reset || got.Cursor != 3 || len(got.Events) != 0 {
		t.Fatalf("marked gap: %+v", got)
	}
	if got := readOutcomes(t, root, 2, 100); got.Reset || len(got.Events) != 1 || got.Events[0].Seq != 3 {
		t.Fatalf("after gap: %+v", got)
	}
}

// E12: mixed commits consume only live IDs for the original notifications.
func TestOutcomesLeaveLiveIDsContiguous(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < live.Limit+3; i++ {
		events := append(live.Changes(factory.Item{}, factory.Item{ID: 1, State: factory.StateShipped}, time.Now()), outcomeEvent(1))
		if err := live.Append(root, events...); err != nil {
			t.Fatal(err)
		}
	}
	events, cursor, err := live.Read(root)
	if err != nil || len(events) != live.Limit || cursor != "live_203" {
		t.Fatalf("live window: %d %s %v", len(events), cursor, err)
	}
	for i, e := range events {
		if e.ID != fmt.Sprintf("live_%d", i+4) || e.Kind != "job.state" {
			t.Fatalf("live event: %+v", e)
		}
	}
}

// E13: a refused snapshot restores both durable records.
func TestOutcomesRefusedWriteRestoresBothRecords(t *testing.T) {
	root := t.TempDir()
	if err := live.Append(root, outcomeEvent(1), live.New("taken", 1, live.Payload{})); err != nil {
		t.Fatal(err)
	}
	before := readOutcomes(t, root, 0, 100)
	liveBefore, cursorBefore, err := live.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("job write refused")
	err = live.Commit(root, []live.Event{outcomeEvent(2), live.New("approved", 2, live.Payload{})}, func() error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("error: %v", err)
	}
	after := readOutcomes(t, root, 0, 100)
	liveAfter, cursorAfter, err := live.Read(root)
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(liveBefore, liveAfter) || cursorBefore != cursorAfter {
		t.Fatalf("refused records: %+v %+v %v", after, liveAfter, err)
	}
}

// E1: titles are bounded in bytes without splitting a Unicode character.
func TestOutcomeTitleRuneBoundary(t *testing.T) {
	title := ""
	for i := 0; i < 90; i++ {
		title += "界"
	}
	events := live.Outcomes(factory.Item{ID: 1, State: factory.StateRunning}, factory.Item{ID: 1, Title: title, State: factory.StateShipped}, time.Now())
	var got live.Outcome
	if len(events) != 1 || json.Unmarshal(events[0].Data, &got) != nil || len(got.Title) != 198 || !utf8.ValidString(got.Title) {
		t.Fatalf("title: %+v", got)
	}
}

// E5: uncommitted trailing entries are ignored and dropped before the next append.
func TestOutcomesIgnoreSequencesBeyondNext(t *testing.T) {
	root := t.TempDir()
	data := []byte(`{"next":1,"floor":0,"events":[{"seq":1,"kind":"finished","job":1},{"seq":2,"kind":"failed","job":99}]}`)
	if err := os.WriteFile(filepath.Join(root, live.OutcomeFile), data, 0600); err != nil {
		t.Fatal(err)
	}
	got := readOutcomes(t, root, 0, 100)
	if len(got.Events) != 1 || got.Cursor != 1 || got.More {
		t.Fatalf("trailing entry: %+v", got)
	}
	if err := live.Append(root, outcomeEvent(2)); err != nil {
		t.Fatal(err)
	}
	got = readOutcomes(t, root, 1, 100)
	if len(got.Events) != 1 || got.Events[0].Job != 2 || got.Events[0].Seq != 2 {
		t.Fatalf("next append: %+v", got)
	}
}

// E6: deliberate erasure removes outcomes without reusing cursors or changing the floor.
func TestOutcomesForgetJobsPreservesSequence(t *testing.T) {
	root := t.TempDir()
	if err := live.Append(root, outcomeEvent(1), outcomeEvent(2)); err != nil {
		t.Fatal(err)
	}
	if err := live.ForgetJobs(root, map[int]bool{1: true}); err != nil {
		t.Fatal(err)
	}
	got := readOutcomes(t, root, 0, 100)
	if got.Reset || got.Cursor != 2 || len(got.Events) != 1 || got.Events[0].Job != 2 || got.Events[0].Seq != 2 {
		t.Fatalf("forgotten: %+v", got)
	}
}

// E13: rollback preserves a pending gap, and removes a newly created outcome file.
func TestOutcomesRefusedGapAndFirstWrite(t *testing.T) {
	for _, older := range []bool{false, true} {
		t.Run(fmt.Sprint(older), func(t *testing.T) {
			root := t.TempDir()
			if older {
				if err := os.WriteFile(filepath.Join(root, live.File), []byte(`{"next":3,"events":[]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			refused := errors.New("snapshot refused")
			err := live.Commit(root, []live.Event{outcomeEvent(1)}, func() error { return refused })
			if !errors.Is(err, refused) {
				t.Fatalf("commit: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, live.OutcomeFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused first outcome left a file: %v", err)
			}
			got := readOutcomes(t, root, 0, 100)
			if got.Reset != older || got.Cursor != 0 || len(got.Events) != 0 {
				t.Fatalf("rollback: %+v", got)
			}
		})
	}
}

// E1: fallback times, transition order and unchanged failure facts are explicit.
func TestOutcomeFallbacksAndTransitionOrder(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	done := now.Add(-time.Hour)
	before := factory.Item{ID: 1, State: factory.StateRunning}
	for _, tt := range []struct {
		item factory.Item
		at   time.Time
	}{
		{factory.Item{ID: 1, State: factory.StateShipped}, now},
		{factory.Item{ID: 1, PullRequest: &factory.PullRequest{State: factory.PRMerged}, DoneAt: done}, done},
		{factory.Item{ID: 1, PullRequest: &factory.PullRequest{State: factory.PRMerged}}, now},
		{factory.Item{ID: 1, State: factory.StateNeedsYou}, now},
		{factory.Item{ID: 1, State: factory.StateNeedsYou, Question: "Which file?"}, time.Time{}},
		{factory.Item{ID: 1, Failure: &factory.Failure{}}, time.Time{}},
	} {
		events := live.Outcomes(before, tt.item, now)
		var got live.Outcome
		if len(events) != 1 || json.Unmarshal(events[0].Data, &got) != nil || !got.At.Equal(tt.at) {
			t.Fatalf("fallback: %+v %+v", tt.item, got)
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(events[0].Data, &payload) != nil || payload["seq"] != nil {
			t.Fatal("sequence was assigned before commit")
		}
	}
	item := factory.Item{ID: 1, PullRequest: &factory.PullRequest{State: factory.PRMerged}, Failure: &factory.Failure{At: now}}
	events := live.Outcomes(before, item, now)
	var kinds []string
	for _, event := range events {
		var got live.Outcome
		if err := json.Unmarshal(event.Data, &got); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, got.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"merged", "failed"}) || len(live.Outcomes(item, item, now)) != 0 {
		t.Fatalf("ordered transitions: %v", kinds)
	}
}

// HealOutcomes closes only a real gap, the way the next commit would: a
// reader of the record resets once to a cursor past the gap and reads on from
// there; a record with no gap, or no record at all, is left as it is.
func TestHealOutcomesClosesOnlyAGap(t *testing.T) {
	empty := t.TempDir()
	if healed, err := live.HealOutcomes(empty); err != nil || healed {
		t.Fatalf("an empty Factory healed: %v %v", healed, err)
	}
	if _, err := os.Stat(filepath.Join(empty, live.OutcomeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("healing an empty Factory wrote an outcome record: %v", err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, live.File), []byte(`{"next":3,"events":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readOutcomes(t, root, 0, 100); !got.Reset || got.Cursor != 0 {
		t.Fatalf("the gap: %+v", got)
	}
	if healed, err := live.HealOutcomes(root); err != nil || !healed {
		t.Fatalf("the gap was not healed: %v %v", healed, err)
	}
	if healed, err := live.HealOutcomes(root); err != nil || healed {
		t.Fatalf("a healed record healed again: %v %v", healed, err)
	}
	once := readOutcomes(t, root, 0, 100)
	if !once.Reset || once.Cursor != 1 {
		t.Fatalf("a reader before the gap: %+v", once)
	}
	if got := readOutcomes(t, root, once.Cursor, 100); got.Reset || len(got.Events) != 0 {
		t.Fatalf("a reader past the gap: %+v", got)
	}
	if err := live.Append(root, outcomeEvent(4)); err != nil {
		t.Fatal(err)
	}
	if got := readOutcomes(t, root, once.Cursor, 100); got.Reset || len(got.Events) != 1 || got.Events[0].Seq != 2 || got.Events[0].Job != 4 {
		t.Fatalf("an outcome after the gap: %+v", got)
	}
}
