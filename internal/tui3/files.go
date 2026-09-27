package tui3

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// THE @ COMPLETION: type @ and two characters, and the files under this
// directory are offered.
//
// It is the same overlay grammar as the command list and the model picker
// (palette.go), opened by typing rather than by a key, and it inserts TEXT and
// nothing else. THE SURFACE DOES NOT READ THE FILE: what is submitted is
// "@internal/session/agent.go" exactly as it stands in the sentence, and the
// agent's read tool resolves it if it wants to. Inlining the contents here
// would be the surface deciding, on a person's behalf, to spend a hundred
// thousand tokens they never asked to send — and it would do it silently, in a
// box that shows one line of what it sent.
//
// A PICTURE IS THE ONE EXCEPTION, and it is not an exception to that rule but a
// consequence of it: no tool on the belt turns a PNG into anything a model can
// look at, so the path in the sentence would be a path nobody can resolve. An
// image row is therefore tagged "img" and choosing it ATTACHES the file instead
// of typing it — see attach.go, which owns everything that happens after.
//
// The same list answers a command's path argument ("/image shot.png", "/export
// notes.md"), opened by tab rather than by "@" and committing the path as text,
// because there the path IS what the line says.

// THE @ ALSO OFFERS TASKS, AND THEY SIT ABOVE THE FILES. See taskmention.go
// for the whole of that half — the index it reads, the sections it draws, and
// the pointer block a chosen task becomes at submit. What is in THIS file is
// only what the two halves share: one list, one cursor, one scroll.
//
// The reason they share a list rather than opening a second overlay is the
// reason this surface has one overlay grammar at all (palette.go): "@" means
// "something I want to point at", and a person reaching for it does not yet
// know whether the thing they are reaching for is a file or a piece of work
// somebody did last week. Two lists would make them decide before they can see
// either.

// The completion's four bounds. The walk is capped so that an @ typed inside a
// home directory cannot become a filesystem crawl; the list shows a screenful,
// and a screenful with tasks on it is allowed to be taller because the tasks
// come with section rules that are not themselves choices; the list opens on
// the bare @ and the query narrows it from there.
const (
	walkCap      = 10000
	completeRows = 8
	completeTall = 14
	// completeMin is zero: the @ opens its list the moment it lands, the way /
	// opens the command list — tasks sit on top, so an empty query is a
	// screenful of the project's work, not a crawl. atToken's token-start gate
	// keeps an address's @ from opening anything.
	completeMin = 0
)

// skipDirs are the directories the walk never enters. It is .gitignore's
// SPIRIT and not its parser: the three names below are what actually fill a
// completion list with rows nobody wants, and a dot-directory is skipped for
// the same reason a file browser hides it. A real .gitignore reader belongs in
// a package of its own the day something else needs one too.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"node_modules": true,
}

// completion is the file picker's whole state. The zero value is closed, and it
// keeps its walk across closes: the second @ of a session opens instantly.
type completion struct {
	open bool
	// at is the rune index of the '@' in the draft, and query what follows it.
	at    int
	query string
	// arg says this completion was opened over a command's PATH ARGUMENT
	// ([argPrefixes]) rather than over an @token. Two things follow from it: the
	// chosen path replaces the argument WHOLE, with no '@' kept in front of it,
	// and an image is written into the line like any other file, because in
	// "/image shot.png" the path is what the command takes.
	arg bool
	// done is the query this completion just INSERTED at at. It is what keeps
	// the list from reopening on top of its own answer: the caret ends up
	// inside a perfectly good @token, and a list that reappeared over it would
	// make enter a key that never finishes.
	done string

	all     []string
	loaded  bool
	loading bool

	hits  []int
	score []int

	// tasks is the project's task index as this surface last read it, newest
	// first, and the two flags beside it are the file walk's own (taskmention.go
	// loads it the same way and for the same reason: an overlay that appeared a
	// second after it was asked for reads as a glitch).
	tasks       []session.TaskIndexEntry
	tasksLoaded bool
	tasksHeld   bool
	// tasksStale says a node changed state while a read was already in flight,
	// so the answer on its way back is one event out of date. It is what stops
	// that answer from marking the snapshot fresh (taskmention.go).
	tasksStale bool
	// taskHits are the tasks this query matched, ranked, capped, and already in
	// section order; taskSection is which section each of them is in, aligned
	// with it (taskmention.go).
	taskHits    []session.TaskIndexEntry
	taskSection []int
	// older is how many matching tasks fell outside the two named sections —
	// the count the "older" rule carries when the cap left no room to draw them.
	older int

	// teams and chats are the catalogs this list ranks, copied from memory on
	// the update loop (mention.go). recents is the recent-conversation snapshot
	// loaded once, off the loop, because that list can touch the disk.
	teams   []mentionTeam
	chats   []mentionChat
	recents []mentionChat
	// teamHits and chatHits are what this query kept, already capped.
	teamHits []mentionTeam
	chatHits []mentionChat
	// scope is which section a prefix narrowed to: "team", "chat", "file", or
	// "" for every section at once.
	scope string
	// recentsHeld says a read of the recent list is already in flight.
	recentsHeld bool

	// lines is what the overlay DRAWS, section rules included, and sel is the
	// line each selectable row sits on, in cursor order. The split is what lets
	// a list with headings in it keep one cursor that cannot land on a heading:
	// cursor indexes sel, everything geometric indexes lines.
	lines []compLine
	sel   []int

	cursor int
	top    int
}

