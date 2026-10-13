package config

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAppCapsExpireAtLocalMidnightAcrossDSTAndRestart(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 0, 30, 0, 0, loc)
	profile := t.TempDir()
	for _, scope := range []string{"day", "agent", "task"} {
		id, base := "a", 5.0
		if scope == "day" {
			id, base = "", 20
		}
		if scope == "task" {
			id, base = AppTaskCapID("a", "1"), 2
		}
		if err := RaiseAppCap(profile, scope, id, base, 30, true, now); err != nil {
			t.Fatal(err)
		}
		midnight := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
		if cap, err := AppCapAt(profile, scope, id, base, midnight.Add(-time.Nanosecond)); err != nil || cap != 30 {
			t.Fatalf("%s before: %v %v", scope, cap, err)
		}
		if cap, err := AppCapAt(profile, scope, id, base, midnight); err != nil || cap != base {
			t.Fatalf("%s midnight: %v %v", scope, cap, err)
		}
	}
}

func TestAppCapRaiseComparesUnderTheSharedProfileLock(t *testing.T) {
	profile, now := t.TempDir(), time.Now()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- RaiseAppCap(profile, "day", "", 20, 30, true, now) }()
	}
	wg.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrAppCapConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted %d conflicts %d", accepted, conflicts)
	}
	values, err := readProfileConfig(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := values[KeyDailyBudget]; ok {
		t.Fatal("temporary raise changed permanent day default")
	}
}

func TestTemporaryTaskCapFollowsChangedDefaultAfterExpiry(t *testing.T) {
	profile, now := t.TempDir(), time.Now()
	id := AppTaskCapID("a", "1")
	if err := RaiseAppCap(profile, "task", id, 2, 5, true, now); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAppSettings(profile, map[string]json.RawMessage{"spending.per_task": json.RawMessage(`{"usd":"3.00"}`)}, ""); err != nil {
		t.Fatal(err)
	}
	if cap, err := AppCapAt(profile, "task", id, 2, now.AddDate(0, 0, 1)); err != nil || cap != 3 {
		t.Fatalf("cap %v %v", cap, err)
	}
}

func TestAppAgentBindingPreservesTemporaryRaiseAndUnrelatedSettings(t *testing.T) {
	profile, now := t.TempDir(), time.Now()
	if err := WriteDailyBudgetUSD(profile, 42); err != nil {
		t.Fatal(err)
	}
	if err := RaiseAppCap(profile, "agent", "a", 5, 8, true, now); err != nil {
		t.Fatal(err)
	}
	if err := BindAppAgentBudget(profile, "a", 5); err != nil {
		t.Fatal(err)
	}
	if cap, err := AppCapAt(profile, "agent", "a", 5, now); err != nil || cap != 8 {
		t.Fatalf("cap %v %v", cap, err)
	}
	if day, err := DailyBudgetUSDAt(profile); err != nil || day != 42 {
		t.Fatalf("day %v %v", day, err)
	}
}
