package session

// BEFORE THE MONEY: WHO ELSE IS ALREADY IN THE FILES THIS WORK NAMES.
//
// taskclaims.go can already answer "who else is writing this path right now".
// This file is the one MOMENT that question is worth the most — the moment a
// brief is about to become a paid run. Two windows taking the same files is
// discovered today at land time, when both invoices have been paid and one of
// the two results is thrown away; asked here it costs one readdir and a loop
// over two path lists.
//
// ── WHAT IS KNOWABLE AT THIS MOMENT, AND WHAT IS NOT ──
//
// A brief says what to do and only SOMETIMES says where. Nothing here resolves a
// brief into the files it WILL write, because that is a promise no reader can
// keep — the same reason claims are facts and never intents. So the guess is
// narrow on purpose: the paths a brief SPELLS, and nothing else. A brief that
// spells none is answered with the one thing still true — somebody else is
// working in this project — and never with a guess about where.
//
// That narrowness is what makes the answer honest, and it is also the whole of
// what a caller may claim from it. THIS FILE NEVER SAYS "YOU ARE ALONE." An
// empty line means there was nothing to say, not that the files are clear: the
// brief may have named no paths, and another window's node may not have written
// anything yet ([Elsewhere.Touching]'s second list, which is why the cautious
// wording below exists at all).
//
// ── IT IS A FACT, NEVER A GATE ──
//
// Nothing here blocks, queues, waits or retries. The line it makes is read by
// the person on the proposal card and by the model in its tool result, and both
// are free to do nothing about it. A preflight that could stop a task would be
// one window's file steering another window's work, which is exactly the law
// taskclaims.go and the coordination design both refuse.
//
// ── AND IT MUST NOT BECOME WALLPAPER ──
//
// A warning that fires on every proposal is a warning nobody reads by Tuesday,
// so two rules keep it quiet. A word counts as a place only when it is SPELLED
// with a directory on it and ends in an extension, which drops prose, import
// paths and bare filenames in one move. And an overlap that lies only in the
// files every piece of work touches — go.mod, the lockfile, CLAUDE.md — is not
// an overlap worth a line ([hotFiles]).

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// preflightPathsShown caps how many shared paths the line spells before it
	// says how many are left. Three is what still fits beside a task's name on a
	// card at an ordinary width, and the count after it keeps the claim honest.
	preflightPathsShown = 3
	// preflightNamesShown is the same cap over the other windows' task names, and
	// it is smaller because a title is a sentence where a path is a token.
	preflightNamesShown = 2
	// preflightScanLimit caps how many candidate paths one brief yields. A brief
	// that spells sixty files is a brief whose overlap is going to be reported out
	// of the first few anyway, and the cap is what stops a pasted directory
	// listing turning this into a real piece of work.
	preflightScanLimit = 64
)

// The person-facing words, spelled once. THE VOCABULARY IS THE ONE THE HISTORY
// PAGE ALREADY USES — `another window`, and the task's own title after a `·` —
// so somebody who has read a row there reads this line without learning
// anything new. Nothing here says conflict, collision, lock or blocked: nothing
// is being prevented, and a word that implied otherwise would be the surface
// lying about what the record is allowed to do.
const (
	preflightInWord    = "another window is already in "
	preflightOutWord   = "another window has work out in this project"
	preflightQuietWord = " · it has not said which files yet"
	preflightJoin      = " · "
)

// hotFiles are the files that every piece of work in a repository touches, keyed
// by base name so a nested one counts too.
//
// AN OVERLAP THAT IS ONLY HERE IS NOT NEWS. Two tasks in one repository both
// reaching go.mod is the ordinary day, and a warning that fired on it would fire
// on nearly every proposal — which is how a warning becomes wallpaper and then
// becomes a setting somebody turns off. The count is not hidden anywhere as a
// result: the line simply is not drawn, because the honest content of it was
// "these two pieces of work are both in this repository".
var hotFiles = map[string]bool{
	"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true,
	"CLAUDE.md": true, "README.md": true, "Makefile": true,
	"package.json": true, "package-lock.json": true, "pnpm-lock.yaml": true,
	"yarn.lock": true, "Cargo.toml": true, "Cargo.lock": true,
	"requirements.txt": true, "pyproject.toml": true, ".gitignore": true,
}

