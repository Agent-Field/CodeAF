package session

// WHAT WORK RUNNING ON ITS OWN MAY DO TO A REPOSITORY, which is a question about
// its AUTHORITY and not about its manners.
//
// ── THE BREACH THIS WAS WRITTEN FROM ──
//
// A task was asked to fix ten issues in a Python repository. One of its parts,
// working in its own worktree, ran:
//
//	git merge --ff-only main
//
// and fast-forwarded itself onto a branch that already held the upstream
// project's own fixes for the very issues it had been asked to fix. It then
// reported the work as done. Everything it claimed was true and none of it was
// its: the deliverable came home carrying somebody else's commits, and a reader
// looking at the branch could not tell which lines the task wrote. A sibling
// worker in the same run merged one of its OWN family's branches by hand, which
// is the landing road's job and nobody else's (task_run.go's comeHome).
//
// Neither of those is a bad answer to the brief. They are a worker reaching for
// authority it does not have, and no wording of a brief will reliably stop that —
// a model that can see a branch with the answer on it will take the branch. So
// this is a mechanism, fitted where the command is about to run.
//
// ── THE RULE, IN ONE SENTENCE ──
//
// A TASK WORKER'S GIT MAY READ ANYTHING AND MOVE NOTHING. A session that holds
// its own acceptance and decides on its own word that the work is done answers
// to the same list. Both may look wherever they like — log, diff, show,
// merge-base, any branch in the repository — because knowing what is around
// them is how they do the work. Both may SAVE: add and commit leave the work in
// the copy that will be judged. What neither may do is reach a REMOTE, or move
// this working copy onto work it did not do itself.
//
// ── WHY REFS AND NOT PATHS ──
//
// A node's worktree is cut from the person's own repository (task_run.go's
// prepareTaskTree), so it SHARES the object store and every branch in it. A guard
// written about directories would have passed `git merge --ff-only main` without
// blinking: the command ran inside the worktree, on the worktree's own HEAD, and
// touched no path outside it. The thing that was breached was never a directory.
// It was the answer to "whose work is this".
//
// ── WHAT IS DELIBERATELY NOT HERE ──
//
// THE PERSON'S CONVERSATION IS UNTOUCHED. A session somebody is sitting in front
// of, in a checkout they opened themselves, may pull, stash, rebase and merge
// exactly as they would at their own terminal — that is their repository and
// their decision, and a harness that policed it would be answering a question
// nobody asked. An unattended session with no ceiling has the same [Person]
// posture. The other posture is read from [Agent.steward], the one existing
// answer to whether the session decides that its own work is done.
//
// AND THE READING HALF IS NOT NARROWED BY ONE COMMAND. Every refusal below is a
// verb that WRITES; a worker that wants to know what is on main still runs
// `git log main`, `git show main:file` and `git diff HEAD main` and gets the whole
// answer. The line the guard draws is between knowing and taking.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// taskGitGuard is the control plane's citizen for the rule above: git belonging
// to work that will be judged on its own word may read anything and move nothing.
//
// IT IS A HOOK AND NOT A WRAPPER ON THE BELT'S BASH, for the reason loop.go gives
// about the chokepoint: the pre-action seam is the one moment EVERY execution
// passes through, while a wrapper fitted around [Agent.backgroundBash] would be
// missed by the two belts that rebuild bash from bare directly — an auditor's
// (task_audit.go) and a hand's (fork.go). The seam is also where the refusal can
// be a RESULT THE MODEL READS rather than an error that ends anything: a worker
// told no can pick a different command and carry on, which is the whole reason
// the sentence names what to do instead.
//
// It runs after the write scope and last of all, and its position carries no
// argument beyond tidiness: it is the only citizen that reads bash, and no other
// citizen reads what it writes.
type taskGitGuard struct{ agent *Agent }

func (taskGitGuard) Name() string { return "task-git" }

