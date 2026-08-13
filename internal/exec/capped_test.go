package exec

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/rtk"
)

// The collector replaces reading a command's whole output and clamping it
// afterwards, so the test is that it is a replacement: for anything a command
// could print, at any size, arriving in any sized pieces, it renders the exact
// string the old path rendered. Bounded memory is worth nothing if the answer
// moved by a byte.

// The two limits the tests run the collector at. They are the unknown-model
// fallbacks, which is what a toolbox with no catalog behind it uses.
const (
	testCarry = spillBytes
	testKeep  = previewBytes
)

// heldWhole is what the collector would be if memory were free: read the whole
// output, strip the nudge, bound it. The collector must render exactly this
// from a stream it never holds.
func heldWhole(output string, wrapped bool) string {
	if wrapped {
		output = rtk.StripNudge(output)
	}
	bounded, _ := boundResult(output, testCarry, testKeep, shapeCommand, spillRef{})
	return bounded
}

// nudgeFilter is the predicate runShell hands the collector for a wrapped run.
func nudgeFilter(start []byte) bool {
	line := string(start)
	return rtk.StripNudge(line) != line
}

func collect(t *testing.T, output string, chunk int, wrapped bool) *cappedOutput {
	t.Helper()
	var filter func([]byte) bool
	if wrapped {
		filter = nudgeFilter
	}
	collector := newCappedOutput(filter, testCarry, testKeep, nil)
	for rest := output; len(rest) > 0; {
		size := min(chunk, len(rest))
		n, err := collector.Write([]byte(rest[:size]))
		if err != nil || n != size {
			t.Fatalf("write returned %d, %v for %d bytes", n, err, size)
		}
		rest = rest[size:]
	}
	return collector
}

func TestCappedOutputRendersWhatHoldingItWholeWould(t *testing.T) {
	// Multibyte characters sit across both cut points, which is what the
	// whole-rune trimming exists for.
	multibyte := strings.Repeat("日本語のテキストが続きます。", 4000)

	for _, sample := range []struct {
		name   string
		output string
	}{
		{"empty", ""},
		{"one line", "hello\n"},
		{"no trailing newline", "hello"},
		{"under the limit", strings.Repeat("a line of output\n", 100)},
		{"one byte under the limit", strings.Repeat("x", testCarry-1)},
		{"exactly the limit", strings.Repeat("x", testCarry)},
		{"one byte over the limit", strings.Repeat("x", testCarry+1)},
		{"just over the kept window", strings.Repeat("x", testKeep+1)},
		{"far over the limit", strings.Repeat("a verbose build says a great deal\n", 200_000)},
		{"multibyte over the limit", multibyte},
		{"binary", string(binaryNoise(1 << 20))},
		// The line cap alone, with no byte cap in sight: thin output, well
		// inside the carry limit, and past two thousand lines.
		{"over the line cap and under the byte cap", strings.Repeat("x\n", maxResultLines+50)},
		{"exactly the line cap", strings.Repeat("x\n", maxResultLines)},
		// A giant final line, which is where whole-line truncation has to give
		// up and cut inside one.
		{"one line, no newline at all", strings.Repeat("j", 4*testCarry)},
		{"a short line then a giant one", "first\n" + strings.Repeat("j", 4*testCarry)},
	} {
		for _, chunk := range []int{1, 7, 4096, 32 << 10, 1 << 30} {
			for _, wrapped := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/chunks of %d/wrapped=%v", sample.name, chunk, wrapped), func(t *testing.T) {
					got := collect(t, sample.output, chunk, wrapped).String()
					if want := heldWhole(sample.output, wrapped); got != want {
						t.Fatalf("rendered %d bytes, want %d\n got %q\nwant %q",
							len(got), len(want), snipEnds(got), snipEnds(want))
					}
				})
			}
		}
	}
}

