package chat

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui2/blocks"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The execution trace, asserted against the SHAPE a real recorder writes and
// none of its content.
//
// The fixture below is the byte grammar internal/exec/trace.go's Fprintf calls
// produce, and it is deliberately the same shape v1's own parser test drives
// (internal/tui/model_test.go) — the lens law asserted as a test: one source,
// two renderings, and if the writer's grammar ever moves, both surfaces fail on
// the same commit for the same reason. The fixture is UNCHANGED by the restyle;
// what these tests assert about it is the dressing, and holding the input still
// is the whole point.

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

// executionRows is the record's trace region AS ROWS AT A WIDTH: the seam, the
// legend and every execution row the open room would draw, blank lines
// included.
//
// It renders the room's own blocks rather than the terminal frame, and the
// reason is the blank lines themselves — they are the grammar under test, and
// in a frame the rail is drawn beside the transcript, so a row that is empty in
// the document carries a tree branch at column 90 and is not empty on screen.
// The frame is asserted too, by the tests below; this is the document.
func executionRows(t *testing.T, app *App, width int) []string {
	t.Helper()
	room := roomBlocks(recordInputs{
		record:     app.source.workRecordAt(app.view.node),
		messages:   app.view.messages,
		style:      app.style,
		board:      app.source,
		traces:     app.traces,
		open:       app.foldOpen,
		treeOpen:   app.foldOpenDefault,
		money:      app.source.jobSpend(app.view.node),
		models:     app.source.jobModels(app.view.node),
		nodeModels: app.source.jobModels,
		now:        app.now(),
		clock:      app.view.transcript.Clock(),
	})
	start, end := -1, -1
	for i, block := range room {
		if block.ID() == traceSeamID {
			start = i
		}
		if isTraceBlock(block) {
			end = i
		}
	}
	if start < 0 || end < start {
		t.Fatalf("the record has no execution region: seam at %d, last row at %d", start, end)
	}
	var rows []string
	for _, block := range room[start : end+1] {
		rows = append(rows, block.Rows(width)...)
	}
	return rows
}

// -- the parse -----------------------------------------------------------------

// The recorder's five shapes become FOUR VOICES and a boundary. The `── turn ──`
// rule is read and spent: it arms the blank line and never becomes a row, which
// is §15 ("blank lines are boundaries") and §14 ("turn" is machinery vocabulary)
// in one assertion.
func TestARecordersRoundRuleBecomesABoundaryAndNotARow(t *testing.T) {
	events := parseTrace(traceFixture)
	want := []struct {
		kind   traceKind
		gist   string
		breaks bool
	}{
		{traceNote, "contract in force: verify before finishing", false},
		{traceThought, "I'll gather live data first.", true},
		{traceCall, "ls -la clips/", false},
		{traceNote, "nudge", true},
		{traceCall, "ffmpeg concat mp4", false},
		{traceSteer, "focus on scene 10 only", false},
	}
	if len(events) != len(want) {
		for i, e := range events {
			t.Logf("%d: kind=%d gist=%q breaks=%v", i, e.kind, e.gist, e.breaks)
		}
		t.Fatalf("the recorder parsed to %d rows, want %d", len(events), len(want))
	}
	for i := range want {
		got := events[i]
		if got.kind != want[i].kind || got.gist != want[i].gist || got.breaks != want[i].breaks {
			t.Fatalf("row %d is kind %d %q (breaks=%v), want kind %d %q (breaks=%v)",
				i, got.kind, got.gist, got.breaks,
				want[i].kind, want[i].gist, want[i].breaks)
		}
	}
	// The first row of a document never opens a boundary: a blank line above the
	// first thing said would be a hole where the record starts.
	if events[0].breaks {
		t.Fatal("the document opened on a boundary")
	}
	// A result is folded ONTO the call above it, and it lands in `output`: what
	// went IN is on the collapsed row, so the fold holds only what came back.
	if !strings.Contains(events[2].output, "total 3984") {
		t.Fatalf("the shell result did not fold onto its call: %q", events[2].output)
	}
	if events[2].detail != "" {
		t.Fatalf("a call's fold held something other than its output: %q", events[2].detail)
	}
	if !events[2].returned || events[2].failed || events[2].bytes != 1438 {
		t.Fatalf("the shell call reads as %+v", events[2])
	}
	if !events[4].failed || events[4].bytes != 902 || events[4].reason != "exa 503: upstream" {
		t.Fatalf("the errored web call reads as %+v", events[4])
	}
}

// NO ROW EVER SHOWS JSON. The row says what the call was FOR, through one small
// table of FAMILIES, and a call whose subject cannot be found says the tool's
// own name — never the object, and never the transport.
func TestACallIsDrawnAsItsInputAndNeverAsItsArguments(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{`sh {"cmd":"ls -la clips/"}`, "ls -la clips/"},
		{`web {"q":"ffmpeg concat mp4"}`, "ffmpeg concat mp4"},
		{`write {"path":"notes/one.md","content":"hi"}`, "notes/one.md"},
		// A place, not an address: the scheme is the same six characters on
		// every row and the query string is a tracking parameter more often
		// than it is information.
		{`fetch {"url":"https://docs.rs/tokio/latest/index.html?utm=x"}`, "docs.rs/tokio/latest/index.html"},
		// A tool nobody has written yet, whose subject is named the way
		// subjects are named.
		{`imagegen {"prompt":"a heron at dusk","seed":4}`, "a heron at dusk"},
	} {
		if got := parseCall(c.line).gist; got != c.want {
			t.Fatalf("%s drew %q, want %q", c.line, got, c.want)
		}
	}
	// And the one that says nothing recognisable says NOTHING rather than
	// `{"alpha":1}`. The row falls back to the tool's own name (traceBlock).
	blank := parseCall(`control {"alpha":1,"beta":2}`)
	if blank.gist != "" || blank.tool != "control" {
		t.Fatalf("an unreadable call drew %+v", blank)
	}
	for _, event := range parseTrace(traceFixture) {
		if strings.Contains(event.gist, "{") || strings.Contains(event.gist, `":`) {
			t.Fatalf("a row drew the argument object: %q", event.gist)
		}
	}
}

// SUCCESS IS SILENT AND ONLY FAILURE MARKS. A ✓ on every row of a nine-call run
// is noise that buries the one row that is not fine, and the reporter's own
// screenshot is what that looks like.
func TestOnlyAFailedCallWearsAMark(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	events := parseTrace(traceFixture)
	fine := traceBlock("a", events[2], events[2].output, true, 0, style)
	row := fine.Rows(90)[0]
	if strings.Contains(row, tokens.GlyphSettled) {
		t.Fatalf("a call that simply worked wore a tick: %q", row)
	}
	broke := traceBlock("b", events[4], events[4].output, true, 0, style)
	row = broke.Rows(90)[0]
	if !strings.Contains(row, tokens.GlyphFailed) || !strings.Contains(row, "exa 503") {
		t.Fatalf("the failed call did not say so with its reason: %q", row)
	}
	if broke.head.GlyphHue != blocks.HueBroken {
		t.Fatalf("the failed call's glyph is not coral: %+v", broke.head)
	}
}

// No row carries a token count any more, and no row carries a number that came
// off the round rule. §5 is explicit; the parse is where it has to be true,
// because a field that survived here would find a cell eventually.
func TestNoRowKeepsTheRoundsTelemetry(t *testing.T) {
	for _, event := range parseTrace(traceFixture) {
		for _, cell := range []string{event.gist, event.detail, event.tool, event.reason} {
			for _, banned := range []string{"tok", "finish=", "in=", "out=", "cached="} {
				if strings.Contains(cell, banned) {
					t.Fatalf("%q survived onto a row as %q", banned, cell)
				}
			}
		}
	}
	// And a call still in flight claims no ending: the ✓ is a fact about a
	// result that has arrived, not a decoration on a row that has one coming.
	live := parseTrace(`call sh {"cmd":"sleep 30"}` + "\n")
	if len(live) != 1 || live[0].returned || live[0].failed {
		t.Fatalf("a call with no result yet reads as %+v", live)
	}
}

