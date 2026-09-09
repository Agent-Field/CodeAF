package tui3

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The small-UX bundle's acceptance tests: the shell highlighter, the
// what-changed line, the silence indicator, the notification, copy mode, the
// light ladder and the linear tier.
//
// Each one asserts the FACT the feature exists for rather than the shape of the
// code under it — the escape group a token lands in, the text of a line, the
// bytes that reach the terminal — because every one of these is a promise about
// what a person sees.

// sgr256 is one hue's 256-colour foreground sequence, which is what the pinned
// test palette paints with (see [newTestApp]).
func sgr256(h hue) string { return "\x1b[38;5;" + itoa(int(h.idx)) + "m" }

// ctrlKey is a control chord the shared [key] table does not spell.
func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

// ── 1. THE SHELL LEXER ──────────────────────────────────────────────────────

// Every token kind, on one command, in one pass. The command is chosen because
// it contains all eight: two verbs, a path, an operator run, a quoted string, a
// flag, a bare word and a comment.
func TestTheShellLexerFindsEveryGroup(t *testing.T) {
	const command = `cd /tmp && echo "hi" | grep -n x # note`
	want := []shellTok{
		{shellCommand, "cd"},
		{shellSpace, " "},
		{shellPath, "/tmp"},
		{shellSpace, " "},
		{shellOperator, "&&"},
		{shellSpace, " "},
		{shellCommand, "echo"},
		{shellSpace, " "},
		{shellString, `"hi"`},
		{shellSpace, " "},
		{shellOperator, "|"},
		{shellSpace, " "},
		{shellCommand, "grep"},
		{shellSpace, " "},
		{shellFlag, "-n"},
		{shellSpace, " "},
		{shellPlain, "x"},
		{shellSpace, " "},
		{shellComment, "# note"},
	}
	got := lexShell(command)
	if len(got) != len(want) {
		t.Fatalf("the lexer produced %d tokens, want %d:\n%#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token %d is %#v, want %#v", i, got[i], want[i])
		}
	}
	// The invariant every width on this surface depends on: the tokens rejoin
	// into exactly the line that went in.
	var rejoined strings.Builder
	for _, tok := range got {
		rejoined.WriteString(tok.text)
	}
	if rejoined.String() != command {
		t.Fatalf("the tokens rejoin as %q", rejoined.String())
	}
}

// A bash row is quiet telemetry: verbs and their objects remain readable while
// syntax recedes. No signal hue is spent on filling the command with colour.
func TestTheShellHighlightUsesOnlyReadingTiers(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	painted := pal.shell(`cd /tmp && echo "hi" | grep -n x # note`)

	for _, want := range []struct {
		what  string
		token string
		hue   hue
	}{
		{"the command", "cd", hueInk},
		{"the second command", "echo", hueInk},
		{"the string", `"hi"`, hueInk},
		{"the flag", "-n", hueDim},
		{"the operator", "&&", hueDim},
		{"the pipe", "|", hueDim},
		{"the comment", "# note", hueDim},
	} {
		if !strings.Contains(painted, sgr256(want.hue)+want.token) {
			t.Fatalf("%s (%q) is not painted in its hue:\n%q", want.what, want.token, painted)
		}
	}
	// The path is ink AND underlined — the one token that takes two attributes.
	if !strings.Contains(painted, "\x1b[4m"+sgr256(hueInk)+"/tmp") {
		t.Fatalf("the path is not underlined ink:\n%q", painted)
	}
	// A number is dim, and it is a number and not a flag.
	if !strings.Contains(pal.shell("sleep 30"), sgr256(hueDim)+"30") {
		t.Fatalf("a bare number is not dim: %q", pal.shell("sleep 30"))
	}
	for _, hue := range []hue{hueAccent, hueAdd, hueViolet, hueAsk} {
		if strings.Contains(painted, sgr256(hue)) {
			t.Fatalf("a command line took signal hue %v:\n%q", hue, painted)
		}
	}
}

// A terminal told to draw nothing draws nothing, underline included.
func TestTheShellHighlightIsPlainWithoutColour(t *testing.T) {
	pal := newPalette(tokens.NoColor, true)
	if got := pal.shell(`echo "hi" > /tmp/x`); got != `echo "hi" > /tmp/x` {
		t.Fatalf("NO_COLOR was styled anyway: %q", got)
	}
}

// ── 2. THE BASH EXPANSION ───────────────────────────────────────────────────

// Clicking a bash row shows the WHOLE command — every line of a multi-line one,
// nothing clipped — and then the output.
func TestOpeningABashRowShowsTheWholeCommand(t *testing.T) {
	const command = "set -euo pipefail\n" +
		"for f in internal/session/*.go; do\n" +
		"  grep -n 'argsLimit' \"$f\" || true\n" +
		"done\n" +
		"echo done"
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	a := toolApp(t, tokens.ANSI256, call("bash", string(args), "loop.go:12: argsLimit\n"))
	a.width = 100

	body := openFirst(t, a)
	for _, line := range strings.Split(command, "\n") {
		if !containsRow(body, strings.TrimSpace(line)) {
			t.Fatalf("the expansion dropped %q:\n%s", line, strings.Join(body, "\n"))
		}
	}
	if !containsRow(body, "loop.go:12: argsLimit") {
		t.Fatalf("the output is missing:\n%s", strings.Join(body, "\n"))
	}
	// The command leads and the output follows it, which is the reading order.
	if at(body, "set -euo pipefail") > at(body, "loop.go:12: argsLimit") {
		t.Fatalf("the output was drawn above the command:\n%s", strings.Join(body, "\n"))
	}
}

// A call still RUNNING shows its command too — that is what the row was opened
// for — and says it is running under it. An unopened one still shows nothing:
// a command that unfolded itself under every bash call would take the screen.
func TestARunningBashRowShowsItsCommandOnceOpened(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		announced("bash", "bash go test ./...", `{"command":"go test ./...\ngo vet ./..."}`),
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "run it")

	for _, line := range plainRows(a) {
		if strings.HasPrefix(line, railCont) {
			t.Fatalf("an unopened bash call previewed itself: %q", line)
		}
	}

	body := openFirst(t, a)
	for _, want := range []string{"go test ./...", "go vet ./...", "queued"} {
		if !containsRow(body, want) {
			t.Fatalf("the open row is missing %q:\n%s", want, strings.Join(body, "\n"))
		}
	}
}

// A line longer than the terminal WRAPS rather than losing its tail: the whole
// promise of the expansion is that nothing was cut.
func TestALongCommandWrapsInsteadOfTruncating(t *testing.T) {
	tail := strings.Repeat("x", 200)
	pal := newPalette(tokens.ANSI256, false)
	rows := shellRows(pal, "echo "+tail, 40)
	if len(rows) < 5 {
		t.Fatalf("a 205-cell command became %d rows of 40", len(rows))
	}
	var rejoined strings.Builder
	for _, r := range rows {
		rejoined.WriteString(plain(r))
	}
	if !strings.Contains(rejoined.String(), tail) {
		t.Fatalf("the wrap lost part of the command:\n%s", strings.Join(rows, "\n"))
	}
	if strings.Contains(rejoined.String(), glyphMore) {
		t.Fatal("the expansion truncated a command it was supposed to wrap")
	}
}

// ── 3. WHAT CHANGED ─────────────────────────────────────────────────────────

// A turn that wrote files ends with one line naming them and their diffstats.
func TestATurnThatTouchedFilesSaysWhatChanged(t *testing.T) {
	edit := editArgs(t, "internal/session/loop.go",
		[2]string{"a\nb\nc", "a\nB\nc\nd"})
	write, err := json.Marshal(map[string]string{
		"path": "internal/session/agent.go", "content": "one\ntwo\nthree\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	a := toolApp(t, tokens.ANSI256,
		call("edit", edit, "Successfully replaced 1 block(s)."),
		call("read", `{"path":"internal/session/tools.go"}`, "…"),
		call("bash", `{"command":"go build ./..."}`, ""),
		call("write", string(write), "Successfully wrote 14 bytes."),
	)

	line := findRow(t, a, "2 files")
	// The edit replaced one line and added one: +2 −1. The write laid down three.
	for _, want := range []string{"loop.go " + glyphAdd + "2 " + glyphDel + "1",
		"agent.go " + glyphAdd + "3 " + glyphDel + "0"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the changed line is missing %q: %q", want, line)
		}
	}
	// A read and a bash are not writes, and the line does not claim them.
	for _, never := range []string{"tools.go", "3 files"} {
		if strings.Contains(line, never) {
			t.Fatalf("the changed line claimed %q: %q", never, line)
		}
	}
}

// A turn that only read says nothing at all: a line reporting zero files is a
// line that has to be read to learn nothing.
func TestATurnThatChangedNothingSaysNothing(t *testing.T) {
	a := toolApp(t, tokens.ANSI256, call("read", `{"path":"loop.go"}`, "x"))
	for _, line := range plainRows(a) {
		if strings.Contains(line, "files ·") || strings.Contains(line, "1 file ·") {
			t.Fatalf("a read-only turn drew a changed line: %q", line)
		}
	}
}

// The cap: four files named, the rest counted.
func TestTheChangedLineCapsTheFilesItNames(t *testing.T) {
	stats := []fileStat{
		{"a/one.go", 1, 0}, {"b/two.go", 2, 1}, {"c/three.go", 3, 2},
		{"d/four.go", 4, 3}, {"e/five.go", 5, 4}, {"f/six.go", 6, 5},
	}
	word := changedWord(stats)
	if !strings.HasPrefix(word, "6 files · one.go") {
		t.Fatalf("the head is wrong: %q", word)
	}
	if !strings.HasSuffix(word, "· +2 more") {
		t.Fatalf("the overflow is not counted: %q", word)
	}
	if strings.Contains(word, "five.go") {
		t.Fatalf("the cap did not hold: %q", word)
	}
	if got := changedWord(stats[:1]); !strings.HasPrefix(got, "1 file · one.go") {
		t.Fatalf("one file is not singular: %q", got)
	}
}

// Two edits to one file are one entry with one sum, in the order the file was
// first touched.
func TestTheChangedLineSumsAFileTouchedTwice(t *testing.T) {
	first := editArgs(t, "loop.go", [2]string{"a", "A"})
	second := editArgs(t, "loop.go", [2]string{"b\nc", "B\nC\nD"})
	a := toolApp(t, tokens.ANSI256,
		call("edit", first, "ok"), call("edit", second, "ok"))

	line := findRow(t, a, "1 file")
	if !strings.Contains(line, "loop.go "+glyphAdd+"4 "+glyphDel+"3") {
		t.Fatalf("the two edits did not sum: %q", line)
	}
}

// ── 4. THE SILENCE INDICATOR ────────────────────────────────────────────────

// A turn that has said nothing for ten seconds says so, and one that has just
// spoken does not.
func TestALongSilenceSaysStillWorking(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.state = stateWorking

	a.lastDelta = time.Now()
	line, ok := a.ellipsis()
	if !ok {
		t.Fatal("a working turn drew no indicator")
	}
	if strings.Contains(plain(line), stillWorkingWord) {
		t.Fatalf("a turn that just spoke claimed a stall: %q", plain(line))
	}

	a.lastDelta = time.Now().Add(-(stillWorking + time.Second))
	line, ok = a.ellipsis()
	if !ok {
		t.Fatal("a silent turn drew no indicator")
	}
	if !strings.HasSuffix(plain(line), stillWorkingWord) {
		t.Fatalf("ten seconds of silence said nothing: %q", plain(line))
	}
	// It is the surface talking about itself, so it is dim.
	if !strings.Contains(line, sgr256(hueDim)) {
		t.Fatalf("the suffix is not dim: %q", line)
	}
}

// An idle surface has no indicator to add it to.
func TestAnIdleSurfaceNeverSaysStillWorking(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state = stateIdle
	a.lastDelta = time.Now().Add(-time.Hour)
	if line, ok := a.ellipsis(); ok {
		t.Fatalf("an idle surface drew %q", plain(line))
	}
}

// ── 5. THE NOTIFICATION ─────────────────────────────────────────────────────

// A turn that ends on an unfocused terminal sends OSC 777; one that ends in
// front of the person sends nothing.
func TestAFinishedTurnNotifiesOnlyWhenUnfocused(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.title = "porting the parser"

	if cmd := a.notifyDone(); cmd != nil {
		t.Fatal("a focused terminal was notified")
	}

	drive(t, a, tea.BlurMsg{})
	if a.focused || !a.seenFocus {
		t.Fatalf("blur was not recorded: focused=%v seen=%v", a.focused, a.seenFocus)
	}
	cmd := a.notifyDone()
	if cmd == nil {
		t.Fatal("an unfocused terminal was not notified")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("the notification is a %T, not a raw write", cmd())
	}
	seq, _ := raw.Msg.(string)
	if !strings.HasPrefix(seq, "\x1b]777;notify;"+product+";") {
		t.Fatalf("the sequence is not an OSC 777 notify: %q", seq)
	}
	if !strings.Contains(seq, "porting the parser · turn done") {
		t.Fatalf("the banner does not name the conversation: %q", seq)
	}
	// It ends with BEL because that is OSC's terminator — and it is NOT a bare
	// BEL: the bell was the fallback for a terminal that could not report focus,
	// and this one can (notify.go says why that path does not exist).
	if !strings.HasSuffix(seq, "\a") || seq == "\a" {
		t.Fatalf("the sequence is not BEL-terminated: %q", seq)
	}
	if strings.Count(seq, "\a") != 1 {
		t.Fatalf("the payload carries a stray BEL: %q", seq)
	}

	// Focus comes back, and the surface goes quiet again.
	drive(t, a, tea.FocusMsg{})
	if cmd := a.notifyDone(); cmd != nil {
		t.Fatal("a refocused terminal was notified anyway")
	}
}

// A title carrying the two characters OSC cannot hold does not break the
// sequence.
func TestTheNotificationSanitizesItsFields(t *testing.T) {
	seq := notifySeq("open;af", "a\x1b]b\ac")
	if strings.Count(seq, ";") != 3 { // 777; notify; title; body
		t.Fatalf("a semicolon leaked into a field: %q", seq)
	}
	if strings.Count(seq, "\a") != 1 || strings.Count(seq, "\x1b") != 1 {
		t.Fatalf("a terminator leaked into a field: %q", seq)
	}
}

// ── 6. COPY MODE ────────────────────────────────────────────────────────────

// copyApp is a surface with a known transcript, in copy mode.
func copyApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	for _, line := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		a.entries = append(a.entries, entry{kind: entryNote, text: line})
	}
	a.touch()
	drive(t, a, ctrlKey('b'))
	if !a.copy.on {
		t.Fatal("ctrl+b did not enter copy mode")
	}
	return a
}

// ctrl+b freezes, ↑ moves, esc leaves — and the status line says which of those
// is happening.
func TestCopyModeFreezesScrollsAndExits(t *testing.T) {
	a := copyApp(t)
	frozen := append([]string(nil), a.copy.rows...)

	word, _ := a.stateWord()
	if word != "COPY" {
		t.Fatalf("the status line says %q", word)
	}
	// The whole frame draws, and it says so where a person is already looking.
	screen := plain(frame(a))
	if !strings.Contains(screen, "COPY") {
		t.Fatalf("the frame does not say COPY:\n%s", screen)
	}
	if !strings.Contains(screen, "alpha") {
		t.Fatalf("the frozen conversation is not on screen:\n%s", screen)
	}

	// The conversation keeps going underneath and the frozen rows do not move.
	a.note("this arrived after the freeze")
	if len(a.copy.rows) != len(frozen) {
		t.Fatalf("the snapshot grew from %d to %d rows", len(frozen), len(a.copy.rows))
	}
	body, _ := a.bodyRows(a.width, a.viewHeight())
	for _, r := range body {
		if strings.Contains(plain(r.text), "after the freeze") {
			t.Fatal("the frozen viewport drew a row that arrived after it froze")
		}
	}

	at := a.copy.at
	drive(t, a, key("up"))
	if a.copy.at != at-1 {
		t.Fatalf("↑ moved the cursor from %d to %d", at, a.copy.at)
	}
	drive(t, a, key("down"))
	if a.copy.at != at {
		t.Fatalf("↓ did not come back: %d", a.copy.at)
	}
	// The cursor cannot walk off either end.
	for i := 0; i < len(a.copy.rows)+5; i++ {
		a.copyScroll(-1)
	}
	if a.copy.at != 0 {
		t.Fatalf("the cursor walked past the top: %d", a.copy.at)
	}

	drive(t, a, key("esc"))
	if a.copy.on {
		t.Fatal("esc did not leave copy mode")
	}
	if !a.stick {
		t.Fatal("leaving copy mode did not rejoin the live edge")
	}
	if word, _ := a.stateWord(); word == "COPY" {
		t.Fatal("the status line still says COPY")
	}
}

// v marks, y yanks the span, and what reaches the terminal is an OSC 52 write
// carrying the plain text of the marked rows.
func TestCopyModeYanksTheMarkedSpan(t *testing.T) {
	a := copyApp(t)
	// Park on a row whose text is known, then mark two rows.
	a.copy.at = rowWith(t, a, "charlie")
	drive(t, a, key("v"))
	if a.copy.mark < 0 {
		t.Fatal("v did not drop a mark")
	}
	drive(t, a, key("up"))
	from, to := a.copySpan()
	if to-from != 1 {
		t.Fatalf("the span is %d rows", to-from+1)
	}
	if word, _ := a.stateWord(); word != "COPY · 2 lines" {
		t.Fatalf("the status line does not count the span: %q", word)
	}

	payload := yank(t, a)
	if payload != "  · bravo\n  · charlie" {
		t.Fatalf("the yank carried %q", payload)
	}
	if a.copy.mark >= 0 {
		t.Fatal("the mark survived the yank")
	}
	// v again with no mark set copies the cursor's line alone.
	a.copy.at = rowWith(t, a, "delta")
	if got := yank(t, a); got != "  · delta" {
		t.Fatalf("an unmarked yank carried %q", got)
	}
}

// Inside tmux the same write goes out wrapped in the passthrough, with every
// ESC doubled — the bare form is silently eaten there.
func TestTheYankTakesTheTmuxPassthroughInsideTmux(t *testing.T) {
	bare := osc52("hi", false)
	if want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi")) + "\a"; bare != want {
		t.Fatalf("the bare form is %q", bare)
	}
	wrapped := osc52("hi", true)
	if !strings.HasPrefix(wrapped, "\x1bPtmux;\x1b\x1b]52;c;") || !strings.HasSuffix(wrapped, "\x1b\\") {
		t.Fatalf("the tmux form is %q", wrapped)
	}

	for term, want := range map[string]bool{
		"tmux-256color": true, "screen-256color": true, "screen": true,
		"xterm-256color": false, "": false, "alacritty": false,
	} {
		if got := tmuxTerm(func(string) string { return term }); got != want {
			t.Fatalf("TERM=%q read as tmux=%v", term, got)
		}
	}
}

// yank presses y and returns the text the clipboard write carries.
func yank(t *testing.T, a *app) string {
	t.Helper()
	cmd, taken := a.copyKey(key("y"))
	if !taken || cmd == nil {
		t.Fatal("y did not yank")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("the yank is a %T, not a raw write", cmd())
	}
	seq, _ := raw.Msg.(string)
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("the payload is not base64: %q", seq)
	}
	return string(decoded)
}

// "a" takes the whole thing under the cursor rather than a range of lines a
// person had to count out, and what comes back is pasteable: the column the
// frame draws down the left of a block is the frame speaking, not the text.
func TestCopyModeTakesTheBlockUnderTheCursorAndYanksItClean(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	// THE CALL COMES BEFORE THE ANSWER, which is the order a turn actually runs
	// in and the order THE ANSWER HIERARCHY reads (hierarchy.go): prose with more
	// work under it in the same turn is narration and is drawn at the working
	// tier, so an answer written above its own tool call would be demoted here —
	// and this test is about copying the ANSWER's fence.
	a.entries = append(a.entries,
		entry{kind: entryUser, text: "how do I print?"},
		entry{kind: entryTool, tool: "read", text: "main.go", status: toolOK, open: true,
			detail: toolDetail{Output: "line one\nline two"}},
		entry{kind: entryAssistant, settled: true, text: "Use fmt:\n\n```go\nfmt.Println(\"hi\")\nif ok {\n\tprintln(1)\n}\n```\n\nThat is all."},
	)
	// Copying a result starts with that result on screen, so open both the
	// completed turn and its caption before freezing the copy view.
	a.openWorkfold(0)
	a.setCapOpen(a.conversation(), 1, true)
	a.touch()
	drive(t, a, ctrlKey('b'))

	// On a code row, "a" takes the fence — and only the fence, without the
	// hairline the renderer draws beside it.
	a.copy.at = rowWith(t, a, "println(1)")
	drive(t, a, key("a"))
	if got := yank(t, a); got != "fmt.Println(\"hi\")\nif ok {\n    println(1)\n}" {
		t.Fatalf("the code block came out as %q", got)
	}

	// Pressing it again on the same row widens to the answer the fence lives in,
	// which is the block the code row also belongs to.
	a.copy.at = rowWith(t, a, "println(1)")
	drive(t, a, key("a"))
	drive(t, a, key("a"))
	got := yank(t, a)
	// Flush at both ends: this is the turn's ANSWER, so it carries no work
	// gutter for the yank to have to strip (hierarchy.go).
	if !strings.HasPrefix(strings.TrimLeft(got, " "), "Use fmt:") || !strings.HasSuffix(got, "That is all.") {
		t.Fatalf("the second press did not widen to the answer: %q", got)
	}
	if strings.Contains(got, tokens.GlyphCodeGutter) {
		t.Fatalf("the answer carried the code hairline: %q", got)
	}

	// A tool's output is a block too, and its stem is chrome the same way.
	a.copy.at = rowWith(t, a, "line two")
	drive(t, a, key("a"))
	if got := yank(t, a); !strings.Contains(got, "line one\nline two") {
		t.Fatalf("the tool result came out as %q", got)
	} else if strings.Contains(got, "│") {
		t.Fatalf("the tool result carried its stem: %q", got)
	}

	// A blank belongs to nothing, so "a" there guesses at nothing.
	blank := -1
	for i, line := range a.copy.text {
		if strings.TrimSpace(line) == "" && a.copy.owner[i] < 0 {
			blank = i
			break
		}
	}
	if blank < 0 {
		t.Fatal("the layout emitted no blank between the blocks")
	}
	a.copy.at, a.copy.mark = blank, -1
	drive(t, a, key("a"))
	if a.copy.mark >= 0 {
		t.Fatal("a blank row was taken as a block")
	}
}

// A waiting sign-in is the one thing here a person needs somewhere else, so a
// press on the card copies the link whole — and the card says it did.
func TestPressingAWaitingSignInCopiesItsLink(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	link := "https://accounts.example.com/o/oauth2/auth?client=abc&scope=mail"
	a.entries = append(a.entries, entry{kind: entryConnect, conn: &connectCard{
		service: "google", name: "Google", link: link, state: connectWaiting,
	}})
	a.touch()

	cmd, took := a.connectLinkPress(len(a.entries) - 1)
	if !took || cmd == nil {
		t.Fatal("the card did not take the press")
	}
	raw, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("the copy is a %T, not a raw write", cmd())
	}
	seq, _ := raw.Msg.(string)
	body := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b]52;c;"), "\a")
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		t.Fatalf("the payload is not base64: %q", seq)
	}
	// WHOLE, and not as the frame wrapped it across two indented rows.
	if string(decoded) != link {
		t.Fatalf("the copy carried %q", decoded)
	}
	if !strings.Contains(plain(frame(a)), "copied") {
		t.Fatalf("the card did not say it copied:\n%s", plain(frame(a)))
	}

	// A settled card has no link to hand anybody, and says nothing about one.
	a.entries[len(a.entries)-1].conn = &connectCard{name: "Google", state: connectConnected}
	a.touch()
	if _, took := a.connectLinkPress(len(a.entries) - 1); took {
		t.Fatal("a finished sign-in took a press meant for a link it does not have")
	}
}

// ctrl+s hands the pointer to the terminal so that an ordinary drag selects
// text, says so on the one line that names live keys, and gives nothing away
// permanently: the person's next keystroke takes it back and does its own job
// on the way.
func TestTheSelectKeyHandsThePointerOverAndTheNextKeyTakesItBack(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.mouse = true
	// Wide enough for the legend to carry its hint slot at all: under hudTight
	// the cells are worth more to the path (render.go's [app.legendRight]).
	a.width = 80
	a.touch()
	if a.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("the surface did not start out holding the pointer")
	}

	drive(t, a, key(selectKey))
	if !a.released {
		t.Fatal("ctrl+s did not hand the pointer over")
	}
	if a.View().MouseMode != tea.MouseModeNone {
		t.Fatal("the frame still asks for the pointer")
	}
	if !strings.Contains(plain(frame(a)), "drag to select") {
		t.Fatalf("nothing on the frame says the pointer is theirs:\n%s", plain(frame(a)))
	}

	// Pressing it again is the plain toggle it looks like, rather than a second
	// handover of something already handed over.
	drive(t, a, key(selectKey))
	if a.released {
		t.Fatal("ctrl+s twice did not put it back")
	}

	// And any other key ends it AND still does what it always does — nothing is
	// swallowed by the exit, because there is no exit.
	drive(t, a, key(selectKey))
	drive(t, a, key("x"))
	if a.released {
		t.Fatal("a keystroke did not take the pointer back")
	}
	if a.input.String() != "x" {
		t.Fatalf("the keystroke that ended it was eaten: draft is %q", a.input.String())
	}

	// With the pointer already the terminal's there is nothing to hand over, and
	// the key says nothing rather than claiming it did something.
	a.mouse = false
	drive(t, a, key(selectKey))
	if a.released {
		t.Fatal("ctrl+s handed over a pointer the surface never had")
	}
}