// compLine is one drawn row: a section rule, the prefix words, a team, a
// conversation, a task, or a file. Exactly one of those is set. The index
// fields are -1 when they are not the row.
type compLine struct {
	header  string
	filters bool
	task    int
	file    int
	team    int
	chat    int
}

// deadLine is a row that is not a team, a chat, a task or a file.
func deadLine() compLine {
	return compLine{task: -1, file: -1, team: -1, chat: -1}
}

// sync opens, narrows or closes the completion from the draft and the caret. It
// is called after every edit; there is no key for it, because the key is "@".
//
// A completion opened over a command's argument follows the same call — the
// draft is still what it is about — but it answers to [argToken] instead, and
// it stays open while the person types a path with spaces in it.
func (c *completion) sync(e *editor) {
	if at, query, ok := argToken(e.value, e.cursor); ok {
		switch {
		case query == "" && !(c.open && c.arg):
			// Nothing typed after the command yet, and nobody asked: tab is what
			// opens a list over an empty argument.
			c.open = false
		case c.arg && query == c.done:
			// This list just INSERTED that path (the @ completion's own rule, and
			// here it is what keeps enter from re-completing what enter completed
			// instead of running the command).
			c.open = false
		default:
			c.narrow(at, query, true)
		}
		return
	}
	if c.arg {
		// The line stopped being a command with a path in it: the argument list
		// has nothing left to complete, and what follows is an ordinary draft.
		c.open, c.arg = false, false
	}
	at, query, ok := atToken(e.value, e.cursor)
	if !ok || len([]rune(query)) < completeMin {
		c.open = false
		return
	}
	// done == "" is the zero value, and it is also a team insertion, which
	// takes the "@" out. Matching it here closed a list on the first "@" of
	// a draft, because that token sits at rune 0 with an empty query and the
	// zero at is 0 too.
	if c.done != "" && at == c.at && query == c.done {
		c.open = false
		return
	}
	c.narrow(at, query, false)
}

// narrow is the half sync and [completion.openArg] share: point the list at a
// token, re-rank it, and keep the cursor only if it is still walking the same
// list.
func (c *completion) narrow(at int, query string, arg bool) {
	was := c.open && c.at == at && c.arg == arg
	c.open, c.arg, c.at, c.query = true, arg, at, query
	c.rank()
	if !was {
		c.cursor, c.top = 0, 0
	}
}

// openArg is tab: open the list over a command's path argument even when
// nothing has been typed after the command yet. It reports whether there was an
// argument to open it over.
func (c *completion) openArg(e *editor) bool {
	at, query, ok := argToken(e.value, e.cursor)
	if !ok {
		return false
	}
	c.narrow(at, query, true)
	return true
}

func (c *completion) close() { c.open = false }

