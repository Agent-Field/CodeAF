package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the batch: what it says, and how often ──────────────────────────────────

// formingClock is a hand-wound clock for the throttle. Nothing here may depend
// on wall time: a test that slept a tenth of a second per fragment would be
// measuring the machine rather than the rule.
type formingClock struct{ at time.Time }

func (c *formingClock) tick(d time.Duration) { c.at = c.at.Add(d) }

func newFormingBatch() (*formingBatch, *formingClock) {
	clock := &formingClock{at: time.Unix(1_700_000_000, 0)}
	return &formingBatch{now: func() time.Time { return clock.at }}, clock
}

// feedForming pushes one accumulated-arguments state in, as the provider would.
func feedForming(batch *formingBatch, index int, id, tool, args string) (Event, bool) {
	return batch.note(provider.StreamEvent{
		Kind: provider.StreamToolCallForming, Index: index, ID: id, Tool: tool, Delta: args,
	})
}

// A write is named the moment its path closes — while the file body, which is
// the part that takes seconds, is still arriving. That is the whole point: the
// row says "write internal/foo.go" for the duration instead of saying nothing.
func TestAWriteIsGlossedByItsPathWhileItsBodyStillArrives(t *testing.T) {
	batch, clock := newFormingBatch()

	// The first fragment: the name, no arguments yet.
	event, spoke := feedForming(batch, 0, "call_1", "write", "")
	if !spoke {
		t.Fatal("the first fragment said nothing")
	}
	if event.Kind != EventToolForming || event.Tool != "write" || event.CallID != "call_1" {
		t.Fatalf("first forming = %+v, want the call named", event)
	}
	if event.Hint != "" {
		t.Fatalf("hint = %q before any field closed, want empty", event.Hint)
	}

	// The path arrives and CLOSES. This speaks regardless of the throttle.
	clock.tick(time.Millisecond)
	event, spoke = feedForming(batch, 0, "call_1", "write", `{"path":"internal/foo.go",`)
	if !spoke {
		t.Fatal("a closing field was swallowed by the throttle")
	}
	if event.Hint != "write internal/foo.go" {
		t.Fatalf("hint = %q, want the path gloss", event.Hint)
	}

	// The body streams. The hint does not change and the byte count grows.
	clock.tick(200 * time.Millisecond)
	body := `{"path":"internal/foo.go","content":"package main\n\nfunc main() {}`
	event, spoke = feedForming(batch, 0, "call_1", "write", body)
	if !spoke {
		t.Fatal("a fragment past the interval said nothing")
	}
	if event.Hint != "write internal/foo.go" {
		t.Fatalf("hint = %q mid-body, want it unchanged", event.Hint)
	}
	if event.Bytes != len(body) {
		t.Fatalf("Bytes = %d, want %d — the size of what has arrived", event.Bytes, len(body))
	}
	if event.ArgsText != body {
		t.Fatalf("ArgsText = %q, want the raw partial arguments", event.ArgsText)
	}
}

// The hint field per tool, each read from the arguments as they close. The
// gloss is [gloss]'s shape, so the forming row and the announced row read as one
// line that gained detail rather than as two different sentences.
func TestFormingHintPerTool(t *testing.T) {
	for _, probe := range []struct {
		tool string
		args string
		want string
	}{
		{"read", `{"path":"a.go"}`, "read a.go"},
		{"edit", `{"path":"internal/session/loop.go","old":"x`, "edit internal/session/loop.go"},
		{"bash", `{"command":"go test ./...","timeout":30`, "bash go test ./..."},
		{"propose_task", `{"title":"port the forming events","brief":"the gap is`, "propose_task port the forming events"},
		{"tasks", `{"id":"t-42","resolve":"done"`, "tasks t-42"},
		// The verb alone, when the id has not closed yet: the row says what it
		// can, which is more than nothing.
		{"tasks", `{"resolve":"accept"`, "tasks accept"},
		// A tool with no gloss field says only that something is arriving.
		{"unknown_hand", `{"whatever":"value"`, ""},
	} {
		batch, _ := newFormingBatch()
		event, spoke := feedForming(batch, 0, "call_1", probe.tool, probe.args)
		if !spoke {
			t.Fatalf("%s: the first fragment said nothing", probe.tool)
		}
		if event.Hint != probe.want {
			t.Fatalf("%s (%s): hint = %q, want %q", probe.tool, probe.args, event.Hint, probe.want)
		}
	}
}

