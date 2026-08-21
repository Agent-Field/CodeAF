package tui3

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// PER-TOOL DERIVATION (docs/CHAT-V3.md D11).
//
// Every tool line carries an inline stat and every open tool line carries a
// tool-shaped expansion, and BOTH are derived HERE — in the surface, from the
// Args and Output the event already carries. internal/session does not compute
// them, and must not: "+3 −1" is a sentence about a screen, not a fact about a
// tool call, and a session that shipped presentation would have to ship a
// second one the day this column changes.
//
// The schemas are pi's, from internal/exec/bare/tools.go, and stable:
//
//	read   {path, offset?, limit?}            → the file's text, with an
//	                                            optional "[Showing lines a-b of
//	                                            c…]" footer
//	edit   {path, edits:[{oldText,newText}]}  → "Successfully replaced N
//	                                            block(s) in P."
//	write  {path, content}                    → "Successfully wrote N bytes…"
//	bash   {command, timeout?}                → the output, and on a non-zero
//	                                            exit "\n\nCommand exited with
//	                                            code N"
//	grep   {pattern, path?, glob?, …}         → "path:line: text" rows, or "No
//	                                            matches found"
//	find   {pattern, path?, limit?}           → one path per row, or "No files
//	                                            found matching pattern"
//	ls     {path?, limit?}                    → one entry per row, or "(empty
//	                                            directory)"
//
// Three of them append a bracketed notice block ("\n\n[… limit reached]") that
// is commentary rather than content, so every count here runs over
// [outputBody], which drops it — and drops session's own "… (N more bytes)"
// display cap, whose presence is instead remembered and spelled as a trailing
// "+" on the number. A capped copy can only ever say "at least this many", and
// saying it exactly is cheaper than pretending.

// The caps on an expanded call, per D11's table. They are line counts, not byte
// counts: an expansion is read down a column.
const (
	diffWindow  = 40 // an edit's unified diff
	writeWindow = 20 // a write's content preview
	readWindow  = 30 // a read's returned chunk
	bashWindow  = 30 // a bash call's output
	listWindow  = 30 // grep matches, find/ls listings
	// contextLines is how much unchanged text a diff hunk keeps on each side of
	// a change. Two is enough to place a change in a function and short of
	// quoting the function.
	contextLines = 2
)

// argsOf parses one call's Args into its raw fields. A payload that does not
// parse yields nil, and every accessor below answers "not present" — a
// malformed call still draws a line, it just draws it without a stat.
func argsOf(args string) map[string]json.RawMessage {
	args = strings.TrimSpace(args)
	if args == "" || args[0] != '{' {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &fields); err != nil {
		return nil
	}
	return fields
}

// argString reads one string field. Non-string JSON degrades to its raw text
// rather than to nothing: a path the model sent as a number is still a path a
// person should see.
func argString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return value
}

// targetField names the argument that says what a call is POINTED AT, per
// tool. It is the one thing on a tool line painted in primary ink: the tool's
// name is chrome, the target is the substance.
//
// It mirrors internal/session's glossField because they answer the same
// question about the same schemas — and it is a second copy on purpose: that
// one composes a one-line hint for a log, this one picks the ink column of a
// terminal row, and a shared table would be a shared reason to change both.
var targetField = map[string]string{
	"read":  "path",
	"edit":  "path",
	"write": "path",
	"ls":    "path",
	"find":  "pattern",
	"grep":  "pattern",
	"bash":  "command",
	// THE TWO WEB HANDS, and they are here because their absence was visible.
	// A tool this table does not know falls back to session's hint, which is
	// clipped to eighty BYTES for a log column (its loop.go's hintLimit) — so a
	// `web_fetch` on a two-hundred column frame drew a URL cut at seventy cells
	// with a hundred and thirty cells of blank after it. The arguments have the
	// whole URL, and the row has the room for it.
	"web_fetch":  "url",
	"web_search": "query",
}

// toolTarget is the file, command or pattern a call is about.
//
// The payload is asked FIRST and the hint only after: session's hint is a
// clipped one-liner built for a log column, and the arguments are the thing
// that actually arrived. A call whose target cannot be found either way draws
// its name alone, which is honest.
func toolTarget(tool, args, hint string) string {
	if field, known := targetField[tool]; known {
		if value := argString(argsOf(args), field); value != "" {
			return strings.TrimSpace(firstLine(value))
		}
	}
	_, gloss := toolWords(tool, hint)
	return gloss
}

