package tui3

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
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

// The completion's three bounds. The walk is capped so that an @ typed inside a
// home directory cannot become a filesystem crawl; the list shows a screenful;
// two characters is where the query stops matching everything.
const (
	walkCap      = 10000
	completeRows = 8
	completeMin  = 2
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
	// done is the query this completion just INSERTED at at. It is what keeps
	// the list from reopening on top of its own answer: the caret ends up
	// inside a perfectly good @token, and a list that reappeared over it would
	// make enter a key that never finishes.
	done string

	all     []string
	loaded  bool
	loading bool

	hits   []int
	score  []int
	cursor int
	top    int
}

// sync opens, narrows or closes the completion from the draft and the caret. It
// is called after every edit; there is no key for it, because the key is "@".
func (c *completion) sync(e *editor) {
	at, query, ok := atToken(e.value, e.cursor)
	if !ok || len([]rune(query)) < completeMin {
		c.open = false
		return
	}
	if at == c.at && query == c.done {
		c.open = false
		return
	}
	was := c.open && c.at == at
	c.open, c.at, c.query = true, at, query
	c.rank()
	if !was {
		c.cursor, c.top = 0, 0
	}
}

func (c *completion) close() { c.open = false }

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

// rank scores every path against the query and keeps the ones that match.
func (c *completion) rank() {
	if cap(c.score) < len(c.all) {
		c.score = make([]int, len(c.all))
	}
	needle := strings.ToLower(c.query)
	c.hits = c.hits[:0]
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
	c.cursor = moveCursor(c.cursor, 0, len(c.hits))
	c.follow(completeRows)
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
	c.cursor = moveCursor(c.cursor, delta, len(c.hits))
	c.follow(completeRows)
}

func (c *completion) follow(height int) { c.top = listTop(c.cursor, c.top, len(c.hits), height) }

// choice is the path under the cursor.
func (c *completion) choice() (string, bool) {
	if !c.open || c.cursor < 0 || c.cursor >= len(c.hits) {
		return "", false
	}
	return c.all[c.hits[c.cursor]], true
}

// height is how many rows the list wants. A walk still running wants one, and
// says so — an overlay that appeared silently a second after it was asked for
// would read as a glitch.
func (c *completion) height() int {
	switch {
	case !c.open:
		return 0
	case !c.loaded:
		return 1
	case len(c.hits) == 0:
		return 1
	case len(c.hits) < completeRows:
		return len(c.hits)
	default:
		return completeRows
	}
}

func (c *completion) rows(width, n int, pal palette) []string {
	if n <= 0 {
		return nil
	}
	if !c.loaded {
		return []string{pal.dim("  looking…")}
	}
	if len(c.hits) == 0 {
		return []string{pal.dim("  no file matches")}
	}
	c.follow(n)
	out := make([]string, 0, n)
	for at := c.top; at < len(c.hits) && len(out) < n; at++ {
		out = append(out, overlayRow(c.all[c.hits[at]], "", at == c.cursor, false, width, pal))
	}
	return out
}

// filesLoadedMsg carries the walk back to the loop.
type filesLoadedMsg struct{ paths []string }

// loadFiles walks the workspace off the loop. It runs ONCE per surface: the
// list is a completion aid, and a person who creates a file mid-conversation
// can type its name, which is what they were going to do anyway.
func (a *app) loadFiles() tea.Cmd {
	if a.comp.loaded || a.comp.loading || a.workspace == "" {
		return nil
	}
	a.comp.loading = true
	root := a.workspace
	return func() tea.Msg { return filesLoadedMsg{paths: walkFiles(root, walkCap)} }
}

// completeFile is enter on the list: the path replaces what was typed after the
// '@', and the '@' itself stays. What is submitted is the text as typed.
func (a *app) completeFile() {
	path, ok := a.comp.choice()
	if !ok {
		a.comp.close()
		return
	}
	e := &a.input
	head := append([]rune(nil), e.value[:a.comp.at+1]...)
	tail := append([]rune(nil), e.value[e.cursor:]...)
	e.value = append(append(head, []rune(path)...), tail...)
	e.cursor = a.comp.at + 1 + len([]rune(path))
	a.comp.done = path
	a.comp.close()
	a.touch()
}

// walkFiles lists the files under root, relative to it, in walk order, capped.
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