// PreflightNote is the ONE LINE a proposal carries about the work other windows
// already have out, or "" when there is nothing to say.
//
// It is pure and takes the reading it judges, so a surface holding a cached
// [Elsewhere] does not go back to the disk for this (internal/tui3 holds one on
// a three-second clock). root is the workspace the paths in parts would be
// relative to, and "" simply means an absolute path in the brief cannot be
// placed and is dropped.
//
// parts are the pieces of the brief to read paths out of — title, summary, the
// work itself, the deliverable, the acceptance — passed separately because they
// arrive separately and joining them here would be one more spelling of the
// same text.
func PreflightNote(root string, e Elsewhere, parts ...string) string {
	return preflightLine(briefFiles(root, parts...), e)
}

// preflightLine is the decision itself, over paths somebody has already pulled
// out of a brief.
//
// THE FOUR ANSWERS, in the order the better one comes first:
//
//   - nobody else has work out — nothing at all, and it is the common case.
//   - the brief spells paths another window's live node has already written —
//     the specific line, naming the paths and the work.
//   - the brief spells paths and nothing overlaps, but somewhere a live node has
//     said nothing about files — the cautious line. It is NOT "you are alone",
//     which is the claim [Elsewhere.Touching]'s second list exists to forbid.
//   - the brief spells no paths at all — the general line, said once, naming
//     where the other windows are so far.
func preflightLine(files []string, e Elsewhere) string {
	live := e.Tasks()
	if len(live) == 0 {
		return ""
	}
	if len(files) == 0 {
		// NOTHING WAS SPELLED, SO NOTHING CAN BE INTERSECTED. What is left is the
		// bare fact that somebody else is working here, plus — where their nodes
		// have written anything — the places they are in. That is offered as
		// orientation and not as an overlap, because no overlap was computed.
		if where := joinBounded(liveFiles(live), preflightPathsShown); where != "" {
			return preflightOutWord + preflightJoin + where
		}
		return preflightOutWord
	}
	touching, unknown := e.Touching(files)
	var shared, names []string
	seen := make(map[string]bool, len(files))
	for _, at := range touching {
		// THE BRIEF'S OWN ORDER, and hot files dropped: what is left is the part of
		// the overlap that is worth somebody's attention. A node whose only shared
		// path was the lockfile falls out here and is not named.
		hit := coolFiles(SharedFiles(files, at.Task.Files))
		if len(hit) == 0 {
			continue
		}
		for _, one := range hit {
			if !seen[one] {
				seen[one] = true
				shared = append(shared, one)
			}
		}
		if title := strings.TrimSpace(at.Task.Title); title != "" {
			names = append(names, title)
		}
	}
	if len(shared) > 0 {
		line := preflightInWord + joinBounded(shared, preflightPathsShown)
		if len(names) > 0 {
			line += preflightJoin + joinBounded(names, preflightNamesShown)
		}
		return line
	}
	if len(unknown) > 0 {
		return preflightOutWord + preflightQuietWord
	}
	// Every live node named its files and none of them is here. That is the one
	// case where somebody could be told they have these files to themselves — and
	// it is still not said, because the silence costs nothing and the sentence
	// would be believed for longer than the three seconds it is true for.
	return ""
}

// liveFiles is every path the other windows' nodes have written so far, first
// mention first, blanks and repeats dropped.
func liveFiles(live []ElsewhereTask) []string {
	var out []string
	seen := make(map[string]bool)
	for _, at := range live {
		for _, one := range at.Task.Files {
			if one = strings.TrimSpace(one); one == "" || seen[one] {
				continue
			}
			seen[one] = true
			out = append(out, one)
		}
	}
	return out
}

// coolFiles is these paths with the ones everything touches taken out. See
// [hotFiles] for why they are taken out rather than counted.
func coolFiles(paths []string) []string {
	var out []string
	for _, one := range paths {
		if hotFiles[path.Base(one)] {
			continue
		}
		out = append(out, one)
	}
	return out
}

// joinBounded spells at most n of these, comma-joined, and says how many are
// left over. Nothing in, nothing out — the emptiness law, so a caller can test
// the result rather than the input.
func joinBounded(items []string, n int) string {
	if len(items) == 0 {
		return ""
	}
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + " +" + strconv.Itoa(len(items)-n) + " more"
}