// toolStat is the dim figure trailing a tool line: the one number that says
// how big the call was. It returns the plain text (for width) and the painted
// text (for the screen), because an edit's stat is two colours and a width
// measured through escape sequences is a width measured wrong.
//
// A stat is only ever drawn for a FINISHED call. An unfinished one has a mark
// of its own — the queue's circle, the question, the spinner — and a number
// that changes under any of them is a number nobody can read. An edit's +N −M
// is the one that is knowable early, and it is deliberately withheld: it is
// the shape the PREVIEW collapses into when the change actually lands
// (toolview.go), and a stat that appeared before the diff did would make the
// preview a repetition instead of an answer.
func (a *app) toolStat(e *entry) (plain, painted string) {
	if e.status.live() {
		return "", ""
	}
	switch e.tool {
	case "edit":
		adds, dels, floor := editStat(e.detail.Args)
		if adds == 0 && dels == 0 {
			return "", ""
		}
		// A payload session had to shorten to fit its display cap makes both
		// figures counts of THE PART OF THE CHANGE THAT ARRIVED, so they are
		// spelled the way every other capped count on this surface is: with a
		// trailing "+", meaning at least this many ([countStat]).
		add, del := glyphAdd+itoa(adds)+floor, glyphDel+itoa(dels)+floor
		return add + " " + del, a.pal.add(add) + " " + a.pal.del(del)

	case "write":
		content, capped := argBody(argString(argsOf(e.detail.Args), "content"))
		if content == "" {
			return "", ""
		}
		more := ""
		if capped {
			more = "+"
		}
		return a.dimStat(glyphAdd + itoa(lineCount(content)) + more + " lines")

	case "read":
		return a.dimStat(countStat(readLines(e.detail.Output), "line"))

	case "bash":
		// Only a failure speaks. A command that exited zero has already said
		// everything it has to say by not saying anything.
		if code, failed := bashExit(e.detail.Output); failed {
			return a.dimStat("exit " + itoa(code))
		}
		return "", ""

	case "grep":
		return a.dimStat(countStat(grepMatches(e.detail.Output), "match"))

	case "find", "ls":
		return a.dimStat(countStat(listEntries(e.detail.Output), "entr"))

	case "web_fetch":
		// HOW MUCH PAGE CAME BACK. A fetch has no lines and no matches worth
		// counting — the markup is stripped and what is left is prose — so the
		// one honest figure is its size, in [byteWord]'s own spelling so the row
		// that watched it arrive and the row that reports it agree.
		//
		// It is the WHOLE page and not the display copy: session cuts its copy
		// and says by how much in the marker it leaves behind, so the two halves
		// add back up ([fetchedBytes]). Nothing at all is drawn for a page that
		// arrived empty, which is the emptiness law.
		return a.dimStat(byteWord(fetchedBytes(e.detail.Output)))
	}
	return "", ""
}

// fetchedBytes is how large a fetched page was, in bytes: what arrived, plus
// whatever session said it had cut off the end of the display copy.
//
// The marker is `… (12345 more bytes)` and it is a sentence about the payload
// rather than part of it, so it is stripped from the body before the body is
// measured and its figure added back afterwards. A cut nobody can parse simply
// contributes nothing, which makes the answer a floor rather than a fiction.
func fetchedBytes(output string) int {
	body, capped := outputBody(output)
	n := len(body)
	if !capped {
		return n
	}
	if at := strings.LastIndex(output, capMarker); at >= 0 {
		rest := strings.TrimSuffix(strings.TrimRight(output[at+len(capMarker):], "\n"), capEnd)
		if more, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil && more > 0 {
			return n + more
		}
	}
	return n
}

// dimStat paints one plain stat and hands back both forms.
func (a *app) dimStat(text string) (string, string) {
	if text == "" {
		return "", ""
	}
	return text, a.pal.dim(text)
}

// count is a number that may be a floor: capped is set when the display copy
// of the output was cut, so the figure is "at least this many".
//
// A NEGATIVE n means the payload said nothing at all — no result arrived on
// this event — and draws no stat. It is the one case that must not collapse
// into zero: "· 0 matches" is a claim about a search, and a search whose result
// nobody sent has not made one.
type count struct {
	n      int
	capped bool
}

// unknown is the count of a result that never arrived.
var unknown = count{n: -1}

