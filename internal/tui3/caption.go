package tui3

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// caption is the title of one discrete step of a turn's work.
//
// A STEP IS NOT THINKING. Thinking is private machinery that may sit under a
// step the way tool rows do; the caption is the checklist item a person reads —
// "listing github issues", "ranking by end-result quality" — the shape ChatGPT's
// o1 outline made familiar. A step is a maximal run of calls, optionally headed
// by the one short narrating line the model wrote before them.
type caption struct {
	text             string
	source           captionSource
	start, head, end int
	calls            int
	began, ended     time.Time
	told             string
}

// captionSource records which rung supplied the words.
type captionSource uint8

const (
	captionMade captionSource = iota
	captionSaid
	captionTold
)

// deriveCaptions names each batch large enough to benefit from a heading.
//
// The hierarchy stamp must run first. That makes the answer impossible to lift:
// only prose already proved to precede more work can become a caption.
func deriveCaptions(es []entry, runningTurn int) []caption {
	var out []caption
	for from := 0; from < len(es); {
		if es[from].kind != entryTool {
			from++
			continue
		}
		to := from + 1
		for to < len(es) && es[to].kind == entryTool && es[to].turn == es[from].turn {
			prev, next := es[to-1], es[to]
			// PARALLEL CALLS SHARE A CAPTION; SEQUENTIAL ONES DO NOT. A call that
			// began at or after its predecessor finished is the next step of the
			// outline (caption 1, then caption 2), even when no prose sits between.
			if !prev.ended.IsZero() && !next.began.IsZero() && !next.began.Before(prev.ended) {
				break
			}
			to++
		}

		// EVERY BATCH GETS A HEADING. Skipping finished singleton calls left the
		// common turn — one read, then one edit — looking exactly like it did
		// before captions existed, and the chip then had no outline to open onto.
		head := -1
		for i := from - 1; i >= 0 && es[i].turn == es[from].turn; i-- {
			if es[i].kind == entryTool || groupBreaks(&es[i]) {
				break
			}
			if es[i].kind == entryAssistant && es[i].demoted {
				// Walking backwards and retaining the match merges consecutive
				// prose heads into the earliest head over this one batch.
				head = i
				continue
			}
			// Reasoning may sit between this step's narration and its calls.
			// Once its narration is found, an intervening block separates it
			// from an older phase; borrowing that older head loses this one.
			if head >= 0 {
				break
			}
		}

		c := caption{start: from, head: head, end: to, calls: to - from}
		if head >= 0 {
			c.start = head
			c.source = captionSaid
			c.text = captionWords(es[head].text)
		}
		// THINKING NEVER SUPPLIES THE TITLE. A first-line of chain-of-thought is
		// reasoning, not a step; without a narrating line we compose a floor from
		// the tools, and the cheap narrator may overwrite it while the batch runs.
		if c.text == "" {
			c.source = captionMade
			c.text = shortCaption(composeCaption(es, from, to))
		}
		live := false
		for i := from; i < to; i++ {
			e := es[i]
			live = live || e.status.live()
			if c.began.IsZero() || (!e.began.IsZero() && e.began.Before(c.began)) {
				c.began = e.began
			}
			if e.ended.After(c.ended) {
				c.ended = e.ended
			}
			if e.caption != "" {
				c.told = e.caption
			}
		}
		if live {
			c.ended = time.Time{}
		} else if c.ended.IsZero() {
			// Replayed and synthetic finished calls may not carry clocks. A
			// non-zero sentinel keeps finished history from claiming to be live.
			c.ended = time.Unix(1, 0)
		}
		if c.source != captionSaid && c.told != "" {
			c.source = captionTold
			c.text = captionWords(c.told)
		}
		out = append(out, c)
		from = to
	}
	return out
}

func captionWords(text string) string {
	line := strings.TrimSpace(firstLine(text))
	return shortCaption(line)
}

// shortCaption keeps ONE short sentence for the step title. Same rules as
// session.cleanCaption: prefer a complete sentence under ten words; never
// ellipsis-cut mid-clause — the row wraps what remains.
const captionWordMax = 10