// /select is the same act, typed — and typed deliberately, so the case with
// nothing to do answers instead of going quiet.
func TestTheSelectCommandAnswersWhenThereIsNothingToHandOver(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.mouse = true
	_ = a.slash("/select")
	if !a.released {
		t.Fatal("/select did not hand the pointer over")
	}

	a = newTestApp(&fakeAgent{model: "m"})
	a.mouse = false
	_ = a.slash("/select")
	if a.released {
		t.Fatal("/select handed over a pointer the surface never had")
	}
	if !strings.Contains(plain(frame(a)), "already has the pointer") {
		t.Fatalf("/select said nothing:\n%s", plain(frame(a)))
	}
}

// rowWith is the frozen row holding a word.
func rowWith(t *testing.T, a *app, word string) int {
	t.Helper()
	for i, line := range a.copy.text {
		if strings.Contains(line, word) {
			return i
		}
	}
	t.Fatalf("no frozen row holds %q:\n%s", word, strings.Join(a.copy.text, "\n"))
	return -1
}

// ── 7. THE LIGHT LADDER ─────────────────────────────────────────────────────

// The authored values, and the one law that has to hold on the rung where hues
// are rounded: no two roles resolve to the same 256-colour index.
func TestTheLightLadderIsAuthoredAndDistinct(t *testing.T) {
	for _, want := range []struct {
		name string
		got  hue
		hex  string
	}{
		{"ink", lightInk, "#3B4252"},
		{"accent", lightAccent, "#5E81AC"},
		{"dim", lightDim, "#9AA3B2"},
		{"add", lightAdd, "#7BA23F"},
		{"del", lightDel, "#B55B64"},
		{"violet", hueViolet, "#8F6FA8"},
		{"hover", lightCursor, "#E5E9F0"},
	} {
		r, g, b, ok := parseHex(want.hex)
		if !ok {
			t.Fatalf("%s: %q is malformed", want.name, want.hex)
		}
		if want.got.r != r || want.got.g != g || want.got.b != b {
			t.Fatalf("%s is #%02X%02X%02X, want %s",
				want.name, want.got.r, want.got.g, want.got.b, want.hex)
		}
	}

	seen := map[uint8]string{}
	for name, h := range map[string]hue{
		"ink": lightInk, "accent": lightAccent, "muted": lightMuted, "dim": lightDim,
		"add": lightAdd, "del": lightDel, "bad": lightBad, "ask": lightAsk,
		"warn": lightWarn, "data": lightData, "hover": lightCursor, "violet": hueViolet,
		// The streaming step is a role on this ladder like any other, and it owes
		// the same rounding check — see [lightLive], and settle_test.go for what it
		// is for.
		"live": lightLive,
	} {
		if other, clash := seen[h.idx]; clash {
			t.Fatalf("%s and %s both resolve to xterm-256 %d", name, other, h.idx)
		}
		seen[h.idx] = name
	}
	// The question hue must not collide with the operator violet on EITHER
	// ladder — that is the whole reason there are two violets (styles.go).
	if hueAsk.idx == hueViolet.idx || lightAsk.idx == hueViolet.idx {
		t.Fatal("the question hue and the operator violet resolve to one index")
	}
}

// The seam: a row's value picks a ladder, and an unset row asks the terminal.
func TestTheThemeSeamPicksTheLadder(t *testing.T) {
	for row, want := range map[string]theme{
		"light": themeLight, "LIGHT": themeLight, " dark ": themeDark,
		"": themeAuto, "auto": themeAuto, "nonsense": themeAuto,
	} {
		if got := themeFromRow(row); got != want {
			t.Fatalf("themeFromRow(%q) is %v, want %v", row, got, want)
		}
	}

	env := func(value string) func(string) string {
		return func(key string) string {
			if key == "COLORFGBG" {
				return value
			}
			return ""
		}
	}
	for value, want := range map[string]theme{
		"15;0": themeDark, "0;15": themeLight, "15;default;0": themeDark,
		"": themeDark, "nonsense": themeDark, "0;7": themeLight, "7;8": themeDark,
	} {
		if got := detectTheme(env(value)); got != want {
			t.Fatalf("COLORFGBG=%q read as %v, want %v", value, got, want)
		}
	}

	// And the palette actually paints from the ladder it was handed.
	light := newThemedPalette(tokens.ANSI256, false, themeLight, nil)
	if !strings.Contains(light.ink("x"), sgr256(lightInk)) {
		t.Fatalf("the light palette paints ink as %q", light.ink("x"))
	}
	dark := newThemedPalette(tokens.ANSI256, false, themeDark, env("0;15"))
	if !strings.Contains(dark.ink("x"), sgr256(hueInk)) {
		t.Fatalf("a pinned dark palette followed the terminal: %q", dark.ink("x"))
	}
	// The light fade lifts toward the page rather than sinking toward black.
	if lightRamp.fade[0].r <= lightRamp.fade[2].r {
		t.Fatal("the light thinking window fades the wrong way")
	}
}

// ── 8. THE LINEAR TIER ──────────────────────────────────────────────────────

// Linear mode draws the same facts with no motion, no pointer and no glyphs.
func TestLinearModeRendersPlain(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash go test", Args: `{"command":"go test ./..."}`},
	}}}
	a := newApp(t.Context(), Options{Agent: agent, Workspace: "/tmp/lab", Linear: true})
	a.width, a.height = 60, 20
	a.pal.profile = tokens.ANSI256
	a.entries = nil
	a.welcome = welcome{spent: true}
	a.touch()
	typeLine(t, a, "run it")
	showLiveWork(t, a)

	body := strings.Join(plainRows(a), "\n")
	for _, glyph := range []string{railMid, railLast, railCont, glyphYou, glyphTool} {
		if strings.Contains(body, glyph) {
			t.Fatalf("linear mode drew %q:\n%s", glyph, body)
		}
	}
	if !strings.Contains(body, railASCII) {
		t.Fatalf("linear mode drew no ASCII rail:\n%s", body)
	}
	if !strings.Contains(body, glyphYouASCII+"run it") {
		t.Fatalf("the person's own row has no ASCII marker:\n%s", body)
	}

	// NO ANIMATION. The running call's mark is the same at frame 0 and frame 99,
	// where the braille spinner would have turned twice.
	first := toolLineOf(t, a)
	a.paints += 99
	a.touch()
	if second := toolLineOf(t, a); second != first {
		t.Fatalf("a linear tool line animated:\n%q\n%q", first, second)
	}
	if !strings.Contains(first, glyphRunASCII) {
		t.Fatalf("a running call has no ASCII mark: %q", first)
	}
	// The pulse is still, too.
	if a.pulse() != ellipsisFrames[len(ellipsisFrames)-1] {
		t.Fatalf("the linear pulse animates: %q", a.pulse())
	}

	// NO POINTER. The hover paint is refused at both ends: the palette draws no
	// background, and the layout pass asks for none.
	a.hot = hoverAt{kind: hoverEntry, entry: 0}
	if got := a.pal.cursor("x", 10); got != "x" {
		t.Fatalf("the linear palette painted a hover: %q", got)
	}
	if a.isHot(row{entry: 0}) {
		t.Fatal("the linear layout pass found a hot row")
	}
	// And the thinking window's gradient collapses to the tier it fades within.
	if a.pal.fade("x", 0) != a.pal.dim("x") {
		t.Fatalf("the linear window drew a gradient: %q", a.pal.fade("x", 0))
	}
}

// ── 9. SETTINGS FIDELITY: EVERY MODEL ROW IS A MODEL CHOICE ─────────────────
//
// Three rows asked "which model" with a text box: the two tiers and the vision
// slot. A text box is the wrong widget for a question this panel can already
// answer — it makes a person type an id from memory in front of a catalog that
// knows every one of them — and the fix is the widget, not a fourth list.

// modalityCatalog is one row of every shape the modality law has to separate:
// a chat model that can also see, a transcription model (text OUT, sound IN —
// the family the output-only rule let through), a chat model that reads text
// and nothing else, a drawing model that captions, and three silent rows whose
// only witness is their name.
var modalityCatalog = []Model{
	{
		ID: "anthropic/claude-sonnet-4.5", ContextLength: 200_000,
		PromptPrice: 3e-6, CompletionPrice: 1.5e-5, ArenaElo: 1300,
		Output: []string{"text"}, Input: []string{"text", "image"},
	},
	{ID: "openai/whisper-large-v3", Output: []string{"text"}, Input: []string{"audio"}},
	{ID: "vendor/blind-chat", ContextLength: 32_000, Output: []string{"text"}, Input: []string{"text"}},
	{
		ID:     "google/gemini-3.1-flash-image",
		Output: []string{"image", "text"}, Input: []string{"text", "image"},
	},
	{ID: "moonshotai/kimi-k3", ContextLength: 256_000},
	{ID: "openai/gpt-4o-transcribe-audio"},
	{ID: "vendor/text-embedding-3"},
}

// THE TIER ROWS AND THE VISION ROW OPEN THE PICKER, and enter writes the id
// through the registry — the same road every other slot takes.
func TestTheTierRowsAndTheVisionRowAreAnsweredByThePicker(t *testing.T) {
	a, dir := sheetApp(t)
	a.models = func() []Model { return modalityCatalog }
	a.openSettings()

	// The classes sit on the Providers tab, with the model they answer under
	// (settings.go's [modelsSection]).
	toProviders(t, a)
	for _, row := range []string{config.KeyTierLowModel, config.KeyTierHighModel} {
		cursorTo(t, a, row)
		if got := a.sheet.items[a.sheet.cursor].meta.widget; got != widgetSelect {
			t.Fatalf("%s is answered by widget %v, want the picker", row, got)
		}
		drive(t, a, key("enter"))
		if a.sheet.sel == nil {
			t.Fatalf("%s did not open a picker", row)
		}
		// It is THE picker: the rows carry what /model's rows carry.
		if !strings.Contains(plain(frame(a)), "$3/$15 per M · 200k · elo 1300") {
			t.Fatalf("%s opened a plainer list:\n%s", row, plain(frame(a)))
		}
		drive(t, a, key("esc"))
	}

	// And the choice lands in the profile, not just on the screen.
	cursorTo(t, a, config.KeyTierLowModel)
	drive(t, a, key("enter"))
	for _, r := range "blind" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, key("enter"))
	if got := config.TierModelAt(dir, config.ModelTierLow); got != "vendor/blind-chat" {
		t.Fatalf("the small-work row reads %q after the picker chose", got)
	}

	// The looking row is a picker too, further down the same tab.
	cursorTo(t, a, config.KeyVisionModel)
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("the looking row did not open a picker")
	}
}

// EACH SLOT'S PICKER ANSWERS THAT SLOT'S QUESTION. The conversation rows offer
// models you can talk to; the looking row offers models that can SEE, which is
// a different list drawn by the same component through one predicate.
func TestEachSlotFiltersTheModelsByWhatItNeeds(t *testing.T) {
	a, _ := sheetApp(t)
	a.models = func() []Model { return modalityCatalog }
	a.openSettings()
	toProviders(t, a)

	cursorTo(t, a, config.KeyTierHighModel)
	drive(t, a, key("enter"))
	chat := []string{"anthropic/claude-sonnet-4.5", "vendor/blind-chat", "moonshotai/kimi-k3"}
	if got := pickedIDs(a.sheet.sel); strings.Join(got, ",") != strings.Join(chat, ",") {
		t.Fatalf("a class row offers %v, want the models you can talk to %v", got, chat)
	}
	drive(t, a, key("esc"))

	cursorTo(t, a, config.KeyVisionModel)
	drive(t, a, key("enter"))
	// The slot is an inspection proxy — a picture in, a sentence back — so the
	// row has to SEE and to ANSWER. Sonnet publishes an image input and text out;
	// blind-chat publishes an input list WITHOUT a picture in it and is gone;
	// gemini reads pictures and answers in pictures, so it is gone from here too,
	// exactly as it is gone from the tier rows; the transcriber and the embedder
	// are silent and read by their names, which say they do not talk.
	//
	// AND THE SILENT ROW NO NAME MARKS IS GONE TOO, which is the one silence law
	// (docs/MULTIMODAL.md Decision 6): an unpublished modality list means
	// text-in/text-out and nothing more, so sight is never assumed. It used to
	// fall through here and be refused by the gate that actually sends the
	// photo — a list that offers what the gate will reject.
	vision := []string{"anthropic/claude-sonnet-4.5"}
	if got := pickedIDs(a.sheet.sel); strings.Join(got, ",") != strings.Join(vision, ",") {
		t.Fatalf("the looking row offers %v, want the models that see %v", got, vision)
	}
	if a.sheet.sel.keep == nil {
		t.Fatal("the slot opened its picker without a question")
	}
}

// The two predicates as a table: what a row PUBLISHES decides, on each side
// separately, and only a silent side is read by the name.
func TestTheModalityPredicates(t *testing.T) {
	for _, c := range []struct {
		model      Model
		chat, sees bool
	}{
		// Published, both sides.
		{Model{ID: "anthropic/claude-sonnet-4.5", Output: []string{"text"}, Input: []string{"text", "image"}}, true, true},
		{Model{ID: "vendor/blind-chat", Output: []string{"text"}, Input: []string{"text"}}, true, false},
		// The transcription family: text out, sound in. The output law alone
		// kept it, which is the defect this side closes.
		{Model{ID: "openai/whisper-large-v3", Output: []string{"text"}, Input: []string{"audio"}}, false, false},
		{Model{ID: "google/gemini-3.1-flash-image", Output: []string{"image", "text"}, Input: []string{"text", "image"}}, false, true},
		// A PUBLISHED LIST BEATS THE NAME on both sides: a model called "audio"
		// that says it reads and writes text is a chat model.
		{Model{ID: "vendor/audio-critic", Output: []string{"text"}, Input: []string{"text"}}, true, false},
		// Silence, read by the id — the wider vocabulary, since a silent row has
		// no other witness left.
		//
		// THE SIGHT COLUMN IS FALSE ALL THE WAY DOWN, and that is the one
		// silence law: an unpublished modality list means text-in/text-out and
		// NOTHING MORE, so no silent row is assumed to see. Only a name that
		// says sight in so many words — the vl and vision marks — earns it back.
		{Model{ID: "moonshotai/kimi-k3"}, true, false},
		{Model{ID: "qwen/qwen3.5-vl-32b-instruct"}, true, true},
		{Model{ID: "vendor/vision-8b"}, true, true},
		{Model{ID: "openai/gpt-4o-transcribe-audio"}, false, false},
		{Model{ID: "vendor/text-embedding-3"}, false, false},
		{Model{ID: "elevenlabs/voice-v3"}, false, false},
		{Model{ID: "openai/whisper-1"}, false, false},
		{Model{ID: "google/lyria-3-preview"}, false, false},
		{Model{ID: "vendor/music-gen"}, false, false},
		{Model{ID: "bytedance/seedance-video-pro"}, false, false},
		{Model{ID: "vendor/rerank-2"}, false, false},
		{Model{ID: "openai/omni-moderation-latest"}, false, false},
		{Model{ID: "google/imagen-4"}, false, false},
		{Model{ID: "openai/gpt-4o-mini-tts"}, false, false},
		{Model{ID: "openai/sora-2"}, false, false},
		{Model{ID: "google/veo-3"}, false, false},
		{Model{ID: "openai/dalle-3"}, false, false},
		// The marks are WHOLE WORDS of the id and never substrings, so a chat
		// model whose name merely carries the letters survives.
		{Model{ID: "vendor/videographer-8b"}, true, false},
		{Model{ID: "vendor/audiophile"}, true, false},
	} {
		if got := chatModel(c.model); got != c.chat {
			t.Fatalf("chatModel(%q, in=%v out=%v) = %v, want %v",
				c.model.ID, c.model.Input, c.model.Output, got, c.chat)
		}
		if got := seesImages(c.model); got != c.sees {
			t.Fatalf("seesImages(%q, in=%v) = %v, want %v",
				c.model.ID, c.model.Input, got, c.sees)
		}
	}
}

// /model is a slot like any other and passes the same chat predicate, so a
// transcription model is no more offered there than in the panel.
func TestTheModelOverlayAsksTheChatQuestion(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "vendor/blind-chat"}, modalityCatalog)
	typeLine(t, a, "/model")
	want := []string{"anthropic/claude-sonnet-4.5", "vendor/blind-chat", "moonshotai/kimi-k3"}
	if got := pickerIDs(a); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("/model offers %v, want %v", got, want)
	}
}

// ── 10. THE WHOLE-ROW HIGHLIGHT ─────────────────────────────────────────────

// THE EMPHASIZED ROW IS ONE GROUND, LEAD TO NOTE, ACROSS THE WHOLE LINE — and
// the note is inside it rather than dim underneath it.
//
// It also pins WHICH STEP each of a list's three states takes, which is the
// question this whole surface was getting backwards: the keyboard cursor and the
// pointer are one fact reached by two hands and share the cursor step, and the
// row this terminal is actually IN — the chosen thing, still chosen when nobody
// is touching the list — takes the step above them.
func TestTheEmphasizedOverlayRowIsOneGroundAcrossTheLine(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	const width = 48
	const note = "128k · elo 1200"
	cursor := "\x1b[48;5;" + itoa(int(hueCursor.idx)) + "m"
	chosen := "\x1b[48;5;" + itoa(int(hueSelected.idx)) + "m"

	line := overlayRow("openai/gpt-4.1-mini", note, true, false, false, width, pal)
	if !strings.HasPrefix(line, cursor) || !strings.HasSuffix(line, "\x1b[49m") {
		t.Fatalf("the cursor's row is not one ground:\n%q", line)
	}
	// The whole line: the ground is opened once, closed once, and everything the
	// row says is between them.
	inside := strings.TrimSuffix(strings.TrimPrefix(line, cursor), "\x1b[49m")
	if strings.Contains(inside, "\x1b[49m") {
		t.Fatalf("the ground is broken up mid-row:\n%q", line)
	}
	if !strings.Contains(inside, pal.ink(note)) {
		t.Fatalf("the note is not painted inside the ground:\n%q", line)
	}
	if strings.Contains(line, pal.dim(note)) {
		t.Fatalf("the note stayed dim inside the ground:\n%q", line)
	}
	if got := ansi.StringWidth(plain(line)); got != width {
		t.Fatalf("the ground is %d cells wide, want the whole %d", got, width)
	}

	// CURSOR AND HOVER ARE ONE STEP, NOT TWO. The row a person is on does not
	// change appearance depending on which hand they used to get there; what
	// tells the two apart is the lead, `›` against `·`, and not the rung.
	hovered := overlayRow("openai/gpt-4.1-mini", note, false, false, true, width, pal)
	if !strings.HasPrefix(hovered, cursor) {
		t.Fatalf("the pointer's row is not on the cursor step:\n%q", hovered)
	}
	if strings.Contains(hovered, chosen) {
		t.Fatalf("the pointer's row reached the chosen step:\n%q", hovered)
	}
	if !strings.Contains(line, pal.accent("› ")) {
		t.Fatalf("the cursor's row does not lead with its mark in the accent:\n%q", line)
	}
	if !strings.Contains(hovered, pal.accent("· ")) {
		t.Fatalf("the pointer's row does not lead with its mark in the accent:\n%q", hovered)
	}

	// THE CHOSEN ROW TAKES THE STEP ABOVE BOTH OF THEM, and keeps its accent
	// label inside it. This is the row the ladder's selected step exists for, and
	// it wore no ground at all until the language was adopted.
	marked := overlayRow("openai/gpt-4.1-mini", note, false, true, false, width, pal)
	if !strings.HasPrefix(marked, chosen) {
		t.Fatalf("the row this terminal is in wears no ground:\n%q", marked)
	}
	if !strings.Contains(marked, pal.accent("openai/gpt-4.1-mini")) {
		t.Fatalf("the marked row lost its accent to the ground:\n%q", marked)
	}
	if hueCursor.idx == hueSelected.idx {
		t.Fatal("the cursor step and the chosen step resolve to one colour")
	}
	// A row that is both is the cursor standing on the conversation you are in.
	// The LOUDER step wins, so that row never gets quieter for being arrived at,
	// and the cursor is still said — on the lead.
	both := overlayRow("openai/gpt-4.1-mini", note, true, true, false, width, pal)
	if !strings.HasPrefix(both, chosen) {
		t.Fatalf("the cursor painted over the row you are in:\n%q", both)
	}
	if !strings.Contains(both, pal.accent("› ")) {
		t.Fatalf("the cursor lost its mark on the row you are in:\n%q", both)
	}
}

// ── 11. THE COUNT-UP CLOCK ──────────────────────────────────────────────────

// A RUNNING CALL SAYS HOW LONG IT HAS BEEN RUNNING, and stops saying it the
// moment it is done.
func TestARunningCallCountsUpAndStopsWhenItFinishes(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash", Args: `{"command":"go test ./..."}`},
	}}}
	a := newTestApp(agent)
	base := time.Now()
	a.clock = func() time.Time { return base }
	typeLine(t, a, "run the tests")
	showLiveWork(t, a)

	at := firstTool(t, a)
	if a.entries[at].began != base {
		t.Fatalf("the call's clock started at %v, want the begin event's own moment", a.entries[at].began)
	}
	// Under a second there is nothing worth saying.
	if got := a.countUp(&a.entries[at]); got != "" {
		t.Fatalf("a call that just began drew %q", got)
	}

	// Sixty-five seconds later, on the frame clock that was already turning the
	// spinner — no ticker of its own.
	a.clock = func() time.Time { return base.Add(65 * time.Second) }
	a.touch()
	line := toolLineOf(t, a)
	if !strings.Contains(line, "1m 5s") {
		t.Fatalf("a call 65s old does not say so: %q", line)
	}
	// The spinner is still there beside it: the clock joined the row, it did not
	// take the spinner's place.
	if !strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the count-up displaced the spinner: %q", line)
	}
	// And the open row says it in words.
	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, "running · 1m 5s") {
		t.Fatalf("the open call does not carry the clock:\n%s", body)
	}

	// A TURN THAT ENDED WITH THE CALL UNRESOLVED stops the clock as well — the
	// same rule that freezes the spinner there. A number still climbing on an
	// abandoned call is the surface claiming work it cannot see is alive.
	a.state = stateInterrupted
	if got := a.countUp(&a.entries[at]); got != "" {
		t.Fatalf("an abandoned call is still counting: %q", got)
	}
	a.state = stateWorking

	// IT STOPS AT COMPLETION. The finished row has its own figure, said the
	// finished way, and only one of the two is ever on a line.
	a.entries[at].status = toolOK
	a.entries[at].ended = base.Add(65 * time.Second)
	a.touch()
	if got := a.countUp(&a.entries[at]); got != "" {
		t.Fatalf("a finished call is still counting: %q", got)
	}
	done := toolLineOf(t, a)
	if strings.Contains(done, "1m 5s") || !strings.Contains(done, "1m05s") {
		t.Fatalf("the finished line is not the finished figure: %q", done)
	}
}

// The scale, spelled the way a person says a duration out loud.
func TestCountUpWordSpellsEveryScale(t *testing.T) {
	for _, c := range []struct {
		took time.Duration
		want string
	}{
		{0, ""},
		{999 * time.Millisecond, ""},
		{time.Second, "1s"},
		{12 * time.Second, "12s"},
		{59 * time.Second, "59s"},
		{64 * time.Second, "1m 4s"},
		{65 * time.Second, "1m 5s"},
		{750 * time.Second, "12m 30s"},
		{59*time.Minute + 59*time.Second, "59m 59s"},
		{2*time.Hour + 5*time.Minute, "2h 5m"},
	} {
		if got := countUpWord(c.took); got != c.want {
			t.Fatalf("countUpWord(%v) = %q, want %q", c.took, got, c.want)
		}
	}

	// A queued call has no clock: nothing has started, so there is nothing to
	// count — the same reason it draws no spinner.
	a := newTestApp(&fakeAgent{model: "m"})
	waiting := entry{status: toolQueued, began: time.Now().Add(-time.Minute)}
	if got := a.countUp(&waiting); got != "" {
		t.Fatalf("a queued call counted %q", got)
	}
}

