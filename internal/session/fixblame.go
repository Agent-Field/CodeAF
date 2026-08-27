package session

// WHICH FAILURES A MEMORY OF FIXES MAY LEARN FROM.
//
// fixstore.go pairs a failed call with the command that ran next and did not
// fail, and files the pairing as a fix. THAT INFERENCE IS SOUND FOR EXACTLY ONE
// KIND OF FAILURE: one the model caused by acting, and could have avoided by
// acting differently. For every other kind the "fix" is whatever the model
// happened to type next, and the store fills with coincidences it then reads
// back as advice.
//
// ── THE MEASURED FAILURE THAT PUT THIS HERE ──
//
// On a SWE-Marathon run the sidecar's file read `{asked: 14, found: 2,
// worked: 0, failed: 0}` and both of its entries carried the same patch — `pwd`
// — filed under two different errors: `git: command not found`, from a
// container with no git in it, and `refused: bash <path> is not verification …`,
// the checker's own door saying no. Neither is a failure a command fixed. There
// is no git on that machine and there was never going to be, and the door
// refused the call before anything ran. `pwd` was simply the next thing the
// model typed. An hour later the store read it back to the checker as
// "this exact error was fixed 1/1 times before · what worked: pwd".
//
// ── THE LAW ──
//
// A MEMORY OF FIXES LEARNS ONLY FROM FAILURES THE MODEL CAN FIX BY ACTING
// DIFFERENTLY IN THE WORLD. Three kinds are therefore never learned from, and
// never answered:
//
//	(a) A DOOR SAID NO. A refusal is the harness's own sentence, written by the
//	    harness, produced BEFORE anything ran. Nothing in the world moved, and
//	    nothing the model types next moves the door.
//	(b) THE HAND IS NOT THERE. A tool the belt does not carry, or has taken
//	    away, is answered in the harness's words for the same reason.
//	(c) THE PROGRAM IS NOT ON THIS MACHINE. `git: command not found` is not a
//	    command that ran badly; it is the absence of a command. Routing around
//	    an absence is not a patch anybody can be handed later — the next session
//	    on a machine that HAS the program needs no patch, and one on a machine
//	    that does not needs its own route.
//
// ── WHY THIS IS NOT A LIST OF ERROR MESSAGES ──
//
// A list of error texts is the thing that cannot be maintained: every door
// reworded is a silent regression, and every door added is a hole nobody sees.
// So the two recognisers below are each ONE STRUCTURAL FACT, not a catalogue.
//
// [harnessRefusalOpeners] is the harness's own VOICE — the word every door in
// this package opens with when it refuses, from the consent gate (consent.go's
// [refusal]) to the checker's and the hand's reading-only bash (task_audit.go's
// [refuseOutsideAllowlist], installed by fork.go too) to the dispatch that has
// no such tool ([Agent.executeTool]). It is one word, owned here, and
// fixblame_test.go pins the live doors against it so a reworded refusal fails a
// test rather than quietly teaching the store nonsense.
//
// [absentProgramMarks] is the POSIX shells' one sentence for "this machine does
// not have that program" — the same fact in the four spellings the shells and
// the Go exec package give it. It is a convention, older than this repository,
// not a message anybody here maintains.
//
// Most refusals never reach the sidecar at all, and that is by construction:
// the consent gate's answer is returned straight out of [Agent.executeTool]
// before the result-growing seam, and so is "Unknown tool". The two recognised
// here are the ones a GUARD WRAPPED AROUND A TOOL produces, which arrive as an
// ordinary error result because that is exactly what a guard has to look like
// to the model.

import "strings"

// harnessRefusalOpeners are the openings that say the harness itself answered.
//
// Matched on the FIRST diagnostic line and case-insensitively, because a
// refusal puts its refusal first — a door that buried "refused" in its third
// paragraph would be a door no model reads either.
var harnessRefusalOpeners = []string{
	"refused:",
	"refused in a task:",
	"unknown tool:",
	"denied by",
}

// absentProgramMarks are the shells' words for A PROGRAM THIS MACHINE DOES NOT
// HAVE, in the spellings the shells and the Go exec package give it.
//
//	bash: git: command not found
//	zsh: command not found: git
//	exec: "git": executable file not found in $PATH
var absentProgramMarks = []string{
	"command not found",
	"executable file not found",
}

// absentProgramEnding is dash's and ash's spelling of the same fact:
//
//	sh: 1: git: not found
//
// It is matched at the END of the diagnostic line and nowhere else. An
// unanchored "not found" is in half the error messages ever written — a missing
// module, a missing key, a search that matched nothing — and most of those ARE
// failures a command fixes. Anchoring costs the odd shell that words it
// differently, and that miss is silence; the other direction is the store
// learning from an absence again.
const absentProgramEnding = ": not found"

// blamelessFailure reports whether a failed result is one NOTHING THE MODEL
// TYPES NEXT COULD HAVE FIXED, and which the sidecar must therefore neither
// learn from nor answer.
//
// It reads the diagnostic line rather than the whole result, and it reads it
// through the same chooser the signature is made from ([fixDiagnosticLine]):
// the two must agree about which line of a failed result IS the failure, or a
// refusal would be recognised here and keyed on there.
func blamelessFailure(text string) bool {
	line, found := fixDiagnosticLine(text)
	if !found {
		return false
	}
	lowered := strings.ToLower(line)
	for _, opener := range harnessRefusalOpeners {
		if strings.HasPrefix(lowered, opener) {
			return true
		}
	}
	for _, mark := range absentProgramMarks {
		if strings.Contains(lowered, mark) {
			return true
		}
	}
	return strings.HasSuffix(lowered, absentProgramEnding)
}
