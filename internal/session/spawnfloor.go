package session

// THE SPAWN FLOOR: a trivial ask is answered in the conversation, and no door
// into the graph may convert it.
//
// THE MEASURED FAILURE (F16/F26). A person typed "commit everything with a
// sensible message". The chat staged the files, then the write seam said
// "this is changing more than a quick edit · moving it to a task" and spawned
// task 5. The commit never happened. A one-command ask was translated into a
// worktree, and the deliverable was dropped.
//
// The route judge already proved that reading the REQUEST cannot judge the
// work, and the checkpoint already reads the work. What neither had was a
// hard floor on the ASK itself: if the words are a single command or a
// known-trivial verb — commit, undo, a one-file or one-line edit, a single
// read — the turn stays here. The prompt already says small work is not
// work; a prompt is remembered exactly as often as the model remembers it,
// and F26 is the turn it forgot.
//
// SO THIS IS CODE, NOT ANOTHER SENTENCE IN A BRIEF. Every door that starts a
// task from a conversation — propose_task, the route judge, the checkpoint
// handover, the write seam that shares that handover — asks [trivialAsk]
// before it spends anything. A person who typed `/task commit everything`
// still gets a task: they asked for one. A genuine multi-part or multi-file
// ask is not in this set, and the other gates still convert it.
//
// A WRONG YES IS THE COSTLY DIRECTION. Refusing a real piece of work keeps
// it in the conversation, which is slower and still does it. Converting a
// commit is how the commit is lost. The matcher is a closed set and nothing
// else.

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// spawnFloorRefusal is what propose_task reads back when the person's words
// are a trivial ask. It names the fix the model can act on: do the command
// here. It is a result, not an error, so the turn continues.
const spawnFloorRefusal = "this ask is one command — do it here. A commit, an undo, a one-file edit or a single read stays in the conversation; handing it to a task is how the work gets dropped."

// refuseProposedTask is every check that can turn a propose_task call around
// BEFORE a card is raised or a slot is taken: a proposal that left out the
// program the person named, the spawn floor, then a depends_on that can never
// resolve. They used to live as endings of [Agent.proposeTask]; they are here
// so that road does not grow (the complexity ratchet holds it at 16).
//
// THE PROGRAM THE PERSON NAMED COMES FIRST, and it lifts the floor. "fix this
// file with senior-dev" is a one-file fix on the floor's reading and an ask
// for a program on the person's: the proposal without `via` is turned back
// to name it, and the one that names it is not refused for being small
// (delegate_asked.go). A proposal naming a program the person did not ask
// for meets the floor as any proposal does.
//
// IT ANSWERS A STAGED CALL, nil for none, because the bounce is not a settled
// refusal: it marks the message it was made for, and a call withdrawn before
// it went ahead takes that mark back ([askBounce.Withdraw]).
func (a *Agent) refuseProposedTask(spec taskSpec) bare.Staged {
	if bounce := a.programAskBounce(spec); bounce != nil {
		return bounce
	}
	if !a.config.InTask {
		if trivialAsk(a.taskRequest()) && !a.askedForProgram(spec.via) {
			return bare.Settled(spawnFloorRefusal, true)
		}
	}
	if missing, failed := a.graph().doomedDependencies(spec.dependsOn); len(missing)+len(failed) > 0 {
		if bashBeltAsked() {
			missing = a.missingRunDependencies(missing)
		}
		if len(missing)+len(failed) > 0 {
			return bare.Settled(dependencyRefusal(missing, failed), true)
		}
	}
	return nil
}

// spawnFloorWide is the words that mean the ask has MORE THAN ONE piece of
// work in it. An "and" or a sweep lifts the floor: "commit everything and
// rewrite the tests" is two jobs, and the other gates may still convert it.
var spawnFloorWide = map[string]bool{
	"and":     true,
	"then":    true,
	"across":  true,
	"every":   true,
	"several": true,
	"each":    true,
	"both":    true,
}

// trivialAsk reports whether the person's words are a single command or a
// known-trivial verb that must stay in the conversation.
//
// THE TEST IS THE ASK, not the work so far. A "commit" that has already
// staged five files is still a commit, and converting it is how F26 lost
// the commit. Breadth of what the turn has touched does not lift the floor.
func trivialAsk(asked string) bool {
	words := dropAskLeadIn(normalizedWords(asked))
	if len(words) == 0 {
		return false
	}
	// An explicit ask for a task lifts the floor. "as a task", "make this a
	// task", "spin it off", "hand it to a task" is the person OVER RULING the
	// trivial matcher, exactly as `/task` does. Without this, "as a task: fix
	// the one-file bug" ran inline and "continue task N" had no task to
	// continue (R1).
	if wantsTask(normalizedWords(asked)) {
		return false
	}
	if hasWideSignal(words) {
		return false
	}
	head, rest := words[0], words[1:]
	if head == "git" && len(rest) > 0 {
		head, rest = rest[0], rest[1:]
	}
	switch head {
	case "commit", "undo", "revert":
		return true
	case "read":
		return isSingleRead(rest)
	case "fix", "edit", "change", "patch":
		return isOneFileOrLineEdit(words)
	}
	return false
}

// dropAskLeadIn strips the politeness a person puts in front of a command
// so "please commit everything" and "can you undo that" match the same
// floor as the bare verb. It stops at the first word that is the ask.
func dropAskLeadIn(words []string) []string {
	for len(words) > 0 {
		switch words[0] {
		case "please", "just":
			words = words[1:]
			continue
		}
		if len(words) >= 2 && words[1] == "you" {
			switch words[0] {
			case "can", "could", "would", "will":
				words = words[2:]
				continue
			}
		}
		return words
	}
	return words
}

// wantsTask reports whether the person's words explicitly asked for a task:
// "as a task", "make this a task", "spin it off", "hand-off/hand it to a task",
// "as a subtask". A one-file fix is still a task when the person said so.
func wantsTask(words []string) bool {
	joined := strings.Join(words, " ")
	for _, phrase := range []string{
		"as a task", "as task", "make it a task", "make this a task",
		"spin off", "spin-off", "spin it off", "hand off", "hand-off",
		"hand it to a task", "as a subtask",
	} {
		if strings.Contains(joined, phrase) {
			return true
		}
	}
	return false
}

func hasWideSignal(words []string) bool {
	for _, word := range words {
		if spawnFloorWide[word] {
			return true
		}
	}
	return false
}

// isSingleRead is one file (or "this file"), not a sweep. "read the four
// files" is the ask the write-seam tests already convert around, and it
// stays convertible.
func isSingleRead(rest []string) bool {
	for _, word := range rest {
		switch word {
		case "files", "dirs", "directories", "packages", "sources":
			return false
		}
	}
	return true
}

// isOneFileOrLineEdit is the explicit one-thing phrasing, never a bare
// "fix the crash". Existing propose_task tests open on that sentence, and
// a floor that ate them would be a floor that ate real work.
func isOneFileOrLineEdit(words []string) bool {
	joined := strings.Join(words, " ")
	switch {
	case strings.Contains(joined, "one line"),
		strings.Contains(joined, "this line"),
		strings.Contains(joined, "that line"),
		strings.Contains(joined, "one file"),
		strings.Contains(joined, "this file"),
		strings.Contains(joined, "that file"):
		return true
	}
	return false
}