// A BOUNDED CALL COUNTS DOWN. Up to ten seconds out the row states the bound
// beside the age; inside them it says what is left, and the remainder — and
// only the remainder — takes the warning hue and then the failure one.
func TestABoundedCallCountsDownAndEscalates(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state = stateWorking
	// This test isolates the command's own timeout law; H11 below covers the
	// earlier session clock.
	a.bashBackgroundAfter = 0
	base := time.Now()

	// A sixty-second bash, read at four moments of its life.
	bounded := entry{
		kind: entryTool, tool: "bash", status: toolRunning, began: base,
		detail: toolDetail{Args: `{"command":"go test ./...","timeout":60}`},
	}
	for _, c := range []struct {
		at      time.Duration
		want    string
		wantInk string
		which   string
	}{
		{
			at: 20 * time.Second, want: "20s / 1m",
			wantInk: a.pal.dim("20s / 1m"), which: "the bound, stated in dim",
		},
		{
			at: 51 * time.Second, want: "51s · 9s left",
			wantInk: a.pal.dim("51s · ") + a.pal.warn("9s left"), which: "the warning",
		},
		{
			at: 56 * time.Second, want: "56s · 4s left",
			wantInk: a.pal.dim("56s · ") + a.pal.bad("4s left"), which: "the failure hue",
		},
		{
			at: 61 * time.Second, want: "1m 1s · 0s left",
			wantInk: a.pal.dim("1m 1s · ") + a.pal.bad("0s left"), which: "already overdue",
		},
	} {
		a.clock = func() time.Time { return base.Add(c.at) }
		got, painted := a.countClock(&bounded)
		if got != c.want {
			t.Fatalf("at %v the clock reads %q, want %q", c.at, got, c.want)
		}
		if painted != c.wantInk {
			t.Fatalf("at %v the clock is not %s:\n got %q\nwant %q", c.at, c.which, painted, c.wantInk)
		}
	}

	// NO TIMEOUT, NO CHROME: an unbounded tool carries its age and nothing else,
	// because nothing is going to happen to it at any particular moment.
	a.clock = func() time.Time { return base.Add(20 * time.Second) }
	unbounded := entry{
		kind: entryTool, tool: "read", status: toolRunning, began: base,
		detail: toolDetail{Args: `{"path":"loop.go"}`},
	}
	if got, _ := a.countClock(&unbounded); got != "20s" {
		t.Fatalf("an unbounded call drew a bound: %q", got)
	}
	// And neither does a background bash: it is a job, and no clock runs on it.
	job := entry{
		kind: entryTool, tool: "bash", status: toolRunning, began: base,
		detail: toolDetail{Args: `{"command":"npm run dev","background":true}`},
	}
	if got, _ := a.countClock(&job); got != "20s" {
		t.Fatalf("a background job counted down: %q", got)
	}
}

// H11: the row counts against the earlier session clock when it is on, and the
// command's timeout law unchanged when that clock is set to zero.
//
// THE BOUND ON THE ROW IS THE BOUND THE COMMAND DIES ON. A weak model that
// spells its optional arguments out — `"timeout": null` — is asking for nothing,
// and the session writes its ceiling over it (internal/session's withTimeoutLaw
// and [session.BashCeilingSeconds]). The row has to count down against that
// same figure: a bound drawn here that nothing was going to enforce is the one
// number on this line a person cannot check for themselves. The figure is READ
// from the constant rather than typed here, because a test that spelled it out
// was the second place the number lived, and it drifted the day the ceiling
// moved.
func TestTheRowCountsDownAgainstTheBoundTheSessionActuallyArmed(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.state = stateWorking
	base := time.Now()
	a.clock = func() time.Time { return base.Add(20 * time.Second) }
	a.bashBackgroundAfter = 0
	ceiling := "20s / " + countUpWord(session.BashCeilingSeconds*time.Second)
	for _, args := range []string{
		`{"command":"cd work"}`,
		`{"command":"cd work","timeout":null}`,
		`{"command":"cd work","timeout":0}`,
		`{"command":"cd work","timeout":-5}`,
	} {
		row := entry{
			kind: entryTool, tool: "bash", status: toolRunning, began: base,
			detail: toolDetail{Args: args},
		}
		if got, _ := a.countClock(&row); got != ceiling {
			t.Fatalf("%s draws %q, want the session's ceiling %q beside the age", args, got, ceiling)
		}
	}
	// And a figure the model really did ask for is still its own, clamped at the
	// session's cap.
	clamped := entry{
		kind: entryTool, tool: "bash", status: toolRunning, began: base,
		detail: toolDetail{Args: `{"command":"go test ./...","timeout":` + itoa(session.BashCeilingSeconds+300) + `}`},
	}
	if got, _ := a.countClock(&clamped); got != ceiling {
		t.Fatalf("an ask above the ceiling draws %q, want the ceiling %q", got, ceiling)
	}

	// With the shipping-shaped threshold armed, that earlier bound is what the
	// row states — never the ten-minute timeout it will not reach in foreground.
	a.bashBackgroundAfter = 30
	a.clock = func() time.Time { return base.Add(15 * time.Second) }
	threshold := entry{
		kind: entryTool, tool: "bash", status: toolRunning, began: base,
		detail: toolDetail{Args: `{"command":"go test ./...","timeout":600}`},
	}
	if got, _ := a.countClock(&threshold); got != "15s / 30s" {
		t.Fatalf("the 30-second session clock draws %q, want %q", got, "15s / 30s")
	}
}

// A ROW MEASURES ITS OWN CALL. The calls of one batch run together and their
// results are delivered together, after the last of them returns — so a `cd`
// that took a tenth of a second used to sit under a climbing clock until the
// build beside it finished, and then wrote the build's minutes onto its own
// line. The call's own finish is its own event (session.EventToolFinished) and
// it stops that row's clock where the call stopped.
func TestARowsClockIsItsOwnCallsAndNotItsSlowestSiblings(t *testing.T) {
	quick := `{"command":"cd work"}`
	slow := `{"command":"go build ./..."}`
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash cd work", Args: quick},
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash go build ./…", Args: slow},
		{Kind: session.EventToolFinished, Tool: "bash", Args: quick, Took: 120 * time.Millisecond},
	}}}
	a := newTestApp(agent)
	a.bashBackgroundAfter = 0
	base := time.Now()
	a.clock = func() time.Time { return base }
	typeLine(t, a, "build it")
	a.clock = func() time.Time { return base.Add(51 * time.Second) }

	find := func(args string) *entry {
		for i := range a.entries {
			if e := &a.entries[i]; e.kind == entryTool && e.detail.Args == args {
				return e
			}
		}
		t.Fatalf("no row for %s", args)
		return nil
	}
	done, running := find(quick), find(slow)
	if got, _ := a.countClock(done); got != "" {
		t.Fatalf("the finished call is still counting: %q", got)
	}
	if got := elapsedWord(done); got != "0.1s" {
		t.Fatalf("the finished call says %q, want its own tenth of a second", got)
	}
	// Its sibling is genuinely still going, and says so.
	want := "51s / " + countUpWord(session.BashCeilingSeconds*time.Second)
	if got, _ := a.countClock(running); got != want {
		t.Fatalf("the call still running says %q, want %q", got, want)
	}
}

// A MESSAGE TYPED AT A WORKING TURN IS NOT A SECOND TURN. It waits above the
// box for the answer (park.go), so the calls on screen are still this turn's
// calls — and the surface used to age them out with the turn counter, then draw
// "still working" underneath a call that was visibly working.
func TestAWaitingMessageLeavesTheTurnsRunningCallsOnTheSurface(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash go build ./…",
			Args: `{"command":"go build ./..."}`},
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "build it")
	if !a.running() {
		t.Fatal("the call that just began is not running")
	}
	parkLine(t, a, "and the tests too")
	if !a.running() {
		t.Fatal("a waiting message hid a call that is still running")
	}
	if _, drawn := a.ellipsis(); drawn {
		t.Fatal("the surface drew its nothing-is-happening sign over a running call")
	}
	// And the session heard nothing new: the words are held on the surface until
	// this turn is over, which is what makes them editable until then.
	if len(agent.sent) != 1 {
		t.Fatalf("the session was sent %v", agent.sent)
	}
	if len(a.parks) != 1 {
		t.Fatalf("the message typed at the turn was not held: %+v", a.parks)
	}
}

// ── 8. THE COMPACTION ROW, AND THE TWO METERS BESIDE IT ─────────────────────

// A compaction pass is WATCHED, not discovered afterwards. The start event
// opens a spinning row with a clock on it, and the end event settles the SAME
// row into the rule the finished conversation keeps.
func TestTheCompactionRowRunsAndThenSettlesInPlace(t *testing.T) {
	agent := &fakeAgent{model: "m", weight: 168_000, turns: [][]session.Event{{
		{Kind: session.EventCompacting, Hint: "compacting ~168k tokens"},
	}}}
	a := newTestApp(agent)
	base := time.Now()
	a.clock = func() time.Time { return base }
	typeLine(t, a, "keep going")

	// THE ROW IS ALIVE: the hint, the braille spinner, and — six seconds in — the
	// clock, which climbs on the frame the spinner already turns on.
	a.clock = func() time.Time { return base.Add(6 * time.Second) }
	a.touch()
	line := findRow(t, a, "compacting ~168k tokens")
	if !strings.Contains(line, "· 6s") {
		t.Fatalf("the running compaction has no clock: %q", line)
	}
	painted := rowHolding(t, a, "compacting ~168k tokens")
	if !strings.ContainsAny(painted, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the running compaction has no spinner: %q", plain(painted))
	}
	// It is the surface talking about its own housekeeping: dim, the hue the
	// divider wears, and never the accent an answer or a question would take.
	if !strings.Contains(painted, sgr256(a.pal.ramp.dim)) {
		t.Fatalf("the running compaction is not drawn dim: %q", painted)
	}
	if n := countKind(a, entryCompact); n != 1 {
		t.Fatalf("the pass drew %d rows, want exactly one", n)
	}

	// THE END SETTLES THE ROW IT OPENED — the same one, not a second line — and
	// the rule carries what the pass cost in time.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventCompacted, Hint: "compacted from ~168k tokens",
	}})
	if n := countKind(a, entryCompact); n != 1 {
		t.Fatalf("the settle added a row: %d compaction rows, want one", n)
	}
	settled := findRow(t, a, "compacted from ~168k tokens")
	if !strings.Contains(settled, "⚭") || !strings.Contains(settled, "──") {
		t.Fatalf("a finished pass is not the divider: %q", settled)
	}
	if !strings.Contains(settled, "· took 6s") {
		t.Fatalf("the finished pass does not say what it took: %q", settled)
	}
	if strings.ContainsAny(rowHolding(t, a, "compacted from ~168k tokens"), "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("the settled row is still spinning: %q", settled)
	}
	// A FAILED PASS SETTLES IDENTICALLY. The session promises the end event
	// either way, and its hint is the whole difference.
	if strings.Contains(settled, "failed") {
		t.Fatalf("a successful pass reported a failure: %q", settled)
	}
}

// The end event with nothing running is not dropped: a resumed session, or one
// written before the start event existed, still says a pass happened. The row
// is born settled and claims NO duration — a pass this surface did not watch
// has no honest elapsed time.
func TestACompactedEventWithNothingRunningIsBornSettled(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventCompacted, Hint: "compacted from ~84k tokens"},
	}}}
	a := newTestApp(agent)
	runTurn(t, a, agent, "carry on")

	row := findRow(t, a, "compacted from ~84k tokens")
	if !strings.Contains(row, "⚭") || !strings.Contains(row, "──") {
		t.Fatalf("the replayed pass is not the divider: %q", row)
	}
	if strings.Contains(row, "took") {
		t.Fatalf("a pass nobody watched claimed a duration: %q", row)
	}
	if n := countKind(a, entryCompact); n != 1 {
		t.Fatalf("%d compaction rows, want one", n)
	}
}

// THE METER IS RE-READ THE MOMENT THE PASS ENDS. Compaction changes what the
// conversation weighs by an order of magnitude, and the status line's other
// reader is the end of the turn — which can be minutes of tool calls away.
func TestCompactionRereadsTheContextMeterImmediately(t *testing.T) {
	agent := &fakeAgent{model: "m", weight: 168_000, turns: [][]session.Event{{
		{Kind: session.EventCompacting, Hint: "compacting ~168k tokens"},
	}}}
	a := newTestApp(agent)
	a.ctxWindow, a.ctxTokens = 200_000, 168_000
	typeLine(t, a, "keep going")
	if !strings.Contains(plain(a.status(90)), "168k/200k") {
		t.Fatalf("the meter did not open on the heavy conversation:\n%s", plain(a.status(90)))
	}

	// The pass lands: the agent now weighs a tenth of what it did.
	agent.weight = 12_000
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventCompacted, Hint: "compacted from ~168k tokens",
	}})
	if a.ctxTokens != 12_000 {
		t.Fatalf("ctxTokens = %d after the pass, want the agent's 12000", a.ctxTokens)
	}
	if line := plain(a.status(90)); !strings.Contains(line, "12k/200k") {
		t.Fatalf("the status line still carries the old weight:\n%s", line)
	}
}

// THE THREE-RUNG RAMP, measured against the compaction threshold and not the
// window: a 200k window compacts at 170k, so the accent lights at 136k (80% of
// the threshold) and the bad hue at 170k, where a pass is due.
func TestTheContextMeterClimbsAThreeRungRamp(t *testing.T) {
	for _, test := range []struct {
		name   string
		tokens int
		want   ctxHeat
	}{
		{"half a window is furniture", 100_000, ctxCalm},
		{"one token under the accent", 135_999, ctxCalm},
		{"80% of the threshold", 136_000, ctxNear},
		{"one token under the threshold", 169_999, ctxNear},
		{"compaction is due", 170_000, ctxDue},
		{"compaction is overdue", 190_000, ctxDue},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m", weight: test.tokens})
			a.ctxWindow, a.ctxTokens = 200_000, test.tokens
			if got := a.ctxHeat(); got != test.want {
				t.Fatalf("ctxHeat at %d tokens = %d, want %d", test.tokens, got, test.want)
			}
			// The rung is what the line is PAINTED in, which is the whole point
			// of having one.
			segment, _ := a.contextSegment()
			line := a.status(90)
			switch test.want {
			case ctxCalm:
				if !strings.Contains(line, a.pal.dim(segment)) {
					t.Fatalf("a calm meter is not furniture:\n%q", line)
				}
			case ctxNear:
				if !strings.Contains(line, a.pal.accent(segment)) {
					t.Fatalf("an approaching meter is not in the accent:\n%q", line)
				}
			case ctxDue:
				if !strings.Contains(line, a.pal.bad(segment)) {
					t.Fatalf("an overdue meter is not in the bad hue:\n%q", line)
				}
			}
		})
	}

	// A window nobody has said is every rung's zero: a surface that does not
	// know the threshold must not guess that one has been crossed.
	blind := newTestApp(&fakeAgent{model: "nobody/knows", weight: 900_000})
	blind.ctxWindow, blind.ctxTokens = 0, 900_000
	if got := blind.ctxHeat(); got != ctxCalm {
		t.Fatalf("the ramp climbed without a window: %d", got)
	}
	// And the drop-the-percentage law below 1% survives the ramp.
	small := newTestApp(&fakeAgent{model: "m", weight: 500})
	small.ctxWindow, small.ctxTokens = 128_000, 500
	if segment, _ := small.contextSegment(); segment != "500/128k" {
		t.Fatalf("the sub-percent segment reads %q, want the figure alone", segment)
	}
}

// CACHE SPEAKS CASH: the percentage is the hit RATE, and the dollars are what
// the rate MEANT. The total is summed per turn, under the same price guard the
// per-turn note uses.
func TestTheWarmShareSaysWhatTheCacheWasWorth(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "vendor/priced"})
	a.models = func() []Model {
		return []Model{{
			ID: "vendor/priced", ContextLength: 128_000,
			PromptPrice: 0.00001, CacheReadPrice: 0.000001,
		}}
	}
	a.model = "vendor/priced"
	a.inputTokens, a.cacheRead = 100_000, 89_000

	// Before a turn has been priced there is nothing to claim: the rate alone.
	if got := a.warmSegment(); got != "⟲ 89% cached" {
		t.Fatalf("the segment reads %q before any priced turn, want ⟲ 89%% cached", got)
	}

	// Two turns, each 9,800 cached tokens at a nine-dollar-per-million gap:
	// $0.0882 apiece, and the segment carries the SUM.
	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	if want := 2 * 0.0882; a.cacheSaved < want-1e-9 || a.cacheSaved > want+1e-9 {
		t.Fatalf("cacheSaved = %v after two turns, want %v", a.cacheSaved, want)
	}
	got := a.warmSegment()
	if got != "⟲ saved $0.1764 · 89% cached" {
		t.Fatalf("the segment reads %q, want the cash then the rate", got)
	}
	if line := plain(a.status(120)); !strings.Contains(line, got) {
		t.Fatalf("the status line is missing the warm segment:\n%s", line)
	}

	// AN UNPRICED SESSION KEEPS THE SEGMENT IT HAD. "saved $0.00" is not a true
	// thing this surface knows.
	bare := newTestApp(&fakeAgent{model: "vendor/unpriced"})
	bare.models = func() []Model { return []Model{{ID: "vendor/unpriced", ContextLength: 128_000}} }
	bare.model = "vendor/unpriced"
	bare.inputTokens, bare.cacheRead = 100_000, 89_000
	bare.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	if bare.cacheSaved != 0 {
		t.Fatalf("an unpriced turn banked %v", bare.cacheSaved)
	}
	if got := bare.warmSegment(); got != "⟲ 89% cached" {
		t.Fatalf("the unpriced segment reads %q, want ⟲ 89%% cached", got)
	}

	// The saving is a fact about ONE conversation: the next one does not open
	// holding somebody else's cache.
	a.resetMeters()
	if a.cacheSaved != 0 {
		t.Fatalf("the saving survived the reset: %v", a.cacheSaved)
	}
	if got := a.warmSegment(); got != "" {
		t.Fatalf("the warm segment survived the reset: %q", got)
	}
}

// countKind is how many entries of one kind the conversation holds.
func countKind(a *app, kind entryKind) int {
	n := 0
	for i := range a.entries {
		if a.entries[i].kind == kind {
			n++
		}
	}
	return n
}

// rowHolding is the first PAINTED row containing want — the escapes intact,
// which is what an assertion about hue or a spinner has to read.
func rowHolding(t *testing.T, a *app, want string) string {
	t.Helper()
	for _, r := range rows(a) {
		if strings.Contains(plain(r.text), want) {
			return r.text
		}
	}
	t.Fatalf("no row holds %q:\n%s", want, strings.Join(plainRows(a), "\n"))
	return ""
}

// ── shared helpers ──────────────────────────────────────────────────────────

func containsRow(rows []string, want string) bool { return at(rows, want) >= 0 }

// at is the first row containing want, or -1.
func at(rows []string, want string) int {
	for i, r := range rows {
		if strings.Contains(r, want) {
			return i
		}
	}
	return -1
}

// findRow is the first plain row containing want, or a failure naming the
// screen it looked at.
func findRow(t *testing.T, a *app, want string) string {
	t.Helper()
	lines := plainRows(a)
	if i := at(lines, want); i >= 0 {
		return lines[i]
	}
	t.Fatalf("no row holds %q:\n%s", want, strings.Join(lines, "\n"))
	return ""
}

// ── 12. THE BOTTOM HUD ──────────────────────────────────────────────────────
//
// The two rows at the foot of the frame: the input's legend border, and the
// two-cluster status row. Every test below asserts the FACT the element exists
// for — where a person's eye is sent, and what it is sent to.

// hudApp is a surface with everything the HUD reads pinned: a clock (the fade
// and the count-up are functions of wall time), a repository (the legend), and
// a workspace under a known home (the path abbreviation).
func hudApp(t *testing.T) (*app, *fakeAgent, *time.Time) {
	t.Helper()
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash"}
	a := newTestApp(agent)
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	a.tilde, a.workspace, a.place = "/home/dev", "/home/dev/src/aforge-v2", "aforge-v2"
	a.branch, a.branchDirty = "chat-v3-task", true
	a.gitProbe = func(string) (string, bool, bool) { return "chat-v3-task", true, true }
	a.width, a.height = 200, 24
	return a, agent, &now
}

// ── the legend ──────────────────────────────────────────────────────────────

// THE STATUS LINE OWNS IDENTITY. The adjacent legend keeps the branch and input
// affordances without repeating the conversation name.
func TestTheLegendCarriesTheBranchAndTheInputsAffordances(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title = "porting the parser"

	line := plain(a.legend(100))
	for _, want := range []string{"chat-v3-task*", microcopy} {
		if !strings.Contains(line, want) {
			t.Fatalf("the legend is missing %q:\n%q", want, line)
		}
	}
	if strings.Contains(line, "porting the parser") {
		t.Fatalf("the legend repeats the status line's identity: %q", line)
	}
	// THE PATH IS NOT ON IT ANY MORE. It is a fact a person already has — the
	// shell prompt behind this pane says it — and the slot went to the one fact
	// nothing else on the frame carries.
	if strings.Contains(line, "aforge-v2") {
		t.Fatalf("the legend is still carrying the workspace path: %q", line)
	}
	if !strings.HasPrefix(line, "─ ") || !strings.HasSuffix(line, " ─") {
		t.Fatalf("the label is not sitting inside a border: %q", line)
	}
	if ansi.StringWidth(line) != 100 {
		t.Fatalf("the legend is %d cells wide, want the frame's 100", ansi.StringWidth(line))
	}

	// The star is a CLAIM about the tree, and a clean tree does not make it.
	a.branchDirty = false
	if line := plain(a.legend(100)); strings.Contains(line, "*") {
		t.Fatalf("a clean tree is still starred: %q", line)
	}

	// No repository, no branch — and no empty separator where one would have gone.
	a.branch = ""
	line = plain(a.legend(100))
	if label, _, _ := strings.Cut(strings.TrimPrefix(line, "─ "), " ─"); strings.Contains(label, "·") {
		t.Fatalf("a workspace outside a repository still draws a separator: %q", line)
	}
	if strings.Contains(line, "porting the parser") {
		t.Fatalf("the identity moved back onto the legend: %q", line)
	}
}

// THE EMPTINESS LAW ON THE BORDER: a session names itself one turn in, and until
// it has, the slot where the name goes is EMPTY — never "untitled", never the
// workspace standing in for it.
func TestAnUnnamedSessionPutsNoPlaceholderOnTheLegend(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title = ""

	line := plain(a.legend(100))
	label, _, _ := strings.Cut(strings.TrimPrefix(line, "─ "), " ─")
	if label != "chat-v3-task*" {
		t.Fatalf("an unnamed session's legend label = %q, want the branch alone", label)
	}
	for _, banned := range []string{"untitled", "aforge-v2", "·"} {
		if strings.Contains(label, banned) {
			t.Fatalf("the legend invented %q for a session with no name: %q", banned, line)
		}
	}

	// And with nothing true to say at either end, the border is the plain rule it
	// always was — with the input's own affordance still on it, because a person
	// who has not typed anything yet is exactly who "/ commands" is for.
	a.branch = ""
	line = plain(a.legend(100))
	if !strings.Contains(line, microcopy) {
		t.Fatalf("the hint slot went with the label: %q", line)
	}
	if strings.Contains(line, "─ ─") || strings.Contains(line, "  ") {
		t.Fatalf("the empty label left a gap in the rule: %q", line)
	}
	if ansi.StringWidth(line) != 100 {
		t.Fatalf("the legend is %d cells wide, want the frame's 100", ansi.StringWidth(line))
	}
}

// A LONG IDENTITY CANNOT TAKE CELLS FROM THE LEGEND because identity belongs to
// the status line below it.
func TestALongNameDoesNotChangeTheLegend(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title = "porting the parser off the old tokenizer and onto the new one at last"

	line := plain(a.legend(100))
	if !strings.Contains(line, "chat-v3-task*") || !strings.Contains(line, microcopy) ||
		strings.Contains(line, "porting the parser") {
		t.Fatalf("the conversation name changed the legend: %q", line)
	}
	if ansi.StringWidth(line) != 100 {
		t.Fatalf("the legend is %d cells wide, want the frame's 100", ansi.StringWidth(line))
	}
}

// THE NARROW LADDER: THE BRANCH GOES BEFORE THE DOOR. It used to be the other
// way round, and the phone tier paid for it — under the tight floor the hint
// slot went silent AND the branch was dropped, so the line was refused at both
// ends and drew a bare rule with nothing written on it, at the one width where
// a newcomer most needs to be told that `/` opens the list of everything this
// surface can be told to do. A branch is on the shell prompt behind this pane;
// the door is written nowhere else on a frame this narrow.
func TestTheLegendDropsTheBranchBeforeTheCommandsDoor(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title = "porting the parser"
	// The branch is long enough that the two cannot share an eighty-column frame.
	// The microcopy is one affordance now rather than two (render.go), so the rung
	// of the ladder where the branch stands alone is reached by a longer name
	// rather than by a narrower frame — the ORDER is the law, and the width it
	// bites at is a consequence of how much there is to say.
	const branch = "feature/the-very-long-branch-name-that-goes-on-and-on"
	a.branch, a.branchDirty = branch, false

	tight := plain(a.legend(60))
	if strings.Contains(tight, "feature/") {
		t.Fatalf("a tight frame kept the branch: %q", tight)
	}
	if !strings.Contains(tight, microcopy) {
		t.Fatalf("a tight frame is a rule with nothing written on it: %q", tight)
	}
	if strings.Contains(tight, "porting the parser") {
		t.Fatalf("the tight legend repeated identity: %q", tight)
	}

	// Without a duplicate name, the middle width has room for both facts.
	middle := plain(a.legend(80))
	if !strings.Contains(middle, microcopy) {
		t.Fatalf("the identity-free legend dropped usable hints: %q", middle)
	}
	if !strings.Contains(middle, branch) {
		t.Fatalf("the branch was dropped before the microcopy: %q", middle)
	}
	if strings.Contains(middle, "porting the parser") {
		t.Fatalf("the middle legend repeated identity: %q", middle)
	}

	// And a frame with no room for a label at all is the rule it always was.
	if got := plain(a.legend(6)); strings.Trim(got, "─") != "" {
		t.Fatalf("a six-cell frame drew a label: %q", got)
	}
}

