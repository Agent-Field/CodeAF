//go:build darwin

package processgroup

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDarwinUsageReadsDescendants(t *testing.T) {
	cmd := startDarwinGroup(t, "sh", "-c", `sleep 30 & sleep 30 & sh -c 'sleep 30 & wait' & wait`)
	group := CaptureGroup(cmd.Process.Pid)
	usage := waitForDarwinUsage(t, group, 5)
	if usage.CPUSeconds < 0 {
		t.Fatalf("Usage CPUSeconds = %v, want nonnegative", usage.CPUSeconds)
	}
}

func TestDarwinUsageRefusesUnownedGroup(t *testing.T) {
	if usage, ok := (Group{}).Usage(); ok || usage != (Usage{}) {
		t.Fatalf("unowned Usage = %+v, %v, want zero, false", usage, ok)
	}
	cmd := startDarwinGroup(t, "sleep", "30")
	id, ok := CaptureGroup(cmd.Process.Pid).Identity()
	if !ok {
		t.Fatal("could not capture sleeping leader identity")
	}
	id.Start++
	if usage, ok := Restore(id).Usage(); ok || usage != (Usage{}) {
		t.Fatalf("wrong identity Usage = %+v, %v, want zero, false", usage, ok)
	}

	t.Run("identity changes during sample", func(t *testing.T) {
		group := CaptureGroup(os.Getpid())
		saved := processStart
		t.Cleanup(func() { processStart = saved })
		reads := 0
		processStart = func(pid int) (uint64, bool) {
			reads++
			return group.start, reads == 1
		}
		if usage, ok := group.Usage(); ok || usage != (Usage{}) {
			t.Fatalf("changed identity Usage = %+v, %v, want zero, false", usage, ok)
		}
		if reads != 2 {
			t.Fatalf("identity read %d times, want before and after the sample", reads)
		}
	})
}

func waitForDarwinUsage(tb testing.TB, group Group, processes int) Usage {
	tb.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var usage Usage
	var ok bool
	for time.Now().Before(deadline) {
		usage, ok = group.Usage()
		if ok && usage.Processes >= processes {
			return usage
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("Usage = %+v, %v, want at least %d processes", usage, ok, processes)
	return Usage{}
}

func BenchmarkUsage(b *testing.B) {
	cmd := startDarwinGroup(b, "sh", "-c", strings.Repeat("sleep 30 & ", 9)+"wait")
	group := CaptureGroup(cmd.Process.Pid)
	waitForDarwinUsage(b, group, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if usage, ok := group.Usage(); !ok || usage.Processes < 10 || usage.CPUSeconds < 0 {
			b.Fatalf("Usage = %+v, %v, want the leader and sleeping children", usage, ok)
		}
	}
}