// The generic argument names still answer for a tool no family claims, which is
// what keeps a tool nobody has written yet from drawing nothing at all.
func TestTheSalientArgumentIsFoundNotTabulated(t *testing.T) {
	for _, c := range []struct{ args, key, want string }{
		{`{"cmd":"ls -la"}`, "cmd", "ls -la"},
		{`{"q":"bitcoin price"}`, "q", "bitcoin price"},
		{`{"path":"notes/one.md","content":"hi"}`, "path", "notes/one.md"},
		{`{"urls":["https://a.example","https://b.example"]}`, "urls", "https://a.example  https://b.example"},
		{`{"prompt":"a heron at dusk","seed":4}`, "prompt", "a heron at dusk"},
	} {
		key, got := salientArg(c.args)
		if key != c.key || got != c.want {
			t.Fatalf("salientArg(%s) = %q, %q; want %q, %q", c.args, key, got, c.key, c.want)
		}
	}
}

// THE FAMILY IS READ OFF THE WORDS IN THE TOOL'S NAME, matched as whole tokens
// — never as substrings, or `publish` would be a shell. It is one small table,
// extensible by one row, and it decides the glyph, the argument names and the
// verb a batch of these calls says.
func TestAToolsFamilyIsReadFromTheWordsInItsName(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	for _, c := range []struct {
		line  string
		glyph string
		named bool
		verb  string
	}{
		{`sh {"cmd":"ls"}`, tokens.GlyphShell, true, "ran"},
		{`terminal-9000 {"cmd":"ls"}`, tokens.GlyphShell, true, "ran"},
		{`web_search {"q":"heron"}`, tokens.GlyphSearch, true, "searched"},
		{`mcp.web.fetch {"url":"https://a.example"}`, tokens.GlyphSearch, true, "fetched"},
		{`write {"path":"a.md"}`, tokens.GlyphWrite, true, "wrote"},
		{`imagegen {"prompt":"a heron"}`, tokens.GlyphCollapsed, false, "called"},
		// The substring trap: `publish` contains "sh" and is not a shell.
		{`publish {"prompt":"a heron"}`, tokens.GlyphCollapsed, false, "called"},
	} {
		event := parseCall(c.line)
		glyph, named := callGlyph(event, style)
		if glyph != c.glyph || named != c.named || event.family.verb != c.verb {
			t.Fatalf("%s drew %q (named=%v, verb=%q), want %q (named=%v, verb=%q)",
				c.line, glyph, named, event.family.verb, c.glyph, c.named, c.verb)
		}
	}
}