// A field STILL ARRIVING is not a hint. Half a path is a lie a surface would
// print as a fact, so the gloss waits for the closing quote.
func TestAnUnclosedFieldIsNotAHint(t *testing.T) {
	batch, clock := newFormingBatch()
	if _, spoke := feedForming(batch, 0, "call_1", "write", `{"path":"internal/ses`); !spoke {
		t.Fatal("the first fragment said nothing")
	}
	clock.tick(time.Second)
	event, spoke := feedForming(batch, 0, "call_1", "write", `{"path":"internal/session/lo`)
	if !spoke {
		t.Fatal("a fragment past the interval said nothing")
	}
	if event.Hint != "" {
		t.Fatalf("hint = %q from a path that has not closed", event.Hint)
	}
}

// THE THROTTLE. A token-per-fragment endpoint must not put a token-per-event
// load on the pump — but the two moments the row changes what it SAYS are never
// swallowed, because those are exactly the moments worth interrupting for.
func TestFormingIsThrottledButNeverSwallowsAChange(t *testing.T) {
	batch, clock := newFormingBatch()

	// Fragment 1: the id, no name yet. It speaks — something is arriving.
	if _, spoke := feedForming(batch, 0, "call_1", "", ""); !spoke {
		t.Fatal("the first sight of a call said nothing")
	}
	// Fragment 2, immediately after, no name and no close: swallowed.
	clock.tick(time.Millisecond)
	if _, spoke := feedForming(batch, 0, "call_1", "", ""); spoke {
		t.Fatal("a fragment inside the interval spoke")
	}
	// Fragment 3: the NAME lands. Always speaks.
	clock.tick(time.Millisecond)
	event, spoke := feedForming(batch, 0, "call_1", "bash", "")
	if !spoke {
		t.Fatal("the name landing was swallowed")
	}
	if event.Tool != "bash" {
		t.Fatalf("Tool = %q, want the name that just landed", event.Tool)
	}
	// The command now streams a character at a time, the way an endpoint that
	// streams arguments per token does. Nothing closes and nothing is named, so
	// only an interval crossing may speak — and 14 fragments five milliseconds
	// apart do not cross one.
	const command = `go build ./...`
	spoken := 0
	for step := 1; step <= len(command); step++ {
		clock.tick(5 * time.Millisecond)
		if _, spoke := feedForming(batch, 0, "call_1", "bash", `{"command":"`+command[:step]); spoke {
			spoken++
		}
	}
	if spoken != 0 {
		t.Fatalf("%d of %d fragments inside the interval spoke, want none", spoken, len(command))
	}
	// The CLOSE always speaks, however soon it lands.
	clock.tick(time.Millisecond)
	event, spoke = feedForming(batch, 0, "call_1", "bash", `{"command":"`+command+`"`)
	if !spoke {
		t.Fatal("a closing field was swallowed")
	}
	if event.Hint != "bash go build ./..." {
		t.Fatalf("hint = %q, want the command gloss", event.Hint)
	}
}

// Two calls in one batch keep their own state: the throttle is per call, and a
// busy first call must not silence a second one's first sighting.
func TestParallelCallsFormIndependently(t *testing.T) {
	batch, clock := newFormingBatch()
	if _, spoke := feedForming(batch, 0, "call_1", "read", `{"path":"a.go"`); !spoke {
		t.Fatal("call 0 said nothing")
	}
	clock.tick(time.Millisecond)
	event, spoke := feedForming(batch, 1, "call_2", "read", `{"path":"b.go"`)
	if !spoke {
		t.Fatal("call 1's first sighting was swallowed by call 0's throttle")
	}
	if event.CallID != "call_2" || event.Hint != "read b.go" {
		t.Fatalf("call 1 forming = %+v, want its own identity", event)
	}
}

