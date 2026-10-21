package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestProjectSettingsStampSharesReadsAndKeepsFallbackFresh(t *testing.T) {
	project, profile := t.TempDir(), t.TempDir()
	path := ProjectConfigPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, model string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"models.tiers.low":"`+model+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "first")
	before := projectConfigMemo.Reads()
	check := func(want string, reads uint64) {
		t.Helper()
		var wg sync.WaitGroup
		for range 24 {
			wg.Go(func() {
				got, err := ProjectStringAt(project, profile, KeyTierLowModel)
				if err != nil || got != want {
					t.Errorf("value=%q err=%v want %q", got, err, want)
				}
			})
		}
		wg.Wait()
		if projectConfigMemo.Reads()-before != reads {
			t.Fatalf("reads=%d want %d", projectConfigMemo.Reads()-before, reads)
		}
	}
	check("first", 1)
	info, _ := os.Stat(path)
	write(path+".new", "other")
	if err := os.Chtimes(path+".new", info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	check("other", 2)
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	write(BudgetConfigPath(profile), "profile")
	check("profile", 3)
	write(BudgetConfigPath(profile), "changed-profile")
	check("changed-profile", 3)
}

func TestProjectSettingsUnreadableCurrentDoesNotFallBack(t *testing.T) {
	project := t.TempDir()
	path := ProjectConfigPath(project)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := LoadProjectConfig(project); err == nil || os.IsNotExist(err) {
			t.Fatalf("unreadable settings became an empty layer: %v", err)
		}
	}
}