func TestThePathAbbreviatesLikeFishAndKeepsTheLastSegmentWhole(t *testing.T) {
	for _, tc := range []struct {
		dir, home string
		hard      int
		want      string
	}{
		{"/home/dev/src/aforge-v2", "/home/dev", 0, "~/s/aforge-v2"},
		{"/home/dev/src/aforge-v2", "/home/dev", 1, "…/aforge-v2"},
		{"/home/dev/src/aforge-v2", "/home/dev", 2, "aforge-v2"},
		{"/home/dev", "/home/dev", 0, "~"},
		{"/home/dev/.claude/projects/lab", "/home/dev", 0, "~/.c/p/lab"},
		{"/var/log/nginx", "", 0, "/v/l/nginx"},
		{"/home/other/work", "/home/dev", 0, "/h/o/work"},
		{"lab", "", 0, "lab"},
		{"", "", 0, ""},
	} {
		if got := shortPath(tc.dir, tc.home, tc.hard); got != tc.want {
			t.Fatalf("shortPath(%q, %q, %d) = %q, want %q", tc.dir, tc.home, tc.hard, got, tc.want)
		}
	}
}

// ── the two clusters ────────────────────────────────────────────────────────

// IDENTITY LEFT, TELEMETRY RIGHT, AND A GAP BETWEEN THEM. No pipes, no product
// name, and the model without its vendor.
func TestTheStatusRowIsIdentityLeftAndTelemetryRight(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title, a.cost = "porting the parser", 0.14
	a.ctxWindow, a.ctxTokens = 128_000, 12_400

	rows := a.statusRows(200)
	if len(rows) != 1 {
		t.Fatalf("a 200-column frame took %d rows for the status", len(rows))
	}
	line := plain(rows[0])
	name, model := strings.Index(line, "porting the parser"), strings.Index(line, "deepseek-v4-flash")
	cost, state := strings.Index(line, "$0.14"), strings.Index(line, "idle")
	if name < 0 || model < name || cost < model || state < cost {
		t.Fatalf("the clusters are out of order:\n%q", line)
	}
	if strings.Contains(line, "deepseek/") {
		t.Fatalf("the vendor is still on the line: %q", line)
	}
	if strings.Contains(line, product) {
		t.Fatalf("the product name is still on the line: %q", line)
	}
	if strings.ContainsAny(line, "|│") {
		t.Fatalf("the clusters are separated by a glyph rather than by the gap: %q", line)
	}
	// The barrier IS the gap: the identity ends, and nothing else is said until
	// the telemetry starts.
	if !strings.Contains(line, "    ") {
		t.Fatalf("there is no gap between the clusters: %q", line)
	}
	// The state word is LAST, whatever else is on the line.
	if !strings.HasSuffix(strings.TrimRight(line, " "), "idle") {
		t.Fatalf("the state word is not last: %q", line)
	}
}

// ── the ambient counts ──────────────────────────────────────────────────────

// WHAT IS STILL ALIVE OUT THERE, and nothing at all when the answer is none.
func TestTheAmbientCountsShowOnlyWhatIsAlive(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		beginWith("bash", "bash go run ./cmd/api", `{"command":"go run ./cmd/api","background":true}`),
		toolEnd("bash", "job 3 started; log at /tmp/j3.log"),
		beginWith("watch", "watch tail -f app.log", `{"command":"tail -f app.log","mode":"change"}`),
		toolEnd("watch", "watch app.log started · every 30s — kill with jobs"),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	a.width = 200

	if got := a.ambientSegment(); got != "" {
		t.Fatalf("a session with nothing running drew %q", got)
	}

	runTurn(t, a, agent, "run the api and watch the log")
	if got := a.ambientSegment(); got != "1 job · 1 watch" {
		t.Fatalf("the ambient counts read %q, want 1 job · 1 watch", got)
	}
	if line := plain(a.status(200)); !strings.Contains(line, "1 job · 1 watch") {
		t.Fatalf("the counts are not on the line:\n%q", line)
	}

	// A KILL TAKES THE ONE IT NAMES. The id came off the job's own answer, so a
	// batch that killed the job must not decrement the watch.
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: "jobs", status: toolOK, turn: a.turn,
		detail: toolDetail{Args: `{"action":"kill","id":3}`},
	})
	a.hudStale = true
	if got := a.ambientSegment(); got != "1 watch" {
		t.Fatalf("after killing job 3 the counts read %q, want 1 watch", got)
	}

	// The watch has no id of its own on the wire, so the next kill takes it.
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: "jobs", status: toolOK, turn: a.turn,
		detail: toolDetail{Args: `{"action":"kill","id":4}`},
	})
	a.hudStale = true
	if got := a.ambientSegment(); got != "" {
		t.Fatalf("everything was killed and the segment still reads %q", got)
	}
}

// ── the session delta ───────────────────────────────────────────────────────

// THE LOWEST PRIORITY ON THE LINE: on a comfortable frame, and nowhere else.
func TestTheSessionDeltaRidesOnlyAComfortableFrame(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		beginWith("edit", "edit internal/session/loop.go", editPayload),
		toolEnd("edit", ""),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	a.width = 200
	runTurn(t, a, agent, "bump the limit")

	delta := a.deltaSegment()
	if !strings.HasPrefix(delta, "Σ ") || !strings.Contains(delta, glyphAdd+"1") {
		t.Fatalf("the session delta reads %q, want a Σ with the edit's stat in it", delta)
	}
	if line := plain(a.status(hudWide)); !strings.Contains(line, delta) {
		t.Fatalf("a comfortable frame dropped the delta:\n%q", line)
	}
	if line := plain(a.status(hudWide - 1)); strings.Contains(line, "Σ") {
		t.Fatalf("the delta survived a frame that is not comfortable:\n%q", line)
	}

	// A session that has written nothing says nothing, at any width.
	fresh := newTestApp(&fakeAgent{model: "m"})
	if got := fresh.deltaSegment(); got != "" {
		t.Fatalf("a session that wrote nothing drew %q", got)
	}
}

// ── the sparkline ───────────────────────────────────────────────────────────

// THE SHAPE OF THE APPROACH, not just the distance: six turn-end readings, one
// glyph each, measured against the compaction threshold.
func TestTheContextSparklineNeedsTwoReadingsAndScalesToTheThreshold(t *testing.T) {
	a, _, _ := hudApp(t)
	a.ctxWindow = 200_000
	threshold := session.CompactThreshold(a.ctxWindow)

	if got := a.ctxSpark(); got != "" {
		t.Fatalf("a session with no readings drew %q", got)
	}
	a.ctxRing = []int{threshold / 10}
	if got := a.ctxSpark(); got != "" {
		t.Fatalf("one reading is not a trend, and it drew %q", got)
	}

	a.ctxRing = []int{threshold / 10, threshold / 2, threshold}
	spark := a.ctxSpark()
	bars := []rune(spark)
	if len(bars) != 3 {
		t.Fatalf("three readings drew %d bars: %q", len(bars), spark)
	}
	if !(bars[0] < bars[1] && bars[1] < bars[2]) {
		t.Fatalf("a climbing conversation did not draw a climbing line: %q", spark)
	}
	if top := []rune(sparkBars); bars[2] != top[len(top)-1] {
		t.Fatalf("a reading at the threshold is not the top bar: %q", spark)
	}

	// It rides the meter it is about, and only where there is room for it.
	a.ctxTokens = threshold / 2
	if line := plain(a.status(200)); !strings.Contains(line, spark) {
		t.Fatalf("the sparkline is not beside the meter:\n%q", line)
	}
	if line := plain(a.status(hudTight - 1)); strings.Contains(line, spark) {
		t.Fatalf("a tight frame kept the sparkline:\n%q", line)
	}

	// The two tiers that cannot read shape keep the number and lose the line.
	a.linear = true
	if got := a.ctxSpark(); got != "" {
		t.Fatalf("the linear tier drew a sparkline: %q", got)
	}
	a.linear, a.pal.ascii = false, true
	if got := a.ctxSpark(); got != "" {
		t.Fatalf("an ASCII terminal drew block elements: %q", got)
	}
}

// ── the burn rate and the forecast ──────────────────────────────────────────

func TestTheBurnRateIsThisTurnsOutputOverThisTurnsSeconds(t *testing.T) {
	a, _, now := hudApp(t)

	a.state = stateWorking
	a.turnBegan, a.turnOutStart = *now, 2_000
	a.outputTokens = 12_000

	// Under a second there is no rate, only a first packet.
	if got := a.burnSegment(); got != "" {
		t.Fatalf("a turn 0s old quoted %q", got)
	}
	*now = now.Add(10 * time.Second)
	if got := a.burnSegment(); got != "1k tok/s avg" {
		t.Fatalf("the burn reads %q, want 1k tok/s avg", got)
	}
	if line := plain(a.status(200)); !strings.Contains(line, "1k tok/s avg") {
		t.Fatalf("the burn is not on the line:\n%q", line)
	}

	// A settled turn has no rate: the figure is about now, or it is not drawn.
	a.state = stateIdle
	if got := a.burnSegment(); got != "" {
		t.Fatalf("an idle surface is still burning at %q", got)
	}
}

func TestTheCompactionForecastSpeaksOnlyWhenItIsClose(t *testing.T) {
	a, _, _ := hudApp(t)
	a.ctxWindow = 200_000
	threshold := session.CompactThreshold(a.ctxWindow)

	// Growing by a tenth of the threshold a turn, with three tenths to go.
	step := threshold / 10
	a.ctxRing = []int{threshold - 5*step, threshold - 4*step, threshold - 3*step}
	a.ctxTokens = threshold - 3*step
	if got := a.etaSegment(); got != "compaction in ~3 turns" {
		t.Fatalf("the forecast reads %q, want compaction in ~3 turns", got)
	}

	// A conversation that is not growing gets no forecast at all, whatever it
	// is carrying: "in ~400 turns" is a number nobody will ever use.
	flat := threshold / 2
	a.ctxRing, a.ctxTokens = []int{flat, flat, flat}, flat
	if got := a.etaSegment(); got != "" {
		t.Fatalf("a flat conversation was forecast at %q", got)
	}

	// Far away is silent too.
	a.ctxRing, a.ctxTokens = []int{1_000, 2_000}, 2_000
	if got := a.etaSegment(); got != "" {
		t.Fatalf("a distant compaction was forecast at %q", got)
	}

	// And a compaction that is already due is not a forecast.
	a.ctxRing, a.ctxTokens = []int{threshold - step, threshold + step}, threshold+step
	if got := a.etaSegment(); got != "" {
		t.Fatalf("an overdue compaction was forecast at %q", got)
	}
}

// The ring is sampled at TURN END and nowhere else.
func TestTheContextRingIsSampledOncePerTurn(t *testing.T) {
	agent := &fakeAgent{model: "m", weight: 40_000, turns: [][]session.Event{
		{{Kind: session.EventTurnDone}},
		{{Kind: session.EventTurnDone}},
	}}
	a := newTestApp(agent)
	a.ctxWindow = 200_000

	runTurn(t, a, agent, "one")
	agent.weight = 60_000
	runTurn(t, a, agent, "two")

	if got := a.ctxRing; len(got) != 2 || got[0] != 40_000 || got[1] != 60_000 {
		t.Fatalf("the ring holds %v, want one reading per settled turn", got)
	}
}

// ── the hint slot ───────────────────────────────────────────────────────────

// STATE-DRIVEN, AND EMPTY AT REST. There is no static cheatsheet on this
// surface any more.
func TestTheHintSlotFollowsTheStateAndIsEmptyAtRest(t *testing.T) {
	a, _, _ := hudApp(t)

	if got := a.hintWord(); got != "" {
		t.Fatalf("an idle surface offered %q", got)
	}
	if !strings.Contains(plain(a.legend(120)), microcopy) {
		t.Fatal("an idle legend lost the input's own affordances")
	}

	a.state = stateWorking
	if got := a.hintWord(); got != "esc interrupt" {
		t.Fatalf("a working surface offered %q", got)
	}
	if line := plain(a.legend(120)); !strings.Contains(line, "esc interrupt") ||
		strings.Contains(line, microcopy) {
		t.Fatalf("the hint did not take the slot: %q", line)
	}

	a.state = stateIdle
	a.pick.open = true
	// The crew rides the end of the picker's hint (crew.go's [app.crewHint]),
	// and an ordinary launch has one.
	if got := a.hintWord(); got != "enter switch · esc · crew "+config.DefaultCrew {
		t.Fatalf("an open picker offered %q", got)
	}
	a.pick.open = false

	// A call parked on a person offers the keys that answer it — the SAME keys
	// the question block draws, which is what makes the hint safe to act on and
	// is why the slot is DERIVED from the question rather than spelled here
	// (question.go's [app.questionHint]).
	a.entries = append(a.entries, entry{kind: entryTool, tool: "bash", status: toolConsent})
	raiseAsk(a, 7, "bash")
	hint := a.hintWord()
	for _, want := range []string{"1 allow once", "3 deny", "2 always", "esc later"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("the consent hint %q is missing %q", hint, want)
		}
	}
}

// ── negative-space safety ───────────────────────────────────────────────────

// ABSENCE IS THE SAFE STATE: the posture is drawn only when the gate is open.
func TestTheApprovalPostureIsDrawnOnlyWhenItIsUnsafe(t *testing.T) {
	a, _, _ := hudApp(t)

	for _, mode := range []string{"", "prompt", "deny"} {
		a.approval = mode
		if got := a.yoloSegment(); got != "" {
			t.Fatalf("the %q posture drew %q — absence is the safe state", mode, got)
		}
		if strings.Contains(plain(a.status(200)), "YOLO") {
			t.Fatalf("the %q posture is shouting on the line", mode)
		}
	}

	a.approval = "allow"
	line := a.status(200)
	if !strings.Contains(plain(line), "YOLO") {
		t.Fatalf("an open gate said nothing:\n%q", plain(line))
	}
	if !strings.Contains(line, a.pal.bad("YOLO")) {
		t.Fatalf("the open gate is not painted as one:\n%q", line)
	}
}

// ── the age fade ────────────────────────────────────────────────────────────

// PAINT FOLLOWS RECENCY. A number that just moved is the news on the line; ten
// seconds later it is furniture again.
func TestTelemetryFadesWithAgeSoStaleNumbersStopCompeting(t *testing.T) {
	a, _, now := hudApp(t)
	a.cost = 0.10

	// First appearance is not a change: there was nothing there to have changed.
	if line := a.status(200); !strings.Contains(line, a.pal.dim("$0.10")) {
		t.Fatalf("a segment's first appearance is already glowing:\n%q", line)
	}

	a.cost = 0.20
	if line := a.status(200); !strings.Contains(line, a.pal.ink("$0.20")) {
		t.Fatalf("a segment that just moved is not ink:\n%q", line)
	}
	*now = now.Add(5 * time.Second)
	if line := a.status(200); !strings.Contains(line, a.pal.muted("$0.20")) {
		t.Fatalf("a five-second-old figure is not on the middle rung:\n%q", line)
	}
	*now = now.Add(6 * time.Second)
	if line := a.status(200); !strings.Contains(line, a.pal.dim("$0.20")) {
		t.Fatalf("an eleven-second-old figure is still competing:\n%q", line)
	}

	// The catch-up ticks are BOUNDED: two of them, and no idle ticker behind.
	batch, ok := fadeTicks()().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("a settled turn scheduled %v wakeups, want exactly two", batch)
	}
	if cmd := a.paint(); cmd != nil {
		t.Fatal("an idle surface asked for another frame")
	}
	drive(t, a, hudFadeMsg{})
	if !a.dirty {
		t.Fatal("a catch-up tick did not repaint the frame")
	}
}

// ── attention routing ───────────────────────────────────────────────────────

// THE HUE BUDGET FOLLOWS THE DECISION. While a person is being asked something,
// the state cluster and the legend's label are violet — and nothing else on the
// HUD is allowed to compete, the age fade included.
func TestAWaitingQuestionRoutesTheHueAndQuietsEverythingElse(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	_ = agent
	a.width = 200
	a.tilde, a.workspace = "/home/dev", "/home/dev/src/aforge-v2"
	a.title = "cleaning the build directory"
	a.cost = 0.10
	typeLine(t, a, "clean it")
	a.cost = 0.20 // a figure that moved THIS INSTANT, and still may not glow

	line := a.status(200)
	if !strings.Contains(line, a.pal.askBold(waitingWord)) {
		t.Fatalf("the state cluster is not the question hue:\n%q", line)
	}
	if !strings.Contains(line, a.pal.dim("$0.20")) {
		t.Fatalf("a number is competing with a question:\n%q", line)
	}
	// The legend no longer carries the conversation's name at all — the status
	// line owns identity — so the question hue's second home went with it. The
	// waiting word above and the card itself are where the hue lives, and the
	// legend has no title to route it onto.
	if strings.Contains(plain(a.legend(120)), "cleaning the build directory") {
		t.Fatalf("the legend still carries the conversation's name:\n%q", a.legend(120))
	}

	// Working, the paint is spent on ALIVENESS and on nothing else: the spinner
	// and its clock, in the accent, and the clock is the part that says how long.
	quiet := newTestApp(&fakeAgent{model: "m"})
	quiet.width = 200
	quiet.state = stateWorking
	base := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	quiet.clock = func() time.Time { return base.Add(64 * time.Second) }
	quiet.turnBegan = base
	word, painted := quiet.stateSegment()
	if !strings.HasSuffix(word, "working · 1m 4s") {
		t.Fatalf("the state word is %q, want the spinner, the word and the clock", word)
	}
	if !strings.Contains(painted, quiet.pal.accent("working")) ||
		!strings.Contains(painted, quiet.pal.accent("1m 4s")) {
		t.Fatalf("the aliveness is not the accent:\n%q", painted)
	}
}

// ── the narrow-frame law ────────────────────────────────────────────────────

// One ladder, four frames: what each width keeps, and where the telemetry stops
// sharing a row with the identity.
func TestTheHudLaysOutAtEveryWidth(t *testing.T) {
	agent := &fakeAgent{model: "deepseek/deepseek-v4-flash", turns: [][]session.Event{{
		beginWith("edit", "edit internal/session/loop.go", editPayload),
		toolEnd("edit", ""),
		{Kind: session.EventTurnDone},
	}}}
	a := newTestApp(agent)
	a.tilde, a.workspace, a.place = "/home/dev", "/home/dev/src/aforge-v2", "aforge-v2"
	a.ctxWindow = 200_000
	a.title, a.cost = "the bottom hud wave", 1.42
	runTurn(t, a, agent, "bump the limit")
	// The branch is pinned AFTER the turn: a settled turn re-asks the
	// repository (app.go's [app.settle]), and this directory does not have one.
	a.branch = "chat-v3-task"
	a.ctxTokens = 100_000
	a.ctxRing = []int{20_000, 60_000, 100_000}
	a.inputTokens, a.cacheRead = 10_000, 6_200
	// A dollar spent and a half-full window arm two earned hints (notice.go),
	// and either would take the rest slot this ladder measures. The ladder is
	// about the slot's rest state, so the tips are silenced here as the Display
	// row would silence them.
	a.notices.enabled = false

	spark := a.ctxSpark()
	for _, tc := range []struct {
		width       int
		delta, sp   bool
		branch, mic bool
		rows        int
	}{
		{width: 200, delta: true, sp: true, branch: true, mic: true, rows: 1},
		// At 120 the delta is the segment that yields: "cached" joined the warm
		// share (the owner's word for which percentage this is), the row grew by
		// its width, and the Σ figure is the lowest-ranked fact on the line. A
		// person at 120 columns keeps the word that stops a misreading over a
		// second spelling of money the meter already carries.
		{width: 120, delta: false, sp: true, branch: true, mic: true, rows: 1},
		{width: 100, delta: false, sp: true, branch: true, mic: true, rows: 1},
		// At seventy the clusters stop sharing a row — the identity and the
		// telemetry cannot both fit with a barrier between them — and everything
		// else is still on.
		{width: 70, delta: false, sp: true, branch: true, mic: true, rows: 2},
		// Below the tight floor the legend gives up THE BRANCH and keeps the
		// door — the one fact on this line a person cannot read off the pane
		// behind it — and the meter keeps the number alone.
		{width: 60, delta: false, sp: false, branch: false, mic: true, rows: 2},
	} {
		rows := a.statusRows(tc.width)
		if len(rows) != tc.rows {
			t.Fatalf("at %d columns the status took %d rows, want %d:\n%s",
				tc.width, len(rows), tc.rows, strings.Join(rows, "\n"))
		}
		for _, row := range rows {
			if got := ansi.StringWidth(plain(row)); got > tc.width {
				t.Fatalf("at %d columns a status row is %d wide: %q", tc.width, got, plain(row))
			}
		}
		line := plain(strings.Join(rows, "\n"))
		if has := strings.Contains(line, "Σ"); has != tc.delta {
			t.Fatalf("at %d columns the delta is %v:\n%q", tc.width, has, line)
		}
		if has := strings.Contains(line, spark); has != tc.sp {
			t.Fatalf("at %d columns the sparkline is %v:\n%q", tc.width, has, line)
		}
		// The state word survives every width: it is why the line is there.
		if !strings.Contains(line, "idle") {
			t.Fatalf("at %d columns the state word was dropped:\n%q", tc.width, line)
		}
		legend := plain(a.legend(tc.width))
		if has := strings.Contains(legend, "chat-v3-task"); has != tc.branch {
			t.Fatalf("at %d columns the branch is %v: %q", tc.width, has, legend)
		}
		if has := strings.Contains(legend, microcopy); has != tc.mic {
			t.Fatalf("at %d columns the microcopy is %v: %q", tc.width, has, legend)
		}
		// Identity belongs to the status line at every width and is not repeated
		// on the adjacent legend.
		if strings.Contains(legend, "the bottom hud wave") {
			t.Fatalf("at %d columns the legend repeated identity: %q", tc.width, legend)
		}
		if strings.Contains(legend, "aforge-v2") {
			t.Fatalf("at %d columns the legend is still carrying the path: %q", tc.width, legend)
		}
	}
}

// The frame's geometry agrees with the row count at every width: a status that
// took two rows and a chrome height that counted one would deliver every click
// below the conversation to the wrong row (view.go).
func TestTheChromeHeightCountsTheWrappedStatus(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title, a.cost = "the bottom hud wave", 1.42
	a.ctxWindow, a.ctxTokens = 200_000, 100_000

	for _, width := range []int{200, 120, 100, 70} {
		a.width = width
		a.touch()
		lines := strings.Split(plain(frame(a)), "\n")
		if len(lines) != a.height {
			t.Fatalf("at %d columns the frame is %d rows, want %d", width, len(lines), a.height)
		}
		want := len(a.statusRows(width))
		if got := a.statusHeight(width); got != want {
			t.Fatalf("at %d columns statusHeight says %d and the row builder says %d",
				width, got, want)
		}
		chrome, marks, _, _ := a.chrome(width)
		if len(chrome) != len(marks) {
			t.Fatalf("at %d columns the chrome has %d rows and %d marks",
				width, len(chrome), len(marks))
		}
	}
}

// THE ROW IS NEVER WIDER THAN THE FRAME, AND THE CLOCK MOVING MID-FRAME MAY NOT
// MAKE IT ONE CELL WIDER. Every width decision the status row makes is made
// from the PLAIN cluster (render.go's [app.paintParts]), so a painted segment
// that measures more than the text it was measured as makes a row wider than
// the frame — which the renderer does not wrap but CUTS, dropping the last cell
// of the line and leaving the clock reading "10" instead of "10s", with the
// keeping and money doors a column left of where they were drawn.
//
// The state word is where it happened: it carries the turn's count-up, it is
// built from [app.now] — which is time.Now() in the running program — and the
// painting used to be asked for a second time, at paint time, from a clock that
// had moved on. A turn crossing "9s" into "10s" between the two reads drew a
// row one cell wider than the row it had measured (render.go's [hudPart]).
func TestAClockThatTicksMidFrameCannotPushTheStatusRowPastTheFrame(t *testing.T) {
	a, _, _ := hudApp(t)
	a.title = "Identifying AI-benefiting stocks for growth and hedging"
	a.cost, a.ctxWindow, a.ctxTokens = 0.08, 1_300_000, 22_100
	a.inputTokens, a.cacheRead = 100_000, 42_000
	a.approval = "allow"
	a.state = stateWorking
	began := a.now()
	a.turnBegan = began
	a.outputTokens, a.turnOutStart = 1600, 0

	// The clock stands one millisecond short of ten seconds for the first read
	// of a frame and has crossed it by the next, which is the whole of what
	// time.Now() does to a turn passing that boundary. The offset walks so that
	// the crossing lands between every pair of reads the row makes, wherever in
	// the layout those two happen to fall.
	reads := 0
	a.clock = func() time.Time {
		reads++
		if reads <= 1 {
			return began.Add(9999 * time.Millisecond)
		}
		return began.Add(10 * time.Second)
	}
	for _, width := range []int{210, 160, 120, 100, 80} {
		for shift := 0; shift < 60; shift++ {
			reads = -shift
			for i, row := range a.statusRows(width) {
				if got := ansi.StringWidth(plain(row)); got > width {
					t.Fatalf("at %d columns row %d is %d cells wide: %q",
						width, i, got, plain(row))
				}
			}
			// And the painted cluster measures exactly what the row was laid out
			// against, which is the law the row's arithmetic rests on.
			reads = -shift
			painted, measured := a.paintParts(a.telemetry(width))
			if drawn, want := ansi.StringWidth(plain(painted)), ansi.StringWidth(measured); drawn != want {
				t.Fatalf("at %d columns the telemetry draws %d cells and was measured as %d:\n  %q\n  %q",
					width, drawn, want, plain(painted), measured)
			}
		}
	}
}

