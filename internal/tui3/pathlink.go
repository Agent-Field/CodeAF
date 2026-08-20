package tui3

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// EVERY PATH A PERSON READS ON THIS SURFACE IS A DOOR, AND THE DOOR HAS TO BE
// THERE.
//
// A reply that says "I wrote it to internal/tui3/pathlink.go" is naming a file
// the reader almost always wants to open next, and until this file existed the
// name was dead text. A terminal will sometimes guess — cmd-click on a word
// that looks pathish — but the guess is made on ONE VISUAL LINE, so the moment
// the pane is narrow enough to wrap a long path the click grabs half of it and
// opens nothing. That is the defect: not that paths were unclickable, but that
// they were unreliably clickable, which is worse, because a person stops
// trying.
//
// So the surface writes the link itself, in OSC 8. An OSC 8 anchor carries the
// URI OUT OF BAND: the terminal is told the target once and the anchor text may
// then be any shape at all, including a path broken across three rows, and a
// click anywhere inside it opens the same file. The sequence occupies no cells,
// so nothing that measures a row has to know about it.
//
// ── THE HONESTY RULE: WE STAT IT ────────────────────────────────────────────
//
// A link that opens nothing is worse than no link — it is the surface claiming
// a door it does not have (the same law [linkifyTasks] is written under, where
// an unknown task id stays plain text). So NOTHING BECOMES A LINK UNTIL IT HAS
// BEEN FOUND ON DISK. The shape test ([looksPath]) is only the cheap reject
// that decides what is worth a stat; the stat is what decides.
//
// This is what keeps the pass out of trouble in the places a looser rule falls
// over. A unified diff's `a/internal/x.go` and `b/internal/x.go` are not files
// and there are no `a/` and `b/` directories to make them ones. `1/2`, `4:3`
// and `v2.0` are not files. An import path is not a file. None of them is
// special-cased anywhere in here; all of them simply fail to exist.
//
// ── WHAT IS NOT LINKED, AND WHY ─────────────────────────────────────────────
//
// A MODEL MAY NOT MAKE A LINK. Everything a model or a tool writes is laundered
// through internal/sanitize on its way in (prose's sanitizeSource), and that
// strips every OSC there is, OSC 8 included — a reply carrying its own
// `\x1b]8;;https://evil.example` gets no hyperlink, and that is deliberate, not
// incidental. This pass runs AFTER the laundering, on rows the surface itself
// produced, so the only URIs on screen are ones aforge resolved and checked.
// The target is always file:// and always a path that exists.
//
// A COMMAND'S OUTPUT IS NOT LINKED. Tool stdout is somebody else's program
// talking — a test runner, a compiler, `ls` — and the surface draws it verbatim
// on purpose. See [linker.rows] for the whole of that decision.
//
// ── ON THE FAR SIDE OF A CONNECTION THERE ARE NO LINKS ───────────────────────
//
// A hosted session's workspace is on ANOTHER MACHINE, and `file:///app/main.go`
// handed to the terminal in front of you means this machine's /app/main.go.
// The two outcomes are a click that does nothing and a click that opens a
// stranger's file, and the second is the reason this is off rather than
// best-effort. It is the same judgement the branch probe makes in [newApp] and
// for the same reason: a fact about the other machine is not a fact this
// surface may guess at.

// sgrUnderOn and sgrUnderOff are the underline attribute, spelled once. SGR 4
// is in the original ECMA-48 set, so every terminal above the one that draws no
// SGR at all understands it — which is why [palette.underline] asks the colour
// profile and nothing else.
const (
	sgrUnderOn  = "\x1b[4m"
	sgrUnderOff = "\x1b[24m"
)

// pathWordMax is the longest word this pass will carry to a stat. It is a bound
// on WORK rather than a judgement about names: a row of a hundred columns of
// base64 is one word, it is not a path, and finding that out should not cost a
// syscall.
const pathWordMax = 512

// pathChainMax is how many rows one split path may be reassembled across.
//
// It is a bound on WORK and not a judgement about names: each extra row is one
// more stat, and eight of them is a path eight panes wide — a generated file,
// several directories deep, in a forty-four column frame. It has to be this
// generous precisely because the narrow frame is the case this whole pass
// exists for; a path that fits on one row never needed a join.
const pathChainMax = 8

