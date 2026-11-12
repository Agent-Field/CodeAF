package shardfixture

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func record(t *testing.T, phase string) {
	t.Helper()
	path := os.Getenv("SHARD_FIXTURE_TRACE")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s %s\n", t.Name(), phase); err != nil {
		t.Fatal(err)
	}
}

func TestAlpha(t *testing.T) {
	if os.Getenv("SHARD_FIXTURE_MISMATCH") != "" {
		// An extra result, as a helper process sharing stdout would print. Its
		// lower-case name sorts differently outside the C locale.
		fmt.Println("--- PASS: TestaGhost (0.00s)")
	}
	record(t, "done")
}

func TestBravoWithSubtest(t *testing.T) {
	t.Run("child", func(t *testing.T) {})
	record(t, "done")
}

func TestCharlieCanFail(t *testing.T) {
	if os.Getenv("SHARD_FIXTURE_FAIL") != "" {
		t.Fatal("fixture failure requested")
	}
	if os.Getenv("SHARD_FIXTURE_MISMATCH") != "" {
		// No newline: this test's own result line is no longer at a line start.
		fmt.Print("unfinished line")
	}
	record(t, "done")
}

func TestDeltaCanSleep(t *testing.T) {
	if duration := os.Getenv("SHARD_FIXTURE_SLEEP"); duration != "" {
		d, err := time.ParseDuration(duration)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(d)
	}
	record(t, "done")
}