// The repository is asked off the model loop, and its answer lands on the
// legend. A probe that fails leaves no branch rather than a stale one.
func TestTheBranchArrivesAsAMessageAndCanGoAway(t *testing.T) {
	a, _, _ := hudApp(t)
	a.branch, a.branchDirty = "", false
	drive(t, a, gitMsg{branch: "chat-v3-task", dirty: true, ok: true})
	if !strings.Contains(plain(a.legend(120)), "chat-v3-task*") {
		t.Fatalf("the branch did not reach the legend: %q", plain(a.legend(120)))
	}
	drive(t, a, gitMsg{ok: false})
	if strings.Contains(plain(a.legend(120)), "chat-v3-task") {
		t.Fatalf("a failed probe left a stale branch: %q", plain(a.legend(120)))
	}
}

// ── THE TASK SURFACE: A DECISION, THEN A PRESENCE ───────────────────────────
//
// Every test below asserts the FACT the feature exists for — what a person
// reads, what reaches the engine, what the frame charges for a rail — rather
// than the shape of the code under it (task.go).

// taskFake is a session with a tasker: it records what it was asked to resolve
// and holds the standing update lane open. It embeds the scripted agent every
// other test here runs against, because the task contract is a widening of that
// session and not a different one.
type taskFake struct {
	*fakeAgent
	answered []taskReply
	held     []uint64
	updates  chan session.Event
	pending  []uint64
	work     []session.WorkNode
}

type taskReply struct {
	id     uint64
	answer session.TaskAnswer
}

func (f *taskFake) ResolveTask(id uint64, answer session.TaskAnswer) {
	f.answered = append(f.answered, taskReply{id: id, answer: answer})
	// The engine forgets a proposal the moment it is answered, and
	// [Agent.PendingTasks] is what the surface reads that through.
	for i, waiting := range f.pending {
		if waiting == id {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			break
		}
	}
}

func (f *taskFake) HoldTask(id uint64) { f.held = append(f.held, id) }

// ResolveQuestion is THE ONE DOOR, on this stand-in: the proposal is answered
// on the question block now, so this is the road every answer takes.
//
// IT ROUTES RATHER THAN DECIDING, exactly as the engine's does (session's
// applyToLane): the key's meaning comes from the ONE mapping
// ([session.AnswerFromKey]), and a typed answer with no key is the redirect —
// a correction is a yes to the corrected version. A stand-in that decided any
// of that itself would be a second table, and the first hour one of them moved
// these tests would be green about the wrong answer.
func (f *taskFake) ResolveQuestion(answer session.Answer) error {
	if answer.Kind != session.QuestionTask {
		return nil
	}
	if words := answer.Words(); answer.FirstKey() == "" && words != "" {
		f.ResolveTask(answer.ID, session.TaskAnswer{Approved: true, Redirect: words})
		return nil
	}
	action, ok := session.AnswerFromKey(answer.Kind, answer.FirstKey())
	if !ok {
		return nil
	}
	f.ResolveTask(answer.ID, action.Task)
	return nil
}

func (f *taskFake) TaskUpdates() <-chan session.Event { return f.updates }
func (f *taskFake) PendingTasks() []uint64            { return f.pending }
func (f *taskFake) WorkingNow() []session.WorkNode    { return f.work }

// taskApp is a surface with a tasker under it and a pinned clock over it: a
// countdown cannot be tested by waiting four seconds.
func taskApp(t *testing.T) (*app, *taskFake, func(time.Duration)) {
	t.Helper()
	agent := &taskFake{
		fakeAgent: &fakeAgent{model: "deepseek/deepseek-v4-flash"},
		updates:   make(chan session.Event, 8),
	}
	a := newTestApp(agent)
	a.width, a.height = 200, 24
	// AND IT PINS THE PLACES ROOT. The tasks place reads the MACHINE — every
	// project under [session.PlacesRoot] — so a suite that left this empty would
	// be reading the developer's own history: the refusal on an empty machine
	// would pass in CI and fail on any laptop that had ever run a task.
	a.homeRoot = t.TempDir()
	now := taskFixtureNow
	a.clock = func() time.Time { return now }
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

// taskFixtureNow is the one clock every task fixture is dated from.
//
// ONE CLOCK AND NOT TWO. The surface's own is pinned so a countdown can be
// tested without waiting four seconds; a row dated from time.Now() beside it is
// a row that landed in the future, which the place's time window drops and every
// age on screen reads wrong. So the rows are dated from this and the surface is
// too, and the suite is the same on any day of any year.
var taskFixtureNow = time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

// proposal is one EventTaskProposal, as the engine sends it.
func proposal(a *app, id uint64, countdown time.Duration) session.Event {
	deadline := time.Time{}
	if countdown > 0 {
		deadline = a.now().Add(countdown)
	}
	return session.Event{
		Kind: session.EventTaskProposal,
		Tool: "propose_task",
		Task: &session.TaskNotice{
			ID:         id,
			Title:      "Fix the nil-map crash",
			Summary:    "The parser drops a key on an empty map. This adds the guard and the regression test.",
			Brief:      "internal/parse/keys.go builds its map lazily and writes to it before it exists.",
			Acceptance: "go test ./internal/parse passes with the new case",
			Deadline:   deadline,
		},
	}
}

// update is one EventTaskUpdate for a node's life.
func update(id uint64, title string, state session.TaskState, notice session.TaskNotice) session.Event {
	notice.ID, notice.Title, notice.State = id, title, state
	return session.Event{Kind: session.EventTaskUpdate, Tool: "propose_task", Task: &notice}
}

// taskText is the conversation as a reader sees it, laid out at the width the
// transcript actually gets.
func taskText(a *app) string {
	var out []string
	for _, r := range a.visible(a.bodyWidth()) {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// askTask puts one proposal in front of a person and lets its question settle,
// which is what a real screen does between the block arriving and a hand
// reaching the keyboard (question.go's SETTLE GUARD). Every test below goes
// through it, because a key pressed on the frame a question arrives on is
// DROPPED — and a test that drove keys straight at a fresh question would be
// asserting against the guard rather than against the answer.
func askTask(t *testing.T, a *app, id uint64, countdown time.Duration) {
	t.Helper()
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, id, countdown)})
	taskText(a)
	settleAsk(a)
}

// taskAsk is the question block as a reader sees it: the rows above the box,
// which is where every decision on this surface is put (question.go).
func taskAsk(a *app) string {
	return plain(strings.Join(a.questionRows(a.width), "\n"))
}

// THE PROPOSAL IS TWO THINGS IN TWO PLACES. The assignment is a block in the
// TRANSCRIPT — part of what happened, and readable a week later — and the ASKING
// is above the box with every other question this surface puts (question.go).
// The brief is behind the expansion every other detail on this surface is behind.
func TestATaskProposalRendersTheDecisionAndHidesTheBrief(t *testing.T) {
	a, _, _ := taskApp(t)
	askTask(t, a, 7, 4*time.Second)

	text := taskText(a)
	// THE BLOCK IN THE TRANSCRIPT: a head with the name in it, the sentence
	// under it, the facts about the work, and a foot under the lot.
	for _, want := range []string{
		// THE HEAD IS THE NAME AND THE NODE'S OWN MARK (taskident.go): the title
		// the engine wrote is cut to the two-or-three-word name, and the identity
		// cell that will follow this node onto the card that lands rides beside
		// the question glyph. The rail does not carry it — that column holds
		// nothing but tasks, so the mark tells nothing apart there.
		taskHeadCorner + " " + glyphAsk + " " + plain(a.taskMark(identFor(7))) + " Fix the nil-map",
		"The parser drops a key",
		taskFootCorner,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the proposal is missing %q:\n%s", want, text)
		}
	}
	// AND THE ANSWERS ARE NOT ON IT. They were a decision drawn in a place no
	// other decision on this surface is drawn; every one of them is above the
	// box now, and the block below is what the question is ABOUT.
	for _, gone := range []string{"[ yes ]", "[ redirect ]", "auto-starts in", meterFull} {
		if strings.Contains(text, gone) {
			t.Fatalf("the transcript block still draws its own answers (%q):\n%s", gone, text)
		}
	}
	for _, hidden := range []string{"builds its map lazily", "go test ./internal/parse"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("the collapsed proposal leaked %q:\n%s", hidden, text)
		}
	}
	// THE ASKING, in the block's own grammar: the head, the summary, one row per
	// answer, and the clock saying which answer silence takes and when.
	ask := taskAsk(a)
	for _, want := range []string{
		session.TaskProposalLead + "Fix the nil-map crash",
		"1  start it",
		"2  no",
		"start it in 4s",
	} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the question is missing %q:\n%s", want, ask)
		}
	}
	// IT IS A QUESTION, SO IT TAKES THE QUESTION HUE — the same violet the
	// approval question spends and nothing else on this surface does. The card is
	// found rather than assumed to lead the frame: the conversation's opening
	// breath (render.go's [app.layout]) is a blank row above everything.
	painted := ""
	for _, r := range a.visible(a.bodyWidth()) {
		if strings.TrimSpace(r.text) != "" {
			painted = r.text
			break
		}
	}
	if !strings.Contains(painted, sgr256(hueAsk)) {
		t.Fatalf("the proposal is not painted in the question hue:\n%q", painted)
	}
	// This proposal has a clock: it starts automatically unless redirected.
	if word, _ := a.stateWord(); word != taskStartingWord {
		t.Fatalf("the status word is %q while a proposal is open", word)
	}

	// The expansion is the tool rows' own mechanic: ctrl+e on an empty draft,
	// and a click anywhere on the card.
	drive(t, a, ctrlKey('e'))
	text = taskText(a)
	for _, want := range []string{"builds its map lazily", "done when: go test ./internal/parse"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the opened proposal is missing %q:\n%s", want, text)
		}
	}
	clickHit(t, a, hitTask)
	if strings.Contains(taskText(a), "builds its map lazily") {
		t.Fatalf("a second click did not close the brief:\n%s", taskText(a))
	}
}

// V1: A proposal using the fresh-profile countdown announces the full fifteen
// seconds, in the words of the answer that is about to be taken.
func TestTheDefaultTaskCountdownRendersFifteenSeconds(t *testing.T) {
	a, _, _ := taskApp(t)
	window := time.Duration(config.DefaultTaskAutoApprove) * time.Second
	askTask(t, a, 7, window)
	if !strings.Contains(taskAsk(a), "start it in 15s") {
		t.Fatalf("default proposal does not announce 15s:\n%s", taskAsk(a))
	}
}

// THE COUNTDOWN TICKS ON THE FRAME CLOCK — no ticker of its own — and at the
// deadline the question STOPS BEING ONE: the engine's clock owns the answer, and
// a row still counting down would be a question nobody can answer any more.
func TestTheProposalCountdownTicksAndStopsAtTheDeadline(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 4*time.Second)

	if !strings.Contains(taskAsk(a), "start it in 4s") {
		t.Fatalf("the countdown did not open at 4s:\n%s", taskAsk(a))
	}
	advance(3200 * time.Millisecond)
	drive(t, a, frameMsg{})
	if !strings.Contains(taskAsk(a), "start it in 1s") {
		t.Fatalf("the countdown did not tick down:\n%s", taskAsk(a))
	}
	if !a.awaitingTask() {
		t.Fatal("the proposal stopped asking before its deadline")
	}
	advance(time.Second)
	drive(t, a, frameMsg{})
	if a.awaitingTask() {
		t.Fatalf("the proposal is still asking past its deadline:\n%s", taskText(a))
	}
	if !strings.Contains(taskText(a), taskClockWord) {
		t.Fatalf("the settled row does not keep the clock's verdict:\n%s", taskText(a))
	}
	// AND THE QUESTION IS WITHDRAWN RATHER THAN ANSWERED. Nobody decided
	// anything, so nothing is recorded as though somebody had, and the reason is
	// what the row promised would happen.
	if a.questioning() {
		t.Fatalf("the block is still asking past the deadline:\n%s", taskAsk(a))
	}
	if !strings.Contains(taskAsk(a), taskStartedItselfReason) {
		t.Fatalf("the withdrawn line does not say what happened:\n%s", taskAsk(a))
	}
	// The clock is the ENGINE's: the surface stops asking and answers nothing.
	if len(agent.answered) != 0 {
		t.Fatalf("the surface raced the engine's clock: %+v", agent.answered)
	}
}

// V2: The first typed rune holds the proposal for this surface and every
// watcher, and erasing the draft cannot restore the old clock.
func TestTypingHoldsTheProposalClock(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 15*time.Second)
	drive(t, a, key("x"))

	if len(agent.held) != 1 || agent.held[0] != 7 {
		t.Fatalf("first rune held proposals %v, want [7]", agent.held)
	}
	if !a.task.deadline.IsZero() || strings.Contains(taskAsk(a), "start it in") {
		t.Fatalf("a typed rune did not stop the countdown:\n%s", taskAsk(a))
	}
	card := a.task
	held := proposal(a, 7, 0)
	drive(t, a, streamEventMsg{gen: a.gen, ev: held})
	if a.task != card || a.input.String() != "x" {
		t.Fatalf("the engine's hold update replaced the card or its draft")
	}

	watcher, _, watcherAdvance := taskApp(t)
	drive(t, watcher,
		streamEventMsg{gen: watcher.gen, ev: proposal(watcher, 7, 15*time.Second)},
		streamEventMsg{gen: watcher.gen, ev: proposal(watcher, 7, 0)},
	)
	if strings.Contains(taskAsk(watcher), "start it in") {
		t.Fatalf("a watching surface kept the old countdown:\n%s", taskAsk(watcher))
	}

	drive(t, a, key("backspace"))
	advance(16 * time.Second)
	watcherAdvance(16 * time.Second)
	drive(t, a, frameMsg{})
	if a.input.String() != "" || !a.awaitingTask() || len(agent.answered) != 0 {
		t.Fatalf("deleting after hold restarted or answered the proposal: draft=%q awaiting=%v answers=%+v",
			a.input.String(), a.awaitingTask(), agent.answered)
	}
	if strings.Contains(taskAsk(a), "start it in") {
		t.Fatalf("a held proposal started counting again:\n%s", taskAsk(a))
	}
}

// V2: A clipboard edit holds the same proposal clock as a typed rune, and the
// old deadline cannot take the waiting question away.
func TestPastingHoldsTheProposalClock(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 15*time.Second)
	drive(t, a, tea.PasteMsg{Content: "wait"})

	if len(agent.held) != 1 || agent.held[0] != 7 {
		t.Fatalf("paste held proposals %v, want [7]", agent.held)
	}
	if a.input.String() != "wait" || !a.task.deadline.IsZero() {
		t.Fatalf("paste left draft=%q deadline=%v", a.input.String(), a.task.deadline)
	}
	advance(16 * time.Second)
	drive(t, a, frameMsg{})
	if !a.awaitingTask() || len(agent.answered) != 0 || strings.Contains(taskAsk(a), "start it in") {
		t.Fatalf("pasted proposal did not remain waiting past its old deadline: awaiting=%v answers=%+v\n%s",
			a.awaitingTask(), agent.answered, taskAsk(a))
	}
}

// V3: Once typing has held the clock, the answers still mean exactly what the
// row says they mean.
func TestHeldProposalAnswersKeepTheirMeanings(t *testing.T) {
	t.Run("the decline is a digit", func(t *testing.T) {
		a, agent, _ := taskApp(t)
		agent.pending = []uint64{7}
		askTask(t, a, 7, 15*time.Second)
		drive(t, a, key("x"), key("backspace"), key("2"))
		if len(agent.answered) != 1 || agent.answered[0].answer.Approved {
			t.Fatalf("the decline after hold = %+v", agent.answered)
		}
	})

	t.Run("text redirects", func(t *testing.T) {
		a, agent, _ := taskApp(t)
		agent.pending = []uint64{7}
		askTask(t, a, 7, 15*time.Second)
		for _, typed := range "use the flag" {
			drive(t, a, key(string(typed)))
		}
		drive(t, a, key("enter"))
		if len(agent.answered) != 1 || !agent.answered[0].answer.Approved ||
			agent.answered[0].answer.Redirect != "use the flag" {
			t.Fatalf("text after hold = %+v", agent.answered)
		}
	})

	t.Run("empty enter takes the pick", func(t *testing.T) {
		a, agent, _ := taskApp(t)
		agent.pending = []uint64{7}
		askTask(t, a, 7, 15*time.Second)
		drive(t, a, key("x"), key("backspace"), key("enter"))
		if len(agent.answered) != 1 || !agent.answered[0].answer.Approved ||
			agent.answered[0].answer.Redirect != "" {
			t.Fatalf("empty enter after hold = %+v", agent.answered)
		}
	})
}

// EVERY SENTENCE IN THE BOX IS A CORRECTION, INCLUDING THE ONES THAT LOOK LIKE
// ANSWERS.
//
// This block used to keep a dialect: a bare `no`, `nope`, `n`, `stop`, `cancel`,
// `don't` typed into the box declined, and `yes`, `y`, `ok`, `okay`, `go`,
// `sure` approved — thirteen words, none of them drawn anywhere, each one a
// different answer from the sentence that merely began with it. The answers are
// on the row with their keys now, so the box is words: what a person types is a
// correction, and a correction is a yes to the corrected version.
func TestEveryTypedSentenceIsACorrectionAndNotAHiddenAnswer(t *testing.T) {
	// The words are chosen not to open with a key the question DRAWS: `c` is
	// `[c] change` and `?` is `[?] ask back` over an empty box, which is the
	// trade every letter on this block is held to (question.go's key grammar).
	// Everything else is a letter.
	for _, text := range []string{"no", "nope", "stop", "don't", "yes", "ok", "sure", "no, use the flag"} {
		t.Run(text, func(t *testing.T) {
			a, agent, _ := taskApp(t)
			agent.pending = []uint64{7}
			askTask(t, a, 7, 15*time.Second)
			for _, typed := range text {
				drive(t, a, key(string(typed)))
			}
			drive(t, a, key("enter"))
			if len(agent.answered) != 1 {
				t.Fatalf("%q answered %d proposals", text, len(agent.answered))
			}
			got := agent.answered[0].answer
			if !got.Approved || got.Redirect != text {
				t.Fatalf("%q = %+v, want approved with the words verbatim", text, got)
			}
			if agent.answered[0].id != 7 || len(agent.sent) != 0 || a.input.String() != "" {
				t.Fatalf("%q leaked after answering: draft=%q sent=%v answers=%+v",
					text, a.input.String(), agent.sent, agent.answered)
			}
		})
	}
}

// THE INPUT BOX IS THE CORRECTION LANE. Typing is a correction that reaches the
// engine as the person wrote it; a bare enter takes the pick the clock is about
// to take anyway.
func TestTheRedirectLaneReachesResolveTask(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 4*time.Second)

	// The box says what it is for while the question is open.
	block, _, _, _ := a.chrome(a.width)
	if !strings.Contains(plain(strings.Join(block, "\n")), taskRedirectLane) {
		t.Fatalf("the input box does not offer the correction lane:\n%s", plain(strings.Join(block, "\n")))
	}

	a.input.setText("leave the tests alone")
	drive(t, a, key("enter"))
	if len(agent.answered) != 1 {
		t.Fatalf("enter resolved %d proposals, want 1", len(agent.answered))
	}
	got := agent.answered[0]
	if got.id != 7 || !got.answer.Approved || got.answer.Redirect != "leave the tests alone" {
		t.Fatalf("the redirect reached the engine as %+v", got)
	}
	if !strings.Contains(taskText(a), taskRedirectWord) {
		t.Fatalf("the settled row does not say it was redirected:\n%s", taskText(a))
	}
	// The sentence was about the question, so it does not stay in the box to be
	// sent to the model by the next enter.
	if a.input.String() != "" {
		t.Fatalf("the draft kept the redirect: %q", a.input.String())
	}
	if len(agent.sent) != 0 {
		t.Fatalf("the redirect was also sent to the model: %v", agent.sent)
	}

	// A bare enter is approval as briefed, on a proposal that has a pick.
	agent.pending = []uint64{8}
	askTask(t, a, 8, 4*time.Second)
	drive(t, a, key("enter"))
	last := agent.answered[len(agent.answered)-1]
	if last.id != 8 || !last.answer.Approved || last.answer.Redirect != "" {
		t.Fatalf("a bare enter reached the engine as %+v", last)
	}
	// A proposal with no clock draws no countdown: zero is a clock that is off,
	// and a number counting down to nothing is a promise the engine did not
	// make. What stands there instead is the one word every question that is
	// waiting on somebody says (question.go's [app.questionHeldWord]).
	agent.pending = []uint64{9}
	askTask(t, a, 9, 0)
	ask := taskAsk(a)
	if strings.Contains(ask, "start it in") || strings.Contains(ask, meterFull) {
		t.Fatalf("a proposal with no deadline drew a countdown:\n%s", ask)
	}
	if !strings.Contains(ask, session.ConsentWaiting) {
		t.Fatalf("a proposal with no deadline does not say it is waiting:\n%s", ask)
	}
}

// A CORRECTION MAY BEGIN WITH ANY LETTER. This drives the same key router the
// terminal does, from the empty box through ResolveTask, so a future bare chord
// cannot silently eat the first rune again.
func TestARedirectStartingWithRReachesResolveTaskVerbatim(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 15*time.Second)

	const redirect = "run tests first"
	for _, typed := range redirect {
		drive(t, a, key(string(typed)))
	}
	drive(t, a, key("enter"))

	if len(agent.answered) != 1 {
		t.Fatalf("typed redirect resolved %d proposals, want 1", len(agent.answered))
	}
	got := agent.answered[0]
	if got.id != 7 || !got.answer.Approved || got.answer.Redirect != redirect {
		t.Fatalf("typed redirect reached ResolveTask as %+v, want %q verbatim", got, redirect)
	}
}

// THE ANSWERS ARE ON THE BLOCK AND THEY ARE REACHABLE BY DIGIT OR BY POINTER —
// the one key grammar every question on this surface takes. Settled, the block
// in the transcript collapses to its head and keeps both halves of what
// happened: the answer that was chosen and what it came to.
func TestTheProposalChoicesAnswerByPointerAndByKey(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 4*time.Second)

	// A LETTER IS TEXT WHILE THE BOX HAS WORDS IN IT — "no, keep the tests" must
	// not decline the very thing it is correcting.
	drive(t, a, key("n"), key("o"))
	if a.input.String() != "no" {
		t.Fatalf("the box lost its letters to the answers row: %q", a.input.String())
	}
	if len(agent.answered) != 0 {
		t.Fatalf("typing into the box answered the question: %+v", agent.answered)
	}
	a.input.reset()

	// The digit answers, and it is the option's own key.
	drive(t, a, key("1"))
	if len(agent.answered) != 1 || !agent.answered[0].answer.Approved {
		t.Fatalf("1 did not start the work: %+v", agent.answered)
	}
	// SETTLED, THE BLOCK IS TWO ROWS: the head, and the answer beside the verdict.
	text := taskText(a)
	if !strings.Contains(text, "start it · "+taskApprovedWord) {
		t.Fatalf("the settled block does not keep the answer and its verdict:\n%s", text)
	}
	// AND THE RECEIPT IS ABOVE THE BOX, in the words the engine writes into
	// decisions.jsonl (question.go's [app.recordQuestion]).
	if !strings.Contains(taskAsk(a), "decided "+session.TaskProposalLead+"Fix the nil-map crash") {
		t.Fatalf("the answer left no receipt:\n%s", taskAsk(a))
	}

	// THE POINTER: a press on the answers row is that row's, resolved against the
	// spans the renderer published — one layout, one set of targets.
	agent.pending = []uint64{8}
	askTask(t, a, 8, 4*time.Second)
	x, y := taskAnswerAt(t, a, 1)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	last := agent.answered[len(agent.answered)-1]
	if last.id != 8 || last.answer.Approved || last.answer.Redirect != "" {
		t.Fatalf("a click on the decline reached the engine as %+v", last)
	}
	if !strings.Contains(taskText(a), "no · "+taskDeclinedWord) {
		t.Fatalf("the declined block does not keep the answer it was declined with:\n%s", taskText(a))
	}
}

