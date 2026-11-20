package session

// A TASK DOES NOT SEARCH THE WHOLE COMPUTER.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// qloop run32, FIRE t1. A canvas built its sheet in ninety seconds and handed a
// "Chennai move rules doc" to a task. The task's worker, unsure where a skill's
// text lived, ran
//
//	find / -iname '*reports*research*' -not -path '*/.git/*' 2>/dev/null | head
//
// and that one read-only command held the worker for the full ten-minute
// ceiling: it printed nothing (every hit was further down a disk of build
// caches, and every refusal went to /dev/null), so nothing on the screen moved,
// and the person's turn ran past fifteen minutes with the sheet long since done.
// Other runs show the same reach in quieter forms — a worker listing the
// person's private notes folder, a worker walking a website checkout two
// projects away — because nothing told any of them where their search belonged.
//
// ── WHY READING STAYS OPEN, AND WHY THIS IS NOT A READ LAW ──
//
// taskoutside.go states the law a task lives under: it may READ THE WHOLE
// MACHINE and write in one directory. That is deliberate and it is kept. A task
// briefed about a repository it is not standing in has to be able to look at
// it, a task told "the export is in ~/Downloads" has to be able to open it, and
// looking at a named place has never been what goes wrong.
//
// What goes wrong is looking EVERYWHERE. A walk of `/` or of the person's whole
// home is not a read of anything the brief named — it is a guess, it costs
// minutes, it can wander through the person's private folders on the way, and
// its answer is almost never the thing the task needed. So this file refuses
// exactly that shape and nothing wider: a SEARCH (a program whose job is to walk
// a tree) ROOTED AT A WIDE ROOT (the top of the disk, one level under it, the
// person's home, or the folder that holds every home) and NOT BOUNDED BY A
// DEPTH. `cat /etc/hosts`, `ls ~`, `grep -rn x ~/that-repo` and
// `find / -maxdepth 2 -name bin` all pass.
//
// ── AND A SEARCH THAT GOES QUIET IS STOPPED ──
//
// The refusal reads the paths a command names, and a search can still be wide
// without naming a wide root (`cd / && find . -name x` is caught; a path built
// out of a variable is not, and approval's CommandWords says why). So the second
// half is a clock on what the search SAYS: a search that has printed nothing for
// [searchQuietLimit] is walking far more than it is finding, and it is stopped
// with the output it had and a sentence naming the task's own folder. A search
// that is finding things keeps printing, and printing resets the clock, so a
// long search that is working is never cut by it.
//
// Both halves are a task's and never a conversation's, for taskoutside.go's
// reason: a person in their own chat who asks for `find ~ -name thesis.pdf`
// asked for it, and a conversation already yields a long command to a job at
// the background-after clock so their keyboard stays theirs.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

const (
	// searchQuietLimit is how long a task's search may print nothing before it is
	// stopped. Thirty seconds is longer than a search of any one project takes to
	// print its first hit, and it sits at the conversation's own background-after
	// default, so the two clocks a person might read agree.
	searchQuietLimit = 30 * time.Second

	// searchQuietPoll is how often a running search's quiet is read. The figure
	// is the resolution of the cut, not its bound.
	searchQuietPoll = 2 * time.Second

	// searchDepthBound is the deepest a depth-limited search of a wide root may
	// go and still pass. `find / -maxdepth 2 -name bin` looks at a few hundred
	// directories; a depth that reaches into every project does not.
	searchDepthBound = 3
)

// ── what a search is ────────────────────────────────────────────────────────

// treeSearch is one command segment that walks a tree: the program, the roots it
// walks, and whether a depth bound keeps it shallow.
type treeSearch struct {
	program string
	roots   []string
	shallow bool
}

