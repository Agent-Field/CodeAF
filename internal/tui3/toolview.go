package tui3

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

// THE TOOL CLUSTER (docs/CHAT-V3.md D11).
//
// A turn's calls are one object on the screen, not a stream of them: a rail
// down the left, one row per call, the elbow closing the run.
//
//	├─▶ read internal/session/session.go   · 189 lines
//	├─▶ edit internal/session/loop.go      +3 −1
//	│ internal/session/loop.go
//	│ @@ -1,4 +1,4 @@
//	│   // argsLimit bounds Event.Args.
//	│ -const argsLimit = 400
//	│ +const argsLimit = 8192
//	╰─▶ bash go test ./internal/session    exit 1 ✗
//
// The grammar is four parts and every one of them is a decision:
//
//   - the RAIL says these rows are one thing. `╰─▶` is the last call, `├─▶`
//     every call above it, `│ ` an expanded call's detail; a terminal that
//     cannot draw them gets `+-> ` and `| `, which are the same widths.
//   - the NAME is chrome, so it is the muted accent. The TARGET — the path,
//     the command, the pattern — is what the person is actually reading, so it
//     is primary ink and never dimmed.
//   - the STAT trails, dim, and is derived from the payload in toolstat.go.
//   - the MARK: nothing at all on success, `✗` on failure, a spinner at the
//     right edge while the call runs. NO SUCCESS GLYPH, EVER. A column of ✓ is
//     a column that must be read to learn nothing; a quiet line is a success.
//
// At most [toolWindow] calls stay on screen; the rest fold into one line that
// says how many, and ctrl+o (or a click on that line) unfolds them. One call
// opens inline — click it, or select it with ↑/↓ and press enter — and shows a
// tool-shaped expansion under the rail rather than in a pane somewhere else.

// toolDetail is a call's payload as the surface holds it: the two display
// fields internal/session sends (session.go's Event.Args and Event.Output),
// unparsed. Everything derived from them — the stat, the diff, the previews —
// is computed in toolstat.go at render time, so this struct never has to be
// invalidated when a colour or a cap changes.
type toolDetail struct {
	// Args is the call's arguments, the compacted JSON the model sent.
	Args string
	// Output is what the call returned, capped by session for display.
	Output string
}

// clusterRows lays out one contiguous run of tool entries — a.entries[from:to],
// all from one turn — and appends it to out. This is where the fold lives,
// because folding is a property of the RUN and not of any call in it, and it is
// where the elbow is chosen for the same reason.
func (a *app) clusterRows(out []row, from, to, width int) []row {
	turn := a.entries[from].turn
	start := from
	if to-from > toolWindow && !a.unfolded[turn] {
		start = to - toolWindow
		out = append(out, row{
			text:  a.pal.dim(glyphTool + foldWord(start-from)),
			entry: -1,
			hit:   hitFold,
			turn:  turn,
		})
	}
	for i := start; i < to; i++ {
		out = append(out, a.toolRows(i, i == to-1, width)...)
	}
	return out
}

// foldWord is the fold line's sentence. It names the key that opens it, because
// a surface that hides something without saying how to see it has hidden it.
func foldWord(n int) string {
	if n == 1 {
		return "1 earlier tool call · ctrl+o"
	}
	return strconv.Itoa(n) + " earlier tool calls · ctrl+o"
}

// toolRows is one call: its line, plus its expansion when it is open.
//
// It returns rows rather than strings — unlike every other entry — because the
// "… N more lines" foot of a capped expansion is a DIFFERENT click target from
// the line it hangs under: one lifts the cap, the other closes the call.
func (a *app) toolRows(i int, last bool, width int) []row {
	e := &a.entries[i]
	out := []row{{text: a.toolLine(e, i, last, width), entry: i, hit: hitTool}}
	if !e.open {
		return out
	}
	stem := a.pal.railCont()
	body, more := a.detailBody(e, width-ansi.StringWidth(a.pal.railCont()))
	for _, line := range body {
		out = append(out, row{text: a.pal.dim(stem) + line, entry: i, hit: hitTool})
	}
	if more > 0 {
		out = append(out, row{
			text:  a.pal.dim(stem + glyphMore + " " + strconv.Itoa(more) + " more lines"),
			entry: i,
			hit:   hitMore,
		})
	}
	return out
}