// ONE PROPOSAL IS ONE BLOCK AND ONE RECEIPT, however many times the engine
// sends it.
//
// THIS IS A DEFECT A REAL SCREEN FOUND. Answering holds the clock — the block
// tells the engine on the first key a question reads — and holding rebroadcasts
// the notice ([session.Agent.HoldTask]), so the surface hears about the proposal
// again a moment AFTER it answered. It drew a second block, raised the question a
// second time, and then wrote a second receipt when the engine's own answer came
// back to a question that was open again: one proposal, two blocks, two receipts,
// all from one keystroke.
func TestARebroadcastProposalIsNotASecondProposal(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 15*time.Second)
	drive(t, a, key("1"))

	// The hold that the answer's own keystroke caused, coming back.
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 0)})

	blocks := 0
	for _, e := range a.entries {
		if e.kind == entryTask && e.card != nil {
			blocks++
		}
	}
	if blocks != 1 {
		t.Fatalf("one proposal drew %d blocks:\n%s", blocks, taskText(a))
	}
	if a.questioning() {
		t.Fatalf("the answered proposal is asking again:\n%s", taskAsk(a))
	}
	if len(a.questionRecords) != 1 {
		t.Fatalf("one answer left %d receipts:\n%s", len(a.questionRecords), taskAsk(a))
	}
	if len(agent.answered) != 1 {
		t.Fatalf("one answer reached the engine %d times: %+v", len(agent.answered), agent.answered)
	}
}

// taskAnswerAt is the screen position of one of the question's answers — the
// same spans the click resolves through, which is the point: a test that
// computed its own columns would be testing a second layout.
func taskAnswerAt(t *testing.T, a *app, want int) (int, int) {
	t.Helper()
	a.questionRows(a.width)
	for _, band := range a.questionBands {
		if band.at == want {
			return band.span.from, chromeRowY(t, a, band.row)
		}
	}
	for _, span := range a.questionSpans {
		if span.at == want {
			return span.from, chromeRowY(t, a, a.questionSpanRow)
		}
	}
	t.Fatalf("the block offers no answer %d:\n%s", want, taskAsk(a))
	return 0, 0
}

// THE BLOCK IS CONTAINED. A question with a foot on it and the next paragraph
// starting on the row underneath would be a question the reply is inside of, so
// the layout puts a blank after the block — and after the CARD a landed node
// writes (taskdone.go), which closes something in the same way.
func TestTheProposalBlockAndTheLandedCardEndInABlank(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)},
		key("esc"),
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone, session.TaskNotice{
			Elapsed: 8 * time.Second, Merge: mergeWordMerged,
		})},
	)
	rows := a.visible(a.bodyWidth())
	foot := -1
	for i, r := range rows {
		if strings.HasPrefix(strings.TrimSpace(plain(r.text)), taskFootCorner) {
			foot = i
		}
	}
	if foot < 0 {
		t.Fatalf("the settled block has no foot:\n%s", taskText(a))
	}
	if foot+1 >= len(rows) || strings.TrimSpace(plain(rows[foot+1].text)) != "" {
		t.Fatalf("the block runs straight into what follows it:\n%s", taskText(a))
	}
	// THE CARD IS FOUND BY ITS HIT and not by its words: it is a block of two
	// rows now, and what this test owns is the blank under the LAST of them.
	//
	// THE HEAD IS SPELLED THE WAY THIS WAVE SPELLS IT, and both halves of that
	// moved on 2026-09-03. The name arrives WHOLE — taskident.go's [taskTitleOf]
	// stopped cutting to three words, because a fitter cannot give back cells
	// spent before it was asked (rowfit.go's first law) — and the span is joined
	// to the state word with ` · ` like every other fact on the row, because
	// `done 8s` fuses two separate facts into one phrase (taskdone.go's
	// [app.doneTail] states the whole of it).
	if !strings.Contains(taskText(a), "Fix the nil-map crash · "+doneWord+" · 8s · "+mergeWordMerged) {
		t.Fatalf("the landed card is not in the transcript:\n%s", taskText(a))
	}
	// The card is the last entry here, so what it owes the next one is asserted
	// by putting one after it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTextDelta, Text: "and now the reply."}}, frameMsg{})
	rows = a.visible(a.bodyWidth())
	last := -1
	for i, r := range rows {
		if r.hit == hitDone {
			last = i
		}
	}
	if last < 0 {
		t.Fatalf("the landed card left no rows of its own:\n%s", taskText(a))
	}
	if last+1 >= len(rows) || strings.TrimSpace(plain(rows[last+1].text)) != "" {
		t.Fatalf("the landed card runs straight into the next entry:\n%s", taskText(a))
	}
}

// ESC IS LATER, AND IT IS NOT THE TURN'S EITHER.
//
// It used to decline the proposal, which was the honest reading of "get this off
// my screen" while the block was modal and there was no other way out of it.
// There is one now (question.go): the rows fold to the chip, the proposal stays
// open, the engine stays waiting, and NOTHING IS DECIDED BY MAKING SOMETHING GO
// AWAY. The decline is `2`, which is drawn on the row beside it.
func TestEscFoldsTheProposalRatherThanDecliningItOrTheTurn(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	a.state = stateWorking
	askTask(t, a, 7, 4*time.Second)
	drive(t, a, key("esc"))

	if len(agent.answered) != 0 {
		t.Fatalf("esc answered the proposal: %+v", agent.answered)
	}
	if agent.stops != 0 {
		t.Fatal("esc interrupted the turn")
	}
	if !a.awaitingTask() || a.questionCount() != 1 {
		t.Fatalf("esc closed the proposal: awaiting=%v open=%d", a.awaitingTask(), a.questionCount())
	}
	// The rows are off the screen and the chip is counting it, which is the
	// whole of what folding is.
	if strings.Contains(taskAsk(a), "1  start it") {
		t.Fatalf("the folded question is still drawing its answers:\n%s", taskAsk(a))
	}
	if seg := plain(a.questionSegment()); !strings.Contains(seg, "1 question") {
		t.Fatalf("the chip does not carry the folded question: %q", seg)
	}
	// And the decline is the digit the row draws.
	a.raiseFolded()
	taskText(a)
	settleAsk(a)
	drive(t, a, key("2"))
	if len(agent.answered) != 1 || agent.answered[0].answer.Approved {
		t.Fatalf("2 did not decline the proposal: %+v", agent.answered)
	}
	if !strings.Contains(taskText(a), taskDeclinedWord) {
		t.Fatalf("the declined row does not keep its verdict:\n%s", taskText(a))
	}
}

// THE RAIL IS THE PLACE, NOT THE PRESENCE: it stands from the session's first
// frame, says what it is for while it is empty, and work fills it rather than
// raising it (task.go's [app.railShowing]).
func TestTheRailStandsWhileWorkIsAliveAndGoesWhenItLands(t *testing.T) {
	a, _, advance := taskApp(t)
	if !a.railShowing() {
		t.Fatal("an empty session drew no rail")
	}
	if a.bodyWidth() != 200-railCols {
		t.Fatalf("the empty column is not charged against the conversation: body=%d", a.bodyWidth())
	}
	// AND IT SAYS WHAT IT IS FOR. Thirty blank columns beside a paragraph read as
	// a rendering fault, so the empty column carries its one dim label.
	if rail := plain(strings.Join(a.railRows(10), "\n")); strings.Contains(rail, "no tasks yet") {
		t.Fatalf("the empty column announces its emptiness:\n%s", rail)
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})})
	if !a.railShowing() {
		t.Fatal("a running node did not keep the rail standing")
	}
	// The label leaves with the emptiness: a column with a row in it needs no
	// explanation of itself.
	if rail := plain(strings.Join(a.railRows(10), "\n")); strings.Contains(rail, "no tasks yet") {
		t.Fatalf("the label outlived the emptiness it explains:\n%s", rail)
	}
	advance(12 * time.Second)
	rail := plain(strings.Join(a.railRows(10), "\n"))
	// THE ROW CARRIES THE NAME, not the whole title: taskident.go cuts the
	// engine's sentence to the two-or-three-word label this column is wide enough
	// to read.
	if !strings.Contains(rail, "Fix the nil-map") || !strings.Contains(rail, "12s") {
		t.Fatalf("the running node is not on the rail with its clock:\n%s", rail)
	}

	// A queued node is a hollow circle, a failure is the bad glyph, and a node
	// whose branch did not come home keeps the branch name.
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(8, "Mix audio", session.TaskQueued, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(9, "Collect sources", session.TaskFailed, session.TaskNotice{
			Report: "the tests did not build", Merge: mergeWordAborted, Branch: "task/collect",
		})},
		streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone, session.TaskNotice{
			Elapsed: 130 * time.Second, Merge: mergeWordConflicted, Branch: "task/fix-nil-map",
		})},
	)
	// A retained report expands on request, without demanding a merge.
	a.railSetOpen(a.tasks[9], true)
	// A BRANCH NAME IS NEVER ELLIPSIZED: the conflicted sentence wraps inside
	// the rail rather than losing the one handle back to the work, so the
	// assertion is on the two halves and not on one line.
	rail = plain(strings.Join(a.railRows(16), "\n"))
	// ONE GLYPH OPENS EVERY ROW AND IT IS THE STATE. The identity ◆ is not on this
	// column: it is the same cell on every task, this column holds nothing but
	// tasks, and the two cells belong to the name here (task.go's [app.railLead]).
	for _, want := range []string{
		glyphQueued + " Mix audio",
		glyphBad + " Collect sources",
		glyphDone + " Fix the nil-map",
		"conflicted ·", "task/fix-nil-map",
		// A STOPPED NODE DID NOT CRASH. session marks its branch "aborted"; the
		// rail says what that is — it stopped, and the work is still on the branch
		// named beside it.
		// A NODE WHOSE BRANCH NEVER CAME HOME DID NOT CRASH. session marks it
		// "aborted"; the rail says the reading's own sentence about it and names
		// the branch the work is still on beside it.
		"task/collect"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the rail is missing %q:\n%s", want, rail)
		}
	}
	if strings.Contains(rail, mergeWordAborted) {
		t.Fatalf("the rail read the engine's own word for a stopped node:\n%s", rail)
	}

	// A node whose branch CAME HOME keeps its row: the column is the session's
	// record of its own work now, and "where did that task go" is the commonest
	// question asked of it. What it stops saying is anything more than its name.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Mix audio", session.TaskDone, session.TaskNotice{
		Elapsed: 8 * time.Second, Merge: mergeWordMerged,
	})})
	if !strings.Contains(plain(strings.Join(a.railRows(12), "\n")), "Mix audio") {
		t.Fatal("a merged node left the roster's record")
	}
	if !a.railShowing() {
		t.Fatal("the rail left while two kept branches were still on it")
	}
	// The two kept branches are what remains, and dealing with them is the
	// person's business — this test only owns the empty case, so it drops them
	// the way /new does. The column does NOT leave with them: the place is
	// permanent, and an emptied column is back to saying what it is for.
	a.dropTasks()
	if !a.railShowing() {
		t.Fatal("/new took the column down with the nodes")
	}
	if rail := plain(strings.Join(a.railRows(10), "\n")); strings.Contains(rail, "no tasks yet") {
		t.Fatalf("the emptied column announces its emptiness:\n%s", rail)
	}
}

// A NODE'S END IS HISTORY, so it lands in the transcript — once, however many
// lanes carried it. The de-dup is (id, state), because an update raised inside a
// turn arrives on the turn's stream AND on the standing subscription.
//
// WHAT LANDS IS A CARD (taskdone.go), not the one dim sentence this surface used
// to leave, and every fact the sentence carried is asserted here in the card's
// own grammar: the outcome and the elapsed on the head, the engine's own words
// quoted underneath.
//
// The landings are SEPARATED BY A REPLY because a contiguous run of more than
// two cards rolls up into one object (taskdone.go's [doneRollupFloor]), and what
// this test owns is the wording of a card rather than the shape of a batch.
func TestALandedNodeWritesOneCardWhateverLaneCarriedIt(t *testing.T) {
	a, agent, _ := taskApp(t)
	// say is the reply that closes one landing off from the next.
	say := func(text string) {
		drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventTextDelta, Text: text}}, frameMsg{})
	}
	done := update(7, "Fix the nil-map crash", session.TaskDone, session.TaskNotice{
		Elapsed: 130 * time.Second, Merge: mergeWordMerged,
	})
	// The same event, on both lanes, exactly as internal/session emits it.
	cmd := a.watchTasks()
	agent.updates <- done
	drive(t, a, append(runCmd(cmd), streamEventMsg{gen: a.gen, ev: done})...)

	text := taskText(a)
	// THE NAME IS WHOLE AND THE SPAN IS A FACT OF ITS OWN, both since 2026-09-03:
	// taskident.go's [taskTitleOf] no longer cuts a title to three words before
	// any width is known, and taskdone.go's [app.doneTail] joins the span to the
	// state word with the row's own separator rather than with a bare space.
	want := "Fix the nil-map crash · " + doneWord + " · " + taskSpanWord(130*time.Second) + " · " + mergeWordMerged
	if strings.Count(text, want) != 1 {
		t.Fatalf("the transcript holds %d copies of %q:\n%s", strings.Count(text, want), want, text)
	}

	// A failure says why, in the report's first line, and it says it in the
	// node's own words — which is why they are in quotes.
	say("looking at the next one.")
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Collect sources", session.TaskFailed, session.TaskNotice{
		Elapsed: 4 * time.Second, Report: "the tests did not build\nsee the log",
	})})
	for _, want := range []string{
		// `failed` is deleted as a landing's word: the head reads `incomplete` and
		// the row under it says why, in the engine's own sentence for a fault
		// (docs/design/task-states/DESIGN.md).
		"Collect sources · " + taskIncompleteState + " · " + taskSpanWord(4*time.Second),
		"a fault: the tests did not build",
	} {
		if !strings.Contains(taskText(a), want) {
			t.Fatalf("the failure card does not carry %q:\n%s", want, taskText(a))
		}
	}
	if strings.Contains(taskText(a), "see the log") {
		t.Fatalf("the collapsed card leaked the rest of the report:\n%s", taskText(a))
	}

	// A NODE THAT KEPT ITS BRANCH SAYS SO AS A FACT AND NOT AS A STATE. The head
	// carries the word, the file count and `branch kept · <branch>`; the row under
	// it carries the engine's own account of what went wrong, and the fused
	// `stopped — branch kept` is gone from the card altogether
	// (docs/design/task-states/DESIGN.md).
	say("and the audio.")
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(11, "Mix audio", session.TaskFailed, session.TaskNotice{
		Elapsed: 90 * time.Second, Report: "stopped: 40 steps and no finish",
		Merge: mergeWordAborted, Branch: "task/mix",
	})})
	for _, want := range []string{
		"Mix audio · " + taskIncompleteState + " · " + taskSpanWord(90*time.Second) +
			" · " + taskBranchKept + " · task/mix",
		"stopped: 40 steps and no finish",
	} {
		if !strings.Contains(taskText(a), want) {
			t.Fatalf("the kept-branch card does not carry %q:\n%s", want, taskText(a))
		}
	}
	for _, never := range []string{mergeWordAborted, "stopped — branch kept"} {
		if strings.Contains(taskText(a), never) {
			t.Fatalf("the card says %q, which is not a card's word any more:\n%s", never, taskText(a))
		}
	}
	// And a node that ended with nothing to say still says something: an ending
	// this surface was told nothing about is a fault, in the engine's bare word
	// for one, rather than an empty row.
	say("and the titles.")
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(12, "Render titles", session.TaskFailed, session.TaskNotice{
		Elapsed: 3 * time.Second,
	})})
	if !strings.Contains(taskText(a), "Render titles · "+taskIncompleteState) {
		t.Fatalf("a silent failure does not land as %q:\n%s", taskIncompleteState, taskText(a))
	}
}

// THE RAIL IS TREE-READY. v1's graph has no edges, so nothing draws one — but a
// node whose prerequisites are unmet says so in v1's own sentence, from the
// DependsOn the row model already stores.
func TestTheRailNamesWhatABlockedNodeWaitsOn(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(2, "Mix audio", session.TaskQueued, session.TaskNotice{
			DependsOn: []uint64{1},
		})},
	)
	// A NODE WITH NO FAMILY AROUND IT IS THE FLAT ROW THIS COLUMN ALWAYS DREW, and
	// the sentence under it is the only place the surface says what is in the way
	// (task.go's [app.railSaysMore]).
	rail := plain(strings.Join(a.railRows(10), "\n"))
	if !strings.Contains(rail, "Mix audio") {
		t.Fatalf("the blocked node is not on the roster:\n%s", rail)
	}
	if !strings.Contains(rail, "waits: Collect sources") {
		t.Fatalf("a blocked node does not say what it waits on:\n%s", rail)
	}
	// The prerequisite finishing takes the sentence away rather than leaving a
	// node waiting on work that is over.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{
		Merge: mergeWordMerged,
	})})
	if strings.Contains(plain(strings.Join(a.railRows(10), "\n")), "waits:") {
		t.Fatalf("the wait outlived the work it waited on:\n%s", plain(strings.Join(a.railRows(10), "\n")))
	}
}

// underWidth is the cells a node's under-block actually gets at a rail width:
// the column less its seam ([app.railRoom]), less the two-cell indent every
// under-row is drawn behind ([app.railNodeRows]). The telemetry tests measure
// against it rather than against the column, because a test that asserted a row
// at railCols would be asserting four cells the row never had.
func underWidth(cols int) int { return cols - ansi.StringWidth(railSeam) - 2 }

// THE TELEMETRY ROW IS WHAT A RUNNING NODE COSTS, said under its own name: its
// age, its weight, its price and its worker — richest first, and given up from
// the right until the row fits the column it is in.
func TestARunningRowSaysItsAgeWeightPriceAndModel(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning,
		session.TaskNotice{Model: "openai/gpt-5", CostUSD: 0.31})})
	node := a.tasks[7]
	// The weight is the live-usage branch's field; this test is the rail's half
	// of that contract and writes it the way an update will.
	node.tokens = 9_900
	advance(42 * time.Second)

	// ONE RULE AT EVERY WIDTH: the full column carries all four segments, the
	// slim one gives up the model, and a column narrower than either gives up the
	// price and then the weight. The age is the segment that never goes.
	for _, tc := range []struct {
		width int
		want  string
	}{
		{underWidth(railCols), "42s · 9.9k · $0.31 · gpt-5"},
		{underWidth(railSlimCols), "42s · 9.9k · $0.31"},
		{17, "42s · 9.9k"},
		{9, "42s"},
	} {
		if got := plain(strings.Join(a.railUnder(node, tc.width), "\n")); got != tc.want {
			t.Fatalf("at %d cells the telemetry row is %q, want %q", tc.width, got, tc.want)
		}
	}

	// A LIVE CALL TAKES THE ROW ABOVE IT, and the two together are the whole of
	// the under-block: what the node is doing this second, then what it has spent
	// getting there.
	node.tool, node.toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)
	rows := a.railUnder(node, underWidth(railCols))
	if len(rows) != railUnderRows {
		t.Fatalf("a working node drew %d under-rows, want %d:\n%q", len(rows), railUnderRows, rows)
	}
	if got := plain(rows[0]); got != "bash go test ./... · 24s" {
		t.Fatalf("the call row is %q", got)
	}
	if got := plain(rows[1]); got != "42s · 9.9k · $0.31 · gpt-5" {
		t.Fatalf("the telemetry row is %q under a live call", got)
	}
	node.tool, node.toolBegan = "", time.Time{}

	// ABSENCE IS NOTHING, NEVER A ZERO. An engine that has published no usage
	// leaves the two figures off the row rather than claiming the node has burned
	// nothing and cost nothing.
	node.tokens, node.cost = 0, 0
	if got := plain(strings.Join(a.railUnder(node, underWidth(railCols)), "\n")); got != "42s · gpt-5" {
		t.Fatalf("an unmeasured node draws %q, want no figures at all", got)
	}
	if strings.Contains(rosterText(a, 12), "$0.00") {
		t.Fatalf("the roster priced a node nobody has priced:\n%s", rosterText(a, 12))
	}
	node.model = ""
	if got := plain(strings.Join(a.railUnder(node, underWidth(railCols)), "\n")); got != "42s" {
		t.Fatalf("a node nobody has said anything about draws %q, want its age alone", got)
	}
}

// A NODE THAT CAME HOME CLEAN SAYS WHAT IT COST — and the rows that are waiting
// on a PERSON say nothing but the thing they are waiting for.
func TestTheRosterPricesAMergeAndLeavesTheActionableRowsAlone(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(2, "Mix audio", session.TaskQueued, session.TaskNotice{
			DependsOn: []uint64{1},
		})},
		streamEventMsg{gen: a.gen, ev: update(3, "Fix the nil-map crash", session.TaskDone, session.TaskNotice{
			Merge: mergeWordConflicted, Branch: "task/fix-nil-map", CostUSD: 0.42,
		})},
		streamEventMsg{gen: a.gen, ev: update(4, "Render titles", session.TaskFailed, session.TaskNotice{
			Merge: mergeWordAborted, Branch: "task/render", CostUSD: 0.42,
		})},
		streamEventMsg{gen: a.gen, ev: update(5, "Port the parser", session.TaskUnverified, session.TaskNotice{
			CostUSD: 0.42,
		})},
		streamEventMsg{gen: a.gen, ev: update(6, "Cut the trailer", session.TaskDone, session.TaskNotice{
			Merge: mergeWordMerged, CostUSD: 0.42,
		})},
	)
	width := underWidth(railCols)

	// THE MERGE WORD ALWAYS SURVIVES, and the price rides behind it only where
	// there is room for both.
	if got := plain(strings.Join(a.railUnder(a.tasks[6], width), "\n")); got != mergeWordMerged+" · $0.42" {
		t.Fatalf("a merged node says %q, want its price beside the word", got)
	}
	if got := plain(strings.Join(a.railUnder(a.tasks[6], 8), "\n")); got != mergeWordMerged {
		t.Fatalf("a narrow column says %q, want the word alone", got)
	}
	a.tasks[6].cost = 0
	if got := plain(strings.Join(a.railUnder(a.tasks[6], width), "\n")); got != mergeWordMerged {
		t.Fatalf("an unpriced merge says %q, want the word alone", got)
	}

	// AND THE ROWS WITH A HANDLE ON THEM ARE UNCHANGED, to the byte. Each of
	// these carries the one thing a person needs to act — a branch to check out, a
	// node to wait for, a decision to make — and a price beside it would be a
	// figure competing with it. They WRAP at this width (task.go's [railWrap]), so
	// the assertion joins the block back up.
	for _, tc := range []struct {
		id   uint64
		want string
	}{
		{2, "waits: Collect sources"},
		{3, mergeWordConflicted + " · task/fix-nil-map"},
		// A LANDING THAT NAMED NO ENDING AND NOBODY STOPPED is a fault as far as
		// anyone can tell, and the row says so rather than claiming a stop nobody
		// made ([session.TaskReasonOf]).
		{4, taskRecordStoppedWord + " · " + session.TaskReasonOf("", "") + " · task/render"},
		// A your-call row reads the QUESTION and its reason, never a bare word:
		// the reason is the half a person can act on (tasktier.go).
		{5, tierYourCallWord + " · nobody could check it"},
	} {
		if got := plain(strings.Join(a.railUnder(a.tasks[tc.id], width), " ")); got != tc.want {
			t.Fatalf("node %d says %q, want %q", tc.id, got, tc.want)
		}
	}
}

// CHROME ACCOUNTING: the rail is charged against the CONVERSATION and against
// nothing else. The frame keeps its exact size, the status line spans the whole
// window, and below [railFloor] the conversation keeps every column it had.
func TestTheRailIsChargedAgainstTheConversationOnly(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})})

	for _, tc := range []struct {
		width int
		rail  bool
	}{{200, true}, {120, true}, {119, true}, {100, true}, {99, false}} {
		a.width = tc.width
		a.touch()
		if got := a.railShowing(); got != tc.rail {
			t.Fatalf("at %d columns the rail is %v, want %v", tc.width, got, tc.rail)
		}
		want := tc.width
		if tc.rail {
			want -= railColsFor(tc.width)
		}
		if got := a.bodyWidth(); got != want {
			t.Fatalf("at %d columns the conversation is %d wide, want %d", tc.width, got, want)
		}
		lines := strings.Split(plain(frame(a)), "\n")
		if len(lines) != a.height {
			t.Fatalf("at %d columns the frame is %d rows, want %d", tc.width, len(lines), a.height)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > tc.width {
				t.Fatalf("at %d columns frame row %d is %d wide:\n%q", tc.width, i, w, line)
			}
		}
		// The roster opens on the node itself — there are no headings any more, and
		// a session of one node is one family of one (task.go).
		// The column opens with its section label now (margin.go), so the node is
		// the row under it.
		//
		// EACH WIDTH IS ASSERTED IN ITS OWN SPELLING rather than in the prefix they
		// share. The name reaches this column WHOLE now (taskident.go's
		// [taskTitleOf] stopped cutting to three words on 2026-09-03, so the ROW
		// decides what it can afford), and twenty-one cells do not fit a slim
		// rail's title slot — so the full column draws the name and the slim one
		// draws as much of it as [railTitleFloor] leaves once the id is measured
		// out. Both are the roster naming the node, which is what this row of the
		// test is here to say.
		name := "Fix the nil-map crash"
		if railColsFor(tc.width) < railCols {
			name = "Fix the nil-ma"
		}
		top := a.bodyTop()
		if tc.rail && !strings.Contains(lines[top], marginTasksWord) {
			t.Fatalf("at %d columns the column does not open with its label:\n%q", tc.width, lines[top])
		}
		if tc.rail && !strings.Contains(lines[top+1], name) {
			t.Fatalf("at %d columns the roster's first row is not the node:\n%q", tc.width, lines[top+1])
		}
		// AND THE STRIP IS THE ROW ABOVE IT ONLY WHERE THERE IS NO ROSTER: the two
		// answer the same question, and the wide frame answers it in the column.
		// It is asked of the STRIP's own height rather than of [app.bodyTop], which
		// counts the conversation's own pinned bar as well (roomcrumbs.go).
		if tc.rail && a.stripHeight() != 0 {
			t.Fatalf("at %d columns the strip drew over the roster: %d rows", tc.width, a.stripHeight())
		}
		if !tc.rail && !strings.Contains(lines[a.headHeight()], "Fix the nil-map") {
			t.Fatalf("at %d columns the task strip is not the first row under the bar:\n%q",
				tc.width, lines[a.headHeight()])
		}
		// The status row is the whole window's, so it is never under the rail.
		status := lines[len(lines)-1]
		if strings.Contains(status, "│") {
			t.Fatalf("at %d columns the rail's seam reached the status row:\n%q", tc.width, status)
		}
	}
}