// readTreeSearch answers the walk one segment's words make, or false for a
// segment that is not a tree walk. The words are the command's head as
// [taskSegmentHead] hands it back: program first, redirections already gone.
//
// IT IS A LIST OF PROGRAMS AND NOT A CLASSIFIER. Each entry knows where its
// program takes its roots and how its program spells a depth bound, and a
// program not listed here is not a search to this file — which is the side to
// err on, since the cost of a miss is the quiet clock and the cost of a false
// hit is a refused command that was fine.
func readTreeSearch(words []string) (treeSearch, bool) {
	if len(words) == 0 {
		return treeSearch{}, false
	}
	program := filepath.Base(words[0])
	rest := words[1:]
	switch program {
	case "find", "gfind":
		return readFind(program, rest), true
	case "fd", "fdfind":
		return readPositional(program, rest, 1, depthFlags{"-d": true, "--max-depth": true, "--maxdepth": true},
			map[string]bool{"--search-path": true}), true
	case "rg", "ag", "ack":
		return readPatternSearch(program, rest, depthFlags{"--max-depth": true, "--maxdepth": true, "-d": program == "rg"}), true
	case "grep", "egrep", "fgrep", "rgrep":
		if program != "rgrep" && !grepRecursive(rest) {
			return treeSearch{}, false
		}
		return readPatternSearch(program, rest, depthFlags{}), true
	case "du":
		return readPositional(program, rest, 0, depthFlags{"-d": true, "--max-depth": true}, nil), true
	case "tree":
		return readPositional(program, rest, 0, depthFlags{"-L": true}, nil), true
	case "ls":
		if !lsRecursive(rest) {
			return treeSearch{}, false
		}
		return readPositional(program, rest, 0, depthFlags{}, nil), true
	}
	return treeSearch{}, false
}

// depthFlags are the spellings of one program's depth bound.
type depthFlags map[string]bool

// shallowDepth reads one depth figure and answers whether it keeps a search
// shallow enough to pass.
func shallowDepth(value string) bool {
	depth, err := strconv.Atoi(strings.Trim(value, `"'`))
	return err == nil && depth >= 0 && depth <= searchDepthBound
}

// readFind reads find's roots — the words before its first expression — and its
// -maxdepth.
func readFind(program string, rest []string) treeSearch {
	search := treeSearch{program: program}
	expression := false
	for index, word := range rest {
		if word == "-maxdepth" && index+1 < len(rest) && shallowDepth(rest[index+1]) {
			search.shallow = true
		}
		if expression {
			continue
		}
		// find's own options come before its roots and take no path.
		if word == "-H" || word == "-L" || word == "-P" || strings.HasPrefix(word, "-O") || strings.HasPrefix(word, "-D") && len(word) == 2 {
			continue
		}
		if strings.HasPrefix(word, "-") || word == "(" || word == "!" || word == "\\(" {
			expression = true
			continue
		}
		search.roots = append(search.roots, word)
	}
	if len(search.roots) == 0 {
		search.roots = []string{"."}
	}
	return search
}

// readPositional reads a program whose roots are its plain words after `skip`
// leading ones (fd's pattern is one), plus any flag in `rootFlags` that names a
// root by value.
func readPositional(program string, rest []string, skip int, depth depthFlags, rootFlags map[string]bool) treeSearch {
	search := treeSearch{program: program}
	plain := 0
	for index := 0; index < len(rest); index++ {
		word := rest[index]
		name, value, joined := strings.Cut(word, "=")
		switch {
		case depth[word] && index+1 < len(rest):
			search.shallow = search.shallow || shallowDepth(rest[index+1])
			index++
		case joined && depth[name]:
			search.shallow = search.shallow || shallowDepth(value)
		case rootFlags[word] && index+1 < len(rest):
			search.roots = append(search.roots, rest[index+1])
			index++
		case joined && rootFlags[name]:
			search.roots = append(search.roots, value)
		case strings.HasPrefix(word, "-"):
		default:
			if plain >= skip {
				search.roots = append(search.roots, word)
			}
			plain++
		}
	}
	if len(search.roots) == 0 {
		search.roots = []string{"."}
	}
	return search
}

