package provider

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
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

// markedJSONPrice prices only the encoding/json calls the benchmark's two
// marked shapes are designed to make. Their inputs are built before measuring,
// so this baseline cannot absorb an allocation added inside marshalMarked or
// the memo. A new fixture shape needs its own price rather than a guess.
func markedJSONPrice(t *testing.T, message ai.Message) float64 {
	t.Helper()
	// A wire type's own marshaler makes encoding/json's measured price include this package's work.
	// A deliberate marshaler needs named overhead here and in PERF.md in the same change.
	for _, wireType := range []reflect.Type{
		reflect.TypeFor[markedTextPart](),
		reflect.TypeFor[markedMessage](),
		reflect.TypeFor[markedToolMessage](),
	} {
		for _, candidate := range []reflect.Type{wireType, reflect.PointerTo(wireType)} {
			for _, marshaler := range []reflect.Type{
				reflect.TypeFor[json.Marshaler](),
				reflect.TypeFor[encoding.TextMarshaler](),
			} {
				if candidate.Implements(marshaler) {
					t.Fatalf("%v implements %v: encoding/json's price is measured only while the wire types "+
						"have no marshaler of their own. A marshaler added on purpose is this package's work "+
						"and must be priced as a named overhead here and in PERF.md together.", candidate, marshaler)
				}
			}
		}
	}
	if len(message.Content) != 1 || message.Content[0].Type != "text" || len(message.ToolCalls) != 0 {
		t.Fatalf("marked JSON price has no law for role %q with %d content parts and %d tool calls",
			message.Role, len(message.Content), len(message.ToolCalls))
	}
	price := func(marshal func() ([]byte, error)) float64 {
		return testing.AllocsPerRun(100, func() {
			if _, err := marshal(); err != nil {
				t.Fatalf("pricing encoding/json: %v", err)
			}
		})
	}
	part := message.Content[0]
	switch message.Role {
	case "system":
		if message.ToolCallID != "" {
			t.Fatalf("marked JSON price has no law for a system message with tool id %q", message.ToolCallID)
		}
		marshalPart := func() ([]byte, error) {
			return json.Marshal(markedTextPart{
				Type: part.Type, Text: part.Text, CacheControl: ephemeralBreakpoint,
			})
		}
		raw, err := marshalPart()
		if err != nil {
			t.Fatalf("building marked JSON price inputs: %v", err)
		}
		parts := []json.RawMessage{raw}
		return price(marshalPart) + price(func() ([]byte, error) {
			return json.Marshal(markedMessage{Role: message.Role, Content: parts})
		})
	case "tool":
		if message.ToolCallID == "" {
			t.Fatal("marked JSON price has no law for a tool result without an id")
		}
		return price(func() ([]byte, error) {
			return json.Marshal(markedToolMessage{
				Role: message.Role, Content: part.Text,
				ToolCallID: message.ToolCallID, CacheControl: ephemeralBreakpoint,
			})
		})
	default:
		t.Fatalf("marked JSON price has no law for role %q", message.Role)
		return 0
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
// This package's own allocations are named rather than merely bounded, because
// each is a specific thing and a change to one is a change worth reading:
//
//   - automatic: 1 — the []json.RawMessage the caller is handed. Every element
//     of it is a slice the memo already holds. There is nothing else to pay.
//   - breakpoints: that same slice, the parts slice the marked system message is
//     expanded into, and encoding/json's price for the two marked positions.
//     The tool result has no overhead of its own. The marked forms are re-derived
//     per call BY DESIGN: the tail marker rolls forward every turn, so a memo of
//     it would cache the one thing that changes (memo.go explains this choice).
//
// ONLY ENCODING/JSON'S PRICE IS MEASURED IN THIS PROCESS. The complete
// breakpoints path cost 8 on Go 1.26 and 11 on Go 1.27 with the memo unchanged,
// so a total copied from one toolchain turned a Go upgrade into a red that CI
// could not see. The baseline now calls encoding/json directly with this
// package's wire types and prebuilt inputs; it never calls marshalMarked or
// encodeMessages. This package's own costs stay named, and each marked position
// is checked before the whole warm path. An extra allocation of this package's
// own ANYWHERE ON THAT PATH, inside marshalMarked included, therefore fails
// instead of raising its own allowance.
//
// The equality across sizes is the load-bearing assertion; the constants are the
// teaching. One-per-message at 81 turns would be 244.
func TestTheWarmTranscriptEncodeCostsTheSameAtEightyTurnsAsAtEight(t *testing.T) {
	const (
		// resultSliceCost is the slice the caller needs to hold this call's encoded transcript.
		resultSliceCost = 1
		// partsSliceCost is the storage needed to expand the marked system message into array form.
		partsSliceCost = 1
		// toolOverhead is zero because the marked tool result needs no intermediate storage.
		toolOverhead = 0
	)
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

	messages := benchTranscript(81)
	placed := breakpointsFor(messages)
	if placed.system < 0 || placed.tail < 0 {
		t.Fatal("the benchmark transcript must place both a system and a tail breakpoint")
	}
	systemJSON := markedJSONPrice(t, messages[placed.system])
	tailJSON := markedJSONPrice(t, messages[placed.tail])
	for _, position := range []struct {
		name     string
		index    int
		jsonCost float64
		ownCost  float64
	}{
		{"system", placed.system, systemJSON, partsSliceCost},
		{"tail", placed.tail, tailJSON, toolOverhead},
	} {
		allocations := testing.AllocsPerRun(100, func() {
			if _, err := marshalMarked(messages[position.index]); err != nil {
				t.Fatalf("marked %s position: %v", position.name, err)
			}
		})
		t.Logf("marked %s position: encoding/json=%.0f package=%.0f total=%.0f",
			position.name, position.jsonCost, position.ownCost, allocations)
		if want := position.jsonCost + position.ownCost; allocations != want {
			change := "Fewer allocations mean the marked shape or its named overhead has changed."
			if allocations > want {
				change = "An extra allocation inside marshalMarked must fail rather than raise its own allowance."
			}
			t.Fatalf("marked %s position (%d) costs %.0f allocations, want %.0f: "+
				"encoding/json's price is measured in-process (%.0f), and this package's "+
				"own overhead is named (%.0f). %s If this is deliberate, update markedJSONPrice's "+
				"shapes, the named overhead constants, and the PERF.md row together.",
				position.name, position.index, allocations, want, position.jsonCost, position.ownCost, change)
		}
	}

	for _, dialect := range []struct {
		name string
		d    cacheDialect
		want float64
	}{
		{"automatic", cacheDialectAutomatic, resultSliceCost},
		{"breakpoints", cacheDialectBreakpoints, resultSliceCost + systemJSON + partsSliceCost + tailJSON + toolOverhead},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			// 8 and 81 turns are the ends of the range BENCHMARKS.md records for
			// real runs, and they are 25 and 244 messages long.
			small, large := warmEncode(t, 8, dialect.d), warmEncode(t, 81, dialect.d)
			t.Logf("warm %s encode: 8 turns=%.0f 81 turns=%.0f law=%.0f",
				dialect.name, small, large, dialect.want)
			if small != large {
				t.Fatalf("a warm encode costs %.0f allocations at 8 turns and %.0f at 81 — "+
					"the memo's whole promise is that this number does not move with the "+
					"transcript, and a cost that grows here is a per-run cost that grows "+
					"quadratically (memo.go)", small, large)
			}
			if large != dialect.want {
				t.Fatalf("a warm %s encode costs %.0f allocations, and the law is %.0f: "+
					"encoding/json's price is measured in-process, and this package's own "+
					"allocations are named. An extra allocation of this package's own "+
					"anywhere on the warm path, inside marshalMarked included, fails. If "+
					"this is deliberate, change the law here and in PERF.md together; if "+
					"it is not, the warm path is doing work it should not.",
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