func (g taskGitGuard) PreAction(_ context.Context, _ *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	if call.Function.Name != "bash" {
		return call, toolResult{}, true
	}
	voice, guarded := g.whoseCopy()
	if !guarded {
		return call, toolResult{}, true
	}
	var parsed struct {
		Command string `json:"command"`
	}
	// A call whose arguments do not parse is bash's own to complain about, and a
	// guard that refused it here would be answering a different fault in worse
	// words.
	if err := json.Unmarshal([]byte(call.Function.Arguments), &parsed); err != nil {
		return call, toolResult{}, true
	}
	if why := refusedTaskGit(parsed.Command, voice); why != "" {
		return call, toolResult{text: why, isError: true}, false
	}
	return call, toolResult{}, true
}

// whoseCopy answers whose copy this session's git is about, and whether this
// guard answers for it at all.
//
// THE PRINCIPAL IS THE GATE. A task has a copy that lands on somebody else's
// word, while a steward-headed session has a copy it will judge on its own word;
// both need the same verbs and different reasons. A [Person] keeps the terminal's
// authority whether the session is attended or merely has no ceiling.
func (g taskGitGuard) whoseCopy() (gitVoice, bool) {
	if g.agent == nil {
		return gitVoice{}, false
	}
	if g.agent.config.InTask {
		return taskGitVoice, true
	}
	if g.agent.steward() != nil {
		return sessionGitVoice, true
	}
	return gitVoice{}, false
}

// gitVoice is the half of a refusal that says WHOSE COPY THIS IS. There is one
// list of verbs because the acts are the same, and two registers because a
// task's work comes home through a landing while a session's work is judged
// exactly where it stands.
type gitVoice struct {
	opening          string // The opening says whether the refusal belongs to a task or to this session.
	remote           string // The remote tail says why somebody else's work may not be brought in.
	moves            string // The moving tail says why the copy may not be put onto work it did not do.
	stash            string // The stash tail says why taking the working copy away makes its claim false.
	discard          string // The discard tail says why throwing the working copy away is not allowed.
	offAnotherBranch string // The restore tail says why taking a file off another branch is not allowed.
	push             string // Push is whole because its reason is a road rather than the state of the copy.
}

var taskGitVoice = gitVoice{
	opening:          "is not yours to run",
	remote:           "it would bring in work this task did not do, and this task reports what it writes as its own. " + taskGitInstead,
	moves:            "it would put work this task did not do into your copy, and only what you write here comes home. " + taskGitInstead,
	stash:            "it takes your working copy away and puts it back, and a stash that will not go back cleanly leaves conflict markers in the files. " + taskGitInstead,
	discard:          "it throws your working copy away, and what is in it is the deliverable. " + taskGitInstead,
	offAnotherBranch: "it takes a file off another branch, and only what you write here comes home. " + taskGitInstead,
	push:             taskPushRefusal,
}

var sessionGitVoice = gitVoice{
	opening:          "is not yours to run here",
	remote:           "it would bring in work this session did not do, and this session reports what it writes as its own. " + sessionGitInstead,
	moves:            "it would put work this session did not do into your copy, and what is in it is the work this session will be judged on. " + sessionGitInstead,
	stash:            "it takes your working copy away, and what is in it is the work this session will be judged on. " + sessionGitLeaveIt,
	discard:          "it throws your working copy away, and what is in it is the work this session will be judged on. " + sessionGitInstead,
	offAnotherBranch: "it takes a file off another branch, and what is in it is the work this session will be judged on. " + sessionGitInstead,
	push:             "git push is not yours to run here: it would send this session's work to a shared remote on nobody's word but its own. " + sessionGitInstead,
}

// gitReachesARemote are the subcommands that FETCH from another machine. They are
// refused for a worker whether or not a remote is configured: what is wrong with
// them is not that they might fail, it is that a node's working copy is a copy of
// what the person has, and a node that went and got something newer is reporting
// on a repository nobody asked it about.
//
// `push` IS NOT ONE OF THEM, and the split is the repair for a refusal that was
// false. These five bring somebody else's work IN; push sends this task's own
// work OUT, past the landing that is the road it comes home on — a different act,
// which taskoutside.go answers with a sentence about where the work actually goes.
var gitReachesARemote = map[string]bool{
	"pull":      true,
	"fetch":     true,
	"clone":     true,
	"remote":    true,
	"submodule": true,
}