// toolLine is the line itself.
//
// The pieces are measured as PLAIN text and painted afterwards, which is the
// only way the widths land: a width measured through escape sequences is a
// width measured wrong. When the row is too narrow for everything, the stat
// goes first and the target is truncated last — the target is the substance,
// and a stat nobody has room for is a number about a line nobody can read.
func (a *app) toolLine(e *entry, i int, last bool, width int) string {
	name, fallback := toolWords(e.tool, e.text)
	target := toolTarget(e.tool, e.detail.Args, e.text)
	if target == "" {
		target = fallback
	}
	statPlain, statPainted := a.toolStat(e)
	// What the person answered when this call was asked about (consent.go). It
	// rides the stat slot because it is the same kind of fact — dim, trailing,
	// about the call rather than in it — and because a row that was approved
	// must still read as one row.
	if e.decision != "" {
		if statPlain == "" {
			statPlain, statPainted = e.decision, a.pal.dim(e.decision)
		} else {
			statPlain += " · " + e.decision
			statPainted += a.pal.dim(" · " + e.decision)
		}
	}
	mark := a.mark(e)

	rail := a.pal.rail(last)
	railWidth := ansi.StringWidth(rail)
	nameWidth := ansi.StringWidth(name)

	// The right edge holds the spinner; the ✗ is appended to the text instead,
	// so a failure reads as part of the sentence rather than as a column.
	reserve := 1
	if e.status == toolFailed {
		reserve = 2
	}
	room := width - railWidth - nameWidth - reserve
	if statWidth := ansi.StringWidth(statPlain) + 2; room-statWidth < 8 {
		statPlain, statPainted = "", ""
	} else {
		room -= statWidth
	}
	target = fit(target, room-1)

	// A selected line takes the accent on its rail — no band, no marker
	// column, nothing that changes the width. Selection is a brightness here,
	// which is what a one-line row can carry honestly.
	painted := a.pal.dim(rail)
	if a.sel == i {
		painted = a.pal.accent(rail)
	}
	line := painted + a.pal.muted(name)
	used := railWidth + nameWidth
	if target != "" {
		line += " " + a.pal.ink(target)
		used += 1 + ansi.StringWidth(target)
	}
	if statPlain != "" {
		line += "  " + statPainted
		used += 2 + ansi.StringWidth(statPlain)
	}
	switch {
	case e.status == toolFailed:
		return line + " " + mark
	case mark == "":
		return line
	default:
		// The spinner sits at the line's right end and turns in place.
		if pad := width - used - 1; pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		return line + mark
	}
}

// mark is what the right of a tool line says about how the call is going —
// which, on success, is nothing at all.
func (a *app) mark(e *entry) string {
	switch e.status {
	case toolFailed:
		return a.pal.bad(glyphBad)
	case toolOK:
		return ""
	default:
		if a.state != stateWorking {
			// The turn ended with this call unresolved — interrupted, or the
			// stream closed without a close event. A spinner frozen mid-turn
			// would claim the call is still alive.
			return a.pal.dim(glyphIdle)
		}
		return a.pal.muted(tokens.Spinner(a.paints / spinnerStep))
	}
}

// detailBody is what one open call shows, per tool (D11's table), already
// painted and WITHOUT the rail — [app.toolRows] hangs the stem on. It returns
// the rows it kept and how many it dropped; the caller draws the drop as a
// clickable "… N more lines", and a call whose cap has been lifted (e.full)
// drops nothing.
//
// Rows TRUNCATE rather than wrap, for prose's reason about tables: a row that
// stays a row can be counted, and "first 30 lines" has to mean thirty lines on
// screen or it means nothing.
func (a *app) detailBody(e *entry, width int) ([]string, int) {
	if width < 8 {
		width = 8
	}
	if e.status == toolRunning {
		// The one animated expansion: a call with nothing to show yet says so
		// in the same breath the spinner is drawing.
		return []string{a.pal.dim("running" + ellipsisFrames[(a.paints/pulseStep)%len(ellipsisFrames)])}, 0
	}

	switch e.tool {
	case "edit":
		return a.cap(e, a.diffRows(e, width), diffWindow)
	case "write":
		content := argString(argsOf(e.detail.Args), "content")
		return a.cap(e, a.plainRows(content, width), writeWindow)
	case "read":
		return a.cap(e, a.plainRows(resultText(e.detail.Output), width), readWindow)
	case "bash":
		return a.cap(e, a.bashRows(e, width), bashWindow)
	case "grep", "find", "ls":
		return a.cap(e, a.plainRows(resultText(e.detail.Output), width), listWindow)
	}
	return a.cap(e, a.genericRows(e, width), listWindow)
}

