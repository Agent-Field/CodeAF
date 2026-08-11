package chat

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// The execution trace, asserted against the SHAPE a real recorder writes and
// none of its content.
//
// The fixture below is the byte grammar internal/exec/trace.go's four Fprintf
// calls produce, and it is deliberately the same shape v1's own parser test
// drives (internal/tui/model_test.go) — the lens law asserted as a test: one
// source, two renderings, and if the writer's grammar ever moves, both surfaces
// fail on the same commit for the same reason.

const traceFixture = "contract in force: verify before finishing\n" +
	"── turn 1  finish=tool_calls  in=1372 out=45 cached=0 ──\n" +
	"text: I'll gather live data first.⏎Two searches, then a fetch.\n" +
	`call sh {"cmd":"ls -la clips/"}` + "\n" +
	"  → 1438B: total 3984⏎drwx------ 2 santosh santosh\n" +
	"── turn 2  finish=tool_calls  in=2007 out=58 cached=512  [nudge] ──\n" +
	`call web {"q":"ffmpeg concat mp4"}` + "\n" +
	"  → 902B ERROR: exa 503: upstream\n" +
	"steered: focus on scene 10 only\n"

// tracingCommander is a fakeCommander that also has a recorder to hand over —
// the same optional capability *command.Commander has, satisfied structurally.
type tracingCommander struct {
	fakeCommander
	traces map[string]string
	reads  int
}

func (c *tracingCommander) NodeTraceTail(
	nodeID string, _ int, sinceSize int64, sinceMod time.Time,
) (string, int64, time.Time, bool) {
	c.reads++
	text, ok := c.traces[nodeID]
	if !ok {
		return "", 0, time.Time{}, true
	}
	size, mod := int64(len(text)), time.Unix(1700000000, 0)
	if size == sinceSize && mod.Equal(sinceMod) && !sinceMod.IsZero() {
		return "", size, mod, false
	}
	return text, size, mod, true
}

// enterTracedRoom opens a room and folds BOTH of its reads back in, which is
// what the runtime does with the batch the enter key returns.
func enterTracedRoom(t *testing.T, app *App, digit string) {
	t.Helper()
	enterRoom(t, app, digit)
	cmd := app.readTraceCmd(app.view.node)
	if cmd == nil {
		t.Fatal("the room asked for no trace")
	}
	msg, ok := cmd().(traceReadMsg)
	if !ok {
		t.Fatalf("the trace read produced %T", cmd())
	}
	app.applyTraceRead(msg)
}

// -- the parse -----------------------------------------------------------------

func TestParsingARecordersFiveShapes(t *testing.T) {
	events := parseTrace(traceFixture)
	if len(events) != 7 {
		for i, e := range events {
			t.Logf("%d: kind=%d title=%q gist=%q", i, e.kind, e.title, e.gist)
		}
		t.Fatalf("the recorder parsed to %d rows, want 7", len(events))
	}
	want := []struct {
		kind  traceKind
		title string
	}{
		{traceNote, ""},
		{traceTurnRule, "turn 1"},
		{traceThought, "thinking"},
		{traceCall, "sh"},
		{traceTurnRule, "turn 2"},
		{traceCall, "web"},
		{traceSteer, "you"},
	}
	for i := range want {
		if events[i].kind != want[i].kind || events[i].title != want[i].title {
			t.Fatalf("row %d is kind %d %q, want kind %d %q",
				i, events[i].kind, events[i].title, want[i].kind, want[i].title)
		}
	}
	// A result is folded ONTO the call above it and is never a row of its own:
	// v1 drew three lines of output under every call, and a five-call turn filled
	// a screen with bytes nobody had asked to read.
	if !strings.Contains(events[3].detail, "total 3984") {
		t.Fatalf("the shell result did not fold onto its call: %q", events[3].detail)
	}
	if !events[5].failed {
		t.Fatal("the errored web call is not marked failed")
	}
	// A turn's token count reaches a cell; `finish=` and `in=` do not (5.14's
	// never-shown tier), and the recorder's own note does.
	if got := strings.Join(events[4].meta, " "); !strings.Contains(got, "58 tok") ||
		!strings.Contains(got, "nudge") {
		t.Fatalf("the turn's cells are %q", got)
	}
	if strings.Contains(strings.Join(events[4].meta, " "), "finish") {
		t.Fatal("a provider's finish reason reached a cell")
	}
}

// The salient argument is found by LOOKING, not by a per-tool table: a table is
// a list of the tools that existed on the day it was written.
func TestTheSalientArgumentIsFoundNotTabulated(t *testing.T) {
	for _, c := range []struct{ args, want string }{
		{`{"cmd":"ls -la"}`, "ls -la"},
		{`{"q":"bitcoin price"}`, "bitcoin price"},
		{`{"path":"notes/one.md","content":"hi"}`, "notes/one.md"},
		{`{"urls":["https://a.example","https://b.example"]}`, "https://a.example  https://b.example"},
		// A tool nobody has written yet, whose subject happens to be named the
		// way subjects are named.
		{`{"prompt":"a heron at dusk","seed":4}`, "a heron at dusk"},
	} {
		if got := salientArg(c.args); got != c.want {
			t.Fatalf("salientArg(%s) = %q, want %q", c.args, got, c.want)
		}
	}
	// And a call whose arguments say nothing recognisable still draws: the whole
	// object stands in, rather than the row going quiet.
	event := parseCall(`unknown {"alpha":1,"beta":2}`)
	if event.title != "unknown" || event.gist == "" {
		t.Fatalf("an unrecognised call drew %+v", event)
	}
}

