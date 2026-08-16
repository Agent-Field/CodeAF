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
	if got := a.warmSegment(); got != "⟲ 89%" {
		t.Fatalf("the segment reads %q before any priced turn, want ⟲ 89%%", got)
	}

	// Two turns, each 9,800 cached tokens at a nine-dollar-per-million gap:
	// $0.0882 apiece, and the segment carries the SUM.
	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	a.cacheNote(session.Usage{Input: 12_000, CacheRead: 9_800})
	if want := 2 * 0.0882; a.cacheSaved < want-1e-9 || a.cacheSaved > want+1e-9 {
		t.Fatalf("cacheSaved = %v after two turns, want %v", a.cacheSaved, want)
	}
	got := a.warmSegment()
	if got != "⟲ saved $0.1764 · 89%" {
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
	if got := bare.warmSegment(); got != "⟲ 89%" {
		t.Fatalf("the unpriced segment reads %q, want ⟲ 89%%", got)
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
	a.home, a.workspace, a.place = "/home/dev", "/home/dev/src/aforge-v2", "aforge-v2"
	a.branch, a.branchDirty = "chat-v3-task", true
	a.gitProbe = func(string) (string, bool, bool) { return "chat-v3-task", true, true }
	a.width, a.height = 200, 24
	return a, agent, &now
}

// ── the legend ──────────────────────────────────────────────────────────────

// THE BORDER ABOVE THE INPUT IS A FIELDSET LEGEND: where you are on the left,
// what the box answers to on the right, and rule between them.
func TestTheLegendCarriesThePlaceAndTheInputsAffordances(t *testing.T) {
	a, _, _ := hudApp(t)

	line := plain(a.legend(100))
	for _, want := range []string{"~/s/aforge-v2", "chat-v3-task*", microcopy} {
		if !strings.Contains(line, want) {
			t.Fatalf("the legend is missing %q:\n%q", want, line)
		}
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

	// No repository, no branch — and no empty separator where one would have
	// gone. The path is what is left, which is the fact that never fails.
	a.branch = ""
	line = plain(a.legend(100))
	if label, _, _ := strings.Cut(strings.TrimPrefix(line, "─ "), " ─"); strings.Contains(label, "·") {
		t.Fatalf("a workspace outside a repository still draws a separator: %q", line)
	}
	if !strings.Contains(line, "~/s/aforge-v2") {
		t.Fatalf("the path went with the branch: %q", line)
	}
}

// THE NARROW LADDER: the microcopy goes before the branch, and the branch goes
// before the path is touched.
func TestTheLegendDropsTheMicrocopyBeforeTheBranch(t *testing.T) {
	a, _, _ := hudApp(t)
	a.branch, a.branchDirty = "feature/the-very-long-branch-name", false

	tight := plain(a.legend(60))
	if strings.Contains(tight, microcopy) || strings.Contains(tight, "feature/") {
		t.Fatalf("a tight frame kept its furniture: %q", tight)
	}
	if !strings.Contains(tight, "~/s/aforge-v2") {
		t.Fatalf("the tight legend lost the one fact it is for: %q", tight)
	}

	// Between the two: room for the branch, not for both.
	middle := plain(a.legend(75))
	if strings.Contains(middle, microcopy) {
		t.Fatalf("the microcopy outlived the branch: %q", middle)
	}
	if !strings.Contains(middle, "feature/the-very-long-branch-name") {
		t.Fatalf("the branch was dropped before the microcopy: %q", middle)
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
	if got := a.burnSegment(); got != "1k tok/s" {
		t.Fatalf("the burn reads %q, want 1k tok/s", got)
	}
	if line := plain(a.status(200)); !strings.Contains(line, "1k tok/s") {
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
	if got := a.hintWord(); got != "enter switch · esc" {
		t.Fatalf("an open picker offered %q", got)
	}
	a.pick.open = false

	// A call parked on a person offers the keys that answer it — the SAME keys
	// consent.go reads, which is what makes the hint safe to act on.
	a.entries = append(a.entries, entry{kind: entryTool, tool: "bash", status: toolConsent})
	hint := a.hintWord()
	for _, want := range []string{"a allow", "t always", "d deny"} {
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
// the state cluster and the legend's path are violet — and nothing else on the
// HUD is allowed to compete, the age fade included.
func TestAWaitingQuestionRoutesTheHueAndQuietsEverythingElse(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	_ = agent
	a.width = 200
	a.home, a.workspace = "/home/dev", "/home/dev/src/aforge-v2"
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
	if !strings.Contains(a.legend(120), a.pal.ask("~/s/aforge-v2")) {
		t.Fatalf("the legend's path did not answer the question:\n%q", a.legend(120))
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
	a.home, a.workspace, a.place = "/home/dev", "/home/dev/src/aforge-v2", "aforge-v2"
	a.ctxWindow = 200_000
	a.title, a.cost = "the bottom hud wave", 1.42
	runTurn(t, a, agent, "bump the limit")
	// The branch is pinned AFTER the turn: a settled turn re-asks the
	// repository (app.go's [app.settle]), and this directory does not have one.
	a.branch = "chat-v3-task"
	a.ctxTokens = 100_000
	a.ctxRing = []int{20_000, 60_000, 100_000}
	a.inputTokens, a.cacheRead = 10_000, 6_200

	spark := a.ctxSpark()
	for _, tc := range []struct {
		width       int
		delta, sp   bool
		branch, mic bool
		rows        int
	}{
		{width: 200, delta: true, sp: true, branch: true, mic: true, rows: 1},
		{width: 120, delta: true, sp: true, branch: true, mic: true, rows: 1},
		{width: 100, delta: false, sp: true, branch: true, mic: true, rows: 1},
		// At seventy the clusters stop sharing a row — the identity and the
		// telemetry cannot both fit with a barrier between them — and everything
		// else is still on.
		{width: 70, delta: false, sp: true, branch: true, mic: true, rows: 2},
		// Below the tight floor the legend keeps the path alone and the meter
		// keeps the number alone.
		{width: 60, delta: false, sp: false, branch: false, mic: false, rows: 2},
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
		if !strings.Contains(legend, "aforge-v2") {
			t.Fatalf("at %d columns the legend lost the place: %q", tc.width, legend)
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
	updates  chan session.Event
	pending  []uint64
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

func (f *taskFake) TaskUpdates() <-chan session.Event { return f.updates }
func (f *taskFake) PendingTasks() []uint64            { return f.pending }

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
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	a.clock = func() time.Time { return now }
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

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

// THE PROPOSAL IS A DECISION MOMENT, INLINE: the title and the summary are what
// a person decides on, and the brief — the node's whole contract with its runner
// — is behind the expansion every other detail on this surface is behind.
func TestATaskProposalRendersTheDecisionAndHidesTheBrief(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)})

	text := taskText(a)
	for _, want := range []string{"Fix the nil-map crash", "The parser drops a key", "auto-starts in 4s"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the proposal is missing %q:\n%s", want, text)
		}
	}
	for _, hidden := range []string{"builds its map lazily", "go test ./internal/parse"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("the collapsed proposal leaked %q:\n%s", hidden, text)
		}
	}
	// IT IS A QUESTION, SO IT TAKES THE QUESTION HUE — the same violet the
	// consent block spends and nothing else on this surface does.
	painted := a.visible(a.bodyWidth())[0].text
	if !strings.Contains(painted, sgr256(hueAsk)) {
		t.Fatalf("the proposal is not painted in the question hue:\n%q", painted)
	}
	// And the surface says, everywhere it says anything, that it is waiting.
	if word, _ := a.stateWord(); word != waitingWord {
		t.Fatalf("the status word is %q while a proposal is open", word)
	}
	if hint := a.hintWord(); hint != taskProposalHint {
		t.Fatalf("the hint slot says %q while a proposal is open", hint)
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

// THE COUNTDOWN TICKS ON THE FRAME CLOCK — no ticker of its own — and at the
// deadline the row stops asking: the engine's clock owns the answer, and a card
// still counting down would be a question nobody can answer any more.
func TestTheProposalCountdownTicksAndStopsAtTheDeadline(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)})

	if !strings.Contains(taskText(a), "auto-starts in 4s") {
		t.Fatalf("the countdown did not open at 4s:\n%s", taskText(a))
	}
	advance(3200 * time.Millisecond)
	drive(t, a, frameMsg{})
	if !strings.Contains(taskText(a), "auto-starts in 1s") {
		t.Fatalf("the countdown did not tick down:\n%s", taskText(a))
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
	// The clock is the ENGINE's: the surface stops asking and answers nothing.
	if len(agent.answered) != 0 {
		t.Fatalf("the surface raced the engine's clock: %+v", agent.answered)
	}
}

// THE INPUT BOX IS THE REDIRECT LANE. Typing is a correction that reaches the
// engine as the person wrote it; a bare enter starts the work as briefed.
func TestTheRedirectLaneReachesResolveTask(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)})

	// The box says what it is for while the question is open.
	block, _, _, _ := a.chrome(a.width)
	if !strings.Contains(plain(strings.Join(block, "\n")), taskRedirectLane) {
		t.Fatalf("the input box does not offer the redirect lane:\n%s", plain(strings.Join(block, "\n")))
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

	// A bare enter is approval as briefed.
	agent.pending = []uint64{8}
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 8, 0)}, key("enter"))
	last := agent.answered[len(agent.answered)-1]
	if last.id != 8 || !last.answer.Approved || last.answer.Redirect != "" {
		t.Fatalf("a bare enter reached the engine as %+v", last)
	}
	// A proposal with no clock draws no countdown: zero is a clock that is off.
	if strings.Contains(taskText(a), "auto-starts in") {
		t.Fatalf("a proposal with no deadline drew a countdown:\n%s", taskText(a))
	}
}

