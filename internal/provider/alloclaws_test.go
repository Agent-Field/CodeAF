package provider

import (
	"fmt"
	"testing"
)

// THE LAWS THIS FILE DEFENDS ARE ABOUT WORK, NOT ABOUT TIME.
//
// Every gate here counts allocations, and an allocation count is the same
// number on a loaded laptop, on a busy CI box and on a machine three years
// faster: it is a fact about the code and never about the weather. A wall-clock
// threshold would be neither, and a suite whose red means "the machine was busy"
// is a suite people learn to re-run instead of read. PERF.md states the doctrine
// once for the whole repository; this is one of the places it is enforced.
//
// A number below that has to move is not a test to relax. It is a change to a
// law, and it belongs in PERF.md in the same commit.

// THE WARM TOOL BLOCK IS ENCODED ZERO TIMES PER CALL.
//
// The belt is append-only and nothing on it moves ([memoizedTools] says so where
// it explains its key), so within one run the schemas going out on turn N+1 are
// the schemas of turn N — the same backing array at the same length. The memo
// therefore hands back its own slice and touches nothing, and that is not
// "cheap": it is nothing at all. Thirty tool schemas are tens of kilobytes of
// JSON re-derived once per provider call, so any allocation reappearing here is
// the whole encode having come back.
//
// BenchmarkEncodeTools measures the same path; this is the part of it that is
// allowed to be an assertion.
func TestTheWarmToolEncodeAllocatesNothing(t *testing.T) {
	for _, count := range []int{12, 30} {
		t.Run(fmt.Sprintf("tools=%d", count), func(t *testing.T) {
			tools := benchTools(count)
			var memo encodeMemo
			if _, err := memo.encodeTools(tools, cacheDialectBreakpoints); err != nil {
				t.Fatalf("warming the tool memo: %v", err)
			}
			allocations := testing.AllocsPerRun(200, func() {
				if _, err := memo.encodeTools(tools, cacheDialectBreakpoints); err != nil {
					t.Fatalf("warm tool encode: %v", err)
				}
			})
			if allocations != 0 {
				t.Fatalf("a warm encode of %d tool schemas allocated %.0f times, want 0 — "+
					"the belt has not changed, so the memo should be handing back the slice "+
					"it already holds and doing no work whatsoever (memo.go, encodeTools)",
					count, allocations)
			}
		})
	}
}

// THE WARM TRANSCRIPT ENCODE COSTS THE SAME AT EIGHTY TURNS AS AT EIGHT.
//
// This is the law the memo exists for, stated as the only thing that can prove
// it: a cost that does not move when the transcript grows tenfold is O(1), and
// one that does is not. Encode runs once per provider call, so a per-call cost
// linear in transcript length is a per-run cost quadratic in the length of the
// run — which is exactly what it was before memo.go.
//
// The two constants are named rather than merely bounded, because each of them
// is a specific thing and a change to either is a change worth reading:
//
//   - automatic: 1 — the []json.RawMessage the caller is handed. Every element
//     of it is a slice the memo already holds. There is nothing else to pay.
//   - breakpoints: 8 — that same slice, plus the two marked positions, which are
//     re-derived per call BY DESIGN. The tail marker rolls forward every turn, so
//     a memo of the marked form would be a cache of the one thing that changes
//     (memo.go says this where it explains what is deliberately not memoized).
//     Two marshals, whatever they cost, and never 244.
//
// The equality across sizes is the load-bearing assertion; the constants are the
// teaching. One-per-message at 81 turns would be 244.
func TestTheWarmTranscriptEncodeCostsTheSameAtEightyTurnsAsAtEight(t *testing.T) {
	warmEncode := func(t *testing.T, turns int, dialect cacheDialect) float64 {
		t.Helper()
		messages := benchTranscript(turns)
		var memo encodeMemo
		if _, err := memo.encodeMessages(messages, dialect); err != nil {
			t.Fatalf("warming the transcript memo: %v", err)
		}
		return testing.AllocsPerRun(100, func() {
			if _, err := memo.encodeMessages(messages, dialect); err != nil {
				t.Fatalf("warm transcript encode: %v", err)
			}
		})
	}

	for _, dialect := range []struct {
		name string
		d    cacheDialect
		want float64
	}{
		{"automatic", cacheDialectAutomatic, 1},
		{"breakpoints", cacheDialectBreakpoints, 8},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			// 8 and 81 turns are the ends of the range BENCHMARKS.md records for
			// real runs, and they are 25 and 244 messages long.
			small, large := warmEncode(t, 8, dialect.d), warmEncode(t, 81, dialect.d)
			if small != large {
				t.Fatalf("a warm encode costs %.0f allocations at 8 turns and %.0f at 81 — "+
					"the memo's whole promise is that this number does not move with the "+
					"transcript, and a cost that grows here is a per-run cost that grows "+
					"quadratically (memo.go)", small, large)
			}
			if large != dialect.want {
				t.Fatalf("a warm %s encode costs %.0f allocations, and the law is %.0f. "+
					"If this is a deliberate change — a different marked-message shape, a "+
					"different result slice — move the number here and in PERF.md together; "+
					"if it is not, something on the warm path stopped being memoized.",
					dialect.name, large, dialect.want)
			}
		})
	}
}