func shortCaption(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	var pick string
	for _, sentence := range captionSentences(line) {
		words := strings.Fields(strings.TrimSpace(strings.TrimRight(sentence, ".!?;:")))
		if len(words) == 0 {
			continue
		}
		if len(words) < 3 {
			if pick == "" {
				pick = strings.Join(words, " ")
			}
			continue
		}
		if len(words) > captionWordMax {
			words = captionTrimDangling(words[:captionWordMax])
		}
		return strings.Join(words, " ")
	}
	return pick
}

func captionSentences(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var out []string
	start := 0
	for i, r := range line {
		switch r {
		case '.', '!', '?':
			piece := strings.TrimSpace(line[start : i+1])
			if piece != "" {
				out = append(out, piece)
			}
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(line[start:]); rest != "" {
		out = append(out, rest)
	}
	if len(out) == 0 {
		return []string{line}
	}
	return out
}

func captionTrimDangling(words []string) []string {
	dangling := map[string]bool{
		"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
		"to": true, "of": true, "in": true, "on": true, "at": true, "for": true,
		"from": true, "by": true, "with": true, "as": true, "into": true,
		"which": true, "that": true, "this": true, "these": true, "those": true,
		"who": true, "whom": true, "whose": true, "where": true, "when": true,
		"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
		"being": true, "have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "can": true, "could": true,
		"should": true, "may": true, "might": true, "must": true,
		"actually": true, "still": true, "also": true, "just": true, "very": true,
	}
	for len(words) > 2 {
		last := strings.ToLower(strings.Trim(words[len(words)-1], ",;:"))
		if !dangling[last] {
			break
		}
		words = words[:len(words)-1]
	}
	return words
}

// composeCaption is the deterministic floor beneath a model-supplied heading.
//
// IT MUST STILL NAME THE WORK, not the machinery. "running 2 calls" tells the
// reader nothing they cannot already see in the count on the right; a gloss of
// what the tools are pointed at is the floor that earns its place.
func composeCaption(es []entry, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to > len(es) {
		to = len(es)
	}
	if from >= to {
		return ""
	}
	counts := map[string]int{}
	kinds := map[string]bool{}
	var paths []string
	var glosses []string
	allFiles := true
	hasSearch, hasRead, hasEdit, hasTests := false, false, false, false
	for i := from; i < to; i++ {
		if es[i].kind != entryTool {
			continue
		}
		tool := es[i].tool
		verb := captionVerb(tool)
		counts[verb]++
		kinds[verb] = true
		switch tool {
		case "read":
			hasRead = true
		case "grep", "find":
			hasSearch = true
		case "edit", "write":
			hasEdit = true
		case "bash":
			command := strings.ToLower(argString(argsOf(es[i].detail.Args), "command"))
			hasTests = strings.Contains(command, "go test") ||
				strings.Contains(command, "make test") ||
				strings.Contains(command, "pytest") ||
				strings.Contains(command, "npm test")
		}
		if tool == "read" || tool == "edit" || tool == "write" {
			p := argString(argsOf(es[i].detail.Args), "path")
			if p == "" {
				allFiles = false
			} else {
				paths = append(paths, p)
			}
		} else {
			allFiles = false
		}
		if g := toolCaptionGloss(es[i]); g != "" {
			glosses = append(glosses, g)
		}
	}
	n := to - from
	if hasSearch && hasRead && len(kinds) <= 2 {
		return "searching the tree"
	}
	if hasEdit && hasTests {
		return "editing " + strconv.Itoa(fileCallCount(es, from, to)) + " files and running the suite"
	}
	dominant := dominantCaptionVerb(counts)
	if allFiles && len(paths) == n {
		dir := sharedDirectory(paths)
		if dir != "" && dir != "." {
			return dominant + " " + strconv.Itoa(n) + " " + captionPlural(n, "file") + " in " + dir
		}
		if n == 1 {
			return dominant + " " + path.Base(paths[0])
		}
	}
	if len(kinds) == 1 && dominant == "running" {
		if n == 1 && len(glosses) == 1 {
			return glosses[0]
		}
		if theme := sharedBashTheme(es, from, to); theme != "" {
			return theme
		}
	}
	if len(glosses) == 1 {
		return glosses[0]
	}
	if len(kinds) == 1 {
		noun := captionPlural(n, "call")
		if dominant == "reading" || dominant == "editing" {
			noun = captionPlural(n, "file")
		}
		if dominant == "running" {
			// Never "running N calls" — that is the count twice.
			return "running " + strconv.Itoa(n) + " " + captionPlural(n, "command")
		}
		return dominant + " " + strconv.Itoa(n) + " " + noun
	}
	if len(glosses) > 0 {
		return glosses[0]
	}
	if len(kinds) >= 4 {
		return dominant + " and " + strconv.Itoa(n-counts[dominant]) + " more"
	}
	return dominant + " " + strconv.Itoa(n) + " " + captionPlural(n, "thing")
}

// toolCaptionGloss turns one call into a short present-tense line about its
// target. The row underneath still carries the exact command.
func toolCaptionGloss(e entry) string {
	switch e.tool {
	case "bash":
		return bashCaption(argString(argsOf(e.detail.Args), "command"))
	case "web_search":
		if q := argString(argsOf(e.detail.Args), "query"); q != "" {
			return "searching for " + clipRunes(q, 40)
		}
	case "web_fetch":
		if u := argString(argsOf(e.detail.Args), "url"); u != "" {
			return "fetching " + clipRunes(u, 40)
		}
	case "gh", "github":
		return "asking github"
	}
	// Fall back to the session's own hint with the tool name stripped, so
	// "bash gh issue list…" becomes something about the work, not the verb.
	_, gloss := toolWords(e.tool, e.text)
	gloss = strings.TrimSpace(gloss)
	if gloss == "" || gloss == e.tool {
		return ""
	}
	if e.tool == "bash" {
		return bashCaption(gloss)
	}
	return captionWords(gloss)
}

func bashCaption(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "gh issue"):
		return "listing github issues"
	case strings.Contains(lower, "gh pr"):
		return "looking at pull requests"
	case strings.Contains(lower, "gh api"):
		return "asking the github api"
	case strings.Contains(lower, "go test"), strings.Contains(lower, "make test"),
		strings.Contains(lower, "pytest"), strings.Contains(lower, "npm test"):
		return "running the suite"
	case strings.Contains(lower, "go build"), strings.Contains(lower, "make build"):
		return "building"
	case strings.Contains(lower, "git status"):
		return "checking git status"
	case strings.Contains(lower, "git diff"):
		return "reading the diff"
	case strings.Contains(lower, "git log"):
		return "reading the log"
	case strings.HasPrefix(lower, "git "):
		return "working in git"
	}
	// Strip noise prefixes and keep a short readable slice of the command.
	fields := strings.Fields(command)
	for len(fields) > 0 {
		f := strings.ToLower(fields[0])
		if f == "sudo" || f == "env" || strings.Contains(f, "=") {
			fields = fields[1:]
			continue
		}
		break
	}
	if len(fields) == 0 {
		return ""
	}
	line := strings.Join(fields, " ")
	if len(fields) > 4 {
		line = strings.Join(fields[:4], " ")
	}
	return captionWords(line)
}