// pathSeenMax bounds the memo. A conversation that has named four thousand
// distinct words is a conversation whose oldest answers are far off screen, and
// dropping the lot costs one stat each the next time any of them is drawn.
const pathSeenMax = 4096

// fileURI is the ONE spelling of a file link on this surface.
//
// It is built through net/url rather than by concatenation because a path is
// allowed to contain the characters a URI is not — a space, a '#', a '?' — and
// `"file://" + path` hands the terminal a URI that stops at the first one of
// them. The authority is left empty (`file:///Users/...`), which is the form
// every terminal that implements OSC 8 reads; naming a host is a claim about
// which machine the file is on, and this pass only ever links files on this one.
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// linker is the whole of what making a link needs: what the terminal will take,
// what the palette may draw, and where a name that is not absolute is resolved
// from.
//
// It is a value rather than a method set on [app] so that the rules can be
// tested against a temporary directory without standing up a surface, and so
// that the memo is passed in rather than reached for — a pass that reads app
// state on the render path is a pass whose answer can change between two halves
// of one frame.
type linker struct {
	pal palette
	// on is whether an OSC 8 may be written at all. False renders every anchor
	// as the plain text it was, which is what a caller drew before this existed.
	on bool
	// root is the workspace a relative name is resolved against. Empty means
	// only absolute and ~-prefixed names can resolve.
	root string
	// home expands a leading '~'. Empty leaves the tilde unresolved, and an
	// unresolved tilde is a name that does not exist.
	home string
	// seen memoizes the stat. The value is the resolved absolute path, or the
	// empty string for "looked, and it is not a file" — the negative answer is
	// the one worth keeping, because it is the one almost every word gets.
	seen map[string]string
}

// linker builds this surface's [linker]: the palette it paints with, the
// workspace a relative name means, and the memo the stats are kept in.
//
// It is built per call rather than held, because it is a view of app state and
// not a second copy of it — the map is shared, so what one render learned the
// next one does not have to ask again.
func (a *app) linker() linker {
	// AND A TASK'S ROOM IS ANOTHER WORKSPACE. A node works in its own git
	// worktree, so `internal/tui3/app.go` on a room's page means THAT tree's
	// copy — and the only root this surface holds is its own. Resolving one
	// against the other would land on a file with the same name and different
	// contents, which is the connection's failure mode arrived at from the
	// inside: not a click that does nothing, but a click that opens the wrong
	// file. The paths on a room's page are still shown in full.
	return linker{
		pal:  a.pal,
		on:   a.pathLinks && !a.roomOpen(),
		root: a.workspace,
		home: a.tilde,
		seen: a.pathSeen,
	}
}

// linkPaths is the pass every block of rows a person reads goes through.
func (a *app) linkPaths(rows []string) []string { return a.linker().rows(rows) }

// pathLink draws one path whose SHOWN text is not the path — a name fitted to a
// column, shortened against the home directory, cut down to a basename. The
// whole file opens either way, which is the point: what a row has room for and
// what a click has to reach are two different questions, and OSC 8 is what lets
// them be.
//
// The name is taken as a tool wrote it — relative to the workspace, perhaps
// ~-prefixed, perhaps a file that does not exist yet because the call that
// writes it is still running — and resolved and checked before anything is
// linked. So a write in flight is plain text, and the same row is a door the
// moment the file is there.
func (a *app) pathLink(name, shown string) string {
	l := a.linker()
	if !l.on || shown == "" {
		// Asked here rather than left to [linker.anchor] so that a surface which
		// is not writing links does not spend a stat finding that out.
		return shown
	}
	target, ok := l.resolve(strings.TrimSpace(name))
	if !ok {
		return shown
	}
	return l.anchor(shown, target)
}

