package session

// WHAT GIT ALREADY KNOWS, PUT IN FRONT OF THE MODEL INSTEAD OF SHELLED OUT FOR.
//
// This is [nowLine]'s argument one file over. The prompt carried the working
// directory and nothing about its STATE, so a turn that needed to know which
// branch it was on — which is nearly every turn that is about to change a
// repository — opened with `bash git status`: a tool row the person saw, a
// round trip, a step, and an approval question, all to learn something the
// process could have read beside them for nothing.
//
// So `# Project` now carries, for the working directory and for every
// repository the person attached: the branch, whether the tree is clean and how
// many files are not, and how far the branch is from its upstream.
//
// ── IT IS READ BESIDE THE WORK AND NEVER IN FRONT OF IT ─────────────────────
//
// `git status` is milliseconds on a small repository and SECONDS on a large
// one, and the whole of this file's discipline is that nobody ever waits on it.
// The reading is taken with [offpath.Take] — the one shape this codebase has
// for "a fact gathered beside the work and read when the next request is
// assembled" ([loopWatch.treeMoved] is the other caller) — and folded in with a
// ZERO-length settle: what has arrived is used, what has not stays in flight
// and is taken for free at the start of the next turn. [Agent.refreshGitLocked]
// runs under a.mu on the person's path, so a settle that could block there
// would be exactly the thing the ≤50 ms law forbids.
//
// ── AND MESSAGE[0] MOVES ONLY WHEN THE ANSWER DOES ──────────────────────────
//
// A changed byte in message[0] re-prices the whole transcript at the uncached
// rate (memory.go's [Agent.refreshSystemLocked] states the law), so this is
// folded in AT THE START OF A TURN and nowhere else. Inside a turn — the forty
// rounds of a long piece of work, where a write moves the tree every few
// seconds — the prefix is byte-identical, and a conversation whose repository
// nobody touched re-renders nothing at all, because the text compares equal.
// One re-price per turn in which something real moved is the same trade the
// clock and the standing orders already make.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/offpath"
)

// gitFactsLine is one root's state, or "" for a directory that is not a
// repository — THE EMPTINESS LAW: "not a repository" is a fact about nothing,
// and a line saying it would be noise on every conversation opened in a plain
// folder.
//
// ONE COMMAND ANSWERS ALL THREE QUESTIONS. `git status --short --branch` prints
// a `##` header carrying the branch, its upstream and the ahead/behind counts
// git has already computed against it, and then one line per path that is not
// clean. A second `rev-list --left-right --count` would be a second reading of
// a number this one already holds, and the two would disagree the moment a
// fetch landed between them.
func gitFactsLine(root string) string {
	// A COMMAND WITH NO DIRECTORY RUNS WHEREVER THE PROCESS HAPPENS TO BE, which
	// for a conversation with no workspace — a headless run, a test — would be
	// the state of whatever checkout the binary was launched from, reported to
	// the model as though it were its own project. task_run.go's [git] carries
	// the incident this rule comes from; the answer here is to ask nothing.
	if strings.TrimSpace(root) == "" {
		return ""
	}
	out, err := git(root, "status", "--short", "--branch")
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "## ") {
		return ""
	}
	facts := []string{gitBranchWord(lines[0])}
	// EVERY OTHER LINE IS A PATH THAT IS NOT CLEAN, which is the count a person
	// and a model both mean by "dirty": staged, unstaged and untracked alike.
	changed := 0
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			changed++
		}
	}
	if changed == 0 {
		facts = append(facts, "clean")
	} else {
		facts = append(facts, strconv.Itoa(changed)+" "+gitFilesWord(changed)+" not committed")
	}
	if stand := gitUpstreamWord(lines[0]); stand != "" {
		facts = append(facts, stand)
	}
	return strings.Join(facts, ", ")
}