// A retry throws the half-formed calls away with everything else the dead
// attempt started. Keeping them would gloss the new response's first call with
// the old one's path.
func TestFormingResetForgetsTheDeadAttempt(t *testing.T) {
	batch, clock := newFormingBatch()
	if _, spoke := feedForming(batch, 0, "call_1", "write", `{"path":"old.go"`); !spoke {
		t.Fatal("the first fragment said nothing")
	}
	batch.reset()
	clock.tick(time.Millisecond)
	// The same index, a new response. It is a first sighting again — it speaks
	// despite the throttle, and it carries nothing of the old call.
	event, spoke := feedForming(batch, 0, "call_9", "read", `{"path":"new.go"`)
	if !spoke {
		t.Fatal("the retry's first fragment was swallowed by the dead attempt's throttle")
	}
	if event.Hint != "read new.go" {
		t.Fatalf("hint = %q after a reset, want the new call's gloss", event.Hint)
	}
	// A nil batch is inert rather than a panic: reset and note are both called
	// from paths that must not fail.
	var absent *formingBatch
	absent.reset()
	if _, spoke := absent.note(provider.StreamEvent{}); spoke {
		t.Fatal("a nil batch spoke")
	}
}

// ── the scanner ─────────────────────────────────────────────────────────────

// It is not a JSON parser and must never become one: every input it sees is a
// PREFIX, which json.Unmarshal calls invalid. These are the shapes that would
// break a parser and must not break a scanner.
func TestPartialArgsScansPrefixesWithoutFailing(t *testing.T) {
	for _, probe := range []struct {
		name   string
		text   string
		field  string
		want   string
		closed bool
	}{
		{"empty", "", "path", "", false},
		{"brace only", "{", "path", "", false},
		{"key half sent", `{"pa`, "path", "", false},
		{"value half sent", `{"path":"a.`, "path", "", false},
		{"value closed", `{"path":"a.go"`, "path", "a.go", true},
		// Escapes: a quote inside the string does not end it.
		{"escaped quote", `{"command":"echo \"hi\"","x`, "command", `echo "hi"`, true},
		{"escaped backslash", `{"path":"C:\\tmp\\x"`, "path", `C:\tmp\x`, true},
		{"newline escape", `{"title":"one\ntwo"`, "title", "one\ntwo", true},
		{"unicode escape", `{"title":"caf\u00e9"`, "title", "café", true},
		{"half a unicode escape", `{"title":"caf\u00`, "title", "", false},
		// A nested object's fields are NOT top level and are not captured, and
		// the scanner comes back out of it correctly.
		{"nested object skipped", `{"opts":{"path":"inner"},"path":"outer"`, "path", "outer", true},
		{"nested array skipped", `{"paths":["a","b"],"path":"outer"`, "path", "outer", true},
		// A key that looks like a value inside a nested structure must not be
		// mistaken for the top-level one.
		{"nested only", `{"opts":{"path":"inner"}`, "path", "", false},
		// Non-string values are passed over; the string after them still lands.
		{"number then string", `{"timeout":30,"command":"ls"`, "command", "ls", true},
		{"bool then string", `{"force":true,"path":"a.go"`, "path", "a.go", true},
		{"null then string", `{"cwd":null,"path":"a.go"`, "path", "a.go", true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			var args partialArgs
			args.feed(probe.text)
			value, closed := args.value(probe.field)
			if closed != probe.closed {
				t.Fatalf("closed = %v, want %v (value %q)", closed, probe.closed, value)
			}
			if closed && value != probe.want {
				t.Fatalf("value = %q, want %q", value, probe.want)
			}
		})
	}
}

