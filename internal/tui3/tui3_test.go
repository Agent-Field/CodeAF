package tui3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// fakeAgent is the scripted session every test here runs against: it answers
// from a queue of event batches and records what it was asked to do. It is the
// reason [Agent] is an interface — the surface is driven without a provider, a
// key, or a file.
type fakeAgent struct {
	turns   [][]session.Event
	turn    int
	live    chan session.Event
	model   string
	window  int
	usage   session.Usage
	sent    []string
	stops   int
	closes  int
	packs   int
	failing error
	// past is what a resumed session already holds — what [app.replay] draws.
	past []session.DisplayEntry
	// weight is what the agent says the conversation weighs in tokens, which is
	// what the context meter reads (the surface no longer measures it itself).
	weight int
	// levels is the reasoning strength held per model id, the session's own map
	// as far as the surface can see it (internal/session's agent.go).
	levels map[string]string
}

func (f *fakeAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	f.sent = append(f.sent, text)
	if f.failing != nil {
		return nil, f.failing
	}
	if f.live != nil {
		// Steering: the in-flight turn takes the message and the caller gets a
		// closed channel, exactly as session.Agent documents.
		done := make(chan session.Event)
		close(done)
		return done, nil
	}
	out := make(chan session.Event, 64)
	if f.turn < len(f.turns) {
		for _, event := range f.turns[f.turn] {
			out <- event
		}
		f.turn++
	}
	f.live = out
	return out, nil
}

// finish closes the live turn's channel, which is what ends a real stream.
func (f *fakeAgent) finish() {
	if f.live != nil {
		close(f.live)
		f.live = nil
	}
}

func (f *fakeAgent) Interrupt()                       { f.stops++ }
func (f *fakeAgent) Compact(context.Context) error    { f.packs++; return nil }
func (f *fakeAgent) Close() error                     { f.closes++; return nil }
func (f *fakeAgent) Model() string                    { return f.model }
func (f *fakeAgent) SetModel(model string)            { f.model = model }
func (f *fakeAgent) SetContextWindow(tokens int)      { f.window = tokens }
func (f *fakeAgent) ReasoningFor(model string) string { return f.levels[model] }
func (f *fakeAgent) SetReasoningFor(model, level string) {
	if f.levels == nil {
		f.levels = map[string]string{}
	}
	if level == "" {
		delete(f.levels, model)
		return
	}
	f.levels[model] = level
}
func (f *fakeAgent) Usage() session.Usage                 { return f.usage }
func (f *fakeAgent) Transcript() []session.DisplayEntry   { return f.past }
func (f *fakeAgent) ContextTokens() int                   { return f.weight }
func text(kind session.EventKind, s string) session.Event { return session.Event{Kind: kind, Text: s} }

// drive runs messages through the app the way the program loop would: update,
// run whatever command came back, feed its message in, repeat until the
// surface goes quiet.
//
// Two of the surface's commands never return on their own — a wait on a stream
// that has more turn to come, and the paint clock, which reschedules itself
// for as long as a turn is running. A command is therefore given a short budget
// and dropped when it exceeds it, and the paint clock's own message is dropped
// outright: it fires every 33ms forever while working, so a harness that
// followed it would never reach the end of the queue. The tests that care about
// the clock deliver [frameMsg] themselves.
func drive(t *testing.T, a *app, msgs ...tea.Msg) {
	t.Helper()
	queue := append([]tea.Msg(nil), msgs...)
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 500 {
			t.Fatal("the surface did not settle")
		}
		msg := queue[0]
		queue = queue[1:]
		model, cmd := a.Update(msg)
		a = model.(*app)
		for _, produced := range runCmd(cmd) {
			if _, clock := produced.(frameMsg); clock {
				a.painting = false // let the next mutation ask for a tick again
				continue
			}
			queue = append(queue, produced)
		}
	}
}

// runCmd executes one command within a budget and flattens a batch.
func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch produced := msg.(type) {
		case nil:
			return nil
		case tea.BatchMsg:
			var out []tea.Msg
			for _, one := range produced {
				out = append(out, runCmd(one)...)
			}
			return out
		default:
			return []tea.Msg{produced}
		}
	case <-time.After(150 * time.Millisecond):
		return nil
	}
}