// readPatternSearch reads grep, rg, ag and ack: the first plain word is the
// pattern unless one was given by flag, and every plain word after it is a root.
func readPatternSearch(program string, rest []string, depth depthFlags) treeSearch {
	skip := 1
	for _, word := range rest {
		if word == "-e" || word == "-f" || word == "--regexp" || word == "--file" ||
			strings.HasPrefix(word, "--regexp=") || strings.HasPrefix(word, "--file=") {
			skip = 0
		}
	}
	// A flag that takes a value must not have its value read as a root, so the
	// values of the pattern and depth flags are dropped before the roots are
	// read; other valued flags (-A 3, -g '*.go') are the rare miss, and a miss
	// here can only make a search look narrower than it is.
	cleaned := make([]string, 0, len(rest))
	for index := 0; index < len(rest); index++ {
		word := rest[index]
		switch word {
		case "-e", "-f", "--regexp", "--file", "-g", "--glob", "-t", "--type", "-A", "-B", "-C",
			"--include", "--exclude", "--exclude-dir", "-m", "--max-count":
			index++
			continue
		}
		cleaned = append(cleaned, word)
	}
	return readPositional(program, cleaned, skip, depth, nil)
}

// grepRecursive reports whether grep was asked to walk directories at all.
func grepRecursive(rest []string) bool {
	for index, word := range rest {
		switch {
		case word == "--recursive" || word == "--dereference-recursive":
			return true
		case word == "-d" && index+1 < len(rest) && rest[index+1] == "recurse",
			word == "--directories=recurse":
			return true
		case strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.ContainsAny(word[1:], "rR"):
			return true
		}
	}
	return false
}

// lsRecursive reports whether ls was asked to list every directory below.
func lsRecursive(rest []string) bool {
	for _, word := range rest {
		if word == "--recursive" || strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word[1:], "R") {
			return true
		}
	}
	return false
}

// ── what a wide root is ─────────────────────────────────────────────────────

// wideRoot reports whether walking path is walking the computer rather than a
// place: the top of the disk, anything one level under it (/home, /usr, /tmp,
// /Users, /Volumes), the person's home, or the folder their home sits in.
//
// A ROOT THE TASK IS STANDING IN IS NEVER WIDE. A task whose own folder is the
// person's home — which nothing builds today, and which a person could — may
// search its own folder however it likes; the law is about leaving it.
func wideRoot(ground, path, home string) bool {
	path = filepath.Clean(path)
	if ground != "" && withinDir(ground, path) {
		return false
	}
	if path == "/" || filepath.Dir(path) == "/" {
		return true
	}
	if home = filepath.Clean(strings.TrimSpace(home)); home != "." && home != "" && home != "/" {
		if path == home || path == filepath.Dir(home) {
			return true
		}
	}
	return false
}

// searchPath resolves one root the way the shell would, for the spellings of the
// person's home a command writes: `~`, `$HOME` and `${HOME}`.
func searchPath(cwd, word, home string) string {
	word = strings.Trim(word, `"'`)
	for _, spelling := range []string{"${HOME}", "$HOME"} {
		if word == spelling || strings.HasPrefix(word, spelling+"/") {
			if home == "" {
				return ""
			}
			word = home + strings.TrimPrefix(word, spelling)
		}
	}
	if strings.HasPrefix(word, "$") {
		// A root built out of any other variable is a root this reader cannot
		// place; the quiet clock is what answers it.
		return ""
	}
	if word == "~" || strings.HasPrefix(word, "~/") {
		if home == "" {
			return ""
		}
		word = home + strings.TrimPrefix(word, "~")
	}
	return resolvePath(cwd, word)
}

// wideSearchAim answers the first search in a command that walks a wide root —
// spelled the way the refusal will say it, `find /` — or "" when there is none.
//
// IT CARRIES `cd` THE WAY [outsideScopeAim] DOES, because `cd / && find . -name
// x` is the same walk as `find / -name x` and a reader that took each segment
// alone would see a search of the current directory.
func wideSearchAim(ground, command, home string) string {
	cwd := ground
	for _, segment := range approval.CommandWords(command) {
		words, _ := taskSegmentHead(stripRedirections(segment))
		if len(words) == 0 {
			continue
		}
		if filepath.Base(words[0]) == "cd" {
			cwd = resolveCd(cwd, words[1:])
			continue
		}
		search, ok := readTreeSearch(words)
		if !ok || search.shallow {
			continue
		}
		for _, root := range search.roots {
			path := searchPath(cwd, root, home)
			if path != "" && wideRoot(ground, path, home) {
				return search.program + " " + path
			}
		}
	}
	return ""
}

