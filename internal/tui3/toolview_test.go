package tui3

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// The tool column's law is docs/CHAT-V3.md D11, and these are its acceptance
// tests: the grammar of one line, the arithmetic behind its stat, the shape of
// its expansion, and the two tiers it degrades through.

// call scripts one whole tool call the way session emits it: a begin with the
// arguments, an end with the arguments AND the result (loop.go sends a
// self-contained end).
func call(tool, args, output string) []session.Event {
	return []session.Event{
		{Kind: session.EventToolBegin, Tool: tool, Hint: tool, Args: args},
		{Kind: session.EventToolEnd, Tool: tool, Args: args, Output: output},
	}
}

// failedCall is the same with the end the loop sends for a tool that reported
// an error: the hint says why, the output is the result text anyway.
func failedCall(tool, args, why, output string) []session.Event {
	return []session.Event{
		{Kind: session.EventToolBegin, Tool: tool, Hint: tool, Args: args},
		{Kind: session.EventToolFailed, Tool: tool, Hint: why, Args: args, Output: output},
	}
}

// toolApp runs one turn of scripted calls and hands back the settled surface.
func toolApp(t *testing.T, profile tokens.Profile, batches ...[]session.Event) *app {
	t.Helper()
	var events []session.Event
	for _, batch := range batches {
		events = append(events, batch...)
	}
	events = append(events, session.Event{Kind: session.EventTurnDone})
	agent := &fakeAgent{model: "m", turns: [][]session.Event{events}}
	a := newTestApp(agent)
	a.pal = newPalette(profile, false)
	runTurn(t, a, agent, "go on then")
	return a
}

// toolLineOf is the first tool row on screen, plain.
func toolLineOf(t *testing.T, a *app) string {
	t.Helper()
	for _, r := range plainRows(a) {
		r = strings.TrimLeft(r, " ")
		if strings.HasPrefix(r, railMid) || strings.HasPrefix(r, railLast) ||
			strings.HasPrefix(r, railASCII) {
			return r
		}
	}
	t.Fatalf("no tool line was drawn:\n%s", strings.Join(plainRows(a), "\n"))
	return ""
}

// openFirst makes sure the first tool call is expanded and returns the
// surface's rows. A FAILED call opens itself (app.go), so this asks for the
// state rather than toggling it — a toggle would close the one row this helper
// exists to read.
func openFirst(t *testing.T, a *app) []string {
	t.Helper()
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			if !a.entries[i].open {
				a.openTool(i)
			}
			a.touch()
			return plainRows(a)
		}
	}
	t.Fatal("no tool entry")
	return nil
}

func editArgs(t *testing.T, path string, pairs ...[2]string) string {
	t.Helper()
	type edit struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	}
	payload := struct {
		Path  string `json:"path"`
		Edits []edit `json:"edits"`
	}{Path: path}
	for _, p := range pairs {
		payload.Edits = append(payload.Edits, edit{OldText: p[0], NewText: p[1]})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// ── the diffstat ────────────────────────────────────────────────────────────

// THE ARITHMETIC. +N −M is the number of lines that actually changed, computed
// from the old/new pair by longest common subsequence — not the size of the two
// blocks, which would report every replacement as "all of it".
func TestDiffStatCountsOnlyWhatChanged(t *testing.T) {
	cases := []struct {
		name       string
		old, new   string
		adds, dels int
	}{
		{
			name: "one line swapped inside a block",
			old:  "alpha\nbravo\ncharlie\ndelta",
			new:  "alpha\nbravo\nCHARLIE\ndelta",
			adds: 1, dels: 1,
		},
		{
			name: "a middle line replaced and a line appended",
			old:  "a\nb\nc\nd",
			new:  "a\nx\nc\nd\ne",
			adds: 2, dels: 1,
		},
		{
			name: "pure insertion between two kept lines",
			old:  "top\nbottom",
			new:  "top\nmiddle\nmore\nbottom",
			adds: 2, dels: 0,
		},
		{
			name: "pure deletion",
			old:  "one\ntwo\nthree",
			new:  "one\nthree",
			adds: 0, dels: 1,
		},
		{
			name: "a block that moved is not a rewrite",
			old:  "x\nkeep me\nkeep me too\ny",
			new:  "keep me\nkeep me too",
			adds: 0, dels: 2,
		},
		{
			name: "nothing changed",
			old:  "same\nsame",
			new:  "same\nsame",
			adds: 0, dels: 0,
		},
		{
			name: "a trailing newline does not open a line",
			old:  "a\nb\n",
			new:  "a\nB\n",
			adds: 1, dels: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adds, dels := diffStat(splitLines(tc.old), splitLines(tc.new))
			if adds != tc.adds || dels != tc.dels {
				t.Fatalf("diffStat = +%d −%d, want +%d −%d", adds, dels, tc.adds, tc.dels)
			}
			// The same arithmetic through the payload the model actually sends.
			adds, dels = editStat(editArgs(t, "f.go", [2]string{tc.old, tc.new}))
			if adds != tc.adds || dels != tc.dels {
				t.Fatalf("editStat = +%d −%d, want +%d −%d", adds, dels, tc.adds, tc.dels)
			}
		})
	}
}