// argPrefixes are the commands that take a PATH, spelled as they are typed.
//
// There was one of them, and the line here said that one is not a list. There
// are two now — /image, which attaches the file it is given (attach.go), and
// /export, which writes the conversation to it (export.go) — so it IS a list,
// and the token below is found by asking each prefix rather than by measuring
// the only one there was. Every command written here gets the completion; a
// command that takes a path and is not written here gets nothing, silently,
// which is the one failure worth watching for.
var argPrefixes = []string{"/export ", "/attach "}

// argToken finds the path argument the caret is standing in: everything after
// the command's prefix up to the caret. A path may hold spaces, so the token
// runs to the caret rather than back to the last one.
func argToken(value []rune, cursor int) (int, string, bool) {
	for _, prefix := range argPrefixes {
		at := len([]rune(prefix))
		if cursor < at || len(value) < at {
			continue
		}
		if !strings.EqualFold(string(value[:at]), prefix) {
			continue
		}
		if strings.ContainsRune(string(value[at:cursor]), '\n') {
			return 0, "", false
		}
		return at, string(value[at:cursor]), true
	}
	return 0, "", false
}

// atToken finds the @-word the caret is standing in: the run back to a space,
// a newline or the start of the draft, which must begin with '@'. An @ in the
// middle of a word (an email address, a Go doc link) is not one — the run has
// to start with it.
func atToken(value []rune, cursor int) (int, string, bool) {
	start := cursor
	for start > 0 && value[start-1] != ' ' && value[start-1] != '\n' {
		start--
	}
	if start >= cursor || value[start] != '@' {
		return 0, "", false
	}
	return start, string(value[start+1 : cursor]), true
}

// rank scores every path, team, conversation and task against the query, keeps
// what matched, and lays them out as one list.
func (c *completion) rank() {
	if cap(c.score) < len(c.all) {
		c.score = make([]int, len(c.all))
	}
	scope, needle := mentionScope(c.query)
	c.scope = scope
	needle = strings.ToLower(needle)
	c.hits = c.hits[:0]
	// A prefix that is not "file" hides the paths. The argument list is only
	// ever paths, so it never takes a prefix.
	if c.arg || c.scope == "" || c.scope == scopeFile {
		for i, path := range c.all {
			score, ok := pathScore(path, needle)
			if !ok {
				continue
			}
			c.score[i] = score
			c.hits = append(c.hits, i)
		}
		sort.SliceStable(c.hits, func(a, b int) bool { return c.score[c.hits[a]] < c.score[c.hits[b]] })
		if len(c.hits) > completeRows*4 {
			c.hits = c.hits[:completeRows*4]
		}
	}
	c.rankMentions(needle)
	if c.arg || c.scope != "" {
		c.taskHits, c.taskSection, c.older = c.taskHits[:0], c.taskSection[:0], 0
	} else {
		c.rankTasks()
	}
	c.layout()
	c.cursor = moveCursor(c.cursor, 0, len(c.sel))
	c.follow(c.rowsWanted())
}

// layout turns the two ranked lists into the rows the overlay draws: the task
// sections, then the files under a rule of their own.
//
// The FILE RULE is drawn only when there are tasks above it. On a list that is
// only files — every list this surface had before task mentions — a heading over
// the one thing on screen would be a label saying what a person can already see.
func (c *completion) layout() {
	c.lines, c.sel = c.lines[:0], c.sel[:0]
	if !c.arg {
		line := deadLine()
		line.filters = true
		c.lines = append(c.lines, line)
	}
	c.lines = c.layoutMentions(c.lines)
	above := len(c.teamHits) > 0 || len(c.chatHits) > 0 || len(c.taskHits) > 0
	if len(c.taskHits) > 0 {
		c.lines = c.layoutTasks(c.lines)
	}
	if len(c.hits) > 0 && above {
		line := deadLine()
		line.header = compFilesRule
		c.lines = append(c.lines, line)
	}
	for _, at := range c.hits {
		line := deadLine()
		line.file = at
		c.lines = append(c.lines, line)
	}
	selectable := len(c.teamHits) > 0 || len(c.chatHits) > 0 || len(c.taskHits) > 0 || len(c.hits) > 0
	if !c.arg && !selectable && c.loaded {
		line := deadLine()
		line.header = c.emptyWord()
		c.lines = append(c.lines, line)
	}
	for at, line := range c.lines {
		if line.header == "" && !line.filters {
			c.sel = append(c.sel, at)
		}
	}
}