// cap bounds one expansion. The window is the tool's own (D11's table), and a
// call the person has clicked "more" on has no window at all — they asked.
func (a *app) cap(e *entry, rows []string, window int) ([]string, int) {
	if len(rows) == 0 {
		return []string{a.pal.dim("—")}, 0
	}
	if e.full || len(rows) <= window {
		return rows, 0
	}
	return rows[:window], len(rows) - window
}

// diffRows is an edit's expansion: the file it touched, then a unified diff of
// every replacement it sent — computed here from the old/new strings, because
// that pair is the only record of the change that exists (the tool's own result
// is one sentence saying it worked).
func (a *app) diffRows(e *entry, width int) []string {
	fields := argsOf(e.detail.Args)
	pairs := editPairs(e.detail.Args)
	if len(pairs) == 0 {
		return a.genericRows(e, width)
	}
	out := make([]string, 0, 16)
	if path := argString(fields, "path"); path != "" {
		out = append(out, a.pal.dim(fit(path, width)))
	}
	for _, pair := range pairs {
		ops := diffOps(splitLines(pair.old), splitLines(pair.new))
		for _, h := range hunks(ops) {
			out = append(out, a.pal.dim(fit(h.header(), width)))
			for _, op := range h.ops {
				text := fit(string(op.kind)+expandTabs(op.text), width)
				switch op.kind {
				case '+':
					out = append(out, a.pal.add(text))
				case '-':
					out = append(out, a.pal.del(text))
				default:
					out = append(out, a.pal.dim(text))
				}
			}
		}
	}
	return out
}

// bashRows is a command's output, with its exit line kept at the foot when
// there was one: a build log's last thirty lines are the interesting ones, and
// the code is what the person opened the row to see.
func (a *app) bashRows(e *entry, width int) []string {
	out := a.plainRows(resultText(e.detail.Output), width)
	if code, failed := bashExit(e.detail.Output); failed {
		out = append(out, a.pal.bad(fit("exit "+itoa(code), width)))
	}
	return out
}

// genericRows is the expansion for a tool this surface has no table row for —
// a workforce tool, a tool added tomorrow. It shows what went in and what came
// back, which is the honest floor.
func (a *app) genericRows(e *entry, width int) []string {
	var out []string
	if args := strings.TrimSpace(e.detail.Args); args != "" {
		out = append(out, a.pal.dim(fit(expandTabs(args), width)))
	}
	return append(out, a.plainRows(resultText(e.detail.Output), width)...)
}

// plainRows is a block of evidence: dim, truncated to width, one row per line.
func (a *app) plainRows(text string, width int) []string {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, a.pal.dim(fit(expandTabs(line), width)))
	}
	return out
}

// resultText is a tool result as evidence: session's display cap marker and
// bare's trailing notice block removed, because both are sentences ABOUT the
// result and the expansion is showing the result.
func resultText(output string) string {
	body, _ := outputBody(output)
	return body
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// toolWords splits a call into the two things a line says: the tool's NAME and
// a gloss of what it was pointed at. Session hints usually lead with the tool's
// own name ("read internal/session/session.go"), so the name is stripped from
// the front of the gloss — a line that printed both would say "read read
// internal/session/session.go". The gloss is the FALLBACK target: the payload
// is asked first (see [toolTarget]).
func toolWords(tool, hint string) (string, string) {
	tool, hint = strings.TrimSpace(tool), strings.TrimSpace(firstLine(hint))
	if tool == "" {
		return hint, ""
	}
	if rest, cut := strings.CutPrefix(hint, tool); cut {
		return tool, strings.TrimSpace(rest)
	}
	return tool, hint
}

// ToolGloss is how a tool's activity is said in ONE plain sentence, with the
// verb said once. It is exported because the non-interactive door in
// cmd/aforge prints the same fact without a terminal, and one rule for one
// sentence is the point.
func ToolGloss(tool, hint string) string {
	name, gloss := toolWords(tool, hint)
	if gloss == "" {
		return name
	}
	return name + " " + gloss
}