func sharedBashTheme(es []entry, from, to int) string {
	var theme string
	for i := from; i < to; i++ {
		if es[i].tool != "bash" {
			return ""
		}
		g := bashCaption(argString(argsOf(es[i].detail.Args), "command"))
		if g == "" {
			return ""
		}
		if theme == "" {
			theme = g
			continue
		}
		if theme != g {
			return ""
		}
	}
	return theme
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || s == "" {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

func captionVerb(tool string) string {
	switch tool {
	case "read":
		return "reading"
	case "grep", "find":
		return "searching"
	case "edit", "write":
		return "editing"
	case "bash":
		return "running"
	case "web_fetch", "web_search":
		return "looking up"
	case "ls":
		return "listing"
	default:
		return firstNonEmpty(tool, "working")
	}
}

func dominantCaptionVerb(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	best, most := "working", 0
	for _, key := range keys {
		if counts[key] > most {
			best, most = key, counts[key]
		}
	}
	return best
}

func fileCallCount(es []entry, from, to int) int {
	n := 0
	for i := from; i < to; i++ {
		if es[i].tool == "edit" || es[i].tool == "write" {
			n++
		}
	}
	return n
}

func sharedDirectory(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	dir := path.Dir(strings.TrimSpace(paths[0]))
	for _, name := range paths[1:] {
		if path.Dir(strings.TrimSpace(name)) != dir {
			return ""
		}
	}
	return dir
}

func captionPlural(n int, one string) string {
	if n == 1 {
		return one
	}
	return one + "s"
}

// captionLine is the stable plain spelling used by tests and narrow fallbacks.
func captionLine(c caption, live bool, open bool) string {
	left := shortCaption(captionText(c))
	tail := strconv.Itoa(c.calls) + " " + captionPlural(c.calls, "call")
	if live && c.ended.IsZero() {
		tail = ""
	}
	mark := "▸ "
	if open {
		mark = "▾ "
	}
	if tail == "" {
		return mark + left
	}
	return mark + left + " · " + tail
}

func captionText(c caption) string {
	if c.source != captionSaid && c.told != "" {
		return captionWords(c.told)
	}
	return shortCaption(c.text)
}

// captionRows draws the step title. IT WRAPS; IT NEVER ELLIPSIS-CUTS. A
// person-facing caption is short (5–10 words), and on a narrow frame those
// words still show in full across lines rather than ending in `…`.
func (a *app) captionRows(c caption, live, open bool, width int) []row {
	mark := a.linearMark("▾ ", "v ")
	if !open {
		mark = a.linearMark("▸ ", "> ")
	}
	left := captionText(c)
	tail := strconv.Itoa(c.calls) + " " + captionPlural(c.calls, "call")
	if live && c.ended.IsZero() {
		tail = ""
		if !c.began.IsZero() {
			tail = countUpWord(a.now().Sub(c.began))
		}
	}
	room := width - workIndentCols(width)
	markW := ansi.StringWidth(mark)
	bodyW := room - markW
	if bodyW < 8 {
		bodyW = 8
	}
	wrapW := bodyW
	tailOnFirst := false
	if tail != "" {
		need := 1 + ansi.StringWidth(tail)
		if bodyW-need >= 12 {
			wrapW = bodyW - need
			tailOnFirst = true
		}
	}
	lines := wrap(left, wrapW)
	if len(lines) == 0 {
		lines = []string{""}
	}
	out := make([]row, 0, len(lines))
	for i, line := range lines {
		lead := mark
		if i > 0 {
			lead = strings.Repeat(" ", markW)
		}
		painted := a.pal.narr(line)
		if live && !open && i == 0 {
			painted = a.shimmer(line)
		}
		text := a.pal.dim(lead) + painted
		showTail := tail != "" && ((tailOnFirst && i == 0) || (!tailOnFirst && i == len(lines)-1))
		if showTail {
			used := ansi.StringWidth(lead) + ansi.StringWidth(line)
			pad := room - used - ansi.StringWidth(tail)
			if pad < 1 {
				pad = 1
			}
			text += strings.Repeat(" ", pad) + a.pal.dim(tail)
		}
		out = append(out, row{text: text, entry: c.start, hit: hitCaption, turn: c.start})
	}
	return out
}

// captionRow is the single-row form tests still call; live drawing uses
// [captionRows] so a narrow frame wraps instead of clipping.
func (a *app) captionRow(c caption, live, open bool, width int) row {
	rows := a.captionRows(c, live, open, width)
	if len(rows) == 0 {
		return row{entry: c.start, hit: hitCaption, turn: c.start}
	}
	return rows[0]
}

func captionAt(captions []caption, at int) (caption, bool) {
	for _, c := range captions {
		if at >= c.start && at < c.end {
			return c, true
		}
	}
	return caption{}, false
}

func captionFrontier(c caption, captions []caption, es []entry, turn int) bool {
	for _, later := range captions {
		if later.start > c.start && later.start < len(es) && es[later.start].turn == turn {
			return false
		}
	}
	return true
}

func captionTools(c caption, es []entry) (from, to int) {
	from, to = c.start, c.end
	for from < to && es[from].kind != entryTool {
		from++
	}
	return from, to
}