// ESC DECLINES, and it declines the PROPOSAL rather than interrupting the turn:
// while a question is up, the dismiss key is the answer "no".
func TestEscDeclinesTheProposalRatherThanTheTurn(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	a.state = stateWorking
	drive(t, a, streamEventMsg{gen: a.gen, ev: proposal(a, 7, 4*time.Second)}, key("esc"))

	if len(agent.answered) != 1 || agent.answered[0].answer.Approved {
		t.Fatalf("esc did not decline the proposal: %+v", agent.answered)
	}
	if agent.stops != 0 {
		t.Fatal("esc interrupted the turn as well as declining the proposal")
	}
	if !strings.Contains(taskText(a), taskDeclinedWord) {
		t.Fatalf("the declined row does not keep its verdict:\n%s", taskText(a))
	}
	if a.awaitingTask() {
		t.Fatal("the proposal is still asking after it was declined")
	}
}

// THE RAIL IS PRESENCE: it appears when a node is alive, says which state each
// node is in, and disappears when the work has come home.
func TestTheRailStandsWhileWorkIsAliveAndGoesWhenItLands(t *testing.T) {
	a, _, advance := taskApp(t)
	if a.railShowing() {
		t.Fatal("an empty session drew a rail")
	}
	if a.bodyWidth() != 200 {
		t.Fatalf("an empty session charged %d columns for a rail", 200-a.bodyWidth())
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskRunning, session.TaskNotice{})})
	if !a.railShowing() {
		t.Fatal("a running node did not raise the rail")
	}
	advance(12 * time.Second)
	rail := plain(strings.Join(a.railRows(10), "\n"))
	if !strings.Contains(rail, "Fix the nil-map crash") || !strings.Contains(rail, "12s") {
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
	// A BRANCH NAME IS NEVER ELLIPSIZED: the conflicted sentence wraps inside
	// the rail rather than losing the one handle back to the work, so the
	// assertion is on the two halves and not on one line.
	rail = plain(strings.Join(a.railRows(12), "\n"))
	for _, want := range []string{glyphQueued + " Mix audio", glyphBad + " Collect sources",
		glyphDone + " Fix the nil-map crash", "conflicted ·", "task/fix-nil-map"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("the rail is missing %q:\n%s", want, rail)
		}
	}

	// A node whose branch CAME HOME has said everything it has to say in the
	// transcript, so it leaves the rail the moment it lands — and with the last
	// node gone, so does the rail.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(8, "Mix audio", session.TaskDone, session.TaskNotice{
		Elapsed: 8 * time.Second, Merge: mergeWordMerged,
	})})
	if strings.Contains(plain(strings.Join(a.railRows(12), "\n")), "Mix audio") {
		t.Fatal("a merged node stayed on the rail")
	}
	if !a.railShowing() {
		t.Fatal("the rail left while two kept branches were still on it")
	}
	// The two kept branches are what remains, and dealing with them is the
	// person's business — this test only owns the empty case, so it drops them
	// the way /new does.
	a.dropTasks()
	if a.railShowing() {
		t.Fatal("the rail stayed up with nothing on it")
	}
}