// rowsWanted is how many rows this list would take if the frame let it: a
// screenful, or the taller screenful a sectioned list needs to show both halves
// of itself. [app.overlayHeight] is what actually decides, and it clamps.
func (c *completion) rowsWanted() int {
	if len(c.taskHits) > 0 || len(c.teamHits) > 0 || len(c.chatHits) > 0 {
		return completeTall
	}
	return completeRows
}

// The three tiers of a match, in the order a person means them. They are spaced
// far apart so that no offset inside a tier can reach the tier below it.
const (
	tierPrefix      = 0
	tierSubstring   = 1 << 20
	tierSubsequence = 1 << 21
)

// pathScore ranks one path against a lowercased query: a PREFIX beats a
// SUBSTRING beats a SUBSEQUENCE, and inside a tier the earlier match wins, then
// the shorter path.
//
// Both the whole path and the base name are tried at each tier, because a
// person typing "@app.go" means the file and a person typing "@internal/tui3"
// means the directory, and one query cannot be told which it was. The base name
// ranks a hair below the path so that a query which is genuinely a path prefix
// leads.
func pathScore(path, needle string) (int, bool) {
	if needle == "" {
		return len(path), true
	}
	lower := strings.ToLower(path)
	base := lower
	if cut := strings.LastIndexByte(lower, '/'); cut >= 0 {
		base = lower[cut+1:]
	}
	switch {
	case strings.HasPrefix(lower, needle):
		return tierPrefix + len(path), true
	case strings.HasPrefix(base, needle):
		return tierPrefix + 1<<10 + len(path), true
	}
	if at := strings.Index(lower, needle); at >= 0 {
		return tierSubstring + at<<8 + len(path), true
	}
	if span, ok := subsequence(lower, needle); ok {
		return tierSubsequence + span<<8 + len(path), true
	}
	return 0, false
}

// subsequence reports whether needle's runes appear in order in text, and how
// far apart the first and last of them landed — the span, which is what tells a
// tight match from a coincidence.
func subsequence(text, needle string) (int, bool) {
	first, last, at := -1, -1, 0
	runes := []rune(needle)
	for i, r := range text {
		if at >= len(runes) {
			break
		}
		if r == runes[at] {
			if first < 0 {
				first = i
			}
			last = i
			at++
		}
	}
	if at < len(runes) {
		return 0, false
	}
	return last - first, true
}

func (c *completion) move(delta int) {
	c.cursor = moveCursor(c.cursor, delta, len(c.sel))
	c.follow(c.rowsWanted())
}

// follow scrolls in LINE space against a cursor that lives in selectable space,
// and it pulls the section rule above the cursor into view with it. A rule is
// what says which half of the list a row is in, and a cursor sitting on the
// first task under a rule that has just scrolled off is a row whose meaning has
// gone off the top of the screen.
func (c *completion) follow(height int) {
	at := c.selLine()
	if at < 0 {
		c.top = 0
		return
	}
	c.top = listTop(at, c.top, len(c.lines), height)
	if at > 0 && c.lines[at-1].header != "" && c.top == at {
		c.top = at - 1
	}
}

// selLine is the line the cursor is standing on, or -1.
func (c *completion) selLine() int {
	if c.cursor < 0 || c.cursor >= len(c.sel) {
		return -1
	}
	return c.sel[c.cursor]
}

// picked reports whether the cursor is on a row that choosing does something
// with. It is what enter asks before it commits: an empty list, or a list still
// loading, is a list where enter is still the submit key.
func (c *completion) picked() bool {
	if !c.open {
		return false
	}
	return c.selLine() >= 0
}

// choice is the path under the cursor, and only ever a PATH: a task row answers
// false here and is picked up by [completion.taskChoice] instead. The split
// keeps every caller that predates task mentions — the /image argument list
// (input.go), the click — asking exactly the question it was asking before.
func (c *completion) choice() (string, bool) {
	at := c.selLine()
	if at < 0 || c.lines[at].file < 0 {
		return "", false
	}
	return c.all[c.lines[at].file], true
}

