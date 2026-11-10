package upgradenotice

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func standingHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "v3", "standing")
	if err := os.CopyFS(dir, os.DirFS("testdata/standing")); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestStandingUpgradeInventoryAndOnce(t *testing.T) {
	home := standingHome(t)
	before, _ := os.ReadFile(filepath.Join(home, "v3/standing/0.json"))
	line, err := Standing(home, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"5 standing orders", "1 rule", "1 paused", "1 retired", "2 unreadable or unknown-format files", "/automations", "/remember", filepath.Join(home, "v3/standing-upgrade-report.txt")} {
		if !strings.Contains(line, want) {
			t.Errorf("notice %q lacks %q", line, want)
		}
	}
	report, err := os.ReadFile(filepath.Join(home, "v3/standing-upgrade-report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"earlier hold", "earlier at", "earlier every", "earlier probe", "earlier file", "earlier idle", "instruction hold", "/work/project", "old schedule", "1h", "paused", "retired", "damaged.json", "unreadable", "unknown.json", "unknown schema 99", "future words", "0.json"} {
		if !strings.Contains(string(report), want) {
			t.Errorf("report lacks %q", want)
		}
	}
	if line, err := Standing(home, true); err != nil || line != "" {
		t.Fatalf("second launch: %q %v", line, err)
	}
	after, _ := os.ReadFile(filepath.Join(home, "v3/standing/0.json"))
	if string(before) != string(after) {
		t.Fatal("original item changed")
	}
}

func TestStandingUpgradeEmptyHeadlessAndRetry(t *testing.T) {
	empty := t.TempDir()
	if line, err := Standing(empty, true); err != nil || line != "" {
		t.Fatalf("empty: %q %v", line, err)
	}
	if _, err := os.Stat(filepath.Join(empty, "v3")); !os.IsNotExist(err) {
		t.Fatal("empty home was written")
	}
	home := standingHome(t)
	if line, err := Standing(home, false); err != nil || line != "" {
		t.Fatalf("headless: %q %v", line, err)
	}
	ack := filepath.Join(home, "v3/standing-upgrade-noticed-v1.json")
	if _, err := os.Stat(ack); !os.IsNotExist(err) {
		t.Fatal("headless consumed notice")
	}
	report := filepath.Join(home, "v3/standing-upgrade-report.txt")
	if err := os.Mkdir(report, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Standing(home, true); err == nil {
		t.Fatal("report failure hidden")
	}
	if _, err := os.Stat(ack); !os.IsNotExist(err) {
		t.Fatal("failed report consumed notice")
	}
	if err := os.Remove(report); err != nil {
		t.Fatal(err)
	}
	if line, err := Standing(home, true); err != nil || line == "" {
		t.Fatalf("retry: %q %v", line, err)
	}
}

func TestStandingUpgradeConcurrentHomes(t *testing.T) {
	home := standingHome(t)
	var wg sync.WaitGroup
	lines := make(chan string, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			line, err := Standing(home, true)
			if err != nil {
				t.Error(err)
			}
			lines <- line
		}()
	}
	wg.Wait()
	close(lines)
	n := 0
	for line := range lines {
		if line != "" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("notice shown %d times", n)
	}
	if line, err := Standing(standingHome(t), true); err != nil || line == "" {
		t.Fatalf("other home: %q %v", line, err)
	}
}