// newTestApp pins the colour profile AND the glyph tier. A test inherits
// whatever TERM and LANG the machine running it has, and a surface that
// rendered unpainted under NO_COLOR — or ASCII under LANG=C — would make every
// assertion about an escape sequence or a rail marker a test of the
// environment.
func newTestApp(agent Agent) *app {
	a := newApp(context.Background(), Options{Agent: agent, Workspace: "/tmp/lab"})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.entries = nil // drop the opening hint so tests read their own entries
	// The welcome box opens on an empty conversation, which every test here is
	// (welcome.go). It has its own tests; the ones that predate it read the
	// frame it used to have, so it is dismissed exactly as a first keystroke
	// dismisses it.
	a.welcome = welcome{spent: true}
	a.touch()
	return a
}

func key(s string) tea.KeyPressMsg {
	if len([]rune(s)) == 1 {
		return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
	}
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+,":
		return tea.KeyPressMsg{Code: ',', Mod: tea.ModCtrl}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{}
}

func typeLine(t *testing.T, a *app, line string) {
	t.Helper()
	for _, r := range line {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
}

func frame(a *app) string {
	f, _, _ := a.frame()
	return f
}

// plain is the frame as a reader sees it: colour is asserted where colour is
// the subject, and nowhere else.
func plain(s string) string { return ansi.Strip(s) }

// rows lays the conversation out at the test width.
func rows(a *app) []row { return a.visible(a.width) }

func plainRows(a *app) []string {
	out := make([]string, 0, len(a.rows))
	for _, r := range rows(a) {
		out = append(out, plain(r.text))
	}
	return out
}

// clickHit drives a left click on the first VISIBLE row of a kind. Visible is
// the point: a click carries a screen row, and a transcript taller than the
// window has a screen row that is not its row-list index.
func clickHit(t *testing.T, a *app, want hitKind) {
	t.Helper()
	body, pad := a.window(a.width, a.viewHeight())
	for i, r := range body {
		if r.hit == want {
			drive(t, a, tea.MouseClickMsg{Y: a.bodyTop() + pad + i, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("no visible row answers to %d:\n%s", want, strings.Join(plainRows(a), "\n"))
}

// runTurn types a line, closes the stream and drains it.
func runTurn(t *testing.T, a *app, agent *fakeAgent, line string) {
	t.Helper()
	typeLine(t, a, line)
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
}

// toolBegin is a call announced with a hint and NO payload — the degraded
// shape, which every line still has to render: the target falls back to the
// hint's gloss.
func toolBegin(tool, hint string) session.Event {
	return session.Event{Kind: session.EventToolBegin, Tool: tool, Hint: hint}
}

// toolEnd carries the result in Output, where session puts it. A successful
// call's Hint is empty on the wire (loop.go: the result belongs to the model),
// so a test that put its result there would be testing a shape that never
// arrives.
func toolEnd(tool, output string) session.Event {
	return session.Event{Kind: session.EventToolEnd, Tool: tool, Output: output}
}

func TestATurnStreamsTextToolsAndSettles(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", ""),
		text(session.EventTextDelta, "it "),
		text(session.EventTextDelta, "parses."),
		{Kind: session.EventTurnDone, Usage: session.Usage{CostUSD: 0.012}},
	}}}
	a := newTestApp(agent)

	runTurn(t, a, agent, "what does bar.go do?")

	got := plain(frame(a))
	for _, want := range []string{"› what does bar.go do?", "╰─▶ read foo/bar.go", "it parses.", "idle"} {
		if !strings.Contains(got, want) {
			t.Fatalf("frame is missing %q:\n%s", want, got)
		}
	}
	// NO SUCCESS GLYPH, EVER (D11). A quiet line is a success.
	if strings.Contains(got, "✓") {
		t.Fatalf("a settled call drew a success glyph:\n%s", got)
	}
	if agent.sent[0] != "what does bar.go do?" {
		t.Fatalf("submitted %q", agent.sent[0])
	}
	if !strings.Contains(got, "$0.01") {
		t.Fatalf("cost is missing from the status line:\n%s", got)
	}
}

func TestAFailedToolIsMarkedAndSaysWhy(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go build ./..."),
		{Kind: session.EventToolFailed, Tool: "bash", Err: errors.New("exit 2")},
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "build it")

	got := plain(frame(a))
	if !strings.Contains(got, "✗") || !strings.Contains(got, "exit 2") {
		t.Fatalf("a failed tool has to say so:\n%s", got)
	}
}

func TestCompactionDrawsADivider(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventCompacted, Hint: "compacted from ~84k tokens"},
		text(session.EventTextDelta, "carrying on"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "keep going")

	got := plain(frame(a))
	if !strings.Contains(got, "⚭ compacted from ~84k tokens") || !strings.Contains(got, "──") {
		t.Fatalf("the compaction divider is missing:\n%s", got)
	}
}