// terminalTakesLinks reports whether an OSC 8 may be written to this terminal.
//
// It is a VETO and not a capability test, for the reason [detectASCII] is: there
// is no sequence that asks a terminal whether it does hyperlinks, and the ones
// that do not have — for twenty years — ignored an OSC they did not recognize.
// So the answer is yes unless the terminal made no claim at all, which is the
// one case where the bytes might be printed instead of consumed: TERM unset (a
// pipe, a file, a cron job) or TERM=dumb.
//
// NO_COLOR IS NOT ASKED. It is a request about colour, and a person who wants
// no colour has said nothing about whether they would like to open their files.
// What NO_COLOR does take away is the underline, because that is SGR — see
// [palette.underline].
func terminalTakesLinks(env func(string) string) bool {
	if env == nil {
		return false
	}
	term := strings.ToLower(strings.TrimSpace(env("TERM")))
	return term != "" && term != "dumb"
}

// anchor is one finished link: the label, underlined where the terminal can
// draw an underline, wrapped in an OSC 8 to target.
//
// THE UNDERLINE IS THE AFFORDANCE. Some terminals style their own hyperlinks on
// hover and some draw nothing at all until you are already holding the modifier
// down, which means without this a person cannot see that there is anything to
// click. Underline is what this codebase already says "this is a location" with
// — a path inside a highlighted command wears it ([palette.underline], and
// shellx.go's shellPath) — so a linked path and a path in a command line look
// like the same kind of thing, because they are.
//
// THE LABEL NEED NOT BE THE WHOLE PATH, and a caller may spend the same target
// on more than one label: a path too long for the pane goes down across several
// rows, EACH ROW OPENING ITS OWN ANCHOR ON THE SAME FILE, which is what makes a
// terminal treat them as one link and a click on any of them open it
// (imagepreview.go's [picturePathLine], and [linker.rows]).
//
// It is a plain SGR 4 and not one of the styled underlines (the `4:3` curly, the
// `4:2` double). Those are a Kitty extension carried by roughly the same
// terminals that do OSC 8, but the colon sub-parameter is not universally
// parsed, and a terminal that mis-reads it prints the tail of the sequence as
// text. The affordance is worth an underline; it is not worth a row of garbage
// on somebody's screen.
func (l linker) anchor(label, target string) string {
	if label == "" || target == "" || !l.on {
		return label
	}
	return linkify(l.pal.underline(label), fileURI(target))
}

// path is the whole detection rule for one word: what part of it names a file,
// and where that file is.
//
// The returned range is a range INSIDE word, because prose puts punctuation
// against names — a name in brackets, a name at the end of a sentence, a name
// inside a code span whose backticks this terminal kept — and the anchor has to
// cover the name and not the sentence around it. A trailing
// `:12` or `:12:4` is KEPT inside the anchor and dropped from the target: a
// compiler says where it went wrong that way, the whole token is what a person
// aims at, and the file is what opens.
func (l linker) path(word string) (from, to int, target string, ok bool) {
	if len(word) == 0 || len(word) > pathWordMax {
		return 0, 0, "", false
	}
	from, to = pathTrim(word)
	if to-from < 2 {
		return 0, 0, "", false
	}
	name := word[from:to]
	// A line and column follow the name and are not part of it.
	bare := strings.TrimRight(name, "0123456789")
	for strings.HasSuffix(bare, ":") && len(bare) > 1 {
		name = strings.TrimSuffix(bare, ":")
		bare = strings.TrimRight(name, "0123456789")
	}
	target, ok = l.resolve(name)
	if !ok {
		return 0, 0, "", false
	}
	return from, to, target, true
}

// pathTrim finds the name inside a word by peeling the punctuation prose wraps
// names in. It is deliberately symmetric and deliberately dumb: it does not try
// to match a bracket to its partner, because the word it was handed may be one
// fragment of a path a wrap broke in half, and half a path has no partners.
//
// `*` AND `_` ARE NOT PUNCTUATION HERE. Emphasis markers were consumed by the
// markdown parse long before this pass sees a row, so a `*` or a `_` on a
// rendered row is a character of the name — and peeling them would lose
// `__init__.py` and `_test.go`, which are the exact filenames two whole
// language communities write.
func pathTrim(word string) (from, to int) {
	const lead = "([{<\"'`"
	const tail = ".,;:!?)]}>\"'`"
	from, to = 0, len(word)
	for from < to && strings.IndexByte(lead, word[from]) >= 0 {
		from++
	}
	for to > from && strings.IndexByte(tail, word[to-1]) >= 0 {
		to--
	}
	return from, to
}