// An edit call's replacements sum: one call, one stat.
func TestTheEditStatSumsEveryReplacement(t *testing.T) {
	args := editArgs(t, "internal/session/loop.go",
		[2]string{"const argsLimit = 400", "const argsLimit = 8192"},
		[2]string{"old one\nold two", "new one\nnew two\nnew three"},
	)
	a := toolApp(t, tokens.NoColor, call("edit", args, "Successfully replaced 2 block(s) in loop.go."))

	// One line swapped in the first block (+1 −1), a whole two-line block
	// rewritten into three in the second (+3 −2).
	if line := toolLineOf(t, a); !strings.Contains(line, "+4 −3") {
		t.Fatalf("the edit line is missing its diffstat: %q", line)
	}
}

// The legacy spellings are read too: whatever bare's prepareEditArguments
// executes is what this line has to describe.
func TestTheEditStatReadsEveryArgumentSpelling(t *testing.T) {
	for name, args := range map[string]string{
		"legacy oldText/newText": `{"path":"f.go","oldText":"a\nb","newText":"a\nB"}`,
		"old_string/new_string":  `{"path":"f.go","old_string":"a\nb","new_string":"a\nB"}`,
		"edits sent as a string": `{"path":"f.go","edits":"[{\"oldText\":\"a\\nb\",\"newText\":\"a\\nB\"}]"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if adds, dels := editStat(args); adds != 1 || dels != 1 {
				t.Fatalf("editStat = +%d −%d, want +1 −1", adds, dels)
			}
		})
	}
}

// ── the expansion ───────────────────────────────────────────────────────────

// THE DIFF VIEW. An edit opens into a unified diff of the change it made,
// computed from the arguments — the only record of it that reaches a screen —
// with the removals in pastel red and the additions in pastel green.
func TestTheEditExpansionIsAPaintedUnifiedDiff(t *testing.T) {
	args := editArgs(t, "internal/session/loop.go", [2]string{
		"// argsLimit bounds Event.Args.\nconst argsLimit = 400\n\n// outputLimit bounds it.",
		"// argsLimit bounds Event.Args.\nconst argsLimit = 8192\n\n// outputLimit bounds it.",
	})
	a := toolApp(t, tokens.TrueColor, call("edit", args, "Successfully replaced 1 block(s) in loop.go."))
	openFirst(t, a)

	body := strings.Join(plainRows(a), "\n")
	for _, want := range []string{
		"│ internal/session/loop.go", // the file it touched
		"│ @@ -1,4 +1,4 @@",          // the hunk marker
		"│  // argsLimit bounds",     // context, carried unmarked
		"│ -const argsLimit = 400",   // the removal
		"│ +const argsLimit = 8192",  // the addition
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the diff is missing %q:\n%s", want, body)
		}
	}

	// And the two changed rows are painted, in the authored pastels.
	green, red := "", ""
	for _, r := range rows(a) {
		switch {
		case strings.Contains(plain(r.text), "+const argsLimit = 8192"):
			green = r.text
		case strings.Contains(plain(r.text), "-const argsLimit = 400"):
			red = r.text
		}
	}
	if !strings.Contains(green, "\x1b[38;2;163;190;140m") {
		t.Fatalf("the + line is not pastel green: %q", green)
	}
	if !strings.Contains(red, "\x1b[38;2;191;97;106m") {
		t.Fatalf("the − line is not pastel red: %q", red)
	}
}

// A diff longer than its window stops at the window and says how much it is not
// showing; the foot is a click target that lifts the cap.
func TestTheDiffExpansionIsCappedAndTheCapLifts(t *testing.T) {
	var old, new []string
	for i := 0; i < 60; i++ {
		old = append(old, "line "+itoa(i))
		new = append(new, "LINE "+itoa(i))
	}
	args := editArgs(t, "big.go", [2]string{strings.Join(old, "\n"), strings.Join(new, "\n")})
	a := toolApp(t, tokens.NoColor, call("edit", args, "Successfully replaced 1 block(s) in big.go."))
	call := -1
	for i := range a.entries {
		if a.entries[i].kind == entryTool {
			call = i
		}
	}
	a.openTool(call)

	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "more lines") {
		t.Fatalf("a 120-row diff was not capped:\n%s", body)
	}
	shown := 0
	for _, r := range plainRows(a) {
		r = strings.TrimLeft(r, " ")
		if strings.HasPrefix(r, railCont) && !strings.Contains(r, "more lines") {
			shown++
		}
	}
	if shown != diffWindow {
		t.Fatalf("the expansion drew %d rows, want the %d-row window", shown, diffWindow)
	}

	a.showAll(call)
	if strings.Contains(strings.Join(plainRows(a), "\n"), "more lines") {
		t.Fatal("the cap did not lift")
	}
	// Closing the call forgets that it was lifted: the next opening is a window
	// again.
	a.openTool(call)
	a.openTool(call)
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "more lines") {
		t.Fatal("a closed call kept its lifted cap")
	}
}

// A write opens into the content it wrote — which lives in the ARGUMENTS, the
// result being one sentence about bytes — and a read into the chunk it
// returned, which lives in the output. Each is bounded by its own window.
func TestTheWriteAndReadExpansionsComeFromDifferentSides(t *testing.T) {
	var content []string
	for i := 0; i < 30; i++ {
		content = append(content, "written line "+itoa(i))
	}
	args, err := json.Marshal(map[string]string{
		"path": "new.go", "content": strings.Join(content, "\n"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	a := toolApp(t, tokens.NoColor, call("write", string(args), "Successfully wrote 431 bytes to new.go"))
	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, railCont+"written line 0") {
		t.Fatalf("the write expansion is not its content:\n%s", body)
	}
	if !strings.Contains(body, "… 10 more lines") || strings.Contains(body, "written line 20") {
		t.Fatalf("the write expansion is not bounded by its %d-row window:\n%s", writeWindow, body)
	}

	var chunk []string
	for i := 0; i < 40; i++ {
		chunk = append(chunk, "read line "+itoa(i))
	}
	a = toolApp(t, tokens.NoColor, call("read", `{"path":"a.go"}`, strings.Join(chunk, "\n")))
	body = strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, railCont+"read line 0") || !strings.Contains(body, "… 10 more lines") {
		t.Fatalf("the read expansion is not its returned chunk, bounded:\n%s", body)
	}
}

// ── the per-tool stat ───────────────────────────────────────────────────────

// D11's table, one row at a time: the number on the line comes from the
// payload, and it is the number that tool's result actually reports.
func TestEveryToolCarriesItsOwnStat(t *testing.T) {
	cases := []struct {
		name, tool, args, output string
		want                     string
	}{
		{
			name: "read counts the lines it returned",
			tool: "read", args: `{"path":"a.go"}`,
			output: "one\ntwo\nthree",
			want:   "· 3 lines",
		},
		{
			name: "read believes the tool's own footer over the copy it was sent",
			tool: "read", args: `{"path":"a.go"}`,
			output: "one\ntwo\n\n[Showing lines 1-120 of 4000. Use offset=121 to continue.]",
			want:   "· 120 lines",
		},
		{
			name: "write counts the content it wrote",
			tool: "write", args: `{"path":"a.go","content":"package main\n\nfunc main() {}\n"}`,
			output: "Successfully wrote 31 bytes to a.go",
			want:   "+3 lines",
		},
		{
			name: "grep counts matches, not the lines they arrived on",
			tool: "grep", args: `{"pattern":"func main"}`,
			output: "a.go:3: func main()\nb.go:9: func main()",
			want:   "· 2 matches",
		},
		{
			name: "a search that found nothing says so",
			tool: "grep", args: `{"pattern":"nope"}`,
			output: "No matches found",
			want:   "· 0 matches",
		},
		{
			name: "find counts entries",
			tool: "find", args: `{"pattern":"**/*.go"}`,
			output: "a.go\nb.go\nc.go",
			want:   "· 3 entries",
		},
		{
			name: "ls counts one entry as one entry",
			tool: "ls", args: `{"path":"."}`,
			output: "only.go",
			want:   "· 1 entry",
		},
		{
			name: "a capped copy says at least",
			tool: "ls", args: `{"path":"."}`,
			output: "a\nb\nc… (900 more bytes)",
			want:   "· 3+ entries",
		},
		{
			name: "a listing's notice block is not an entry",
			tool: "ls", args: `{"path":"."}`,
			output: "a\nb\n\n[500 entries limit reached. Use limit=1000 for more]",
			want:   "· 2 entries",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := toolApp(t, tokens.NoColor, call(tc.tool, tc.args, tc.output))
			if line := toolLineOf(t, a); !strings.Contains(line, tc.want) {
				t.Fatalf("the %s line is %q, want it to carry %q", tc.tool, line, tc.want)
			}
		})
	}
}

// A stat is a claim about a result, so a call whose result never reached the
// surface makes none — "· 0 matches" would be a search this line did not watch.
func TestACallWithNoResultDrawsNoStat(t *testing.T) {
	for _, tool := range []string{"read", "grep", "find", "ls"} {
		t.Run(tool, func(t *testing.T) {
			a := toolApp(t, tokens.NoColor, call(tool, `{"path":"a.go","pattern":"x"}`, ""))
			if line := toolLineOf(t, a); strings.Contains(line, "·") {
				t.Fatalf("a %s call with no result invented a stat: %q", tool, line)
			}
		})
	}
}

// THE QUIET LINE. A command that worked says its name and its command and
// nothing else — no glyph, no stat, no punctuation standing in for either.
func TestASuccessfulBashLineSaysNothingExtra(t *testing.T) {
	a := toolApp(t, tokens.NoColor,
		call("bash", `{"command":"go test ./internal/tui3"}`, "ok  \tinternal/tui3\t0.412s"))

	line := strings.TrimRight(toolLineOf(t, a), " ")
	if want := railLast + "bash go test ./internal/tui3"; line != want {
		t.Fatalf("the line is %q, want exactly %q", line, want)
	}
	for _, forbidden := range []string{"✓", "✗", "exit", "·"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("a quiet success drew %q: %q", forbidden, line)
		}
	}
}

// AND THE LOUD ONE. Only failure speaks: the soft ✗, and the exit code the
// result text reported.
func TestAFailedBashLineShowsTheCodeAndTheCross(t *testing.T) {
	a := toolApp(t, tokens.TrueColor, failedCall("bash",
		`{"command":"go build ./..."}`,
		"internal/tui3/toolview.go:12:2: undefined: railMid",
		"internal/tui3/toolview.go:12:2: undefined: railMid\n\nCommand exited with code 1"))

	line := toolLineOf(t, a)
	if !strings.Contains(line, "exit 1") || !strings.Contains(line, glyphBad) {
		t.Fatalf("a failed command has to say so: %q", line)
	}
	// The target is read from the PAYLOAD, so the failure text session appends
	// to the hint never displaces the command.
	if !strings.Contains(line, "bash go build ./...") {
		t.Fatalf("the failed line lost its command: %q", line)
	}
	// The ✗ is the soft orange-red, never a signal red.
	for _, r := range rows(a) {
		if strings.Contains(plain(r.text), "exit 1") &&
			!strings.Contains(r.text, "\x1b[38;2;208;135;112m") {
			t.Fatalf("the ✗ is not the soft failure hue: %q", r.text)
		}
	}

	// And the expansion keeps the exit line at the foot of the output.
	body := strings.Join(openFirst(t, a), "\n")
	if !strings.Contains(body, "│ internal/tui3/toolview.go:12:2") || !strings.Contains(body, "│ exit 1") {
		t.Fatalf("the failed expansion is missing its output or its code:\n%s", body)
	}
}

// A running call has no stat at all — a number that changes under a spinner is
// a number nobody can read — and its expansion says what it is doing.
func TestARunningCallSpinsAndSaysNothingElse(t *testing.T) {
	agent := &fakeAgent{model: "m", turns: [][]session.Event{{
		{Kind: session.EventToolBegin, Tool: "bash", Hint: "bash", Args: `{"command":"go test ./..."}`},
	}}}
	a := newTestApp(agent)
	typeLine(t, a, "run the tests")

	line := toolLineOf(t, a)
	if strings.Contains(line, "·") || strings.Contains(line, "exit") {
		t.Fatalf("a running call drew a stat: %q", line)
	}
	if !strings.ContainsAny(line, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("a running call has no spinner: %q", line)
	}
}

// ── the target ──────────────────────────────────────────────────────────────

// THE SUBSTANCE. The tool's name is chrome and takes the muted accent; the
// path is what the person is reading and takes primary ink. Never the other
// way round, and never a dimmed target.
func TestTheTargetIsInkAndTheNameIsMuted(t *testing.T) {
	a := toolApp(t, tokens.TrueColor, call("read", `{"path":"internal/session/loop.go"}`, "one\ntwo"))

	var line string
	for _, r := range rows(a) {
		if r.hit == hitTool {
			line = r.text
			break
		}
	}
	name := "\x1b[38;2;127;166;201mread"           // muted accent
	target := "\x1b[38;2;216;222;233minternal/ses" // primary ink
	if !strings.Contains(line, name) {
		t.Fatalf("the tool name is not the muted accent: %q", line)
	}
	if !strings.Contains(line, target) {
		t.Fatalf("the target is not primary ink: %q", line)
	}
	if strings.Contains(line, "\x1b[38;2;107;114;128minternal/") {
		t.Fatalf("the target was dimmed: %q", line)
	}
}

// The payload is asked first and the hint only after: a call with no arguments
// still says what it was pointed at.
func TestTheTargetFallsBackToTheHint(t *testing.T) {
	a := toolApp(t, tokens.NoColor, []session.Event{
		{Kind: session.EventToolBegin, Tool: "read", Hint: "read foo/bar.go"},
		{Kind: session.EventToolEnd, Tool: "read", Output: "one"},
	})
	if line := toolLineOf(t, a); !strings.Contains(line, "read foo/bar.go") {
		t.Fatalf("a payload-less call lost its target: %q", line)
	}
}

// ── the rail ────────────────────────────────────────────────────────────────

// THE MARKERS. A cluster tees on every call and closes on its last, and an open
// call's rows hang from the stem.
func TestTheRailTeesThenClosesAndTheExpansionContinuesIt(t *testing.T) {
	a := toolApp(t, tokens.NoColor,
		call("read", `{"path":"a.go"}`, "one"),
		call("read", `{"path":"b.go"}`, "two"),
		call("read", `{"path":"c.go"}`, "three"),
	)
	list := plainRows(a)
	var marked []string
	for _, r := range list {
		r = strings.TrimLeft(r, " ")
		if strings.HasPrefix(r, railMid) || strings.HasPrefix(r, railLast) {
			marked = append(marked, r[:len(railMid)])
		}
	}
	if len(marked) != 3 {
		t.Fatalf("want 3 tool lines, got %d:\n%s", len(marked), strings.Join(list, "\n"))
	}
	if marked[0] != railMid || marked[1] != railMid || marked[2] != railLast {
		t.Fatalf("markers are %q, want ├─▶ ├─▶ ╰─▶:\n%s", marked, strings.Join(list, "\n"))
	}

	for _, r := range openFirst(t, a) {
		r = strings.TrimLeft(r, " ")
		if strings.HasPrefix(r, railCont) {
			return
		}
	}
	t.Fatalf("no expansion row carries the │ rail:\n%s", strings.Join(plainRows(a), "\n"))
}

// THE ASCII TIER. A terminal that cannot be trusted with box drawing gets
// "+-> " and "| ", at the same widths, and loses nothing else.
func TestTheASCIITierDrawsTheRailInASCII(t *testing.T) {
	a := toolApp(t, tokens.NoColor, call("read", `{"path":"a.go"}`, "one\ntwo"))
	a.pal = newPalette(tokens.NoColor, true)
	a.touch()
	openFirst(t, a)

	list := plainRows(a)
	joined := strings.Join(list, "\n")
	if !strings.Contains(joined, railASCII+"read a.go") {
		t.Fatalf("the ASCII rail is missing:\n%s", joined)
	}
	if strings.ContainsAny(joined, "├╰│▶") {
		t.Fatalf("box drawing survived the ASCII tier:\n%s", joined)
	}
	if !strings.Contains(joined, railContASCII+"one") {
		t.Fatalf("the ASCII stem is missing:\n%s", joined)
	}
	// The stat is unaffected: the tier is about glyphs, not about facts.
	if !strings.Contains(joined, "· 2 lines") {
		t.Fatalf("the ASCII tier lost the stat:\n%s", joined)
	}
}

// ── the palette ─────────────────────────────────────────────────────────────

// THE LADDER. One authored colour, four terminals: exact, nearest, weight,
// nothing.
func TestThePaletteDegradesByProfile(t *testing.T) {
	cases := []struct {
		profile tokens.Profile
		want    string
		absent  string
	}{
		{tokens.TrueColor, "\x1b[38;2;157;195;230m", ""},
		{tokens.ANSI256, "\x1b[38;5;", "38;2;"},
		{tokens.ANSI16, "\x1b[1m", "38;"},
		{tokens.NoColor, "", "\x1b"},
	}
	for _, tc := range cases {
		t.Run(tc.profile.String(), func(t *testing.T) {
			got := newPalette(tc.profile, false).accent("now")
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Fatalf("accent under %v is %q, want it to carry %q", tc.profile, got, tc.want)
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Fatalf("accent under %v is %q, which must not carry %q", tc.profile, got, tc.absent)
			}
			if plain(got) != "now" {
				t.Fatalf("painting changed the text: %q", plain(got))
			}
		})
	}

	// Sixteen colours means WEIGHT, not hue: dim recedes, ink is left alone.
	pal := newPalette(tokens.ANSI16, false)
	if got := pal.dim("meta"); got != "\x1b[2mmeta\x1b[22m" {
		t.Fatalf("dim at sixteen colours is %q, want the faint tier", got)
	}
	if got := pal.ink("body"); got != "body" {
		t.Fatalf("ink at sixteen colours is %q, want it plain", got)
	}
}

// The 256-colour rung never lands on 0-15: those are whatever the user's theme
// says they are, and a palette that resolved into them would be a palette the
// terminal chose.
func TestTheNearestIndexAvoidsTheThemeColours(t *testing.T) {
	for _, h := range []hue{hueInk, hueAccent, hueMuted, hueDim, hueAdd, hueDel, hueBad, hueWarn} {
		if h.idx < 16 {
			t.Fatalf("hue #%02x%02x%02x resolved to index %d", h.r, h.g, h.b, h.idx)
		}
	}
	// And it is genuinely nearest: a pure grey lands on the grey ramp.
	if got := nearest256(128, 128, 128); got < 232 {
		t.Fatalf("mid grey resolved to %d, want the grey ramp", got)
	}
}

// The glyph tier is decided by what the terminal said about itself, and it may
// only veto — like tokens' own detection, there is no positive signal to trust.
func TestTheASCIITierIsDetectedFromTheEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"a UTF-8 terminal draws the rail", map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, false},
		{"no locale at all is the C locale", map[string]string{"TERM": "xterm-256color"}, true},
		{"a non-UTF-8 locale", map[string]string{"TERM": "xterm-256color", "LC_ALL": "C"}, true},
		{"LC_CTYPE outranks LANG", map[string]string{"TERM": "xterm", "LC_CTYPE": "POSIX", "LANG": "en_US.UTF-8"}, true},
		{"no capability claim at all", map[string]string{"LANG": "en_US.UTF-8"}, true},
		{"TERM=dumb", map[string]string{"TERM": "dumb", "LANG": "en_US.UTF-8"}, true},
		{"a UTF-8 locale spelled without the dash", map[string]string{"TERM": "xterm", "LC_ALL": "en_US.utf8"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectASCII(func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("detectASCII = %v, want %v", got, tc.want)
			}
		})
	}
}