// ── THE ROSTER: A COLUMN THAT SURVIVES A LONG DAY ───────────────────────────
//
// The rail holds every node the session has admitted now, which is only useful
// if a hundred of them are still one readable column (task.go's roster section).
// These four tests are the whole of that claim: the order, the fold, the window,
// and the keyboard that reaches them.

// The key that hands the roster the keyboard is [ctrlT] (reasoning_test.go),
// which is the same chord the model picker spends on effort — and they never
// meet, because the picker is modal and the roster's guard stands down while it
// is up (task.go's [app.railKey]).

// rosterText is the column as a reader sees it. It is not spelled `roster`
// because that is the resume picker's own type now (resume.go).
func rosterText(a *app, height int) string {
	return plain(strings.Join(a.railRows(height), "\n"))
}

// THREE HUNDRED NODES ARE ONE COLUMN. The roster never draws more rows than the
// frame lent it, every row stays inside the column, and the window follows the
// focus down rather than stopping at whatever fitted first.

func TestTheRosterOrdersItsFamiliesByUrgencyAndCountsTheWhole(t *testing.T) {
	a, _, _ := taskApp(t)
	// Use the wide tier so this aggregate test can see every count.
	a.width, a.railWide = 160, true
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{
			Merge: mergeWordMerged,
		})},
		streamEventMsg{gen: a.gen, ev: update(2, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(3, "Mix audio", session.TaskQueued, session.TaskNotice{
			DependsOn: []uint64{2},
		})},
		streamEventMsg{gen: a.gen, ev: update(4, "Render titles", session.TaskFailed, session.TaskNotice{
			Report: "the merge conflicted", Merge: mergeWordConflicted, Branch: "task/render",
		})},
		streamEventMsg{gen: a.gen, ev: update(5, "Cut the trailer", session.TaskQueued, session.TaskNotice{})},
		// A FAILURE THAT KEPT NOTHING IS NOT A DEMAND, so it stands with the record
		// at the bottom of the column rather than at the top (task.go's
		// [app.railGroupOf]).
		streamEventMsg{gen: a.gen, ev: update(6, "Trim silence", session.TaskFailed, session.TaskNotice{
			Report: "the tests did not build",
		})},
	)
	a.cost, a.tokens = 1.42, 312_000
	rail := rosterText(a, 24)

	// The order IS the design: what is asking, what is running, what is waiting
	// for a slot, what is parked behind other work, what is over.
	at := -1
	for _, want := range []string{"Render titles", "Fix the nil-map", "Cut the trailer", "Mix audio",
		"Collect sources", "Trim silence"} {
		found := strings.Index(rail, want)
		if found < 0 {
			t.Fatalf("the roster has no %q row:\n%s", want, rail)
		}
		if found < at {
			t.Fatalf("%q is out of order:\n%s", want, rail)
		}
		at = found
	}
	// NO HEADINGS AT ALL. The five words live in the footer now, where they are
	// counts of the whole session rather than sections of the column.
	for g := railGroup(0); g < railGroupCount; g++ {
		if strings.Contains(rail, glyphOpen+" "+railGroupWords[g]) ||
			strings.Contains(rail, glyphShut+" "+railGroupWords[g]) {
			t.Fatalf("the roster still draws the %q heading:\n%s", railGroupWords[g], rail)
		}
	}
	// ONE GLYPH OPENS A ROW WITH NO FAMILY AROUND IT, and it is the state — the
	// same lead a family row has, so the column reads downward as one column of
	// states (task.go's [app.railLead]).
	if !strings.Contains(rail, glyphBad+" Render titles") {
		t.Fatalf("the flat row does not lead with its state:\n%s", rail)
	}
	if strings.Contains(rail, plain(a.taskMark(identFor(4)))+" Render titles") {
		t.Fatalf("the identity ◆ is back on the rail, two cells from the name:\n%s", rail)
	}
	// THE ID IS META: the title leads the row and the handle trails it, dim.
	if !strings.Contains(rail, "#2") || strings.Contains(rail, "#2 Fix") {
		t.Fatalf("the node's id is not the trailing meta of its row:\n%s", rail)
	}
	// AND THE FOOTER SAYS THE WHOLE, in the group vocabulary.
	for _, want := range []string{railSigma + "$1.42", "312k tok", "1 running", "1 needs you",
		"1 queued", "1 waiting", "2 done"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the footer does not say %q:\n%s", want, rail)
		}
	}
}

func TestTheRosterTakesTheKeyboardOnlyWhenItIsHandedIt(t *testing.T) {
	a, _, _ := roomApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{
			Merge: mergeWordMerged,
		})},
		streamEventMsg{gen: a.gen, ev: update(2, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
	)
	// The keyboard is the draft's until it is asked for, and the marker with it.
	drive(t, a, key("down"))
	if a.railHold {
		t.Fatal("the roster took the keyboard nobody handed it")
	}
	if strings.Contains(rosterText(a, 16), railMark) {
		t.Fatal("an unfocused roster drew a cursor")
	}

	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("ctrl+t did not hand the roster the keyboard")
	}
	if !strings.Contains(rosterText(a, 16), railMark) {
		t.Fatalf("the focused row has no marker:\n%s", rosterText(a, 16))
	}
	// The cursor opens on the first row, and the first row is the oldest of the
	// two running nodes — equal urgency, so the column is in admission order (the
	// session opened with node 7 running).
	if a.railWhere.id != 7 {
		t.Fatalf("the cursor opened on %+v, want the first running node", a.railWhere)
	}
	drive(t, a, key("down"))
	if a.railWhere.id != 2 {
		t.Fatalf("↓ walked to %+v, want the second running node", a.railWhere)
	}

	// Typing still reaches the box while the roster holds the arrows: only the
	// keys it named are taken. ("x" is not one to test with — it is the stop
	// key, and it is aimed at whatever the roster's cursor is standing on.)
	drive(t, a, key("z"))
	if a.input.String() != "z" {
		t.Fatalf("a letter did not reach the draft: %q", a.input.String())
	}
	// esc gives the keyboard back, and the cursor goes with it.
	drive(t, a, key("esc"))
	if a.railHold || strings.Contains(rosterText(a, 16), railMark) {
		t.Fatal("esc did not hand the keyboard back to the box")
	}

	// The pointer's half: a press on a row is that node's door, and it does not
	// take the keyboard on its way past.
	for y := a.bodyTop(); y < a.bodyTop()+a.viewHeight(); y++ {
		if node := a.railNodeAt(y); node != nil && node.id == 1 {
			drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + 4, Y: y, Button: tea.MouseLeft})
			break
		}
	}
	if !a.roomOpen() || a.room.id != 1 {
		t.Fatalf("a press on a roster row did not open its room: open=%v", a.roomOpen())
	}
	if a.railHold {
		t.Fatal("a click took the keyboard away from the box")
	}
}

func TestTheRostersCursorFollowsANodeThatChangesUrgency(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(2, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})},
		altT(),
		key("down"), // the newest running node
	)
	if a.railWhere.id != 2 {
		t.Fatalf("the cursor is on %+v, want the second running node", a.railWhere)
	}
	// It finishes with its branch conflicted, which is the one outcome that needs a
	// person — so the row moves to the top group, and the cursor moves with it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(2, "Fix the nil-map crash", session.TaskFailed,
		session.TaskNotice{Merge: mergeWordConflicted, Branch: "task/fix-nil-map"})})
	entries := a.railEntries()
	at := a.railFocusIndex(entries)
	if at < 0 || entries[at].node == nil || entries[at].node.id != 2 {
		t.Fatalf("the cursor did not follow the node: %+v", entries)
	}
	// AND THE ROW ITSELF MOVED: a conflicted branch is the one outcome that needs a
	// person, so it is the top of the column now (task.go's [app.railGroupOf]).
	if at != 0 {
		t.Fatalf("the node with a conflicted branch is at row %d, want the top of the column", at)
	}
}

func TestTheRosterWindowsHundredsOfNodesAroundItsFocus(t *testing.T) {
	a, _, _ := taskApp(t)
	for i := 1; i <= 300; i++ {
		a.taskUpdate(update(uint64(i), "node "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	rows := a.railRows(12)
	if len(rows) != 12 {
		t.Fatalf("the roster drew %d rows into a 12-row column", len(rows))
	}
	for i, line := range rows {
		if w := ansi.StringWidth(plain(line)); w > railCols {
			t.Fatalf("roster row %d is %d cells wide, want at most %d:\n%q", i, w, railCols, line)
		}
	}
	// Three hundred families of one, all equally urgent, so the column is in the
	// order the session admitted them — under the section label the column opens
	// with (margin.go).
	if !strings.Contains(plain(rows[1]), "node 1") {
		t.Fatalf("the first row is not the first node the session met:\n%q", rows[1])
	}

	// Twenty rows down is past the window, so the window moves.
	drive(t, a, altT())
	for i := 0; i < 20; i++ {
		drive(t, a, key("down"))
	}
	rail := rosterText(a, 12)
	if !strings.Contains(rail, "node 21") || !strings.Contains(rail, railMark) {
		t.Fatalf("the window did not follow the cursor down:\n%s", rail)
	}
	// AND WHAT IS RUNNING DID NOT GO WITH IT. The head of the column is pinned
	// (task.go's [app.railMovingHead]): a person who walks the cursor down into
	// the record must not take the work that is happening off the one surface
	// that exists to say it is happening. Everything under the head scrolls,
	// which is what the cursor is standing in.
	if !strings.Contains(rail, "node 1 ") {
		t.Fatalf("the running head scrolled off the column:\n%s", rail)
	}
	if strings.Contains(rail, "node 20 ") {
		t.Fatalf("nothing scrolled at all — the row above the cursor is still drawn:\n%s", rail)
	}
	// And the footer still counts the whole roster rather than the window.
	if !strings.Contains(rail, "300 "+railGroupWords[railRunning]) {
		t.Fatalf("the footer counts the window instead of the roster:\n%s", rail)
	}
}

// Failures and retained branches are reports. A conflict or unresolved review
// remains actionable; retaining a branch alone is not a request to merge it.
func TestAPlainFailureIsFiledAsNewsAndNotAsADemand(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone, session.TaskNotice{
			Merge: mergeWordMerged,
		})},
		streamEventMsg{gen: a.gen, ev: update(2, "Render titles", session.TaskFailed, session.TaskNotice{
			Report: "the tests did not build",
		})},
		streamEventMsg{gen: a.gen, ev: update(3, "Mix audio", session.TaskFailed, session.TaskNotice{
			Report: "stopped: 40 steps and no finish", Merge: mergeWordAborted, Branch: "task/mix",
		})},
		streamEventMsg{gen: a.gen, ev: update(4, "Port the parser", session.TaskDone, session.TaskNotice{
			Merge: mergeWordConflicted, Branch: "task/parser",
		})},
		streamEventMsg{gen: a.gen, ev: update(5, "Cut the trailer", session.TaskUnverified, session.TaskNotice{
			Merge: mergeWordAborted, Branch: "task/trailer",
		})},
		streamEventMsg{gen: a.gen, ev: update(6, "Write the auth", session.TaskFailed, session.TaskNotice{
			Report: "the tests did not build", Merge: mergeWordInPlace,
		})},
	)
	for _, tc := range []struct {
		id   uint64
		what string
		want railGroup
	}{
		{2, "a failure that kept nothing", railDone},
		{6, "a failure in the person's own tree", railDone},
		{1, "a clean merge", railDone},
		{3, "a run that stopped with its branch kept", railDone},
		{4, "a branch that conflicted", railAttention},
		{5, "a landing nobody could judge", railAttention},
	} {
		if got := a.railGroupOf(a.tasks[tc.id]); got != tc.want {
			t.Fatalf("%s is filed under %q, want %q", tc.what, railGroupWords[got], railGroupWords[tc.want])
		}
	}

	// THE FOLD GIVES ITS FIRST SLOTS TO THE WORK THAT DID NOT COME OFF (8.1.7),
	// and newest-first survives inside each half of that partition.
	members := a.railMembers()
	var order []uint64
	for _, node := range members[railDone] {
		order = append(order, node.id)
	}
	want := []uint64{6, 3, 2, 1}
	if len(order) != len(want) {
		t.Fatalf("the done group holds %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("the done group is ordered %v, want the incomplete work first: %v", order, want)
		}
	}

	// The footer counts two decisions and four finished reports.
	rail := rosterText(a, 24)
	for _, want := range []string{"2 needs you", "4 done"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the roster is missing %q:\n%s", want, rail)
		}
	}
	// The two demands lead the column and the record follows them, whole.
	demands := strings.Index(rail, "Cut the trailer")
	for _, news := range []string{"Render titles", "Write the auth", "Collect sources"} {
		if at := strings.Index(rail, news); at < 0 || at < demands {
			t.Fatalf("%q stands above the work that needs a person:\n%s", news, rail)
		}
	}
}

// ── THE ROOM: A TASK IS A PLACE ─────────────────────────────────────────────
//
// The rail says a node is alive and the transcript says how it ended, and
// neither of them is the WORK. These tests are about the third thing (room.go):
// walking into a node, reading what it is doing, and telling it something.

// roomFake is the tasker with the room's three doors on it. It is a widening of
// [taskFake] rather than a fake of its own for the reason taskFake widens
// fakeAgent: a session with rooms is a session with tasks, and the surface
// asserts the two capabilities separately.
type roomFake struct {
	*taskFake
	journal string
	lanes   map[uint64]chan session.Event
	// catchup is the step the node is in the middle of, which the engine hands
	// to EVERY joiner rather than to the first one (internal/session's
	// [taskCatchup]). It is kept per node and never drained, because that is what
	// makes it survive a room being left and re-opened — the fact this fake would
	// otherwise quietly lose.
	catchup map[uint64][]session.Event
	steered []steerLine
	// steerWaiting is the engine answering that the node was PARKED ON ITS OWN
	// PIECES when it took the line (internal/session's [Agent.SteerTask]).
	steerWaiting bool
	// steerHeld is the third outcome: nobody was inside the node to read the
	// line, its work is being checked, and the engine kept the words on the
	// task's own record rather than sending them back.
	steerHeld bool
	steerErr  error
	watchErr  error
	// retargeted is every explicit model pick this fake was handed, in order, and
	// retargetErr is the engine refusing one — a node that settled between the
	// frame and the press (internal/session's [Agent.RetargetTask]).
	retargeted  []modelPick
	retargetErr error
}

type steerLine struct {
	id   uint64
	text string
}

type modelPick struct {
	id    uint64
	model string
}

// lane is the node's live channel, made on first ask so a test can fill it
// BEFORE the room subscribes: the pump blocks on an empty channel, and a test
// that waited for one would be a test that waited.
func (f *roomFake) lane(id uint64) chan session.Event {
	if f.lanes[id] == nil {
		f.lanes[id] = make(chan session.Event, 16)
	}
	return f.lanes[id]
}

func (f *roomFake) TaskJournal(id uint64) string { return f.journal }

// WatchTask hands every caller ITS OWN channel, seeded with whatever has been
// put on the node's lane so far.
//
// internal/session's door is a FAN-OUT (task_room.go: openRoom().join()), and
// this fake owed it that shape the moment a second watcher appeared: the rail
// flies a pilot on every running node now (task.go's [taskPilot]) as well as the
// room opening a watch of its own, and a fake that handed both the same channel
// would have them eating each other's events.
func (f *roomFake) WatchTask(id uint64) (<-chan session.Event, error) {
	if f.watchErr != nil {
		return nil, f.watchErr
	}
	lane, out := f.lane(id), make(chan session.Event, 64)
	// The catch-up first, exactly as the door serves it: the step in flight, then
	// what happens next.
	for _, ev := range f.catchup[id] {
		out <- ev
	}
	for {
		select {
		case ev, ok := <-lane:
			if !ok {
				// A CLOSED LANE IS A FINISHED NODE, and the door answers it the way
				// the real one does: a channel that is already closed.
				close(out)
				return out, nil
			}
			out <- ev
		default:
			return out, nil
		}
	}
}

func (f *roomFake) SteerTask(id uint64, text string) (session.SteerReceipt, error) {
	if f.steerErr != nil {
		return session.SteerReceipt{}, f.steerErr
	}
	f.steered = append(f.steered, steerLine{id: id, text: text})
	// steerWaiting is the engine's own second answer: the node had handed its
	// pieces out and was parked on their reports, so this line is what wakes it.
	// The whole receipt travels now, sentence included, because a held line — one
	// the engine kept while a task's work was being checked — is a third outcome
	// this fake would otherwise be unable to express.
	return session.SteerReceipt{
		Waiting: f.steerWaiting,
		Held:    f.steerHeld,
		Landing: f.steerLanding(),
	}, nil
}

// steerLanding is the engine's own sentence for what the sending did.
func (f *roomFake) steerLanding() string {
	if f.steerHeld {
		return "held on the task's record — it is being checked, and it cannot land as done without this"
	}
	return session.SteerDelivered(f.steerWaiting)
}

// RetargetTask is the room's fourth door: one running node moved onto another
// model, from its next turn on. The real one publishes the change on a task
// update of its own, which is why nothing here writes the node — a test that
// wants the row to move drives the update the engine would have sent.
func (f *roomFake) RetargetTask(id uint64, model string) error {
	if f.retargetErr != nil {
		return f.retargetErr
	}
	f.retargeted = append(f.retargeted, modelPick{id: id, model: model})
	return nil
}

// roomApp is [taskApp] with the doors open and one node already running, which
// is the only state a rail row exists in.
func roomApp(t *testing.T) (*app, *roomFake, func(time.Duration)) {
	t.Helper()
	base, fake, advance := taskApp(t)
	agent := &roomFake{taskFake: fake, lanes: map[uint64]chan session.Event{}}
	base.agent = agent
	// AND IT NAMES THE CONVERSATION AND ITS DRAFT FILE, which every production
	// door does in one breath (cmd/aforge). A correction is only sent from a
	// conversation this surface could write the send down for first, so a room
	// fixture with neither has no ear at all (steersend.go's [app.steerDurable]).
	base.file = filepath.Join(t.TempDir(), "conversation.jsonl")
	base.draftFile = filepath.Join(filepath.Dir(base.file), "draft.txt")
	drive(t, base, streamEventMsg{gen: base.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})
	return base, agent, advance
}

// roomJournal writes a node's session file: the shape internal/session's
// sessionfile.go appends, header line and all.
func roomJournal(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "node.jsonl")
	body := `{"type":"session","version":1,"id":"s1","cwd":"/tmp/lab"}` + "\n" +
		strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the journal: %v", err)
	}
	return path
}

// roomText is the node's page as a reader sees it.
func roomText(a *app) string {
	var out []string
	for _, r := range a.roomRows(a.bodyWidth()) {
		out = append(out, plain(r.text))
	}
	return strings.Join(out, "\n")
}

// clickRail presses the roster's Nth NODE row — the door into that node's room.
//
// It resolves the row through the column's own hit-testing rather than counting:
// the roster groups its rows under headings now (task.go), so a node's screen row
// is not its index, and a test that assumed it was would be pressing a heading.
func clickRail(t *testing.T, a *app, node int) {
	t.Helper()
	if !a.railShowing() {
		t.Fatal("there is no rail to click")
	}
	// The scan walks SCREEN rows, which is what the click will name: the rail's
	// rows are the BODY REGION's rows, and the rows the frame pins above it — the
	// room's focus header, the task strip — move them down by their own height
	// (room.go, taskstrip.go, view.go's [app.topHeight]) — so the rail's first
	// row is not always the frame's first row.
	head := a.bodyTop()
	seen, at := 0, -1
	for y := head; y < head+a.viewHeight(); y++ {
		row := a.railNodeAt(y)
		if row == nil || (y > head && a.railNodeAt(y-1) == row) {
			continue
		}
		if seen == node {
			at = y
			break
		}
		seen++
	}
	if at < 0 {
		t.Fatalf("the roster has no node row %d", node)
	}
	// The press lands on the row's first TEXT cell: the two cells before it are
	// the seam, which is the column's resize handle at this width (room.go's
	// railPress). That cell holds the row's STATE, and a state is not a control —
	// so this is the node's door like every other cell on the row.
	drive(t, a, tea.MouseClickMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam), Y: at, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: a.bodyWidth() + ansi.StringWidth(railSeam), Y: at, Button: tea.MouseLeft})
}

// openRoomCall clicks the page's row carrying this text, the way a person opens
// a call in the conversation, and reports whether there was one to click.
func openRoomCall(t *testing.T, a *app, want string) bool {
	t.Helper()
	rows := a.roomRows(a.bodyWidth())
	top := a.bodyTop()
	offset := a.roomOffsetFor(len(rows), a.viewHeight())
	for i := offset; i < len(rows); i++ {
		if rows[i].hit != hitTool || !strings.Contains(plain(rows[i].text), want) {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: 0, Y: top + i - offset, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: 0, Y: top + i - offset, Button: tea.MouseLeft})
		return true
	}
	return false
}

// THE CONTRACT THE ENGINE LANDED. It is asserted at runtime rather than as a
// compile-time `var _` on purpose: the room's doors are an ASSERTION on this
// surface (room.go), so a build whose engine has no rooms in it must still
// compile — this is the test that says whether it has them.
func TestTheRoomDoorsAreTheEnginesOwnContract(t *testing.T) {
	if _, ok := any((*session.Agent)(nil)).(taskRoomAgent); !ok {
		t.Fatal("*session.Agent does not answer SteerTask, WatchTask and TaskJournal")
	}
}