// The scan is INCREMENTAL: the provider re-sends the whole accumulated text
// each fragment, and re-scanning it every time would be quadratic in the length
// of a file the model is spelling out a token at a time. Fed a byte at a time,
// the answer is the same as fed whole.
func TestPartialArgsScansIncrementally(t *testing.T) {
	const whole = `{"path":"internal/session/loop.go","content":"package session\n\n// a \"quoted\" word"}`

	var byteAtATime partialArgs
	closes := 0
	for at := 1; at <= len(whole); at++ {
		if byteAtATime.feed(whole[:at]) {
			closes++
		}
	}
	var atOnce partialArgs
	atOnce.feed(whole)

	for _, field := range []string{"path", "content"} {
		incremental, ok := byteAtATime.value(field)
		complete, alsoOK := atOnce.value(field)
		if !ok || !alsoOK {
			t.Fatalf("%s: closed incrementally = %v, whole = %v", field, ok, alsoOK)
		}
		if incremental != complete {
			t.Fatalf("%s: incremental %q != whole %q", field, incremental, complete)
		}
	}
	if closes != 2 {
		t.Fatalf("%d field closes reported, want one per top-level string", closes)
	}
	if byteAtATime.consumed != len(whole) {
		t.Fatalf("consumed = %d, want the whole text scanned once", byteAtATime.consumed)
	}
}

// A captured value is bounded. The scanner does not know which field it will be
// asked about, so it captures every top-level string — and one of those is a
// whole file body. Past the cap the bytes are still SCANNED, so the closing
// quote is not missed and the field after it still lands.
func TestPartialArgsCapsWhatItKeeps(t *testing.T) {
	body := strings.Repeat("a", formingValueLimit*4)
	var args partialArgs
	args.feed(`{"content":"` + body + `","path":"after.go"`)

	content, closed := args.value("content")
	if !closed {
		t.Fatal("the long value never closed")
	}
	if len(content) != formingValueLimit {
		t.Fatalf("kept %d bytes of the body, want the cap %d", len(content), formingValueLimit)
	}
	if path, ok := args.value("path"); !ok || path != "after.go" {
		t.Fatalf("the field after the long one = %q (%v), want it intact", path, ok)
	}
}

// ── the ordering law, end to end ────────────────────────────────────────────

func emitForming(ctx context.Context, index int, id, tool, args string) {
	provider.EmitEvent(ctx, provider.StreamEvent{
		Kind: provider.StreamToolCallForming, Index: index, ID: id, Tool: tool, Delta: args,
	})
}