// THE SPACING LAW (D11). One blank before a user message, one before a cluster
// that follows text, one after a cluster before text, zero everywhere else —
// and never two in a row.
//
// The fixture walks every rule in one transcript: user → cluster (no blank, the
// calls ARE the reply starting) → text (one blank) → cluster (one blank) →
// text (one blank) → user (one blank).
func TestSpacingLaw(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", "ok"),
		toolBegin("grep", "func main"),
		toolEnd("grep", "main.go:3: func main()"),
		text(session.EventTextDelta, "bar.go parses the config.\n"),
		toolBegin("read", "foo/baz.go"),
		toolEnd("read", "ok"),
		text(session.EventTextDelta, "and baz.go writes it back."),
		{Kind: session.EventTurnDone},
	}, {
		text(session.EventTextDelta, "nothing else uses it."),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "what does bar.go do?")
	runTurn(t, a, agent, "and then?")

	list := plainRows(a)
	if len(list) == 0 {
		t.Fatal("nothing was laid out")
	}
	shape := make([]string, 0, len(list))
	for _, r := range list {
		switch {
		case strings.TrimSpace(r) == "":
			shape = append(shape, "_")
		case strings.HasPrefix(r, "›"):
			shape = append(shape, "u")
		case strings.HasPrefix(r, "├─▶") || strings.HasPrefix(r, "╰─▶"):
			shape = append(shape, "t")
		default:
			shape = append(shape, "x")
		}
	}
	got := strings.Join(collapse(shape), "")
	// u t _ x _ t _ x _ u x — a blank before each user message, one on each side
	// of a cluster that sits between two blocks of text, and nowhere else. The
	// reply that FOLLOWS a user message takes none: the person's message already
	// brought the boundary blank with it.
	if want := "ut_x_t_x_ux"; got != want {
		t.Fatalf("layout shape is %q, want %q:\n%s", got, want, strings.Join(list, "\n"))
	}
	for i, r := range list {
		if strings.TrimSpace(r) != "" {
			continue
		}
		if i == 0 || i+1 >= len(list) {
			t.Fatalf("a blank row opens or closes the transcript:\n%s", strings.Join(list, "\n"))
		}
		if strings.TrimSpace(list[i-1]) == "" {
			t.Fatalf("two blank rows in a row at %d:\n%s", i, strings.Join(list, "\n"))
		}
	}
}

// collapse squashes runs of the same symbol, so the shape says "text, blank,
// cluster" rather than how many rows each of them wrapped to.
func collapse(shape []string) []string {
	out := make([]string, 0, len(shape))
	for i, s := range shape {
		if i > 0 && shape[i-1] == s {
			continue
		}
		out = append(out, s)
	}
	return out
}

// The one-blank rule holds around a cluster that is the WHOLE turn: a person
// who asks for a build gets the call and then their own next message, with one
// blank between them and no gap above.
func TestAClusterThatIsTheWholeTurnTakesNoBlankAboveIt(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go build ./..."),
		toolEnd("bash", "ok"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "build it")

	list := plainRows(a)
	for i, r := range list {
		if !strings.HasPrefix(r, "╰─▶") {
			continue
		}
		if i == 0 || strings.TrimSpace(list[i-1]) == "" {
			t.Fatalf("a blank landed between the person's message and the call:\n%s",
				strings.Join(list, "\n"))
		}
		return
	}
	t.Fatalf("no tool line was drawn:\n%s", strings.Join(list, "\n"))
}