// A RAIL ROW IS A DOOR: clicking it replaces the body with the node's own
// transcript, replayed from its journal — and a second click on the same row
// comes back out.
func TestARailClickOpensTheNodesRoomOnItsJournal(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"Fix the nil-map crash"}`,
		`{"type":"message","role":"assistant","content":"I will read the parser first.","toolCalls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"internal/parse/keys.go\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"c1","content":"the whole file, byte for byte"}`,
	)
	clickRail(t, a, 0)

	if !a.roomOpen() {
		t.Fatal("a rail click did not open the node's room")
	}
	// The rail opens compact activity; the journal's detailed tool row is
	// available through the same live disclosure as a newly running call.
	compact := roomText(a)
	if !strings.Contains(compact, "I will read the parser first") || strings.Contains(compact, "read internal/parse/keys.go") {
		t.Fatalf("rail did not open compact journal activity:\n%s", compact)
	}
	openRoomCompactWork(t, a)
	page := roomText(a)
	for _, want := range []string{"Fix the nil-map crash", "I will read the parser first",
		"read internal/parse/keys.go"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the replayed journal is missing %q:\n%s", want, page)
		}
	}
	// A CALL IS A CALL, on a page as in the conversation: the result is behind
	// it rather than on it, and the row says so by answering the pointer.
	if strings.Contains(page, "byte for byte") {
		t.Fatalf("a collapsed call showed its result:\n%s", page)
	}
	if !openRoomCall(t, a, "read internal/parse/keys.go") {
		t.Fatalf("the replayed call does not expand:\n%s", page)
	}
	if !strings.Contains(roomText(a), "byte for byte") {
		t.Fatalf("the expansion opened on nothing:\n%s", roomText(a))
	}
	// The body region IS the room — the conversation is not under it — and the
	// rail is still beside it, because the rail is how you leave one room for
	// another.
	body, _ := a.bodyRows(a.bodyWidth(), a.viewHeight())
	var drawn []string
	for _, r := range body {
		drawn = append(drawn, plain(r.text))
	}
	// The call is drawn by the CONVERSATION's tool line — rail glyph, name,
	// target and the stat at the far end — which is the whole of the parity this
	// slice is for: the page is not a second renderer.
	if !containsRow(drawn, "read internal/parse/keys.go") {
		t.Fatalf("the room is not what the body draws:\n%s", strings.Join(drawn, "\n"))
	}
	if !strings.Contains(plain(frame(a)), "Fix the nil-map") {
		t.Fatal("the rail went away when the room opened")
	}

	// Repeated selection keeps the page open; leaving is a separate action.
	opened := a.room
	clickRail(t, a, 0)
	if a.room != opened {
		t.Fatal("a second click replaced or closed the selected task")
	}
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("Escape did not leave the task")
	}
}

// THE ROOM IS LIVE: the node's own events land in it as they happen — deltas
// coalescing into one growing block, a call's begin and end sharing one line.
func TestTheRoomsLiveLaneAppendsAndCoalesces(t *testing.T) {
	a, agent, _ := roomApp(t)
	lane := agent.lane(7)
	lane <- session.Event{Kind: session.EventTextDelta, Text: "Looking at "}
	lane <- session.Event{Kind: session.EventTextDelta, Text: "the loader."}
	lane <- session.Event{Kind: session.EventToolBegin, Tool: "read", Args: `{"path":"etc/load.go"}`}
	lane <- session.Event{Kind: session.EventToolEnd, Tool: "read", Args: `{"path":"etc/load.go"}`}
	clickRail(t, a, 0)

	page := roomText(a)
	if !strings.Contains(page, "Looking at the loader") {
		t.Fatalf("the deltas did not coalesce into one block:\n%s", page)
	}
	if n := strings.Count(page, "read etc/load.go"); n != 1 {
		t.Fatalf("one call drew %d lines, want 1:\n%s", n, page)
	}
	// A DIFFERENT call is a different line — the begin is adopted by the row its
	// own announcement drew, not by whichever row shares its tool name, or a
	// batch of four reads would read as one.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "read", Args: `{"path":"etc/other.go"}`,
	}})
	if !strings.Contains(roomText(a), "read etc/other.go") {
		t.Fatalf("a second call did not draw its own line:\n%s", roomText(a))
	}
}

// THE INPUT TALKS TO THE NODE. Enter steers, the words arrive at the engine as
// the person wrote them, and they land on the page as the ELBOW a correction is
// drawn as everywhere on this surface (steerelbow.go).
func TestEnterInARoomSteersTheNode(t *testing.T) {
	a, agent, _ := roomApp(t)
	clickRail(t, a, 0)

	// The box says who it is talking to. The NAME is on the segment in front of
	// the prompt (room.go's [app.roomLead]) rather than inside the placeholder,
	// because a placeholder is gone the moment somebody types and the name has to
	// outlive that — and it is the NAME the rail and the cards name the node by
	// (taskident.go). The placeholder is left saying what the box does and which
	// key leaves.
	block, _, _, _ := a.chrome(a.width)
	lane := plain(strings.Join(block, "\n"))
	if !strings.Contains(lane, "Fix the nil-map") {
		t.Fatalf("the box does not name the node it is talking to:\n%s", lane)
	}
	if !strings.Contains(lane, roomSteerHere+roomSteerBack) {
		t.Fatalf("the box does not offer the steering lane:\n%s", lane)
	}

	a.input.setText("the config lives under etc/")
	drive(t, a, key("enter"))

	if len(agent.steered) != 1 {
		t.Fatalf("enter steered %d times, want 1: %+v", len(agent.steered), agent.steered)
	}
	if got := agent.steered[0]; got.id != 7 || got.text != "the config lives under etc/" {
		t.Fatalf("the line reached the engine as %+v", got)
	}
	if !strings.Contains(roomText(a), "the config lives under etc/") {
		t.Fatalf("the steered line is not in the room:\n%s", roomText(a))
	}
	// IT IS AN ELBOW AND NOT A QUESTION: the dim `└ ` mark, the words one reading
	// step under a question's own, and no `›` anywhere on the page — a task page
	// is one question with corrections hanging off it (#252, ruling 2).
	var said string
	for _, r := range a.roomRows(a.bodyWidth()) {
		// Past the reading gutter and no further: what is asserted below is the
		// first style the ROW ITSELF carries (pastGutter).
		row := pastGutter(a.bodyWidth(), r.text)
		if strings.HasPrefix(plain(row), glyphSteer+"the config lives under etc/") {
			said = row
		}
	}
	if said == "" {
		t.Fatalf("the steered line is not drawn as an elbow:\n%s", roomText(a))
	}
	if !strings.HasPrefix(said, sgrOf(a.pal.dim)) {
		t.Fatalf("the elbow glyph is not dim furniture: %q", said)
	}
	// Past the whole ramp, so what is left is the tier the row RESTS at: the
	// person's own prose one reading step under a question's, never the accent.
	a.clock = func() time.Time { return time.Now().Add(hudWarm + time.Second) }
	a.roomTouched()
	settled := ""
	for _, r := range a.roomRows(a.bodyWidth()) {
		row := pastGutter(a.bodyWidth(), r.text)
		if strings.HasPrefix(plain(row), glyphSteer+"the config lives under etc/") {
			settled = row
		}
	}
	if !strings.Contains(settled, sgrOf(a.pal.narr)+"the config lives under etc/") {
		t.Fatalf("the correction's words are not one step under a question's: %q", settled)
	}
	if strings.Contains(settled, sgr256(hueAccent)) {
		t.Fatalf("the correction spent the accent a question is drawn in: %q", settled)
	}
	if strings.Contains(roomText(a), "› the config lives under etc/") {
		t.Fatalf("the correction was drawn as a question of its own:\n%s", roomText(a))
	}
	// AND IT OPENS NO TURN. The page's whole life is the one turn its instruction
	// opened; a correction bends that question rather than asking another.
	if a.room.turn != 0 {
		t.Fatalf("steering opened turn %d on a page with no instruction in it", a.room.turn)
	}
	// It went to the NODE and not to the model, and the box is empty for the
	// next thing to say.
	if len(agent.sent) != 0 {
		t.Fatalf("the steer also reached the model: %v", agent.sent)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept the steered line: %q", a.input.String())
	}
	if countKind(a, entryUser) != 0 {
		t.Fatal("the steered line was also written into the conversation")
	}
}

// ESC RESTORES THE CONVERSATION EXACTLY, scroll position included — which it
// does by never having touched it: the room scrolls its own offset.
func TestEscLeavesTheRoomAndRestoresTheScroll(t *testing.T) {
	a, _, _ := roomApp(t)
	for i := 0; i < 40; i++ {
		a.note("line " + itoa(i))
	}
	a.scroll(-9)
	before, beforePad := a.window(a.bodyWidth(), a.viewHeight())
	offset, stick := a.offset, a.stick
	if offset == 0 || stick {
		t.Fatalf("the transcript was never scrolled off its live edge (offset %d, stick %v)",
			offset, stick)
	}

	clickRail(t, a, 0)
	// Reading the room moves the ROOM, and the transcript underneath keeps
	// growing without moving what a person had parked on screen.
	a.roomScroll(-3)
	a.note("a line that landed while the room was open")

	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not leave the room")
	}
	if a.offset != offset || a.stick != stick {
		t.Fatalf("the conversation's scroll moved: offset %d→%d, stick %v→%v",
			offset, a.offset, stick, a.stick)
	}
	after, afterPad := a.window(a.bodyWidth(), a.viewHeight())
	if afterPad != beforePad || len(after) != len(before) {
		t.Fatalf("the restored window is %d rows (pad %d), want %d (pad %d)",
			len(after), afterPad, len(before), beforePad)
	}
	for i := range before {
		if before[i].text != after[i].text {
			t.Fatalf("row %d changed across the room:\n%q\n%q",
				i, plain(before[i].text), plain(after[i].text))
		}
	}
}

// AND A ROOM ON A NODE THAT IS WAITING ON ITS OWN PIECES SAYS THE TRUER FACT.
// Such a node has handed its work out and parked on the reports
// (internal/session's task_room.go): it is not in a step, so the line is what
// wakes it, and a page that drew the person's words and went quiet is the page
// they would see if the words had gone nowhere at all.
//
// THE SENTENCE IS THE ENGINE'S AND THE PAGE DRAWS IT VERBATIM
// ([session.SteerDelivered]), because the live clause and the one the record
// keeps for tomorrow have to be one sentence with one author.
func TestSteeringANodeWaitingOnItsPiecesSaysWhatTheLineJustDid(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.steerWaiting = true
	clickRail(t, a, 0)

	a.input.setText("the config lives under etc/")
	drive(t, a, key("enter"))

	if len(agent.steered) != 1 {
		t.Fatalf("enter steered %d times, want 1: %+v", len(agent.steered), agent.steered)
	}
	body := roomText(a)
	if !strings.Contains(body, "the config lives under etc/") {
		t.Fatalf("the steered line is not in the room:\n%s", body)
	}
	if !strings.Contains(body, session.SteerDelivered(true)) {
		t.Fatalf("the room took a line into a parked node and said nothing about it:\n%s", body)
	}
	// AND IT IS A CLAUSE ON THE CORRECTION'S OWN ROW, not a second line of the
	// page's own: what happened to those words is a fact about those words.
	if !strings.Contains(elbowRowIn(a, "the config lives under etc/"), session.SteerDelivered(true)) {
		t.Fatalf("the clause is not on the correction's row:\n%s", body)
	}
	if countKind(a, entryNote) != 0 {
		t.Fatal("the note went into the conversation instead of the room")
	}
}

// EVERY TASK STEER CONFIRMS DELIVERY (the owner's ruling 4, 2026-09-01). A
// correction typed into this conversation is answered by the answer; one typed
// into a task crosses to ANOTHER AGENT and the page can stay silent afterwards
// for as long as the node's current step runs. That silence is what the person
// would also see if the words had gone nowhere, so the elbow says they arrived —
// briefly, on the same fade the conversation's own landing clause takes.
func TestEveryTaskSteerConfirmsDelivery(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	a.input.setText("the config lives under etc/")
	drive(t, a, key("enter"))

	row := elbowRowIn(a, "the config lives under etc/")
	if !strings.Contains(row, steerClauseSep+session.SteerDelivered(false)) {
		t.Fatalf("a delivered correction carries no receipt: %q\n%s", row, roomText(a))
	}
	// AND IT DOES NOT CLAIM THE OTHER FACT. A node taking steps was not parked.
	if strings.Contains(roomText(a), session.SteerDelivered(true)) {
		t.Fatalf("a working node's room claims the line woke it:\n%s", roomText(a))
	}
	// THE CLAUSE IS NEWS AND GOES WHEN THE NEWS DOES, and the elbow stays: the
	// block's position is what says where the words went, and a receipt that
	// stayed for ever would be furniture rather than an answer.
	a.clock = func() time.Time { return time.Now().Add(hudWarm + time.Second) }
	a.roomTouched()
	settled := elbowRowIn(a, "the config lives under etc/")
	if settled == "" {
		t.Fatalf("the correction left the page with its clause:\n%s", roomText(a))
	}
	if strings.Contains(settled, session.SteerDelivered(false)) {
		t.Fatalf("the delivery receipt never stopped being news: %q", settled)
	}
}

// pastGutter takes the reading gutter's own cells off the front of a drawn row
// and NOTHING else (gutter.go). The pass prepends bare spaces to a finished row,
// ahead of the row's first style, so a test that reads what a row OPENS with —
// its first glyph, its first SGR — has to step over exactly the air this width
// bought. It asks [textGutterCols] rather than trimming two, because a frame at
// the phone tier buys none and a test that assumed two would then eat the row.
func pastGutter(width int, text string) string {
	return strings.TrimPrefix(text, strings.Repeat(" ", textGutterCols(width)))
}

// elbowRowIn is the drawn row a task page's correction is on, plain and past the
// gutter, or "".
func elbowRowIn(a *app, words string) string {
	width := a.bodyWidth()
	for _, r := range a.roomRows(width) {
		if line := pastGutter(width, plain(r.text)); strings.HasPrefix(line, glyphSteer+words) {
			return line
		}
	}
	return ""
}

// A ROOM ON A NODE THAT HAS LANDED says so at its foot and ASKS about what is
// typed at it. Neither of the two silent answers is this surface's to give: a
// dropped sentence is lost work, and a sentence quietly re-pointed at the main
// conversation is worse — the box said "steer <task>" right up to the enter.
func TestAFinishedNodesRoomShowsItsFootAndGuardsWhatIsTypedAtIt(t *testing.T) {
	a, agent, _ := roomApp(t)
	close(agent.lane(7))
	clickRail(t, a, 0)

	if !strings.Contains(roomText(a), roomFinishedRefusal.what) {
		t.Fatalf("a finished node's room has no foot:\n%s", roomText(a))
	}
	a.input.setText("try the other directory")
	drive(t, a, key("enter"))
	if len(agent.steered) != 0 {
		t.Fatalf("a finished node was steered anyway: %+v", agent.steered)
	}
	if !a.guarding() {
		t.Fatal("a steer at a parked node was answered silently")
	}
	got := plain(frame(a))
	for _, want := range []string{
		"is parked — ", "[r] revive and send", "[m] send to main", "[esc] cancel",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the guard is missing %q:\n%s", want, got)
		}
	}
	// The sentence is still the person's — it is not taken away while they are
	// being asked where it should go.
	if a.input.String() != "try the other directory" {
		t.Fatalf("the guarded line was cleared from the box: %q", a.input.String())
	}
	// And esc leaves it exactly there, in the room it was typed in.
	drive(t, a, key("esc"))
	if a.guarding() {
		t.Fatal("esc did not take the guard down")
	}
	if a.input.String() != "try the other directory" || !a.roomOpen() {
		t.Fatalf("esc spent the words or left the room: %q / room=%v",
			a.input.String(), a.roomOpen())
	}
}

// THE GUARD'S TWO SENDING ANSWERS BOTH LEAVE THE ROOM, because from that
// keystroke on the box is talking to the head model — and a placeholder still
// reading "steer <task>" over a message the head received is the exact lie the
// guard exists to prevent.
func TestTheSteerGuardSendsToMainAndRevivesThroughTheHead(t *testing.T) {
	// [m] sends the person's words verbatim.
	a, agent, _ := roomApp(t)
	close(agent.lane(7))
	clickRail(t, a, 0)
	a.input.setText("check etc/ instead")
	drive(t, a, key("enter"), key("m"))

	if a.roomOpen() || a.guarding() {
		t.Fatal("[m] left the room open under a message that went to the head")
	}
	if len(agent.sent) != 1 || agent.sent[0] != "check etc/ instead" {
		t.Fatalf("[m] sent %+v, want the sentence verbatim", agent.sent)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept a sentence that was spent: %q", a.input.String())
	}

	// [r] names the node and carries the words as the instruction for it. There
	// is no engine door that restarts a node — the head's own tool is what makes
	// one — so revive is a request to the only thing that can honour it.
	a, agent, _ = roomApp(t)
	close(agent.lane(7))
	clickRail(t, a, 0)
	a.input.setText("check etc/ instead")
	drive(t, a, key("enter"), key("r"))

	if len(agent.sent) != 1 {
		t.Fatalf("[r] sent %+v, want one message", agent.sent)
	}
	// The request names the node by the NAME every other surface names it by
	// (taskident.go), which is the name the room's own header carries.
	for _, want := range []string{"Fix the nil-map", "check etc/ instead"} {
		if !strings.Contains(agent.sent[0], want) {
			t.Fatalf("the revive request is missing %q: %q", want, agent.sent[0])
		}
	}
}

// A LINE THE ENGINE HELD IS NOT A REFUSAL AND MUST NOT READ LIKE ONE. The task's
// work was in front of the checker, so nobody was inside it to read the words —
// and the engine kept them on the task's own record instead of sending them
// back. The page takes the line, draws the elbow every correction draws, and
// says what the sending did in the engine's own sentence: "delivered" over words
// nobody has read would be this surface inventing a fact.
func TestALineTheEngineHeldIsDrawnAsKeptRatherThanGuarded(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.steerHeld = true
	clickRail(t, a, 0)
	a.input.setText("make it CSV instead")
	drive(t, a, key("enter"))

	if a.guarding() {
		t.Fatal("a line the engine kept raised the guard, so the person would be asked to send it somewhere else")
	}
	if len(agent.steered) != 1 || agent.steered[0].text != "make it CSV instead" {
		t.Fatalf("the engine was handed %+v, want the person's line once", agent.steered)
	}
	if !a.input.empty() {
		t.Fatal("the box kept words the engine had already taken, which is how one correction gets sent twice")
	}
	got := plain(frame(a))
	if !strings.Contains(got, "cannot land as done without this") {
		t.Fatalf("the page does not say what became of the line:\n%s", got)
	}
}

// A STEER THE ENGINE REFUSED raises the same guard, and keeps the engine's own
// sentence about why — "task 4 is done, not running" and "task 4 has no worker
// to talk to yet" are different facts, and the second row is where the person
// reads which one they are looking at.
func TestASteerTheEngineRefusedRaisesTheGuardWithItsReason(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.steerErr = errors.New("task 7 is done, not running")
	clickRail(t, a, 0)
	a.input.setText("stop and re-read the brief")
	drive(t, a, key("enter"))

	if !a.guarding() {
		t.Fatal("a refused steer was not guarded")
	}
	got := plain(frame(a))
	if !strings.Contains(got, "task 7 is done, not running") {
		t.Fatalf("the engine's own sentence was dropped:\n%s", got)
	}
	if !strings.Contains(got, "[r] revive and send") {
		t.Fatalf("the guard did not offer its keys:\n%s", got)
	}
}

// AND A REFUSAL THAT MEANS THE NODE IS STILL RUNNING WITHHOLDS REVIVE (#273).
// Only the engine can tell the two apart — this page's own `done` is its record
// of a close it may not have been told about yet — so it says so with
// [session.ErrNobodyToRead], and the guard drops the key rather than offering
// to start work again that is minutes from finishing.
func TestASteerRefusedWithNobodyToReadOffersNoRevive(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.steerErr = noReaderRefusal{errors.New(
		"task 7 is being checked — nobody is in there to read your line until the check lands")}
	clickRail(t, a, 0)
	a.input.setText("stop and re-read the brief")
	drive(t, a, key("enter"))

	if !a.guarding() {
		t.Fatal("a refused steer was not guarded")
	}
	got := plain(frame(a))
	if !strings.Contains(got, "is being checked") {
		t.Fatalf("the engine's own sentence was dropped:\n%s", got)
	}
	if strings.Contains(got, "[r] revive and send") {
		t.Fatalf("a node that is still running offered revive:\n%s", got)
	}
	if !strings.Contains(got, "[m] send to main") || !strings.Contains(got, "[esc] cancel") {
		t.Fatalf("the guard did not offer the keys it does have:\n%s", got)
	}
	// AND THE KEY IT DOES NOT OFFER DOES NOTHING. A row that withheld revive
	// while r still revived would be the surface lying about its own keys.
	drive(t, a, key("r"))
	if !a.guarding() {
		t.Fatal("r on a guard that offers no revive left the question")
	}
}

// noReaderRefusal is what the engine hands back for the two refusals that mean
// the node is still running with nobody inside it to read a line — the test's
// stand-in for internal/session's own unexported nobodyToRead.
type noReaderRefusal struct{ error }

func (noReaderRefusal) Unwrap() error { return session.ErrNobodyToRead }

// VIEWER PARITY: a node's page is drawn by the conversation's own renderers, so
// everything the conversation shows about a message it shows about a node's
// message — the pictures that came with it included (attach.go's chipMarkers,
// replay.go's replayUserLine, which this is the third reader of).
func TestARoomsMessagesKeepTheirPictures(t *testing.T) {
	a, agent, _ := roomApp(t)
	// THE FILE IS REALLY THERE, digest and all. A page reads the record the way
	// the conversation does now (#252), and the conversation says so when a
	// picture's file has gone — so a fixture pointing at nothing would be testing
	// the missing-file sentence rather than the marker.
	picture := filepath.Join(t.TempDir(), "chart.png")
	if err := os.WriteFile(picture, []byte("not really a png"), 0o600); err != nil {
		t.Fatalf("writing the picture: %v", err)
	}
	sum := sha256.Sum256([]byte("not really a png"))
	agent.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"what is wrong with this",`+
			`"parts":[{"type":"image","path":`+strconv.Quote(picture)+
			`,"sha256":"`+hex.EncodeToString(sum[:])+`"}]}`,
	)
	clickRail(t, a, 0)

	page := roomText(a)
	if !strings.Contains(page, "[#1 chart.png]") {
		t.Fatalf("a page dropped the message's picture:\n%s", page)
	}
	if strings.Contains(page, picture) {
		t.Fatalf("a page drew the whole path instead of the name:\n%s", page)
	}
}

// AND THE REASONING. A node reasons the way the model in the conversation does,
// and the block behaves the same: a live window while it streams, one collapsed
// row carrying the two facts once anything else happens, and ctrl+e to open it.
func TestARoomDrawsAndCollapsesTheNodesThinking(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	for _, text := range []string{"the loader is the ", "wrong place to look"} {
		drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
			Kind: session.EventReasoning, Text: text,
		}})
	}
	if page := roomText(a); !strings.Contains(page, "thinking") ||
		!strings.Contains(page, "wrong place to look") {
		t.Fatalf("the node's reasoning is not on its page:\n%s", page)
	}
	// The first thing that is not reasoning collapses it.
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "I will read the parser instead.",
	}})
	page := roomText(a)
	if !strings.Contains(page, "thought for") {
		t.Fatalf("the reasoning block did not collapse:\n%s", page)
	}
	if strings.Contains(page, "wrong place to look") {
		t.Fatalf("a collapsed block kept its body:\n%s", page)
	}
	// ctrl+e opens the PAGE's block, not the conversation's.
	drive(t, a, key("ctrl+e"))
	if !strings.Contains(roomText(a), "wrong place to look") {
		t.Fatalf("ctrl+e did not open the page's thinking:\n%s", roomText(a))
	}
}

// A PAGE FOLDS ITS OWN CLUSTERS, from its own map and its own turns: ctrl+o in a
// room is about the rows in the room.
//
// A room keeps a screenful of calls rather than the conversation's three
// (roomscroll_test.go), so the page is fed one call more than its view is tall:
// exactly the first one folds.
func TestARoomFoldsItsOwnToolCluster(t *testing.T) {
	a, _, _ := roomApp(t)
	clickRail(t, a, 0)

	paths := []string{"a.go"}
	for i := 0; i < a.viewHeight(); i++ {
		paths = append(paths, "more"+strconv.Itoa(i)+".go")
	}
	for _, path := range paths {
		drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
			Kind: session.EventToolBegin, Tool: "read", Args: `{"path":"` + path + `"}`,
		}})
	}
	page := roomText(a)
	if !strings.Contains(page, "reading") && !strings.Contains(page, "earlier tool call") {
		t.Fatalf("a screenful and one more of calls on a page did not fold:\n%s", page)
	}
	if strings.Contains(page, "read a.go") {
		t.Fatalf("the folded call is still drawn:\n%s", page)
	}
	drive(t, a, key("ctrl+o"))
	if !strings.Contains(roomText(a), "read a.go") {
		t.Fatalf("ctrl+o did not unfold the page:\n%s", roomText(a))
	}
	if len(a.unfolded) != 0 {
		t.Fatalf("the page folded the CONVERSATION's turn: %v", a.unfolded)
	}
}

// THE FRAME SAYS WHERE YOU ARE: the identity cluster names the task, the legend
// names the way out, and the telemetry beside them is still the SESSION's.
func TestTheFrameSaysAPersonIsInARoom(t *testing.T) {
	a, _, _ := roomApp(t)
	a.cost = 0.42
	clickRail(t, a, 0)

	status := plain(a.status(a.width))
	// The chip is the node's mark and its name — no "task 7" ghost id, and no
	// word standing in for the page's own name (room.go, taskident.go).
	if !strings.Contains(status, "Fix the nil-map") {
		t.Fatalf("the status line does not name the room:\n%s", status)
	}
	if !strings.Contains(status, "$0.42") {
		t.Fatalf("the room took the session's telemetry with it:\n%s", status)
	}
	if !strings.Contains(plain(a.legend(a.width)), roomLegendWord) {
		t.Fatalf("the legend does not say how to leave:\n%s", plain(a.legend(a.width)))
	}
	drive(t, a, key("esc"))
	if strings.Contains(plain(a.status(a.width)), "Fix the nil-map") {
		t.Fatalf("the status line stayed in the room:\n%s", plain(a.status(a.width)))
	}
}

// THE KEYBOARD DOOR: ↑/↓ walk onto a settled proposal in the transcript and
// enter opens that node's room, so the room is not a mouse-only place.
func TestTheKeyboardWalksIntoARoom(t *testing.T) {
	a, agent, _ := roomApp(t)
	agent.pending = []uint64{7}
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 0)})
	// `1` starts it, so the block in the transcript is settled and the walk can
	// reach it. The question has to have been drawn first: the block takes no
	// key from a question it has never put on screen (question.go).
	taskText(a)
	settleAsk(a)
	drive(t, a, key("1"))
	drive(t, a, key("up"))
	if a.sel < 0 || a.entries[a.sel].kind != entryTask {
		t.Fatalf("the walk did not reach the proposal (sel %d)", a.sel)
	}
	drive(t, a, key("enter"))
	if !a.roomOpen() || a.room.id != 7 {
		t.Fatal("enter on the selected proposal did not open its room")
	}
	// And the lane behind it is really pumping: an event sent now arrives.
	agent.lane(7) <- session.Event{Kind: session.EventTextDelta, Text: "still going"}
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventTextDelta, Text: "still going",
	}})
	if !strings.Contains(roomText(a), "still going") {
		t.Fatalf("the room's lane is not live:\n%s", roomText(a))
	}
}

// THE BUILD GUARD: an agent with no room doors on it degrades to a note. The
// room is asserted, never required — see [taskRoomAgent].
func TestASessionWithoutRoomDoorsSaysSoAndStaysPut(t *testing.T) {
	a, _, _ := taskApp(t) // a tasker, but no rooms
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash",
		session.TaskRunning, session.TaskNotice{})})
	clickRail(t, a, 0)

	if a.roomOpen() {
		t.Fatal("a session with no room doors opened a room")
	}
	if !strings.Contains(taskText(a), roomUnavailableRefusal.line()) {
		t.Fatalf("the degraded case said nothing:\n%s", taskText(a))
	}
}