// FORMING → ANNOUNCED → BEGIN, per call. The phase that exists to fill the
// silence before the announcement is ordered before it, and forming never
// implies execution: the write below forms and is announced long before it runs.
func TestFormingPrecedesAnnouncedAndBegin(t *testing.T) {
	writeCall := ai.ToolCall{ID: "c-write", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"out.txt","content":"x"}`}}

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// The call arrives the way the wire sends it: identity, then the
			// path, then the body.
			emitForming(ctx, 0, "c-write", "write", "")
			emitForming(ctx, 0, "c-write", "write", `{"path":"out.txt",`)
			emitForming(ctx, 0, "c-write", "write", `{"path":"out.txt","content":"x`)
			emitReady(t, ctx, writeCall)
			return callsResponse(writeCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collected := collect(t, mustSubmit(t, agent, "write the file"))

	forming := indexOf(collected, EventToolForming, "write")
	announced := indexOf(collected, EventToolAnnounced, "write")
	began := indexOf(collected, EventToolBegin, "write")
	if forming < 0 {
		t.Fatalf("the write never formed: %v", kinds(collected))
	}
	if announced < 0 || began < 0 {
		t.Fatalf("announced = %d, began = %d: %v", announced, began, kinds(collected))
	}
	if !(forming < announced && announced < began) {
		t.Fatalf("order was forming %d, announced %d, began %d", forming, announced, began)
	}

	// Every forming event for this call is before the announcement, carries the
	// call's id, and carries NOTHING that has happened: no output, no result.
	for at, event := range collected {
		if event.Kind != EventToolForming {
			continue
		}
		if at > announced {
			t.Fatalf("a forming event at %d lands after the announcement at %d", at, announced)
		}
		if event.CallID != "c-write" {
			t.Fatalf("forming at %d carries CallID %q, want the provider's id", at, event.CallID)
		}
		if event.Output != "" || event.Err != nil {
			t.Fatalf("forming at %d claims a result: %+v", at, event)
		}
	}

	// The last forming before the announcement says what the call is, from the
	// path that closed while the body was still arriving.
	if collected[announced-1].Kind == EventToolForming && collected[announced-1].Hint != "write out.txt" {
		t.Fatalf("the last forming hint = %q, want the path gloss", collected[announced-1].Hint)
	}

	// AND THE ANNOUNCEMENT CARRIES THE SAME ID. It is what closes the pair: a
	// surface adopts the row it has been drawing since the first fragment, and
	// the id is the only thing that says which row that is.
	if collected[announced].CallID != "c-write" {
		t.Fatalf("the announcement carries CallID %q, want the forming events' id", collected[announced].CallID)
	}
}

// TWO WRITES IN ONE BATCH ARE TWO IDS, and each announcement carries its own.
// This is the case the id is FOR: both calls are `write`, they form
// interleaved, and a surface pairing announcements by tool name alone has
// nothing to tell the two rows apart with.
func TestParallelAnnouncementsCarryTheirOwnCallIDs(t *testing.T) {
	first := ai.ToolCall{ID: "c-1", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"a.txt","content":"a"}`}}
	second := ai.ToolCall{ID: "c-2", Type: "function",
		Function: ai.ToolCallFunction{Name: "write", Arguments: `{"path":"b.txt","content":"b"}`}}

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			// Interleaved, as a parallel batch arrives on the wire.
			emitForming(ctx, 0, "c-1", "write", "")
			emitForming(ctx, 1, "c-2", "write", "")
			emitForming(ctx, 0, "c-1", "write", `{"path":"a.txt",`)
			emitForming(ctx, 1, "c-2", "write", `{"path":"b.txt",`)
			// The SECOND call is announced first, which the ordering law allows
			// and which is exactly what an oldest-of-that-tool pairing gets
			// wrong.
			emitReady(t, ctx, second)
			emitReady(t, ctx, first)
			return callsResponse(first, second), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collected := collect(t, mustSubmit(t, agent, "write both files"))

	var announced []Event
	for _, event := range collected {
		if event.Kind == EventToolAnnounced {
			announced = append(announced, event)
		}
	}
	if len(announced) != 2 {
		t.Fatalf("want two announcements, got %d: %v", len(announced), kinds(collected))
	}
	// In the order they were announced, with the ids that were announced — not
	// the order the rows were drawn in.
	if announced[0].CallID != "c-2" || announced[1].CallID != "c-1" {
		t.Fatalf("announcements carry %q then %q, want c-2 then c-1",
			announced[0].CallID, announced[1].CallID)
	}
	if announced[0].Hint != "write b.txt" || announced[1].Hint != "write a.txt" {
		t.Fatalf("the ids and the glosses disagree: %q/%q",
			announced[0].Hint, announced[1].Hint)
	}
}

// A provider that never forms — every non-streaming endpoint — leaves the turn
// exactly what it was. The announcement and the begin are unchanged, and no
// surface that ignores the new kind sees anything new.
func TestATurnWithoutFormingIsUnchanged(t *testing.T) {
	readCall := ai.ToolCall{ID: "c-read", Type: "function",
		Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"note.txt"}`}}

	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			emitReady(t, ctx, readCall)
			return callsResponse(readCall), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	collected := collect(t, mustSubmit(t, agent, "read the note"))
	for _, event := range collected {
		if event.Kind == EventToolForming {
			t.Fatalf("a non-streaming turn formed: %+v", event)
		}
	}
	if indexOf(collected, EventToolAnnounced, "read") < 0 || indexOf(collected, EventToolBegin, "read") < 0 {
		t.Fatalf("the unchanged path lost its events: %v", kinds(collected))
	}
}