// resolve is the stat, memoized. It answers with an absolute path or with no.
func (l linker) resolve(name string) (string, bool) {
	if l.seen != nil {
		if target, asked := l.seen[name]; asked {
			return target, target != ""
		}
	}
	target := l.locate(name)
	if l.seen != nil {
		if len(l.seen) >= pathSeenMax {
			clear(l.seen)
		}
		l.seen[name] = target
	}
	return target, target != ""
}

// locate is [linker.resolve] without the memo — the rule itself, so that the
// rule can be read in one screenful and tested without one.
func (l linker) locate(name string) string {
	if len(name) < 2 || !looksPath(name) {
		return ""
	}
	for i := 0; i < len(name); i++ {
		// A control byte in a word is not a name; it is a row this pass has been
		// handed in a state it did not expect.
		if name[i] < 0x20 || name[i] == 0x7f {
			return ""
		}
	}
	// A URI that is not a file URI names something on the network. It is not
	// ours to open and it must never reach a stat.
	if at := strings.Index(name, "://"); at >= 0 {
		if !strings.EqualFold(name[:at], "file") {
			return ""
		}
		parsed, err := url.Parse(name)
		if err != nil || parsed.Path == "" {
			return ""
		}
		// A file URI naming a host names another machine's disk.
		if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
			return ""
		}
		name = parsed.Path
	}
	switch {
	case name == "~" || strings.HasPrefix(name, "~/"):
		if l.home == "" {
			return ""
		}
		name = filepath.Join(l.home, strings.TrimPrefix(name[1:], "/"))
	case strings.HasPrefix(name, "~"):
		// `~someone/x` is another account's home and this surface does not know
		// where those are. Guessing would be resolving a name against the wrong
		// directory, which is the one thing this pass may not do.
		return ""
	}
	if !filepath.IsAbs(name) {
		if l.root == "" {
			return ""
		}
		joined := filepath.Join(l.root, name)
		// CONTAINMENT, and it is not about security — nothing here executes
		// anything — but about MEANING. A relative name in a reply means "in the
		// workspace"; one that climbs out of it with `../..` has stopped meaning
		// that, and the file it lands on is a coincidence of where aforge happens
		// to have been started.
		if joined != l.root && !strings.HasPrefix(joined, l.root+string(filepath.Separator)) {
			return ""
		}
		name = joined
	}
	name = filepath.Clean(name)
	if _, err := os.Stat(name); err != nil {
		return ""
	}
	return name
}

// ── the pass over rendered rows ─────────────────────────────────────────────

// pathSpan is one anchor to be spliced into a row: a range in the row's PLAIN
// bytes, and the sequence that opens the link over them.
//
// It carries the OPENER rather than the path because [oscSafeURI] gets to
// refuse, and a span whose URI was refused must never be spliced — a link this
// surface opened and could not close would swallow the rest of the screen.
// Refusing at the point the span is made is what keeps the splicer unable to
// make that mistake.
type pathSpan struct {
	from, to int
	open     string
}

// wordSpan is one whitespace-delimited word of a row, in plain bytes.
type wordSpan struct{ from, to int }