// The tool cluster is a block, not a list with air in it.
func TestToolLinesAreOneUnbrokenCluster(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "a.go"), toolEnd("read", "ok"),
		toolBegin("read", "b.go"), toolEnd("read", "ok"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read both")

	list := plainRows(a)
	first, last := -1, -1
	for i, r := range list {
		if strings.HasPrefix(r, "├─▶") || strings.HasPrefix(r, "╰─▶") {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		t.Fatalf("no tool lines were drawn:\n%s", strings.Join(list, "\n"))
	}
	for i := first; i <= last; i++ {
		if strings.TrimSpace(list[i]) == "" {
			t.Fatalf("a blank landed inside the tool cluster:\n%s", strings.Join(list, "\n"))
		}
	}
	// The rail closes on the last call and tees on every one above it.
	if !strings.HasPrefix(list[first], "├─▶") || !strings.HasPrefix(list[last], "╰─▶") {
		t.Fatalf("the cluster's markers are not ├─▶ … ╰─▶:\n%s", strings.Join(list, "\n"))
	}
}

// Past three calls the older ones fold into one line, and ctrl+o opens them.
func TestTheClusterFoldsPastThreeCalls(t *testing.T) {
	var events []session.Event
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		events = append(events, toolBegin("read", name), toolEnd("read", "ok"))
	}
	events = append(events, session.Event{Kind: session.EventTurnDone})
	agent := &fakeAgent{model: "m", turns: [][]session.Event{events}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read them all")

	list := plainRows(a)
	if !strings.Contains(strings.Join(list, "\n"), "↳ 2 earlier tool calls · ctrl+o") {
		t.Fatalf("the fold line is missing:\n%s", strings.Join(list, "\n"))
	}
	if n := countTools(a); n != toolWindow {
		t.Fatalf("%d tool lines are visible, want %d:\n%s", n, toolWindow, strings.Join(list, "\n"))
	}
	if strings.Contains(strings.Join(list, "\n"), "a.go") {
		t.Fatalf("a folded call is still on screen:\n%s", strings.Join(list, "\n"))
	}

	drive(t, a, key("ctrl+o"))
	list = plainRows(a)
	if n := countTools(a); n != 5 {
		t.Fatalf("ctrl+o showed %d calls, want 5:\n%s", n, strings.Join(list, "\n"))
	}
	if strings.Contains(strings.Join(list, "\n"), "earlier tool calls") {
		t.Fatalf("the fold line survived ctrl+o:\n%s", strings.Join(list, "\n"))
	}

	drive(t, a, key("ctrl+o"))
	if n := countTools(a); n != toolWindow {
		t.Fatalf("ctrl+o did not fold back: %d calls visible", n)
	}
}

// countTools counts the distinct tool entries on the row list.
func countTools(a *app) int {
	seen, n := -1, 0
	for _, r := range rows(a) {
		if r.hit == hitTool && r.entry != seen {
			seen = r.entry
			n++
		}
	}
	return n
}

// Opening one call shows what came back, under the rail of the line it belongs
// to, bounded by that tool's window.
func TestOpeningOneCallShowsItsResultUnderTheRail(t *testing.T) {
	long := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		long = append(long, "src/f"+strconv.Itoa(i)+".go:"+strconv.Itoa(i+1)+": func main()")
	}
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("grep", "func main ./..."),
		toolEnd("grep", strings.Join(long, "\n")),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "find main")

	call := -1
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			call = i
		}
	}
	if call < 0 {
		t.Fatal("no tool entry")
	}

	// ↑ selects the call, enter opens it — the keyboard road.
	drive(t, a, key("up"))
	if a.sel != call {
		t.Fatalf("↑ selected %d, want %d", a.sel, call)
	}
	drive(t, a, key("enter"))
	if !a.entries[call].open {
		t.Fatal("enter did not open the selected call")
	}

	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{"│ src/f0.go:1: func main()", "… 10 more lines"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the open call is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "src/f30.go") {
		t.Fatalf("the expansion window is not bounded:\n%s", body)
	}

	// The "… N more lines" foot is its own click target, and it lifts the cap
	// rather than closing the call.
	clickHit(t, a, hitMore)
	body = strings.Join(plainRows(a), "\n")
	if !a.entries[call].open || !strings.Contains(body, "src/f39.go") {
		t.Fatalf("the more-line did not lift the cap:\n%s", body)
	}

	drive(t, a, key("enter"))
	if a.entries[call].open {
		t.Fatal("enter did not close the call again")
	}
	if strings.Contains(strings.Join(plainRows(a), "\n"), "src/f0.go") {
		t.Fatal("the expansion survived the close")
	}
}