// The nudge is a line rtk writes into the middle of somebody else's output, and
// removing a line removes the newline in front of it when it is the last one.
// Every position it can occupy is checked against StripNudge's own answer.
func TestCappedOutputStripsTheNudgeExactly(t *testing.T) {
	const nudge = "[rtk] tip: install the shell hook"
	for _, sample := range []string{
		nudge,
		nudge + "\n",
		nudge + "\nreal output\n",
		"real output\n" + nudge,
		"real output\n" + nudge + "\n",
		"real output\n" + nudge + "\nmore output",
		nudge + "\n" + nudge + "\n",
		"a\n" + nudge + "\nb\n" + nudge + "\nc",
		"not [rtk] at the start of the line\n",
		strings.Repeat("x", 5000) + "\n" + nudge + "\n" + strings.Repeat("y", 5000),
		// The nudge behind more output than survives the clamp: it is elided
		// either way, but the byte count must not count what was removed.
		strings.Repeat("noise\n", 200_000) + nudge + "\n" + strings.Repeat("tail\n", 100),
	} {
		for _, chunk := range []int{1, 3, 64, 4096, 1 << 30} {
			got := collect(t, sample, chunk, true).String()
			if want := heldWhole(sample, true); got != want {
				t.Fatalf("stripping %q in chunks of %d gave %q, want %q",
					snipEnds(sample), chunk, snipEnds(got), snipEnds(want))
			}
		}
	}
}

// The reason the collector exists. Whatever the command prints, what is held is
// the limit and not the output.
func TestCappedOutputHoldsOnlyWhatItKeeps(t *testing.T) {
	collector := newCappedOutput(nudgeFilter, testCarry, testKeep, nil)
	piece := []byte(strings.Repeat("this line is thrown away almost immediately\n", 1000))
	for range 2000 { // ~86 MB through a collector that may hold 10 KB
		collector.Write(piece)
	}
	held := cap(collector.ring) + cap(collector.pending)
	if held > testCarry+cappedLineDecision {
		t.Fatalf("the collector is holding %d bytes of an 86 MB output", held)
	}
	// The last newline is the separator in front of the empty final line, so it
	// is written when the output is settled rather than when it arrived.
	collector.finish()
	if collector.total != len(piece)*2000 {
		t.Fatalf("counted %d bytes of %d", collector.total, len(piece)*2000)
	}
}

// The file the whole output is teed to holds the whole output, and when the
// command prints more than even a file should take, it holds the beginning and
// the notice stops claiming otherwise.
//
// Both halves matter. Disk is not context, so the multiple is generous and
// almost nothing reaches it — but a command CAN print at line rate for two
// minutes, and a truncation notice that promised a complete file when the file
// stopped at a quarter of the output would be a lie the model discovers a turn
// later.
func TestTheSpilledFileHoldsTheWholeOutputUntilItCannot(t *testing.T) {
	const carry, keep = 1024, 256
	for name, size := range map[string]int{
		"comfortably inside the ceiling": 4 * carry,
		"past the ceiling":               2 * carry * spillFileMultiple,
	} {
		t.Run(name, func(t *testing.T) {
			sink := &memoryFile{}
			collector := newCappedOutput(nil, carry, keep,
				func() (io.WriteCloser, string, bool) { return sink, ".obs/x.txt", true })
			body := strings.Repeat("a line of a very loud command\n", size/30)
			collector.Write([]byte(body))
			rendered := collector.String()

			if !sink.closed {
				t.Error("the spill file was left open after the command finished")
			}
			complete := sink.Len() == collector.total
			if complete != !collector.partial {
				t.Fatalf("the file holds %d of %d bytes but partial=%v", sink.Len(), collector.total, collector.partial)
			}
			if complete {
				if sink.String() != body {
					t.Error("the file does not hold what the command printed")
				}
				if !strings.Contains(rendered, "Whole output: .obs/x.txt") {
					t.Errorf("the notice does not point at the complete file: %q", noticeOf(rendered))
				}
				return
			}
			if sink.Len() != carry*spillFileMultiple {
				t.Errorf("the file grew to %d bytes, past the ceiling of %d", sink.Len(), carry*spillFileMultiple)
			}
			if !strings.Contains(rendered, fmt.Sprintf("First %d bytes: .obs/x.txt", sink.Len())) {
				t.Errorf("the notice claims more than the file holds: %q", noticeOf(rendered))
			}
		})
	}
}

// memoryFile is a spill file that never touches a disk.
type memoryFile struct {
	strings.Builder
	closed bool
}

func (m *memoryFile) Close() error { m.closed = true; return nil }

func (m *memoryFile) Write(p []byte) (int, error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	return m.Builder.Write(p)
}

func binaryNoise(size int) []byte {
	noise := make([]byte, size)
	for i := range noise {
		noise[i] = byte(i * 7 % 251)
	}
	return noise
}

func snipEnds(text string) string {
	if len(text) <= 200 {
		return text
	}
	return text[:100] + "…" + text[len(text)-100:]
}