// A recorder that grows a sixth shape tomorrow must not make the room go quiet.
func TestAnUnknownRecorderLineIsDrawnAndNotDropped(t *testing.T) {
	events := parseTrace("some shape this build has never seen\n")
	if len(events) != 1 || events[0].kind != traceNote {
		t.Fatalf("an unknown line parsed to %+v", events)
	}
	if !strings.Contains(events[0].gist, "never seen") {
		t.Fatalf("the unknown line lost its words: %q", events[0].gist)
	}
}

// -- the room ------------------------------------------------------------------

// THE REPORT: "the task page has no tool use or conversation or anything", and
// "we need to be very clear what the task is and what tools are used".
func TestATaskRoomDrawsTheToolsItsWorkerRan(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 34))
	// The tools, named, with what they were called with under them.
	for _, want := range []string{"sh", "ls -la clips/", "web", "ffmpeg concat mp4"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the room did not draw %q:\n%s", want, frame)
		}
	}
	// The worker's own words between the calls, and the reader's steer.
	if !strings.Contains(frame, "I'll gather live data first.") {
		t.Fatalf("the room did not draw the worker's own words:\n%s", frame)
	}
	if !strings.Contains(frame, "focus on scene 10 only") {
		t.Fatalf("the room did not draw the steer:\n%s", frame)
	}
	// AND THE RESULTS ARE NOT ON SCREEN. 4.3 asks for COLLAPSED tool-call rows;
	// a room that unfolded 1.4KB of directory listing would push the task's own
	// charge off the top, which is 13.10's finding one surface further in.
	if strings.Contains(frame, "drwx------") {
		t.Fatalf("a tool result was drawn unfolded:\n%s", frame)
	}
}

// 13.16's law, on the new rows: the fold is a door a pointer opens.
func TestClickingATraceRowOpensWhatTheToolReturned(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	const w, h = 120, 34
	frame := strings.Split(ansi.Strip(app.Frame(w, h)), "\n")
	y := -1
	for i, line := range frame {
		if strings.Contains(line, "ls -la clips/") {
			// The header is the row above the argument line.
			y = i - 1
		}
	}
	if y < 0 {
		t.Fatal("the shell call is not on screen")
	}
	if cmd := app.pane.Mouse(clickAt(2, y), image.Point{X: 2, Y: y}); cmd != nil {
		_ = cmd()
	}
	after := ansi.Strip(app.Frame(w, h))
	if !strings.Contains(after, "drwx------") {
		t.Fatalf("clicking the call did not open what it returned:\n%s", after)
	}
}

// A room over a worker that has journaled NOTHING but is fifteen seconds into a
// run still has something to say. H13 measured exactly that shape: an atomic job
// between `node_started` and `node_completed` journals money and no words.
func TestARoomWithNoJournalRowsStillDrawsItsTrace(t *testing.T) {
	backend := board()
	commander := &tracingCommander{traces: map[string]string{"job-1": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)

	record := app.source.workRecord("job-1")
	traces := map[string]nodeTrace{"job-1": {text: traceFixture}}
	if !hasTrace(record, traces) {
		t.Fatal("a recorder with four rows in it did not count as a record")
	}
	rows := roomBlocks(record, nil, app.style, app.source, traces)
	if len(rows) == 0 {
		t.Fatal("a room with a trace and no journal rows drew nothing")
	}
}

// The stamp is the whole reason this can run on quiet cycles: a recorder that
// has not grown is one open and one stat, and is not re-parsed.
func TestAnUnchangedRecorderIsNotReadTwice(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	msg, ok := app.readTraceCmd(app.view.node)().(traceReadMsg)
	if !ok {
		t.Fatal("the second pass produced no read")
	}
	if msg.moved {
		t.Fatalf("an unchanged recorder reported as moved: %+v", msg.traces)
	}
}

// A window whose engine cannot answer draws a room without execution rows and
// never fails. Trace is optional, exactly as Graph is.
func TestAnEngineWithNoRecorderDoorIsSilentAndNotBroken(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	enterRoom(t, app, "5")
	if cmd := app.readTraceCmd(app.view.node); cmd != nil {
		t.Fatal("a commander with no recorder door was asked for one")
	}
	if frame := ansi.Strip(app.Frame(120, 30)); !strings.Contains(frame, "XhrSyn") {
		t.Fatalf("the room lost its record when it had no trace:\n%s", frame)
	}
}