// windowedDeltas is a streamed reply cut into pieces of exactly babbleEvery
// bytes, so one delta is one guard window and "allocations per pass" divides
// into "allocations per window" with nothing left over.
func windowedDeltas(windows int) []string {
	// plainProse writes about ninety bytes per sentence, but that is its
	// business and not this test's: ask for more until there is enough.
	prose := ""
	for sentences := 2 * windows; len(prose) < windows*babbleEvery; sentences *= 2 {
		prose = plainProse(sentences)
	}
	deltas := make([]string, 0, windows)
	for index := 0; index < windows; index++ {
		deltas = append(deltas, prose[index*babbleEvery:(index+1)*babbleEvery])
	}
	return deltas
}

// THE GUARD BUILDS ONE COMPRESSOR PER STREAM AND NEVER ANOTHER.
//
// [babbleWatch.loopedTail] runs a zlib writer over the four-kilobyte window once
// every babbleEvery bytes of a reply, which is hundreds of times in a long
// answer. zlib.NewWriter is cheap to count and expensive to weigh: the deflate
// state behind it is a hundred kilobytes wide, allocated on its first write, and
// building one per window costs about nineteen allocations and eight hundred
// kilobytes EACH — sixty megabytes of garbage over a single long reply, produced
// inside the provider's read loop while the model is still writing. Reset leaves
// the writer in exactly the state a new one would be in, so the ratio measured is
// the same ratio bit for bit and the reuse costs the guard nothing.
//
// Both halves of that are pinned. The pointer says the writer is the same writer,
// which is the law in one line; the allocation bound says nothing else on the
// per-window path started allocating either, and it is set where a rebuilt writer
// cannot hide — a window costs about 1.6 allocations today and a rebuild adds
// nineteen.
func TestTheBabbleGuardBuildsOneCompressorPerStream(t *testing.T) {
	const windows = 200
	deltas := windowedDeltas(windows)

	watch := &babbleWatch{}
	for _, delta := range deltas {
		if watch.write(delta) {
			t.Fatal("innocent prose tripped the guard")
		}
	}
	built := watch.squeeze
	if built == nil {
		t.Fatal("two hundred full windows and the guard never built a compressor — " +
			"this test is measuring nothing, so the corpus or babbleEvery has moved")
	}

	allocations := testing.AllocsPerRun(20, func() {
		for _, delta := range deltas {
			watch.write(delta)
		}
	})

	if watch.squeeze != built {
		t.Fatal("the guard replaced its zlib writer part-way through one stream. " +
			"It is held for the life of the stream and Reset per window on purpose " +
			"(streamguard.go, babbleWatch.squeeze): a writer per window is a hundred " +
			"kilobytes of deflate state per five hundred bytes of reply.")
	}
	// Four is the ceiling and 1.6 is the measurement; the gap is there so this
	// never goes red for a rounding, and it is still five times under the cost of
	// one rebuilt compressor.
	if perWindow := allocations / windows; perWindow > 4 {
		t.Fatalf("one guard window costs %.2f allocations, and the law is at most 4. "+
			"A window is a Reset, a write and a ratio; anything that allocates per "+
			"window is being paid once per %d bytes of every reply this harness streams.",
			perWindow, babbleEvery)
	}
}