// countStat spells one count. "entr" pluralizes to "entries", which is why the
// noun arrives as a stem.
//
// IT CARRIES NO SEPARATOR OF ITS OWN. It used to open with "· ", from the days
// when a count sat against the target and needed something between it and a
// path; the figures now live in the row's right column, which joins what it
// holds with the surface's own dot ([app.joinTail]) — and a count that brought a
// second one drew "· 189 lines · 0.4s", a list that starts with a separator.
func countStat(c count, noun string) string {
	if c.n < 0 {
		return ""
	}
	word := noun
	switch {
	case noun == "entr":
		word = "entries"
		if c.n == 1 {
			word = "entry"
		}
	case c.n != 1:
		word = noun + "es"
		if noun == "line" {
			word = noun + "s"
		}
	}
	more := ""
	if c.capped {
		more = "+"
	}
	return itoa(c.n) + more + " " + word
}

// ── the payload readers ─────────────────────────────────────────────────────

// capMarker is what internal/session appends when it cuts its display copy of a
// result. Everything after it is a byte count, not output.
const capMarker = "… ("

// noticeMarker opens the bracketed commentary bare's list tools append to a
// truncated listing. It is a notice about the result, not part of it.
const noticeMarker = "\n\n["

// capEnd and noticeEnd are how each of those two trailers finishes. Both
// markers open something that runs to the end of the result, so the last few
// bytes are what says whether the result has one at all.
const (
	capEnd    = "more bytes)"
	noticeEnd = "]"
)

// outputBody is a tool result with session's display cap and bare's trailing
// notice block removed, and whether the cap was hit. Every count and every
// expansion in this file runs over it: they are about the RESULT, and a
// sentence explaining that the result was shortened is not part of the result.
//
// THE SUFFIX IS TESTED FIRST, and that ordering is the whole performance of
// this function. Both trailers end the result, so a result without the ending
// cannot have the marker either — and the ending is eleven bytes at a known
// offset while the marker search is a backward scan of the entire output. This
// is called for every tool row on every frame, at thirty frames a second, over
// results that run to hundreds of kilobytes; asking the cheap question first
// turned it from 29% of the frame into nothing. Both tests are pure, so the
// answer is the same either way round.
func outputBody(output string) (string, bool) {
	text := strings.TrimRight(output, "\n")
	capped := false
	if strings.HasSuffix(text, capEnd) {
		if at := strings.LastIndex(text, capMarker); at >= 0 {
			text, capped = text[:at], true
		}
	}
	if strings.HasSuffix(text, noticeEnd) {
		if at := strings.LastIndex(text, noticeMarker); at >= 0 {
			text = text[:at]
		}
	}
	return strings.TrimRight(text, "\n"), capped
}

// argBody is one ARGUMENT string with session's display cap removed, and
// whether it was there.
//
// It is [outputBody]'s twin for the other half of a tool event, and it exists
// because session now spends the arguments cap INSIDE the oversized string
// values rather than by cutting the JSON (its loop.go): a 40k write's content
// arrives here ending in `… (12345 more bytes)`, which is a sentence about the
// payload rather than a line of the file.
//
// THE MARKER IS STRIPPED FOR COUNTING AND KEPT FOR READING. Every figure derived
// from a shortened field is a floor and says so with a trailing "+", the same
// grammar [countStat] spells a capped result's count in — while the expansion
// itself shows the marker where it sits, because it is the only thing on screen
// that says this is not the whole file.
func argBody(text string) (string, bool) {
	if !strings.HasSuffix(text, capEnd) {
		return text, false
	}
	at := strings.LastIndex(text, capMarker)
	if at < 0 {
		return text, false
	}
	return text[:at], true
}

// showingRe matches bare's read/bash footer, which states the range it handed
// back. When it is there it is authoritative — it counts the lines of a file
// the display copy may only hold a prefix of.
var showingRe = regexp.MustCompile(`\[Showing lines (\d+)-(\d+) of (\d+)`)

// readLines is how many lines a read call returned.
func readLines(output string) count {
	if strings.TrimSpace(output) == "" {
		return unknown
	}
	if m := showingRe.FindStringSubmatch(output); m != nil {
		from, _ := strconv.Atoi(m[1])
		to, _ := strconv.Atoi(m[2])
		if to >= from {
			return count{n: to - from + 1}
		}
	}
	body, capped := outputBody(output)
	if strings.TrimSpace(body) == "" {
		return count{n: 0, capped: capped}
	}
	// Counted rather than split: the answer is one number, and splitting a
	// hundred-kilobyte read into a string header per line to take len() of it
	// allocated the whole listing on every frame the row was on screen.
	return count{n: strings.Count(body, "\n") + 1, capped: capped}
}

// matchRe is bare's grep row: "path:line: text". Counting these rather than
// every line is what keeps a context-mode search (which prints blocks) from
// reporting its context as hits.
var matchRe = regexp.MustCompile(`(?m)^[^\n]*:\d+:`)