// taskChoice is the task under the cursor.
func (c *completion) taskChoice() (session.TaskIndexEntry, bool) {
	at := c.selLine()
	if at < 0 || c.lines[at].task < 0 {
		return session.TaskIndexEntry{}, false
	}
	return c.taskHits[c.lines[at].task], true
}

// height is how many rows the list wants. A walk still running wants one, and
// says so — an overlay that appeared silently a second after it was asked for
// would read as a glitch.
//
// THE WAIT IS FOR THE FILES ONLY. The task index is one small file and it is
// read in the same instant the walk is started; a list that held its whole self
// back until a ten-thousand-file walk returned would be hiding the half that was
// already there.
func (c *completion) height(width int) int {
	switch {
	case !c.open:
		return 0
	case !c.loaded && len(c.lines) == 0:
		return 1
	case len(c.lines) == 0:
		return 1
	}
	// The ceiling is in LINES (palette.go): a section rule is one wherever it is
	// drawn, a plain path is one because it has no tail to wrap, and a task row
	// with an age on it is two at [tierPhone].
	return overlayWindow(width, c.top, len(c.lines), c.rowsWanted(), func(at int) string {
		return c.lineNote(at)
	})
}

// imageTag is the row saying what choosing it will DO. Everywhere else on this
// list enter types a path; on these rows it attaches a picture (attach.go), and
// a list where one row means something else without saying so is a list that
// surprises people.
const imageTag = "img"

// folderTag is the same idea for the rows the walk now offers that are not
// files at all. It says WHAT the row is rather than what choosing it does,
// because choosing it does exactly what every other path row does — the path
// goes into the sentence — and the one thing a person cannot tell from
// `internal/tui3/` alone, on a narrow frame where the tail is what they read,
// is whether they are pointing at a folder or at something named like one.
const folderTag = "folder"

// isFolderPath reports whether a row from the walk is a directory. The trailing
// separator is the marker, and it is the walk's own ([walkFiles]).
func isFolderPath(path string) bool { return strings.HasSuffix(path, "/") }

// lineNote is the dim tail of one line of this list, and "" for a line that has
// none — a section rule, or a path that is not a picture. It is what decides
// the line's height at [tierPhone], so [completion.height] and
// [completion.rows] ask it rather than each deciding for themselves.
func (c *completion) lineNote(at int) string {
	line := c.lines[at]
	switch {
	case line.header != "" || line.filters:
		return ""
	case line.team >= 0:
		return mentionCount(c.teamHits[line.team])
	case line.chat >= 0:
		return c.chatHits[line.chat].note
	case line.task >= 0:
		return taskNoteWord(c.taskHits[line.task])
	case isFolderPath(c.all[line.file]):
		// A FOLDER IS A FOLDER ON BOTH LISTS, `@` and a command's argument
		// alike — the tag says what the row IS, and that does not change with
		// the door it was opened from the way the picture's tag does.
		return folderTag
	case !c.arg && isImagePath(c.all[line.file]):
		return imageTag
	default:
		return ""
	}
}

func (c *completion) rows(width, n int, pal palette, hover int, headKey string) []string {
	if n <= 0 {
		return nil
	}
	if len(c.lines) == 0 {
		if !c.loaded {
			return []string{pal.dim("  looking…")}
		}
		return []string{pal.dim("  " + c.emptyWord())}
	}
	c.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := c.top; at < len(c.lines) && fill.room(); at++ {
		line := c.lines[at]
		var ok bool
		switch {
		case line.filters:
			ok = fill.plain(mentionHeadLine(pal, c.scope, headKey, width))
		case line.header != "":
			ok = fill.plain(pal.dim("  " + fit(line.header, width-2)))
		case line.team >= 0:
			ok = fill.add(at, mentionTeamLabel(c.teamHits[line.team], pal), c.lineNote(at), at == c.selLine(), false)
		case line.chat >= 0:
			ok = fill.add(at, mentionChatLabel(c.chatHits[line.chat]), c.lineNote(at), at == c.selLine(), false)
		case line.task >= 0:
			ok = fill.add(at, taskRowLabel(c.taskHits[line.task], pal), c.lineNote(at), at == c.selLine(), false)
		default:
			ok = fill.add(at, c.all[line.file], c.lineNote(at), at == c.selLine(), false)
		}
		if !ok {
			break
		}
	}
	lines, _ := fill.done()
	return lines
}