// A call that has not come back says so rather than showing an empty output.
func TestAnOpenCallSaysItIsRunning(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("bash", "go test ./..."),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "run the tests")

	call := len(a.entries) - 1
	a.openTool(call)
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "running") {
		t.Fatalf("a running call has to say so:\n%s", body)
	}
	// And it says it under the rail, like every other expansion.
	if !strings.Contains(body, "│ running") {
		t.Fatalf("the running line is not on the rail:\n%s", body)
	}
}

// A click lands on the row under the pointer, through the same mapping the
// frame draws with.
func TestAClickOpensTheCallUnderIt(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		toolBegin("read", "foo/bar.go"),
		toolEnd("read", "42 lines"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "read it")

	at := -1
	for i, r := range rows(a) {
		if r.hit == hitTool {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatal("no clickable tool row")
	}
	_, pad := a.window(a.width, a.viewHeight())
	y := a.bodyTop() + pad + at

	drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "42 lines") {
		t.Fatalf("the click did not open the call under it (y=%d):\n%s", y, body)
	}

	drive(t, a, tea.MouseClickMsg{Y: y, Button: tea.MouseLeft})
	if strings.Contains(strings.Join(plainRows(a), "\n"), "│ 42 lines") {
		t.Fatal("a second click did not close the call")
	}
}

// THE DISTINCTION. The person's words carry the glyph and the accent HUE; the
// model's carry neither, and weight belongs to markdown on both sides
// (render.go's entryUser, and thinking_test's hue tests).
func TestUserAndAssistantReadDifferently(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "it parses the config file and writes it back out again"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "what does it do, in a sentence long enough to wrap across the width")

	var user, assistant []row
	for _, r := range rows(a) {
		if r.entry < 0 {
			continue
		}
		switch a.entries[r.entry].kind {
		case entryUser:
			user = append(user, r)
		case entryAssistant:
			assistant = append(assistant, r)
		}
	}
	if len(user) < 2 || len(assistant) == 0 {
		t.Fatalf("want a wrapped user message and a reply, got %d and %d rows", len(user), len(assistant))
	}
	if !strings.HasPrefix(plain(user[0].text), glyphYou) {
		t.Fatalf("the user's first row has no glyph: %q", plain(user[0].text))
	}
	if strings.HasPrefix(plain(user[1].text), glyphYou) {
		t.Fatalf("a continuation row repeated the glyph: %q", plain(user[1].text))
	}
	if !strings.HasPrefix(plain(user[1].text), "  ") {
		t.Fatalf("a continuation row is not aligned under the text: %q", plain(user[1].text))
	}
	accent := a.pal.accent("x")
	accent = accent[:strings.Index(accent, "x")]
	for i, r := range user {
		if !strings.Contains(r.text, accent) {
			t.Fatalf("user row %d is not in the accent hue: %q", i, r.text)
		}
		if strings.Contains(r.text, "\x1b[1m") {
			t.Fatalf("user row %d is bold: hue is the marker, not weight: %q", i, r.text)
		}
	}
	for i, r := range assistant {
		if strings.Contains(r.text, accent) {
			t.Fatalf("assistant row %d wears the person's hue: %q", i, r.text)
		}
		if strings.Contains(r.text, "\x1b[1m") {
			t.Fatalf("assistant row %d is bold: %q", i, r.text)
		}
		if strings.HasPrefix(plain(r.text), glyphYou) || strings.HasPrefix(plain(r.text), "›") {
			t.Fatalf("assistant row %d wears a glyph: %q", i, plain(r.text))
		}
	}
}

// SNAPPINESS. A burst of deltas inside one frame interval builds one frame.
func TestDeltasCoalesceIntoOneFrame(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.state = stateWorking

	frame(a)
	built := a.builds
	if a.dirty {
		t.Fatal("a frame that was just built is still dirty")
	}

	a.appendText("one ")
	a.appendText("two ")
	a.appendText("three")
	if a.dirty {
		t.Fatal("a streamed delta dirtied the frame — deltas paint on the clock, not on arrival")
	}
	frame(a)
	frame(a)
	if a.builds != built {
		t.Fatalf("three deltas built %d frames, want 0", a.builds-built)
	}
	if strings.Contains(plain(frame(a)), "three") {
		t.Fatal("a delta reached the screen without a frame tick")
	}

	// One tick of the clock promotes all three at once.
	a.paint()
	if !a.dirty {
		t.Fatal("the clock did not mark the frame dirty")
	}
	if got := plain(frame(a)); !strings.Contains(got, "one two three") {
		t.Fatalf("the frame did not catch up:\n%s", got)
	}
	if a.builds != built+1 {
		t.Fatalf("one tick built %d frames, want 1", a.builds-built)
	}
}