// gitMovesTheWork are the subcommands that put work this node did not do into
// this node's working copy or onto its HEAD. Every one of them ends with a
// worktree whose contents are somebody else's and whose report would say they
// were this node's.
//
// `merge` is the measured one. The rest are the same act by another verb, and
// they are listed rather than inferred because a guard that tried to be clever
// about which merge was harmless is a guard that will one day let the harmful one
// through: there is no form of any of these that a node handed one brief and one
// worktree needs.
var gitMovesTheWork = map[string]bool{
	"merge":        true,
	"rebase":       true,
	"cherry-pick":  true,
	"revert":       true,
	"am":           true,
	"apply":        true,
	"checkout":     true,
	"switch":       true,
	"worktree":     true,
	"update-ref":   true,
	"symbolic-ref": true,
}

// gitOnlyReadsWhenItSaysSo are the subcommands whose HARMLESS FORM is worth
// keeping, keyed to the word that makes them harmless.
//
// Each of the three is a verb a working node reaches for honestly, and each has
// exactly one shape that does not move anything: reading the shelf rather than
// taking off it, unstaging rather than throwing the worktree away, and putting a
// path back rather than fetching a path from another branch. The rest of each
// verb is refused with everything else.
var gitOnlyReadsWhenItSaysSo = map[string][]string{
	// `git stash list` and `git stash show` read the shelf. Every other stash
	// takes the worktree away and puts it back, and a conflicted pop leaves
	// conflict markers in files nobody is going to look at again — which is how
	// a person's own checkout was found holding raw `<<<<<<< Updated upstream`
	// lines the morning after a run.
	"stash": {"list", "show"},
	// `git reset <path>` unstages. `--hard`, `--merge` and `--keep` throw the
	// working copy away, which for a node is throwing away its deliverable.
	"reset": {},
	// `git restore <path>` puts a file back as this worktree last had it.
	// `--source` makes it a way of fetching content off another branch, which is
	// the whole thing this file exists to stop.
	"restore": {},
}

// gitResetKeepsNothing are the reset flags that discard the working copy.
var gitResetKeepsNothing = map[string]bool{"--hard": true, "--merge": true, "--keep": true}

// refusedTaskGit reads one bash command and answers with the sentence its voice
// gets back instead of running it, or "" for a command it may run.
//
// IT SCANS THE WHOLE COMMAND AND NOT THE FIRST WORD. Every breach that was
// measured arrived inside a chain — `cd <worktree> && git merge --ff-only main`,
// `git log … && echo ---- && git diff …` — and a check that read only the first
// token would have passed all of them. So every `git` in the string is found and
// the subcommand after it is read, with git's own global options skipped on the
// way (`-C dir`, `-c key=value`, `--git-dir=…`), because `git -C somewhere merge`
// is the same act with the target said out loud.
//
// AND `git` HAS TO BE STARTING A COMMAND, not merely appearing in one. The word
// counts when it opens the whole line or follows something that ended the segment
// before it — `&&`, `;`, `|`, an opening bracket, a substitution — which is what
// tells a command apart from the same three letters inside a sentence somebody is
// echoing into a file. A quoted word is left alone precisely so that
// `echo 'git merge is a thing people talk about'` is what it looks like.
//
// The scan errs toward refusing rather than toward missing, and the two costs are
// not close: a false positive is one sentence a node reads and works around,
// while a false negative is a deliverable made of somebody else's commits.
func refusedTaskGit(command string, voice gitVoice) string {
	words := strings.Fields(command)
	for index, word := range words {
		if gitWord(word) != "git" {
			continue
		}
		if index > 0 && !endsASegment(words[index-1]) {
			continue
		}
		verb, rest := gitSubcommand(words[index+1:])
		if verb == "" {
			continue
		}
		if why := refusedGitVerb(verb, rest, voice); why != "" {
			return why
		}
	}
	return ""
}

// gitWord strips the shell punctuation that can sit against a command's own name
// — a group, a subshell, a substitution, a trailing separator — and NOT the quotes
// that make it a piece of text instead.
func gitWord(word string) string {
	word = strings.TrimPrefix(word, "$(")
	word = strings.TrimLeft(word, "`({")
	return strings.TrimRight(word, ";")
}