// The recorder truncates an argument object at 300 characters, so the calls a
// reader most wants named — a heredoc, a written file — arrive as INVALID JSON
// with the closing brace cut off. Measured on the reporter's own recorder before
// the scan existed: those rows drew raw `{"cmd":"cd … << 'EOF'\n\nprint(\"…`.
func TestATruncatedArgumentObjectStillNamesItsSubject(t *testing.T) {
	cut := `{"cmd":"cd /home/x/work && cat >> validate.py << 'EOF'⏎⏎print(\"win rate\")⏎EO`
	if key, got := salientArg(cut); key != "cmd" ||
		!strings.HasPrefix(got, "cd /home/x/work && cat >> validate.py") {
		t.Fatalf("a cut heredoc named %q under %q", got, key)
	}
	// The key the row wants may sit before the one that was cut.
	if key, got := salientArg(`{"path":"/home/x/work/report.md","text":"# Why the signal fails,⏎⏎1. Diagnos`); //
	key != "path" || got != "/home/x/work/report.md" {
		t.Fatalf("a cut write named %q under %q", got, key)
	}
	// And the row that comes out of it shows the first line, not the transport.
	event := parseCall("sh " + cut)
	if strings.Contains(event.gist, `{"cmd"`) || strings.Contains(event.gist, "⏎") {
		t.Fatalf("the row drew the recorder's own encoding: %q", event.gist)
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
//
// The frame, at a width, row by row: the seam, the legend once, and then the
// four voices as adjacent lines with the blank line ONLY where a round ended.
func TestTheExecutionRecordDrawsFourVoicesAndOneBoundaryPerRound(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	rows := executionRows(t, app, 90)
	// The whole region, row for row. The rhythm is CLUSTERS: a narration row,
	// the tool rows it set off TIGHT beneath it, then air before the next voice
	// speaks. A blank line therefore lands at four places — after the opening
	// note, between the two rounds, before the steer that follows a tool row,
	// and under the last row — and nowhere between a sentence and the machinery
	// it explains.
	want := []struct {
		lead string
		says string
	}{
		{blocks.RuleMark + " " + traceSeamWord, ""},
		{tokens.GlyphThought + " model", tokens.GlyphCollapsed + " expands"},
		{"", ""},
		{"contract in force", ""},
		{"", ""},
		{tokens.GlyphThought, "I'll gather live data first."},
		{tokens.GlyphShell, "ls -la clips/"},
		{"", ""},
		{"nudge", ""},
		{tokens.GlyphSearch, "ffmpeg concat mp4"},
		{"", ""},
		{tokens.GlyphPromptChat, "focus on scene 10 only"},
		{"", ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("the execution record drew %d rows, want %d:\n%s",
			len(rows), len(want), strings.Join(rows, "\n"))
	}
	for i := range want {
		row := strings.TrimLeft(rows[i], " ")
		if !strings.HasPrefix(row, want[i].lead) {
			t.Fatalf("row %d is %q, want it to open on %q\n%s",
				i, rows[i], want[i].lead, strings.Join(rows, "\n"))
		}
		if want[i].says != "" && !strings.Contains(rows[i], want[i].says) {
			t.Fatalf("row %d is %q, want it to say %q", i, rows[i], want[i].says)
		}
	}
	// The right side of a call row: how much came back, and — only when
	// something went wrong — the mark and the reason.
	if strings.Contains(rows[6], tokens.GlyphSettled) || !strings.Contains(rows[6], "~359 tok") {
		t.Fatalf("the shell call's receipt is wrong: %q", rows[6])
	}
	if !strings.Contains(rows[9], tokens.GlyphFailed) ||
		!strings.Contains(rows[9], "exa 503") || !strings.Contains(rows[9], "~225 tok") {
		t.Fatalf("the failed web call lost its receipt: %q", rows[9])
	}
	// AND THE RESULTS ARE NOT ON SCREEN. §5 asks for collapsed rows; a room that
	// unfolded 1.4KB of directory listing would push the job's own charge off
	// the top, which is 13.10's finding one surface further in.
	if strings.Contains(strings.Join(rows, "\n"), "drwx------") {
		t.Fatalf("a tool result was drawn unfolded:\n%s", strings.Join(rows, "\n"))
	}
}

// §14 and §15, asserted as an absence: the record names no machinery and labels
// no structure. Every word that used to do the spacing's job is gone.
func TestTheExecutionRecordSpeaksNoMachinery(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	region := strings.ToLower(strings.Join(executionRows(t, app, 90), "\n"))
	// A row's own `~Nk tok` estimate is NOT on this list, and the distinction is
	// the point of the ban: what §5 refuses is a ROUND's machinery telemetry —
	// `in=`, `out=`, `cached=`, the numbers that described a turn nobody can see
	// — while what a call cost the window to read is a fact about the work, in
	// the unit a person judges it in, marked as the estimate it is.
	for _, banned := range []string{"turn ", "thinking", "seq", "journal", "cached=", "finish="} {
		if strings.Contains(region, banned) {
			t.Fatalf("the record said %q:\n%s", banned, region)
		}
	}
	// The tool's own name is not on the collapsed row of a call whose kind the
	// glyph already says. It is one keystroke away, in the fold, and this is the
	// §15 test made mechanical: delete the label, and the row still reads.
	for _, row := range strings.Split(region, "\n") {
		if strings.Contains(row, "ls -la clips/") && strings.Contains(row, "sh ") {
			t.Fatalf("the shell row wore its tool's name as well as its glyph: %q", row)
		}
	}
}

// The legend is taught ONCE, inside the scroll, at the top of the execution
// record — never per row and never as a pinned hint.
func TestTheLegendIsTaughtOnceInsideTheScroll(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{
		"job-3":     traceFixture,
		"job-3/xhr": traceFixture,
	}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	frame := ansi.Strip(app.Frame(120, 60))
	legend := traceLegend(app.style)
	if got := strings.Count(frame, legend); got != 1 {
		t.Fatalf("the legend was drawn %d times, want once:\n%s", got, frame)
	}
	if got := strings.Count(frame, blocks.RuleMark+" "+traceSeamWord); got != 1 {
		t.Fatalf("the record drew %d execution seams, want one:\n%s", got, frame)
	}
	// It teaches the marks the rows actually wear, and the door.
	for _, want := range []string{
		tokens.GlyphThought + " model", tokens.GlyphShell + " shell",
		tokens.GlyphSearch + " web", tokens.GlyphPromptChat + " you",
		tokens.GlyphCollapsed + " expands",
	} {
		if !strings.Contains(legend, want) {
			t.Fatalf("the legend does not teach %q: %q", want, legend)
		}
	}
}

// Two recorders spliced into one record keep append order, and the splice reads
// as one more boundary — which is 13.17's numbering finding made moot rather
// than answered: there is no number left on screen to restart.
func TestASpliceBoundaryIsJustAnotherBlankLine(t *testing.T) {
	first := parseTrace(traceFixture)
	blocksOf := traceBlocks("job-3", first, nil, nil, traceLive{})
	if len(blocksOf) != len(first) {
		t.Fatalf("%d rows produced %d blocks", len(first), len(blocksOf))
	}
	// The last row of a recorder is never tight, so whatever is spliced in
	// behind it — the next part's rows, a journaled message — starts after one
	// blank line and not against it.
	last, ok := blocksOf[len(blocksOf)-1].(*messageBlock)
	if !ok || last.tight {
		t.Fatalf("the last row of a recorder closed tight: %+v", blocksOf[len(blocksOf)-1])
	}
	// And inside a round the rows are adjacent: exactly one block per boundary
	// closes on a blank.
	loose := 0
	for i, block := range blocksOf {
		message, isMessage := block.(*messageBlock)
		if !isMessage {
			t.Fatalf("row %d is a %T", i, block)
		}
		if !message.tight {
			loose++
		}
	}
	// Two round boundaries, the cluster boundary before the steer that follows
	// a tool row, and the tail.
	if want := 4; loose != want {
		t.Fatalf("%d rows closed on a blank line, want %d", loose, want)
	}
}

// traceBatchFixture is the shape the reporter photographed: a run of searches
// long enough to bury the two sentences of narration around it.
const traceBatchFixture = "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
	"text: I need the landscape first.\n" +
	`call web {"q":"rust async trait"}` + "\n" +
	"  → 4096B: one\n" +
	`call web {"q":"tokio spawn cost"}` + "\n" +
	"  → 2048B: two\n" +
	`call web {"q":"pin project macro"}` + "\n" +
	"  → 1024B: three\n" +
	`call web {"q":"async drop rfc"}` + "\n" +
	"  → 1024B: four\n" +
	"── turn 2  finish=stop  in=10 out=10 ──\n" +
	"text: I now have rich material.\n"

// A RUN OF THE SAME TOOL IS ONE ROW, and the sentences around it are what is
// left standing. This is the fix for the screenshot where nine searches drowned
// the model's narration.
func TestARunOfTheSameToolCollapsesToOneRow(t *testing.T) {
	events := parseTrace(traceBatchFixture)
	rows := traceBlocks("job-1", events, tokens.NewStyler(tokens.NoColor, tokens.FocusNormal), nil, traceLive{})
	if len(rows) != 3 {
		for i, r := range rows {
			t.Logf("%d: %q", i, r.Rows(90))
		}
		t.Fatalf("six events drew %d rows, want 3 (a thought, the batch, a thought)", len(rows))
	}
	batch := rows[1].(*messageBlock).Rows(90)[0]
	for _, want := range []string{
		tokens.GlyphSearch, "searched 4",
		"rust async trait", "tokio spawn cost", "pin project macro", "+1", "~2K tok",
	} {
		if !strings.Contains(batch, want) {
			t.Fatalf("the batch row is %q, want it to carry %q", batch, want)
		}
	}
	// The fourth query is COUNTED and not shown: a batch is a glance.
	if strings.Contains(batch, "async drop rfc") {
		t.Fatalf("the batch drew every input: %q", batch)
	}
	// And the rhythm still belongs to the ROUNDS and not to the rows: the
	// thought and the run it opened are one round and sit adjacent, and the
	// blank line lands after the batch, where the recorder's second rule was.
	if !rows[0].(*messageBlock).tight {
		t.Fatal("the thought and the run it opened were separated")
	}
	if rows[1].(*messageBlock).tight {
		t.Fatal("the batch did not close the round it ended")
	}
}

// Opening a batch LAYS OUT THE CALLS, indented, each its own door onto what it
// returned. The fold state is what decides which rows exist, so opening one is a
// rebuild of the record and not a re-render of one row.
func TestOpeningABatchLaysOutTheCallsItCollapsed(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	events := parseTrace(traceBatchFixture)
	batchID := traceBlockID("job-1", 1)
	rows := traceBlocks("job-1", events, style, func(id string) bool { return id == batchID }, traceLive{})
	if len(rows) != 7 {
		t.Fatalf("an open batch drew %d rows, want 7 (two thoughts, the batch, four calls)", len(rows))
	}
	for i, want := range []string{"tokio spawn cost", "pin project macro", "async drop rfc"} {
		row := rows[3+i].(*messageBlock)
		if row.indent != bodyIndent {
			t.Fatalf("the call inside the batch sits at column %d, want %d", row.indent, bodyIndent)
		}
		if !strings.Contains(row.Rows(90)[0], want) {
			t.Fatalf("row %d of the open batch is %q, want %q", i, row.Rows(90)[0], want)
		}
	}
	// And every one of them still opens onto its own result.
	if !rows[2].(*messageBlock).collapsible {
		t.Fatal("a call inside an open batch lost its own door")
	}
}

// AN OPENED RESULT IS A BOUNDED BOX. A record that dumped four thousand lines
// into itself because somebody tapped a row would make "expand" a gesture nobody
// dares make; the box shows [traceBoxRows] and the rest is behind one more door,
// whole when it is opened.
func TestAnOpenedResultIsABoundedBoxWithTheRestBehindOneMoreDoor(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	var long strings.Builder
	long.WriteString(`call sh {"cmd":"ls -R"}` + "\n  → 900B: ")
	for i := range 30 {
		if i > 0 {
			long.WriteString("⏎")
		}
		long.WriteString("file-" + strconv.Itoa(i) + ".txt")
	}
	events := parseTrace(long.String() + "\n")
	rowID := traceBlockID("job-1", 0)

	shut := traceBlocks("job-1", events, style, nil, traceLive{})
	if len(shut) != 1 {
		t.Fatalf("a shut row drew %d blocks", len(shut))
	}
	open := traceBlocks("job-1", events, style, func(id string) bool { return id == rowID }, traceLive{})
	if len(open) != 2 {
		t.Fatalf("an opened long result drew %d blocks, want the row and its continuation", len(open))
	}
	box := open[0].(*messageBlock).Rows(90)
	gutter := 0
	for _, row := range box {
		if strings.Contains(row, tokens.GlyphProseQuote) {
			gutter++
		}
	}
	if gutter != traceBoxRows {
		t.Fatalf("the box drew %d quoted rows, want %d", gutter, traceBoxRows)
	}
	more := open[1].(*messageBlock)
	// The continuation is the gutter continuing and the count of what is behind
	// it, and nothing else: a title saying "18 more lines" beside a hint saying
	// the same would be the label spelling what the row already says (§15).
	row := more.Rows(90)[0]
	if !strings.Contains(row, tokens.GlyphProseQuote) ||
		!strings.Contains(row, blocks.Disclose(false, 18, "line", "lines")) {
		t.Fatalf("the continuation is %q", row)
	}
	if !more.collapsible || more.indent != bodyIndent {
		t.Fatalf("the continuation is not a door at the box's own column: %+v", more)
	}
	// Opened, it holds the rest WHOLE — the reader asked for it.
	all := traceBlocks("job-1", events, style, func(id string) bool { return true }, traceLive{})
	rest := all[1].(*messageBlock).Rows(90)
	if !strings.Contains(strings.Join(rest, "\n"), "file-29.txt") {
		t.Fatalf("the continuation did not hold the tail:\n%s", strings.Join(rest, "\n"))
	}
}

// 13.16's law and §10's, on the new rows: the whole line is the door, and what
// a tool returned comes back behind the `│` gutter rather than as more prose.
func TestClickingATraceRowOpensWhatTheToolReturned(t *testing.T) {
	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": traceFixture}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	const w, h = 120, 40
	frame := strings.Split(ansi.Strip(app.Frame(w, h)), "\n")
	y := -1
	for i, line := range frame {
		if strings.Contains(line, "ls -la clips/") {
			y = i
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
	for _, line := range strings.Split(after, "\n") {
		if !strings.Contains(line, "drwx------") {
			continue
		}
		if !strings.Contains(line, tokens.GlyphProseQuote) {
			t.Fatalf("the output came back without its gutter: %q", line)
		}
	}
	// And the fold holds ONLY what came back. The row already says what went
	// in, so a fold that also held `sh {"cmd":"ls -la clips/"}` would be the
	// third spelling of one fact — which is what the reporter photographed.
	if strings.Contains(after, `{"cmd"`) {
		t.Fatalf("the fold drew the arguments the row already named:\n%s", after)
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
		t.Fatal("a recorder with rows in it did not count as a record")
	}
	rows := roomBlocks(recordInputs{
		record: record, style: app.style, board: app.source, traces: traces,
	})
	if len(rows) == 0 {
		t.Fatal("a room with a trace and no journal rows drew nothing")
	}
	// The seam is drawn ONCE and it is drawn where the execution begins, not at
	// a fixed index: it is the record's only hairline (§4).
	seams := 0
	for _, block := range rows {
		if block.ID() == traceSeamID {
			seams++
		}
	}
	if seams != 1 {
		t.Fatalf("the record drew %d seams, want one", seams)
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
// never fails. Trace is optional, exactly as Graph is — and with no rows there
// is no seam and no legend either, because a section header over nothing is the
// surface announcing an absence.
func TestAnEngineWithNoRecorderDoorIsSilentAndNotBroken(t *testing.T) {
	backend := recordBoard()
	app := newTestApp(backend, &fakeCommander{}, nil)
	poll(t, app)
	enterRoom(t, app, "5")
	if cmd := app.readTraceCmd(app.view.node); cmd != nil {
		t.Fatal("a commander with no recorder door was asked for one")
	}
	frame := ansi.Strip(app.Frame(120, 30))
	if !strings.Contains(frame, "XhrSyn") {
		t.Fatalf("the room lost its record when it had no trace:\n%s", frame)
	}
	if strings.Contains(frame, blocks.RuleMark+" "+traceSeamWord) {
		t.Fatalf("a record with no execution rows drew the seam anyway:\n%s", frame)
	}
}

// -- the receipt column and the read seam --------------------------------------

// THE FILED SYMPTOM, at the width it was filed at: a batch row whose named
// inputs ran to the edge of an 88-column terminal pushed its size cell off the
// row entirely, while the shorter row under it kept one — so the column §16
// calls a column had a hole in it, and an absent size was indistinguishable
// from a dropped one.
//
// The size now rides [blocks.Header.Receipt]: reserved first, right-aligned,
// dropped whole or not at all. What pays for a long title is the title.
func TestABatchRowKeepsItsSizeAtTheEdgeWhenTheTitleIsLong(t *testing.T) {
	const width = 88
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	long := "── turn 1  finish=tool_calls  in=10 out=10 ──\n"
	for _, query := range []string{
		"rust async trait object lifetime elision rules",
		"tokio spawn cost per task on a busy runtime",
		"pin project macro versus manual projection",
		"async drop rfc status",
	} {
		long += `call web {"q":"` + query + `"}` + "\n  → 4096B: fine\n"
	}
	rows := traceBlocks("job-1", parseTrace(long), style, nil, traceLive{})
	if len(rows) != 1 {
		t.Fatalf("four searches drew %d rows, want one batch", len(rows))
	}
	row := ansi.Strip(rows[0].Rows(width)[0])
	if blocks.Width(row) > width {
		t.Fatalf("the batch row is %d cells at %d: %q", blocks.Width(row), width, row)
	}
	if !strings.Contains(row, "~4.1K tok") {
		t.Fatalf("the long batch lost its size cell at %d columns: %q", width, row)
	}
	// Right-aligned means right-aligned: the row reaches the edge and ends on
	// the column, with the title cut instead of the receipt. The DOOR is not in
	// the column — it sits inline, right after the words it opens.
	if !strings.HasSuffix(row, "~4.1K tok") {
		t.Fatalf("the batch row does not end on its receipt column: %q", row)
	}
	// (The door is the FIRST thing width pressure takes after the meta cells,
	// which is the header's own law and unchanged here; where it survives it
	// sits inline — see TestTheFoldHintSitsInlineAndNotInTheReceiptColumn.)
	if blocks.Width(row) != width {
		t.Fatalf("the receipt column floats: row is %d of %d cells: %q",
			blocks.Width(row), width, row)
	}
}

// The SAME PLACEMENT on a plain tool row, at the same width, so the two read as
// one table down the record rather than as two rows that happen to carry sizes.
//
// §20 made the column CONDITIONAL — it is a column only while it is close to the
// subject, and past [blocks.ReceiptGulfMax] cells the figure rides inline in
// parentheses instead. What this test now pins is the property that actually
// matters: whichever placement the width chooses, EVERY sized row on the surface
// chooses the same one, because a list in which one row's estimate is at the
// right edge and its neighbour's is inline is the ragged scan the receipt column
// existed to prevent.
func TestAToolRowSharesTheBatchesReceiptColumn(t *testing.T) {
	const width = 88
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	rows := traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{})
	var sized []string
	for _, block := range rows {
		row := ansi.Strip(block.Rows(width)[0])
		if strings.Contains(row, " tok") {
			sized = append(sized, row)
		}
	}
	if len(sized) != 2 {
		t.Fatalf("the fixture drew %d rows with a size, want 2: %q", len(sized), sized)
	}
	inline := strings.HasSuffix(sized[0], ")")
	for _, row := range sized {
		if strings.HasSuffix(row, ")") != inline {
			t.Fatalf("the sized rows disagree about where the estimate goes:\n%q", sized)
		}
		if inline {
			// Placement 1: attached to the row, so the row is as long as it is.
			if !strings.Contains(row, " (~") {
				t.Fatalf("an inline estimate is malformed: %q", row)
			}
			continue
		}
		if blocks.Width(row) != width {
			t.Fatalf("a sized row does not reach the edge: %d of %d cells: %q",
				blocks.Width(row), width, row)
		}
	}
}

// SANITIZE AT THE READ SEAM. A recorder file is model- and tool-authored text
// that arrives from DISK rather than from the store, which is exactly how it
// slipped past the chokepoint every other such string passes: a tool that
// printed an OSC 52 clipboard write, an OSC 8 hyperlink or a cursor query would
// have had it replayed into the terminal by a room that only meant to quote it.
func TestTheRecorderIsSanitizedOnTheWayIn(t *testing.T) {
	hostile := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		"text: here is the output\n" +
		`call sh {"cmd":"cat banner.txt"}` + "\n" +
		"  → 64B: \x1b]52;c;aGVsbG8=\x07stolen\x1b]8;;https://evil.test\x07link\x1b]8;;\x07" +
		"\x1b[2J\x07\x00 tail\n"

	backend := recordBoard()
	commander := &tracingCommander{traces: map[string]string{"job-3": hostile}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	held, ok := app.traces["job-3"]
	if !ok {
		t.Fatal("the room read no recorder")
	}
	for name, bytes := range map[string]string{
		"OSC 52 clipboard": "\x1b]52",
		"OSC 8 hyperlink":  "\x1b]8;",
		"erase display":    "\x1b[2J",
		"BEL":              "\x07",
		"NUL":              "\x00",
	} {
		if strings.Contains(held.text, bytes) {
			t.Fatalf("%s survived the read seam: %q", name, held.text)
		}
	}
	// It SANITIZES and does not censor: the words a tool printed are still the
	// record, and the parse still reads the recorder's own grammar around them.
	for _, want := range []string{"stolen", "link", "tail", "cat banner.txt"} {
		if !strings.Contains(held.text, want) {
			t.Fatalf("the read seam ate the record's own words (%q): %q", want, held.text)
		}
	}
	if events := parseTrace(held.text); len(events) != 2 {
		t.Fatalf("the sanitized tail parsed to %d events, want 2", len(events))
	}
	// And nothing executable reaches a drawn row either — the chokepoint is one
	// call at the read, so no renderer under it has to remember.
	for _, row := range executionRows(t, app, 88) {
		if strings.Contains(row, "\x1b]") || strings.ContainsAny(row, "\x00\x07") {
			t.Fatalf("a drawn row carried an executable sequence: %q", row)
		}
	}
}

// -- the tiers and the running row ---------------------------------------------

// tierOf reports which grey tier a span inside a painted row was drawn at, or
// -1. It matches the SPAN the token layer would have written, which is why it
// can tell a pre-painted secondary title from a primary one the header painted
// itself: the inner span is present verbatim either way.
func tierOf(style *tokens.Styler, row, span string) tokens.Token {
	for _, tier := range []tokens.Token{tokens.TextPrimary, tokens.TextSecondary, tokens.TextTertiary} {
		if strings.Contains(row, style.PaintToken(span, tier)) {
			return tier
		}
	}
	return tokens.Token(255)
}

// THE INVERSION §5 ASKS FOR: "the model's narration must read as the loudest
// voice in the record; tools are the quiet machinery under it."
//
// It was upside down. A thought was chrome — the dimmest tier in the product —
// on the reasoning that the model was talking to itself, while the tool rows
// beside it were primary; so nine machine rows out-shouted the two sentences a
// person opens a record to read. Three tiers now, in the order the reader needs
// them: narration bright, tool input one down, receipts dim.
func TestTheNarrationIsTheLoudestVoiceAndToolsSitUnderIt(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	rows := traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{})
	find := func(span string) string {
		t.Helper()
		for _, block := range rows {
			row := block.Rows(90)[0]
			if strings.Contains(ansi.Strip(row), span) {
				return row
			}
		}
		t.Fatalf("no execution row says %q", span)
		return ""
	}
	narration := find("I'll gather live data first.")
	if tier := tierOf(style, narration, "I'll gather live data first."); tier != tokens.TextPrimary {
		t.Fatalf("the narration is not the loudest row: %q", narration)
	}
	// Its own glyph stays dim: the mark identifies the voice, it does not
	// compete with the sentence for the eye.
	if !strings.Contains(narration, style.PaintToken(style.Glyph(tokens.GThought), tokens.TextTertiary)) {
		t.Fatalf("the thought glyph is as loud as the thought: %q", narration)
	}
	shell := find("ls -la clips/")
	if tier := tierOf(style, shell, "ls -la clips/"); tier != tokens.TextSecondary {
		t.Fatalf("the tool row's input is not one tier under the narration: %q", shell)
	}
	// And the receipt is dim, which is the third and last tier on the surface
	// (§16: a fourth tier on one screen is a bug).
	if tierOf(style, shell, "~359 tok") != tokens.TextTertiary {
		t.Fatalf("the tool row's receipt is not dim: %q", shell)
	}
	// The reader's own steer stays bright — their words must be findable in a
	// document they did not write.
	steer := find("focus on scene 10 only")
	if tier := tierOf(style, steer, "focus on scene 10 only"); tier != tokens.TextPrimary {
		t.Fatalf("the reader's steer lost its ink: %q", steer)
	}
}

// A BATCH IS MACHINERY however many calls it stands for, so it reads at the
// tool tier and not at the narration's.
func TestABatchReadsAtTheToolTier(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	rows := traceBlocks("job-1", parseTrace(traceBatchFixture), style, nil, traceLive{})
	batch := rows[1].Rows(90)[0]
	if tier := tierOf(style, batch, "searched 4"); tier == tokens.TextPrimary {
		t.Fatalf("the batch row is as loud as the narration: %q", batch)
	}
}

// THE RHYTHM IS CLUSTERS: narration, its tools TIGHT beneath it, air, next
// narration. The recorder's `── turn N ──` rule is not enough on its own —
// one round routinely alternates — so a voice that SPEAKS after machinery ran
// opens a new cluster and takes the blank line with it.
func TestANarrationAfterToolsOpensANewCluster(t *testing.T) {
	// One round, two narration/tool clusters inside it: the boundary the
	// recorder never wrote.
	fixture := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		"text: first I look around.\n" +
		`call sh {"cmd":"ls"}` + "\n" +
		"  → 10B: a\n" +
		`call sh {"cmd":"pwd"}` + "\n" +
		"  → 10B: b\n" +
		"text: now I know where I am.\n" +
		`call sh {"cmd":"cat go.mod"}` + "\n" +
		"  → 10B: c\n"
	events := parseTrace(fixture)
	for _, event := range events {
		if event.breaks {
			t.Fatal("the fixture leans on a recorder boundary; it must not")
		}
	}
	rows := traceBlocks("job-1", events, tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		nil, traceLive{})
	if len(rows) != 4 {
		t.Fatalf("the round drew %d rows, want 4 (thought, batch, thought, call)", len(rows))
	}
	tight := func(i int) bool { return rows[i].(*messageBlock).tight }
	if !tight(0) {
		t.Fatal("a narration was separated from the tools it set off")
	}
	if tight(1) {
		t.Fatal("no air between a tool run and the narration after it")
	}
	if !tight(2) {
		t.Fatal("the second narration was separated from its own tool row")
	}
	if tight(3) {
		t.Fatal("the last row of a recorder closed tight")
	}
}

// THE ONE MOVING ROW (§11). A call whose result has not come back is work in
// flight, and it used to draw exactly like a call whose result had — same
// glyph, same tier, no clock — so the row a person was waiting on was the one
// row with no way to tell it was still going.
func TestACallStillInFlightSpinsAndCountsUp(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	fixture := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		"text: running the suite.\n" +
		`call sh {"cmd":"npm test -- --run"}` + "\n"
	clock := blocks.NewClock(0)
	started := time.Unix(1700000000, 0)
	clock.Latch(started.Add(25 * time.Second))

	rows := traceBlocks("job-1", parseTrace(fixture), style, nil,
		traceLive{clock: clock, since: started})
	if len(rows) != 2 {
		t.Fatalf("the tail drew %d rows, want 2 (a thought and the running call)", len(rows))
	}
	live, ok := rows[1].(*runningBlock)
	if !ok {
		t.Fatalf("the in-flight call drew a %T", rows[1])
	}
	if live.IsFinalized() {
		t.Fatal("the running row finalized; the transcript will never tick it")
	}
	row := live.Rows(80)[0]
	if !strings.Contains(row, "npm test -- --run") {
		t.Fatalf("the running row lost its command: %q", row)
	}
	// The clock is on the row wherever §20 puts it — hard right while that edge
	// is near the command, in parentheses beside it once it is not.
	if !strings.HasSuffix(row, "25s") && !strings.HasSuffix(row, "(25s)") {
		t.Fatalf("the running row is not counting up: %q", row)
	}
	spinning := false
	for _, frame := range blocks.Spinner {
		if strings.HasPrefix(row, frame) {
			spinning = true
		}
	}
	if !spinning {
		t.Fatalf("the running row is not wearing a spinner frame: %q", row)
	}
	// It MOVES with the shared clock, and it moves with nothing else: two
	// latches inside one step are byte-identical (8.1.3).
	clock.Latch(started.Add(25*time.Second + blocks.DefaultInterval))
	if next := live.Rows(80)[0]; next == row {
		t.Fatalf("the spinner did not advance with the clock: %q", next)
	}
	// CALM STANDS STILL and still says it is alive, because the clock beside
	// the glyph is what carries the fact (10.1.5).
	calm := blocks.NewClock(0)
	calm.Calm = true
	calm.Latch(started.Add(90 * time.Second))
	still := traceBlocks("job-1", parseTrace(fixture), style, nil,
		traceLive{clock: calm, since: started})[1].Rows(80)[0]
	if strings.HasPrefix(still, blocks.Spinner[1]) {
		t.Fatalf("calm mode kept spinning: %q", still)
	}
	if !strings.HasSuffix(still, "1m30") && !strings.Contains(still, "1m") {
		t.Fatalf("calm mode lost the clock that says the row is alive: %q", still)
	}
}

// SEVERAL CALLS CAN BE IN FLIGHT AT ONCE — a model that issues three tool calls
// in one turn has all three written before any result is — and then the row
// counts rather than lists, because none of them has said anything yet.
func TestSeveralCallsInFlightCountThemselves(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	fixture := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		`call web {"q":"one"}` + "\n" +
		`call web {"q":"two"}` + "\n" +
		`call web {"q":"three"}` + "\n"
	clock := blocks.NewClock(0)
	started := time.Unix(1700000000, 0)
	clock.Latch(started.Add(4 * time.Second))
	rows := traceBlocks("job-1", parseTrace(fixture), style, nil,
		traceLive{clock: clock, since: started})
	if len(rows) != 1 {
		t.Fatalf("three in-flight calls drew %d rows, want one", len(rows))
	}
	row := rows[0].Rows(60)[0]
	if !strings.Contains(row, runningWord+"3") ||
		(!strings.HasSuffix(row, "4s") && !strings.HasSuffix(row, "(4s)")) {
		t.Fatalf("the running row is %q", row)
	}
}

// A CALL WITH NO RESULT IN THE MIDDLE OF A DOCUMENT IS NOT RUNNING. Its result
// was lost to a crash or to the 32KB window this reader keeps, and drawing it
// with a spinner would be the surface claiming liveness it cannot see (8.2.20).
func TestOnlyTheTailOfARecorderIsEverDrawnAsRunning(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	fixture := `call sh {"cmd":"one"}` + "\n" +
		"text: the result of that never arrived\n" +
		`call sh {"cmd":"two"}` + "\n" +
		"  → 10B: done\n"
	rows := traceBlocks("job-1", parseTrace(fixture), style, nil, traceLive{})
	for i, block := range rows {
		if _, live := block.(*runningBlock); live {
			t.Fatalf("row %d of a settled recorder was drawn as running", i)
		}
	}
}

// §5, verbatim: "shell commands syntax-highlighted at the dim end of the pastel
// ramp". A command is the one salient input with structure a reader PARSES —
// the program, its flags, its quoted argument — where a query or a path is one
// word to the eye.
//
// The lane that filed this judged that a pre-painted title would fight the
// header's own painter. It does not: [blocks.Header] measures and cuts
// ANSI-aware, and its paint wraps a span that has already chosen its colour, so
// the inner colours are what reach the screen — the same property a
// [blocks.CardBlock] body line relies on.
func TestAShellCommandIsHighlightedInsideTheHeader(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	fixture := `call sh {"cmd":"if [ -f go.mod ]; then echo \"found\"; fi"}` + "\n" +
		"  → 10B: found\n"
	row := traceBlocks("job-1", parseTrace(fixture), style, nil, traceLive{})[0].Rows(100)[0]

	// The words survive whole and in order — highlighting is colour, never a
	// rewrite of what the row says.
	if got := ansi.Strip(row); !strings.Contains(got, `if [ -f go.mod ]; then echo "found"; fi`) {
		t.Fatalf("the command did not survive the header: %q", got)
	}
	// The lexer's own finds are coloured off the code ramp…
	lit := false
	for _, slot := range tokens.CodeSlots() {
		if slot == tokens.CodeText {
			continue
		}
		if strings.Contains(row, slot.Fg(tokens.TrueColor, tokens.FocusNormal)) {
			lit = true
		}
	}
	if !lit {
		t.Fatalf("no span of the command was highlighted: %q", row)
	}
	// …and everything the lexer had nothing to say about keeps the TOOL tier,
	// so being lexed does not promote the row to the narration's ink. (The
	// header still opens the title with its own tier and the pre-painted span
	// overrides it on the very next byte, which is exactly the pass-through
	// this test exists to prove.)
	if tierOf(style, row, " -f go.mod ") != tokens.TextSecondary {
		t.Fatalf("highlighting promoted the tool row's plain runs: %q", row)
	}
	// A query is NOT source and is not taken apart: only a command is.
	web := traceBlocks("job-2", parseTrace(`call web {"q":"if then echo fi"}`+"\n"),
		style, nil, traceLive{})[0].Rows(100)[0]
	if tierOf(style, web, "if then echo fi") != tokens.TextSecondary {
		t.Fatalf("a search query was lexed as source: %q", web)
	}
	// And a profile with no ramp draws the same row, unpainted, saying the same
	// thing — the degradation a fenced block makes, for the same reason.
	plainStyle := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	plain := traceBlocks("job-1", parseTrace(fixture), plainStyle, nil, traceLive{})[0].Rows(100)[0]
	if strings.ContainsRune(plain, 0x1b) {
		t.Fatalf("a colourless profile drew escapes: %q", plain)
	}
	if !strings.Contains(plain, "go.mod") {
		t.Fatalf("the colourless row lost the command: %q", plain)
	}
}

// A ROOM WATCHING WORK ARMS THE CLOCK. A recorder append journals nothing at
// all, so a poll finds the journal quiet for the whole length of a tool call —
// and without this the spinner would advance only when something else happened
// to move, which is a frozen room that reads as a stopped window.
func TestARoomWatchingACallInFlightArmsTheAnimationClock(t *testing.T) {
	backend := recordBoard()
	running := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		"text: running the suite.\n" +
		`call sh {"cmd":"npm test"}` + "\n"
	commander := &tracingCommander{traces: map[string]string{"job-3": running}}
	app := newTestApp(backend, commander, nil)
	poll(t, app)
	enterTracedRoom(t, app, "5")

	if app.turn.active {
		t.Fatal("the fixture has a live turn; the clock would arm for that instead")
	}
	if !app.roomIsLive() {
		t.Fatal("a room with a call in flight does not read as live")
	}
	if cmd := app.startTick(); cmd == nil {
		t.Fatal("the room did not arm the animation clock")
	}
	app.ticking = false

	// And it stands down the moment the result lands: the tail re-parses, the
	// running row becomes the ordinary quiet one, and the window costs nothing
	// again.
	commander.traces["job-3"] = running + "  → 40B: 12 passed\n"
	// The one part still in flight settles with it. A running row in the record's
	// tree is live in its own right (recordtree.go) — that is what makes its
	// spinner turn and its elapsed count — so a room is only quiet once the
	// recorder AND the graph have both stopped.
	for i := range backend.nodes {
		if backend.nodes[i].ID == "job-3/h2" {
			backend.nodes[i].Status = store.Done
			backend.nodes[i].UpdatedSeq = 60
		}
	}
	backend.journal++
	poll(t, app)
	msg, ok := app.readTraceCmd(app.view.node)().(traceReadMsg)
	if !ok || !msg.moved {
		t.Fatalf("the grown recorder did not read back as moved: %+v", msg)
	}
	app.applyTraceRead(msg)
	if app.roomIsLive() {
		t.Fatal("the room kept ticking after the call came back")
	}
	if cmd := app.startTick(); cmd != nil {
		t.Fatal("a settled room armed the animation clock")
	}
}

// THE DOOR SITS WHERE THE EYE ALREADY IS. `▸ 2 lines` renders immediately after
// the salient input it opens, not right-aligned across a gulf of empty cells:
// a receipt is a figure a reader SCANS down a column, and a door is something
// they AIM AT. The receipt column is telemetry and only telemetry.
func TestTheFoldHintSitsInlineAndNotInTheReceiptColumn(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	rows := traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{})
	var shell string
	for _, block := range rows {
		if row := block.Rows(100)[0]; strings.Contains(row, "ls -la clips/") {
			shell = row
		}
	}
	if shell == "" {
		t.Fatal("the fixture drew no shell row")
	}
	hint := blocks.Disclose(false, 2, "line", "lines")
	if !strings.Contains(shell, "ls -la clips/  "+hint) {
		t.Fatalf("the door is not inline after the input it opens: %q", shell)
	}
	// And the estimate is the only thing after it. At 100 columns a four-word
	// command leaves a gulf §20 refuses to call a column, so the figure rides
	// home in parentheses — but it still comes AFTER the door, and nothing else
	// comes between them.
	if !strings.HasSuffix(shell, "~359 tok") && !strings.HasSuffix(shell, "(~359 tok)") {
		t.Fatalf("the row does not end on its estimate: %q", shell)
	}
	between := shell[strings.Index(shell, hint)+len(hint) : strings.LastIndex(shell, "~359 tok")]
	if strings.Trim(between, " (") != "" {
		t.Fatalf("something crept between the door and the estimate: %q", between)
	}
}

// §20's condition, on the surface it was measured on: a trace row's estimate is
// a right-hand COLUMN only while that edge is near the command it describes.
//
// The record is a wide main pane, not the narrow dense list §20 reserves the
// shared right column for, so a short command on a big terminal used to throw
// `~359 tok` most of a screen away from the four words it was the size of —
// "the defect the user has now flagged twice". Narrow, the column still reads as
// a column and stays.
func TestATraceRowsEstimateComesHomeOnAWideTerminal(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	shell := func(width int) string {
		for _, block := range traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{}) {
			if row := ansi.Strip(block.Rows(width)[0]); strings.Contains(row, "ls -la clips/") {
				return row
			}
		}
		t.Fatalf("the fixture drew no shell row at width %d", width)
		return ""
	}
	if wide := shell(160); !strings.HasSuffix(wide, "(~359 tok)") {
		t.Fatalf("a wide row kept its gulf: %q", wide)
	}
	if narrow := shell(44); !strings.HasSuffix(narrow, "~359 tok") ||
		strings.HasSuffix(narrow, "(~359 tok)") {
		t.Fatalf("a narrow row gave up a column that was still close: %q", narrow)
	}
	// The estimate is never lost and never halved on the way between them. The
	// walk starts where the command itself still fits: below that the row is a
	// truncated command and there is no estimate to place.
	for width := 60; width <= 200; width++ {
		row := shell(width)
		if strings.Contains(row, "tok") && !strings.Contains(row, "~359 tok") {
			t.Fatalf("width %d drew part of an estimate: %q", width, row)
		}
	}
}

// The estimate is MARKED as an estimate and spoken in the unit a person judges
// a run in. Bytes were a fact about a file; `~359 tok` is a fact about what the
// window just swallowed, and the tilde is what keeps it inside 8.2.20 — a
// silent fake is banned, a marked approximation is honest.
func TestAResultsSizeIsAMarkedTokenEstimate(t *testing.T) {
	for _, c := range []struct {
		bytes int
		want  string
	}{
		{0, ""},
		{160, "~40 tok"},
		{6000, "~1.5K tok"},
		{480000, "~120K tok"},
	} {
		if got := resultCost(c.bytes); got != c.want {
			t.Fatalf("%dB reads as %q, want %q", c.bytes, got, c.want)
		}
	}
	// NO PER-CALL DOLLARS, ever: a round's calls share one bill, so a split
	// would have to be invented (8.2.20). The job's own header carries the
	// money, where it is a measurement.
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	for _, block := range traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{}) {
		row := block.Rows(100)[0]
		for _, money := range []string{tokens.GlyphSpend + "0", tokens.GlyphSpend + tokens.GlyphMissing} {
			if strings.Contains(row, money) {
				t.Fatalf("an execution row priced itself: %q", row)
			}
		}
	}
}

// CYAN MEANS ALIVE (§18.3), so it may not sit on a row that has finished. A
// settled tool glyph is dim machinery; a failed one is coral; the ONE row
// entitled to the alive hue is the one that is actually running.
func TestOnlyARunningRowWearsTheAliveHue(t *testing.T) {
	style := tokens.NewStyler(tokens.TrueColor, tokens.FocusNormal)
	cyan := tokens.Cyan.Fg(tokens.TrueColor, tokens.FocusNormal)
	coral := tokens.Coral.Fg(tokens.TrueColor, tokens.FocusNormal)

	for _, block := range traceBlocks("job-1", parseTrace(traceFixture), style, nil, traceLive{}) {
		row := block.Rows(100)[0]
		if strings.Contains(row, cyan) {
			t.Fatalf("a settled execution row wears the alive hue: %q", row)
		}
	}
	// The batch is the same rule with more rows behind it.
	for _, block := range traceBlocks("job-2", parseTrace(traceBatchFixture), style, nil, traceLive{}) {
		if row := block.Rows(100)[0]; strings.Contains(row, cyan) {
			t.Fatalf("a settled batch wears the alive hue: %q", row)
		}
	}
	// A failure still says so in the one hue that means broken.
	failed := traceBlocks("job-3", parseTrace(
		`call web {"q":"ffmpeg concat mp4"}`+"\n  → 902B ERROR: exa 503\n"),
		style, nil, traceLive{})[0].Rows(100)[0]
	if !strings.Contains(failed, coral) {
		t.Fatalf("a failed call lost its coral: %q", failed)
	}
	// And the running row keeps cyan, because it is the one row the hue is
	// actually about.
	clock := blocks.NewClock(0)
	clock.Latch(time.Unix(1700000000, 0))
	running := traceBlocks("job-4", parseTrace(`call sh {"cmd":"npm test"}`+"\n"),
		style, nil, traceLive{clock: clock, since: time.Unix(1700000000, 0)})[0].Rows(100)[0]
	if !strings.Contains(running, cyan) {
		t.Fatalf("the running row lost the alive hue: %q", running)
	}
}

// -- the machine-stream clamp --------------------------------------------------

// machineFixture is a recorder written the way the coding subharness used to
// write one: a couple of sentences and then the child's raw event feed, one
// NDJSON line per row. The shapes are copied from the reporter's own recorder
// (task-9196's 9200.trace.log, 2.1MB, 5,928 lines) and trimmed.
func machineFixture(events int) string {
	var b strings.Builder
	b.WriteString("workspace: an existing git repository, run in place\n")
	b.WriteString("── turn 1  finish=tool_calls  in=10 out=10 ──\n")
	b.WriteString("text: reading the issue.\n")
	for i := 0; i < events; i++ {
		b.WriteString(`{"id":"evt_ff1abc` + strconv.Itoa(i) +
			`","type":"message.part.delta","properties":{"sessionID":"ses_00e543e78ffe",` +
			`"messageID":"msg_ff1abc1","delta":"a token of the model's answer"}}` + "\n")
	}
	return b.String()
}

// THE DEFECT, AS A RENDERING. Hundreds of raw JSONL lines rendered as content,
// full-width, unbounded, for screens on end. The record law already required
// tool output to sit in a bounded box; this content bypassed it by never being
// output — it arrived as N free-form notes, and a note is a header TITLE, which
// no box has ever bounded. One row now, with the law's box behind it.
func TestAMachineStreamIsOneBoundedBoxAndNeverProse(t *testing.T) {
	events := parseTrace(machineFixture(400))

	streams := 0
	for _, event := range events {
		if event.kind == traceMachine {
			streams++
			continue
		}
		if strings.Contains(event.gist, `"type":"message.part.delta"`) {
			t.Fatalf("a machine line is still a row of its own: %q", event.gist)
		}
	}
	if streams != 1 {
		t.Fatalf("400 machine lines parsed into %d stream rows, want exactly one", streams)
	}
	// The sentences around it are untouched: the clamp folds machinery and
	// never a voice.
	if !hasKind(events, traceThought) {
		t.Fatal("the clamp ate the model's own narration")
	}

	blocks := traceBlocks("job-1", events, tokens.NewStyler(tokens.NoColor, tokens.FocusNormal),
		func(string) bool { return false }, traceLive{})
	var rows []string
	for _, block := range blocks {
		rows = append(rows, block.Rows(96)...)
	}
	// A 400-line stream costs ONE row while shut, and the door counts what is
	// behind it. Four rows of slack for the seam's own blanks and the two
	// sentences above.
	if len(rows) > 8 {
		t.Fatalf("a 400-line machine stream drew %d rows:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{traceStreamWord, "▸ ", " tok"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the bounded row is missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "evt_ff1abc") {
		t.Fatalf("the raw stream is on screen while the row is shut:\n%s", joined)
	}
}

// AND OPENING IT IS STILL BOUNDED. §5's box is twelve rows and one more door,
// which is exactly what an opened tool result gets — a record that dumped four
// hundred lines into itself because someone tapped a row would be the same
// defect one gesture later.
func TestAnOpenedMachineStreamStaysInsideTheBox(t *testing.T) {
	events := parseTrace(machineFixture(400))
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	open := func(id string) bool { return !strings.HasSuffix(id, traceMoreSuffix) }

	var rows []string
	for _, block := range traceBlocks("job-1", events, style, open, traceLive{}) {
		rows = append(rows, block.Rows(96)...)
	}
	body := 0
	for _, row := range rows {
		if strings.Contains(row, "evt_ff1abc") {
			body++
		}
	}
	if body == 0 {
		t.Fatal("the opened box shows nothing at all")
	}
	if body > traceBoxRows {
		t.Fatalf("the opened box drew %d lines of stream, the law allows %d", body, traceBoxRows)
	}
}

// A LONG DUMP THAT IS NOT JSON IS STILL NOT A RECORD. The clamp is a defence
// against machine-authored bytes and not against one producer's format.
func TestALongDumpOfNotesCollapsesToo(t *testing.T) {
	var b strings.Builder
	b.WriteString("text: building.\n")
	for i := 0; i < 60; i++ {
		b.WriteString("  compiling module " + strconv.Itoa(i) + " of 60\n")
	}
	events := parseTrace(b.String())
	if !hasKind(events, traceMachine) {
		t.Fatalf("sixty consecutive log lines stayed sixty rows: %d events", len(events))
	}
}

// AND A HANDFUL OF ORDINARY NOTES IS LEFT ALONE. The recorder's own preamble —
// `workspace:`, `engine:`, the contract in force — is prose a person reads, and
// a clamp that swallowed it would have traded one defect for another.
func TestTheRecordersOwnNotesAreNotClamped(t *testing.T) {
	events := parseTrace(
		"workspace: an existing git repository, run in place\n" +
			"engine: aforge run --dir /tmp/x -- <goal>\n" +
			"contract in force: verify before finishing\n" +
			"text: starting.\n")
	if hasKind(events, traceMachine) {
		t.Fatal("three ordinary notes were folded away as a machine stream")
	}
	notes := 0
	for _, event := range events {
		if event.kind == traceNote {
			notes++
		}
	}
	if notes != 3 {
		t.Fatalf("the recorder's preamble drew %d note rows, want 3", notes)
	}
}

func hasKind(events []traceEvent, kind traceKind) bool {
	for _, event := range events {
		if event.kind == kind {
			return true
		}
	}
	return false
}

// -- the record's running rows, frame by frame ---------------------------------

// FRAME N AND FRAME N+1 DIFFER WHILE THE CALL IS IN FLIGHT, AND ARE IDENTICAL
// ONCE IT SETTLES. That is the whole of "something moves while work runs",
// asserted on the bytes rather than on the intent.
func TestTheRecordsRunningRowMovesBetweenFramesAndStopsWhenItSettles(t *testing.T) {
	running := "── turn 1  finish=tool_calls  in=10 out=10 ──\n" +
		"text: running the suite.\n" +
		`call sh {"cmd":"npm test"}` + "\n"
	events := parseTrace(running)
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)

	clock := blocks.NewClock(0)
	base := time.Unix(1700000000, 0)
	live := traceLive{clock: clock, since: base}

	draw := func(at time.Time) string {
		clock.Latch(at)
		var rows []string
		for _, block := range traceBlocks("job-1", events, style, nil, live) {
			rows = append(rows, block.Rows(80)...)
		}
		return strings.Join(rows, "\n")
	}
	first := draw(base.Add(blocks.DefaultInterval))
	next := draw(base.Add(2 * blocks.DefaultInterval))
	if first == next {
		t.Fatalf("two consecutive frames of a running row are identical:\n%s", first)
	}
	// A repaint inside the SAME step is byte-identical, which is what keeps the
	// dirty-row count at zero (8.1.3).
	if again := draw(base.Add(blocks.DefaultInterval + 10*time.Millisecond)); again != first {
		t.Fatalf("a repaint inside one step changed the bytes:\n%s\n%s", first, again)
	}

	// Settled: the result lands, the row stops being live, and nothing moves.
	settled := parseTrace(running + "  → 40B: 12 passed\n")
	drawSettled := func(at time.Time) string {
		clock.Latch(at)
		var rows []string
		for _, block := range traceBlocks("job-1", settled, style, nil, live) {
			rows = append(rows, block.Rows(80)...)
		}
		return strings.Join(rows, "\n")
	}
	a := drawSettled(base.Add(blocks.DefaultInterval))
	b := drawSettled(base.Add(9 * blocks.DefaultInterval))
	if a != b {
		t.Fatalf("a settled record still animates:\n%s\n%s", a, b)
	}
}

// CALM FREEZES THE GLYPH AND KEEPS THE CLOCK. The established rule, on the one
// surface that already had both cells.
func TestCalmFreezesTheRunningGlyphAndKeepsTheClockCounting(t *testing.T) {
	events := parseTrace("── turn 1 ──\n" + `call sh {"cmd":"npm test"}` + "\n")
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	clock := blocks.NewClock(0)
	clock.Calm = true
	base := time.Unix(1700000000, 0)
	live := traceLive{clock: clock, since: base}

	draw := func(at time.Time) string {
		clock.Latch(at)
		var rows []string
		for _, block := range traceBlocks("job-1", events, style, nil, live) {
			rows = append(rows, block.Rows(80)...)
		}
		return strings.Join(rows, "\n")
	}
	first := draw(base.Add(time.Second))
	later := draw(base.Add(9 * time.Second))
	glyph := func(row string) string { return string([]rune(strings.TrimSpace(row))[0]) }
	if glyph(first) != glyph(later) {
		t.Fatalf("a calm profile animated the glyph:\n%s\n%s", first, later)
	}
	if !strings.Contains(first, "1s") || !strings.Contains(later, "9s") {
		t.Fatalf("the clock stopped counting under calm:\n%s\n%s", first, later)
	}
}

// §20's ladder under an OPENED tool row: the continuation that holds the rest of
// a long result hangs its `│` in the child's two marker cells and its bytes at
// the child's edge — one rung under the row it continues, never two.
//
// THE DEFECT WAS A STEP TAKEN TWICE. The continuation block is created at
// `indent + bodyIndent`, which is already the rung; its quoted run then asked for
// bodyIndent again, so the block's own pad and the segment's indent stacked. A
// top-level row at column 2 opened onto a box whose bar sat at column 4 and whose
// bytes sat at column 6 — a whole rung below anything on screen — and a batch
// child's box landed at 6 and 8. §20's ladder is 2+2n with no rung skipped.
func TestAnOpenedResultsBoxHangsOneRungUnderItsRow(t *testing.T) {
	style := tokens.NewStyler(tokens.NoColor, tokens.FocusNormal)
	// A result long enough to spill past the bounded box, so the row opens onto
	// a continuation at all.
	lines := make([]string, 0, traceBoxRows*2)
	for i := 0; i < traceBoxRows*2; i++ {
		lines = append(lines, "line "+strconv.Itoa(i)+" of the result")
	}
	event := traceEvent{
		kind: traceCall, tool: "sh", family: toolFamily{verb: "ran"},
		gist: "ls -la", returned: true, output: strings.Join(lines, "\n"),
	}
	open := func(string) bool { return true }

	for _, depth := range []int{0, 1} {
		indent := blocks.MarkerCol(depth)
		out := callRowBlocks("job-1", 0, event, false, indent, style, open)
		if len(out) != 2 {
			t.Fatalf("depth %d: an opened long result drew %d blocks, want the row and its continuation",
				depth, len(out))
		}
		more, ok := out[1].(*messageBlock)
		if !ok {
			t.Fatalf("depth %d: the continuation is a %T", depth, out[1])
		}
		// The row's own content edge, and the continuation exactly one rung in.
		if want := blocks.Depth(depth); more.indent != want {
			t.Fatalf("depth %d: the continuation sits at %d, want %d", depth, more.indent, want)
		}
		// And what it draws lands there: the bar in the marker cells, the bytes
		// at the edge one step further in.
		var bar, text int = -1, -1
		for _, row := range more.Rows(120) {
			plain := ansi.Strip(row)
			at := strings.Index(plain, tokens.GlyphTreeVert)
			if at < 0 || !strings.Contains(plain, "line ") {
				continue
			}
			bar = blocks.Width(plain[:at])
			text = blocks.Width(plain[:strings.Index(plain, "line ")])
			break
		}
		if bar < 0 {
			t.Fatalf("depth %d: the opened box drew no quoted rows:\n%s",
				depth, strings.Join(more.Rows(120), "\n"))
		}
		if want := blocks.MarkerCol(depth + 1); bar != want {
			t.Fatalf("depth %d: the box's │ sits at column %d, want the child's marker cells at %d",
				depth, bar, want)
		}
		if want := blocks.Depth(depth + 1); text != want {
			t.Fatalf("depth %d: the box's bytes sit at column %d, want the child's edge at %d",
				depth, text, want)
		}
	}
}