// A settled entry is not re-rendered because the screen was asked for again.
func TestSettledEntriesAreNotReRendered(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "a settled paragraph"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "say something")

	frame(a)
	at := len(a.entries) - 1
	if !a.entries[at].built || len(a.entries[at].rows) == 0 {
		t.Fatal("the reply was never rendered")
	}
	// Row identity, not row equality: a re-render would hand back a new
	// backing array even when it produced the same characters.
	before := &a.entries[at].rows[0]
	a.touch()
	frame(a)
	if after := &a.entries[at].rows[0]; before != after {
		t.Fatal("a settled entry re-rendered on a frame that did not change it")
	}

	a.width = 40
	a.touch()
	frame(a)
	if a.entries[at].width != 40 {
		t.Fatal("a width change did not re-render the entry")
	}
}

// The markdown swap: plain while the words are still arriving, rendered once
// the turn is done.
func TestMarkdownArrivesOnSettle(t *testing.T) {
	body := "# Title\n\nsome **words** about it"
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, body),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "write me something")

	at := a.live
	if at < 0 {
		t.Fatal("nothing is live")
	}
	if a.entries[at].settled {
		t.Fatal("a streaming reply is already settled")
	}
	if got, want := a.entryRows(a.conversation(), at, a.width), trimBlanks(wrap(body, a.width)); !sameRows(got, want) {
		t.Fatalf("a streaming reply is not plain:\n%#v\n%#v", got, want)
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTurnDone}})
	if !a.entries[at].settled {
		t.Fatal("the reply did not settle")
	}
	if got, want := a.entryRows(a.conversation(), at, a.width), trimBlanks(renderMarkdown(body, a.width)); !sameRows(got, want) {
		t.Fatalf("a settled reply is not rendered markdown:\n%#v\n%#v", got, want)
	}
}

func sameRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSlashCommandsAreConsumedLocally(t *testing.T) {
	agent := &fakeAgent{model: "start/model"}
	a := newTestApp(agent)

	typeLine(t, a, "/model openai/gpt-4.1-mini")
	if agent.model != "openai/gpt-4.1-mini" {
		t.Fatalf("model is %q", agent.model)
	}
	if !strings.Contains(plain(frame(a)), "openai/gpt-4.1-mini") {
		t.Fatalf("the status line did not follow the model:\n%s", plain(frame(a)))
	}

	typeLine(t, a, "/compact")
	if agent.packs != 1 {
		t.Fatalf("compact ran %d times", agent.packs)
	}

	typeLine(t, a, "/nonsense")
	if !strings.Contains(plain(frame(a)), "unknown command: /nonsense") {
		t.Fatalf("an unknown slash has to answer:\n%s", plain(frame(a)))
	}

	typeLine(t, a, "/help")
	// The table is asserted at its source and the note at the screen: the help
	// block is taller than a twenty-row test frame, so which of its rows the
	// bottom of the screen happens to show is a fact about the terminal.
	if !strings.Contains(helpText(a.file), "/model <slug>") {
		t.Fatalf("help is missing the command table:\n%s", helpText(a.file))
	}
	if !strings.Contains(plain(frame(a)), "/compact") {
		t.Fatalf("help did not reach the screen:\n%s", plain(frame(a)))
	}

	if len(agent.sent) != 0 {
		t.Fatalf("a slash command reached the model: %v", agent.sent)
	}
}

func TestEscInterruptsAndCtrlCCloses(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "thinking about it"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "long one")

	drive(t, a, key("esc"))
	if agent.stops != 1 {
		t.Fatalf("esc did not interrupt (%d)", agent.stops)
	}
	if !strings.Contains(plain(frame(a)), "interrupted") {
		t.Fatalf("the status line has to say interrupted:\n%s", plain(frame(a)))
	}

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("ctrl+c has to quit")
	}
	if agent.closes != 1 {
		t.Fatalf("ctrl+c closed the agent %d times", agent.closes)
	}
}

