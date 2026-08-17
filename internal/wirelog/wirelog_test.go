package wirelog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sink is a log the test can read while the meter still holds it.
type sink struct {
	bytes.Buffer
	closed bool
}

func (s *sink) Close() error { s.closed = true; return nil }

// clock hands the meter a second whenever the test says so.
type clock struct{ at time.Time }

func (c *clock) now() time.Time       { return c.at }
func (c *clock) tick(d time.Duration) { c.at = c.at.Add(d) }

// terminal is a real *os.File standing in for the tty, because Meter carries a
// descriptor through and there is nothing to carry through a bytes.Buffer.
func terminal(t *testing.T) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "tty"))
	if err != nil {
		t.Fatalf("temp terminal: %v", err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

// rows returns the log's data lines, header and blank dropped.
func rows(t *testing.T, log string) []string {
	t.Helper()
	out := []string{}
	for _, line := range strings.Split(log, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func TestMeterCountsBytesAndWrites(t *testing.T) {
	tick := &clock{at: time.Unix(1_700_000_000, 0)}
	log := &sink{}
	meter := newMeter(terminal(t), log, tick.now)

	for _, frame := range []string{"abc", "de", "fghij"} {
		if _, err := meter.Write([]byte(frame)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if got, want := meter.Total(), int64(10); got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	if err := meter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !log.closed {
		t.Error("Close left the log file open")
	}

	got := rows(t, log.String())
	if len(got) != 1 {
		t.Fatalf("rows = %q, want one second", got)
	}
	if want := "1700000000000 10 3 10"; got[0] != want {
		t.Errorf("row = %q, want %q", got[0], want)
	}
}

func TestMeterBucketsPerSecondAndFillsSilence(t *testing.T) {
	tick := &clock{at: time.Unix(1_700_000_000, 900*int64(time.Millisecond))}
	log := &sink{}
	meter := newMeter(terminal(t), log, tick.now)

	// Two writes inside the first second, then three silent seconds, then one
	// more write. The silent seconds have to show up as zeros: "we drew
	// nothing" is the answer the idle measurement is made of.
	meter.Write([]byte("0123456789")) //nolint:errcheck
	tick.tick(50 * time.Millisecond)
	meter.Write([]byte("012345678901234")) //nolint:errcheck
	tick.tick(4 * time.Second)
	meter.Write([]byte("x")) //nolint:errcheck
	if err := meter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	want := []string{
		"1700000000000 25 2 25",
		"1700000001000 0 0 25",
		"1700000002000 0 0 25",
		"1700000003000 0 0 25",
		"1700000004000 1 1 26",
	}
	got := rows(t, log.String())
	if len(got) != len(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMeterWritesThroughToTheTerminal(t *testing.T) {
	tty := terminal(t)
	meter := newMeter(tty, &sink{}, (&clock{at: time.Unix(1, 0)}).now)
	if _, err := meter.Write([]byte("\x1b[?2026hframe\x1b[?2026l")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := meter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The terminal is still ours to close, which is the point: Close closes the
	// log and leaves the tty alone.
	if _, err := tty.Write([]byte("still open")); err != nil {
		t.Fatalf("terminal closed under us: %v", err)
	}
	painted, err := os.ReadFile(tty.Name())
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if want := "\x1b[?2026hframe\x1b[?2026lstill open"; string(painted) != want {
		t.Errorf("terminal saw %q, want %q", painted, want)
	}
}

func TestMeterCarriesTheDescriptor(t *testing.T) {
	tty := terminal(t)
	meter := newMeter(tty, &sink{}, time.Now)
	defer meter.Close() //nolint:errcheck
	if got, want := meter.Fd(), tty.Fd(); got != want {
		t.Errorf("Fd = %d, want the terminal's %d", got, want)
	}
}

func TestFromEnvIsNilWhenUnset(t *testing.T) {
	t.Setenv(EnvVar, "")
	if meter := FromEnv(os.Stdout); meter != nil {
		t.Fatalf("FromEnv with %s unset returned %#v, want nil", EnvVar, meter)
	}
}

func TestFromEnvIsNilWhenTheLogCannotBeOpened(t *testing.T) {
	// A directory that does not exist. The surface still has to open.
	t.Setenv(EnvVar, filepath.Join(t.TempDir(), "nope", "wire.log"))
	if meter := FromEnv(os.Stdout); meter != nil {
		_ = meter.Close()
		t.Fatal("FromEnv returned a meter for an unopenable log")
	}
}

func TestOpenAppendsAndSweepsSilentSeconds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wire.log")
	t.Setenv(EnvVar, path)

	meter := FromEnv(terminal(t))
	if meter == nil {
		t.Fatal("FromEnv returned nil for a writable log")
	}
	meter.Write(bytes.Repeat([]byte("f"), 100)) //nolint:errcheck
	// The sweeper, not a write, has to close this second.
	time.Sleep(1200 * time.Millisecond)
	if err := meter.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if n := len(rows(t, string(first))); n < 2 {
		t.Errorf("rows = %d, want the written second and at least one swept one", n)
	}

	// A second run against the same path adds to it rather than erasing it.
	again := FromEnv(terminal(t))
	if again == nil {
		t.Fatal("second FromEnv returned nil")
	}
	again.Write([]byte("second run")) //nolint:errcheck
	if err := again.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !bytes.HasPrefix(second, first) {
		t.Error("the second run truncated the first run's log")
	}
	if got, want := bytes.Count(second, []byte("# unix_ms")), 2; got != want {
		t.Errorf("headers = %d, want %d (one per run)", got, want)
	}
}
