package run

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── WHAT A STEP IS TOLD ABOUT ITS ITEM ─────────────────────────────────────
//
// A STEP MUST NEVER NEED THE NETWORK OR GIT PLUMBING TO SEE WHAT IT IS
// WORKING ON. On the 2026-10-09 run of a pull request the read step's brief
// carried the title and a body cut mid-sentence and nothing else: no files,
// no base, no checks, no word of where the change was. It ran `git remote`
// and `git worktree` (refused), read .git, then searched the web and fetched
// the pull request's diff page and the API's file list. Everything it went
// looking for was already on the item. So the brief carries it: who opened
// it, base and head, the changed files with their counts, the checks, the
// last comments, the issue it closes, and where in the work tree the change
// is read. The rest of the brief is in [stageBrief].

// briefCommentMost is how much of one comment the brief carries.
const briefCommentMost = 600

// briefFilesMost is how many changed files the brief lists.
const briefFilesMost = 20

// stageRole is the sentence that tells the conversation what it is: one step
// of a run on this item, with the goal on the line above it.
func stageRole(job Job) string {
	name := stageLabel(job.Stage)
	if name == "" {
		return ""
	}
	on := itemNoun(job.Item)
	if ref := job.Item.Ref(); ref != "" && on != "" {
		on += " " + ref
	}
	line := "You are the " + name + " step of a codeaf run"
	if on != "" {
		line += " on " + on
	}
	return line + ". The line above is this step's goal. What you need is below and in your work tree: work from them, not the network."
}

// itemNoun is what the item is, in words: `pull request`, `issue`, "" for
// work with no forge kind.
func itemNoun(it factory.Item) string {
	switch it.Kind {
	case factory.KindPR:
		return "pull request"
	case factory.KindIssue:
		return "issue"
	case factory.KindCI:
		return "ci run"
	case factory.KindChore:
		return "chore"
	}
	return ""
}

// itemContext is the item as the step needs it, one paragraph each, every
// paragraph left out when it has nothing to say.
func itemContext(job Job) [][]string {
	it := job.Item
	head := it.Ref()
	if t := oneLine(it.Title); t != "" {
		head += " · " + t
	}
	if r := oneLine(it.Repo); r != "" {
		if p := oneLine(it.Product); p != "" && !strings.Contains(r, "/") {
			r = p + "/" + r
		}
		head += " · " + r
	}
	if n := itemNoun(it); n != "" {
		head = n + " " + head
	}
	paras := [][]string{{head, itemFacts(it), itemLinked(it), cutBody(it.Body)}}
	if files := changedFiles(it); len(files) > 0 {
		paras = append(paras, files)
	}
	if said := itemComments(it); len(said) > 0 {
		paras = append(paras, said)
	}
	if tree := workTree(job); tree != "" {
		paras = append(paras, []string{tree})
	}
	return paras
}

// itemFacts is the line under the title: `by 7vignesh · into main from
// 7vignesh:census-lanes · +188 −1 · checks: check success, lint failure ·
// labels: bug`.
func itemFacts(it factory.Item) string {
	var parts []string
	if a := oneLine(it.Author); a != "" {
		parts = append(parts, "by "+a)
	}
	if it.Kind == factory.KindPR {
		if b := oneLine(it.Base); b != "" {
			flow := "into " + b
			if h := oneLine(it.Head); h != "" {
				flow += " from " + h
			}
			parts = append(parts, flow)
		}
		if d := oneLine(it.Diff); d != "" {
			parts = append(parts, d)
		}
		if c := itemChecks(it); c != "" {
			parts = append(parts, "checks: "+c)
		}
	}
	var labels []string
	for _, l := range it.Labels {
		if l = oneLine(l); l != "" {
			labels = append(labels, l)
		}
	}
	if len(labels) > 0 {
		parts = append(parts, "labels: "+strings.Join(labels, ", "))
	}
	return strings.Join(parts, " · ")
}