// endsASegment reports whether the word before a command closed whatever came
// before it, which is the whole of what makes the next word a command.
func endsASegment(word string) bool {
	switch word {
	case "then", "else", "do", "!", "time", "sudo", "env", "exec", "nohup":
		return true
	}
	return word != "" && strings.ContainsRune(";|&({", rune(word[len(word)-1]))
}

// gitSubcommand reads the subcommand out of the words after `git`, stepping over
// git's own global options, and answers it with whatever followed it.
func gitSubcommand(words []string) (string, []string) {
	for index := 0; index < len(words); index++ {
		word := words[index]
		switch {
		case word == "-C" || word == "-c":
			// These take a value, which is the next word and never a subcommand.
			index++
		case strings.HasPrefix(word, "-"):
			// --git-dir=…, --no-pager, -P and the rest carry their own value or
			// none, so nothing has to be skipped.
		default:
			return word, words[index+1:]
		}
	}
	return "", nil
}

// refusedGitVerb is the decision about one subcommand, and the sentence for it.
//
// THE SENTENCE SAYS WHAT TO DO INSTEAD, in the same breath as the refusal. A node
// told only "no" spends its next three steps trying the same thing in other words
// — which is a real cost, and which the no-progress counter would then read as
// spinning.
func refusedGitVerb(verb string, rest []string, voice gitVoice) string {
	switch {
	case verb == "push":
		return voice.push
	case gitReachesARemote[verb]:
		// IT DOES NOT SAY "reaches no remote", which is what this sentence said
		// until a task was pointed at the person's live checkout and told that
		// about it. What is true of every copy a task works in, wherever it
		// stands, is the second clause: work it did not do is not its to bring in.
		return "git " + verb + " " + voice.opening + ": " + voice.remote
	case gitMovesTheWork[verb]:
		return "git " + verb + " " + voice.opening + ": " + voice.moves
	}
	allowed, guarded := gitOnlyReadsWhenItSaysSo[verb]
	if !guarded {
		return ""
	}
	switch verb {
	case "stash":
		if len(rest) > 0 && containsWord(allowed, rest[0]) {
			return ""
		}
		return "git stash " + voice.opening + ": " + voice.stash
	case "reset":
		for _, word := range rest {
			if gitResetKeepsNothing[word] {
				return "git reset " + word + " " + voice.opening + ": " + voice.discard
			}
		}
		return ""
	case "restore":
		for _, word := range rest {
			if word == "-s" || word == "--source" || strings.HasPrefix(word, "--source=") {
				return "git restore --source " + voice.opening + ": " + voice.offAnotherBranch
			}
		}
		return ""
	}
	return ""
}

// taskGitInstead is the second half of every refusal above, spelled once. It
// names the whole of what a worker's git IS — look wherever you like — and then
// points at the road its work actually comes home on.
//
// IT DOES NOT OFFER `git add`, and the prompt is why: a node's deliverable is
// every path it passed to write or edit, staged BY NAME on the landing road
// (prompts/worker.md's "What comes home", task_run.go's stageTaskWork). A refusal
// that sent a worker off to stage its own work would be the harness contradicting
// its own brief in the one sentence the worker is certain to read.
const taskGitInstead = "Look with git status, diff, log and show — any branch, as much as you want. What you write with write and edit in this copy comes home on its own."

// sessionGitLeaveIt is the shortest true answer to a session trying to take its
// own work out of the tree, and is spelled once because the stash sentence is
// quoted exactly in the manual.
const sessionGitLeaveIt = "Leave the change in the tree, or commit it."

// sessionGitInstead is the second half of every session refusal except stash,
// which has already named the one thing the session must do next.
const sessionGitInstead = "Look with git status, diff, log and show — any branch, as much as you want. " + sessionGitLeaveIt

// containsWord reports whether a word is in a short list.
func containsWord(list []string, word string) bool {
	for _, item := range list {
		if item == word {
			return true
		}
	}
	return false
}