// rows turns every path in a block of already-rendered rows into a link, and is
// the only entry point the surface uses for model prose.
//
// ── IT RUNS ON ROWS, AND ON A WHOLE BLOCK OF THEM ────────────────────────────
//
// Rows, because the wrap has already happened and a link's anchor is a fact
// about the characters that ended up on screen — the same reason [linkifyTasks]
// runs where it does. A whole block, because that is the one thing a task
// reference did not need and a path does: prose splits a word longer than the
// pane ACROSS rows, and the fragments are only a path when they are put back
// together. So this looks at the next row, and the one after it, and links every
// fragment of a split path to the same file ([linker.join]).
//
// Re-opening the anchor on each visual line is what makes that work: a terminal
// treats consecutive anchors carrying the same URI as one link, so the whole
// path highlights together and a click on any row of it opens the file. Carrying
// ONE anchor across the row break would have been fewer bytes and a bug — the
// rows are padded, indented and sliced by a viewport after this, and every one
// of those bytes would have landed inside the link.
//
// ── WHAT THIS IS NOT POINTED AT ─────────────────────────────────────────────
//
// TOOL OUTPUT IS NOT PUT THROUGH HERE, and that is a decision rather than an
// omission. A command's stdout is another program's text — a stack trace, a
// test log, `ls -l` — and it is drawn verbatim on purpose. Sweeping it for
// pathish words would mean linking whatever `go test` prints, most of which is
// not a file and some of which is a file only by coincidence, and the reader
// cannot tell which underline is which. What the surface DOES link in a tool
// card is the part it wrote itself: the target of a read, a write or an edit,
// which is a path aforge resolved and not one it found. Chrome, then, and the
// model's prose. Nothing that arrived from a subprocess.
func (l linker) rows(rows []string) []string {
	if !l.on || len(rows) == 0 {
		return rows
	}
	// THE CHEAP REJECT, and it is asked of the WHOLE BLOCK rather than of each
	// row. Every shape [looksPath] says yes to carries a '/', a '~' or a dot, and
	// no escape sequence this surface writes into prose carries one — so a block
	// with none of them anywhere has no path in it and costs one scan instead of
	// one flattened copy per row. It cannot be asked row by row: the middle rows
	// of a split path are the ones with no punctuation left in them at all
	// (`ngPathWrappedAcrossRows`), and skipping those is skipping the join.
	if !hasPathByte(rows) {
		return rows
	}
	flats := make([]string, len(rows))
	words := make([][]wordSpan, len(rows))
	any := false
	for i, text := range rows {
		flats[i], _ = flatten(text)
		words[i] = splitWords(flats[i])
		if len(words[i]) > 0 {
			any = true
		}
	}
	if !any {
		return rows
	}
	spans := make([][]pathSpan, len(rows))
	taken := make([]int, len(rows)) // how many leading words a join has claimed
	for i := range rows {
		for w, word := range words[i] {
			if w < taken[i] {
				continue
			}
			// A word that runs to the very end of its row may be the head of a
			// path the wrap broke. The join is tried FIRST, because when both it
			// and the head alone name something the join is the longer, more
			// specific answer — and it is the one the reader was looking at.
			if word.to == len(flats[i]) && w == len(words[i])-1 {
				if l.join(flats, words, taken, spans, i, word) {
					continue
				}
			}
			from, to, target, ok := l.path(flats[i][word.from:word.to])
			if !ok {
				continue
			}
			open := linkOpen(fileURI(target))
			if open == "" {
				continue
			}
			spans[i] = append(spans[i], pathSpan{
				from: word.from + from, to: word.from + to, open: open,
			})
		}
	}
	out := make([]string, len(rows))
	for i, text := range rows {
		if len(spans[i]) == 0 {
			out[i] = text
			continue
		}
		out[i] = splicePathLinks(text, spans[i], l.pal)
	}
	return out
}

// join reassembles a path a wrap split across rows and links every fragment of
// it to the same file. It reports whether it found one.
//
// THE LONGEST CHAIN THAT NAMES SOMETHING WINS, and it is worth saying why the
// obvious tie-break is the wrong one. Asking at each step and stopping at the
// first yes sounds cheaper and is a bug, because the prefixes of a path are
// DIRECTORIES and directories exist: a name broken after `…/001/internal`
// resolves, so a first-yes rule would link four rows of a seven-row path to the
// folder two levels above the file. The chain therefore runs to
// [pathChainMax] — the walk is bounded, the stats are memoized, and the last
// answer is kept.
//
// The chain stops early at a continuation that ended before its row did: that
// row's wrap broke on a space, so there was no split, and there is nothing left
// for a longer chain to pick up.
func (l linker) join(flats []string, words [][]wordSpan, taken []int, spans [][]pathSpan, at int, head wordSpan) bool {
	text := flats[at][head.from:head.to]
	rows := []int{at}
	parts := []wordSpan{head}
	var (
		best     int // how many fragments the winning chain used; 0 is none
		bestFrom int
		bestTo   int
		bestOpen string
	)
	for next := at + 1; next < len(flats) && len(rows) < pathChainMax; next++ {
		if len(words[next]) == 0 || taken[next] > 0 {
			break
		}
		tail := words[next][0]
		text += flats[next][tail.from:tail.to]
		rows = append(rows, next)
		parts = append(parts, tail)
		if from, to, target, ok := l.path(text); ok {
			if open := linkOpen(fileURI(target)); open != "" {
				best, bestFrom, bestTo, bestOpen = len(parts), from, to, open
			}
		}
		if tail.to != len(flats[next]) {
			break
		}
	}
	if best == 0 {
		return false
	}
	markJoin(spans, taken, rows[:best], parts[:best], bestFrom, bestTo, bestOpen)
	return true
}