// itemChecks is each check by name and state, or the one-word summary when
// the runs were not read.
func itemChecks(it factory.Item) string {
	var runs []string
	for _, r := range it.CheckRuns {
		name, state := oneLine(r.Name), oneLine(r.State)
		if name == "" {
			continue
		}
		if state != "" {
			name += " " + state
		}
		runs = append(runs, name)
	}
	if len(runs) > 0 {
		return strings.Join(runs, ", ")
	}
	return oneLine(it.Checks)
}

// closesRef is a closing keyword and the issue it names: `fixes #12`.
var closesRef = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s+#(\d+)\b`)

// titleRef is the `(#969)` a pull request's title ends on by convention.
var titleRef = regexp.MustCompile(`\(#(\d+)\)\s*$`)

// itemLinked is the issue a pull request says it closes or answers:
// `linked: #969`. "" for an issue, and for a pull request that names none.
func itemLinked(it factory.Item) string {
	if it.Kind != factory.KindPR {
		return ""
	}
	var refs []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && n != strconv.Itoa(it.Num) && !seen[n] {
			seen[n] = true
			refs = append(refs, "#"+n)
		}
	}
	if m := titleRef.FindStringSubmatch(it.Title); m != nil {
		add(m[1])
	}
	for _, m := range closesRef.FindAllStringSubmatch(it.Body, -1) {
		add(m[1])
	}
	if len(refs) == 0 {
		return ""
	}
	return "linked: " + strings.Join(refs, ", ")
}

// changedFiles is a pull request's files as the brief lists them, the most
// changed first as the source keeps them: `changed files (6):` and a line
// each, `path +62 −0`.
func changedFiles(it factory.Item) []string {
	if len(it.Files) == 0 {
		return nil
	}
	lines := []string{"changed files (" + strconv.Itoa(len(it.Files)) + "):"}
	for i, f := range it.Files {
		if i == briefFilesMost {
			lines = append(lines, "… "+plural(len(it.Files)-i, "more file"))
			break
		}
		if p := strings.TrimSpace(f.Path); p != "" {
			lines = append(lines, p+" +"+strconv.Itoa(f.Added)+" −"+strconv.Itoa(f.Removed))
		}
	}
	return lines
}

// itemComments is the last things said on the item, oldest first, each on
// one line and cut: `the last comments, oldest first:` and `- alice: …`.
func itemComments(it factory.Item) []string {
	var lines []string
	for _, c := range it.Comments {
		body := oneLine(c.Body)
		if body == "" {
			continue
		}
		if r := []rune(body); len(r) > briefCommentMost {
			body = strings.TrimSpace(string(r[:briefCommentMost])) + " …"
		}
		who := oneLine(c.Author)
		if who == "" {
			who = "someone"
		}
		lines = append(lines, "- "+who+": "+body)
	}
	if len(lines) == 0 {
		return nil
	}
	return append([]string{"the last comments, oldest first:"}, lines...)
}

// workTree is the sentence that says what the step's folder holds and how
// to read the change in it. A pull request's worktree is cut at its head
// (workdir.go), so the change is a `git diff` against its base; an issue's
// is the default branch on a branch of the item's own, with nothing of the
// work in it yet. "" for a round with no folder.
func workTree(job Job) string {
	if strings.TrimSpace(job.Dir) == "" {
		return ""
	}
	it := job.Item
	if isPull(it) {
		base := PullBase(it)
		return "your work tree is checked out at this pull request's head. `git diff " + base + "...HEAD` is the whole change, `git diff --stat " + base +
			"...HEAD` its files and `git log " + base + "..HEAD` its commits; read the files there. You need neither the network nor the forge to see what you are working on."
	}
	return "your work tree is the repository on the item's own branch, " + itemBranch(it) +
		", cut from the default branch; what earlier steps changed is in it, and `git diff HEAD` and `git log` show it."
}
