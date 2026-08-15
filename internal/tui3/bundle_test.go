package tui3

import (
	"encoding/base64"
	"encoding/json"
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

// And the groups reach the SCREEN as the escape sequences they were authored
// as. This is the assertion that survives a refactor of the lexer: whatever the
// tokens are called, `echo` is the accent and `"hi"` is the pastel green.
func TestTheShellHighlightPaintsEachGroup(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	painted := pal.shell(`cd /tmp && echo "hi" | grep -n x # note`)

	for _, want := range []struct {
		what  string
		token string
		hue   hue
	}{
		{"the command", "cd", hueAccent},
		{"the second command", "echo", hueAccent},
		{"the string", `"hi"`, hueAdd},
		{"the flag", "-n", hueMuted},
		{"the operator", "&&", hueViolet},
		{"the pipe", "|", hueViolet},
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
	// THE QUESTION HUE IS NOT SPENT HERE. An operator is violet, and violet on
	// this surface is two hues apart on purpose (styles.go).
	if strings.Contains(painted, sgr256(hueAsk)) {
		t.Fatalf("a command line took the question hue:\n%q", painted)
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
	if payload != "· bravo\n· charlie" {
		t.Fatalf("the yank carried %q", payload)
	}
	if a.copy.mark >= 0 {
		t.Fatal("the mark survived the yank")
	}
	// v again with no mark set copies the cursor's line alone.
	a.copy.at = rowWith(t, a, "delta")
	if got := yank(t, a); got != "· delta" {
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
		{"hover", lightHover, "#E5E9F0"},
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
		"hover": lightHover, "violet": hueViolet,
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
	if got := a.pal.hover("x", 10); got != "x" {
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
		ID: "google/gemini-3.1-flash-image",
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

	// The two tiers sit on the Session tab, where the panel opens.
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
		if !strings.Contains(plain(frame(a)), "200k · $3/$15 per M · elo 1300") {
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

	// The vision row is a picker too, over on Providers.
	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
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

	cursorTo(t, a, config.KeyTierHighModel)
	drive(t, a, key("enter"))
	chat := []string{"anthropic/claude-sonnet-4.5", "vendor/blind-chat", "moonshotai/kimi-k3"}
	if got := pickedIDs(a.sheet.sel); strings.Join(got, ",") != strings.Join(chat, ",") {
		t.Fatalf("a tier row offers %v, want the models you can talk to %v", got, chat)
	}
	drive(t, a, key("esc"))

	for i := 0; i < 4; i++ {
		drive(t, a, key("right"))
	}
	cursorTo(t, a, config.KeyVisionModel)
	drive(t, a, key("enter"))
	// Sonnet publishes an image input; blind-chat publishes an input list
	// WITHOUT one and is gone; the silent row falls through, because silence on
	// this side is a cache written before the field travelled and not a refusal.
	vision := []string{"anthropic/claude-sonnet-4.5", "moonshotai/kimi-k3"}
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
		{Model{ID: "moonshotai/kimi-k3"}, true, true},
		{Model{ID: "openai/gpt-4o-transcribe-audio"}, false, true},
		{Model{ID: "vendor/text-embedding-3"}, false, true},
		{Model{ID: "elevenlabs/voice-v3"}, false, true},
		{Model{ID: "openai/whisper-1"}, false, true},
		{Model{ID: "google/lyria-3-preview"}, false, true},
		{Model{ID: "vendor/music-gen"}, false, true},
		{Model{ID: "bytedance/seedance-video-pro"}, false, true},
		{Model{ID: "vendor/rerank-2"}, false, true},
		{Model{ID: "openai/omni-moderation-latest"}, false, true},
		{Model{ID: "google/imagen-4"}, false, true},
		{Model{ID: "openai/gpt-4o-mini-tts"}, false, true},
		{Model{ID: "openai/sora-2"}, false, true},
		{Model{ID: "google/veo-3"}, false, true},
		{Model{ID: "openai/dalle-3"}, false, true},
		// The marks are WHOLE WORDS of the id and never substrings, so a chat
		// model whose name merely carries the letters survives.
		{Model{ID: "vendor/videographer-8b"}, true, true},
		{Model{ID: "vendor/audiophile"}, true, true},
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

// THE SELECTED ROW IS ONE BAND, LEAD TO NOTE, ACROSS THE WHOLE LINE — and the
// note is inside it rather than dim underneath it.
func TestTheSelectedOverlayRowIsOneBandAcrossTheLine(t *testing.T) {
	pal := newPalette(tokens.ANSI256, false)
	const width = 48
	const note = "128k · elo 1200"
	band := "\x1b[48;5;" + itoa(int(hueBand.idx)) + "m"

	line := overlayRow("openai/gpt-4.1-mini", note, true, false, false, width, pal)
	if !strings.HasPrefix(line, band) || !strings.HasSuffix(line, "\x1b[49m") {
		t.Fatalf("the selected row is not one band:\n%q", line)
	}
	// The whole line: the band is opened once, closed once, and everything the
	// row says is between them.
	inside := strings.TrimSuffix(strings.TrimPrefix(line, band), "\x1b[49m")
	if strings.Contains(inside, "\x1b[49m") {
		t.Fatalf("the band is broken up mid-row:\n%q", line)
	}
	if !strings.Contains(inside, pal.ink(note)) {
		t.Fatalf("the note is not painted inside the band:\n%q", line)
	}
	if strings.Contains(line, pal.dim(note)) {
		t.Fatalf("the note stayed dim inside the band:\n%q", line)
	}
	if got := ansi.StringWidth(plain(line)); got != width {
		t.Fatalf("the band is %d cells wide, want the whole %d", got, width)
	}

	// HOVER IS THE SUBTLER ONE, and it is a different colour: a pointer crossing
	// a list must never read as the cursor moving.
	hovered := overlayRow("openai/gpt-4.1-mini", note, false, false, true, width, pal)
	hover := "\x1b[48;5;" + itoa(int(hueHover.idx)) + "m"
	if !strings.HasPrefix(hovered, hover) || strings.Contains(hovered, band) {
		t.Fatalf("the hovered row wears the selection band:\n%q", hovered)
	}
	if hueHover.idx == hueBand.idx {
		t.Fatal("the hover and the selection resolve to one colour")
	}
	// A row that is both takes the selection: the cursor outranks the pointer.
	both := overlayRow("openai/gpt-4.1-mini", note, true, false, true, width, pal)
	if !strings.HasPrefix(both, band) {
		t.Fatalf("the pointer painted over the cursor:\n%q", both)
	}
	// The model in use keeps its accent inside the band.
	marked := overlayRow("openai/gpt-4.1-mini", note, true, true, false, width, pal)
	if !strings.Contains(marked, pal.accent("openai/gpt-4.1-mini")) {
		t.Fatalf("the marked row lost its accent to the band:\n%q", marked)
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