// filesLoadedMsg carries the walk back to the loop.
type filesLoadedMsg struct {
	paths []string
	// home says the walk was home's list's (homeat.go) and not the box's.
	home bool
}

// loadFiles walks the workspace off the loop. It runs ONCE per surface: the
// list is a completion aid, and a person who creates a file mid-conversation
// can type its name, which is what they were going to do anyway.
//
// IT WALKS A DIRECTORY THIS PROCESS CAN ACTUALLY OPEN ([app.pathRoot]), which
// over --host is this machine's own and not the conversation's. There is no
// choice about it — the workspace is on another disk and filepath.WalkDir has no
// way to reach it — and the alternative is worse than a local list: walking the
// remote path HERE would either find nothing or, if a directory of that name
// happens to exist on this machine, offer somebody else's files as though they
// were the project's.
//
// The local list is the RIGHT one for the door that needs a real file — /image,
// whose picture is on the machine the person is sitting at and whose bytes
// travel with the message. For an "@" mention it is a convenience rather than an
// index: the completion only ever puts TEXT in the sentence (this file's own
// law — the surface does not read the file), and the engine resolves that text
// against its own workspace. So a name picked here is a name the far side looks
// up, which is right when the two machines hold the same project and visibly
// wrong when they do not.
func (a *app) loadFiles() tea.Cmd {
	root := a.pathRoot()
	if a.comp.loaded || a.comp.loading || root == "" {
		return nil
	}
	a.comp.loading = true
	return func() tea.Msg { return filesLoadedMsg{paths: walkFiles(root, walkCap)} }
}

// completeFile is enter on the list: the path replaces what was typed after the
// '@', and the '@' itself stays. What is submitted is the text as typed.
//
// An image chosen from an @token is the one row that does something else: the
// half-typed token comes OUT of the sentence and the file goes into the tray
// (attach.go). The token is removed rather than left behind because the person
// was never writing a path — they were reaching for a picture, and "@pho" is
// what reaching for it looked like halfway.
func (a *app) completeFile() {
	path, ok := a.comp.choice()
	if !ok {
		a.comp.close()
		return
	}
	e := &a.input
	if !a.comp.arg && isImagePath(path) {
		head := append([]rune(nil), e.value[:a.comp.at]...)
		tail := append([]rune(nil), e.value[e.cursor:]...)
		e.value = append(head, tail...)
		e.cursor = a.comp.at
		a.attach(a.resolvePath(path))
		a.comp.done = ""
		a.comp.close()
		a.touch()
		return
	}
	// The '@' is kept and the argument list has none to keep.
	keep := 1
	if a.comp.arg {
		keep = 0
	}
	head := append([]rune(nil), e.value[:a.comp.at+keep]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(path)...), tail...)
	e.cursor = a.comp.at + keep + len([]rune(path))
	a.comp.done = path
	a.comp.close()
	a.touch()
}

// walkFiles lists what is under root, relative to it, in walk order, capped.
//
// IT EMITS DIRECTORIES AS WELL AS FILES, and a directory is spelled with a
// trailing separator — `internal/tui3/`. The walk always VISITED them; what it
// did not do was offer them, so an `@` reaching for a folder found nothing and
// a person had to type the whole path. The slash is the marker and there is no
// parallel slice beside this one: it is what tells a directory row from a file
// row ([isFolderPath]), it is what the row's own tag is drawn from, and it is
// what goes into the sentence when the row is chosen — which reads the way
// somebody would have typed it anyway.
func walkFiles(root string, limit int) []string {
	out := make([]string, 0, 512)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is skipped, not fatal: a completion list
			// that refused to exist because of one permission is worth less
			// than a list with one directory missing from it.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			out = append(out, filepath.ToSlash(relative)+"/")
			if len(out) >= limit {
				return fs.SkipAll
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(relative))
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return out
}