// commandSearches reports whether any segment of a command is a tree walk, which
// is what puts the quiet clock on it.
func commandSearches(command string) bool {
	for _, segment := range approval.CommandWords(command) {
		words, _ := taskSegmentHead(stripRedirections(segment))
		if _, ok := readTreeSearch(words); ok {
			return true
		}
	}
	return false
}

// ── the refusal and the cut, in the words the worker reads ──────────────────

// wideSearchRefusal is the whole of the refusal's wording, in one place. It names
// the search, says why it is not run, and names the task's own folder — the one
// place the model can always search instead — so the next call has somewhere to
// go rather than a corner of the same command to change.
func wideSearchRefusal(aim, ground string) string {
	return aim + " searches far beyond this task, so it was not run: a search that wide takes many minutes and passes through the person's own folders. " +
		"This task works in " + ground + " — search there, or in a folder your brief names, or keep a wide search shallow with a depth limit of " +
		strconv.Itoa(searchDepthBound) + " or less."
}

// searchQuietSentence is what a stopped search ends with, after the output it
// had. It says what happened in the same plain terms as the refusal, so a model
// that meets both learns one rule.
func searchQuietSentence(ground string) string {
	return "Search stopped: it printed nothing for " + strconv.Itoa(int(searchQuietLimit/time.Second)) +
		" seconds, so it was walking far more than it was finding. This task works in " + ground +
		" — search there, or in a folder your brief names."
}

// ── the guard ───────────────────────────────────────────────────────────────

// wideSearchGuard is the control plane's citizen for the refusal. It is a task's
// alone; see this file's header.
type wideSearchGuard struct{ agent *Agent }

func (wideSearchGuard) Name() string { return "wide-search" }

func (g wideSearchGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	if g.agent == nil || !g.agent.config.InTask {
		return call, toolResult{}, true
	}
	ground := g.agent.taskGround()
	if ground == "" {
		return call, toolResult{}, true
	}
	home, _ := os.UserHomeDir()
	var aim string
	switch call.Function.Name {
	case "bash":
		var parsed struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &parsed) != nil {
			return call, toolResult{}, true
		}
		aim = wideSearchAim(ground, parsed.Command, home)
	case "find", "grep":
		// The belt's own search tools walk a tree from their path argument, and
		// `find` with a pattern across `/` is the same ten minutes as the shell's.
		var parsed struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(call.Function.Arguments), &parsed) != nil || strings.TrimSpace(parsed.Path) == "" {
			return call, toolResult{}, true
		}
		if path := searchPath(ground, parsed.Path, home); path != "" && wideRoot(ground, path, home) {
			aim = call.Function.Name + " " + path
		}
	}
	if aim == "" {
		return call, toolResult{}, true
	}
	return call, toolResult{text: wideSearchRefusal(aim, ground), isError: true}, false
}

// ── the quiet clock ─────────────────────────────────────────────────────────

// armSearchQuiet puts the quiet clock on one running foreground call when this
// agent is a task and the command is a search, and answers the function that
// takes it off again (nil when nothing was armed).
//
// IT STOPS THE COMMAND RATHER THAN HANDING IT TO A JOB. A worker waits for the
// commands it started (task_job_park.go), so a silent search promoted into a job
// would hold the worker exactly as long as it held the call; what ends the wait
// for the person is ending the search, with the output it had.
func (a *Agent) armSearchQuiet(call *bare.BashCall) func() {
	if !a.config.InTask || !commandSearches(call.Command()) {
		return nil
	}
	limit, poll := searchQuietLimit, searchQuietPoll
	if a.config.searchQuiet > 0 {
		limit, poll = a.config.searchQuiet, a.config.searchQuiet/10+time.Millisecond
	}
	sentence := searchQuietSentence(a.taskGround())
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if call.QuietFor() >= limit {
					call.Stop(sentence)
					return
				}
			}
		}
	}()
	return func() { close(done) }
}