func grepMatches(output string) count {
	if strings.TrimSpace(output) == "" {
		return unknown
	}
	body, capped := outputBody(output)
	if strings.TrimSpace(body) == "" || strings.HasPrefix(body, "No matches found") {
		return count{n: 0}
	}
	if n := len(matchRe.FindAllString(body, -1)); n > 0 {
		return count{n: n, capped: capped}
	}
	return count{n: nonEmptyLines(body), capped: capped}
}

func listEntries(output string) count {
	if strings.TrimSpace(output) == "" {
		return unknown
	}
	body, capped := outputBody(output)
	switch {
	case strings.TrimSpace(body) == "",
		strings.HasPrefix(body, "No files found"),
		strings.HasPrefix(body, "(empty directory)"):
		return count{n: 0}
	}
	return count{n: nonEmptyLines(body), capped: capped}
}

func nonEmptyLines(text string) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// exitRe matches bare's bash status footer. pi's bash tool appends it — and
// only it — when the command exits non-zero, so its presence IS the failure and
// its absence is a clean exit.
var exitRe = regexp.MustCompile(`Command exited with code (\d+)`)

// bashExit reads a command's exit code out of its result text.
func bashExit(output string) (int, bool) {
	m := exitRe.FindStringSubmatch(output)
	if m == nil {
		return 0, false
	}
	code, err := strconv.Atoi(m[1])
	if err != nil || code == 0 {
		return 0, false
	}
	return code, true
}

// lineCount is how many lines a block of text is. A trailing newline ends the
// last line rather than opening an empty one — a file of three lines written
// with a final newline is three lines, not four.
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// ── the edit payload ────────────────────────────────────────────────────────

// editPair is one targeted replacement out of an edit call.
type editPair struct{ old, new string }

// editPairs pulls the replacements out of an edit call's arguments.
//
// It accepts every spelling bare's prepareEditArguments does, because whatever
// that tool executes is what this line has to describe: the normal edits array,
// an edits array the model sent as a JSON *string*, and the legacy top-level
// pair — under both the oldText/newText names bare uses and the
// old_string/new_string names other belts spell them with.
func editPairs(args string) []editPair {
	fields := argsOf(args)
	if fields == nil {
		return nil
	}
	var pairs []editPair
	if raw, ok := fields["edits"]; ok {
		pairs = append(pairs, decodeEdits(raw)...)
	}
	for _, names := range [][2]string{{"oldText", "newText"}, {"old_string", "new_string"}} {
		old, want := argString(fields, names[0]), argString(fields, names[1])
		if old != "" || want != "" {
			pairs = append(pairs, editPair{old: old, new: want})
		}
	}
	return pairs
}