// ── reading places out of prose ─────────────────────────────────────────────

// briefFiles is every repo path a brief SPELLS, in the order it spells them.
//
// It is deliberately a poor guesser. The alternative — a model call, or a walk
// of the tree looking for names the brief might have meant — would put a second
// price and a second guess in front of work the person has not agreed to yet,
// and the design's rule for this moment is that a preflight which cannot guess
// says NOTHING rather than guesses wrong.
func briefFiles(root string, parts ...string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, part := range parts {
		for _, word := range strings.Fields(part) {
			one, ok := briefPath(root, word)
			if !ok || seen[one] {
				continue
			}
			seen[one] = true
			out = append(out, one)
			if len(out) == preflightScanLimit {
				return out
			}
		}
	}
	return out
}

// briefPathTrim are the characters prose wraps a path in — quotes, brackets,
// backticks and the punctuation a sentence ends on.
const briefPathTrim = "`'\"“”‘’(){}[]<>,;:.!?*…"

// briefPath decides whether one word of a brief is a place, and spells it the
// way a claim spells it: repo-relative and slash-spelled, which is what
// task_run.go's changedPath produces on the other side of the comparison.
//
// THE TEST IS A DIRECTORY AND AN EXTENSION, and it is strict on purpose:
//
//   - a URL is not a path in this repository;
//   - `internal/session/task.go` is a place, `task.go` is not — a bare name
//     cannot be matched against a claim that spells its directory, so accepting
//     one would only ever produce a miss or a false hit on the wrong file;
//   - `github.com/Agent-Field/aforge-v2/internal/session` is an import path and
//     has no extension, so it falls out without a rule of its own;
//   - `internal/session/task.go:112` is what grep and a compiler print, so the
//     line number is cut rather than making the whole word unreadable.
func briefPath(root, word string) (string, bool) {
	word = strings.Trim(word, briefPathTrim)
	if word == "" || strings.Contains(word, "://") {
		return "", false
	}
	if at := strings.LastIndex(word, ":"); at > 0 && digitsOnly(word[at+1:]) {
		word = word[:at]
	}
	word = filepath.ToSlash(word)
	if strings.HasPrefix(word, "/") {
		// AN ABSOLUTE PATH IS PLACED OR DROPPED. Claims are repo-relative, so a
		// path from another disk — or from this one with no workspace to measure it
		// against — is a path this comparison cannot spell and must not pretend to.
		if root == "" {
			return "", false
		}
		rel, err := filepath.Rel(root, filepath.FromSlash(word))
		if err != nil || strings.HasPrefix(filepath.ToSlash(rel), "..") {
			return "", false
		}
		word = filepath.ToSlash(rel)
	}
	word = path.Clean(word)
	if !strings.Contains(word, "/") || strings.HasPrefix(word, "..") {
		return "", false
	}
	if !hasExtension(path.Base(word)) {
		return "", false
	}
	return word, true
}

// hasExtension reports whether a base name ends in something that reads as a
// file extension: a dot with one to eight letters or digits after it.
func hasExtension(name string) bool {
	dot := strings.LastIndex(name, ".")
	if dot <= 0 || dot == len(name)-1 || len(name)-dot-1 > 8 {
		return false
	}
	return isAlphanumeric(name[dot+1:])
}

func digitsOnly(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAlphanumeric(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// ── the agent's own door ────────────────────────────────────────────────────

// taskPreflight is [PreflightNote] over this session's own reading of the other
// windows open on this project.
//
// IT TAKES THE READING HERE rather than holding one, because it is asked once
// per proposal and a held reading would be a claim about liveness that aged
// while nothing was looking at it. [Agent.Elsewhere] answers the empty reading
// for a session with no folder on disk, which is how a memory-only conversation
// and a scripted test get silence without a rule of their own.
func (a *Agent) taskPreflight(parts ...string) string {
	return PreflightNote(a.config.Workspace, a.Elsewhere(), parts...)
}

// withElsewhere puts the preflight's one line at the end of what the model
// reads, and puts nothing there when there was nothing to say.
func withElsewhere(result, line string) string {
	if line == "" {
		return result
	}
	return result + "\n" + line
}