// A NODE'S END IS HISTORY, so it lands in the transcript — once, however many
// lanes carried it. The de-dup is (id, state), because an update raised inside a
// turn arrives on the turn's stream AND on the standing subscription.
func TestALandedNodeWritesOneNoteWhateverLaneCarriedIt(t *testing.T) {
	a, agent, _ := taskApp(t)
	done := update(7, "Fix the nil-map crash", session.TaskDone, session.TaskNotice{
		Elapsed: 130 * time.Second, Merge: mergeWordMerged,
	})
	// The same event, on both lanes, exactly as internal/session emits it.
	cmd := a.watchTasks()
	agent.updates <- done
	drive(t, a, append(runCmd(cmd), streamEventMsg{gen: a.gen, ev: done})...)

	text := taskText(a)
	want := "task Fix the nil-map crash done in 2m 10s · merged"
	if strings.Count(text, want) != 1 {
		t.Fatalf("the transcript holds %d copies of %q:\n%s", strings.Count(text, want), want, text)
	}

	// A failure says why, in the report's first line.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(9, "Collect sources", session.TaskFailed, session.TaskNotice{
		Elapsed: 4 * time.Second, Report: "the tests did not build\nsee the log",
	})})
	if !strings.Contains(taskText(a), "task Collect sources failed in 4s — the tests did not build") {
		t.Fatalf("the failure note does not carry its reason:\n%s", taskText(a))
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
	rail := plain(strings.Join(a.railRows(10), "\n"))
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
		// The slim rail fits the title to its column, so the assertion reads
		// the prefix both widths keep.
		if tc.rail && !strings.Contains(lines[0], "Fix the nil-map") {
			t.Fatalf("at %d columns the rail is not on the frame's first row:\n%q", tc.width, lines[0])
		}
		// The status row is the whole window's, so it is never under the rail.
		status := lines[len(lines)-1]
		if strings.Contains(status, "│") {
			t.Fatalf("at %d columns the rail's seam reached the status row:\n%q", tc.width, status)
		}
	}
}