// decodeEdits reads the edits array, unwrapping one layer of JSON string.
func decodeEdits(raw json.RawMessage) []editPair {
	var list []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
		OldStr  string `json:"old_string"`
		NewStr  string `json:"new_string"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return nil
		}
		if err := json.Unmarshal([]byte(encoded), &list); err != nil {
			return nil
		}
	}
	pairs := make([]editPair, 0, len(list))
	for _, item := range list {
		pairs = append(pairs, editPair{
			old: firstNonEmpty(item.OldText, item.OldStr),
			new: firstNonEmpty(item.NewText, item.NewStr),
		})
	}
	return pairs
}

// editStat is the +N −M of one edit call, summed over its replacements, and the
// suffix those numbers wear — "+" when any replacement block reached session's
// display cap and "" when none did.
//
// A block that was shortened is diffed as far as it arrived, which is all any
// reader of this event can do, and the suffix is how the row admits it. The
// markers themselves come off first: `… (5000 more bytes)` on the end of a
// replacement is a sentence about the payload, and a diff that counted it would
// report one changed line that nobody wrote.
func editStat(args string) (adds, dels int, floor string) {
	capped := false
	for _, pair := range editPairs(args) {
		old, oldCut := argBody(pair.old)
		want, newCut := argBody(pair.new)
		capped = capped || oldCut || newCut
		a, d := diffStat(splitLines(old), splitLines(want))
		adds, dels = adds+a, dels+d
	}
	if capped {
		floor = "+"
	}
	return adds, dels, floor
}

// splitLines breaks a replacement block into lines, treating a trailing newline
// as the end of the last line rather than the start of an empty one — the same
// rule [lineCount] counts by, so a stat and a diff never disagree.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// ── the diff ────────────────────────────────────────────────────────────────

// diffOp is one row of an edit script: kept, added or removed.
type diffOp struct {
	kind byte // ' ', '+', '-'
	text string
}

// diffCeiling is the largest pair of blocks the exact algorithm is run on. The
// table below is O(n×m) cells, so 600×600 is 360k ints — a fraction of a
// millisecond, and about the largest replacement a model has ever sent. Past
// it the diff degrades to "all of that became all of this", which is both true
// and what a person reading a 600-line replacement wanted anyway.
const diffCeiling = 600

// diffOps is the edit script from old to new, by longest common subsequence.
//
// LCS rather than a heuristic because the STAT is the point: "+3 −1" has to be
// the number of lines that actually changed, and a diff that reports a moved
// line as a delete plus an add reports a number the person can see is wrong.
// The blocks are one replacement each — tens of lines, not a repository — so
// the quadratic table is the cheap option here and Myers' would be arithmetic
// for its own sake.
func diffOps(old, new []string) []diffOp {
	if len(old) > diffCeiling || len(new) > diffCeiling {
		ops := make([]diffOp, 0, len(old)+len(new))
		for _, line := range old {
			ops = append(ops, diffOp{kind: '-', text: line})
		}
		for _, line := range new {
			ops = append(ops, diffOp{kind: '+', text: line})
		}
		return ops
	}

	// lcs[i][j] is the length of the longest common subsequence of old[i:] and
	// new[j:]. Filling from the end lets the walk below run forwards, which is
	// the order the rows are emitted in.
	lcs := make([][]int, len(old)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(new)+1)
	}
	for i := len(old) - 1; i >= 0; i-- {
		for j := len(new) - 1; j >= 0; j-- {
			if old[i] == new[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
				continue
			}
			lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
		}
	}

	ops := make([]diffOp, 0, len(old)+len(new))
	i, j := 0, 0
	for i < len(old) && j < len(new) {
		switch {
		case old[i] == new[j]:
			ops = append(ops, diffOp{kind: ' ', text: old[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{kind: '-', text: old[i]})
			i++
		default:
			ops = append(ops, diffOp{kind: '+', text: new[j]})
			j++
		}
	}
	for ; i < len(old); i++ {
		ops = append(ops, diffOp{kind: '-', text: old[i]})
	}
	for ; j < len(new); j++ {
		ops = append(ops, diffOp{kind: '+', text: new[j]})
	}
	return ops
}

// diffStat counts the script's changed rows.
func diffStat(old, new []string) (adds, dels int) {
	for _, op := range diffOps(old, new) {
		switch op.kind {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	return adds, dels
}

// hunk is a run of the script worth showing: the changes, plus [contextLines]
// of unchanged text on each side, with the line numbers each side starts at.
type hunk struct {
	oldStart, oldCount int
	newStart, newCount int
	ops                []diffOp
}

// hunks groups an edit script the way a unified diff does: every changed row,
// each padded with context, and runs that overlap merged into one. Line numbers
// are relative to the replacement BLOCK rather than to the file, because the
// block is all the payload carries — an edit call sends the text it is
// replacing, never where in the file it sits.
func hunks(ops []diffOp) []hunk {
	changed := make([]int, 0, len(ops))
	for i, op := range ops {
		if op.kind != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	// Walk the line numbers once so every row knows where it sits on both
	// sides; the hunk headers are read off this.
	oldAt, newAt := make([]int, len(ops)), make([]int, len(ops))
	o, n := 1, 1
	for i, op := range ops {
		oldAt[i], newAt[i] = o, n
		switch op.kind {
		case ' ':
			o, n = o+1, n+1
		case '-':
			o++
		case '+':
			n++
		}
	}

	var out []hunk
	for at := 0; at < len(changed); {
		from := max(changed[at]-contextLines, 0)
		to := changed[at] + contextLines
		for at+1 < len(changed) && changed[at+1]-contextLines <= to+1 {
			at++
			to = changed[at] + contextLines
		}
		at++
		if to > len(ops)-1 {
			to = len(ops) - 1
		}
		h := hunk{oldStart: oldAt[from], newStart: newAt[from], ops: ops[from : to+1]}
		for _, op := range h.ops {
			if op.kind != '+' {
				h.oldCount++
			}
			if op.kind != '-' {
				h.newCount++
			}
		}
		out = append(out, h)
	}
	return out
}

// header is the hunk's @@ line, in the spelling every diff reader knows.
func (h hunk) header() string {
	return "@@ -" + itoa(h.oldStart) + "," + itoa(h.oldCount) +
		" +" + itoa(h.newStart) + "," + itoa(h.newCount) + " @@"
}