// markJoin lays one joined path back down on the rows it came from: the trim
// [linker.path] found is a range in the CONCATENATION, so it is walked back
// across the fragments a byte at a time and each row gets the part of the anchor
// that landed on it.
func markJoin(spans [][]pathSpan, taken []int, rows []int, parts []wordSpan, from, to int, open string) {
	at := 0 // the offset in the concatenation where this fragment begins
	for n, part := range parts {
		size := part.to - part.from
		start, end := max(from-at, 0), min(to-at, size)
		if start < end {
			spans[rows[n]] = append(spans[rows[n]], pathSpan{
				from: part.from + start, to: part.from + end, open: open,
			})
		}
		at += size
		if n > 0 {
			taken[rows[n]] = 1
		}
	}
}

// hasPathByte reports whether any row of a block carries a byte a path has to
// have. It is the block's one scan before any of it is taken apart.
func hasPathByte(rows []string) bool {
	for _, text := range rows {
		if strings.ContainsAny(text, "/~.") {
			return true
		}
	}
	return false
}

// splitWords finds every whitespace-delimited word of a plain row.
func splitWords(flat string) []wordSpan {
	var out []wordSpan
	for at := 0; at < len(flat); {
		if flat[at] == ' ' || flat[at] == '\t' {
			at++
			continue
		}
		start := at
		for at < len(flat) && flat[at] != ' ' && flat[at] != '\t' {
			at++
		}
		out = append(out, wordSpan{from: start, to: at})
	}
	return out
}

// splicePathLinks writes one painted row back out with an anchor around each
// span. Spans arrive in order and never overlap.
//
// WHAT WAS PAINTED UNDER THE ANCHOR IS KEPT. A path link differs from a task
// link here and on purpose: a task reference is a control and is repainted as
// one ([paintLinks]), while a path is part of the sentence it was written in and
// keeps the sentence's colour — a filename inside a code span stays code
// coloured, one inside a heading stays a heading. All the anchor adds is the
// underline, and it re-opens that after any escape inside the span that closed
// it, because prose is allowed to change style in the middle of a word.
func splicePathLinks(text string, spans []pathSpan, pal palette) string {
	var (
		out    strings.Builder
		fg     string
		under  bool
		at     int  // the PLAIN offset the walk has reached
		next   int  // the span being looked for
		inside bool // the walk is between a span's from and its to
		opened bool // this span turned the underline on and owes a 24
	)
	underlines := pal.profile != tokens.NoColor
	out.Grow(len(text) + 64*len(spans))
	for i := 0; i <= len(text); {
		if inside && at == spans[next].to {
			if opened {
				out.WriteString(sgrUnderOff)
				under = false
			}
			out.WriteString(linkClose())
			inside, opened = false, false
			next++
		}
		if !inside && next < len(spans) && at == spans[next].from {
			out.WriteString(spans[next].open)
			inside = true
			if opened = underlines && !under; opened {
				out.WriteString(sgrUnderOn)
				under = true
			}
		}
		if i == len(text) {
			break
		}
		if n := escLen(text, i); n > 0 {
			seq := text[i : i+n]
			out.WriteString(seq)
			fg, under = sgrInk(seq, fg, under)
			if inside && opened && !under {
				out.WriteString(sgrUnderOn)
				under = true
			}
			i += n
			continue
		}
		out.WriteByte(text[i])
		at++
		i++
	}
	return out.String()
}