func TestSteeringDoesNotAbandonTheLiveStream(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		text(session.EventTextDelta, "first"),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "one")
	generation, stream := a.gen, a.stream

	typeLine(t, a, "two")
	if a.gen != generation || a.stream != stream {
		t.Fatal("a steering submit replaced the stream it was steering")
	}
	if a.state != stateWorking {
		t.Fatalf("state is %v", a.state)
	}
	if len(agent.sent) != 2 {
		t.Fatalf("the steering message was not sent: %v", agent.sent)
	}
}

func TestScrollSticksToTheBottomUntilTheReaderLeaves(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	for i := 0; i < 40; i++ {
		a.note("line")
	}
	if !a.stick {
		t.Fatal("a surface that never scrolled has to be stuck to the bottom")
	}
	bottom := a.offsetFor(len(a.visible(a.width)), a.viewHeight())

	a.scroll(-5)
	if a.stick {
		t.Fatal("scrolling up has to release the stick")
	}
	if got := a.offsetFor(len(a.visible(a.width)), a.viewHeight()); got != bottom-5 {
		t.Fatalf("offset is %d, want %d", got, bottom-5)
	}

	a.scroll(100)
	if !a.stick {
		t.Fatal("reaching the bottom has to re-arm the stick")
	}
}

func TestTheEllipsisOnlyShowsWhileNothingElseIsMoving(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{}, {
		toolBegin("bash", "go test ./..."),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "think about it")
	if _, ok := a.ellipsis(); !ok {
		t.Fatal("a working turn with nothing on screen has to show the ellipsis")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: toolBegin("bash", "go test ./...")})
	if _, ok := a.ellipsis(); ok {
		t.Fatal("a spinning call is already the sign of life")
	}
	drive(t, a, streamEventMsg{gen: a.gen, ev: toolEnd("bash", "ok")})

	a.appendText("done: ")
	if _, ok := a.ellipsis(); ok {
		t.Fatal("streaming text replaces the ellipsis")
	}

	a.state = stateIdle
	if _, ok := a.ellipsis(); ok {
		t.Fatal("an idle surface has no ellipsis")
	}
}

func TestNewOpensAFreshAgentAndClearsTheTranscript(t *testing.T) {
	first := &fakeAgent{model: "m"}
	second := &fakeAgent{model: "m2"}
	a := newApp(context.Background(), Options{
		Agent:     first,
		Workspace: "/tmp/lab",
		Fresh:     func() (Agent, string, error) { return second, "/tmp/next.jsonl", nil },
	})
	a.width, a.height = 60, 20
	a.pal = newPalette(tokens.ANSI256, false)
	a.note("something old")

	typeLine(t, a, "/new")
	if first.closes != 1 {
		t.Fatalf("the old agent was closed %d times", first.closes)
	}
	if a.agent != Agent(second) || a.file != "/tmp/next.jsonl" {
		t.Fatal("/new did not take the fresh agent")
	}
	got := plain(frame(a))
	if strings.Contains(got, "something old") {
		t.Fatalf("the old conversation survived /new:\n%s", got)
	}
	if !strings.Contains(got, "new session · /tmp/next.jsonl") {
		t.Fatalf("/new has to name the file:\n%s", got)
	}
}

// syncBuffer is the output side of the headless boot: a Bubble Tea program
// writes from its own goroutine, and the test reads while it does.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The surface comes up, draws, takes a command and goes down with no terminal
// attached at all — the same headless door tui2 is checked through.
func TestTheSurfaceBootsAndQuitsHeadlessly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	agent := &fakeAgent{model: "openai/gpt-4.1-mini"}
	in, keyboard := io.Pipe()
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Agent: agent, Workspace: "/tmp/lab",
			Input: in, Output: out, Width: 80, Height: 24,
		})
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), agent.model) {
		if time.Now().After(deadline) {
			t.Fatalf("the surface never drew its status line:\n%q", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if wire := out.String(); !strings.Contains(wire, "\x1b[?1049h") {
		t.Fatal("the surface did not enter the alt screen")
	}

	if _, err := keyboard.Write([]byte("/quit\r")); err != nil {
		t.Fatalf("write to the surface: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the surface returned %v", err)
		}
	case <-ctx.Done():
		t.Fatal("/quit did not close the surface")
	}
	if agent.closes != 1 {
		t.Fatalf("the agent was closed %d times", agent.closes)
	}
}