// gitFilesWord is the plural, spelled here because nothing in this package
// should be composing a sentence out of a bare count.
func gitFilesWord(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

// gitBranchWord reads the branch out of the `## ` header.
//
// The header is `## <branch>...<upstream> [ahead 1, behind 2]`, `## <branch>`
// with no upstream, or `## HEAD (no branch)` on a detached checkout — and the
// last of those is said in git's own words, because "on HEAD" would be this
// program inventing a spelling for a state the person already recognises.
func gitBranchWord(header string) string {
	head := strings.TrimSpace(strings.TrimPrefix(header, "## "))
	if index := strings.Index(head, " ["); index >= 0 {
		head = head[:index]
	}
	if index := strings.Index(head, "..."); index >= 0 {
		head = head[:index]
	}
	if head == "" || strings.HasPrefix(head, "HEAD (") {
		return "on no branch"
	}
	return "on " + head
}

// gitUpstreamWord is how far the branch stands from what it tracks, in the
// upstream's own name: `2 ahead of origin/dev`, `1 behind origin/main`, or both.
//
// A BRANCH WITH NO UPSTREAM SAYS NOTHING, and a branch level with its upstream
// says nothing either. Both are the emptiness law: "0 ahead, 0 behind" is the
// ordinary state of a repository and carries no instruction to anybody.
func gitUpstreamWord(header string) string {
	head := strings.TrimSpace(strings.TrimPrefix(header, "## "))
	marker := strings.Index(head, "...")
	if marker < 0 {
		return ""
	}
	rest := head[marker+3:]
	upstream, counts := rest, ""
	if index := strings.Index(rest, " ["); index >= 0 {
		upstream, counts = rest[:index], strings.Trim(rest[index+1:], "[]")
	}
	if strings.TrimSpace(counts) == "" {
		return ""
	}
	var said []string
	for _, part := range strings.Split(counts, ",") {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) != 2 {
			continue
		}
		// git writes `ahead 2` and `behind 1`; `gone` has no count and is not a
		// distance, so it falls out here rather than being half-read.
		said = append(said, fields[1]+" "+fields[0])
	}
	if len(said) == 0 {
		return ""
	}
	return strings.Join(said, " and ") + " of " + upstream
}

// gitFacts is the whole block of lines `# Project` carries, and "" when there is
// nothing true to say.
//
// THE WORKING DIRECTORY COMES FIRST AND IS NAMED AS ITSELF, because the line
// above it in the prompt already gave its path and this is that directory's
// state. An attached folder is named by its absolute path, exactly as the
// `# Attached folders` block names it, so the model can match the two without
// resolving anything.
func gitFacts(workspace string, places []PlaceRef) string {
	var out strings.Builder
	if line := gitFactsLine(strings.TrimSpace(workspace)); line != "" {
		fmt.Fprintf(&out, "- Git here: %s\n", line)
	}
	for _, place := range places {
		if place.Arrival != PlaceSaid || !place.Repository {
			continue
		}
		if line := gitFactsLine(place.Path); line != "" {
			fmt.Fprintf(&out, "- Git in %s: %s\n", place.Path, line)
		}
	}
	return out.String()
}

// gitFactsReading is the reading itself, named so a test can record what was run
// without staging a repository, and so this and any later caller cannot drift
// into two readings of one thing.
var gitFactsReading = gitFacts

// refreshGitLocked folds in whatever the last reading found and starts the next
// one, and it does neither of those things in front of anybody.
//
// It is called with a.mu held, from [Agent.startTurnLocked], beside
// [Agent.refreshClockLocked] and [Agent.refreshStandingLocked] — the three facts
// a turn must not open blind to, refreshed on one trigger. NOTHING HERE MAY
// BLOCK: the settle is zero-length by law, so a repository too big to read
// between two turns arrives a turn late rather than making somebody wait, and
// the prompt goes on saying what was true at the last reading rather than
// saying nothing.
func (a *Agent) refreshGitLocked(now time.Time) {
	if !a.systemOwn {
		// Somebody else owns this prompt — a task node handed one, a test that
		// set its own — and rewriting it here would replace what they wrote.
		return
	}
	if a.gitAhead != nil {
		if text, settled := a.gitAhead.Settle(0); settled {
			a.gitAhead = nil
			if text != a.config.gitFacts {
				a.config.gitFacts = text
				a.system = renderSystemAt(a.config, now)
				a.systemAt = now
			}
		}
	}
	if a.gitAhead == nil {
		// The workspace and the folder set are taken HERE, on this side of the
		// goroutine, so nothing the gatherer reads can be written while it runs.
		read, workspace := gitFactsReading, strings.TrimSpace(a.config.Workspace)
		places := append([]PlaceRef{}, a.places...)
		a.gitAhead = offpath.Take(func() string { return read(workspace, places) })
	}
}

// gitFactsBlock is what `# Project` renders, or nothing at all. It is a method on
// Config rather than a field read in [renderSystemAt] so that the one rule about
// spacing lives with the lines it is about.
func (c Config) gitFactsBlock() string {
	text := strings.TrimRight(c.gitFacts, "\n")
	if text == "" {
		return ""
	}
	return text + "\n"
}
