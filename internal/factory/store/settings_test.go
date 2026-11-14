package store

import (
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestRunnerSettingsPersistAtomicallyAndTrimOldestLogs(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	st.clock = func() time.Time { return now }
	settings, err := st.ChangeRunnerSettings(func(out *RunnerSettings) error { out.KeepOutputDays = 2; out.KeepOutputMB = 1; return nil })
	if err != nil || settings.KeepOutputDays != 2 {
		t.Fatal(settings, err)
	}
	_, err = st.ChangeRunnerSettings(func(out *RunnerSettings) error { out.RunnerTimeoutSeconds = 90; out.KeepOutputMB = 0; return nil })
	if err == nil {
		t.Fatal("accepted invalid policy")
	}
	settings, _ = st.RunnerSettings()
	if settings.RunnerTimeoutSeconds != DefaultRunnerTimeoutSeconds {
		t.Fatal("partial policy write")
	}
	item, err := st.Create(factory.Item{Title: "logs", Stream: &factory.Stream{Log: []factory.LogLine{
		{At: now.Add(-3 * 24 * time.Hour), Text: "expired"},
		{At: now.Add(-time.Hour), Text: strings.Repeat("a", 700<<10)},
		{At: now, Text: strings.Repeat("b", 700<<10)},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := st.Get(item.ID)
	if err != nil || len(read.Stream.Log) != 1 || read.Stream.Log[0].Text[0] != 'b' {
		t.Fatal("logs did not retain the newest bytes", err)
	}
	raw, _ := os.ReadFile(st.ItemPath(item.ID))
	if strings.Contains(string(raw), "expired") || strings.Contains(string(raw), strings.Repeat("a", 32)) {
		t.Fatal("trimmed logs reached disk")
	}
}

func TestConcurrentListsRetainLogsIndependently(t *testing.T) {
	st := open(t)
	var clock atomic.Int64
	start := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	clock.Store(start.UnixNano())
	st.clock = func() time.Time { return time.Unix(0, clock.Load()) }
	st.SetOutputPolicy(func() (int, int) { return 1, 1 << 20 })
	log := make([]factory.LogLine, 10000)
	for i := range log {
		log[i] = factory.LogLine{At: start.Add(-23 * time.Hour), Text: "old"}
	}
	log = append(log, factory.LogLine{At: start, Text: "recent"})
	if _, err := st.Create(factory.Item{Title: "logs", Stream: &factory.Stream{Log: log}}); err != nil {
		t.Fatal(err)
	}
	first, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	clock.Store(start.Add(2 * time.Hour).UnixNano())
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			for range 20 {
				items, err := st.List()
				if err != nil {
					t.Error(err)
					return
				}
				if len(items[0].Stream.Log) != 1 || items[0].Stream.Log[0].Text != "recent" {
					t.Error("retention lost the recent line")
					return
				}
			}
		})
	}
	group.Wait()
	if len(first[0].Stream.Log) != 10001 {
		t.Fatalf("another List mutated an earlier result: %d lines", len(first[0].Stream.Log))
	}
}
